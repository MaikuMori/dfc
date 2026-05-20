package ui

import (
	"errors"
	"os"

	"github.com/MaikuMori/dfc/internal/core"
	"github.com/MaikuMori/dfc/internal/project"
	"github.com/MaikuMori/dfc/internal/storage"
	"github.com/MaikuMori/dfc/internal/watch"
	tea "charm.land/bubbletea/v2"
	"golang.org/x/term"
)

// Run launches the TUI bound to the current working directory's project. It
// returns a friendly error when stdout is not a terminal.
func Run() error {
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return errors.New("not a terminal — run `dfc c \"...\"` from a non-interactive context")
	}

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	resolved, err := project.Resolve(cwd)
	if err != nil {
		return err
	}

	cr, err := core.Open(core.Options{})
	if err != nil {
		return err
	}
	defer func() { _ = cr.Close() }()

	if err := cr.EnsureProject(resolved.Slug, project.DeriveName(resolved)); err != nil {
		return err
	}
	if err := cr.Registry().Touch(resolved.Slug); err != nil {
		return err
	}

	store, err := cr.StoreFor(resolved.Slug)
	if err != nil {
		return err
	}
	tasks, err := store.List()
	if err != nil {
		return err
	}

	// Filesystem watcher is optional: if it fails to initialize we still
	// launch the TUI, just without live updates. We always try to attach
	// to two things: the current project's task dir and the registry file,
	// so out-of-process `dfc` invocations that register new projects show
	// up live (in global view) or surface on the next `P` press
	// (per-project view).
	var watcher *watch.Watcher
	if w, werr := watch.New(); werr == nil {
		anyAttached := false
		if addErr := w.Add(store.Dir()); addErr == nil {
			anyAttached = true
		}
		if regPath := storage.RegistryPath(); regPath != "" {
			// Touch the file if missing so Add succeeds. Empty content
			// is fine — LoadRegistry tolerates it.
			if _, statErr := os.Stat(regPath); os.IsNotExist(statErr) {
				if f, ferr := os.OpenFile(regPath, os.O_CREATE|os.O_WRONLY, 0o644); ferr == nil {
					_ = f.Close()
				}
			}
			if addErr := w.Add(regPath); addErr == nil {
				anyAttached = true
			}
		}
		if anyAttached {
			watcher = w
			defer func() { _ = w.Close() }()
		} else {
			_ = w.Close()
		}
	}

	// Make sure the search index isn't blank or behind on first launch.
	cr.EnsureFresh()

	model := New(cr, resolved.Slug, store, tasks, watcher)
	// AltScreen is now declared per-frame in View(); bubbletea v2 dropped
	// the WithAltScreen program option. No keyboard-enhancement options
	// are needed: v2 auto-requests basic disambiguation, which is what
	// gives us "shift+enter" recognition on terminals that support the
	// Kitty keyboard protocol (Ghostty, Kitty, Alacritty, WezTerm, etc.).
	prog := tea.NewProgram(model)
	_, err = prog.Run()
	return err
}
