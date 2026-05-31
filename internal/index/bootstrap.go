package index

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/MaikuMori/dfc/internal/storage"
)

// Bootstrap walks every project under the dfc root and inserts every
// task into the index. It is safe to call against an existing index —
// upserts are idempotent — but the cheaper "fast path" is to call only
// when Count() is 0 or after a wipe.
func (i *Index) Bootstrap() error {
	root, err := storage.Root()
	if err != nil {
		return err
	}
	projectsDir := filepath.Join(root, "projects")
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // nothing to index yet
		}
		return err
	}
	tx, err := i.db.Begin()
	if err != nil {
		return fmt.Errorf("could not begin search index rebuild: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	upsert, err := tx.Prepare(upsertSQL)
	if err != nil {
		return fmt.Errorf("could not prepare search index rebuild: %w", err)
	}
	defer func() { _ = upsert.Close() }()

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		s, err := storage.Open(e.Name())
		if err != nil {
			continue
		}
		tasks, err := s.List()
		if err != nil {
			continue
		}
		for _, t := range tasks {
			if _, err := upsert.Exec(
				t.ID, t.ProjectSlug, t.Path, string(t.Status),
				t.Description, t.Details, t.Created.Unix(), t.Modified.Unix(),
			); err != nil {
				return fmt.Errorf("could not index task %s during rebuild: %w", t.ID, err)
			}
		}
	}
	return tx.Commit()
}

// Reindex drops the entire tasks_meta table (FTS5 follows via trigger)
// and re-bootstraps from disk. Use this as the nuclear option when the
// index goes wrong in a way Bootstrap can't fix idempotently.
func (i *Index) Reindex() error {
	if _, err := i.db.Exec(`DELETE FROM tasks_meta`); err != nil {
		return fmt.Errorf("could not clear search index: %w", err)
	}
	return i.Bootstrap()
}

// SyncStaleSince re-indexes any task whose file mtime is newer than the
// given cutoff. Cheap drift-recovery to run at the start of a query
// when an external editor might have touched a file behind our back.
//
// We only scan the file headers (frontmatter + first heading) which is
// what storage.Load already does, so the cost is one ReadDir per
// project + one read per newly-modified file.
func (i *Index) SyncStaleSince(cutoff int64) error {
	root, err := storage.Root()
	if err != nil {
		return err
	}
	projectsDir := filepath.Join(root, "projects")
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(projectsDir, e.Name())
		files, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		var s *storage.Store
		for _, f := range files {
			if f.IsDir() || filepath.Ext(f.Name()) != ".md" {
				continue
			}
			info, err := f.Info()
			if err != nil || info.ModTime().Unix() <= cutoff {
				continue
			}
			if s == nil {
				s, err = storage.Open(e.Name())
				if err != nil {
					break
				}
			}
			t, err := s.Load(filepath.Join(dir, f.Name()))
			if err != nil {
				continue
			}
			if err := i.Upsert(t); err != nil {
				return err
			}
		}
	}
	return nil
}

// EnsureFresh combines a starts-empty check with the drift sync.
// Callers run it at the start of a query so the answer reflects on-disk
// reality even when an external editor wrote a file we didn't see.
//
// The contract:
//   - On a brand-new index (Count == 0), bootstrap the whole tree.
//   - Otherwise, resync anything strictly newer than the index's
//     MAX(modified).
func (i *Index) EnsureFresh() error {
	n, err := i.Count()
	if err != nil {
		return err
	}
	if n == 0 {
		return i.Bootstrap()
	}
	maxMod, err := i.MaxModified()
	if err != nil {
		return err
	}
	if err := i.SyncStaleSince(maxMod); err != nil {
		return err
	}
	return i.pruneOrphans()
}

// pruneOrphans drops index rows for tasks whose files have been deleted out
// from under us. SyncStaleSince only re-indexes newer files, so an external
// `rm` (or a failed best-effort delete) leaves a stale row pointing at a
// vanished path. Renames don't orphan — the row is keyed by ULID, so the
// newer file upserts onto the same row. Detection is by count: when a
// project's on-disk .md files and its index rows disagree, that project is
// rebuilt from disk.
func (i *Index) pruneOrphans() error {
	counts, err := i.CountsByProject()
	if err != nil {
		return err
	}
	if len(counts) == 0 {
		return nil
	}
	root, err := storage.Root()
	if err != nil {
		return err
	}
	projectsDir := filepath.Join(root, "projects")
	for slug, c := range counts {
		if countMarkdown(filepath.Join(projectsDir, slug)) != c.Open+c.Done {
			if err := i.reindexProject(slug); err != nil {
				return err
			}
		}
	}
	return nil
}

// reindexProject replaces every index row for one project with the tasks
// currently on disk, in a single transaction. A project whose directory is
// gone is simply cleared.
func (i *Index) reindexProject(slug string) error {
	var tasks []storage.Task
	if storage.ProjectDirExists(slug) {
		s, err := storage.Open(slug)
		if err != nil {
			return err
		}
		tasks, err = s.List()
		if err != nil {
			return err
		}
	}
	tx, err := i.db.Begin()
	if err != nil {
		return fmt.Errorf("could not begin project reindex: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM tasks_meta WHERE project = ?`, slug); err != nil {
		return fmt.Errorf("could not clear project %s from index: %w", slug, err)
	}
	upsert, err := tx.Prepare(upsertSQL)
	if err != nil {
		return err
	}
	defer func() { _ = upsert.Close() }()
	for _, t := range tasks {
		if _, err := upsert.Exec(
			t.ID, t.ProjectSlug, t.Path, string(t.Status),
			t.Description, t.Details, t.Created.Unix(), t.Modified.Unix(),
		); err != nil {
			return fmt.Errorf("could not reindex task %s: %w", t.ID, err)
		}
	}
	return tx.Commit()
}

// countMarkdown returns the number of .md files directly in dir, or 0 when
// the directory is missing.
func countMarkdown(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".md" {
			n++
		}
	}
	return n
}

