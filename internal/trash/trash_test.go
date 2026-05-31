package trash

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MaikuMori/dfc/internal/storage"
)

func TestListOrdersSameSecondByID(t *testing.T) {
	isolate(t)
	root, err := TrashRoot()
	if err != nil {
		t.Fatal(err)
	}
	when := time.Now().UTC().Truncate(time.Second)
	idLo := "01AAAAAAAAAAAAAAAAAAAAAAAA"
	idHi := "01BBBBBBBBBBBBBBBBBBBBBBBB"
	for _, id := range []string{idLo, idHi} {
		dir := filepath.Join(root, id)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		m := Manifest{ID: id, Kind: KindTask, Slug: "p", TaskID: "x", Filename: "f.md", DeletedAt: when}
		if err := writeManifest(dir, m); err != nil {
			t.Fatal(err)
		}
	}

	out, err := List()
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("List len = %d, want 2", len(out))
	}
	if out[0].ID != idHi {
		t.Errorf("same-second order: out[0].ID = %s, want the newer ULID %s", out[0].ID, idHi)
	}
}

// isolate points the storage root at t.TempDir() so trash operations
// don't touch the real ~/.dfc.
func isolate(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv(storage.EnvRoot, root)
	return root
}

// writeFakeTask drops a markdown file under the default test project "p".
// Tests that need multiple files pass distinct names.
func writeFakeTask(t *testing.T, name, body string) string {
	t.Helper()
	dir, err := storage.ProjectDir("p")
	if err != nil {
		t.Fatalf("ProjectDir: %v", err)
	}
	path := filepath.Join(dir, name+".md")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write fake task: %v", err)
	}
	return path
}

func TestTrashRootCreatesDir(t *testing.T) {
	root := isolate(t)
	got, err := TrashRoot()
	if err != nil {
		t.Fatalf("TrashRoot: %v", err)
	}
	want := filepath.Join(root, "trash")
	if got != want {
		t.Errorf("TrashRoot() = %q, want %q", got, want)
	}
	if info, err := os.Stat(got); err != nil || !info.IsDir() {
		t.Errorf("TrashRoot did not create directory: %v", err)
	}
}

func TestTTLFromEnvDefault(t *testing.T) {
	t.Setenv("DFC_TRASH_TTL_DAYS", "")
	if got := TTLFromEnv(); got != DefaultTTL {
		t.Errorf("TTLFromEnv unset = %v, want %v", got, DefaultTTL)
	}
}

func TestTTLFromEnvCustom(t *testing.T) {
	t.Setenv("DFC_TRASH_TTL_DAYS", "3")
	want := 3 * 24 * time.Hour
	if got := TTLFromEnv(); got != want {
		t.Errorf("TTLFromEnv(3) = %v, want %v", got, want)
	}
}

func TestTTLFromEnvMalformedFallsBack(t *testing.T) {
	t.Setenv("DFC_TRASH_TTL_DAYS", "not-a-number")
	if got := TTLFromEnv(); got != DefaultTTL {
		t.Errorf("TTLFromEnv(malformed) = %v, want default %v", got, DefaultTTL)
	}
}

func TestTTLFromEnvNonPositiveFallsBack(t *testing.T) {
	t.Setenv("DFC_TRASH_TTL_DAYS", "-5")
	if got := TTLFromEnv(); got != DefaultTTL {
		t.Errorf("TTLFromEnv(-5) = %v, want default %v", got, DefaultTTL)
	}
}

func TestTrashTaskAndRestoreRoundtrip(t *testing.T) {
	isolate(t)
	taskPath := writeFakeTask(t, "01abcdef00-fake", "body")

	m, err := TrashTask("p", "01ABCDEFGHJKMNPQRSTVWXYZ12", "the heading", taskPath)
	if err != nil {
		t.Fatalf("TrashTask: %v", err)
	}
	if m.Kind != KindTask || m.Slug != "p" || m.Description != "the heading" {
		t.Errorf("manifest = %+v", m)
	}
	if _, err := os.Stat(taskPath); !os.IsNotExist(err) {
		t.Errorf("task file should be gone after TrashTask; got %v", err)
	}

	// Look it up by exact id.
	entry, err := Find(m.ID)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	destDir, err := storage.ProjectDir("p")
	if err != nil {
		t.Fatalf("ProjectDir: %v", err)
	}
	restoredPath, err := RestoreTask(entry, destDir)
	if err != nil {
		t.Fatalf("RestoreTask: %v", err)
	}
	if _, err := os.Stat(restoredPath); err != nil {
		t.Errorf("restored path not on disk: %v", err)
	}
}

func TestTrashProjectAndRestoreRoundtrip(t *testing.T) {
	root := isolate(t)
	projectDir, err := storage.ProjectDir("doomed")
	if err != nil {
		t.Fatalf("ProjectDir: %v", err)
	}
	_ = os.WriteFile(filepath.Join(projectDir, "01ab-x.md"), []byte("body"), 0o644)

	m, err := TrashProject("doomed", "Doomed", projectDir)
	if err != nil {
		t.Fatalf("TrashProject: %v", err)
	}
	if m.Kind != KindProject || m.Slug != "doomed" || m.Name != "Doomed" {
		t.Errorf("manifest = %+v", m)
	}
	if _, err := os.Stat(projectDir); !os.IsNotExist(err) {
		t.Errorf("project dir should be gone; got %v", err)
	}

	entry, err := Find(m.ID)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	// Restore to a fresh location to avoid the duplicate-slug guard.
	restoreTo := filepath.Join(root, "projects", "doomed")
	if err := RestoreProject(entry, restoreTo); err != nil {
		t.Fatalf("RestoreProject: %v", err)
	}
	if _, err := os.Stat(restoreTo); err != nil {
		t.Errorf("restored project not on disk: %v", err)
	}
}

func TestRestoreTaskFailsWhenDestExists(t *testing.T) {
	isolate(t)
	taskPath := writeFakeTask(t, "01abcdef00-fake", "body")
	m, err := TrashTask("p", "01ABCDEFGHJKMNPQRSTVWXYZ12", "x", taskPath)
	if err != nil {
		t.Fatalf("TrashTask: %v", err)
	}

	// Recreate the original path so restore conflicts.
	if err := os.WriteFile(taskPath, []byte("blocked"), 0o644); err != nil {
		t.Fatalf("write conflict file: %v", err)
	}

	entry, _ := Find(m.ID)
	if _, err := RestoreTask(entry, filepath.Dir(taskPath)); err == nil {
		t.Errorf("expected RestoreTask to fail when destination exists")
	}
}

func TestListReturnsNewestFirst(t *testing.T) {
	isolate(t)
	first := writeFakeTask(t, "01abcdef00-first", "first")
	if _, err := TrashTask("p", "01AAAAAAAAAAAAAAAAAAAAAAA1", "first", first); err != nil {
		t.Fatalf("TrashTask 1: %v", err)
	}
	// Force a distinct DeletedAt second so sorting is deterministic.
	time.Sleep(1100 * time.Millisecond)
	second := writeFakeTask(t, "01abcdef00-second", "second")
	if _, err := TrashTask("p", "01BBBBBBBBBBBBBBBBBBBBBBB2", "second", second); err != nil {
		t.Fatalf("TrashTask 2: %v", err)
	}

	entries, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Description != "second" {
		t.Errorf("newest first: got %q, want %q", entries[0].Description, "second")
	}
}

func TestFindAmbiguousPrefixErrors(t *testing.T) {
	isolate(t)
	a := writeFakeTask(t, "01abcdef00-a", "a")
	b := writeFakeTask(t, "01abcdef00-b", "b")
	if _, err := TrashTask("p", "01A1", "a", a); err != nil {
		t.Fatalf("TrashTask a: %v", err)
	}
	if _, err := TrashTask("p", "01B1", "b", b); err != nil {
		t.Fatalf("TrashTask b: %v", err)
	}
	// Prefix "01" matches both ulid-style trash IDs (they all start with 01J... or similar).
	if _, err := Find("01"); err == nil {
		t.Errorf("expected ambiguous-prefix error")
	} else if !strings.Contains(err.Error(), "matches") {
		t.Errorf("unexpected error text: %v", err)
	}
}

func TestFindMissingErrors(t *testing.T) {
	isolate(t)
	if _, err := Find("ZZZZZZ"); err == nil {
		t.Errorf("expected error for missing prefix")
	}
}

func TestFindEmptyArgErrors(t *testing.T) {
	isolate(t)
	if _, err := Find(""); err == nil {
		t.Errorf("expected error for empty prefix")
	}
}

func TestEmptyPurgesAll(t *testing.T) {
	isolate(t)
	for i := range 3 {
		path := writeFakeTask(t, "01abcdef00-x"+string(rune('0'+i)), "x")
		if _, err := TrashTask("p", "01A"+string(rune('0'+i)), "x", path); err != nil {
			t.Fatalf("TrashTask %d: %v", i, err)
		}
	}
	n, err := Empty()
	if err != nil {
		t.Fatalf("Empty: %v", err)
	}
	if n != 3 {
		t.Errorf("Empty purged %d, want 3", n)
	}
	if remaining, _ := List(); len(remaining) != 0 {
		t.Errorf("entries remaining after Empty: %d", len(remaining))
	}
}

func TestSweepExpiredRespectsTTL(t *testing.T) {
	isolate(t)
	path := writeFakeTask(t, "01abcdef00-sweep", "x")
	if _, err := TrashTask("p", "01ABCDEFGHJKMNPQRSTVWXYZ12", "x", path); err != nil {
		t.Fatalf("TrashTask: %v", err)
	}

	// TTL=0 means disabled; nothing should be purged.
	if n, _ := SweepExpired(0); n != 0 {
		t.Errorf("SweepExpired(0) purged %d, want 0", n)
	}
	// Very short TTL drops everything (DeletedAt is truncated to the
	// second, so it's already past now-1ns).
	if n, _ := SweepExpired(1 * time.Nanosecond); n != 1 {
		t.Errorf("SweepExpired(1ns) purged %d, want 1", n)
	}
}
