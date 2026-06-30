package index

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/MaikuMori/dfc/internal/query"
	"github.com/MaikuMori/dfc/internal/storage"
)

// TagFilter is one #tag predicate applied in SQL, before LIMIT, so a
// matching task can never be cut off by ranking. Tag is matched against the
// task's mirrored inline tags with hierarchy ("a" also matches "a/b").
// Projects lists slugs whose project-level (registry) tags already satisfy
// the predicate — their tasks match regardless of inline tags. Exclude
// inverts the whole predicate.
type TagFilter struct {
	Tag      string
	Projects []string
	Exclude  bool
}

// SearchOpts narrows and orders search results. Zero-values are valid:
// the searcher returns all matches across every project / status,
// score-ordered, with a default limit of 20.
type SearchOpts struct {
	Project string // restrict to one slug; "" = any
	// Projects, when non-nil, restricts results to that set of slugs
	// (project IN (…)). Built by callers from a tag filter against the
	// registry. Empty slice = no match (intersect-with-empty); nil = no
	// project restriction beyond Project.
	Projects []string
	Status   string      // "open" | "done" | "" (any)
	Limit    int         // 0 → defaultLimit; negative → no limit
	SortBy   string      // "score" (default) | "modified" | "created"
	Tags     []TagFilter // #tag / -#tag predicates, AND-ed
}

// SearchHit is one returned match. Task carries the same shape as
// TaskOut for parity with the CLI's JSON; Snippet is an FTS5 excerpt
// with the matched terms wrapped in `<mark>…</mark>`.
type SearchHit struct {
	Task    storage.Task
	Score   float64
	Snippet string
}

const defaultLimit = 20

// sql renders the filter as one WHERE condition plus its bind args. A task
// matches when it carries the tag inline (exactly, or nested under it — the
// substr comparison is `tag/` as a prefix, mirroring tag.Has) or belongs to
// one of the pre-resolved projects. substr instead of LIKE sidesteps `_`
// being a LIKE wildcard, which is a legal tag character.
func (f TagFilter) sql() (cond string, args []any) {
	name := strings.ToLower(f.Tag)
	cond = `EXISTS (SELECT 1 FROM task_tags tt
		WHERE tt.id = tasks_meta.id AND (tt.tag = ? OR substr(tt.tag, 1, ?) = ?))`
	args = []any{name, len(name) + 1, name + "/"}
	if len(f.Projects) > 0 {
		placeholders := strings.Repeat("?,", len(f.Projects))
		cond = fmt.Sprintf(`(%s OR tasks_meta.project IN (%s))`, cond, placeholders[:len(placeholders)-1])
		for _, p := range f.Projects {
			args = append(args, p)
		}
	} else {
		cond = `(` + cond + `)`
	}
	if f.Exclude {
		cond = `NOT ` + cond
	}
	return cond, args
}

// Search runs an FTS5 MATCH against tasks_fts, applies optional
// filters in SQL, and returns the top-N ranked hits.
func (i *Index) Search(q string, opts SearchOpts) ([]SearchHit, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, nil
	}
	match := sanitizeFTSQuery(q)
	if match == "" {
		return nil, nil
	}

	// A negative limit passes through to SQLite, where LIMIT -1 means
	// unlimited — the TUI uses that to show every match in its scrollable
	// list.
	limit := opts.Limit
	if limit == 0 {
		limit = defaultLimit
	}

	// We rely on rank (BM25, lower is better in FTS5 → we ORDER BY rank
	// ascending) by default, falling back to a timestamp when the caller
	// asks for recency.
	orderBy := `rank`
	switch opts.SortBy {
	case "modified":
		orderBy = `tasks_meta.modified DESC`
	case "created":
		orderBy = `tasks_meta.created DESC`
	}

	// Build the WHERE clause and matching positional args together. Bind
	// order matters: it must walk left-to-right through the SQL.
	conds := []string{`tasks_fts MATCH ?`}
	args := []any{match}
	if opts.Project != "" {
		conds = append(conds, `tasks_meta.project = ?`)
		args = append(args, opts.Project)
	}
	if opts.Projects != nil {
		if len(opts.Projects) == 0 {
			// Empty set means no project survives the filter. Return early
			// without hitting the DB.
			return nil, nil
		}
		placeholders := strings.Repeat("?,", len(opts.Projects))
		placeholders = placeholders[:len(placeholders)-1]
		conds = append(conds, fmt.Sprintf(`tasks_meta.project IN (%s)`, placeholders))
		for _, p := range opts.Projects {
			args = append(args, p)
		}
	}
	if opts.Status != "" && opts.Status != "all" {
		conds = append(conds, `tasks_meta.status = ?`)
		args = append(args, opts.Status)
	}
	for _, f := range opts.Tags {
		cond, condArgs := f.sql()
		conds = append(conds, cond)
		args = append(args, condArgs...)
	}
	args = append(args, limit)

	stmt := fmt.Sprintf(`
		SELECT
			tasks_meta.id,
			tasks_meta.project,
			tasks_meta.path,
			tasks_meta.status,
			tasks_meta.description,
			tasks_meta.details,
			tasks_meta.created,
			tasks_meta.modified,
			-rank AS score,
			snippet(tasks_fts, -1, '<mark>', '</mark>', '…', 16) AS snippet
		FROM tasks_fts
		JOIN tasks_meta ON tasks_meta.rowid = tasks_fts.rowid
		WHERE %s
		ORDER BY %s
		LIMIT ?
	`, strings.Join(conds, " AND "), orderBy)

	rows, err := i.db.Query(stmt, args...)
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var hits []SearchHit
	for rows.Next() {
		var (
			t           storage.Task
			createdUnix int64
			modUnix     int64
			score       float64
			snippet     sql.NullString
			statusSt    string
		)
		if err := rows.Scan(
			&t.ID, &t.ProjectSlug, &t.Path, &statusSt,
			&t.Description, &t.Details, &createdUnix, &modUnix,
			&score, &snippet,
		); err != nil {
			return nil, fmt.Errorf("could not scan search result: %w", err)
		}
		t.Status = storage.Status(statusSt)
		t.Created = time.Unix(createdUnix, 0).UTC()
		t.Modified = time.Unix(modUnix, 0)
		hits = append(hits, SearchHit{Task: t, Score: score, Snippet: snippet.String})
	}
	return hits, rows.Err()
}

// ftsColumns are the indexed columns that may be used as a `column:term`
// qualifier. Any other prefix before a `:` is treated as literal text so
// SQLite can't error with "no such column".
var ftsColumns = map[string]bool{"description": true, "details": true}

// sanitizeFTSQuery turns a user-typed string into an FTS5 MATCH expression
// that can never be syntactically invalid. The rules:
//
//   - Quoted runs ("…") become FTS5 phrase tokens, exact match.
//   - Bare positive terms get an implicit `*` suffix so `mac` matches
//     `macos`, `macintosh`, etc. — grep-like prefix behavior.
//   - `description:term` / `details:term` filter to one indexed column.
//     Any other `:` (`12:30`, `host:port`) is quoted as literal text.
//   - A leading `-` marks a negation; negations stay exact.
//   - Bare AND/OR/NOT/NEAR (FTS5's uppercase operators) are quoted so a
//     literal search for one of those words can't break the grammar.
//   - Tokens with no letters or digits (a lone `*`, `:`, …) are dropped.
//   - We assemble `(pos1 AND pos2 …) NOT (neg1 OR neg2 …)`; an all-empty
//     result returns "" so the caller treats it as "no matches".
func sanitizeFTSQuery(q string) string {
	tokens := query.Tokenize(q)
	if len(tokens) == 0 {
		return ""
	}
	var pos, neg []string
	for _, tok := range tokens {
		if !hasAlnum(tok.Value) {
			continue
		}
		switch {
		case tok.Phrase:
			pos = append(pos, `"`+strings.ReplaceAll(tok.Value, `"`, `""`)+`"`)
		case tok.Negate:
			neg = append(neg, quoteBareToken(tok.Value))
		default:
			if v := withPrefixWildcard(tok.Value); v != "" {
				pos = append(pos, v)
			}
		}
	}
	if len(pos) == 0 {
		return "" // FTS5 has no "match everything" anchor for pure NOT.
	}
	expr := strings.Join(pos, " AND ")
	if len(neg) > 0 {
		expr = "(" + expr + ") NOT (" + strings.Join(neg, " OR ") + ")"
	}
	return expr
}

// withPrefixWildcard renders a bare positive term as an FTS5 sub-expression.
// Safe terms get a `*` suffix for prefix matching; anything carrying
// punctuation, a column qualifier, or an operator keyword is rewritten so the
// result is always valid. Returns "" for a term with no searchable content.
func withPrefixWildcard(s string) string {
	if !hasAlnum(s) {
		return ""
	}
	// Explicit trailing wildcard: prefix query on the stem.
	if strings.HasSuffix(s, "*") {
		stem := strings.Trim(s, "*")
		if !hasAlnum(stem) {
			return ""
		}
		if isSafeBareTerm(stem) {
			return stem + "*"
		}
		return quoteBareToken(stem)
	}
	// Column qualifier against a real FTS column.
	if col, val, ok := strings.Cut(s, ":"); ok && ftsColumns[strings.ToLower(col)] {
		val = strings.Trim(val, "*")
		if !hasAlnum(val) {
			return ""
		}
		if isSafeBareTerm(val) {
			return strings.ToLower(col) + ":" + val + "*"
		}
		return strings.ToLower(col) + ":" + quoteBareToken(val)
	}
	// Drop stray wildcards anywhere else, then prefix-match or quote.
	s = strings.Trim(s, "*")
	if isSafeBareTerm(s) && !isFTSKeyword(s) {
		return s + "*"
	}
	return quoteBareToken(s)
}

// quoteBareToken wraps a term in an FTS5 phrase quote when it can't stand as a
// bare token — i.e. it carries punctuation outside the safe set or is an
// uppercase operator keyword. Embedded double-quotes are doubled per FTS5's
// grammar.
func quoteBareToken(s string) string {
	if s == "" {
		return s
	}
	if isSafeBareTerm(s) && !isFTSKeyword(s) {
		return s
	}
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// isFTSKeyword reports whether s is one of FTS5's uppercase operator keywords.
// Only the exact uppercase form is special to the grammar; `and` is a term.
func isFTSKeyword(s string) bool {
	switch s {
	case "AND", "OR", "NOT", "NEAR":
		return true
	}
	return false
}

// hasAlnum reports whether s contains at least one letter or digit — i.e. any
// content the tokenizer would keep.
func hasAlnum(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

// isSafeBareTerm reports whether s may appear unquoted in an FTS5 MATCH
// expression. Only letters, digits, and `_` qualify: FTS5's query grammar
// reads an unquoted `-` as a column-filter separator (so a bare `oauth2-proxy`
// is parsed as the term `oauth2` filtered to a column named `proxy`, which
// errors with "no such column"), and other punctuation breaks the grammar too.
// Such terms must be phrase-quoted instead.
func isSafeBareTerm(s string) bool {
	for _, r := range s {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
		case r == '_':
		default:
			return false
		}
	}
	return true
}
