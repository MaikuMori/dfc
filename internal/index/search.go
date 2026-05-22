package index

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/MaikuMori/dfc/internal/storage"
)

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
	Status   string // "open" | "done" | "" (any)
	Limit    int    // 0 → defaultLimit
	SortBy   string // "score" (default) | "modified" | "created"
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

	limit := opts.Limit
	if limit <= 0 {
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
			t          storage.Task
			createdUnix int64
			modUnix    int64
			score      float64
			snippet    sql.NullString
			statusSt   string
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

// sanitizeFTSQuery turns a user-typed string into an FTS5 MATCH
// expression. The rules are deliberately small:
//
//   - Quoted runs ("…") become FTS5 phrase tokens, exact match.
//   - Bare positive terms get an implicit `*` suffix so `mac` matches
//     `macos`, `macintosh`, etc. — most users (and agents) expect
//     grep-like substring/prefix behavior, not strict token equality.
//     Terms that already end in `*` or that contain punctuation
//     forcing quoting are left exact.
//   - A leading `-` on a token marks it as a negation. Negations stay
//     exact (we don't want `-stale` to also exclude `staleness`).
//   - We collect positives and negatives separately, then assemble:
//     `(pos1 AND pos2 …) NOT (neg1 OR neg2 …)`. FTS5 only supports
//     binary NOT, so this is the only grammatically valid shape.
//   - Pure-negation queries (e.g. just `-bread`) return "" — FTS5
//     can't express "everything except X" without an anchor.
//
// Column qualifiers like `description:foo` pass through unchanged
// because `:` is in the safe-character set; the `*` suffix lands on
// the value, which FTS5 interprets correctly.
func sanitizeFTSQuery(q string) string {
	tokens := Tokenize(q)
	if len(tokens) == 0 {
		return ""
	}
	var pos, neg []string
	for _, tok := range tokens {
		if tok.Phrase {
			// Phrases are exact matches; re-quote for FTS5.
			pos = append(pos, `"`+strings.ReplaceAll(tok.Value, `"`, `""`)+`"`)
			continue
		}
		if tok.Negate {
			// Negations are exact — broad excludes are surprising.
			neg = append(neg, quoteBareToken(tok.Value))
			continue
		}
		pos = append(pos, withPrefixWildcard(tok.Value))
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

// withPrefixWildcard appends `*` to a safe bare term so the search
// behaves like a substring/prefix match. If the term contains
// punctuation that forces quoting, or already ends in `*`, leave it
// alone — quoted FTS5 terms can't carry a wildcard.
func withPrefixWildcard(s string) string {
	if s == "" {
		return s
	}
	if strings.HasSuffix(s, "*") {
		return s
	}
	if !isSafeBareTerm(s) {
		return quoteBareToken(s)
	}
	return s + "*"
}

// quoteBareToken safely quotes a single bare FTS5 term when it contains
// punctuation outside the safe set; otherwise returns it unchanged.
// Embedded double-quotes are doubled, per FTS5's grammar.
func quoteBareToken(s string) string {
	if s == "" || isSafeBareTerm(s) {
		return s
	}
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

func isSafeBareTerm(s string) bool {
	for _, r := range s {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
		case r == '_' || r == '-' || r == '*' || r == ':':
			// allow common safe punctuation: _ for identifiers, - for
			// in-word hyphens, * for prefix queries, : for column
			// qualifiers (description:foo).
		default:
			return false
		}
	}
	return true
}
