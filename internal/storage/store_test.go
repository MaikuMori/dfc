package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCreateDisambiguatesSameSecondCollision(t *testing.T) {
	root := t.TempDir()
	t.Setenv(EnvRoot, root)
	s, err := Open("p")
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	const slug = "0123456789-dup"
	a, err := s.Create(Task{ID: "0123456789ABCDEFGHJKMNPQRS", Status: StatusOpen, Created: now, Description: "dup"}, slug)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Create(Task{ID: "0123456789ZYXWVTSRQPNMKJHG", Status: StatusOpen, Created: now, Description: "dup"}, slug)
	if err != nil {
		t.Fatal(err)
	}

	if a.Path == b.Path {
		t.Fatalf("both tasks landed at %s; first was overwritten", a.Path)
	}
	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("List len = %d, want 2", len(list))
	}
	for _, id := range []string{a.ID, b.ID} {
		if p, err := s.FindByID(id); err != nil || p == "" {
			t.Errorf("FindByID(%s) = %q, %v; want a path", id, p, err)
		}
	}
}

func TestRenameForDescriptionRefusesCollision(t *testing.T) {
	root := t.TempDir()
	t.Setenv(EnvRoot, root)
	s, err := Open("p")
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	a, err := s.Create(Task{ID: "0123456789AAAAAAAAAAAAAAAA", Status: StatusOpen, Created: now, Description: "buy milk"}, "0123456789-buy-milk")
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Create(Task{ID: "0123456789BBBBBBBBBBBBBBBB", Status: StatusOpen, Created: now, Description: "walk dog"}, "0123456789-walk-dog")
	if err != nil {
		t.Fatal(err)
	}

	if err := s.RenameForDescription(&b, "buy milk"); err == nil {
		t.Fatalf("expected a collision error renaming over %s", a.Path)
	}

	loaded, err := s.Load(a.Path)
	if err != nil {
		t.Fatalf("task A was destroyed: %v", err)
	}
	if loaded.ID != a.ID {
		t.Errorf("task A clobbered: id = %s, want %s", loaded.ID, a.ID)
	}
}

func TestListSkipsUnparseableFile(t *testing.T) {
	root := t.TempDir()
	t.Setenv(EnvRoot, root)
	s, err := Open("p")
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	if _, err := s.Create(Task{ID: "0123456789AAAAAAAAAAAAAAAA", Status: StatusOpen, Created: now, Description: "good"}, "0123456789-good"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Dir(), "0123456789-bad.md"), []byte("not valid frontmatter"), 0o644); err != nil {
		t.Fatal(err)
	}

	list, err := s.List()
	if err != nil {
		t.Fatalf("List should tolerate one bad file: %v", err)
	}
	if len(list) != 1 || list[0].Description != "good" {
		t.Fatalf("List = %+v, want only the good task", list)
	}
}

func TestFindByIDIsCaseInsensitive(t *testing.T) {
	root := t.TempDir()
	t.Setenv(EnvRoot, root)
	s, err := Open("p")
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	id := "0123456789ABCDEFGHJKMNPQRS"
	if _, err := s.Create(Task{ID: id, Status: StatusOpen, Created: now, Description: "find me"}, FilenameSlug(id[:10], "find me")); err != nil {
		t.Fatal(err)
	}

	got, err := s.FindByID(strings.ToLower(id))
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got == "" {
		t.Errorf("FindByID(lowercased ULID) returned empty; want the task path")
	}
}

func TestMoveInRelocatesFileAndRetagsSlug(t *testing.T) {
	root := t.TempDir()
	t.Setenv(EnvRoot, root)
	a, err := Open("alpha")
	if err != nil {
		t.Fatal(err)
	}
	b, err := Open("beta")
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	id := "0123456789ABCDEFGHJKMNPQRS"
	task, err := a.Create(Task{ID: id, Status: StatusOpen, Created: now, Description: "move me"}, FilenameSlug(id[:10], "move me"))
	if err != nil {
		t.Fatal(err)
	}
	oldPath := task.Path

	moved, err := b.MoveIn(task)
	if err != nil {
		t.Fatalf("MoveIn: %v", err)
	}
	if moved.ProjectSlug != "beta" {
		t.Errorf("ProjectSlug = %q, want beta", moved.ProjectSlug)
	}
	if filepath.Dir(moved.Path) != b.Dir() {
		t.Errorf("moved path %q not under beta dir %q", moved.Path, b.Dir())
	}
	if moved.ID != id {
		t.Errorf("ULID changed to %q", moved.ID)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Errorf("old file should be gone, stat err = %v", err)
	}
	if p, _ := b.FindByID(id); p == "" {
		t.Errorf("beta.FindByID should locate the moved task")
	}
	if p, _ := a.FindByID(id); p != "" {
		t.Errorf("alpha.FindByID should no longer find it, got %q", p)
	}
}

func TestMoveInDisambiguatesDestCollision(t *testing.T) {
	root := t.TempDir()
	t.Setenv(EnvRoot, root)
	a, _ := Open("alpha")
	b, _ := Open("beta")
	now := time.Now().UTC().Truncate(time.Second)

	const slug = "0123456789-dup"
	if _, err := b.Create(Task{ID: "0123456789ZZZZZZZZZZZZZZZZ", Status: StatusOpen, Created: now, Description: "dup"}, slug); err != nil {
		t.Fatal(err)
	}
	moveID := "0123456789AAAAAAAAAAAAAAAA"
	src, err := a.Create(Task{ID: moveID, Status: StatusOpen, Created: now, Description: "dup"}, slug)
	if err != nil {
		t.Fatal(err)
	}

	moved, err := b.MoveIn(src)
	if err != nil {
		t.Fatalf("MoveIn: %v", err)
	}
	if filepath.Base(moved.Path) == "0123456789-dup.md" {
		t.Errorf("moved file should be disambiguated, got %s", filepath.Base(moved.Path))
	}
	if list, _ := b.List(); len(list) != 2 {
		t.Fatalf("beta should hold both files, got %d", len(list))
	}
	if p, _ := b.FindByID(moveID); p == "" {
		t.Errorf("moved ULID should resolve in beta")
	}
}

func TestMoveInMissingPath(t *testing.T) {
	root := t.TempDir()
	t.Setenv(EnvRoot, root)
	b, _ := Open("beta")
	if _, err := b.MoveIn(Task{ID: "x"}); err == nil {
		t.Error("MoveIn with no file path should error")
	}
}

func TestStoreCRUD(t *testing.T) {
	root := t.TempDir()
	t.Setenv(EnvRoot, root)

	s, err := Open("test-project")
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	t1, err := s.Create(Task{
		ID: "01AAA", Status: StatusOpen, Created: now,
		Description: "first",
	}, "01aaa-first")
	if err != nil {
		t.Fatal(err)
	}
	t2, err := s.Create(Task{
		ID: "01BBB", Status: StatusOpen, Created: now.Add(time.Second),
		Description: "second", Details: "with notes",
	}, "01bbb-second")
	if err != nil {
		t.Fatal(err)
	}

	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("list len = %d, want 2", len(list))
	}
	if list[0].ID != "01AAA" || list[1].ID != "01BBB" {
		t.Errorf("wrong order: %+v", list)
	}

	// Toggle t1 to done and re-save.
	t1.Status = StatusDone
	if err := s.Save(&t1); err != nil {
		t.Fatal(err)
	}
	loaded, err := s.Load(t1.Path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != StatusDone {
		t.Errorf("status = %s, want done", loaded.Status)
	}
	if loaded.Description != "first" {
		t.Errorf("description lost on save: %q", loaded.Description)
	}

	// Delete t2.
	if err := s.Delete(t2.Path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(t2.Path); !os.IsNotExist(err) {
		t.Errorf("file should be gone: err=%v", err)
	}

	list, err = s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("after delete len = %d, want 1", len(list))
	}
}

func TestStoreList_SortsByMtime(t *testing.T) {
	root := t.TempDir()
	t.Setenv(EnvRoot, root)

	s, err := Open("proj")
	if err != nil {
		t.Fatal(err)
	}

	a, err := s.Create(Task{ID: "A", Status: StatusOpen, Created: time.Now()}, "ts1-a")
	if err != nil {
		t.Fatal(err)
	}
	// Sleep just past mtime resolution worst-case so Save bumps clearly.
	time.Sleep(20 * time.Millisecond)
	_, err = s.Create(Task{ID: "B", Status: StatusOpen, Created: time.Now()}, "ts2-b")
	if err != nil {
		t.Fatal(err)
	}

	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if list[0].ID != "A" || list[1].ID != "B" {
		t.Fatalf("initial order: %s,%s want A,B", list[0].ID, list[1].ID)
	}

	// Re-save A → its mtime should jump past B.
	time.Sleep(20 * time.Millisecond)
	if err := s.Save(&a); err != nil {
		t.Fatal(err)
	}
	list, err = s.List()
	if err != nil {
		t.Fatal(err)
	}
	if list[0].ID != "B" || list[1].ID != "A" {
		t.Fatalf("after re-save: %s,%s want B,A (modified=%v,%v)",
			list[0].ID, list[1].ID, list[0].Modified, list[1].Modified)
	}
}

func TestRenameForDescription(t *testing.T) {
	root := t.TempDir()
	t.Setenv(EnvRoot, root)

	s, err := Open("proj")
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	task, err := s.Create(Task{
		ID: "01CCCCCCCC", Status: StatusOpen, Created: now,
		Description: "buy milk",
		Details:     "from the corner store",
	}, "01cccccccc-buy-milk")
	if err != nil {
		t.Fatal(err)
	}
	oldPath := task.Path

	if err := s.RenameForDescription(&task, "buy cheese"); err != nil {
		t.Fatal(err)
	}
	if task.Description != "buy cheese" {
		t.Errorf("description not updated in struct: %q", task.Description)
	}
	if task.Path == oldPath {
		t.Errorf("path unchanged: %q", task.Path)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Errorf("old file should be gone: err=%v", err)
	}
	if filepath.Base(task.Path) != "01cccccccc-buy-cheese.md" {
		t.Errorf("new filename = %q", filepath.Base(task.Path))
	}

	// Re-load and check details survived the rename.
	loaded, err := s.Load(task.Path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Description != "buy cheese" {
		t.Errorf("description lost: %q", loaded.Description)
	}
	if loaded.Details != "from the corner store" {
		t.Errorf("details lost: %q", loaded.Details)
	}

	// No-op rename (same description).
	before := task.Path
	if err := s.RenameForDescription(&task, "buy cheese"); err != nil {
		t.Fatal(err)
	}
	if task.Path != before {
		t.Errorf("expected no rename, path changed: %q -> %q", before, task.Path)
	}
}

func TestFindByID(t *testing.T) {
	root := t.TempDir()
	t.Setenv(EnvRoot, root)
	s, err := Open("proj")
	if err != nil {
		t.Fatal(err)
	}
	id := "01ABCDEFGH123456789012345A"
	task, err := s.Create(Task{ID: id, Status: StatusOpen, Created: time.Now(), Description: "x"}, "01abcdefgh-x")
	if err != nil {
		t.Fatal(err)
	}

	got, err := s.FindByID(id)
	if err != nil {
		t.Fatal(err)
	}
	if got != task.Path {
		t.Errorf("FindByID = %q, want %q", got, task.Path)
	}

	miss, err := s.FindByID("01ZZZZZZZZ00000000000000ZZ")
	if err != nil {
		t.Fatal(err)
	}
	if miss != "" {
		t.Errorf("expected miss, got %q", miss)
	}
}

func TestStoreTagsProjectSlug(t *testing.T) {
	root := t.TempDir()
	t.Setenv(EnvRoot, root)

	s, err := Open("acme")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	task, err := s.Create(Task{
		ID: "01TAGTEST00000000000000000", Status: StatusOpen, Created: now,
		Description: "tag me",
	}, "01tagtest00-tag-me")
	if err != nil {
		t.Fatal(err)
	}
	if task.ProjectSlug != "acme" {
		t.Errorf("Create: ProjectSlug = %q, want %q", task.ProjectSlug, "acme")
	}

	// Save round-trip: clear and re-set via Save.
	task.ProjectSlug = ""
	if err := s.Save(&task); err != nil {
		t.Fatal(err)
	}
	if task.ProjectSlug != "acme" {
		t.Errorf("Save: ProjectSlug = %q, want %q", task.ProjectSlug, "acme")
	}

	loaded, err := s.Load(task.Path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ProjectSlug != "acme" {
		t.Errorf("Load: ProjectSlug = %q, want %q", loaded.ProjectSlug, "acme")
	}

	if err := s.RenameForDescription(&loaded, "renamed me"); err != nil {
		t.Fatal(err)
	}
	if loaded.ProjectSlug != "acme" {
		t.Errorf("RenameForDescription: ProjectSlug = %q, want %q", loaded.ProjectSlug, "acme")
	}
}

func TestStoreDir(t *testing.T) {
	root := t.TempDir()
	t.Setenv(EnvRoot, root)
	s, err := Open("acme")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "projects", "acme")
	if s.Dir() != want {
		t.Errorf("dir = %q, want %q", s.Dir(), want)
	}
}
