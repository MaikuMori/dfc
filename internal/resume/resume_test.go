package resume

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := Dir(t.TempDir())
	path := NewPath(dir)
	want := Draft{Kind: KindCapture, Text: "heading\n\nbody", Slug: "acme"}
	if err := Save(path, want); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("perm = %o, want 600", perm)
	}
	if got := Load(path); got != want {
		t.Errorf("Load = %+v, want %+v", got, want)
	}
	if got := Load(path); got != want {
		t.Errorf("second Load = %+v, want %+v", got, want)
	}
}

func TestSaveZeroRemoves(t *testing.T) {
	path := NewPath(Dir(t.TempDir()))
	if err := Save(path, Draft{}); err != nil {
		t.Fatalf("Save zero with no file: %v", err)
	}
	if err := Save(path, Draft{Kind: KindEdit, Text: "x", TaskID: "id"}); err != nil {
		t.Fatal(err)
	}
	if err := Save(path, Draft{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("file still present after zero Save: %v", err)
	}
}

func TestLoadRejectsMissingAndGarbage(t *testing.T) {
	dir := t.TempDir()
	if d := Load(filepath.Join(dir, "missing.json")); !d.IsZero() {
		t.Errorf("missing file = %+v, want zero", d)
	}
	bad := filepath.Join(dir, "bad.json")
	for _, body := range []string{"not json", `{"kind":"other","text":"x"}`} {
		if err := os.WriteFile(bad, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if d := Load(bad); !d.IsZero() {
			t.Errorf("Load(%q) = %+v, want zero", body, d)
		}
	}
}

func TestPruneByAge(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "old.json")
	fresh := filepath.Join(dir, "fresh.json")
	other := filepath.Join(dir, "keep.txt")
	for _, p := range []string{old, fresh, other} {
		if err := os.WriteFile(p, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	past := time.Now().Add(-MaxAge - time.Hour)
	for _, p := range []string{old, other} {
		if err := os.Chtimes(p, past, past); err != nil {
			t.Fatal(err)
		}
	}
	Prune(dir, MaxAge)
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Error("old draft not pruned")
	}
	for _, p := range []string{fresh, other} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s pruned: %v", filepath.Base(p), err)
		}
	}
}

func exitedPID(t *testing.T) int {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^$")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	return cmd.Process.Pid
}

func writeRecord(t *testing.T, path string, r record) {
	t.Helper()
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestOrphansOnlyDeadOwnersOnThisHost(t *testing.T) {
	dir := t.TempDir()
	d := Draft{Kind: KindCapture, Text: "x", Slug: "p"}
	dead := exitedPID(t)

	older := filepath.Join(dir, "older.json")
	newer := filepath.Join(dir, "newer.json")
	writeRecord(t, older, record{Draft: d, PID: dead, Host: hostname()})
	writeRecord(t, newer, record{Draft: d, PID: dead, Host: hostname()})
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(older, past, past); err != nil {
		t.Fatal(err)
	}
	writeRecord(t, filepath.Join(dir, "remote.json"), record{Draft: d, PID: dead, Host: hostname() + "-other"})
	if err := Save(filepath.Join(dir, "mine.json"), d); err != nil {
		t.Fatal(err)
	}

	got := Orphans(dir)
	if len(got) != 2 || got[0].Path != newer || got[1].Path != older {
		t.Fatalf("Orphans = %+v, want [newer older]", got)
	}
}

func TestClaimOnce(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.json")
	d := Draft{Kind: KindEdit, Text: "x", Slug: "p", TaskID: "id"}
	writeRecord(t, src, record{Draft: d, PID: exitedPID(t), Host: hostname()})

	a, b := NewPath(dir), NewPath(dir)
	if got := Claim(src, a); got != d {
		t.Fatalf("first Claim = %+v, want %+v", got, d)
	}
	if got := Claim(src, b); !got.IsZero() {
		t.Errorf("second Claim = %+v, want zero", got)
	}
	if len(Orphans(dir)) != 0 {
		t.Error("claimed draft still listed as an orphan")
	}
}
