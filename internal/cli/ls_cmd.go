package cli

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/MaikuMori/dfc/internal/project"
	"github.com/MaikuMori/dfc/internal/storage"
)

// LsCmd lists tasks. Defaults to the cwd project; -p selects a specific
// project; -a/--all merges every registered project. --status filters by
// open/done. --json switches the human one-line format to NDJSON.
type LsCmd struct {
	Project string `name:"project" short:"p" predictor:"project" help:"List tasks for this project slug (defaults to cwd)."`
	All     bool   `name:"all" short:"a" help:"List tasks from every registered project."`
	Status  string `name:"status" enum:"open,done,all" default:"all" help:"Filter by status."`
	Sort    string `name:"sort" enum:"modified,created" default:"modified" help:"Order by modification time or creation time."`
	JSON    bool   `name:"json" help:"Emit one JSON object per line (NDJSON)."`
}

func (c *LsCmd) Run() error {
	if c.All && c.Project != "" {
		return errors.New("--all and --project are mutually exclusive")
	}

	cr, err := openCore()
	if err != nil {
		return err
	}
	defer func() { _ = cr.Close() }()
	reg := cr.Registry()

	var tasks []storage.Task
	switch {
	case c.All:
		tasks, err = cr.ListAll()
		if err != nil {
			return err
		}
	default:
		slug := c.Project
		switch {
		case slug == "":
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			resolved, err := project.Resolve(cwd)
			if err != nil {
				return err
			}
			slug = resolved.Slug
		default:
			resolved, _, found, err := resolveProjectInput(slug)
			if err != nil {
				return err
			}
			if !found {
				return fmt.Errorf("unknown project %q", slug)
			}
			slug = resolved
		}
		tasks, err = cr.List(slug)
		if err != nil {
			return err
		}
	}

	// Filter by status.
	if c.Status != "all" {
		filtered := tasks[:0]
		for _, t := range tasks {
			if string(t.Status) == c.Status {
				filtered = append(filtered, t)
			}
		}
		tasks = filtered
	}

	// Newest first by the chosen timestamp. Stable so equal stamps preserve
	// relative order.
	sort.SliceStable(tasks, func(i, j int) bool {
		if c.Sort == "created" {
			return tasks[i].Created.After(tasks[j].Created)
		}
		return tasks[i].Modified.After(tasks[j].Modified)
	})

	if c.JSON {
		for _, t := range tasks {
			if err := writeJSON(os.Stdout, taskOut(t)); err != nil {
				return err
			}
		}
		return nil
	}

	if len(tasks) == 0 {
		fmt.Println("no tasks")
		return nil
	}

	w := os.Stdout
	for _, t := range tasks {
		if _, err := fmt.Fprintf(w, "%s  %s  %s%s\n", t.ID, statusGlyph(t.Status), tagPrefix(reg, t, c.All), t.Description); err != nil {
			return err
		}
	}
	return nil
}

// tagPrefix returns "" in single-project mode, otherwise `[tag] ` so callers
// can tell rows from different projects apart in the merged view.
func tagPrefix(reg *project.Registry, t storage.Task, allProjects bool) string {
	if !allProjects {
		return ""
	}
	tag := reg.Tag(t.ProjectSlug)
	if tag == "" {
		tag = t.ProjectSlug
	}
	var b strings.Builder
	b.WriteByte('[')
	b.WriteString(tag)
	b.WriteString("] ")
	return b.String()
}
