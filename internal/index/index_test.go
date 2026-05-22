package index

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MaikuMori/dfc/internal/storage"
)

// openTempIndex opens a fresh DB inside t.TempDir() and registers a
// Close hook so each test owns its own file.
func openTempIndex(t *testing.T) *Index {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(storage.EnvRoot, dir)
	idx, err := Open(filepath.Join(dir, "index.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = idx.Close() })
	return idx
}

func mkTask(id, desc, details string) storage.Task {
	return storage.Task{
		ID:          id,
		ProjectSlug: "acme",
		Path:        "/tmp/" + id + ".md",
		Status:      storage.StatusOpen,
		Description: desc,
		Details:     details,
		Modified:    time.Unix(1_700_000_000, 0),
	}
}

func TestDefaultPathRespectsDFCRoot(t *testing.T) {
	root := t.TempDir()
	t.Setenv(storage.EnvRoot, root)
	got, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	want := filepath.Join(root, "index.db")
	if got != want {
		t.Errorf("DefaultPath() = %q, want %q", got, want)
	}
}

func TestCountsByProjectAcrossStatuses(t *testing.T) {
	idx := openTempIndex(t)

	// Two open + one done in "alpha"; one open in "beta".
	a1 := mkTask("01AAAAAAAAAAAAAAAAAAAAAAA1", "alpha-open-1", "")
	a1.ProjectSlug = "alpha"
	a2 := mkTask("01AAAAAAAAAAAAAAAAAAAAAAA2", "alpha-open-2", "")
	a2.ProjectSlug = "alpha"
	a3 := mkTask("01AAAAAAAAAAAAAAAAAAAAAAA3", "alpha-done", "")
	a3.ProjectSlug = "alpha"
	a3.Status = storage.StatusDone
	b1 := mkTask("01BBBBBBBBBBBBBBBBBBBBBBB1", "beta-open", "")
	b1.ProjectSlug = "beta"

	for _, task := range []storage.Task{a1, a2, a3, b1} {
		if err := idx.Upsert(task); err != nil {
			t.Fatalf("Upsert: %v", err)
		}
	}

	counts, err := idx.CountsByProject()
	if err != nil {
		t.Fatalf("CountsByProject: %v", err)
	}
	if counts["alpha"].Open != 2 || counts["alpha"].Done != 1 {
		t.Errorf("alpha = %+v, want {Open:2 Done:1}", counts["alpha"])
	}
	if counts["beta"].Open != 1 || counts["beta"].Done != 0 {
		t.Errorf("beta = %+v, want {Open:1 Done:0}", counts["beta"])
	}
}

func TestMaxModifiedEmptyReturnsZero(t *testing.T) {
	idx := openTempIndex(t)
	got, err := idx.MaxModified()
	if err != nil {
		t.Fatalf("MaxModified: %v", err)
	}
	if got != 0 {
		t.Errorf("MaxModified() on empty = %d, want 0", got)
	}
}

func TestMaxModifiedReturnsLargest(t *testing.T) {
	idx := openTempIndex(t)
	earlier := mkTask("01CCCCCCCCCCCCCCCCCCCCCCC1", "earlier", "")
	earlier.Modified = time.Unix(1_700_000_000, 0)
	later := mkTask("01CCCCCCCCCCCCCCCCCCCCCCC2", "later", "")
	later.Modified = time.Unix(1_800_000_000, 0)
	for _, task := range []storage.Task{earlier, later} {
		if err := idx.Upsert(task); err != nil {
			t.Fatalf("Upsert: %v", err)
		}
	}
	got, err := idx.MaxModified()
	if err != nil {
		t.Fatalf("MaxModified: %v", err)
	}
	if got != 1_800_000_000 {
		t.Errorf("MaxModified() = %d, want %d", got, int64(1_800_000_000))
	}
}

func TestOpenInitsSchema(t *testing.T) {
	idx := openTempIndex(t)
	var v int
	if err := idx.db.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil {
		t.Fatal(err)
	}
	if v != schemaVersion {
		t.Errorf("user_version = %d, want %d", v, schemaVersion)
	}
}

func TestUpsertAndCount(t *testing.T) {
	idx := openTempIndex(t)
	for i, desc := range []string{"buy milk", "take out trash", "buy bread"} {
		if err := idx.Upsert(mkTask(string(rune('A'+i)), desc, "")); err != nil {
			t.Fatalf("upsert %d: %v", i, err)
		}
	}
	n, err := idx.Count()
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("Count = %d, want 3", n)
	}
}

func TestUpsertReplacesByID(t *testing.T) {
	idx := openTempIndex(t)
	if err := idx.Upsert(mkTask("X", "first version", "")); err != nil {
		t.Fatal(err)
	}
	updated := mkTask("X", "second version", "")
	if err := idx.Upsert(updated); err != nil {
		t.Fatal(err)
	}
	hits, err := idx.Search("second", SearchOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Task.Description != "second version" {
		t.Errorf("Search hits = %+v", hits)
	}
	// And the old text shouldn't match anymore.
	hits, err = idx.Search("first", SearchOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Errorf("expected 0 hits for stale text, got %+v", hits)
	}
}

func TestDelete(t *testing.T) {
	idx := openTempIndex(t)
	_ = idx.Upsert(mkTask("Y", "doomed task", ""))
	if err := idx.Delete("Y"); err != nil {
		t.Fatal(err)
	}
	hits, _ := idx.Search("doomed", SearchOpts{})
	if len(hits) != 0 {
		t.Errorf("expected 0 hits after delete, got %d", len(hits))
	}
}

func TestSearchPhrase(t *testing.T) {
	idx := openTempIndex(t)
	_ = idx.Upsert(mkTask("A", "buy milk and bread", ""))
	_ = idx.Upsert(mkTask("B", "bread and butter", ""))

	hits, err := idx.Search(`"milk and bread"`, SearchOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Task.ID != "A" {
		t.Errorf("phrase search hits: %+v", hits)
	}
}

func TestSearchTermsAreANDed(t *testing.T) {
	idx := openTempIndex(t)
	_ = idx.Upsert(mkTask("A", "buy milk", ""))
	_ = idx.Upsert(mkTask("B", "milk and cookies", ""))
	_ = idx.Upsert(mkTask("C", "buy bread", ""))

	hits, err := idx.Search("buy milk", SearchOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Task.ID != "A" {
		t.Errorf("AND search hits: %+v", hits)
	}
}

func TestSearchNOT(t *testing.T) {
	idx := openTempIndex(t)
	_ = idx.Upsert(mkTask("A", "buy milk", ""))
	_ = idx.Upsert(mkTask("B", "buy bread", ""))

	hits, err := idx.Search("buy -bread", SearchOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Task.ID != "A" {
		t.Errorf("NOT search hits: %+v", hits)
	}
}

func TestSearchFiltersProject(t *testing.T) {
	idx := openTempIndex(t)
	a := mkTask("A", "milk", "")
	a.ProjectSlug = "alpha"
	b := mkTask("B", "milk", "")
	b.ProjectSlug = "beta"
	_ = idx.Upsert(a)
	_ = idx.Upsert(b)

	hits, _ := idx.Search("milk", SearchOpts{Project: "alpha"})
	if len(hits) != 1 || hits[0].Task.ID != "A" {
		t.Errorf("project filter hits: %+v", hits)
	}
}

func TestSearchFiltersStatus(t *testing.T) {
	idx := openTempIndex(t)
	a := mkTask("A", "milk", "")
	a.Status = storage.StatusDone
	b := mkTask("B", "milk", "")
	b.Status = storage.StatusOpen
	_ = idx.Upsert(a)
	_ = idx.Upsert(b)

	hits, _ := idx.Search("milk", SearchOpts{Status: "open"})
	if len(hits) != 1 || hits[0].Task.ID != "B" {
		t.Errorf("status filter hits: %+v", hits)
	}
}

func TestSearchSnippetHighlights(t *testing.T) {
	idx := openTempIndex(t)
	_ = idx.Upsert(mkTask("A", "Buy milk and bread", "Some details about it"))

	hits, _ := idx.Search("milk", SearchOpts{})
	if len(hits) != 1 || !strings.Contains(hits[0].Snippet, "<mark>milk</mark>") {
		t.Errorf("snippet = %q", hits[0].Snippet)
	}
}

func TestSearchEmptyQueryReturnsNoHits(t *testing.T) {
	idx := openTempIndex(t)
	_ = idx.Upsert(mkTask("A", "anything", ""))
	hits, err := idx.Search("", SearchOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Errorf("empty query should return 0 hits, got %d", len(hits))
	}
}

func TestSearchSanitizesPunctuation(t *testing.T) {
	idx := openTempIndex(t)
	_ = idx.Upsert(mkTask("A", "buy! milk!!", ""))
	// Raw "buy!" would crash FTS5 if passed as-is. quoteBareTerm should
	// wrap it.
	hits, err := idx.Search("buy!", SearchOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hits) != 1 {
		t.Errorf("punctuated term hits: %+v", hits)
	}
}

func TestSortByModified(t *testing.T) {
	idx := openTempIndex(t)
	old := mkTask("OLD", "milk", "")
	old.Modified = time.Unix(1_700_000_000, 0)
	fresh := mkTask("FRESH", "milk", "")
	fresh.Modified = time.Unix(1_800_000_000, 0)
	_ = idx.Upsert(old)
	_ = idx.Upsert(fresh)

	hits, _ := idx.Search("milk", SearchOpts{SortBy: "modified"})
	if len(hits) != 2 || hits[0].Task.ID != "FRESH" {
		t.Errorf("modified order: %+v", hits)
	}
}

// TestProjectsInFilter restricts search to a set of project slugs (the
// SQL builds `project IN (...)`). Empty slice short-circuits to no
// results; nil leaves the WHERE clause unconstrained.
func TestProjectsInFilter(t *testing.T) {
	idx := openTempIndex(t)
	alpha := mkTask("ALPHA", "shared milk", "")
	alpha.ProjectSlug = "alpha"
	beta := mkTask("BETA", "shared milk", "")
	beta.ProjectSlug = "beta"
	gamma := mkTask("GAMMA", "shared milk", "")
	gamma.ProjectSlug = "gamma"
	_ = idx.Upsert(alpha)
	_ = idx.Upsert(beta)
	_ = idx.Upsert(gamma)

	got, _ := idx.Search("milk", SearchOpts{Projects: []string{"alpha", "gamma"}})
	if len(got) != 2 {
		t.Fatalf("Projects filter [alpha gamma] → %d hits, want 2", len(got))
	}
	ids := map[string]bool{got[0].Task.ID: true, got[1].Task.ID: true}
	if !ids["ALPHA"] || !ids["GAMMA"] {
		t.Errorf("wrong slugs in result: %+v", got)
	}

	if hits, _ := idx.Search("milk", SearchOpts{Projects: []string{}}); len(hits) != 0 {
		t.Errorf("empty Projects slice should short-circuit to 0 hits, got %d", len(hits))
	}
	if hits, _ := idx.Search("milk", SearchOpts{Projects: nil}); len(hits) != 3 {
		t.Errorf("nil Projects = no restriction; expected 3 hits, got %d", len(hits))
	}
}

// TestSortByCreated covers the "created" branch of the SortBy switch.
// Created and Modified are intentionally opposite so the test would fail
// if the SQL fell through to the modified path.
func TestSortByCreated(t *testing.T) {
	idx := openTempIndex(t)
	old := mkTask("OLD_CREATED", "milk", "")
	old.Created = time.Unix(1_700_000_000, 0)
	old.Modified = time.Unix(1_900_000_000, 0) // newer mtime — must be ignored
	fresh := mkTask("FRESH_CREATED", "milk", "")
	fresh.Created = time.Unix(1_800_000_000, 0)
	fresh.Modified = time.Unix(1_700_000_000, 0) // older mtime — must be ignored
	_ = idx.Upsert(old)
	_ = idx.Upsert(fresh)

	hits, _ := idx.Search("milk", SearchOpts{SortBy: "created"})
	if len(hits) != 2 || hits[0].Task.ID != "FRESH_CREATED" {
		t.Errorf("created order: %+v", hits)
	}
}

func TestPersistenceRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(storage.EnvRoot, dir)
	path := filepath.Join(dir, "index.db")

	idx, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = idx.Upsert(mkTask("A", "persistent description", ""))
	if err := idx.Close(); err != nil {
		t.Fatal(err)
	}

	idx2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = idx2.Close() }()
	hits, _ := idx2.Search("persistent", SearchOpts{})
	if len(hits) != 1 {
		t.Errorf("expected row to survive close+reopen, got %d hits", len(hits))
	}
}
