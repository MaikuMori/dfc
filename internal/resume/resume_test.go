package resume

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func openT(t *testing.T, dir string) *Instance {
	t.Helper()
	i, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return i
}

// abandon leaves a state file behind as a killed instance would.
func abandon(t *testing.T, dir string, s State) string {
	t.Helper()
	i := openT(t, dir)
	if err := i.Save(s); err != nil {
		t.Fatal(err)
	}
	if err := i.Close(false); err != nil {
		t.Fatal(err)
	}
	return i.Path()
}

var capture = State{
	View:  View{Project: "p", Cursor: "id"},
	Draft: Draft{Kind: KindCapture, Text: "heading\n\nbody", Slug: "p"},
}

func TestSaveReadRoundTrip(t *testing.T) {
	i := openT(t, Dir(t.TempDir()))
	t.Cleanup(func() { _ = i.Close(true) })
	if err := i.Save(capture); err != nil {
		t.Fatal(err)
	}
	if got, ok := Read(i.Path()); !ok || got != capture {
		t.Errorf("Read = %+v, %v; want %+v", got, ok, capture)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(i.Path())
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("perm = %o, want 600", perm)
		}
	}
}

func TestCloseDiscardOrKeep(t *testing.T) {
	dir := Dir(t.TempDir())
	kept := abandon(t, dir, capture)
	if _, err := os.Stat(kept); err != nil {
		t.Errorf("Close(false) removed the state file: %v", err)
	}
	if _, err := os.Stat(lockPath(kept)); !os.IsNotExist(err) {
		t.Errorf("Close(false) left the lock file: %v", err)
	}

	i := openT(t, dir)
	if err := i.Save(capture); err != nil {
		t.Fatal(err)
	}
	if err := i.Close(true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(i.Path()); !os.IsNotExist(err) {
		t.Errorf("Close(true) left the state file: %v", err)
	}
}

func TestOrphansSkipLiveForeignAndDraftless(t *testing.T) {
	dir := Dir(t.TempDir())
	older := abandon(t, dir, capture)
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(older, past, past); err != nil {
		t.Fatal(err)
	}
	newer := abandon(t, dir, capture)
	abandon(t, dir, State{View: View{Project: "p"}})

	live := openT(t, dir)
	t.Cleanup(func() { _ = live.Close(true) })
	if err := live.Save(capture); err != nil {
		t.Fatal(err)
	}

	foreign := filepath.Join(dir, "foreign.json")
	data := `{"view":{},"draft":{"kind":"capture","text":"x"},"host":"elsewhere"}`
	if err := os.WriteFile(foreign, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	got := Orphans(dir)
	if len(got) != 2 || got[0] != newer || got[1] != older {
		t.Errorf("Orphans = %q, want [%s %s]", got, newer, older)
	}
}

func TestOrphanWithStaleLockFile(t *testing.T) {
	dir := Dir(t.TempDir())
	path := abandon(t, dir, capture)
	// A crash leaves the lock file behind, unlocked.
	if err := os.WriteFile(lockPath(path), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := Orphans(dir); len(got) != 1 || got[0] != path {
		t.Errorf("Orphans = %q, want [%s]", got, path)
	}
}

func TestClaimOnce(t *testing.T) {
	dir := Dir(t.TempDir())
	src := abandon(t, dir, capture)
	a, b := openT(t, dir), openT(t, dir)
	t.Cleanup(func() { _ = a.Close(true); _ = b.Close(true) })

	if got, ok := a.Claim(src); !ok || got != capture {
		t.Fatalf("first Claim = %+v, %v", got, ok)
	}
	if _, ok := b.Claim(src); ok {
		t.Error("second Claim succeeded")
	}
	if got, ok := Read(a.Path()); !ok || got != capture {
		t.Errorf("claimed state = %+v, %v", got, ok)
	}
	if len(Orphans(dir)) != 0 {
		t.Error("claimed state still listed as an orphan")
	}
}

func TestClaimRefusesAndLeavesSource(t *testing.T) {
	dir := Dir(t.TempDir())
	i := openT(t, dir)
	t.Cleanup(func() { _ = i.Close(true) })

	live := openT(t, dir)
	t.Cleanup(func() { _ = live.Close(true) })
	if err := live.Save(capture); err != nil {
		t.Fatal(err)
	}

	outside := filepath.Join(t.TempDir(), "notes.json")
	garbage := filepath.Join(dir, "garbage.json")
	other := filepath.Join(dir, "notes.txt")
	for _, p := range []string{outside, garbage, other} {
		if err := os.WriteFile(p, []byte("keep me"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, src := range []string{outside, garbage, other, live.Path(), i.Path()} {
		if _, ok := i.Claim(src); ok {
			t.Errorf("Claim(%s) succeeded", filepath.Base(src))
		}
	}
	for _, p := range []string{outside, garbage, other} {
		if data, err := os.ReadFile(p); err != nil || string(data) != "keep me" {
			t.Errorf("%s changed: %q, %v", filepath.Base(p), data, err)
		}
	}
	if _, ok := Read(live.Path()); !ok {
		t.Error("live instance's state was taken")
	}
}

func TestPruneOnlyOldAbandoned(t *testing.T) {
	dir := Dir(t.TempDir())
	old := abandon(t, dir, capture)
	fresh := abandon(t, dir, capture)
	live := openT(t, dir)
	t.Cleanup(func() { _ = live.Close(true) })
	if err := live.Save(capture); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-MaxAge - time.Hour)
	for _, p := range []string{old, live.Path()} {
		if err := os.Chtimes(p, past, past); err != nil {
			t.Fatal(err)
		}
	}
	Prune(dir, MaxAge)
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Error("old abandoned state not pruned")
	}
	for _, p := range []string{fresh, live.Path()} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s pruned: %v", filepath.Base(p), err)
		}
	}
}
