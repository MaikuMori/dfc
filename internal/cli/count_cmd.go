package cli

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/MaikuMori/dfc/internal/core"
	"github.com/MaikuMori/dfc/internal/storage"
)

// CountCmd reports task counts for shell-prompt / tmux integration. It adds no
// state on disk — open/done come from the search index, done_today from the
// task files' mtimes.
type CountCmd struct {
	Format      string `name:"format" enum:"human,prompt,json" default:"human" help:"Output format: human (default), prompt, or json."`
	AllProjects bool   `name:"all-projects" short:"a" help:"Count across every project (default: cwd project)."`
	Project     string `name:"project" short:"p" predictor:"project" help:"Count one project (slug or display name)."`
}

func (c *CountCmd) Run() error {
	if c.AllProjects && c.Project != "" {
		return errors.New("-a and -p are mutually exclusive")
	}
	cr, err := openCore()
	if err != nil {
		return err
	}
	defer func() { _ = cr.Close() }()

	slugs, err := c.scope(cr)
	if err != nil {
		return err
	}

	counts := cr.CountsByProject()
	var open, done int
	for _, s := range slugs {
		open += counts[s].Open
		done += counts[s].Done
	}

	switch c.Format {
	case "prompt":
		fmt.Println(promptCount(open))
	case "json":
		reg := cr.Registry()
		byTag := map[string]int{}
		for _, s := range slugs {
			for _, tag := range reg.Tags(s) {
				byTag[tag] += counts[s].Open
			}
		}
		doneToday := 0
		for _, s := range slugs {
			if !storage.ProjectDirExists(s) {
				continue // don't resurrect a registry stub with no dir
			}
			tasks, err := cr.List(s)
			if err != nil {
				return err
			}
			for _, t := range tasks {
				if t.Status == storage.StatusDone && isToday(t.Modified) {
					doneToday++
				}
			}
		}
		return writeJSON(os.Stdout, countOut{Open: open, Done: done, DoneToday: doneToday, ByTag: byTag})
	default:
		fmt.Printf("%d open · %d done\n", open, done)
	}
	return nil
}

func (c *CountCmd) scope(cr *core.Core) ([]string, error) {
	switch {
	case c.AllProjects:
		return cr.Registry().Slugs(), nil
	case c.Project != "":
		s, _, found, err := resolveProjectInput(c.Project)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, fmt.Errorf("unknown project %q", c.Project)
		}
		return []string{s}, nil
	default:
		s, err := resolveCwdSlug()
		if err != nil {
			return nil, err
		}
		return []string{s}, nil
	}
}

// countOut is the JSON shape for `dfc count --format json`.
type countOut struct {
	Open      int            `json:"open"`
	Done      int            `json:"done"`
	DoneToday int            `json:"done_today"`
	ByTag     map[string]int `json:"by_tag"`
}

// promptCount renders a compact prompt segment of the open-task count ("12○"),
// or "" when there are none.
func promptCount(open int) string {
	if open == 0 {
		return ""
	}
	return fmt.Sprintf("%d○", open)
}

// isToday reports whether ts falls on the current local calendar day.
func isToday(ts time.Time) bool {
	y, m, d := ts.Local().Date()
	ny, nm, nd := time.Now().Date()
	return y == ny && m == nm && d == nd
}
