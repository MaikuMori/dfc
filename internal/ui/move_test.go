package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/MaikuMori/dfc/internal/core"
	"github.com/MaikuMori/dfc/internal/storage"
)

func moveKey() tea.KeyPressMsg { return tea.KeyPressMsg{Code: 'm', Text: "m"} }

// newPerProjectMoveModel builds a per-project-view model: project "work" with
// three tasks (the one being viewed) plus a registered destination "home".
func newPerProjectMoveModel(t *testing.T) Model {
	t.Helper()
	root := t.TempDir()
	t.Setenv(storage.EnvRoot, root)
	cr, err := core.Open(core.Options{Warn: func(string, error) {}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cr.Close() })
	for _, d := range []string{"task one", "task two", "task three"} {
		if _, err := cr.Capture(core.CaptureInput{Slug: "work", Description: d}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := cr.Capture(core.CaptureInput{Slug: "home", Description: "elsewhere"}); err != nil {
		t.Fatal(err)
	}
	store, _ := cr.StoreFor("work")
	tasks, _ := store.List()
	m := New(cr, "work", store, tasks, nil)
	m.width, m.height = 80, 24
	m.relayout()
	return m
}

func commitMove(t *testing.T, m Model) Model {
	t.Helper()
	model, _ := m.updateList(moveKey())
	m = model.(Model)
	if m.mode != modeMoveTarget {
		t.Fatalf("opener: mode = %v, want modeMoveTarget", m.mode)
	}
	model, _ = m.updateMoveTarget(tea.KeyPressMsg{Code: tea.KeyEnter})
	return model.(Model)
}

func TestMovePerProjectCursorStaysPut(t *testing.T) {
	m := newPerProjectMoveModel(t)
	m.cursor = 1 // middle task
	src := m.tasks[1]

	got := commitMove(t, m)
	if got.mode != modeList {
		t.Fatalf("mode = %v after commit", got.mode)
	}
	if len(got.tasks) != 2 {
		t.Fatalf("work should have 2 tasks after move-out, got %d", len(got.tasks))
	}
	if got.cursor != 1 {
		t.Errorf("cursor = %d, want 1 (stay put at the old slot)", got.cursor)
	}
	for _, tk := range got.tasks {
		if tk.ID == src.ID {
			t.Errorf("moved task %s should have left the work list", src.ID)
		}
	}
}

func TestMovePerProjectLastTaskClamps(t *testing.T) {
	m := newPerProjectMoveModel(t)
	m.cursor = len(m.tasks) - 1 // last task; srcIndex falls out of range post-shrink

	got := commitMove(t, m)
	if got.cursor != len(got.tasks)-1 {
		t.Errorf("cursor = %d, want %d (clamped to last)", got.cursor, len(got.tasks)-1)
	}
	if got.cursor < 0 || got.cursor >= len(got.tasks) {
		t.Errorf("cursor %d out of bounds (len %d)", got.cursor, len(got.tasks))
	}
}

func TestMoveSourceGoneBailsClean(t *testing.T) {
	m := newTagModel(t)
	model, _ := m.updateList(moveKey())
	m = model.(Model)
	if m.mode != modeMoveTarget {
		t.Fatal("precondition: not in move mode")
	}
	m.tasks = nil // source vanishes out-of-band while the picker is open

	model, _ = m.updateMoveTarget(tea.KeyPressMsg{Code: tea.KeyEnter})
	got := model.(Model)
	if got.mode != modeList {
		t.Errorf("mode = %v, want modeList", got.mode)
	}
	if got.err != nil {
		t.Errorf("clean bail should not set err, got %v", got.err)
	}
	if got.moveSrcID != "" {
		t.Errorf("moveSrc not cleared on bail")
	}
}

func TestMoveOpenerSnapshotsSource(t *testing.T) {
	m := newTagModel(t) // global view, 3 projects, cursor on a task
	src := m.tasks[m.cursor]

	model, _ := m.updateList(moveKey())
	got := model.(Model)

	if got.mode != modeMoveTarget {
		t.Fatalf("mode = %v, want modeMoveTarget", got.mode)
	}
	if got.moveSrcID != src.ID || got.moveSrcSlug != src.ProjectSlug {
		t.Errorf("snapshot = (%q,%q), want (%q,%q)", got.moveSrcID, got.moveSrcSlug, src.ID, src.ProjectSlug)
	}
	if got.moveSrcIndex != m.cursor {
		t.Errorf("moveSrcIndex = %d, want %d", got.moveSrcIndex, m.cursor)
	}
}

func TestMoveOpenerSingleProjectBails(t *testing.T) {
	root := t.TempDir()
	t.Setenv(storage.EnvRoot, root)
	cr, err := core.Open(core.Options{Warn: func(string, error) {}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cr.Close() })
	if _, err := cr.Capture(core.CaptureInput{Slug: "solo", Description: "only", DisplayName: "solo"}); err != nil {
		t.Fatal(err)
	}
	store, _ := cr.StoreFor("solo")
	all, _ := cr.ListAll()
	m := New(cr, "solo", store, all, nil)
	m.width, m.height = 80, 24
	m.relayout()

	model, _ := m.updateList(moveKey())
	got := model.(Model)

	if got.mode != modeList {
		t.Errorf("single-project: mode = %v, want modeList (bail)", got.mode)
	}
	if got.moveSrcID != "" {
		t.Errorf("moveSrcID should stay empty on bail, got %q", got.moveSrcID)
	}
	if got.status == "" {
		t.Errorf("expected a 'no other project' status hint")
	}
}

func TestMoveTargetCancelClearsState(t *testing.T) {
	m := newTagModel(t)
	model, _ := m.updateList(moveKey())
	m = model.(Model)
	if m.mode != modeMoveTarget {
		t.Fatalf("precondition: mode = %v, want modeMoveTarget", m.mode)
	}

	model, _ = m.updateMoveTarget(tea.KeyPressMsg{Code: tea.KeyEscape})
	got := model.(Model)
	if got.mode != modeList {
		t.Errorf("mode = %v, want modeList after cancel", got.mode)
	}
	if got.moveSrcID != "" {
		t.Errorf("moveSrcID = %q, want cleared after cancel", got.moveSrcID)
	}
}

func TestMoveCommitMovesTask(t *testing.T) {
	m := newTagModel(t)
	src := m.tasks[m.cursor]
	srcSlug := src.ProjectSlug

	model, _ := m.updateList(moveKey())
	m = model.(Model)
	// The move picker opens on its first target; Enter commits it.
	model, _ = m.updateMoveTarget(tea.KeyPressMsg{Code: tea.KeyEnter})
	got := model.(Model)

	if got.mode != modeList {
		t.Fatalf("mode = %v, want modeList after commit", got.mode)
	}
	moved, err := got.core.Show(src.ID)
	if err != nil {
		t.Fatalf("Show(%s): %v", src.ID, err)
	}
	if moved.ProjectSlug == srcSlug {
		t.Errorf("task should have moved off %q, still there", srcSlug)
	}
	// Global view: the cursor follows the re-tagged task to its new home.
	if got.cursor >= len(got.tasks) || got.tasks[got.cursor].ID != src.ID || got.tasks[got.cursor].ProjectSlug == srcSlug {
		t.Errorf("cursor should follow the moved task, landed on %+v", got.tasks[got.cursor])
	}
}
