package ui

import (
	"errors"
	"os"
	"os/exec"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// editorFinishedMsg is delivered after the external editor exits.
type editorFinishedMsg struct{ err error }

// errNoEditor is returned when neither $DFC_EDITOR nor $EDITOR is set.
// Surfaced to the TUI footer; the user picks how to set it up.
var errNoEditor = errors.New("set $DFC_EDITOR or $EDITOR (e.g. `vim`, `nano`, or `code --wait`)")

// editorArgv returns the full argv for launching the user's editor on path.
// Resolution order: $DFC_EDITOR → $EDITOR. Errors when neither is set —
// dfc does not silently pick a default.
func editorArgv(path string) ([]string, error) {
	raw := os.Getenv("DFC_EDITOR")
	if raw == "" {
		raw = os.Getenv("EDITOR")
	}
	parts := strings.Fields(raw)
	if len(parts) == 0 {
		return nil, errNoEditor
	}
	return append(parts, path), nil
}

// openInEditor returns a tea.Cmd that suspends the program, runs the editor on
// the given path, and resumes once the user finishes. When no editor is
// configured, returns a Cmd that delivers the missing-editor error to the
// TUI so the footer can display it.
func openInEditor(path string) tea.Cmd {
	argv, err := editorArgv(path)
	if err != nil {
		return func() tea.Msg { return editorFinishedMsg{err: err} }
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return editorFinishedMsg{err: err}
	})
}
