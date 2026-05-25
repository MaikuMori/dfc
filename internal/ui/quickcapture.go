package ui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"github.com/mattn/go-runewidth"
)

// quickCaptureModel is a one-shot inline prompt that returns the typed text on
// enter and aborts on esc / ctrl+c. Enter alone submits; alt+enter / ctrl+j
// inserts a newline so the buffer can carry both a heading and a details body.
type quickCaptureModel struct {
	input  textarea.Model
	label  string // optional one-line header rendered above the prompt
	result string
	ok     bool
}

func (m quickCaptureModel) Init() tea.Cmd { return textarea.Blink }

func (m quickCaptureModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if sz, ok := msg.(tea.WindowSizeMsg); ok {
		w := sz.Width - 2 // textarea prompt ("> "/"  ") + cursor breathing room
		if w < 1 {
			w = 1
		}
		m.input.SetWidth(w)
		resizeQuickCapture(&m.input)
		return m, nil
	}
	if k, ok := msg.(tea.KeyPressMsg); ok {
		// Plain (unmodified) Enter submits. Any modifier (alt/shift/ctrl)
		// falls through so the textarea's rebound InsertNewline binding
		// can insert a newline instead.
		if isEnter(k) && k.Mod == 0 {
			m.result = strings.TrimRight(m.input.Value(), " \t\n")
			m.ok = strings.TrimSpace(m.result) != ""
			return m, tea.Quit
		}
		switch k.String() {
		case "esc", "ctrl+c":
			return m, tea.Quit
		}
	}
	growCaptureForKey(&m.input)
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	resizeQuickCapture(&m.input)
	return m, cmd
}

func (m quickCaptureModel) View() tea.View {
	var b strings.Builder
	if m.label != "" {
		b.WriteString(styleHint.Render(m.label))
		b.WriteByte('\n')
	}
	b.WriteString(m.input.View())
	return tea.NewView(b.String())
}

// QuickCapture runs an inline prompt and returns the captured text. The
// returned text may contain newlines when the user inserted any via
// alt+enter / ctrl+j. Callers split the first non-blank line off as the
// task description if they want heading + body semantics.
//
// When the user cancels (esc / ctrl+c) or submits empty whitespace, ok is
// false. label is rendered above the prompt; pass "" for the bare `> _` form.
func QuickCapture(label string) (text string, ok bool, err error) {
	ta := newCaptureArea("task description")

	m := quickCaptureModel{input: ta, label: label}
	finalModel, runErr := tea.NewProgram(m).Run()
	if runErr != nil {
		return "", false, runErr
	}
	final := finalModel.(quickCaptureModel)
	return final.result, final.ok, nil
}

// newCaptureArea returns a textarea configured for an inline single-prompt
// capture: stripped of borders, line numbers, and cursor-line backgrounds;
// styled to match the rest of the TUI; with Enter freed up for "submit"
// (the textarea's InsertNewline binding is moved to alt+enter / ctrl+j).
//
// Shared between the standalone `dfc c` prompt and the TUI's inline
// capture overlay so the two feel identical.
func newCaptureArea(placeholder string) textarea.Model {
	ta := textarea.New()
	ta.Placeholder = placeholder
	ta.Prompt = "> "
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.SetHeight(1)
	// SetPromptFunc lets us draw "> " only on the first line and indent
	// continuation lines two spaces so wrapped / multi-line input visually
	// hangs under the heading instead of starting at column 0.
	ta.SetPromptFunc(2, func(info textarea.PromptInfo) string {
		if info.LineNumber == 0 {
			return "> "
		}
		return "  "
	})
	// Default InsertNewline is bound to "enter" — which would race with our
	// outer Update reading plain Enter as "submit". Rebind to every form of
	// modified Enter we recognize so plain (unmodified) Enter alone submits.
	// shift+enter only fires on terminals that speak the Kitty keyboard
	// protocol (Ghostty, Kitty, Alacritty, WezTerm, ...); ctrl+j and
	// alt+enter are the universal fallbacks.
	ta.KeyMap.InsertNewline = key.NewBinding(
		key.WithKeys("alt+enter", "shift+enter", "ctrl+j"),
		key.WithHelp("shift+enter", "newline"),
	)
	stripTextareaChrome(&ta)
	ta.Focus()
	return ta
}

// stripTextareaChrome removes the cursor-line highlight, the trailing
// end-of-buffer column, and the prompt's default foreground color so the
// textarea fits the TUI's flat, color-light aesthetic.
func stripTextareaChrome(ta *textarea.Model) {
	styles := textarea.DefaultStyles(hasDarkBG)
	for _, s := range []*textarea.StyleState{&styles.Focused, &styles.Blurred} {
		s.CursorLine = noStyle
		s.CursorLineNumber = noStyle
		s.EndOfBuffer = noStyle
		s.Prompt = stylePrompt
		s.Placeholder = styleHint
	}
	ta.SetStyles(styles)
	ta.EndOfBufferCharacter = ' '
}

// resizeQuickCapture grows the textarea's visible height to match its
// content (capped so a runaway paste can't eat the screen). Without this
// the textarea would scroll inside its 1-row viewport instead of pushing
// new lines down.
func resizeQuickCapture(ta *textarea.Model) {
	ta.SetHeight(captureAreaHeight(ta))
}

// captureAreaHeight returns the row count the capture overlay should
// render at: at least one row, capped at a reasonable maximum so a long
// paste doesn't push the task list off-screen. Visual rows account for
// soft-wrapped lines so a long single sentence stays visible instead of
// disappearing past the right edge.
func captureAreaHeight(ta *textarea.Model) int {
	h := captureVisualRows(ta)
	if h < 1 {
		h = 1
	}
	if h > captureMaxRows {
		h = captureMaxRows
	}
	return h
}

// captureVisualRows counts how many visible rows the textarea content
// occupies after soft-wrapping at the current width. The textarea's own
// LineCount() returns logical (\n-separated) lines and ignores wrap, which
// would let a long single line slip past the visible viewport.
func captureVisualRows(ta *textarea.Model) int {
	wrap := ta.Width() - 2 // "> " / "  " prompt width
	if wrap < 1 {
		wrap = 1
	}
	total := 0
	for _, line := range strings.Split(ta.Value(), "\n") {
		w := runewidth.StringWidth(line)
		rows := (w + wrap - 1) / wrap
		if rows < 1 {
			rows = 1
		}
		total += rows
	}
	if total < 1 {
		total = 1
	}
	return total
}

const captureMaxRows = 12

// growCaptureForKey temporarily expands the textarea's visible height to
// the cap before passing a key in. The textarea's Update() reflows its
// internal viewport to keep the cursor visible; if the visible height is
// 1 and the user inserts a newline, the cursor advances to row 1 and the
// viewport scrolls down by 1 — pushing the first line off-screen. By
// pre-expanding to the cap we guarantee `repositionView` won't need to
// scroll, then `resizeQuickCapture` immediately after Update brings the
// height back down to fit the actual content.
func growCaptureForKey(ta *textarea.Model) {
	ta.SetHeight(captureMaxRows)
}
