package cli

import (
	"cmp"
	"fmt"
	"os"
	"slices"
	"text/tabwriter"

	"github.com/MaikuMori/dfc/internal/project"
)

// ProjectsCmd lists every project in the registry, ordered by recency
// (most-recently-used first; never-used entries sorted by name).
type ProjectsCmd struct {
	Tag  []string `name:"tag" predictor:"tag" help:"Filter to projects carrying this tag. Repeatable / comma-separated."`
	JSON bool     `name:"json" help:"Emit one JSON object per project (NDJSON)."`
}

func (c *ProjectsCmd) Run() error {
	cr, err := openCore()
	if err != nil {
		return err
	}
	defer func() { _ = cr.Close() }()
	reg := cr.Registry()
	counts := cr.CountsByProject()

	tagFilter := expandCommaTagFilter(c.Tag)
	slugs := projectSlugsMatchingTags(reg, tagFilter)
	// Sort by LastUsed desc, slug asc.
	sortSlugsByRecency(reg, slugs)

	if c.JSON {
		for _, s := range slugs {
			c := counts[s]
			if err := writeJSON(os.Stdout, projectOut(reg, s, c.Open, c.Done)); err != nil {
				return err
			}
		}
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for _, s := range slugs {
		c := counts[s]
		_, _ = fmt.Fprintf(w, "%s\t[%s]\t%s\t%d open · %d done\n", s, reg.Prefix(s), reg.Name(s), c.Open, c.Done)
	}
	return w.Flush()
}

// ProjectCmd prints the project resolved from cwd.
type ProjectCmd struct {
	JSON bool `name:"json" help:"Emit the resolved project as a single JSON object."`
}

func (c *ProjectCmd) Run() error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	resolved, err := project.Resolve(cwd)
	if err != nil {
		return err
	}
	reg, err := project.LoadRegistry()
	if err != nil {
		return err
	}

	cr, err := openCore()
	if err == nil {
		defer func() { _ = cr.Close() }()
	}
	var open, done int
	if cr != nil {
		cnts := cr.CountsByProject()[resolved.Slug]
		open, done = cnts.Open, cnts.Done
	}

	if c.JSON {
		out := projectOut(reg, resolved.Slug, open, done)
		out.Source = string(resolved.Source)
		out.Raw = resolved.Raw
		return writeJSON(os.Stdout, out)
	}

	fmt.Printf("slug:    %s\n", resolved.Slug)
	fmt.Printf("name:    %s\n", reg.Name(resolved.Slug))
	fmt.Printf("prefix:  %s\n", reg.Prefix(resolved.Slug))
	fmt.Printf("tasks:   %d open · %d done\n", open, done)
	fmt.Printf("source:  %s\n", resolved.Source)
	if resolved.Raw != "" {
		fmt.Printf("raw:     %s\n", resolved.Raw)
	}
	return nil
}

func sortSlugsByRecency(reg *project.Registry, slugs []string) {
	slices.SortStableFunc(slugs, func(a, b string) int {
		la, lb := reg.LastUsed(a), reg.LastUsed(b)
		if !la.Equal(lb) {
			return lb.Compare(la)
		}
		return cmp.Compare(a, b)
	})
}
