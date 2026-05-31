package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/MaikuMori/dfc/internal/core"
)

// ProjectsRenameCmd sets a project's display name (mirrors the TUI picker's
// ctrl+r).
type ProjectsRenameCmd struct {
	JSON    bool   `name:"json" help:"Emit the updated project as a single JSON object."`
	Project string `arg:"" predictor:"project" help:"Project to rename (slug or current display name)."`
	Name    string `arg:"" help:"New display name."`
}

func (c *ProjectsRenameCmd) Run() error {
	slug, _, found, err := resolveProjectInput(c.Project)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("unknown project %q", c.Project)
	}
	cr, err := openCore()
	if err != nil {
		return err
	}
	defer func() { _ = cr.Close() }()
	if err := cr.Registry().Rename(slug, c.Name); err != nil {
		return err
	}
	return emitProjectMeta(cr, slug, c.JSON, fmt.Sprintf("renamed %s → %s", slug, c.Name))
}

// ProjectsSetPrefixCmd sets a project's global-view prefix (mirrors ctrl+p).
type ProjectsSetPrefixCmd struct {
	JSON    bool   `name:"json" help:"Emit the updated project as a single JSON object."`
	Project string `arg:"" predictor:"project" help:"Project (slug or display name)."`
	Prefix  string `arg:"" optional:"" help:"New global-view prefix. Empty reverts to the derived default."`
}

func (c *ProjectsSetPrefixCmd) Run() error {
	slug, _, found, err := resolveProjectInput(c.Project)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("unknown project %q", c.Project)
	}
	cr, err := openCore()
	if err != nil {
		return err
	}
	defer func() { _ = cr.Close() }()
	if err := cr.Registry().SetPrefix(slug, c.Prefix); err != nil {
		return err
	}
	return emitProjectMeta(cr, slug, c.JSON,
		fmt.Sprintf("prefix for %s → [%s]", slug, cr.Registry().Prefix(slug)))
}

// ProjectsMergeCmd folds the source project's tasks into the destination, then
// removes the (now-empty) source project.
type ProjectsMergeCmd struct {
	JSON bool   `name:"json" help:"Emit {src, dst, moved} as a single JSON object."`
	Src  string `arg:"" predictor:"project" help:"Source project (folded in, then removed)."`
	Dst  string `arg:"" predictor:"project" help:"Destination project (created if new)."`
}

func (c *ProjectsMergeCmd) Run() error {
	srcSlug, _, found, err := resolveProjectInput(c.Src)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("unknown project %q", c.Src)
	}
	dstSlug, dstName, _, err := resolveProjectInput(c.Dst)
	if err != nil {
		return err
	}
	if dstSlug == "" {
		return errors.New("missing destination project")
	}
	if dstSlug == srcSlug {
		return errors.New("source and destination are the same project")
	}

	cr, err := openCore()
	if err != nil {
		return err
	}
	defer func() { _ = cr.Close() }()
	// Register the destination with the typed name before the merge so a
	// brand-new project keeps a friendly display name.
	if err := cr.EnsureProject(dstSlug, dstName); err != nil {
		return err
	}
	moved, err := cr.MergeProject(srcSlug, dstSlug)
	if err != nil {
		return err
	}
	return emitMerge(srcSlug, dstSlug, moved, c.JSON)
}

// emitProjectMeta re-reads slug's registry row + counts and emits it, so
// rename/set-prefix --json output matches a `dfc projects --json` row.
func emitProjectMeta(cr *core.Core, slug string, jsonOut bool, humanLine string) error {
	reg := cr.Registry()
	cnt := cr.CountsByProject()[slug]
	if jsonOut {
		return writeJSON(os.Stdout, projectOut(reg, slug, cnt.Open, cnt.Done))
	}
	fmt.Println(humanLine)
	return nil
}

func emitMerge(srcSlug, dstSlug string, moved int, jsonOut bool) error {
	if jsonOut {
		return writeJSON(os.Stdout, map[string]any{
			"src":   srcSlug,
			"dst":   dstSlug,
			"moved": moved,
		})
	}
	fmt.Printf("merged %s into %s  (%d task%s moved)\n", srcSlug, dstSlug, moved, pluralS(moved))
	return nil
}
