package cli

import (
	"fmt"
	"os"
)

// RmCmd removes a task by full ULID. Defaults to the cwd project;
// `--all-projects` searches every project (today's behaviour).
type RmCmd struct {
	JSON        bool   `name:"json" help:"Emit the removed task's id/project/path as a single JSON object."`
	AllProjects bool   `name:"all-projects" short:"a" help:"Search every project for this ID (default: only the cwd project)."`
	ID          string `arg:"" predictor:"task-any" help:"Full task ID (26-char ULID)."`
}

func (c *RmCmd) Run() error {
	cr, err := openCore()
	if err != nil {
		return err
	}
	defer func() { _ = cr.Close() }()

	var (
		slug, path, trashID string
	)
	if c.AllProjects {
		slug, path, trashID, err = cr.Remove(c.ID)
	} else {
		var cwdSlug string
		cwdSlug, err = resolveCwdSlug()
		if err != nil {
			return err
		}
		slug, path, trashID, err = cr.RemoveInProject(c.ID, cwdSlug)
		err = scopedNotFoundHint(err, c.ID, cwdSlug)
	}
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
