package ui

import (
	"errors"

	tea "charm.land/bubbletea/v2"

	"github.com/MaikuMori/dfc/internal/core"
	"github.com/MaikuMori/dfc/internal/watch"
)

// fsChangedMsg is delivered when the watched project directory has had at
// least one event inside the debounce window. The model handles it by
// re-listing tasks from disk.
type fsChangedMsg struct{}

// fsErrorMsg surfaces a watcher error. We deliberately do not reissue
// the listener after an fsErrorMsg — a closed channel would otherwise
// flood the message loop. The watcher only dies at teardown, so no
// recovery is attempted: relaunch dfc to get a live watch again.
type fsErrorMsg struct{ err error }

// ensureFreshCmd runs the index drift sync off the Update goroutine, so a
// debounced filesystem burst doesn't block keypress handling on a disk walk
// plus SQLite writes. The single-connection index pool serializes it against
// any concurrent query.
func ensureFreshCmd(c *core.Core) tea.Cmd {
	return func() tea.Msg {
		c.EnsureFresh()
		return nil
	}
}

// waitForChange returns a tea.Cmd that pulls one Event off the watch
// subscription and forwards it as fsChangedMsg. A closed channel becomes
// an fsErrorMsg so the model can stop re-arming.
func waitForChange(sub <-chan watch.Event) tea.Cmd {
	if sub == nil {
		return nil
	}
	return func() tea.Msg {
		_, ok := <-sub
		if !ok {
			return fsErrorMsg{err: errors.New("watch subscription closed")}
		}
		return fsChangedMsg{}
	}
}
