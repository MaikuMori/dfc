package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// editorFinishedMsg is delivered after $EDITOR exits.
type editorFinishedMsg struct{ err error }

// editorArgv returns the full argv for launching the user's editor on path.
// For GUI editors whose CLI returns immediately (VS Code, Cursor, Sublime,
// ...) we inject a wait flag so the TUI doesn't resume before the file is
// actually closed.
func editorArgv(path string) []string {
	raw := os.Getenv("VISUAL")
	if raw == "" {
		raw = os.Getenv("EDITOR")
	}
	if raw == "" {
		raw = "vi"
	}
	parts := strings.Fields(raw)
	if len(parts) == 0 {
		parts = []string{"vi"}
	}
	if flag := waitFlagFor(parts[0]); flag != "" && !hasWaitFlag(parts[1:]) {
		parts = append(parts, flag)
	}
	return append(parts, path)
}

// waitFlagFor returns the flag that makes the given editor block until the
// file is closed, or "" if no flag is needed (terminal editors block by
// default).
func waitFlagFor(bin string) string {
	switch filepath.Base(bin) {
	case "code", "code-insiders", "codium", "vscodium",
		"cursor", "windsurf", "code-server",
		"atom", "atom-beta":
		return "--wait"
	case "subl", "sublime_text":
		return "--wait"
	case "mate":
		return "-w"
	}
	return ""
}

func hasWaitFlag(args []string) bool {
	for _, a := range args {
		if a == "-w" || a == "--wait" {
			return true
		}
	}
	return false
}

// openInEditor returns a tea.Cmd that suspends the program, runs the editor on
// the given path, and resumes once the user finishes.
func openInEditor(path string) tea.Cmd {
	argv := editorArgv(path)
	cmd := exec.Command(argv[0], argv[1:]...)
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return editorFinishedMsg{err: err}
	})
}
