package ui

import (
	"os"
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/MaikuMori/dfc/internal/resume"
	"github.com/MaikuMori/dfc/internal/storage"
)

func runeKey(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Text: string(r)} }

func resumeDir(t *testing.T) string {
	t.Helper()
	root, err := storage.Root()
	if err != nil {
		t.Fatal(err)
	}
	return resume.Dir(root)
}

// withResume gives m its own state file, as on a terminal that supports
// program resume.
func withResume(t *testing.T, m Model) Model {
	t.Helper()
	t.Setenv(ansi.ProgramResumeTokenEnv, "test")
	inst, err := resume.Open(resumeDir(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = inst.Close(true) })
	m.res = &resumeState{exe: "/usr/local/bin/dfc", dir: "/src/work", inst: inst}
	return m
}

// newResumeModel is the per-project "work" model (plus a "home" project)
// with its own state file.
func newResumeModel(t *testing.T) Model {
	t.Helper()
	return withResume(t, newPerProjectMoveModel(t))
}

// sibling is a second instance over the same data, as a relaunch would be.
func sibling(t *testing.T, m Model) Model {
	t.Helper()
	store, err := m.core.StoreFor("work")
	if err != nil {
		t.Fatal(err)
	}
	tasks, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	n := New(m.core, "work", store, tasks, nil)
	n.width, n.height = 80, 24
	n.relayout()
	return withResume(t, n)
}

func press(t *testing.T, m Model, keys ...tea.KeyPressMsg) Model {
	t.Helper()
	for _, k := range keys {
		next, _ := m.Update(k)
		m = next.(Model)
	}
	return m
}

func settle(m Model) Model {
	next, _ := m.Update(stateSaveMsg{})
	return next.(Model)
}

func saved(t *testing.T, m Model) resume.State {
	t.Helper()
	st, _ := resume.Read(m.res.inst.Path())
	return st
}

// abandon exits m without a deliberate quit, as a closed terminal does.
func abandon(t *testing.T, m Model) string {
	t.Helper()
	if err := m.finishState(); err != nil {
		t.Fatal(err)
	}
	return m.res.inst.Path()
}

func TestProgramResumeIsStateFile(t *testing.T) {
	m := newResumeModel(t)
	pr := m.programResume()
	want := []string{"/usr/local/bin/dfc", "_tui", "--state", m.res.inst.Path()}
	if pr == nil || !slices.Equal(pr.Args, want) || pr.Dir != "/src/work" {
		t.Fatalf("programResume = %+v, want %q in /src/work", pr, want)
	}
	m = press(t, m, runeKey('k'), runeKey('c'), runeKey('x'))
	if got := m.programResume().Args; !slices.Equal(got, want) {
		t.Errorf("Args changed with the view: %q", got)
	}
}

func TestQuitClearsResumeAndState(t *testing.T) {
	m := settle(press(t, newResumeModel(t), runeKey('c'), runeKey('x')))
	m = press(t, m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if !m.quitting || m.View().ProgramResume != nil {
		t.Fatalf("quitting = %v, ProgramResume = %+v", m.quitting, m.View().ProgramResume)
	}
	if err := m.finishState(); err != nil {
		t.Fatal(err)
	}
	if _, ok := resume.Read(m.res.inst.Path()); ok {
		t.Error("state file survived a deliberate quit")
	}
}

func TestStateSavesAreThrottled(t *testing.T) {
	m := newResumeModel(t)
	next, cmd := m.Update(runeKey('c'))
	m = next.(Model)
	if !m.res.ticking || cmd == nil {
		t.Fatal("no save scheduled after a change")
	}
	m = press(t, m, runeKey('h'), runeKey('i'))
	if _, ok := resume.Read(m.res.inst.Path()); ok {
		t.Fatal("state written before the tick")
	}
	m = settle(m)
	st := saved(t, m)
	if st.Draft != (resume.Draft{Kind: resume.KindCapture, Text: "hi", Slug: "work"}) || st.View.Project != "work" {
		t.Errorf("saved state = %+v", st)
	}
	if m.res.ticking {
		t.Error("tick still marked pending after it fired")
	}
}

func TestCommittedCaptureDropsDraftAtOnce(t *testing.T) {
	m := settle(press(t, newResumeModel(t), runeKey('c'), runeKey('x')))
	m = press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.mode != modeList {
		t.Fatalf("mode = %v after enter", m.mode)
	}
	if d := saved(t, m).Draft; !d.IsZero() {
		t.Errorf("draft still on disk right after commit: %+v", d)
	}
}

func TestEditDraftFollowsEditedTask(t *testing.T) {
	m := press(t, newResumeModel(t), runeKey('e'))
	if d := m.currentDraft(); !d.IsZero() {
		t.Fatalf("unchanged edit is a draft: %+v", d)
	}
	task := m.tasks[m.cursor]
	m = press(t, m, runeKey('!'))
	m.cursor = 0 // a watcher reload moved the cursor
	want := resume.Draft{Kind: resume.KindEdit, Text: task.Description + "!", Slug: "work", TaskID: task.ID}
	if d := m.currentDraft(); d != want {
		t.Fatalf("draft = %+v, want %+v", d, want)
	}
	m = press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if i := indexByIDSlug(m.tasks, task.ID, "work", true); i < 0 || m.tasks[i].Description != want.Text {
		t.Errorf("edit not committed to the task it opened on")
	}
}

func TestResumeRestoresViewAndDraft(t *testing.T) {
	m := newResumeModel(t)
	target := m.tasks[0]
	m = press(t, m, runeKey('c'), runeKey('h'), runeKey('i'))
	m.globalView, m.sortKey, m.searchQuery = true, sortByCreated, "task"
	m.moveCursorTo(target.ID)
	path := abandon(t, m)

	r := sibling(t, m).applyOptions(readStateOptions(Options{State: path}))
	if !r.globalView || r.sortKey != sortByCreated || r.searchQuery != "task" || r.tasks[r.cursor].ID != target.ID {
		t.Errorf("view: global = %v, sort = %v, query = %q, cursor on %s", r.globalView, r.sortKey, r.searchQuery, r.tasks[r.cursor].ID)
	}
	if r.mode != modeCapture || r.capArea.Value() != "hi" {
		t.Errorf("draft: mode = %v, text = %q", r.mode, r.capArea.Value())
	}
	if r.status != "" {
		t.Errorf("resume showed the recovery hint %q", r.status)
	}
	if _, ok := resume.Read(path); ok {
		t.Error("resumed state left at its old path")
	}
	if d := saved(t, r).Draft; d.Text != "hi" {
		t.Errorf("draft not saved under the new instance: %+v", d)
	}
}

func TestResumeDoesNotTakeAnotherTabsDraft(t *testing.T) {
	m := newResumeModel(t)
	withDraft := abandon(t, settle(press(t, m, runeKey('c'), runeKey('x'))))
	plain := abandon(t, settle(press(t, sibling(t, m), runeKey('k'))))

	r := sibling(t, m).applyOptions(readStateOptions(Options{State: plain}))
	if r.mode != modeList {
		t.Errorf("draft-free resume opened mode %v", r.mode)
	}
	if _, ok := resume.Read(withDraft); !ok {
		t.Error("the other tab's draft was taken")
	}
}

func TestLaunchRecoversOrphanedCapture(t *testing.T) {
	m := newResumeModel(t)
	home := press(t, sibling(t, m), runeKey('c'))
	home.capTargetSlug = "home"
	other := abandon(t, settle(press(t, home, runeKey('h'))))
	abandon(t, settle(press(t, m, runeKey('c'), runeKey('l'), runeKey('o'), runeKey('n'), runeKey('g'))))

	r := sibling(t, m).applyOptions(Options{})
	if r.mode != modeCapture || r.capArea.Value() != "long" || r.capTargetSlug != "" {
		t.Fatalf("mode = %v, text = %q, target = %q", r.mode, r.capArea.Value(), r.capTargetSlug)
	}
	if r.status == "" {
		t.Error("no hint for the recovered draft")
	}
	if st, ok := resume.Read(other); !ok || st.Draft.Slug != "home" {
		t.Errorf("another project's draft was touched: %+v, %v", st, ok)
	}
	if r = press(t, r, runeKey('!')); r.status != "" {
		t.Errorf("hint %q survived typing", r.status)
	}
}

func TestResumedEditSurvivesFilter(t *testing.T) {
	m := press(t, newResumeModel(t), runeKey('e'))
	task := m.tasks[m.cursor]
	m = settle(press(t, m, runeKey('!')))
	m.searchQuery = "nomatch"
	m.res.pending = m.currentState()
	if err := m.flushState(); err != nil {
		t.Fatal(err)
	}
	path := abandon(t, m)

	r := sibling(t, m).applyOptions(readStateOptions(Options{State: path}))
	if r.mode != modeEdit || r.editID != task.ID || r.input.Value() != task.Description+"!" {
		t.Fatalf("mode = %v, editing %s, text = %q", r.mode, r.editID, r.input.Value())
	}
	if r.searchQuery != "" {
		t.Errorf("filter %q kept over the edit", r.searchQuery)
	}
}

func TestLaunchRecoversOrphanedEdit(t *testing.T) {
	m := press(t, newResumeModel(t), runeKey('e'))
	task := m.tasks[m.cursor]
	abandon(t, settle(press(t, m, runeKey('!'))))

	r := sibling(t, m).applyOptions(Options{})
	if r.mode != modeEdit || r.editID != task.ID || r.input.Value() != task.Description+"!" {
		t.Fatalf("mode = %v, editing %s, text = %q", r.mode, r.editID, r.input.Value())
	}
}

func TestLaunchSkipsLiveDraft(t *testing.T) {
	m := newResumeModel(t)
	settle(press(t, m, runeKey('c'), runeKey('x')))
	if r := sibling(t, m).applyOptions(Options{}); r.mode != modeList {
		t.Errorf("claimed a running instance's draft: mode %v", r.mode)
	}
}

func TestApplyOptionsDropsStaleValues(t *testing.T) {
	m := newResumeModel(t)
	cursor := m.cursor
	m = m.applyOptions(readStateOptions(Options{Cursor: "01AAAAAAAAAAAAAAAAAAAAAAAA", State: "/nonexistent/x.json"}))
	if m.cursor != cursor || m.mode != modeList {
		t.Errorf("cursor = %d, mode = %v; want %d, modeList", m.cursor, m.mode, cursor)
	}
}

func TestRestoredCursorLeavesNewestUnread(t *testing.T) {
	m := newResumeModel(t)
	newest, older := m.tasks[m.cursor].ID, m.tasks[0].ID
	m = m.applyOptions(Options{Cursor: older})
	if m.readIDs[newest] {
		t.Error("newest task marked read though the cursor never reached it")
	}
	if !m.readIDs[older] {
		t.Error("restored cursor task not marked read")
	}
}

func TestLaunchRecoversGlobalViewCaptureIntoOtherProject(t *testing.T) {
	m := newResumeModel(t)
	m = press(t, m, runeKey('c'))
	m.capTargetSlug = "home"
	m.globalView = true
	abandon(t, settle(press(t, m, runeKey('x'))))

	r := sibling(t, m).applyOptions(Options{})
	if r.mode != modeCapture || r.capTargetSlug != "home" || r.capArea.Value() != "x" || !r.globalView {
		t.Fatalf("mode = %v, target = %q, text = %q, global = %v", r.mode, r.capTargetSlug, r.capArea.Value(), r.globalView)
	}
}

func TestStateFileLocationPicksDataRoot(t *testing.T) {
	m := newResumeModel(t)
	path := abandon(t, m)
	want, err := storage.Root()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(storage.EnvRoot, t.TempDir())
	readStateOptions(Options{State: path})
	if got := os.Getenv(storage.EnvRoot); got != want {
		t.Errorf("DFC_ROOT = %q, want %q", got, want)
	}
}

func TestExternalRenameIsNotAnEditDraft(t *testing.T) {
	m := press(t, newResumeModel(t), runeKey('e'))
	i := m.cursor
	m.tasks[i].Description = "renamed elsewhere" // a watcher reload
	if d := m.currentDraft(); !d.IsZero() {
		t.Errorf("untouched editor saved as a draft: %+v", d)
	}
}

func TestEditCommitsWhenTaskLeftTheFilter(t *testing.T) {
	m := press(t, newResumeModel(t), runeKey('e'))
	task := m.tasks[m.cursor]
	m = press(t, m, runeKey('!'))
	m.tasks = nil // a reload filtered it out
	m = press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.err != nil || m.mode != modeList {
		t.Fatalf("err = %v, mode = %v", m.err, m.mode)
	}
	path, err := m.core.FindByIDInProject(task.ID, "work")
	if err != nil || path == "" {
		t.Fatal("task gone")
	}
	store, _ := m.core.StoreFor("work")
	got, err := store.Load(path)
	if err != nil || got.Description != task.Description+"!" {
		t.Errorf("description = %q, %v", got.Description, err)
	}
}

func TestEditOfDeletedTaskKeepsEditorOpen(t *testing.T) {
	m := press(t, newResumeModel(t), runeKey('e'), runeKey('!'))
	m.editID = "01AAAAAAAAAAAAAAAAAAAAAAAA"
	m.tasks = nil
	m = press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.mode != modeEdit || m.err == nil {
		t.Errorf("mode = %v, err = %v; want the editor kept with an error", m.mode, m.err)
	}
}

func TestRecoverySkipsEditOfDeletedTask(t *testing.T) {
	m := newResumeModel(t)
	abandon(t, settle(press(t, sibling(t, m), runeKey('c'), runeKey('o'), runeKey('k'))))
	e := press(t, sibling(t, m), runeKey('e'), runeKey('!'))
	e.editID = "01AAAAAAAAAAAAAAAAAAAAAAAA"
	abandon(t, e)

	r := sibling(t, m).applyOptions(Options{})
	if r.mode != modeCapture || r.capArea.Value() != "ok" {
		t.Errorf("mode = %v, text = %q; want the capture recovered", r.mode, r.capArea.Value())
	}
}

func TestDraftlessExitWithoutResumeLeavesNothing(t *testing.T) {
	m := newResumeModel(t)
	t.Setenv(ansi.ProgramResumeTokenEnv, "")
	path := abandon(t, settle(press(t, m, runeKey('k'))))
	if _, ok := resume.Read(path); ok {
		t.Error("state file kept with no draft and no resume support")
	}

	m = sibling(t, m)
	t.Setenv(ansi.ProgramResumeTokenEnv, "")
	path = abandon(t, settle(press(t, m, runeKey('c'), runeKey('x'))))
	if st, ok := resume.Read(path); !ok || st.Draft.Text != "x" {
		t.Errorf("draft not kept without resume support: %+v, %v", st, ok)
	}
}

func TestLaunchRecoversDraftTypedAfterSwitch(t *testing.T) {
	m := newResumeModel(t)
	m.res.launch = "work"
	if err := m.switchTo("home"); err != nil {
		t.Fatal(err)
	}
	abandon(t, settle(press(t, m, runeKey('c'), runeKey('x'))))

	r := withResume(t, sibling(t, m))
	r = r.applyOptions(Options{})
	if r.mode != modeCapture || r.capArea.Value() != "x" || r.capTargetSlug != "home" {
		t.Errorf("mode = %v, text = %q, target = %q", r.mode, r.capArea.Value(), r.capTargetSlug)
	}
}

func TestResumedEditOfMovedTaskKeepsText(t *testing.T) {
	m := press(t, newResumeModel(t), runeKey('e'), runeKey('!'))
	m.editID = "01AAAAAAAAAAAAAAAAAAAAAAAA"
	path := abandon(t, m)
	r := sibling(t, m).applyOptions(readStateOptions(Options{State: path}))
	if r.mode != modeCapture || r.capArea.Value() == "" || r.status == "" {
		t.Errorf("mode = %v, text = %q, status = %q", r.mode, r.capArea.Value(), r.status)
	}
}
