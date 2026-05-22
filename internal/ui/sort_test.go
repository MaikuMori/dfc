package ui

import (
	"testing"
	"time"

	"github.com/MaikuMori/dfc/internal/storage"
)

func TestSortDoneFirst(t *testing.T) {
	base := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	mk := func(id string, status storage.Status, offset time.Duration) storage.Task {
		return storage.Task{ID: id, Status: status, Modified: base.Add(offset)}
	}
	in := []storage.Task{
		mk("c", storage.StatusOpen, 2*time.Second),
		mk("a", storage.StatusDone, 0),
		mk("d", storage.StatusOpen, 3*time.Second),
		mk("b", storage.StatusDone, time.Second),
	}
	out := sortDoneFirst(in, sortByModified)

	wantOrder := []string{"a", "b", "c", "d"}
	for i, id := range wantOrder {
		if out[i].ID != id {
			t.Errorf("position %d: got %s, want %s (full: %+v)", i, out[i].ID, id, out)
		}
	}
}

func TestSortDoneFirst_AllOpen(t *testing.T) {
	base := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	in := []storage.Task{
		{ID: "y", Status: storage.StatusOpen, Modified: base.Add(time.Second)},
		{ID: "x", Status: storage.StatusOpen, Modified: base},
	}
	out := sortDoneFirst(in, sortByModified)
	if out[0].ID != "x" || out[1].ID != "y" {
		t.Errorf("ordering wrong: %+v", out)
	}
}

func TestSortDoneFirst_ByCreated(t *testing.T) {
	base := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	// Diverge created vs modified intentionally so the two keys produce
	// different orderings.
	in := []storage.Task{
		{ID: "y", Status: storage.StatusOpen, Created: base.Add(2 * time.Second), Modified: base},
		{ID: "x", Status: storage.StatusOpen, Created: base, Modified: base.Add(2 * time.Second)},
	}
	out := sortDoneFirst(in, sortByCreated)
	if out[0].ID != "x" || out[1].ID != "y" {
		t.Errorf("sortByCreated ordering wrong: %+v", out)
	}
	out = sortDoneFirst(in, sortByModified)
	if out[0].ID != "y" || out[1].ID != "x" {
		t.Errorf("sortByModified should disagree with created here: %+v", out)
	}
}

func TestSortKeyCycleAndLabel(t *testing.T) {
	if (sortByModified).String() != "modified" || (sortByCreated).String() != "created" {
		t.Errorf("labels: %q / %q", sortByModified, sortByCreated)
	}
	if (sortByModified).next() != sortByCreated || (sortByCreated).next() != sortByModified {
		t.Errorf("cycle should toggle: %v %v", sortByModified.next(), sortByCreated.next())
	}
}

func TestIndexByID(t *testing.T) {
	tasks := []storage.Task{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	if indexByID(tasks, "b") != 1 {
		t.Error("expected 1")
	}
	if indexByID(tasks, "z") != -1 {
		t.Error("expected -1 for missing")
	}
}
