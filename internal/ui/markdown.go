package ui

import (
	"regexp"
	"strconv"
	"strings"

	"charm.land/glamour/v2"
	"charm.land/glamour/v2/ansi"
	"charm.land/glamour/v2/styles"

	"github.com/MaikuMori/dfc/internal/tag"
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

// glamourRun matches one of glamour's colored runs: an opening SGR (with
// params), the run's text (no inner escapes), and the closing reset. glamour
// emits each wrapped line as such a run, so pillTags can recolor tag spans
// inside it.
var glamourRun = regexp.MustCompile("\x1b\\[[0-9;]+m[^\x1b]*\x1b\\[0?m")

// pillOpen / pillClose are styleTag's raw enter/exit sequences, captured once
// so pillTags can wrap a tag's bytes inside an existing glamour run.
var pillOpen, pillClose = func() (string, string) {
	on, off, _ := strings.Cut(styleTag.Render("\x00"), "\x00")
	return on, off
}()

// hasBackground reports whether an SGR sequence (e.g. "\x1b[38;5;228;48;5;63m")
// sets a background color. It parses params rather than substring-matching so a
// foreground color value that happens to be 48 isn't mistaken for the extended
// background introducer — the extended-fg params (38;5;n / 38;2;r;g;b) are
// skipped over before the scan looks for a background.
func hasBackground(sgr string) bool {
	s := strings.TrimSuffix(strings.TrimPrefix(sgr, "\x1b["), "m")
	if s == "" {
		return false
	}
	params := strings.Split(s, ";")
	for i := 0; i < len(params); i++ {
		switch params[i] {
		case "48": // extended background introducer (48;5;n or 48;2;r;g;b)
			return true
		case "38": // extended foreground: skip its color value params
			if i+1 < len(params) && params[i+1] == "5" {
				i += 2
			} else if i+1 < len(params) && params[i+1] == "2" {
				i += 4
			}
		default:
			if n, err := strconv.Atoi(params[i]); err == nil &&
				((n >= 40 && n <= 47) || (n >= 100 && n <= 107)) {
				return true
			}
		}
	}
	return false
}

// pillTags repaints inline #tags in glamour's rendered output as pills. It runs
// after word-wrap, so recoloring a finished run is zero-width and can't disturb
// layout. For each colored run that holds tags, the run's text is split and
// each tag span is wrapped in the pill style while the run's own color is
// re-applied around the rest. Runs with a background (headings, code blocks)
// are left alone so their styling isn't broken.
func pillTags(s string) string {
	if !strings.Contains(s, "#") {
		return s
	}
	return glamourRun.ReplaceAllStringFunc(s, func(run string) string {
		openEnd := strings.IndexByte(run, 'm') + 1
		closeStart := strings.LastIndex(run, "\x1b[")
		open, closer, text := run[:openEnd], run[closeStart:], run[openEnd:closeStart]
		if hasBackground(open) {
			return run // background run (heading / code block): leave it
		}
		spans := tag.Spans(text)
		if len(spans) == 0 {
			return run
		}
		var b strings.Builder
		prev := 0
		for _, sp := range spans {
			if sp.Start > prev {
				b.WriteString(open + text[prev:sp.Start] + closer)
			}
			b.WriteString(pillOpen + text[sp.Start:sp.End] + pillClose)
			prev = sp.End
		}
		if prev < len(text) {
			b.WriteString(open + text[prev:] + closer)
		}
		return b.String()
	})
}

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
