package core

import (
	"fmt"
	"slices"
	"strings"

	"github.com/MaikuMori/dfc/internal/index"
	"github.com/MaikuMori/dfc/internal/query"
	"github.com/MaikuMori/dfc/internal/storage"
)

// QueryOpts configures Core.Query.
type QueryOpts struct {
	Project  string   // restrict to this project slug; "" = all projects
	Projects []string // further restrict to this set (e.g. CLI --tag); nil = no restriction
	Status   string   // open | done | all ("" = all)
	Limit    int      // final result cap; 0 = no cap
	SortBy   string   // score | modified | created
}

// QueryResult is what Core.Query returns.
type QueryResult struct {
	Hits []index.SearchHit
	// Text is the residual full-text query after stripping `@name` and tag
	// predicates — callers use it to highlight matches.
	Text string
	// Ranked is true when the hits came back BM25-scored from the index (a
	// text query). It is false for a tags-only / listed result, which is in
	// modification order. Callers that re-order (the TUI) branch on it.
	Ranked bool
}

// Query is the single execution path behind `dfc s`/`ss` and the TUI filter:
//   - a leading `@name` expands to the saved search's stored query
//   - `#tag` / `-#tag` predicates filter on each task's effective tags
//   - the residual text runs through the FTS index (scored), or, when the
//     query is tags-only, the scoped task list is used
//
// On the index path the tag predicates ride along as SQL, applied before
// LIMIT, so a tag match can never be cut off by ranking. The in-memory tag
// filter covers tags-only listings and the index-less fallback.
func (c *Core) Query(raw string, opts QueryOpts) (QueryResult, error) {
	expanded, err := c.expandSavedSearch(raw)
	if err != nil {
		return QueryResult{}, err
	}
	tq := query.Split(expanded)
	hasTags := len(tq.Include)+len(tq.Exclude) > 0
	text := strings.TrimSpace(tq.Text)
	if text == "" && !hasTags {
		// Nothing parsed as text or tags (e.g. a lone "-"): treat the whole
		// expanded query as text so behavior matches a plain search.
		text = strings.TrimSpace(expanded)
	}

	res := QueryResult{Text: text}
	switch {
	case text != "":
		res.Hits, res.Ranked, err = c.searchText(text, opts, tq)
		if err != nil {
			return QueryResult{}, err
		}
		if res.Ranked {
			return res, nil // tag predicates and limit were applied in SQL
		}
	default: // tags-only
		res.Hits = tasksToHits(c.listScoped(opts))
	}

	if hasTags {
		res.Hits = c.filterHits(res.Hits, tq.Include, tq.Exclude)
	}
	if opts.Limit > 0 && len(res.Hits) > opts.Limit {
		res.Hits = res.Hits[:opts.Limit]
	}
	return res, nil
}

// searchText runs the FTS query with the tag predicates pushed into SQL, or
// falls back to a case-insensitive substring scan over the scoped list when
// the index is unavailable — the caller then applies the tag filter and limit
// in memory. ranked reports whether the result is BM25-scored.
func (c *Core) searchText(text string, opts QueryOpts, tq query.Query) (hits []index.SearchHit, ranked bool, err error) {
	if c.idx == nil {
		return tasksToHits(substringFilter(c.listScoped(opts), text)), false, nil
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = -1 // no cap: the index treats a negative limit as unlimited
	}
	hits, err = c.idx.Search(text, index.SearchOpts{
		Project:  opts.Project,
		Projects: opts.Projects,
		Status:   opts.Status,
		Limit:    limit,
		SortBy:   opts.SortBy,
		Tags:     c.tagFilters(tq.Include, tq.Exclude),
	})
	return hits, err == nil, err
}

// expandSavedSearch resolves a query of the form `@name` to the saved search's
// stored query. Names are free text, so the whole remainder after `@` is the
// name. Queries without a leading `@` pass through unchanged.
func (c *Core) expandSavedSearch(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "@") {
		return raw, nil
	}
	name := strings.TrimSpace(raw[1:])
	ss, err := c.SavedSearches()
	if err != nil {
		return "", err
	}
	stored, ok := ss.Get(name)
	if !ok {
		return "", fmt.Errorf("no saved search named %q", name)
	}
	return stored, nil
}

// listScoped lists tasks for the query scope, applying the project restriction
// and status filter, sorted newest-first (by the requested timestamp).
func (c *Core) listScoped(opts QueryOpts) []storage.Task {
	var tasks []storage.Task
	if opts.Project != "" {
		tasks, _ = c.List(opts.Project)
	} else {
		tasks, _ = c.ListAll()
	}
	if len(opts.Projects) > 0 {
		set := make(map[string]bool, len(opts.Projects))
		for _, s := range opts.Projects {
			set[s] = true
		}
		f := tasks[:0]
		for _, t := range tasks {
			if set[t.ProjectSlug] {
				f = append(f, t)
			}
		}
		tasks = f
	}
	if opts.Status != "" && opts.Status != "all" {
		f := tasks[:0]
		for _, t := range tasks {
			if string(t.Status) == opts.Status {
				f = append(f, t)
			}
		}
		tasks = f
	}
	slices.SortStableFunc(tasks, func(a, b storage.Task) int {
		if opts.SortBy == "created" {
			return b.Created.Compare(a.Created)
		}
		return b.Modified.Compare(a.Modified)
	})
	return tasks
}

// filterHits keeps hits whose task satisfies the tag predicates.
func (c *Core) filterHits(hits []index.SearchHit, include, exclude []string) []index.SearchHit {
	if len(include) == 0 && len(exclude) == 0 {
		return hits
	}
	out := hits[:0]
	for _, h := range hits {
		if c.taskMatchesTags(h.Task, include, exclude) {
			out = append(out, h)
		}
	}
	return out
}

func tasksToHits(tasks []storage.Task) []index.SearchHit {
	hits := make([]index.SearchHit, len(tasks))
	for i, t := range tasks {
		hits[i] = index.SearchHit{Task: t}
	}
	return hits
}

func substringFilter(tasks []storage.Task, query string) []storage.Task {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return tasks
	}
	var out []storage.Task
	for _, t := range tasks {
		if strings.Contains(strings.ToLower(t.Description), q) ||
			strings.Contains(strings.ToLower(t.Details), q) {
			out = append(out, t)
		}
	}
	return out
}
