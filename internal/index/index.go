// Package index is a write-through SQLite FTS5 mirror of the tasks on
// disk. Markdown files in ~/.dfc/projects/<slug>/ are the source of
// truth; the index is a search-aware cache that callers must keep in
// sync via Upsert / Delete after every successful filesystem mutation.
//
// The schema uses external-content FTS5: tasks_meta is the row store
// (filterable scalars + description/details text) and tasks_fts is a
// virtual table mirrored by triggers. Schema version is tracked via
// PRAGMA user_version so we can migrate forward without losing data.
package index

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/MaikuMori/dfc/internal/storage"

	_ "modernc.org/sqlite"
)

// schemaVersion is bumped any time the on-disk DDL changes in a way
// that requires migration. v1 is the initial layout; v2 adds task_tags,
// the per-task mirror of inline #tags.
const schemaVersion = 2

// upsertSQL is the canonical insert-or-update for tasks_meta. Every write
// path funnels through taskWriter, which pairs it with the task_tags
// refresh so the two tables can never drift.
const upsertSQL = `
	INSERT INTO tasks_meta (id, project, path, status, description, details, created, modified)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		project     = excluded.project,
		path        = excluded.path,
		status      = excluded.status,
		description = excluded.description,
		details     = excluded.details,
		created     = excluded.created,
		modified    = excluded.modified
`

const (
	clearTagsSQL = `DELETE FROM task_tags WHERE id = ?`
	insertTagSQL = `INSERT INTO task_tags (id, tag) VALUES (?, ?)`
)

// Index is a handle on the SQLite-backed search index.
type Index struct {
	db *sql.DB
}

// DefaultPath returns ~/.dfc/index.db, respecting DFC_ROOT.
func DefaultPath() (string, error) {
	root, err := storage.Root()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "index.db"), nil
}

// Open opens (or creates) the SQLite database at path. WAL + busy_timeout
// keep concurrent CLI/TUI opens from deadlocking.
func Open(path string) (*Index, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("could not open search index %s: %w", path, err)
	}
	// Single-conn writer model — avoids "database is locked" under concurrent open.
	db.SetMaxOpenConns(1)

	idx := &Index{db: db}
	if err := idx.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return idx, nil
}

// Close releases the database handle.
func (i *Index) Close() error {
	if i == nil || i.db == nil {
		return nil
	}
	return i.db.Close()
}

// migrate brings the schema up to schemaVersion. v0 → v1 means create
// everything from scratch.
func (i *Index) migrate() error {
	var v int
	if err := i.db.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil {
		return fmt.Errorf("could not read search index schema version: %w", err)
	}
	if v == schemaVersion {
		return nil
	}
	if v > schemaVersion {
		return fmt.Errorf("search index schema is newer (%d) than this binary supports (%d) — upgrade dfc", v, schemaVersion)
	}
	if v == 0 {
		if err := i.createV1(); err != nil {
			return err
		}
		v = 1
	}
	if v < 2 {
		if err := i.migrateV2(); err != nil {
			return err
		}
	}
	// Future migrations: chain `if v < N { migrateToN(); v = N }` blocks here.
	return nil
}

func (i *Index) createV1() error {
	stmts := []string{
		`CREATE TABLE tasks_meta (
			rowid       INTEGER PRIMARY KEY AUTOINCREMENT,
			id          TEXT UNIQUE NOT NULL,
			project     TEXT NOT NULL,
			path        TEXT NOT NULL,
			status      TEXT NOT NULL,
			description TEXT NOT NULL,
			details     TEXT NOT NULL DEFAULT '',
			created     INTEGER NOT NULL DEFAULT 0,
			modified    INTEGER NOT NULL
		)`,
		`CREATE INDEX idx_tasks_modified ON tasks_meta(modified DESC)`,
		`CREATE INDEX idx_tasks_project  ON tasks_meta(project)`,
		`CREATE INDEX idx_tasks_status   ON tasks_meta(status)`,
		`CREATE VIRTUAL TABLE tasks_fts USING fts5(
			description,
			details,
			content='tasks_meta',
			content_rowid='rowid',
			tokenize='porter unicode61'
		)`,
		`CREATE TRIGGER tasks_ai AFTER INSERT ON tasks_meta BEGIN
			INSERT INTO tasks_fts(rowid, description, details)
			VALUES (new.rowid, new.description, new.details);
		END`,
		`CREATE TRIGGER tasks_ad AFTER DELETE ON tasks_meta BEGIN
			INSERT INTO tasks_fts(tasks_fts, rowid, description, details)
			VALUES ('delete', old.rowid, old.description, old.details);
		END`,
		`CREATE TRIGGER tasks_au AFTER UPDATE ON tasks_meta BEGIN
			INSERT INTO tasks_fts(tasks_fts, rowid, description, details)
			VALUES ('delete', old.rowid, old.description, old.details);
			INSERT INTO tasks_fts(rowid, description, details)
			VALUES (new.rowid, new.description, new.details);
		END`,
		`PRAGMA user_version = 1`,
	}
	tx, err := i.db.Begin()
	if err != nil {
		return fmt.Errorf("could not begin search index migration: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, s := range stmts {
		if _, err := tx.Exec(s); err != nil {
			return fmt.Errorf("search index migration failed: %w\nstmt: %s", err, strings.TrimSpace(s))
		}
	}
	return tx.Commit()
}

// migrateV2 adds task_tags — one lowercased inline #tag per row, mirroring
// what storage.Task.Tags() extracts from the task's markdown — and backfills
// it from the descriptions/details already in tasks_meta. The AFTER DELETE
// trigger keeps the table in step with every delete path (single-row Delete,
// project clears, and full wipes) without each call site remembering to.
func (i *Index) migrateV2() error {
	tx, err := i.db.Begin()
	if err != nil {
		return fmt.Errorf("could not begin search index migration: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	stmts := []string{
		`CREATE TABLE task_tags (
			id  TEXT NOT NULL,
			tag TEXT NOT NULL,
			PRIMARY KEY (id, tag)
		) WITHOUT ROWID`,
		`CREATE INDEX idx_task_tags_tag ON task_tags(tag)`,
		`CREATE TRIGGER task_tags_ad AFTER DELETE ON tasks_meta BEGIN
			DELETE FROM task_tags WHERE id = old.id;
		END`,
	}
	for _, s := range stmts {
		if _, err := tx.Exec(s); err != nil {
			return fmt.Errorf("search index migration failed: %w\nstmt: %s", err, strings.TrimSpace(s))
		}
	}
	backfill, err := tagBackfill(tx)
	if err != nil {
		return err
	}
	for _, p := range backfill {
		for _, name := range p.tags {
			if _, err := tx.Exec(insertTagSQL, p.id, strings.ToLower(name)); err != nil {
				return fmt.Errorf("could not backfill tags for task %s: %w", p.id, err)
			}
		}
	}
	if _, err := tx.Exec(`PRAGMA user_version = 2`); err != nil {
		return fmt.Errorf("search index migration failed: %w", err)
	}
	return tx.Commit()
}

// taskTags pairs a task id with its parsed inline tags during the v2
// backfill.
type taskTags struct {
	id   string
	tags []string
}

// tagBackfill re-derives every indexed task's inline tags from the content
// already in tasks_meta. Results are collected before the caller writes them:
// the pool holds a single connection, so the SELECT must be fully drained
// before inserts can proceed on the same transaction.
func tagBackfill(tx *sql.Tx) ([]taskTags, error) {
	rows, err := tx.Query(`SELECT id, description, details FROM tasks_meta`)
	if err != nil {
		return nil, fmt.Errorf("could not read tasks for tag backfill: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []taskTags
	for rows.Next() {
		var id string
		var t storage.Task
		if err := rows.Scan(&id, &t.Description, &t.Details); err != nil {
			return nil, fmt.Errorf("could not scan task for tag backfill: %w", err)
		}
		if tags := t.Tags(); len(tags) > 0 {
			out = append(out, taskTags{id: id, tags: tags})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("could not read tasks for tag backfill: %w", err)
	}
	return out, nil
}

// taskWriter binds the prepared statements that keep tasks_meta and
// task_tags in step inside one transaction. Every write path (Upsert,
// Bootstrap, project reindex) funnels through it so the row store and the
// tag mirror can never drift.
type taskWriter struct {
	meta, clearTags, addTag *sql.Stmt
}

func newTaskWriter(tx *sql.Tx) (*taskWriter, error) {
	w := &taskWriter{}
	var err error
	if w.meta, err = tx.Prepare(upsertSQL); err != nil {
		return nil, fmt.Errorf("could not prepare search index write: %w", err)
	}
	if w.clearTags, err = tx.Prepare(clearTagsSQL); err != nil {
		w.close()
		return nil, fmt.Errorf("could not prepare search index write: %w", err)
	}
	if w.addTag, err = tx.Prepare(insertTagSQL); err != nil {
		w.close()
		return nil, fmt.Errorf("could not prepare search index write: %w", err)
	}
	return w, nil
}

func (w *taskWriter) close() {
	for _, s := range []*sql.Stmt{w.meta, w.clearTags, w.addTag} {
		if s != nil {
			_ = s.Close()
		}
	}
}

// put inserts or replaces the task's row and rewrites its tag mirror. The
// FTS trigger keeps tasks_fts in sync automatically; tags are stored
// lowercased (Task.Tags is already case-insensitively unique, so lowering
// cannot collide).
func (w *taskWriter) put(t storage.Task) error {
	if t.ID == "" {
		return errors.New("task has no id")
	}
	if _, err := w.meta.Exec(
		t.ID, t.ProjectSlug, t.Path, string(t.Status),
		t.Description, t.Details, t.Created.Unix(), t.Modified.Unix(),
	); err != nil {
		return fmt.Errorf("could not index task %s: %w", t.ID, err)
	}
	if _, err := w.clearTags.Exec(t.ID); err != nil {
		return fmt.Errorf("could not index task %s: %w", t.ID, err)
	}
	for _, name := range t.Tags() {
		if _, err := w.addTag.Exec(t.ID, strings.ToLower(name)); err != nil {
			return fmt.Errorf("could not index task %s: %w", t.ID, err)
		}
	}
	return nil
}

// Upsert inserts or replaces a row keyed by the task's ULID, refreshing
// the task_tags mirror in the same transaction.
func (i *Index) Upsert(t storage.Task) error {
	tx, err := i.db.Begin()
	if err != nil {
		return fmt.Errorf("could not index task %s: %w", t.ID, err)
	}
	defer func() { _ = tx.Rollback() }()
	w, err := newTaskWriter(tx)
	if err != nil {
		return err
	}
	defer w.close()
	if err := w.put(t); err != nil {
		return err
	}
	return tx.Commit()
}

// Delete removes the row (its FTS shadow and task_tags rows follow via
// triggers) for the given ULID. Missing ids are a no-op.
func (i *Index) Delete(id string) error {
	if id == "" {
		return errors.New("missing task id")
	}
	if _, err := i.db.Exec(`DELETE FROM tasks_meta WHERE id = ?`, id); err != nil {
		return fmt.Errorf("could not remove task %s from search index: %w", id, err)
	}
	return nil
}

// Count returns the number of rows currently in the index. Useful for
// drift detection and `dfc index status`.
func (i *Index) Count() (int, error) {
	var n int
	err := i.db.QueryRow(`SELECT COUNT(*) FROM tasks_meta`).Scan(&n)
	return n, err
}

// ProjectCounts is the open / done tally for a single project, returned
// by Index.CountsByProject. Open + Done == total tasks in that project.
type ProjectCounts struct {
	Open int
	Done int
}

// CountsByProject returns one entry per project that has at least one
// row in the index, keyed by project slug. Single round-trip — the
// picker and `dfc projects` use this so showing counts costs the same
// as not showing them.
func (i *Index) CountsByProject() (map[string]ProjectCounts, error) {
	rows, err := i.db.Query(`
		SELECT project, status, COUNT(*)
		FROM tasks_meta
		GROUP BY project, status
	`)
	if err != nil {
		return nil, fmt.Errorf("could not count tasks: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]ProjectCounts{}
	for rows.Next() {
		var (
			slug, status string
			n            int
		)
		if err := rows.Scan(&slug, &status, &n); err != nil {
			return nil, fmt.Errorf("could not scan task count row: %w", err)
		}
		c := out[slug]
		switch status {
		case "done":
			c.Done += n
		default:
			c.Open += n
		}
		out[slug] = c
	}
	return out, rows.Err()
}

// MaxModified returns the largest modified timestamp known to the index
// (zero when empty). Used by the staleness check.
func (i *Index) MaxModified() (int64, error) {
	var t sql.NullInt64
	err := i.db.QueryRow(`SELECT MAX(modified) FROM tasks_meta`).Scan(&t)
	if err != nil {
		return 0, err
	}
	if !t.Valid {
		return 0, nil
	}
	return t.Int64, nil
}
