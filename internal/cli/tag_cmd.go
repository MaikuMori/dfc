package cli

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/MaikuMori/dfc/internal/project"
)

// TagsCmd groups the project-tag management subcommands. With no
// subcommand, prints the current project's tags (default shape).
type TagsCmd struct {
	Show   TagsShowCmd   `cmd:"" default:"withargs" help:"Show a project's tags (default: cwd project)."`
	Add    TagsAddCmd    `cmd:"" help:"Add one or more tags to a project."`
	Rm     TagsRmCmd     `cmd:"" help:"Remove one or more tags from a project."`
	Set    TagsSetCmd    `cmd:"" help:"Replace a project's full tag list."`
	Ls     TagsLsCmd     `cmd:"" aliases:"list" help:"List every distinct tag across the registry."`
}

// TagsShowCmd prints the project's tag list.
type TagsShowCmd struct {
	Project string `name:"project" short:"p" predictor:"project" help:"Project slug or display name (defaults to cwd)."`
	JSON    bool   `name:"json" help:"Emit {slug, tags} as a single JSON object."`
}

func (c *TagsShowCmd) Run() error {
	slug, reg, err := resolveTagTarget(c.Project)
	if err != nil {
		return err
	}
	return emitTagsResult(slug, reg.Tags(slug), c.JSON, false)
}

// TagsAddCmd appends one or more tags to a project's tag list.
type TagsAddCmd struct {
	Project string   `name:"project" short:"p" predictor:"project" help:"Project slug or display name (defaults to cwd)."`
	JSON    bool     `name:"json" help:"Emit {slug, tags} as a single JSON object."`
	Tags    []string `arg:"" predictor:"tag" help:"Tag name(s) to add."`
}

func (c *TagsAddCmd) Run() error {
	slug, reg, err := resolveTagTarget(c.Project)
	if err != nil {
		return err
	}
	for _, t := range expandTagArgs(c.Tags) {
		if err := reg.AddTag(slug, t); err != nil {
			return err
		}
	}
	return emitTagsResult(slug, reg.Tags(slug), c.JSON, true)
}

// TagsRmCmd drops one or more tags from a project's tag list.
type TagsRmCmd struct {
	Project string   `name:"project" short:"p" predictor:"project" help:"Project slug or display name (defaults to cwd)."`
	JSON    bool     `name:"json" help:"Emit {slug, tags} as a single JSON object."`
	Tags    []string `arg:"" predictor:"tag" help:"Tag name(s) to remove."`
}

func (c *TagsRmCmd) Run() error {
	slug, reg, err := resolveTagTarget(c.Project)
	if err != nil {
		return err
	}
	for _, t := range expandTagArgs(c.Tags) {
		if err := reg.RemoveTag(slug, t); err != nil {
			return err
		}
	}
	return emitTagsResult(slug, reg.Tags(slug), c.JSON, true)
}

// TagsSetCmd replaces the entire tag list. Pass no tags to clear.
type TagsSetCmd struct {
	Project string   `name:"project" short:"p" predictor:"project" help:"Project slug or display name (defaults to cwd)."`
	JSON    bool     `name:"json" help:"Emit {slug, tags} as a single JSON object."`
	Tags    []string `arg:"" optional:"" predictor:"tag" help:"Tag name(s); empty list clears."`
}

func (c *TagsSetCmd) Run() error {
	slug, reg, err := resolveTagTarget(c.Project)
	if err != nil {
		return err
	}
	if err := reg.SetTags(slug, expandTagArgs(c.Tags)); err != nil {
		return err
	}
	return emitTagsResult(slug, reg.Tags(slug), c.JSON, true)
}

// TagsLsCmd lists every distinct tag across every project, with
// per-tag project counts.
type TagsLsCmd struct {
	JSON bool `name:"json" help:"Emit one JSON object per tag (NDJSON)."`
}

func (c *TagsLsCmd) Run() error {
	reg, err := project.LoadRegistry()
	if err != nil {
		return err
	}
	all := reg.AllTags()
	if c.JSON {
		for _, t := range all {
			if err := writeJSON(os.Stdout, map[string]any{
				"name":     t.Name,
				"projects": t.Slugs,
			}); err != nil {
				return err
			}
		}
		return nil
	}
	if len(all) == 0 {
		fmt.Println("(no tags)")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for _, t := range all {
		_, _ = fmt.Fprintf(w, "%s\t%d project%s\t%s\n", t.Name, len(t.Slugs), pluralS(len(t.Slugs)), strings.Join(t.Slugs, ", "))
	}
	return w.Flush()
}

// resolveTagTarget routes explicit -p input or the cwd through
// resolveProjectInput / resolveCwdSlug into a canonical slug, and
// returns the loaded registry alongside so callers can mutate without
// reloading.
func resolveTagTarget(explicit string) (slug string, reg *project.Registry, err error) {
	reg, err = project.LoadRegistry()
	if err != nil {
		return "", nil, err
	}
	if explicit != "" {
		s, _, found, ierr := resolveProjectInput(explicit)
		if ierr != nil {
			return "", nil, ierr
		}
		if !found {
			return "", nil, fmt.Errorf("unknown project %q", explicit)
		}
		return s, reg, nil
	}
	s, err := resolveCwdSlug()
	if err != nil {
		return "", nil, err
	}
	if !reg.Has(s) {
		return "", nil, fmt.Errorf("cwd resolves to %q, which isn't a registered project — pass --project explicitly or `dfc c` once to register it", s)
	}
	return s, reg, nil
}

// expandTagArgs flattens repeated positional tags + their comma-split
// forms ("work,oss") into a single slice. Empty tokens are dropped.
func expandTagArgs(in []string) []string {
	var out []string
	for _, raw := range in {
		for p := range strings.SplitSeq(raw, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

// emitTagsResult prints (or JSON-emits) a project's current tag list.
// labelSlug controls whether the human output prefixes the line with
// "<slug>: " (true for after-mutation prints, false for the bare show
// path).
func emitTagsResult(slug string, tags []string, jsonOut bool, labelSlug bool) error {
	if jsonOut {
		return writeJSON(os.Stdout, map[string]any{"slug": slug, "tags": tags})
	}
	if len(tags) == 0 {
		if labelSlug {
			fmt.Printf("%s: (no tags)\n", slug)
		} else {
			fmt.Println("(no tags)")
		}
		return nil
	}
	if labelSlug {
		fmt.Printf("%s: %s\n", slug, strings.Join(tags, ", "))
	} else {
		fmt.Println(strings.Join(tags, ", "))
	}
	return nil
}

// matchesTagFilter reports whether the project at slug matches the given
// --tag filter list (case-insensitive OR). The special token
// "(untagged)" matches projects with empty Tags. Empty filter matches
// everything.
func matchesTagFilter(reg *project.Registry, slug string, filter []string) bool {
	if len(filter) == 0 {
		return true
	}
	tags := reg.Tags(slug)
	hasUntagged := false
	wantTags := make([]string, 0, len(filter))
	for _, f := range filter {
		if strings.EqualFold(f, "(untagged)") {
			hasUntagged = true
			continue
		}
		wantTags = append(wantTags, f)
	}
	if hasUntagged && len(tags) == 0 {
		return true
	}
	for _, w := range wantTags {
		for _, t := range tags {
			if strings.EqualFold(w, t) {
				return true
			}
		}
	}
	return false
}

// projectSlugsMatchingTags returns the set of slugs whose tag list
// satisfies filter. Sorted alphabetically.
func projectSlugsMatchingTags(reg *project.Registry, filter []string) []string {
	all := reg.Slugs()
	if len(filter) == 0 {
		return all
	}
	var out []string
	for _, slug := range all {
		if matchesTagFilter(reg, slug, filter) {
			out = append(out, slug)
		}
	}
	slices.Sort(out)
	return out
}

func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
