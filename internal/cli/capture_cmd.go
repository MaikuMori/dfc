package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/MaikuMori/dfc/internal/capture"
	"github.com/MaikuMori/dfc/internal/core"
	"github.com/MaikuMori/dfc/internal/project"
	"github.com/MaikuMori/dfc/internal/ui"
)

// CaptureCmd writes a new task.
//
// Inputs:
//   - `dfc c "description"` — single-line description, no details body.
//   - `dfc c "description" --details "body"` — description plus details.
//   - `dfc c "description" --details -` — details read from stdin.
//   - `dfc c -` — whole stdin: first non-blank line = description, rest =
//     details (useful for piping a buffer in).
//   - `dfc c` (no args, TTY) — inline prompt overlay.
type CaptureCmd struct {
	Project     string   `name:"project" short:"p" predictor:"project" help:"Target project slug (overrides cwd)."`
	Details     string   `name:"details" short:"d" help:"Details body. Use '-' to read from stdin."`
	JSON        bool     `name:"json" help:"Emit the created task as a single JSON object."`
	Description []string `arg:"" optional:"" help:"Task description; use '-' to read description+details from stdin, omit to open an inline prompt."`
}

func (c *CaptureCmd) Run() error {
	desc := strings.TrimSpace(strings.Join(c.Description, " "))
	details := c.Details

	// `-` as the positional reads the whole stdin and splits first
	// non-blank line off as the description, the rest as details.
	if desc == "-" {
		body, err := io.ReadAll(os.Stdin)
		if err != nil {
			return err
		}
		desc, details = capture.SplitHeading(string(body))
	}

	if details == "-" {
		body, err := io.ReadAll(os.Stdin)
		if err != nil {
			return err
		}
		details = strings.TrimRight(string(body), "\n")
	}

	slug, displayName, err := resolveCaptureTarget(c.Project)
	if err != nil {
		return err
	}

	if desc == "" {
		if !term.IsTerminal(int(os.Stdout.Fd())) {
			return errors.New("no description supplied — pass one as an argument, or pipe `-` to read description+details from stdin")
		}
		text, ok, err := ui.QuickCapture("")
		if err != nil {
			return err
		}
		if !ok {
			return nil // user canceled or submitted empty
		}
		// The inline prompt accepts alt+enter / ctrl+j for newline, so the
		// returned text may carry both heading and body. The first non-blank
		// line becomes the description; the rest is the details body.
		// An explicit -d wins over inline body when no inline body was typed.
		inlineDesc, inlineDetails := capture.SplitHeading(text)
		desc = inlineDesc
		if inlineDetails != "" {
			details = inlineDetails
		}
	}

	cr, err := openCore()
	if err != nil {
		return err
	}
	defer func() { _ = cr.Close() }()

	res, err := cr.Capture(core.CaptureInput{
		Slug:        slug,
		Description: desc,
		Details:     details,
		DisplayName: displayName,
	})
	if err != nil {
		return err
	}
	if c.JSON {
		return writeJSON(os.Stdout, taskOut(res.Task))
	}
	if res.NewProject {
		fmt.Println("captured · new project")
	} else {
		fmt.Println("captured")
	}
	return nil
}

// resolveCaptureTarget decides which project a capture lands in and
// returns the display name Core should use when auto-registering it.
// Explicit -p input is resolved through the registry (accepting slug
// or display name); unknown values fall through as literal slugs so
// capture can register new projects. Without -p, cwd resolves to a
// slug + derived friendly name.
func resolveCaptureTarget(explicit string) (slug, displayName string, err error) {
	if explicit != "" {
		slug, displayName, _, err = resolveProjectInput(explicit)
		return slug, displayName, err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", "", err
	}
	resolved, err := project.Resolve(cwd)
	if err != nil {
		return "", "", err
	}
	return resolved.Slug, project.DeriveName(resolved), nil
}
