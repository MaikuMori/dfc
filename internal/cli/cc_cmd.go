package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/MaikuMori/dfc/internal/capture"
	"github.com/MaikuMori/dfc/internal/core"
	"github.com/MaikuMori/dfc/internal/ui"
	"golang.org/x/term"
)

// GlobalCaptureCmd is `dfc cc`: pick any known project via a fuzzy filter,
// then capture into it via the inline prompt.
type GlobalCaptureCmd struct{}

func (g *GlobalCaptureCmd) Run() error {
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return errors.New("interactive picker needs a terminal — use `dfc c -p <slug> \"...\"` to capture into a specific project from a script")
	}

	cr, err := openCore()
	if err != nil {
		return err
	}
	defer func() { _ = cr.Close() }()

	if err := cr.DiscoverProjects(); err != nil {
		return err
	}
	reg := cr.Registry()

	items := ui.ProjectPickerItems(reg, cr.CountsByProject())
	if len(items) == 0 {
		fmt.Println("no projects yet — run `dfc c \"...\"` in one first")
		return nil
	}

	var slug string
	if len(items) == 1 {
		// Picking from a list of one isn't a choice; jump straight to capture.
		slug = items[0].Slug
	} else {
		picked, ok, err := ui.Pick("capture to:", items, reg.Rename, reg.SetTag)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		slug = picked
	}

	text, ok, err := ui.QuickCapture("→ " + reg.Name(slug))
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	desc, details := capture.SplitHeading(text)

	_, err = cr.Capture(core.CaptureInput{
		Slug:        slug,
		Description: desc,
		Details:     details,
		DisplayName: reg.Name(slug),
	})
	return err
}
