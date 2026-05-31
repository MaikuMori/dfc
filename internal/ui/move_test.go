package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/MaikuMori/dfc/internal/core"
	"github.com/MaikuMori/dfc/internal/storage"
)

func moveKey() tea.KeyPressMsg { return tea.KeyPressMsg{Code: 'm', Text: "m"} }

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
}
