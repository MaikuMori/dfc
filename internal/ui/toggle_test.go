package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/MaikuMori/dfc/internal/storage"
)

func spaceKey() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeySpace} }

func TestToggleDoneCollapsesExpandedCursor(t *testing.T) {
	m := newPerProjectMoveModel(t)
	m.cursor = 1
	target := m.tasks[1]
	m.expandedID = target.ID
	m.expandedSlug = target.ProjectSlug

	model, _ := m.updateList(spaceKey())
	got := model.(Model)

	marked, err := got.core.Show(target.ID)
	if err != nil {
		t.Fatalf("Show(%s): %v", target.ID, err)
	}
	if marked.Status != storage.StatusDone {
		t.Fatalf("task status = %v, want done", marked.Status)
	}
	if got.expandedID != "" {
		t.Errorf("expandedID = %q, want cleared once the task is done", got.expandedID)
	}
	if got.expandedSlug != "" {
		t.Errorf("expandedSlug = %q, want cleared once the task is done", got.expandedSlug)
	}
}

func TestToggleDoneLeavesOtherExpansion(t *testing.T) {
	m := newPerProjectMoveModel(t)
	m.cursor = 0
	other := m.tasks[2]
	m.expandedID = other.ID
	m.expandedSlug = other.ProjectSlug

	model, _ := m.updateList(spaceKey())
	got := model.(Model)

	if got.expandedID != other.ID {
		t.Errorf("expandedID = %q, want %q (an unrelated expansion must survive)", got.expandedID, other.ID)
	}
}

func TestToggleReopenDoesNotExpand(t *testing.T) {
	m := newPerProjectMoveModel(t)
	m.cursor = 0
	target := m.tasks[0]
	target.Status = storage.StatusDone
	if err := m.core.SaveTask(&target); err != nil {
		t.Fatal(err)
	}
	m.tasks = sortDoneFirst(replaceByID(m.tasks, target), m.sortKey)
	m.cursor = indexByIDSlug(m.tasks, target.ID, target.ProjectSlug, m.globalView)

	model, _ := m.updateList(spaceKey())
	got := model.(Model)

	reopened, err := got.core.Show(target.ID)
	if err != nil {
		t.Fatalf("Show(%s): %v", target.ID, err)
	}
	if reopened.Status != storage.StatusOpen {
		t.Fatalf("task status = %v, want open", reopened.Status)
	}
	if got.expandedID != "" {
		t.Errorf("expandedID = %q, want empty (reopening never expands)", got.expandedID)
	}
}
