package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRegistry_LoadEmpty(t *testing.T) {
	dir := t.TempDir()
	r, err := loadRegistryFromPath(filepath.Join(dir, "projects.json"))
	if err != nil {
		t.Fatal(err)
	}
	if r.Has("anything") {
		t.Error("expected empty registry")
	}
	if r.Name("missing") != "missing" {
		t.Error("expected slug fallback on Name miss")
	}
}

func TestRegistry_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "projects.json")
	r, err := loadRegistryFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if added := r.Register("github-com-acme-widget", "Acme Widget"); !added {
		t.Error("expected newly added")
	}
	if added := r.Register("github-com-acme-widget", "Acme Widget"); added {
		t.Error("second register should be a refresh, not add")
	}
	r.Register("users-miks-code-dfc", "dfc")
	if err := r.Save(); err != nil {
		t.Fatal(err)
	}

	// Re-load from disk and verify.
	r2, err := loadRegistryFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if r2.Name("github-com-acme-widget") != "Acme Widget" {
		t.Errorf("name lost: %q", r2.Name("github-com-acme-widget"))
	}
	if !r2.Has("users-miks-code-dfc") {
		t.Error("second slug missing")
	}

	// File should be human-readable JSON.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "\"name\": \"Acme Widget\"") {
		t.Errorf("unexpected on-disk shape:\n%s", raw)
	}
}

func TestRegistry_TouchPreservesNameAndBumpsLastUsed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "projects.json")
	r, _ := loadRegistryFromPath(path)
	r.Register("a", "Apples")
	if err := r.Save(); err != nil {
		t.Fatal(err)
	}
	if !r.LastUsed("a").IsZero() {
		t.Error("expected zero LastUsed before Touch")
	}
	if err := r.Touch("a"); err != nil {
		t.Fatal(err)
	}
	if r.Name("a") != "Apples" {
		t.Errorf("name lost: %q", r.Name("a"))
	}
	if r.LastUsed("a").IsZero() {
		t.Error("LastUsed not bumped")
	}

	// Round-trip via disk.
	r2, _ := loadRegistryFromPath(path)
	if r2.LastUsed("a").IsZero() {
		t.Error("LastUsed not persisted")
	}
}

func TestRegistry_TouchAutoRegisters(t *testing.T) {
	dir := t.TempDir()
	r, _ := loadRegistryFromPath(filepath.Join(dir, "projects.json"))
	if err := r.Touch("new-slug"); err != nil {
		t.Fatal(err)
	}
	if !r.Has("new-slug") {
		t.Fatal("Touch should auto-register")
	}
	if r.Name("new-slug") != "new-slug" {
		t.Errorf("default name should be slug: %q", r.Name("new-slug"))
	}
}

func TestDefaultTag(t *testing.T) {
	cases := map[string]string{
		"users-maiku-projects-dfc":  "dfc",
		"github-com-acme-widget":    "widget",
		"gitlab-com-group-sub-repo": "repo",
		"single":                    "single",
		"":                          "",
	}
	for in, want := range cases {
		if got := DefaultTag(in); got != want {
			t.Errorf("DefaultTag(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRegistry_TagFallsBackToDefault(t *testing.T) {
	dir := t.TempDir()
	r, _ := loadRegistryFromPath(filepath.Join(dir, "projects.json"))
	r.Register("users-x-acme", "Acme")
	if got := r.Tag("users-x-acme"); got != "acme" {
		t.Errorf("Tag fallback = %q, want %q", got, "acme")
	}
}

func TestRegistry_SetTagPersists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "projects.json")
	r, _ := loadRegistryFromPath(path)
	r.Register("foo", "Foo")
	if err := r.SetTag("foo", "F"); err != nil {
		t.Fatal(err)
	}
	if r.Tag("foo") != "F" {
		t.Errorf("Tag = %q after SetTag", r.Tag("foo"))
	}
	r2, _ := loadRegistryFromPath(path)
	if r2.Tag("foo") != "F" {
		t.Errorf("Tag = %q after reload", r2.Tag("foo"))
	}
	// Clearing reverts to default.
	if err := r2.SetTag("foo", ""); err != nil {
		t.Fatal(err)
	}
	if r2.Tag("foo") != "foo" {
		t.Errorf("Tag = %q after clear, want default", r2.Tag("foo"))
	}
}

func TestLoadRegistryRespectsDFCRoot(t *testing.T) {
	root := t.TempDir()
	t.Setenv("DFC_ROOT", root)

	r, err := LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	if r.Has("anything") {
		t.Errorf("expected empty registry")
	}
	r.Register("acme", "Acme")
	if err := r.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	// File should land at $DFC_ROOT/projects.json.
	if _, err := os.Stat(filepath.Join(root, "projects.json")); err != nil {
		t.Errorf("expected projects.json on disk: %v", err)
	}

	// A second LoadRegistry must see the saved entry.
	r2, err := LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry 2: %v", err)
	}
	if !r2.Has("acme") {
		t.Errorf("reload lost the saved entry")
	}
}

func TestRegistry_RenameUpdatesName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "projects.json")
	r, _ := loadRegistryFromPath(path)
	r.Register("foo", "Foo")
	if err := r.Rename("foo", "Foo Renamed"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if r.Name("foo") != "Foo Renamed" {
		t.Errorf("Name = %q, want %q", r.Name("foo"), "Foo Renamed")
	}
	// Persistence.
	r2, _ := loadRegistryFromPath(path)
	if r2.Name("foo") != "Foo Renamed" {
		t.Errorf("Rename not persisted; reload = %q", r2.Name("foo"))
	}
}

func TestRegistry_RenameUnknownSlugErrors(t *testing.T) {
	dir := t.TempDir()
	r, _ := loadRegistryFromPath(filepath.Join(dir, "projects.json"))
	if err := r.Rename("ghost", "x"); err == nil {
		t.Errorf("expected error renaming unknown slug")
	}
}

func TestRegistry_UnregisterRemovesEntry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "projects.json")
	r, _ := loadRegistryFromPath(path)
	r.Register("doomed", "Doomed")
	if err := r.Unregister("doomed"); err != nil {
		t.Fatalf("Unregister: %v", err)
	}
	if r.Has("doomed") {
		t.Errorf("entry still present after Unregister")
	}
	// Persistence.
	r2, _ := loadRegistryFromPath(path)
	if r2.Has("doomed") {
		t.Errorf("Unregister not persisted")
	}
}

func TestRegistry_UnregisterMissingIsNoOp(t *testing.T) {
	dir := t.TempDir()
	r, _ := loadRegistryFromPath(filepath.Join(dir, "projects.json"))
	if err := r.Unregister("never-was"); err != nil {
		t.Errorf("Unregister of unknown slug should be no-op, got: %v", err)
	}
}

func TestRegistry_SetTagUnknownSlugErrors(t *testing.T) {
	dir := t.TempDir()
	r, _ := loadRegistryFromPath(filepath.Join(dir, "projects.json"))
	if err := r.SetTag("ghost", "G"); err == nil {
		t.Errorf("expected error on SetTag for unknown slug")
	}
}

func TestRegistry_Slugs(t *testing.T) {
	dir := t.TempDir()
	r, _ := loadRegistryFromPath(filepath.Join(dir, "projects.json"))
	r.Register("b", "B")
	r.Register("a", "A")
	got := r.Slugs()
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("Slugs() = %v, want [a b]", got)
	}
}
