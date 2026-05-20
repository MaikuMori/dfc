package ui

import (
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
	return base
}()

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
