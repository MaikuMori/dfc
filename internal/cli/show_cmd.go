package cli

import (
	"fmt"
	"os"

	"github.com/MaikuMori/dfc/internal/storage"
)

// timestampLayout is the human-readable timestamp format used by every
// CLI command that prints task metadata. ULID frontmatter still carries
// the canonical RFC3339 value; this is purely the rendered form.
const timestampLayout = "2006-01-02 15:04:05 MST"

// ShowCmd prints a single task by full ULID. Defaults to the cwd
// project; `--all-projects` searches every project (today's behavior).
type ShowCmd struct {
	JSON        bool   `name:"json" help:"Emit the task as a single JSON object."`
	AllProjects bool   `name:"all-projects" short:"a" help:"Search every project for this ID (default: only the cwd project)."`
	ID          string `arg:"" predictor:"task-any" help:"Full task ID (26-char ULID)."`
}

func (c *ShowCmd) Run() error {
	cr, err := openCore()
	if err != nil {
		return err
	}
	defer func() { _ = cr.Close() }()

	var t storage.Task
	if c.AllProjects {
		t, err = cr.Show(c.ID)
	} else {
		slug, slugErr := resolveCwdSlug()
		if slugErr != nil {
			return slugErr
		}
		t, err = cr.ShowInProject(c.ID, slug)
		err = scopedNotFoundHint(err, c.ID, slug)
	}
	if err != nil {
		return err
	}

	if c.JSON {
		return writeJSON(os.Stdout, taskOut(t))
	}
	fmt.Printf("id:          %s\n", t.ID)
	fmt.Printf("project:     %s\n", t.ProjectSlug)
	fmt.Printf("status:      %s\n", t.Status)
	fmt.Printf("created:     %s\n", t.Created.Format(timestampLayout))
	fmt.Printf("modified:    %s\n", t.Modified.Format(timestampLayout))
	fmt.Printf("path:        %s\n", t.Path)
	fmt.Println()
	fmt.Println(t.Description)
	if t.Details != "" {
		fmt.Println()
		fmt.Println(t.Details)
	}
	return nil
}
