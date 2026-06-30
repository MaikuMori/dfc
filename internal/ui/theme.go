// Single source of truth for colors and lipgloss styles used by the TUI.
// No other file in this package may import lipgloss or hard-code a color.
package ui

import (
	"image/color"
	"os"

	"charm.land/lipgloss/v2"
)

// hasDarkBG is resolved once at package init. lipgloss v2 dropped the
// auto-resolving AdaptiveColor type, so we probe the terminal once and
// pick concrete light/dark colors with LightDark from then on.
//
// Probing has to happen at init (outside the bubbletea program lifecycle)
// because reading the terminal's response after altscreen owns stdin
// deadlocks.
var hasDarkBG = lipgloss.HasDarkBackground(os.Stdin, os.Stdout)

var ld = lipgloss.LightDark(hasDarkBG)

var (
	colorFg       = ld(lipgloss.Color("#1a1a1a"), lipgloss.Color("#e6e6e6"))
	colorMuted    = ld(lipgloss.Color("#888888"), lipgloss.Color("#666666"))
	colorAccent   = ld(lipgloss.Color("#0066cc"), lipgloss.Color("#7aa2f7"))
	colorCursorBg = ld(lipgloss.Color("#e5e5e5"), lipgloss.Color("#2a2a2a"))
	colorError    = ld(lipgloss.Color("#cc3333"), lipgloss.Color("#f7768e"))
	// Inline #tags render as a colored pill: a tinted background with a
	// high-contrast foreground so they pop out of the description text.
	colorTagBg = ld(lipgloss.Color("#dbe6fb"), lipgloss.Color("#3a4a6b"))
	colorTagFg = ld(lipgloss.Color("#1f4486"), lipgloss.Color("#cdd9f5"))
)

var (
	styleOpen = lipgloss.NewStyle().Foreground(colorFg)
	// Done description: italic + muted, no strikethrough. The ✓ glyph carries
	// the "done" signal; the italic + dim gives a calm visual recede without
	// making the words illegible.
	styleDone       = lipgloss.NewStyle().Foreground(colorMuted).Italic(true)
	styleCursor     = lipgloss.NewStyle().Background(colorCursorBg).Foreground(colorFg).Bold(true)
	styleDoneCursor = lipgloss.NewStyle().Background(colorCursorBg).Foreground(colorMuted).Italic(true)
	styleHint       = lipgloss.NewStyle().Foreground(colorMuted)
	// styleTag paints an inline #tag as a tinted pill so it stands out from the
	// surrounding description text.
	styleTag = lipgloss.NewStyle().Background(colorTagBg).Foreground(colorTagFg)
	// styleCheckDone mutes a completed checkbox line in the expanded markdown
	// view so done sub-tasks recede beneath the outstanding ones.
	styleCheckDone = lipgloss.NewStyle().Foreground(colorMuted)
	stylePrompt    = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	styleEmpty     = lipgloss.NewStyle().Foreground(colorMuted).Italic(true)
	styleError     = lipgloss.NewStyle().Foreground(colorError)
	styleHeader    = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	// noStyle is the explicit "no attributes" lipgloss style used to neutralize
	// chrome we inherit from third-party bubbles (textarea cursor-line bg,
	// end-of-buffer foreground, etc.).
	noStyle = lipgloss.NewStyle()
)

const (
	iconOpen       = "○"
	iconDone       = "✓"
	iconHasDetails = "…" // 1-char hint that the row has expandable content
)

// _ asserts that colorFg etc. resolve to the color.Color interface; lipgloss
// uses image/color throughout.
var _ color.Color = colorFg
