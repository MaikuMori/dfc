package ui

import (
	"errors"

	"github.com/MaikuMori/dfc/internal/watch"
	tea "charm.land/bubbletea/v2"
)

// fsChangedMsg is delivered when the watched project directory has had at
// least one event inside the debounce window. The model handles it by
// re-listing tasks from disk.
type fsChangedMsg struct{}

// fsErrorMsg surfaces a watcher error. We deliberately do not reissue
// the listener after an fsErrorMsg — a closed channel would otherwise
// flood the message loop. Recovery happens via project switch or relaunch.
type fsErrorMsg struct{ err error }

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
