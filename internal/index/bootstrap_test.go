package index

import (
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/MaikuMori/dfc/internal/storage"
)

// writeTask drops a real task file under the DFC root so Bootstrap and
// SyncStaleSince can see it. Returns the created task for assertions.
func writeTask(t *testing.T, slug, desc string) storage.Task {
	t.Helper()
	store, err := storage.Open(slug)
	if err != nil {
		t.Fatalf("storage.Open(%s): %v", slug, err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	id, err := ulid.New(ulid.Timestamp(now), rand.Reader)
	if err != nil {
		t.Fatalf("ulid: %v", err)
	}
	task := storage.Task{
		ID:          id.String(),
		Status:      storage.StatusOpen,
		Created:     now,
		Description: desc,
	}
	filenameSlug := storage.FilenameSlug(task.ID[:10], desc)
	saved, err := store.Create(task, filenameSlug)
	if err != nil {
		t.Fatalf("store.Create: %v", err)
	}
	return saved
}

func TestBootstrapPicksUpExistingFiles(t *testing.T) {
	root := t.TempDir()
	t.Setenv(storage.EnvRoot, root)

	a := writeTask(t, "alpha", "buy milk")
	b := writeTask(t, "alpha", "take out trash")
	c := writeTask(t, "beta", "lay tile")

	idx, err := Open(filepath.Join(root, "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = idx.Close() })

	if err := idx.Bootstrap(); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	n, err := idx.Count()
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("Count = %d, want 3 (saw %s/%s/%s)", n, a.ID, b.ID, c.ID)
	}

	hits, err := idx.Search("milk", SearchOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Task.ProjectSlug != "alpha" {
		t.Errorf("search hits: %+v", hits)
	}
}

func TestBootstrapIsIdempotent(t *testing.T) {
	root := t.TempDir()
	t.Setenv(storage.EnvRoot, root)
	writeTask(t, "alpha", "buy milk")

	idx, err := Open(filepath.Join(root, "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = idx.Close() })

	for i := range 3 {
		if err := idx.Bootstrap(); err != nil {
			t.Fatalf("Bootstrap[%d]: %v", i, err)
		}
	}
	n, _ := idx.Count()
	if n != 1 {
		t.Errorf("repeated Bootstrap inflated row count: %d", n)
	}
}

func TestEnsureFreshBootstrapsEmptyIndex(t *testing.T) {
	root := t.TempDir()
	t.Setenv(storage.EnvRoot, root)
	writeTask(t, "alpha", "ensure fresh kicks off bootstrap")

	idx, err := Open(filepath.Join(root, "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = idx.Close() })

	if err := idx.EnsureFresh(); err != nil {
		t.Fatalf("EnsureFresh: %v", err)
	}
	n, _ := idx.Count()
	if n != 1 {
		t.Errorf("EnsureFresh should have bootstrapped, got %d rows", n)
	}
}

func TestEnsureFreshPrunesDeletedTask(t *testing.T) {
	root := t.TempDir()
	t.Setenv(storage.EnvRoot, root)

	keep := writeTask(t, "p", "keep me")
	gone := writeTask(t, "p", "delete me")

	idx, err := Open(filepath.Join(root, "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = idx.Close() })

	if err := idx.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	if n, _ := idx.Count(); n != 2 {
		t.Fatalf("precondition: Count = %d, want 2", n)
	}

	// Delete one task file behind the index's back.
	if err := os.Remove(gone.Path); err != nil {
		t.Fatal(err)
	}
	if err := idx.EnsureFresh(); err != nil {
		t.Fatalf("EnsureFresh: %v", err)
	}

	if n, _ := idx.Count(); n != 1 {
		t.Errorf("orphan not pruned: Count = %d, want 1", n)
	}
	if hits, _ := idx.Search("delete", SearchOpts{}); len(hits) != 0 {
		t.Errorf("deleted task still searchable: %+v", hits)
	}
	if hits, _ := idx.Search("keep", SearchOpts{}); len(hits) != 1 || hits[0].Task.ID != keep.ID {
		t.Errorf("surviving task missing after prune: %+v", hits)
	}
}

func TestSearchDegenerateQueriesDoNotError(t *testing.T) {
	root := t.TempDir()
	t.Setenv(storage.EnvRoot, root)
	writeTask(t, "p", "fix the bug at 12:30 today")

	idx, err := Open(filepath.Join(root, "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = idx.Close() })
	if err := idx.Bootstrap(); err != nil {
		t.Fatal(err)
	}

	degenerate := []string{
		"12:30", "host:port", "foo:bar", "col:", ":",
		"*", "*foo", "description:", "details:",
		"AND", "OR", "NOT", "NEAR",
		"fix AND", "bug NOT", "fix OR", "**", "  ",
	}
	for _, q := range degenerate {
		if _, err := idx.Search(q, SearchOpts{}); err != nil {
			t.Errorf("Search(%q) errored: %v", q, err)
		}
	}

	// The column-qualifier feature still works.
	if hits, err := idx.Search("description:bug", SearchOpts{}); err != nil || len(hits) != 1 {
		t.Errorf("description:bug → hits=%d err=%v, want 1 hit", len(hits), err)
	}
}

func TestBootstrapHandlesMissingProjectsDir(t *testing.T) {
	root := t.TempDir()
	t.Setenv(storage.EnvRoot, root)
	// No projects/ dir yet.

	idx, err := Open(filepath.Join(root, "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = idx.Close() })

	if err := idx.Bootstrap(); err != nil {
		t.Errorf("Bootstrap on empty root should succeed quietly, got: %v", err)
	}
	n, _ := idx.Count()
	if n != 0 {
		t.Errorf("expected empty index, got %d rows", n)
	}
}

func TestSyncStaleSincePicksUpNewerFiles(t *testing.T) {
	root := t.TempDir()
	t.Setenv(storage.EnvRoot, root)

	old := writeTask(t, "p", "old task")
	newOne := writeTask(t, "p", "new task")

	// Force deterministic mtimes that straddle a known cutoff. Avoids any
	// real-clock timing dependency on platforms whose filesystem mtime
	// resolution doesn't match our 1s sleep budget (Windows is the
	// repeat offender here). The cutoff is inclusive: a file at exactly the
	// cutoff second is re-synced (catching same-second siblings), only
	// strictly-older files are skipped.
	cutoff := int64(1_700_000_000)
	if err := os.Chtimes(old.Path, time.Unix(cutoff-10, 0), time.Unix(cutoff-10, 0)); err != nil {
		t.Fatalf("Chtimes old: %v", err)
	}
	if err := os.Chtimes(newOne.Path, time.Unix(cutoff, 0), time.Unix(cutoff, 0)); err != nil {
		t.Fatalf("Chtimes new: %v", err)
	}

	idx, err := Open(filepath.Join(root, "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = idx.Close() }()

	if err := idx.SyncStaleSince(cutoff); err != nil {
		t.Fatalf("SyncStaleSince: %v", err)
	}

	// Only `newOne` should have made it in.
	hits, err := idx.Search("new", SearchOpts{})
	if err != nil {
		t.Fatalf("Search 'new': %v", err)
	}
	if len(hits) != 1 || hits[0].Task.ID != newOne.ID {
		t.Errorf("expected only the newer task, got %d hits", len(hits))
	}
	if hits, _ := idx.Search("old", SearchOpts{}); len(hits) != 0 {
		t.Errorf("stale task should not have been re-indexed, got %d hits (task %s)", len(hits), old.ID)
	}
}

func TestSyncStaleSinceMissingProjectsDirIsNoOp(t *testing.T) {
	root := t.TempDir()
	t.Setenv(storage.EnvRoot, root)
	// Open the index but never write any project — projects/ never exists.
	idx, err := Open(filepath.Join(root, "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = idx.Close() }()

	if err := idx.SyncStaleSince(0); err != nil {
		t.Errorf("SyncStaleSince with no projects dir should be no-op, got: %v", err)
	}
}

func TestReindexRebuildsFromDisk(t *testing.T) {
	root := t.TempDir()
	t.Setenv(storage.EnvRoot, root)
	writeTask(t, "alpha", "row one")
	writeTask(t, "alpha", "row two")

	idx, err := Open(filepath.Join(root, "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = idx.Close() })

	// Plant a stale row that doesn't exist on disk.
	stale := mkTask("stale-id-not-on-disk", "ghost task", "")
	if err := idx.Upsert(stale); err != nil {
		t.Fatal(err)
	}
	if err := idx.Reindex(); err != nil {
		t.Fatal(err)
	}
	n, _ := idx.Count()
	if n != 2 {
		t.Errorf("Reindex should have dropped stale row; got %d", n)
	}
	hits, _ := idx.Search("ghost", SearchOpts{})
	if len(hits) != 0 {
		t.Errorf("stale row survived reindex: %+v", hits)
	}
}
