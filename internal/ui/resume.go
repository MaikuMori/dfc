package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

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
	Launch  string // project the resumed TUI was first started in
	State   string
}

// stateSaveInterval caps how often the state file is written, and so how much
// typing a killed terminal can lose.
const stateSaveInterval = 300 * time.Millisecond

// resumeState is shared across Model copies (like readIDs).
type resumeState struct {
	exe     string // argv[0] of the resume command
	dir     string // working directory of the resume command
	launch  string // project the TUI started in
	inst    *resume.Instance
	pending resume.State // latest state, written on the next tick
	saved   resume.State // what the state file holds
	ticking bool         // a save tick is scheduled
}

type stateSaveMsg struct{}

// newResumeState opens this instance's state file, or returns nil when it or
// the resume command can't be set up.
func newResumeState(cwd string) *resumeState {
	exe, err := resumeExe()
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
	return &resumeState{exe: exe, dir: cwd, inst: inst}
}

// resumeExe is the program a resume command should run: the `dfc` on PATH
// when it is this binary, so an upgrade that swaps what a symlink points at
// still resumes, otherwise this binary's own path.
func resumeExe() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if p, err := exec.LookPath("dfc"); err == nil {
		if abs, err := filepath.Abs(p); err == nil && sameFile(abs, exe) {
			return abs, nil
		}
	}
	return exe, nil
}

// readStateOptions folds the view saved at opts.State into opts and points
// DFC_ROOT at the data root the file lives under, so the resumed TUI opens
// the same stores whatever environment the terminal starts it in. It runs
// before anything resolves the root.
func readStateOptions(opts Options) Options {
	if opts.State == "" {
		return opts
	}
	st, ok := resume.Read(opts.State)
	root := resume.RootOf(opts.State)
	if !ok || root == "" {
		return opts
	}
	if cur, err := storage.Root(); err != nil || !sameFile(cur, root) {
		_ = os.Setenv(storage.EnvRoot, root)
	}
	v := st.View
	opts.Project, opts.Global, opts.Query, opts.Sort, opts.Cursor, opts.Launch = v.Project, v.Global, v.Query, v.Sort, v.Cursor, v.Launch
	return opts
}

func sameFile(a, b string) bool {
	ai, aerr := os.Stat(a)
	bi, berr := os.Stat(b)
	return aerr == nil && berr == nil && os.SameFile(ai, bi)
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
	v := resume.View{Project: m.slug, Global: m.globalView, Query: m.searchQuery, Launch: m.res.launch}
	if m.sortKey == sortByCreated {
		v.Sort = sortByCreated.String()
	}
	if m.cursor >= 0 && m.cursor < len(m.tasks) {
		v.Cursor = m.tasks[m.cursor].ID
	}
	return resume.State{View: v, Draft: m.currentDraft()}
}

// currentDraft returns the unsubmitted input on screen, if any. An inline
// edit counts once its text differs from what the editor opened with.
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
		if v := m.input.Value(); v != m.editOrig {
			return resume.Draft{Kind: resume.KindEdit, Text: v, Slug: m.editSlug, TaskID: m.editID}
		}
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
	r.pending = m.currentState()
	// Without a draft the file only serves a terminal resume.
	if m.quitting || (r.pending.Draft.IsZero() && os.Getenv(ansi.ProgramResumeTokenEnv) == "") {
		return r.inst.Close(true)
	}
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
			if (st.View.Global || st.Draft.Slug != m.slug) && !m.globalView && len(m.core.Registry().Slugs()) > 1 {
				// The draft targets, or edits a task in, another project;
				// show it where its result will be listed.
				m.globalView = true
				if err := m.refreshWatches(); err != nil {
					m.err = err
				}
				m = m.reloadActive()
			}
			m = m.restoreDraft(st)
			if m.mode != modeList && m.status == "" {
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
// longer running whose draft belongs here: made in a TUI launched in or
// showing the current project, or aimed at the current project. Edits of
// tasks that are gone come last; restoreDraft reopens their text as a
// capture.
func (m Model) recoverState() (resume.State, bool) {
	var goneEdits []string
	for _, path := range resume.Orphans(filepath.Dir(m.res.inst.Path())) {
		st, ok := resume.Read(path)
		if !ok || (st.Draft.Slug != m.slug && st.View.Project != m.slug && st.View.Launch != m.slug) {
			continue
		}
		if d := st.Draft; d.Kind == resume.KindEdit && !m.taskExists(d.TaskID) {
			goneEdits = append(goneEdits, path)
			continue
		}
		if st, ok := m.res.inst.Claim(path); ok {
			return st, true
		}
	}
	for _, path := range goneEdits {
		if st, ok := m.res.inst.Claim(path); ok {
			return st, true
		}
	}
	return resume.State{}, false
}

// taskExists reports whether a task with id is on disk in any project.
func (m Model) taskExists(id string) bool {
	_, path, err := m.core.FindByID(id)
	return err == nil && path != ""
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
		switch {
		case target == m.slug:
			target = ""
		case !storage.ProjectDirExists(target):
			m.status = fmt.Sprintf("project %s is gone; enter captures into %s", target, m.core.Registry().Name(m.slug))
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
		if !found {
			// Keep the text rather than lose it with the task.
			m.mode = modeCapture
			m.beginCapture()
			m.capArea.SetValue(d.Text)
			resizeQuickCapture(&m.capArea)
			m.relayout()
			m.status = "the edited task is gone; enter saves the text as a new task"
			break
		}
		// The task may have moved projects; follow it, not the saved slug.
		t := m.tasks[m.cursor]
		m.mode = modeEdit
		m.editID, m.editSlug, m.editOrig = t.ID, t.ProjectSlug, t.Description
		m.input.Reset()
		m.input.SetValue(d.Text)
		m.input.CursorEnd()
		m.input.Focus()
		m.relayout()
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
