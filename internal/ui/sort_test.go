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
	out := sortDoneFirst(in)

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
	out := sortDoneFirst(in)
	if out[0].ID != "x" || out[1].ID != "y" {
		t.Errorf("ordering wrong: %+v", out)
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
