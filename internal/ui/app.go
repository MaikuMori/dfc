package ui

import (
	"errors"
	"os"
	"os/signal"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
	"golang.org/x/term"

	"github.com/MaikuMori/dfc/internal/core"
	"github.com/MaikuMori/dfc/internal/project"
	"github.com/MaikuMori/dfc/internal/savedsearch"
	"github.com/MaikuMori/dfc/internal/storage"
	"github.com/MaikuMori/dfc/internal/watch"
)

// attachFile adds a single file to the watcher, creating it empty first when
// missing so the Add can succeed (both registry and saved-search loaders
// tolerate an empty file). Reports whether the watch landed.
func attachFile(w *watch.Watcher, path string) bool {
	if path == "" {
		return false
	}
	if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
		if f, ferr := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o644); ferr == nil {
			_ = f.Close()
		}
	}
	return w.Add(path) == nil
}

// Run launches the TUI bound to the current working directory's project, or
// to opts.Project when that project is still registered. It returns a
// friendly error when stdout is not a terminal.
func Run(opts Options) error {
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return errors.New("not a terminal — run `dfc c \"...\"` from a non-interactive context")
	}
	opts = readStateOptions(opts)

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

	slug := resolved.Slug
	if p := opts.Project; p != "" && cr.Registry().Has(p) && storage.ProjectDirExists(p) {
		slug = p
	} else if err := cr.EnsureProject(resolved.Slug, project.DeriveName(resolved)); err != nil {
		return err
	}
	if err := cr.Registry().Touch(slug); err != nil {
		return err
	}

	store, err := cr.StoreFor(slug)
	if err != nil {
		return err
	}
	tasks, err := store.List()
	if err != nil {
		return err
	}

	// Filesystem watcher is optional: if it fails to initialize we still
	// launch the TUI, just without live updates. We always try to attach
	// to the current project's task dir, the registry file (so out-of-process
	// `dfc` invocations that register new projects show up live in global
	// view, or on the next `P` press per-project), and searches.json (so
	// `dfc searches save` in another shell reaches the `S` picker and `@name`
	// queries without a relaunch).
	var watcher *watch.Watcher
	if w, werr := watch.New(); werr == nil {
		anyAttached := w.Add(store.Dir()) == nil
		if attachFile(w, storage.RegistryPath()) {
			anyAttached = true
		}
		if attachFile(w, savedsearch.Path()) {
			anyAttached = true
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

	model := New(cr, slug, store, tasks, watcher)
	model.res = newResumeState(cwd)
	if model.res != nil {
		model.res.launch = slug
	}
	model = model.applyOptions(opts)
	if len(tasks) == 0 && slug == resolved.Slug {
		if shift, ok := cr.DetectSlugShift(cwd, resolved.Slug); ok {
			model.status = shift.Hint()
		}
	}
	// AltScreen is now declared per-frame in View(); bubbletea v2 dropped
	// the WithAltScreen program option. No keyboard-enhancement options
	// are needed: v2 auto-requests basic disambiguation, which is what
	// gives us "shift+enter" recognition on terminals that support the
	// Kitty keyboard protocol (Ghostty, Kitty, Alacritty, WezTerm, etc.).
	prog := tea.NewProgram(model)

	// A closing terminal sends SIGHUP; quit through the program so the state
	// file is flushed below and the resume record is left in place.
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	defer signal.Stop(hup)
	go func() {
		if _, ok := <-hup; ok {
			prog.Quit()
			// Quit waits while $EDITOR runs; don't outlive the terminal.
			time.Sleep(2 * time.Second)
			prog.Kill()
		}
	}()

	final, err := prog.Run()
	var fm Model
	switch f := final.(type) {
	case Model:
		fm = f
	case *Model:
		fm = *f
	default:
		return err
	}
	if errors.Is(err, tea.ErrProgramKilled) {
		err = nil
	}
	if derr := fm.finishState(); derr != nil && err == nil {
		err = derr
	}
	if cr.ReloadRegistry() == nil {
		if serr := cr.Registry().MarkSeen(fm.seenSlugs()...); serr != nil && err == nil {
			err = serr
		}
	}
	return err
}
