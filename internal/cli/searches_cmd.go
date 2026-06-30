package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/MaikuMori/dfc/internal/savedsearch"
)

// SearchesCmd groups saved-search management. With no subcommand it lists.
// Saved searches are named query strings (text plus #tag / -#tag predicates)
// that `dfc s @name` / `dfc ss @name` expand.
type SearchesCmd struct {
	List SearchesListCmd `cmd:"" default:"withargs" aliases:"ls" help:"List saved searches (default)."`
	Save SearchesSaveCmd `cmd:"" help:"Save (or overwrite) a named search."`
	Rm   SearchesRmCmd   `cmd:"" help:"Remove a saved search."`
}

// SearchesListCmd lists every saved search.
type SearchesListCmd struct {
	JSON bool `name:"json" help:"Emit one {name, query} object per line (NDJSON)."`
}

func (c *SearchesListCmd) Run() error {
	cr, err := openCore()
	if err != nil {
		return err
	}
	defer func() { _ = cr.Close() }()
	ss, err := cr.SavedSearches()
	if err != nil {
		return err
	}
	entries := ss.List()
	if c.JSON {
		for _, e := range entries {
			if err := writeJSON(os.Stdout, e); err != nil {
				return err
			}
		}
		return nil
	}
	if len(entries) == 0 {
		fmt.Println("no saved searches")
		return nil
	}
	for _, e := range entries {
		fmt.Printf("@%s  %s\n", e.Name, e.Query)
	}
	return nil
}

// SearchesSaveCmd persists a named query string.
type SearchesSaveCmd struct {
	JSON  bool     `name:"json" help:"Emit the saved {name, query}."`
	Force bool     `name:"force" short:"f" help:"Overwrite an existing saved search of the same name."`
	Name  string   `arg:"" help:"Saved search name (free text; quote if it has spaces)."`
	Query []string `arg:"" help:"Query string (joined with spaces); text and #tag / -#tag predicates."`
}

func (c *SearchesSaveCmd) Run() error {
	cr, err := openCore()
	if err != nil {
		return err
	}
	defer func() { _ = cr.Close() }()
	ss, err := cr.SavedSearches()
	if err != nil {
		return err
	}
	if _, exists := ss.Get(c.Name); exists && !c.Force {
		return fmt.Errorf("saved search %q already exists (pass --force to overwrite)", strings.TrimSpace(c.Name))
	}
	query := strings.Join(c.Query, " ")
	if err := ss.Set(c.Name, query); err != nil {
		return err
	}
	if c.JSON {
		return writeJSON(os.Stdout, savedsearch.Entry{Name: c.Name, Query: strings.TrimSpace(query)})
	}
	fmt.Printf("saved @%s\n", c.Name)
	return nil
}

// SearchesRmCmd removes a saved search.
type SearchesRmCmd struct {
	JSON bool   `name:"json" help:"Emit {removed: <name>}."`
	Name string `arg:"" help:"Saved search name to remove."`
}

func (c *SearchesRmCmd) Run() error {
	cr, err := openCore()
	if err != nil {
		return err
	}
	defer func() { _ = cr.Close() }()
	ss, err := cr.SavedSearches()
	if err != nil {
		return err
	}
	ok, err := ss.Remove(c.Name)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("no saved search named %q", c.Name)
	}
	if c.JSON {
		return writeJSON(os.Stdout, map[string]string{"removed": c.Name})
	}
	fmt.Printf("removed @%s\n", c.Name)
	return nil
}
