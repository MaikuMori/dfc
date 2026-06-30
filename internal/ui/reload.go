package ui

import (
	"errors"
	"strings"

	"github.com/MaikuMori/dfc/internal/core"
	"github.com/MaikuMori/dfc/internal/query"
	"github.com/MaikuMori/dfc/internal/storage"
)

// reloadAfterFS wraps reloadActive() with the extra hygiene needed after
// an unsolicited filesystem event: it clears expandedID when its task is
// gone and drops a stale error if the new list succeeded.
func (m Model) reloadAfterFS() Model {
	prevErr := m.err
	m = m.reloadActive()
	if m.expandedID != "" && indexByIDSlug(m.tasks, m.expandedID, m.expandedSlug, m.globalView) < 0 {
		m.expandedID = ""
		m.expandedSlug = ""
	}
	// If reloadActive() didn't surface a fresh error, clear any stale one
	// so the footer reflects the live state. errors.Is handles both
	// nil-to-nil and same-instance (or wrapped-same) equality.
	if errors.Is(m.err, prevErr) {
		m.err = nil
	}
	return m
}

// reloadActive picks the right reload strategy based on the current view.
// When a search filter is active it stays applied — callers that mutate
// state want the filtered view to refresh, not snap back to "show
// everything".
func (m Model) reloadActive() Model {
	if m.searchQuery != "" {
		return m.runSearch()
	}
	if m.globalView {
		return m.reloadAll()
	}
	return m.reload()
}

// runSearch applies the active query via the shared core.Query executor and
// replaces m.tasks. Scope follows the current view: per-project filters to
// m.slug, global searches every project. A text query comes back BM25-ranked
// and is reversed so the best hit sits at the bottom next to the input; a
// tags-only result is shown like the normal list (done-first, bottom-up).
func (m Model) runSearch() Model {
	if strings.TrimSpace(m.searchQuery) == "" {
		// No live query — bypass the search path and reload normally.
		if m.globalView {
			return m.reloadAll()
		}
		return m.reload()
	}

	scope := ""
	if !m.globalView {
		scope = m.slug
	}
	res, err := m.core.Query(m.searchQuery, core.QueryOpts{
		Project: scope,
		Status:  "all",
		SortBy:  "score",
		// 0 = no cap: the TUI list is scrollable, so show every match.
	})
	if err != nil {
		m.err = err
		return m
	}
	tasks := make([]storage.Task, len(res.Hits))
	for i, h := range res.Hits {
		tasks[i] = h.Task
	}
	if res.Ranked {
		// FTS score order: reverse so the best-scored hit lands at the bottom.
		for l, r := 0, len(tasks)-1; l < r; l, r = l+1, r-1 {
			tasks[l], tasks[r] = tasks[r], tasks[l]
		}
		return m.applySearchResults(tasks)
	}
	// Tags-only / listed: render like the normal list.
	return m.applyTaskListPreservingCursor(tasks)
}

// applySearchResults commits a filtered task list to the model. Unlike
// applyTaskListPreservingCursor (which re-applies sortDoneFirst), this
// keeps the caller's ordering and parks the cursor on the last row by
// default — i.e. the best-scored result in the bottom-up render. When
// the previously focused task survives the filter, the cursor follows
// it so refining the query feels stable.
func (m Model) applySearchResults(tasks []storage.Task) Model {
	prevID, prevSlug := "", ""
	if len(m.tasks) > 0 && m.cursor < len(m.tasks) {
		prevID = m.tasks[m.cursor].ID
		prevSlug = m.tasks[m.cursor].ProjectSlug
	}
	m.tasks = tasks
	if len(tasks) == 0 {
		m.cursor = 0
	} else {
		m.cursor = len(tasks) - 1
		if prevID != "" {
			if i := indexByIDSlug(tasks, prevID, prevSlug, m.globalView); i >= 0 {
				m.cursor = i
			}
		}
	}
	m.followCursor()
	return m
}

// reload re-reads the current project's store, preserving cursor on the
// same task ID if possible.
func (m Model) reload() Model {
	tasks, err := m.store.List()
	if err != nil {
		m.err = err
		return m
	}
	return m.applyTaskListPreservingCursor(tasks)
}

// explainQuery turns a filter query into a short plain-English description —
// e.g. `tagged #p3, not #later, matching "mig"` — so the footer can give live
// feedback that the query parsed. Empty when the query has no recognizable
// predicates, letting the caller fall back to a syntax hint.
func explainQuery(q string) string {
	tq := query.Split(q)
	var parts []string
	if len(tq.Include) > 0 {
		parts = append(parts, "tagged "+hashJoin(tq.Include))
	}
	if len(tq.Exclude) > 0 {
		parts = append(parts, "not "+hashJoin(tq.Exclude))
	}
	if terms := query.PositiveTerms(tq.Text); len(terms) > 0 {
		quoted := make([]string, len(terms))
		for i, t := range terms {
			quoted[i] = `"` + t + `"`
		}
		parts = append(parts, "matching "+strings.Join(quoted, " "))
	}
	return strings.Join(parts, ", ")
}

func hashJoin(tags []string) string {
	out := make([]string, len(tags))
	for i, t := range tags {
		out[i] = "#" + t
	}
	return strings.Join(out, " ")
}

// reloadAll lists every known project and merges into one sorted slice.
// Per-project errors are non-fatal (best-effort).
func (m Model) reloadAll() Model {
	all, err := m.core.ListAll()
	if err != nil {
		m.err = err
	}
	return m.applyTaskListPreservingCursor(all)
}

// applyTaskListPreservingCursor commits a new task list to the model,
// re-applies sorting, and tries to keep the cursor on the same task by ID
// (and slug, when globalView is on, to defend against any theoretical ULID
// collision across projects).
func (m Model) applyTaskListPreservingCursor(tasks []storage.Task) Model {
	currentID, currentSlug := "", ""
	if len(m.tasks) > 0 && m.cursor < len(m.tasks) {
		currentID = m.tasks[m.cursor].ID
		currentSlug = m.tasks[m.cursor].ProjectSlug
	}
	m.tasks = sortDoneFirst(tasks, m.sortKey)
	m.cursor = 0
	if currentID != "" {
		if i := indexByIDSlug(m.tasks, currentID, currentSlug, m.globalView); i >= 0 {
			m.cursor = i
		}
	}
	if m.cursor >= len(m.tasks) {
		m.cursor = max(0, len(m.tasks)-1)
	}
	m.followCursor()
	return m
}
