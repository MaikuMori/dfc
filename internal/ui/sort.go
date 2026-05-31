package ui

import (
	"slices"
	"time"

	"github.com/MaikuMori/dfc/internal/project"
	"github.com/MaikuMori/dfc/internal/storage"
)

// sortKey selects which timestamp drives within-zone ordering.
//
//   - sortByModified: filesystem mtime — picks up external edits.
//   - sortByCreated:  frontmatter `created` — capture-order stable.
type sortKey int

const (
	sortByModified sortKey = iota
	sortByCreated
)

// String returns the lowercase label used in transient status hints.
func (k sortKey) String() string {
	if k == sortByCreated {
		return "created"
	}
	return "modified"
}

// next returns the next key in the cycle.
func (k sortKey) next() sortKey {
	if k == sortByModified {
		return sortByCreated
	}
	return sortByModified
}

// sortDoneFirst returns a new slice with done tasks at the top and open tasks
// below, each zone sorted by the chosen timestamp ascending (newest lands at
// the bottom of its zone, flush against the input).
func sortDoneFirst(tasks []storage.Task, key sortKey) []storage.Task {
	out := make([]storage.Task, len(tasks))
	copy(out, tasks)
	slices.SortStableFunc(out, func(a, b storage.Task) int {
		da := a.Status == storage.StatusDone
		db := b.Status == storage.StatusDone
		if da != db {
			if da {
				return -1 // done zone before open zone
			}
			return 1
		}
		return timeForKey(a, key).Compare(timeForKey(b, key))
	})
	return out
}

// timeForKey returns the timestamp used to order t under the chosen key.
func timeForKey(t storage.Task, key sortKey) time.Time {
	if key == sortByCreated {
		return t.Created
	}
	return t.Modified
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
