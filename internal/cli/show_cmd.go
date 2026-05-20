package cli

import (
	"fmt"
	"os"
)

// timestampLayout is the human-readable timestamp format used by every
// CLI command that prints task metadata. ULID frontmatter still carries
// the canonical RFC3339 value; this is purely the rendered form.
const timestampLayout = "2006-01-02 15:04:05 MST"

// ShowCmd prints a single task by full ULID. ULIDs are globally unique so
// the lookup walks every project until it finds a match.
type ShowCmd struct {
	JSON bool   `name:"json" help:"Emit the task as a single JSON object."`
	ID   string `arg:"" help:"Full task ID (26-char ULID)."`
}

func (c *ShowCmd) Run() error {
	cr, err := openCore()
	if err != nil {
		return err
	}
	defer func() { _ = cr.Close() }()

	t, err := cr.Show(c.ID)
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
