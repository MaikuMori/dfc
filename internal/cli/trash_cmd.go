package cli

import (
	"fmt"
	"os"
	"text/tabwriter"
	"time"
)

// UndoCmd restores the most recently trashed item (task or project).
type UndoCmd struct {
	JSON bool `name:"json" help:"Emit the restored entry as a single JSON object."`
}

func (c *UndoCmd) Run() error {
	cr, err := openCore()
	if err != nil {
		return err
	}
	defer func() { _ = cr.Close() }()
	m, err := cr.Undo()
	if err != nil {
		return err
	}
	if c.JSON {
		return writeJSON(os.Stdout, m)
	}
	switch m.Kind {
	case "project":
		fmt.Printf("restored project %s\n", m.Slug)
	default:
		desc := m.Description
		if desc == "" {
			desc = m.TaskID
		}
		fmt.Printf("restored task in %s: %s\n", m.Slug, desc)
	}
	return nil
}

// TrashCmd groups the trash-inspection subcommands.
type TrashCmd struct {
	List    TrashListCmd    `cmd:"" default:"withargs" help:"List trash entries (newest first)."`
	Restore TrashRestoreCmd `cmd:"" help:"Restore a specific trash entry by id or unique prefix."`
	Empty   TrashEmptyCmd   `cmd:"" help:"Purge every trash entry."`
}

// TrashListCmd shows what's currently recoverable.
type TrashListCmd struct {
	JSON bool `name:"json" help:"Emit one JSON object per entry (NDJSON)."`
}

func (c *TrashListCmd) Run() error {
	cr, err := openCore()
	if err != nil {
		return err
	}
	defer func() { _ = cr.Close() }()
	entries, err := cr.TrashList()
	if err != nil {
		return err
	}
	if c.JSON {
		for _, e := range entries {
			if err := writeJSON(os.Stdout, e.Manifest); err != nil {
				return err
			}
		}
		return nil
	}
	if len(entries) == 0 {
		fmt.Println("trash is empty")
		return nil
	}
	now := time.Now().UTC()
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for _, e := range entries {
		age := now.Sub(e.DeletedAt).Round(time.Minute)
		label := e.Slug
		switch e.Kind {
		case "task":
			if e.Description != "" {
				label = fmt.Sprintf("%s: %s", e.Slug, e.Description)
			}
		case "project":
			if e.Name != "" && e.Name != e.Slug {
				label = fmt.Sprintf("%s (%s)", e.Slug, e.Name)
			}
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s ago\t%s\n", shortID(e.ID), e.Kind, humanDuration(age), label)
	}
	return w.Flush()
}

// TrashRestoreCmd brings back a specific trash entry.
type TrashRestoreCmd struct {
	ID   string `arg:"" help:"Trash entry id or unique prefix."`
	JSON bool   `name:"json" help:"Emit the restored entry as a single JSON object."`
}

func (c *TrashRestoreCmd) Run() error {
	cr, err := openCore()
	if err != nil {
		return err
	}
	defer func() { _ = cr.Close() }()
	m, err := cr.Restore(c.ID)
	if err != nil {
		return err
	}
	if c.JSON {
		return writeJSON(os.Stdout, m)
	}
	fmt.Printf("restored %s %s\n", m.Kind, m.Slug)
	return nil
}

// TrashEmptyCmd purges every entry without confirmation.
type TrashEmptyCmd struct{}

func (c *TrashEmptyCmd) Run() error {
	cr, err := openCore()
	if err != nil {
		return err
	}
	defer func() { _ = cr.Close() }()
	n, err := cr.TrashEmpty()
	if err != nil {
		return err
	}
	fmt.Printf("purged %d entr%s\n", n, pluralEntries(n))
	return nil
}

func pluralEntries(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}

// humanDuration condenses a duration into a short label suitable for the
// trash listing column — "3h", "2d", "11m". Stops at days because trash
// retention is two weeks at most.
func humanDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "<1m"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
