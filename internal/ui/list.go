package ui

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mattn/go-runewidth"
	"github.com/muesli/reflow/wordwrap"

	"github.com/MaikuMori/dfc/internal/storage"
	"github.com/MaikuMori/dfc/internal/tag"
)

// maxRowTags caps how many #tag pills a collapsed row shows; any beyond it
// collapse into a "+N" indicator so a heavily-tagged task can't crowd out its
// title.
const maxRowTags = 3

// stripTags removes inline #tag spans from a plain string and collapses the
// whitespace they leave behind, returning the bare title text. A collapsed row
// shows the title with its tags lifted out and grouped after it, so they read
// as a set instead of scattered through the words.
func stripTags(s string) string {
	spans := tag.Spans(s)
	if len(spans) == 0 {
		return s
	}
	var b strings.Builder
	prev := 0
	for _, sp := range spans {
		b.WriteString(s[prev:sp.Start])
		prev = sp.End
	}
	b.WriteString(s[prev:])
	return strings.Join(strings.Fields(b.String()), " ")
}

// rowTags renders a row's collected #tags as a trailing group: up to maxRowTags
// pills followed by a "+N" overflow indicator. It returns the styled form (tag
// pills for foreground-only open rows), the raw form (embedded inside cursor /
// done rows whose outer style would otherwise mangle a pill's reset codes — the
// same reason the project prefix is embedded raw), and the display width of the
// raw form for wrap accounting. All three are empty / zero when tags is empty.
func rowTags(tags []string) (styled, raw string, width int) {
	if len(tags) == 0 {
		return "", "", 0
	}
	shown, overflow := tags, 0
	if len(tags) > maxRowTags {
		shown, overflow = tags[:maxRowTags], len(tags)-maxRowTags
	}
	rawParts := make([]string, 0, len(shown)+1)
	styledParts := make([]string, 0, len(shown)+1)
	for _, t := range shown {
		pill := "#" + t
		rawParts = append(rawParts, pill)
		styledParts = append(styledParts, styleTag.Render(pill))
	}
	if overflow > 0 {
		more := fmt.Sprintf("+%d", overflow)
		rawParts = append(rawParts, more)
		styledParts = append(styledParts, styleHint.Render(more))
	}
	raw = strings.Join(rawParts, " ")
	return strings.Join(styledParts, " "), raw, runewidth.StringWidth(raw)
}

// progressBadge returns a "[done/total]" checkbox-progress badge for the task,
// or "" when it carries no task-list items.
func progressBadge(t storage.Task) string {
	done, total := storage.Checkboxes(t.Details)
	if total == 0 {
		return ""
	}
	return fmt.Sprintf("[%d/%d]", done, total)
}

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

// rowKey captures every input that determines a single rendered row, so a
// row can be reused from the cache whenever none of them changed. Content
// edits are covered by modified — every Save bumps the file's mtime.
type rowKey struct {
	id       string
	slug     string
	modified int64
	status   storage.Status
	prefix   string
	cursor   bool
	expanded bool
	width    int
}

// rowCache holds the previous frame's per-row keys and rendered strings.
// Navigation only changes the cursor flag on two rows, so reusing the rest
// avoids re-wrapping and re-styling the whole list on every keypress.
var rowCache struct {
	sync.Mutex
	keys []rowKey
	rows []string
}

func buildRows(m Model, width int) (rows []string, heights []int) {
	if len(m.tasks) == 0 {
		return []string{styleEmpty.Render("No tasks yet.")}, []int{1}
	}
	rows = make([]string, len(m.tasks))
	heights = make([]int, len(m.tasks))

	rowCache.Lock()
	defer rowCache.Unlock()
	keys := make([]rowKey, len(m.tasks))
	for i, t := range m.tasks {
		prefix := projectPrefix(m, t)
		expanded := m.expandedID != "" && t.ID == m.expandedID &&
			(!m.globalView || t.ProjectSlug == m.expandedSlug)
		k := rowKey{
			id: t.ID, slug: t.ProjectSlug, modified: t.Modified.UnixNano(),
			status: t.Status, prefix: prefix, cursor: i == m.cursor,
			expanded: expanded, width: width,
		}
		keys[i] = k

		var row string
		switch {
		case i < len(rowCache.keys) && rowCache.keys[i] == k:
			row = rowCache.rows[i]
		case expanded:
			row = renderTaskMarkdown(t, prefix, width, rowInlineTags(m, t))
		default:
			row = renderRow(t, prefix, i == m.cursor, width, rowInlineTags(m, t))
		}
		rows[i] = row
		heights[i] = strings.Count(row, "\n") + 1
	}
	rowCache.keys = keys
	rowCache.rows = rows
	return rows, heights
}

// rowInlineTags returns a task's inline #tags for the collapsed row's tag
// group, going through core's (ID, mtime) memo so a full rebuild doesn't
// re-parse every task's markdown. Project tags are deliberately excluded: they
// apply uniformly to every task in the project, so showing them on each row
// would be noise.
func rowInlineTags(m Model, t storage.Task) []string {
	if m.core != nil {
		return m.core.InlineTags(t)
	}
	return t.Tags()
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
	if runewidth.StringWidth(name) > prefixMaxLen-2 {
		name = runewidth.Truncate(name, prefixMaxLen-2, "…")
	}
	return "[" + name + "] "
}

// renderRow renders one collapsed task row. The task's #tags are lifted out of
// the title and grouped after it (capped, with a "+N" overflow), so a glance
// reads the title cleanly and its tags as a set. Styling is identical in every
// view (a filtered list renders done tasks the same muted way as the full list).
func renderRow(t storage.Task, prefix string, cursor bool, width int, tags []string) string {
	icon := iconOpen
	if t.Status == storage.StatusDone {
		icon = iconDone
	}

	const iconWidth = 2 // "○ " or "✓ "
	prefixWidth := runewidth.StringWidth(prefix)
	indent := strings.Repeat(" ", iconWidth+prefixWidth)

	hasDetails := strings.TrimSpace(t.Details) != ""
	done := t.Status == storage.StatusDone

	groupStyled, groupRaw, groupWidth := rowTags(tags)

	// The trailing hint after the description: a checkbox-progress badge when
	// the task has sub-tasks, otherwise the "…" dot when it has any details.
	// The badge supersedes the dot — a task with sub-tasks always has details,
	// and the count is the more useful at-a-glance signal.
	trailer := progressBadge(t)
	if trailer == "" && hasDetails {
		trailer = iconHasDetails
	}

	// Reserve room for the tag group and trailer (each as " " + text) so the
	// title wraps before them, never past them. Otherwise a full-width title
	// plus the appended group/trailer overflows into a terminal-wrapped extra
	// row that the height accounting below doesn't count, drifting the viewport.
	reserve := 0
	if groupWidth > 0 {
		reserve += 1 + groupWidth
	}
	if trailer != "" {
		reserve += 1 + runewidth.StringWidth(trailer)
	}
	wrapWidth := width - iconWidth - prefixWidth - reserve
	if wrapWidth < 1 {
		wrapWidth = 1
	}

	wrapped := wordwrap.String(stripTags(t.Description), wrapWidth)
	lines := strings.Split(wrapped, "\n")

	// Open non-cursor rows render foreground-only, so the project prefix and
	// #tag pills compose cleanly. Done and cursor rows take the outer-style
	// path below.
	if !cursor && !done {
		first := styleOpen.Render(icon + " ")
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
		if groupStyled != "" {
			rendered += " " + groupStyled
		}
		if trailer != "" {
			rendered += " " + styleHint.Render(trailer)
		}
		return rendered
	}

	// Everything else: raw prefix and raw tag group in the text, single outer
	// style. Pills can't nest inside an outer style that changes background or
	// attributes, so the group rides along as plain text the row style paints.
	for i, line := range lines {
		if i == 0 {
			lines[i] = icon + " " + prefix + line
		} else {
			lines[i] = indent + line
		}
	}
	if groupRaw != "" {
		lines[len(lines)-1] += " " + groupRaw
	}
	if trailer != "" && cursor {
		lines[len(lines)-1] += " " + trailer
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

	if trailer != "" && !cursor {
		rendered += " " + styleHint.Render(trailer)
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
func renderTaskMarkdown(t storage.Task, prefix string, width int, tags []string) string {
	const iconWidth = 2 // "○ " or "✓ "
	prefixWidth := runewidth.StringWidth(prefix)

	// The badge rides on the first rendered line; reserve its width so glamour
	// wraps the heading before it and the line never overflows the terminal.
	badge := progressBadge(t)
	mdWidth := width - iconWidth - prefixWidth
	if badge != "" {
		mdWidth -= 1 + runewidth.StringWidth(badge)
	}
	// Below the markdown renderer's minimum useful width the expanded
	// view would overflow the terminal; fall back to the collapsed row
	// so a tiny screen stays legible.
	if mdWidth < 20 {
		return renderRow(t, prefix, false, width, tags)
	}
	rendered := cachedMarkdown(t, mdWidth)
	if rendered == "" {
		return renderRow(t, prefix, false, width, tags)
	}

	icon := iconOpen
	if t.Status == storage.StatusDone {
		icon = iconDone
	}
	indent := strings.Repeat(" ", iconWidth+prefixWidth)
	lines := strings.Split(rendered, "\n")
	firstSet := false
	for i, line := range lines {
		if !firstSet && strings.TrimSpace(line) != "" {
			head := icon + " "
			if prefix != "" {
				head += styleHint.Render(prefix)
			}
			line = head + line
			if badge != "" {
				line += " " + styleHint.Render(badge)
			}
			lines[i] = line
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
		out = dimDoneTasks(out, storage.CheckedItems(t.Details))
		out = pillTags(out)
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
