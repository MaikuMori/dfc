package cli

import (
	"fmt"
	"os"
)

// RmCmd removes a task by full ULID. ULIDs are globally unique so the
// lookup walks every project until it finds a match.
type RmCmd struct {
	JSON bool   `name:"json" help:"Emit the removed task's id/project/path as a single JSON object."`
	ID   string `arg:"" help:"Full task ID (26-char ULID)."`
}

func (c *RmCmd) Run() error {
	cr, err := openCore()
	if err != nil {
		return err
	}
	defer func() { _ = cr.Close() }()

	slug, path, trashID, err := cr.Remove(c.ID)
	if err != nil {
		return err
	}
	return emitRm(c.ID, slug, path, trashID, c.JSON)
}

func emitRm(id, slug, path, trashID string, jsonOut bool) error {
	if jsonOut {
		return writeJSON(os.Stdout, RmOut{ID: id, Project: slug, Path: path, TrashID: trashID})
	}
	fmt.Printf("moved %s to trash (%s) · `dfc undo` to restore\n", id, shortID(trashID))
	return nil
}
