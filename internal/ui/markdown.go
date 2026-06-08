package ui

import (
	"regexp"
	"strings"

	"charm.land/glamour/v2"
	"charm.land/glamour/v2/ansi"
	"charm.land/glamour/v2/styles"
)

// markdownStyles is built once at package init. We start from glamour's
// dark/light standard styles, then strip the BlockPrefix/BlockSuffix from
// every heading level so the highlighted background hugs the title text
// instead of padding it with a leading/trailing space. That keeps the title
// aligned with the icon column in collapsed rows so tab-toggle doesn't jump.
//
// We pre-resolve to a static style at init rather than via
// glamour.WithAutoStyle() because the auto-style path probes the terminal
// over stdin and deadlocks against bubbletea's altscreen capture.
var markdownStyles = func() ansi.StyleConfig {
	base := styles.DarkStyleConfig
	if !hasDarkBG {
		base = styles.LightStyleConfig
	}
	// Drop the document-level left margin so rendered content sits flush
	// against column 0 and lines up with our icon prefix.
	base.Document.Margin = nil
	base.Document.BlockPrefix = ""
	base.Document.BlockSuffix = ""

	heads := []*ansi.StyleBlock{
		&base.H1, &base.H2, &base.H3, &base.H4, &base.H5, &base.H6,
	}
	for _, h := range heads {
		// Strip the " Title " padding inside the highlighted background and
		// drop heading-level left margin so the title aligns with non-heading
		// content at column 0.
		h.BlockPrefix = ""
		h.BlockSuffix = ""
		h.Prefix = ""
		h.Suffix = ""
		h.Margin = nil
		h.Indent = nil
	}
	// Render checkbox list items with the app's own status icons so sub-tasks
	// read like the top-level list (○ outstanding, ✓ done) instead of glamour's
	// bracket form.
	base.Task.Ticked = iconDone + " "
	base.Task.Unticked = iconOpen + " "
	return base
}()

// ansiSGR matches the SGR escape sequences glamour emits for color and text
// attributes — enough to recover the plain text of a rendered line.
var ansiSGR = regexp.MustCompile("\x1b\\[[0-9;]*m")

// dimDoneTasks mutes the lines of each completed checkbox item so finished
// sub-tasks recede next to the outstanding ones. `checked` is the set of
// checked item texts (from storage.CheckedItems); a rendered line that starts
// with the done glyph is dimmed only when its text matches a known checked
// item, so a heading or paragraph that merely begins with "✓ " is left alone.
// glamour wraps continuation lines back to column 0, so the item's known full
// text — not indentation — decides how many wrapped lines to dim.
//
// Glamour gives task-item text no color of its own, so stripping a line's ANSI
// and repainting it in one muted foreground is safe — there are no inner
// attributes to nest under.
func dimDoneTasks(s string, checked []string) string {
	if len(checked) == 0 {
		return s
	}
	dim := func(lead, body string) string {
		return lead + styleCheckDone.Render(strings.TrimRight(body, " "))
	}
	lines := strings.Split(s, "\n")
	for i := 0; i < len(lines); {
		plain := ansiSGR.ReplaceAllString(lines[i], "")
		trimmed := strings.TrimLeft(plain, " ")
		if !strings.HasPrefix(trimmed, iconDone+" ") {
			i++
			continue
		}
		first := normalizeSpaces(strings.TrimPrefix(trimmed, iconDone+" "))
		full, ok := matchChecked(first, checked)
		if !ok {
			i++ // a check glyph in a heading or prose, not a completed item
			continue
		}
		lines[i] = dim(plain[:len(plain)-len(trimmed)], trimmed)
		// Consume wrapped continuation lines, growing the accumulated text until
		// it reaches the item's full text. Stop at a blank line, the next list
		// glyph, or text that no longer extends this item.
		acc, j := first, i+1
		for j < len(lines) && acc != full {
			cplain := ansiSGR.ReplaceAllString(lines[j], "")
			ctrim := strings.TrimLeft(cplain, " ")
			if ctrim == "" || strings.HasPrefix(ctrim, iconDone+" ") || strings.HasPrefix(ctrim, iconOpen+" ") {
				break
			}
			cand := normalizeSpaces(acc + " " + ctrim)
			if cand != full && !strings.HasPrefix(full, cand+" ") {
				break
			}
			lines[j] = dim(cplain[:len(cplain)-len(ctrim)], ctrim)
			acc, j = cand, j+1
		}
		i = j
	}
	return strings.Join(lines, "\n")
}

// matchChecked reports whether a rendered task line's text belongs to a checked
// item. linePrefix is the item's first (possibly wrap-truncated) line; it
// matches a checked item that equals it or begins with it. An exact match wins;
// otherwise the longest prefix candidate is returned so continuation-line
// consumption has the full target text.
func matchChecked(linePrefix string, checked []string) (string, bool) {
	best := ""
	for _, c := range checked {
		if c == linePrefix {
			return c, true
		}
		if strings.HasPrefix(c, linePrefix+" ") && len(c) > len(best) {
			best = c
		}
	}
	return best, best != ""
}

func normalizeSpaces(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// renderMarkdown turns the given markdown source into a styled, word-wrapped
// terminal string suitable for inline embedding. It returns the source
// unchanged on render failure rather than swallowing the content.
func renderMarkdown(src string, width int) string {
	if strings.TrimSpace(src) == "" {
		return ""
	}
	if width < 20 {
		width = 20
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithStyles(markdownStyles),
		glamour.WithWordWrap(width),
		glamour.WithEmoji(),
	)
	if err != nil {
		return src
	}
	out, err := r.Render(src)
	if err != nil {
		return src
	}
	return trimBlankEdges(out)
}

// trimBlankEdges drops leading and trailing lines that contain only
// whitespace. Plain strings.Trim(out, "\n") doesn't help when glamour emits
// a line of spaces (its block-level vertical margin).
func trimBlankEdges(s string) string {
	lines := strings.Split(s, "\n")
	start := 0
	for start < len(lines) && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	end := len(lines)
	for end > start && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	return strings.Join(lines[start:end], "\n")
}
