package ui

import (
	"errors"
	"strings"

	"github.com/MaikuMori/dfc/internal/index"
	"github.com/MaikuMori/dfc/internal/project"
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

// runSearch queries the FTS5 index with the active query string and
// replaces m.tasks with the ranked hits. Scope follows the current
// view: per-project view filters to m.slug; global view searches every
// project. If the index isn't open we fall back to a case-insensitive
// substring match over the in-memory list so the filter still does
// something useful.
//
// The list is reversed before display so the best-scored hit lands at
// the bottom of the viewport, closest to the input — matching the
// rest of the TUI's bottom-up reading direction.
func (m Model) runSearch() Model {
	if strings.TrimSpace(m.searchQuery) == "" {
		// No live query — bypass the search path and reload normally.
		if m.globalView {
			return m.reloadAll()
		}
		return m.reload()
	}
	if !m.core.HasIndex() {
		return m.runSearchFallback()
	}
	scope := ""
	if !m.globalView {
		scope = m.slug
	}
	opts := index.SearchOpts{
		Project: scope,
		Status:  "all",
		Limit:   200,
		SortBy:  "score",
	}
	if m.globalView && len(m.tagFilter) > 0 {
		reg := m.core.Registry()
		slugs := []string{}
		for _, slug := range reg.Slugs() {
			if tagFilterMatches(reg, slug, m.tagFilter) {
				slugs = append(slugs, slug)
			}
		}
		opts.Projects = slugs
	}
	hits, err := m.core.Search(m.searchQuery, opts)
	if err != nil {
		m.err = err
		return m
	}
	tasks := make([]storage.Task, len(hits))
	for i, h := range hits {
		tasks[len(hits)-1-i] = h.Task
	}
	return m.applySearchResults(tasks)
}

// runSearchFallback is the no-index path: walk m.tasks (whatever the
// last reload populated) and keep rows whose description or details
// contains any positive term, case-insensitive. Hits are ordered by
// their original mtime position (still bottom-up: newest at bottom).
func (m Model) runSearchFallback() Model {
	q := strings.ToLower(strings.TrimSpace(m.searchQuery))
	if q == "" {
		return m
	}
	var base []storage.Task
	if m.globalView {
		base = m.reloadAll().tasks
	} else {
		base = m.reload().tasks
	}
	var filtered []storage.Task
	for _, t := range base {
		if strings.Contains(strings.ToLower(t.Description), q) ||
			strings.Contains(strings.ToLower(t.Details), q) {
			filtered = append(filtered, t)
		}
	}
	return m.applySearchResults(filtered)
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

// reloadAll lists every known project and merges into one sorted slice.
// Per-project errors are non-fatal (best-effort). When m.tagFilter is
// non-empty, only tasks belonging to matching projects survive.
func (m Model) reloadAll() Model {
	all, err := m.core.ListAll()
	if err != nil {
		m.err = err
	}
	if len(m.tagFilter) > 0 {
		reg := m.core.Registry()
		out := all[:0]
		for _, t := range all {
			if tagFilterMatches(reg, t.ProjectSlug, m.tagFilter) {
				out = append(out, t)
			}
		}
		all = out
	}
	return m.applyTaskListPreservingCursor(all)
}

// tagFilterMatches mirrors matchesTagFilter from internal/cli but lives
// here so the ui package doesn't need to import internal/cli (which
// would be a layer violation). Keeps the OR + (untagged) semantics.
func tagFilterMatches(reg *project.Registry, slug string, filter []string) bool {
	if len(filter) == 0 {
		return true
	}
	tags := reg.Tags(slug)
	hasUntagged := false
	wantTags := make([]string, 0, len(filter))
	for _, f := range filter {
		if strings.EqualFold(f, "(untagged)") {
			hasUntagged = true
			continue
		}
		wantTags = append(wantTags, f)
	}
	if hasUntagged && len(tags) == 0 {
		return true
	}
	for _, w := range wantTags {
		for _, t := range tags {
			if strings.EqualFold(w, t) {
				return true
			}
		}
	}
	return false
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
