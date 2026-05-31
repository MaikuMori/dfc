package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/MaikuMori/dfc/internal/index"
	"github.com/MaikuMori/dfc/internal/project"
	"github.com/muesli/reflow/wordwrap"
	"golang.org/x/term"
)

// SearchCmd queries the FTS5-backed index. Default scope is the cwd's
// project — use `-a/--all` (or the dedicated `dfc ss` command) to query
// across every project. Bare words are AND-ed and prefix-matched
// (`mac` → `mac*` → matches `macos`); phrases stay exact; `-term`
// negates exactly.
type SearchCmd struct {
	Project string   `name:"project" short:"p" predictor:"project" help:"Restrict to this project slug (overrides cwd)."`
	All     bool     `name:"all" short:"a" help:"Search every project, not just the current one."`
	Tag     []string `name:"tag" predictor:"tag" help:"Restrict to projects carrying this tag. Repeatable / comma-separated."`
	Status  string   `name:"status" enum:"open,done,all" default:"all" help:"Filter by status."`
	Limit   int      `name:"limit" short:"n" default:"20" help:"Maximum hits to return."`
	Sort    string   `name:"sort" enum:"score,modified,created" default:"score" help:"Order results by score, modification time, or creation time."`
	JSON    bool     `name:"json" help:"Emit one SearchHit JSON object per line."`
	Reindex bool     `name:"reindex" help:"Drop and rebuild the index from disk before searching."`
	Query   []string `arg:"" optional:"" help:"Query string (joined with spaces)."`
}

// SsCmd is the explicit global-scope sibling. Same flags, but starts
// out searching every project.
type SsCmd struct {
	Project string   `name:"project" short:"p" predictor:"project" help:"Restrict to this project slug."`
	Tag     []string `name:"tag" predictor:"tag" help:"Restrict to projects carrying this tag. Repeatable / comma-separated."`
	Status  string   `name:"status" enum:"open,done,all" default:"all" help:"Filter by status."`
	Limit   int      `name:"limit" short:"n" default:"20" help:"Maximum hits to return."`
	Sort    string   `name:"sort" enum:"score,modified,created" default:"score" help:"Order results by score, modification time, or creation time."`
	JSON    bool     `name:"json" help:"Emit one SearchHit JSON object per line."`
	Reindex bool     `name:"reindex" help:"Drop and rebuild the index from disk before searching."`
	Query   []string `arg:"" optional:"" help:"Query string (joined with spaces)."`
}

func (c *SsCmd) Run() error {
	return (&SearchCmd{
		Project: c.Project,
		All:     true,
		Tag:     c.Tag,
		Status:  c.Status,
		Limit:   c.Limit,
		Sort:    c.Sort,
		JSON:    c.JSON,
		Reindex: c.Reindex,
		Query:   c.Query,
	}).Run()
}

// searchHitOut is the JSON shape on the wire. The Task field reuses
// our existing TaskOut so consumers have a single parsing path.
type searchHitOut struct {
	Task    TaskOut `json:"task"`
	Score   float64 `json:"score"`
	Snippet string  `json:"snippet,omitempty"`
}

func (c *SearchCmd) Run() error {
	cr, err := openCore()
	if err != nil {
		return err
	}
	defer func() { _ = cr.Close() }()

	if c.Reindex {
		if err := cr.Reindex(); err != nil {
			return err
		}
	} else {
		// Drift sync failures shouldn't block a search outright — Core
		// logs them to stderr and we continue with whatever the index
		// currently holds.
		cr.EnsureFresh()
	}

	if len(c.Query) == 0 {
		// Empty query is legal only with --reindex; otherwise nothing
		// to do.
		if c.Reindex {
			fmt.Fprintln(os.Stderr, "reindexed")
			return nil
		}
		return errors.New("missing search query")
	}
	q := strings.Join(c.Query, " ")

	// Resolve effective scope:
	//   - explicit -p slug/name wins (resolved through the registry)
	//   - --all forces global
	//   - otherwise default to cwd's project (the `dfc s` shape the
	//     user expects). `dfc ss` is the global-by-default sibling.
	scope := c.Project
	switch {
	case scope != "":
		resolved, _, found, err := resolveProjectInput(scope)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("unknown project %q", scope)
		}
		scope = resolved
	case !c.All:
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		resolved, err := project.Resolve(cwd)
		if err != nil {
			return err
		}
		scope = resolved.Slug
	}

	var projectSet []string
	if tagFilter := expandTagArgs(c.Tag); len(tagFilter) > 0 {
		projectSet = projectSlugsMatchingTags(cr.Registry(), tagFilter)
	}

	hits, err := cr.Search(q, index.SearchOpts{
		Project:  scope,
		Projects: projectSet,
		Status:   c.Status,
		Limit:    c.Limit,
		SortBy:   c.Sort,
	})
	if err != nil {
		return err
	}

	if c.JSON {
		for _, h := range hits {
			out := searchHitOut{
				Task:    taskOut(h.Task),
				Score:   h.Score,
				Snippet: h.Snippet,
			}
			if err := writeJSON(os.Stdout, out); err != nil {
				return err
			}
		}
		return nil
	}

	if len(hits) == 0 {
		fmt.Println("no matches")
		return nil
	}

	terms := index.PositiveTerms(q)
	useANSI := term.IsTerminal(int(os.Stdout.Fd()))
	groupByProject := countProjects(hits) > 1
	contentWidth := computeContentWidth()

	var lastProject string
	for i, h := range hits {
		// Blank line between adjacent hits in the same project, so
		// each card has breathing room. Project headers contribute
		// their own spacing.
		switchedProject := groupByProject && h.Task.ProjectSlug != lastProject
		if i > 0 && !switchedProject {
			fmt.Println()
		}
		if switchedProject {
			if lastProject != "" {
				fmt.Println()
			}
			fmt.Println(formatProjectHeader(h.Task.ProjectSlug, useANSI))
			lastProject = h.Task.ProjectSlug
		}

		renderHit(h, terms, contentWidth, useANSI)
	}
	return nil
}

// renderHit emits one search hit using the TUI's row layout:
// icon at column 0, " " (one space), then description / body at
// column 2. Continuation lines indent by two. The dim `[id]` rides
// the last line of the description so the eye lands on the prose.
func renderHit(h index.SearchHit, terms []string, contentWidth int, useANSI bool) {
	idStr := "[" + shortID(h.Task.ID) + "]"

	// Wrap the description on the raw text so width math doesn't have
	// to deal with embedded ANSI escapes. Reserve room on the last line
	// for "  [id]" when it would fit; otherwise the ID becomes its own
	// continuation line, still dim, still aligned.
	descLines := wrapLines(h.Task.Description, contentWidth)
	appendInline := len(descLines[len(descLines)-1])+2+len(idStr) <= contentWidth

	for i, raw := range descLines {
		styled := boldText(renderMarks(highlightTerms(raw, terms), useANSI), useANSI)
		if i == len(descLines)-1 && appendInline {
			styled += "  " + dimText(idStr, useANSI)
		}
		if i == 0 {
			fmt.Printf("%s %s\n", statusGlyph(h.Task.Status), styled)
		} else {
			fmt.Printf("  %s\n", styled)
		}
	}
	if !appendInline {
		fmt.Printf("  %s\n", dimText(idStr, useANSI))
	}

	// If the match landed only in the body, show the surrounding
	// paragraph or heading so the user has the context that pulled
	// the row into the result set. Same column-2 indent as the
	// description continuation lines.
	if !containsAnyFold(h.Task.Description, terms) {
		if para := matchedParagraph(h.Task.Details, terms); para != "" {
			for _, raw := range wrapLines(para, contentWidth) {
				styled := renderMarks(highlightTerms(raw, terms), useANSI)
				fmt.Printf("  %s\n", styled)
			}
		}
	}
}

// wrapLines wraps s at width and returns the lines slice. width clamps
// to a sensible minimum so tiny terminals don't degenerate.
func wrapLines(s string, width int) []string {
	if width < 10 {
		width = 10
	}
	wrapped := wordwrap.String(s, width)
	return strings.Split(wrapped, "\n")
}

// computeContentWidth returns the column budget for description / body
// lines, i.e. terminal width minus the 2-column icon gutter. Falls
// back to 80 when stdout isn't a terminal (piped output keeps wrapping
// to a reasonable default so logs and review tools don't get one
// 4000-char line per task).
func computeContentWidth() int {
	const iconWidth = 2
	w := 80
	if cols, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && cols > 0 {
		w = cols
	}
	w -= iconWidth
	if w < 20 {
		w = 20
	}
	return w
}

// boldText wraps s with ANSI bold codes for TTY output. Falls back to
// the plain string when stdout is piped so a downstream parser doesn't
// see escape sequences it can't reason about.
func boldText(s string, ansi bool) string {
	if !ansi {
		return s
	}
	return "\x1b[1m" + s + "\x1b[22m"
}

// dimText wraps s with ANSI faint codes for visually deprioritized
// secondary info (task IDs, project headers).
func dimText(s string, ansi bool) string {
	if !ansi {
		return s
	}
	return "\x1b[2m" + s + "\x1b[22m"
}

func formatProjectHeader(slug string, ansi bool) string {
	return dimText("── "+slug+" ──", ansi)
}

// countProjects reports how many distinct projects show up in hits —
// used to decide whether to group output.
func countProjects(hits []index.SearchHit) int {
	seen := map[string]struct{}{}
	for _, h := range hits {
		seen[h.Task.ProjectSlug] = struct{}{}
	}
	return len(seen)
}

