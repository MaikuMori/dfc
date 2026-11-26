package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/MaikuMori/dfc/internal/resume"
	"github.com/MaikuMori/dfc/internal/storage"
)

// Options seeds a TUI launch. A resume command reported to the terminal
// carries these as `dfc tui` flags; every field tolerates stale values.
// Without Draft, a launch recovers a draft left by a dfc that was killed.
type Options struct {
	Project string // project slug; the cwd project is used when its directory is gone
	Global  bool
	Query   string
	Sort    string // "modified" or "created"
	Cursor  string // task id to put the cursor on
	Draft   string // draft file to reopen in the capture overlay or inline editor
}

// draftSaveDelay batches keystrokes into one draft write; it bounds how much
// typing a terminal crash can lose.
const draftSaveDelay = 300 * time.Millisecond

// resumeState is shared across Model copies (like readIDs).
type resumeState struct {
	exe string // argv[0] of the resume command
	dir string // working directory of the resume command
	// draftPath is this instance's draft file; "" disables draft saving.
	draftPath string
	pending   resume.Draft // latest draft, written after draftSaveDelay
	saved     resume.Draft // what draftPath holds
	gen       int          // bumped per draft change; only the newest tick saves
}

type draftSaveMsg struct{ gen int }

// newResumeState returns the resume state for this instance, or nil when the
// command to bring it back can't be built.
func newResumeState(cwd string) *resumeState {
	exe, err := os.Executable()
	if err != nil {
		return nil
	}
	r := &resumeState{exe: exe, dir: cwd}
	if root, err := storage.Root(); err == nil {
		dir := resume.Dir(root)
		resume.Prune(dir, resume.MaxAge)
		r.draftPath = resume.NewPath(dir)
	}
	return r
}

// quit ends the program as a deliberate user exit: the last frame clears the
// resume record and the draft is dropped.
func (m Model) quit() (tea.Model, tea.Cmd) {
	m.quitting = true
	return m, tea.Quit
}

// programResume is the command that reopens this TUI where it stands, or nil
// once the user has quit.
func (m Model) programResume() *tea.ProgramResume {
	r := m.res
	if r == nil || m.quitting {
		return nil
	}
	// `--flag=value` keeps values that start with `-` (e.g. `-#tag`) from
	// parsing as flags.
	args := []string{r.exe, "tui", "--project=" + m.slug}
	if m.globalView {
		args = append(args, "--global")
	}
	if m.searchQuery != "" {
		args = append(args, "--query="+m.searchQuery)
	}
	if m.sortKey == sortByCreated {
		args = append(args, "--sort=created")
	}
	if m.cursor >= 0 && m.cursor < len(m.tasks) {
		args = append(args, "--cursor="+m.tasks[m.cursor].ID)
	}
	if r.draftPath != "" && !r.pending.IsZero() {
		args = append(args, "--draft="+r.draftPath)
	}
	return &tea.ProgramResume{Args: args, Dir: r.dir}
}

// currentDraft returns the unsubmitted input on screen, if any. An inline
// edit counts only once its text differs from the task's description.
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
		if m.cursor >= 0 && m.cursor < len(m.tasks) {
			t := m.tasks[m.cursor]
			if v := m.input.Value(); v != t.Description {
				return resume.Draft{Kind: resume.KindEdit, Text: v, Slug: t.ProjectSlug, TaskID: t.ID}
			}
		}
	}
	return resume.Draft{}
}

// trackDraft schedules a draft save when the on-screen draft changed.
func (m Model) trackDraft(cmd tea.Cmd) tea.Cmd {
	r := m.res
	if r == nil || r.draftPath == "" {
		return cmd
	}
	d := m.currentDraft()
	if d == r.pending {
		return cmd
	}
	r.pending = d
	r.gen++
	gen := r.gen
	return tea.Batch(cmd, tea.Tick(draftSaveDelay, func(time.Time) tea.Msg { return draftSaveMsg{gen: gen} }))
}

// flushDraft writes the pending draft if it hasn't been written yet.
func (m Model) flushDraft() error {
	r := m.res
	if r == nil || r.draftPath == "" || r.pending == r.saved {
		return nil
	}
	if err := resume.Save(r.draftPath, r.pending); err != nil {
		return fmt.Errorf("could not save draft: %w", err)
	}
	r.saved = r.pending
	return nil
}

// finishDraft settles the draft file as the program exits: removed on a
// deliberate quit, kept current otherwise so a resume can reopen it.
func (m Model) finishDraft() error {
	if m.quitting && m.res != nil {
		m.res.pending = resume.Draft{}
	}
	return m.flushDraft()
}

// applyOptions restores the view described by opts onto a freshly built
// model. Values that no longer apply (a project that left the registry, a
// query that won't parse, a vanished task) are dropped.
func (m Model) applyOptions(opts Options) Model {
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
	if r := m.res; r != nil && r.draftPath != "" {
		if opts.Draft != "" {
			m = m.restoreDraft(resume.Claim(opts.Draft, r.draftPath))
		} else if d := m.recoverDraft(); !d.IsZero() {
			m = m.restoreDraft(d)
			if m.mode != modeList {
				m.status = "recovered an unsent draft; esc discards it"
			}
		}
	}
	return m
}

// recoverDraft claims the newest draft left behind by a dfc that is no longer
// running, if it belongs to the current project: a capture into it, or an
// edit of a listed task.
func (m Model) recoverDraft() resume.Draft {
	for _, o := range resume.Orphans(filepath.Dir(m.res.draftPath)) {
		switch d := o.Draft; d.Kind {
		case resume.KindCapture:
			if d.Slug != m.slug {
				continue
			}
		case resume.KindEdit:
			if !slices.ContainsFunc(m.tasks, func(t storage.Task) bool { return t.ID == d.TaskID }) {
				continue
			}
		}
		if d := resume.Claim(o.Path, m.res.draftPath); !d.IsZero() {
			return d
		}
	}
	return resume.Draft{}
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

// restoreDraft reopens d, already claimed onto this instance's draft path, in
// the overlay it came from.
func (m Model) restoreDraft(d resume.Draft) Model {
	switch d.Kind {
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
		if m.moveCursorTo(d.TaskID) {
			m.mode = modeEdit
			m.input.Reset()
			m.input.SetValue(d.Text)
			m.input.CursorEnd()
			m.input.Focus()
			m.relayout()
		}
	}
	// The claimed file holds d; rewrite it as what reopened, or drop it when
	// nothing could.
	if r := m.res; r != nil {
		r.saved = d
		r.pending = m.currentDraft()
		if err := m.flushDraft(); err != nil {
			m.err = err
		}
	}
	return m
}
