package cli

import (
	"fmt"
	"os"

	"github.com/MaikuMori/dfc/internal/storage"
)

// DoneCmd flips a task's status to done.
type DoneCmd struct {
	JSON bool   `name:"json" help:"Emit the updated task as a single JSON object."`
	ID   string `arg:"" help:"Full task ID (26-char ULID)."`
}

func (c *DoneCmd) Run() error { return setStatus("done", c.ID, storage.StatusDone, c.JSON) }

// ReopenCmd flips a task's status back to open.
type ReopenCmd struct {
	JSON bool   `name:"json" help:"Emit the updated task as a single JSON object."`
	ID   string `arg:"" help:"Full task ID (26-char ULID)."`
}

func (c *ReopenCmd) Run() error {
	return setStatus("reopen", c.ID, storage.StatusOpen, c.JSON)
}

// setStatus is the shared mutation path for done/reopen. humanLabel is
// the verb shown to humans on success ("done", "reopen").
func setStatus(humanLabel, id string, want storage.Status, jsonOut bool) error {
	cr, err := openCore()
	if err != nil {
		return err
	}
	defer func() { _ = cr.Close() }()

	t, changed, err := cr.SetStatus(id, want)
	if err != nil {
		return err
	}
	label := humanLabel
	if !changed {
		label = humanLabel + " (no change)"
	}
	return emitTask(t, jsonOut, label)
}

// emitTask prints a JSON object (when requested) or a human one-liner.
// `humanLabel` is the leading verb shown to humans ("done", "reopen", ...).
func emitTask(t storage.Task, jsonOut bool, humanLabel string) error {
	if jsonOut {
		return writeJSON(os.Stdout, taskOut(t))
	}
	fmt.Printf("%s  %s  %s\n", humanLabel, t.ID, t.Description)
	return nil
}
