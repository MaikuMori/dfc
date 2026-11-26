package ui

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/MaikuMori/dfc/internal/resume"
)

func runeKey(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Text: string(r)} }

// newResumeModel is the per-project "work" model with resume state whose
// draft file lives under a temp dir.
func newResumeModel(t *testing.T) Model {
	t.Helper()
	m := newPerProjectMoveModel(t)
	m.res = &resumeState{exe: "/usr/local/bin/dfc", dir: "/src/work", draftPath: resume.NewPath(t.TempDir())}
	return m
}

func press(t *testing.T, m Model, keys ...tea.KeyPressMsg) Model {
	t.Helper()
	for _, k := range keys {
		next, _ := m.Update(k)
		m = next.(Model)
	}
	return m
}

// settle delivers the pending debounced draft save.
func settle(m Model) Model {
	next, _ := m.Update(draftSaveMsg{gen: m.res.gen})
	return next.(Model)
}

func TestProgramResumeArgs(t *testing.T) {
	m := newResumeModel(t)
	pr := m.programResume()
	if pr == nil {
		t.Fatal("programResume = nil")
	}
	want := []string{"/usr/local/bin/dfc", "tui", "--project=work", "--cursor=" + m.tasks[m.cursor].ID}
	if !slices.Equal(pr.Args, want) {
		t.Errorf("Args = %q, want %q", pr.Args, want)
	}
	if pr.Dir != "/src/work" {
		t.Errorf("Dir = %q", pr.Dir)
	}

	m.globalView = true
	m.searchQuery = "-#later"
	m.sortKey = sortByCreated
	m.tasks = nil
	want = []string{"/usr/local/bin/dfc", "tui", "--project=work", "--global", "--query=-#later", "--sort=created"}
	if got := m.programResume().Args; !slices.Equal(got, want) {
		t.Errorf("Args = %q, want %q", got, want)
	}
}

func TestProgramResumeNilWithoutState(t *testing.T) {
	m := newPerProjectMoveModel(t)
	if pr := m.programResume(); pr != nil {
		t.Errorf("programResume = %+v, want nil", pr)
	}
}

func TestQuitClearsResume(t *testing.T) {
	m := press(t, newResumeModel(t), runeKey('q'))
	if !m.quitting {
		t.Fatal("q did not mark quitting")
	}
	if pr := m.View().ProgramResume; pr != nil {
		t.Errorf("last frame ProgramResume = %+v, want nil", pr)
	}
}

func TestCaptureDraftSavedAndRestored(t *testing.T) {
	m := press(t, newResumeModel(t), runeKey('c'), runeKey('h'), runeKey('i'))
	if m.mode != modeCapture {
		t.Fatalf("mode = %v, want modeCapture", m.mode)
	}
	path := m.res.draftPath
	if d := resume.Load(path); !d.IsZero() {
		t.Fatalf("draft written before the debounce: %+v", d)
	}
	if !slices.Contains(m.programResume().Args, "--draft="+path) {
		t.Errorf("Args %q lack --draft", m.programResume().Args)
	}

	settle(m)
	want := resume.Draft{Kind: resume.KindCapture, Text: "hi", Slug: "work"}
	if d := resume.Load(path); d != want {
		t.Fatalf("saved draft = %+v, want %+v", d, want)
	}

	r := newResumeModel(t)
	r = r.applyOptions(Options{Draft: path})
	if r.mode != modeCapture || r.capArea.Value() != "hi" {
		t.Fatalf("restored mode = %v, text = %q", r.mode, r.capArea.Value())
	}
	if d := resume.Load(r.res.draftPath); d != want {
		t.Errorf("restored draft not saved under the new instance: %+v", d)
	}
	if d := resume.Load(path); !d.IsZero() {
		t.Errorf("resumed draft left at its old path: %+v", d)
	}
}

func TestCommittedCaptureRemovesDraft(t *testing.T) {
	m := settle(press(t, newResumeModel(t), runeKey('c'), runeKey('x')))
	path := m.res.draftPath
	m = settle(press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter}))
	if m.mode != modeList {
		t.Fatalf("mode = %v after enter", m.mode)
	}
	if d := resume.Load(path); !d.IsZero() {
		t.Errorf("draft survived commit: %+v", d)
	}
}

func TestEditDraftOnlyWhenChanged(t *testing.T) {
	m := press(t, newResumeModel(t), runeKey('e'))
	if m.mode != modeEdit {
		t.Fatalf("mode = %v, want modeEdit", m.mode)
	}
	if d := m.currentDraft(); !d.IsZero() {
		t.Fatalf("unchanged edit is a draft: %+v", d)
	}
	task := m.tasks[m.cursor]
	m = settle(press(t, m, runeKey('!')))
	want := resume.Draft{Kind: resume.KindEdit, Text: task.Description + "!", Slug: "work", TaskID: task.ID}
	if d := resume.Load(m.res.draftPath); d != want {
		t.Fatalf("saved draft = %+v, want %+v", d, want)
	}

	r := m
	r.exitInput()
	r.cursor = 0
	r.res = &resumeState{draftPath: resume.NewPath(t.TempDir())}
	r = r.applyOptions(Options{Draft: m.res.draftPath})
	if r.mode != modeEdit || r.tasks[r.cursor].ID != task.ID || r.input.Value() != want.Text {
		t.Errorf("restored mode = %v, task = %s, text = %q", r.mode, r.tasks[r.cursor].ID, r.input.Value())
	}
}

func TestQuitDropsDraft(t *testing.T) {
	m := settle(press(t, newResumeModel(t), runeKey('c'), runeKey('x')))
	path := m.res.draftPath
	m = press(t, m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if !m.quitting {
		t.Fatal("ctrl+c did not mark quitting")
	}
	if err := m.finishDraft(); err != nil {
		t.Fatal(err)
	}
	if d := resume.Load(path); !d.IsZero() {
		t.Errorf("draft survived quit: %+v", d)
	}
}

func TestUnsavedDraftFlushedOnExit(t *testing.T) {
	m := press(t, newResumeModel(t), runeKey('c'), runeKey('x'))
	if err := m.finishDraft(); err != nil {
		t.Fatal(err)
	}
	if d := resume.Load(m.res.draftPath); d.Text != "x" {
		t.Errorf("draft = %+v, want text x", d)
	}
}

func TestApplyOptionsDropsStaleValues(t *testing.T) {
	m := newResumeModel(t)
	cursor := m.cursor
	m = m.applyOptions(Options{
		Cursor: "01AAAAAAAAAAAAAAAAAAAAAAAA",
		Draft:  filepath.Join(t.TempDir(), "missing.json"),
	})
	if m.cursor != cursor || m.mode != modeList {
		t.Errorf("cursor = %d, mode = %v; want %d, modeList", m.cursor, m.mode, cursor)
	}

	path := resume.NewPath(t.TempDir())
	if err := resume.Save(path, resume.Draft{Kind: resume.KindEdit, Text: "x", TaskID: "01AAAAAAAAAAAAAAAAAAAAAAAA"}); err != nil {
		t.Fatal(err)
	}
	if m = m.applyOptions(Options{Draft: path}); m.mode != modeList {
		t.Errorf("edit draft for a vanished task opened mode %v", m.mode)
	}
}

func TestApplyOptionsRestoresView(t *testing.T) {
	m := newResumeModel(t)
	target := m.tasks[0]
	m = m.applyOptions(Options{Global: true, Sort: "created", Query: "task", Cursor: target.ID})
	if !m.globalView || m.sortKey != sortByCreated || m.searchQuery != "task" {
		t.Fatalf("global = %v, sort = %v, query = %q", m.globalView, m.sortKey, m.searchQuery)
	}
	if m.tasks[m.cursor].ID != target.ID {
		t.Errorf("cursor on %s, want %s", m.tasks[m.cursor].ID, target.ID)
	}
}

// writeOrphan leaves a draft owned by a process that has exited, as a killed
// dfc would.
func writeOrphan(t *testing.T, m Model, d resume.Draft) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^$")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	host, _ := os.Hostname()
	data, err := json.Marshal(map[string]any{
		"kind": d.Kind, "text": d.Text, "slug": d.Slug, "task_id": d.TaskID,
		"pid": cmd.Process.Pid, "host": host,
	})
	if err != nil {
		t.Fatal(err)
	}
	path := resume.NewPath(filepath.Dir(m.res.draftPath))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLaunchRecoversOrphanedCapture(t *testing.T) {
	m := newResumeModel(t)
	other := writeOrphan(t, m, resume.Draft{Kind: resume.KindCapture, Text: "for home", Slug: "home"})
	mine := writeOrphan(t, m, resume.Draft{Kind: resume.KindCapture, Text: "long task\n\nbody", Slug: "work"})

	m = m.applyOptions(Options{})
	if m.mode != modeCapture || m.capArea.Value() != "long task\n\nbody" || m.capTargetSlug != "" {
		t.Fatalf("mode = %v, text = %q, target = %q", m.mode, m.capArea.Value(), m.capTargetSlug)
	}
	if m.status == "" {
		t.Error("no status hint for the recovered draft")
	}
	if _, err := os.Stat(mine); !os.IsNotExist(err) {
		t.Error("recovered draft left at its old path")
	}
	if d := resume.Load(m.res.draftPath); d.Text != "long task\n\nbody" {
		t.Errorf("recovered draft not owned by this instance: %+v", d)
	}
	if d := resume.Load(other); d.Text != "for home" {
		t.Errorf("another project's draft was touched: %+v", d)
	}

	m = press(t, m, runeKey('!'))
	if m.status != "" {
		t.Errorf("status %q survived typing", m.status)
	}
}

func TestLaunchRecoversOrphanedEdit(t *testing.T) {
	m := newResumeModel(t)
	task := m.tasks[0]
	writeOrphan(t, m, resume.Draft{Kind: resume.KindEdit, Text: "renamed", Slug: "work", TaskID: task.ID})

	m = m.applyOptions(Options{})
	if m.mode != modeEdit || m.tasks[m.cursor].ID != task.ID || m.input.Value() != "renamed" {
		t.Fatalf("mode = %v, task = %s, text = %q", m.mode, m.tasks[m.cursor].ID, m.input.Value())
	}
}

func TestLaunchSkipsLiveDraft(t *testing.T) {
	m := newResumeModel(t)
	live := resume.NewPath(filepath.Dir(m.res.draftPath))
	if err := resume.Save(live, resume.Draft{Kind: resume.KindCapture, Text: "still typing", Slug: "work"}); err != nil {
		t.Fatal(err)
	}
	if m = m.applyOptions(Options{}); m.mode != modeList {
		t.Errorf("claimed a draft whose owner is running: mode %v", m.mode)
	}
}
