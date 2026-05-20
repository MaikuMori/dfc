package ui

import (
	"sort"

	"github.com/MaikuMori/dfc/internal/project"
	"github.com/MaikuMori/dfc/internal/storage"
)

// sortDoneFirst returns a new slice with done tasks at the top and open tasks
// below, each zone sorted by filesystem modification time ascending (most
// recently touched lands at the bottom of its zone).
func sortDoneFirst(tasks []storage.Task) []storage.Task {
	out := make([]storage.Task, len(tasks))
	copy(out, tasks)
	sort.SliceStable(out, func(i, j int) bool {
		di := out[i].Status == storage.StatusDone
		dj := out[j].Status == storage.StatusDone
		if di != dj {
			return di // done zone before open zone
		}
		return out[i].Modified.Before(out[j].Modified)
	})
	return out
}

// indexByID returns the index of the task with the given id, or -1.
func indexByID(tasks []storage.Task, id string) int {
	for i, t := range tasks {
		if t.ID == id {
			return i
		}
	}
	return -1
}

// indexByIDSlug locates a task by id, optionally also matching ProjectSlug
// (when slugQualified is true — used in global view to defend against a
// theoretical ULID collision across projects).
func indexByIDSlug(tasks []storage.Task, id, slug string, slugQualified bool) int {
	for i, t := range tasks {
		if t.ID != id {
			continue
		}
		if slugQualified && t.ProjectSlug != slug {
			continue
		}
		return i
	}
	return -1
}

// slugSetSnapshot returns a deduplicated set of slugs from the registry.
func slugSetSnapshot(reg *project.Registry) map[string]bool {
	out := map[string]bool{}
	if reg == nil {
		return out
	}
	for _, s := range reg.Slugs() {
		out[s] = true
	}
	return out
}

// equalStringSets reports whether two string sets contain the same keys.
func equalStringSets(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}
