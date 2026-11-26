package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/MaikuMori/dfc/internal/resume"
	"github.com/MaikuMori/dfc/internal/storage"
)

// Options seeds a TUI launch; every field tolerates stale values. State
// names a saved state file (the resume command a terminal runs) whose view
// replaces the other fields and whose draft reopens. Without State, a launch
// recovers a draft left by a dfc that was killed.
type Options struct {
	Project string // project slug; the cwd project is used when its directory is gone
	Global  bool
	Query   string
	Sort    string // "modified" or "created"
	Cursor  string // task id to put the cursor on
	State   string
}

// stateSaveInterval caps how often the state file is written, and so how much
// typing a killed terminal can lose.
const stateSaveInterval = 300 * time.Millisecond

// resumeState is shared across Model copies (like readIDs).
type resumeState struct {
	exe     string // argv[0] of the resume command
	dir     string // working directory of the resume command
	root    string // DFC_ROOT override to carry into the state, if any
	inst    *resume.Instance
	pending resume.State // latest state, written on the next tick
	saved   resume.State // what the state file holds
	ticking bool         // a save tick is scheduled
}

type stateSaveMsg struct{}

// newResumeState opens this instance's state file, or returns nil when it or
// the resume command can't be set up.
func newResumeState(cwd string) *resumeState {
	exe, err := os.Executable()
	if err != nil {
		return nil
	}
	root, err := storage.Root()
	if err != nil {
		return nil
	}
	dir := resume.Dir(root)
	resume.Prune(dir, resume.MaxAge)
	inst, err := resume.Open(dir)
	if err != nil {
		return nil
	}
	r := &resumeState{exe: exe, dir: cwd, inst: inst}
	if os.Getenv(storage.EnvRoot) != "" {
		r.root, _ = filepath.Abs(root)
	}
	return r
}

// readStateOptions folds the view saved at opts.State into opts and applies
// its data root, so the resumed TUI opens the same stores. It runs before
// anything resolves the root.
func readStateOptions(opts Options) Options {
	if opts.State == "" {
		return opts
	}
	st, ok := resume.Read(opts.State)
	if !ok {
		return opts
	}
	if st.Root != "" && os.Getenv(storage.EnvRoot) == "" {
		_ = os.Setenv(storage.EnvRoot, st.Root)
	}
	v := st.View
	opts.Project, opts.Global, opts.Query, opts.Sort, opts.Cursor = v.Project, v.Global, v.Query, v.Sort, v.Cursor
	return opts
}

// quit ends the program as a deliberate user exit: the last frame clears the
// resume record and the state file is dropped.
func (m Model) quit() (tea.Model, tea.Cmd) {
	m.quitting = true
	return m, tea.Quit
}

// programResume is the command that reopens this TUI where it stands, or nil
// once the user has quit. It stays fixed for the instance's lifetime; the
// state file carries everything that changes.
func (m Model) programResume() *tea.ProgramResume {
	r := m.res
	if r == nil || m.quitting {
		return nil
	}
	return &tea.ProgramResume{Args: []string{r.exe, "_tui", "--state", r.inst.Path()}, Dir: r.dir}
}

// currentState is what the state file should hold right now.
func (m Model) currentState() resume.State {
	v := resume.View{Project: m.slug, Global: m.globalView, Query: m.searchQuery}
	if m.sortKey == sortByCreated {
		v.Sort = sortByCreated.String()
	}
	if m.cursor >= 0 && m.cursor < len(m.tasks) {
		v.Cursor = m.tasks[m.cursor].ID
	}
	return resume.State{Root: m.res.root, View: v, Draft: m.currentDraft()}
}

// currentDraft returns the unsubmitted input on screen, if any. An inline
// edit counts once its text differs from the description of the task it
// opened on.
func (m Model) currentDraft() resume.Draft {
	switch m.mode {
	case modeCapture:
		if v := m.capArea.Value(); strings.TrimSpace(v) != "" {
			slug := m.capTargetSlug
			if slug == "" {
				slug = m.slug
			}
			return resume.Draft{Kind: resume.KindCapture, Text: v, Slug: slug}
		}
	case modeEdit:
		if m.editID == "" {
			return resume.Draft{}
		}
		v := m.input.Value()
		if i := indexByIDSlug(m.tasks, m.editID, m.editSlug, true); i >= 0 && m.tasks[i].Description == v {
			return resume.Draft{}
		}
		return resume.Draft{Kind: resume.KindEdit, Text: v, Slug: m.editSlug, TaskID: m.editID}
	}
	return resume.Draft{}
}

// saveTick schedules the next state write.
func saveTick() tea.Cmd {
	return tea.Tick(stateSaveInterval, func(time.Time) tea.Msg { return stateSaveMsg{} })
}

// trackState notes a change to the state. Writes are throttled to one per
// stateSaveInterval, except that a draft going away is written at once, so a
// submitted capture can't come back as unsent.
func (m Model) trackState(cmd tea.Cmd) (Model, tea.Cmd) {
	r := m.res
	if r == nil {
		return m, cmd
	}
	st := m.currentState()
	if st == r.pending {
		return m, cmd
	}
	draftGone := !r.pending.Draft.IsZero() && st.Draft.IsZero()
	r.pending = st
	if draftGone {
		if err := m.flushState(); err != nil {
			m.err = err
		}
		return m, cmd
	}
	if !r.ticking {
		r.ticking = true
		cmd = tea.Batch(cmd, saveTick())
	}
	return m, cmd
}

// flushState writes the pending state if it hasn't been written yet.
func (m Model) flushState() error {
	r := m.res
	if r == nil || r.pending == r.saved {
		return nil
	}
	if err := r.inst.Save(r.pending); err != nil {
		return fmt.Errorf("could not save draft: %w", err)
	}
	r.saved = r.pending
	return nil
}

// finishState settles the state file as the program exits: removed on a
// deliberate quit, written out otherwise so a resume or the next launch can
// pick it up.
func (m Model) finishState() error {
	r := m.res
	if r == nil {
		return nil
	}
	if m.quitting {
		return r.inst.Close(true)
	}
	r.pending = m.currentState()
	err := m.flushState()
	_ = r.inst.Close(false)
	return err
}

// applyOptions restores the view described by opts onto a freshly built
// model, then reopens a draft: the one in opts.State, or on a fresh launch
// one abandoned by a killed dfc. Values that no longer apply (a project that
// left the registry, a query that won't parse, a vanished task) are dropped.
func (m Model) applyOptions(opts Options) Model {
	initialRead := ""
	if m.cursor >= 0 && m.cursor < len(m.tasks) {
		initialRead = m.tasks[m.cursor].ID
	}
	if opts.Sort == sortByCreated.String() {
		m.sortKey = sortByCreated
		m.tasks = sortDoneFirst(m.tasks, m.sortKey)
	}
	reload := false
	if opts.Global && len(m.core.Registry().Slugs()) > 1 {
		m.globalView = true
		reload = true
		if err := m.refreshWatches(); err != nil {
			m.err = err
		}
	}
	if opts.Query != "" {
		m.searchQuery = opts.Query
		reload = true
	}
	if reload {
		m = m.reloadActive()
		if m.err != nil && m.searchQuery != "" {
			m.searchQuery = ""
			m.err = nil
			m = m.reloadActive()
		}
	}
	if opts.Cursor != "" {
		m.moveCursorTo(opts.Cursor)
	}
	if r := m.res; r != nil {
		if opts.State != "" {
			if st, ok := r.inst.Claim(opts.State); ok {
				m = m.restoreDraft(st)
			}
		} else if st, ok := m.recoverState(); ok {
			if st.View.Global && !m.globalView && len(m.core.Registry().Slugs()) > 1 {
				// The draft may target, or edit a task in, another project.
				m.globalView = true
				if err := m.refreshWatches(); err != nil {
					m.err = err
				}
				m = m.reloadActive()
			}
			m = m.restoreDraft(st)
			if m.mode != modeList {
				m.status = "recovered an unsent draft; esc discards it"
			}
		}
	}
	if m.cursor < 0 || m.cursor >= len(m.tasks) || m.tasks[m.cursor].ID != initialRead {
		delete(m.readIDs, initialRead)
	}
	return m
}

// recoverState claims the newest state file abandoned by a dfc that is no
// longer running whose draft belongs here: made in the current project's TUI,
// or aimed at the current project. An edit draft also needs its task to still
// exist.
func (m Model) recoverState() (resume.State, bool) {
	for _, path := range resume.Orphans(filepath.Dir(m.res.inst.Path())) {
		st, ok := resume.Read(path)
		if !ok || (st.Draft.Slug != m.slug && st.View.Project != m.slug) {
			continue
		}
		if d := st.Draft; d.Kind == resume.KindEdit {
			if _, err := m.core.FindByIDInProject(d.TaskID, d.Slug); err != nil {
				continue
			}
		}
		if st, ok := m.res.inst.Claim(path); ok {
			return st, true
		}
	}
	return resume.State{}, false
}

// moveCursorTo puts the cursor on the task with id, if listed.
func (m *Model) moveCursorTo(id string) bool {
	for i, t := range m.tasks {
		if t.ID == id {
			m.cursor = i
			m.readIDs[id] = true
			return true
		}
	}
	return false
}

// restoreDraft reopens the draft in st, a state file already claimed by this
// instance, in the overlay it came from.
func (m Model) restoreDraft(st resume.State) Model {
	switch d := st.Draft; d.Kind {
	case resume.KindCapture:
		target := d.Slug
		if target == m.slug || !storage.ProjectDirExists(target) {
			target = ""
		}
		m.mode = modeCapture
		m.beginCapture()
		m.capTargetSlug = target
		m.capArea.SetValue(d.Text)
		resizeQuickCapture(&m.capArea)
		m.relayout()
	case resume.KindEdit:
		found := m.moveCursorTo(d.TaskID)
		if !found && m.searchQuery != "" {
			// The task may have stopped matching the filter while dfc was
			// down; the edit matters more than the filter.
			m.searchQuery = ""
			m = m.reloadActive()
			found = m.moveCursorTo(d.TaskID)
		}
		if found {
			m.mode = modeEdit
			m.editID, m.editSlug = d.TaskID, d.Slug
			m.input.Reset()
			m.input.SetValue(d.Text)
			m.input.CursorEnd()
			m.input.Focus()
			m.relayout()
		}
	}
	if r := m.res; r != nil {
		r.saved = st
		r.pending = m.currentState()
		if err := m.flushState(); err != nil {
			m.err = err
		}
	}
	return m
}
