package ui

import (
	"strings"
	"sync"
	"time"

	"github.com/MaikuMori/dfc/internal/storage"
	"github.com/muesli/reflow/wordwrap"
)

// glamour rendering for the expanded task is by far the most expensive
// per-frame work on `j`/`k` through a long body. Only one task is expanded
// at a time, so a single-slot cache keyed by the inputs that change the
// output is enough.
var markdownCache struct {
	sync.Mutex
	id       string
	modified time.Time
	width    int
	body     string
}

// renderList returns the body of the list area and a parallel slice of
// per-task visual-line counts. The list grows bottom-up: when total visual
// lines fit in height, blank lines are prepended so the newest item sits
// flush against the input/footer.
//
// heights[i] is the number of visual lines the i-th task occupies in the
// returned string (1 for collapsed, more when expanded).
func renderList(m Model, width, height int) (string, []int) {
	rows, heights := buildRows(m, width)
	total := sum(heights)
	body := strings.Join(rows, "\n")
	if pad := height - total; pad > 0 {
		return strings.Repeat("\n", pad) + body, heights
	}
	return body, heights
}

func buildRows(m Model, width int) (rows []string, heights []int) {
	if len(m.tasks) == 0 {
		return []string{styleEmpty.Render("No tasks yet.")}, []int{1}
	}
	// When a search filter is active, done rows render in the open style so
	// the eye can scan results without strikethrough/dim interfering — the
	// green check is enough state cue in that context.
	flatDone := m.searchQuery != ""
	rows = make([]string, len(m.tasks))
	heights = make([]int, len(m.tasks))
	for i, t := range m.tasks {
		prefix := projectPrefix(m, t)
		var row string
		if m.expandedID != "" && t.ID == m.expandedID &&
			(!m.globalView || t.ProjectSlug == m.expandedSlug) {
			row = renderTaskMarkdown(t, prefix, width)
		} else {
			row = renderRow(t, prefix, i == m.cursor, width, flatDone)
		}
		rows[i] = row
		heights[i] = strings.Count(row, "\n") + 1
	}
	return rows, heights
}

// prefixMaxLen caps the rendered `[project]` width so a chatty project name
// can't eat the row.
const prefixMaxLen = 14

// projectPrefix returns the raw (un-styled) prefix text for t under the
// current view. Empty in per-project view. Content comes from the
// registry's Prefix(slug) which falls back to the last `-`-separated
// slug segment when nothing custom has been set.
func projectPrefix(m Model, t storage.Task) string {
	if !m.globalView {
		return ""
	}
	name := t.ProjectSlug
	if m.core != nil {
		name = m.core.Registry().Prefix(t.ProjectSlug)
	}
	if len(name) > prefixMaxLen-2 {
		name = name[:prefixMaxLen-3] + "…"
	}
	return "[" + name + "] "
}

// renderRow renders one collapsed task row.
//
// flatDone collapses done-vs-open styling: the row paints in the open
// style with just the ✓ glyph swapped in for the ○. Used during search so
// matching done tasks are as easy to scan as open ones.
func renderRow(t storage.Task, prefix string, cursor bool, width int, flatDone bool) string {
	icon := iconOpen
	if t.Status == storage.StatusDone {
		icon = iconDone
	}

	const iconWidth = 2 // "○ " or "✓ "
	indent := strings.Repeat(" ", iconWidth+len(prefix))
	wrapWidth := width - iconWidth - len(prefix)
	if wrapWidth < 1 {
		wrapWidth = 1
	}

	hasDetails := strings.TrimSpace(t.Details) != ""
	done := t.Status == storage.StatusDone

	wrapped := wordwrap.String(t.Description, wrapWidth)
	lines := strings.Split(wrapped, "\n")

	// Open-style rendering (non-cursor): the icon paints separately so a
	// done check mark can be green even while the description sits in the
	// regular foreground. Tag dims so global-view rows scan as one column.
	if !cursor && (!done || flatDone) {
		iconStyled := styleOpen.Render(icon + " ")
		if done {
			iconStyled = styleDoneIcon.Render(icon) + styleOpen.Render(" ")
		}
		first := iconStyled
		if prefix != "" {
			first += styleHint.Render(prefix)
		}
		first += styleOpen.Render(lines[0])
		rest := make([]string, len(lines))
		rest[0] = first
		for i := 1; i < len(lines); i++ {
			rest[i] = styleOpen.Render(indent + lines[i])
		}
		rendered := strings.Join(rest, "\n")
		if hasDetails {
			rendered += " " + styleHint.Render(iconHasDetails)
		}
		return rendered
	}

	// Everything else: raw prefix in the text, single outer style.
	for i, line := range lines {
		if i == 0 {
			lines[i] = icon + " " + prefix + line
		} else {
			lines[i] = indent + line
		}
	}
	if hasDetails && cursor {
		lines[len(lines)-1] += " " + iconHasDetails
	}
	text := strings.Join(lines, "\n")

	var rendered string
	switch {
	case done && cursor:
		rendered = styleDoneCursor.Width(width).Render(text)
	case done:
		rendered = styleDone.Render(text)
	case cursor:
		rendered = styleCursor.Width(width).Render(text)
	default:
		rendered = styleOpen.Render(text)
	}

	if hasDetails && !cursor {
		rendered += " " + styleHint.Render(iconHasDetails)
	}
	return rendered
}

// renderTaskMarkdown renders the full task (heading + details) as styled
// markdown for the expanded view. The icon column ("○ " / "✓ ") is kept
// flush with the collapsed rows, then the rendered block flows below
// indented to align with the title text — so tab feels like the same row
// gaining markdown styling instead of a jumping layout. In global view,
// the project prefix goes between icon and title on the first rendered line
// so the row's ownership stays visible while expanded.
func renderTaskMarkdown(t storage.Task, prefix string, width int) string {
	const iconWidth = 2 // "○ " or "✓ "

	mdWidth := width - iconWidth - len(prefix)
	// Below the markdown renderer's minimum useful width the expanded
	// view would overflow the terminal; fall back to the collapsed row
	// so a tiny screen stays legible.
	if mdWidth < 20 {
		return renderRow(t, prefix, false, width, false)
	}
	rendered := cachedMarkdown(t, mdWidth)
	if rendered == "" {
		return renderRow(t, prefix, false, width, false)
	}

	icon := iconOpen
	if t.Status == storage.StatusDone {
		icon = iconDone
	}
	indent := strings.Repeat(" ", iconWidth+len(prefix))
	lines := strings.Split(rendered, "\n")
	firstSet := false
	for i, line := range lines {
		if !firstSet && strings.TrimSpace(line) != "" {
			if prefix != "" {
				lines[i] = icon + " " + styleHint.Render(prefix) + line
			} else {
				lines[i] = icon + " " + line
			}
			firstSet = true
			continue
		}
		lines[i] = indent + line
	}
	return strings.Join(lines, "\n")
}

// cachedMarkdown returns the glamour-rendered body for t at the given
// width, reusing the previous result when (id, modified, width) match. The
// returned string has already been passed through stripCommonLeadingSpaces.
func cachedMarkdown(t storage.Task, width int) string {
	markdownCache.Lock()
	defer markdownCache.Unlock()
	if markdownCache.id == t.ID &&
		markdownCache.modified.Equal(t.Modified) &&
		markdownCache.width == width &&
		markdownCache.body != "" {
		return markdownCache.body
	}
	src := "# " + t.Description
	if details := strings.TrimSpace(t.Details); details != "" {
		src += "\n\n" + details
	}
	out := renderMarkdown(src, width)
	if out != "" {
		out = stripCommonLeadingSpaces(out)
	}
	markdownCache.id = t.ID
	markdownCache.modified = t.Modified
	markdownCache.width = width
	markdownCache.body = out
	return out
}

// stripCommonLeadingSpaces removes the longest leading-space prefix shared
// by every non-blank line. Glamour's standard styles indent the whole
// document by two spaces; this drops that margin without touching nested
// indents (code blocks, list continuations).
func stripCommonLeadingSpaces(s string) string {
	lines := strings.Split(s, "\n")
	prefix := -1
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		n := 0
		for n < len(line) && line[n] == ' ' {
			n++
		}
		if prefix == -1 || n < prefix {
			prefix = n
		}
	}
	if prefix <= 0 {
		return s
	}
	for i, line := range lines {
		if len(line) >= prefix {
			lines[i] = line[prefix:]
		} else {
			lines[i] = strings.TrimLeft(line, " ")
		}
	}
	return strings.Join(lines, "\n")
}

func sum(xs []int) int {
	n := 0
	for _, x := range xs {
		n += x
	}
	return n
}
