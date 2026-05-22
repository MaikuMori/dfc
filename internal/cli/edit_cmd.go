package cli

import (
	"errors"
	"io"
	"os"
	"strings"

	"github.com/MaikuMori/dfc/internal/core"
	"github.com/MaikuMori/dfc/internal/storage"
)


// EditCmd rewrites a task's description and/or details.
//
//   - --description "new" replaces the description (and renames the file
//     to match the new slug).
//   - --details "body" replaces the details body. Use "-" to read it from
//     stdin (handy for multi-line content from an agent or pipe).
//
// At least one of the two must be supplied. Both can be set in a single
// invocation; the rename pass also saves the new details.
type EditCmd struct {
	Description *string `name:"description" help:"New description (heading text). Omit to leave unchanged."`
	Details     *string `name:"details" short:"d" help:"New details body. Use '-' to read from stdin. Omit to leave unchanged."`
	JSON        bool    `name:"json" help:"Emit the updated task as a single JSON object."`
	AllProjects bool    `name:"all-projects" short:"a" help:"Search every project for this ID (default: only the cwd project)."`
	ID          string  `arg:"" predictor:"task-any" help:"Full task ID (26-char ULID)."`
}

func (c *EditCmd) Run() error {
	if c.Description == nil && c.Details == nil {
		return errors.New("pass --description and/or --details")
	}

	// Resolve --details "-" before we touch the store.
	in := core.EditInput{ID: c.ID, Description: c.Description}
	if c.Details != nil {
		body := *c.Details
		if body == "-" {
			raw, err := io.ReadAll(os.Stdin)
			if err != nil {
				return err
			}
			body = strings.TrimRight(string(raw), "\n")
		}
		in.Details = &body
	}

	cr, err := openCore()
	if err != nil {
		return err
	}
	defer func() { _ = cr.Close() }()

	var t storage.Task
	if c.AllProjects {
		t, err = cr.Edit(in)
	} else {
		slug, slugErr := resolveCwdSlug()
		if slugErr != nil {
			return slugErr
		}
		t, err = cr.EditInProject(in, slug)
		err = scopedNotFoundHint(err, c.ID, slug)
	}
	if err != nil {
		return err
	}
	return emitTask(t, c.JSON, "edited")
}
