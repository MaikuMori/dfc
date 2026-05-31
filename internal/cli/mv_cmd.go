package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/MaikuMori/dfc/internal/storage"
)

// MvCmd moves a task to another project. The source is looked up in the cwd
// project by default (`--all-projects` searches every project); the
// destination is a slug or display name, created if it doesn't exist yet.
type MvCmd struct {
	JSON        bool   `name:"json" help:"Emit the moved task as a single JSON object."`
	AllProjects bool   `name:"all-projects" short:"a" help:"Search every project for this ID (default: only the cwd project)."`
	ID          string `arg:"" predictor:"task-any" help:"Full task ID (26-char ULID)."`
	Project     string `arg:"" predictor:"project" help:"Destination project (slug or name; created if new)."`
}

func (c *MvCmd) Run() error {
	dest, destName, _, err := resolveProjectInput(c.Project)
	if err != nil {
		return err
	}
	if dest == "" {
		return errors.New("missing destination project")
	}

	cr, err := openCore()
	if err != nil {
		return err
	}
	defer func() { _ = cr.Close() }()

	var t storage.Task
	if c.AllProjects {
		t, err = cr.Show(c.ID)
	} else {
		var slug string
		slug, err = resolveCwdSlug()
		if err != nil {
			return err
		}
		t, err = cr.ShowInProject(c.ID, slug)
		err = scopedNotFoundHint(err, c.ID, slug)
	}
	if err != nil {
		return err
	}

	if dest == t.ProjectSlug {
		if c.JSON {
			return writeJSON(os.Stdout, taskOut(t))
		}
		fmt.Printf("already in %s\n", dest)
		return nil
	}

	// Register the destination with the name the user typed before the move,
	// so a brand-new project keeps a friendly display name rather than the
	// slug (MoveTask's own auto-register would default the name to the slug).
	if err := cr.EnsureProject(dest, destName); err != nil {
		return err
	}
	moved, err := cr.MoveTask(t, dest)
	if err != nil {
		return err
	}
	return emitMove(moved, t.ProjectSlug, c.JSON)
}

func emitMove(t storage.Task, fromSlug string, jsonOut bool) error {
	if jsonOut {
		return writeJSON(os.Stdout, taskOut(t))
	}
	fmt.Printf("moved %s  %s → %s\n", t.ID, fromSlug, t.ProjectSlug)
	return nil
}
