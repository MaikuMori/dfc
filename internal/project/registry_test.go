package project

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestRenameTag(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "projects.json")
	r, err := loadRegistryFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	r.Register("a", "A")
	_ = r.SetTags("a", []string{"work", "oss"})
	r.Register("b", "B")
	_ = r.SetTags("b", []string{"Work"}) // different casing — must still match
	r.Register("c", "C")
	_ = r.SetTags("c", []string{"personal"})

	slugs, err := r.RenameTag("work", "active", false)
	if err != nil {
		t.Fatalf("RenameTag: %v", err)
	}
	if !slices.Equal(slugs, []string{"a", "b"}) {
		t.Errorf("affected = %v, want [a b]", slugs)
	}
	if got := r.Tags("a"); !slices.Equal(got, []string{"active", "oss"}) {
		t.Errorf("a tags = %v, want [active oss]", got)
	}
	if got := r.Tags("b"); !slices.Equal(got, []string{"active"}) {
		t.Errorf("b tags = %v, want [active]", got)
	}
	if got := r.Tags("c"); !slices.Equal(got, []string{"personal"}) {
		t.Errorf("c (no match) tags = %v, want [personal]", got)
	}

	// Persisted to disk.
	r2, err := loadRegistryFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := r2.Tags("a"); !slices.Equal(got, []string{"active", "oss"}) {
		t.Errorf("persisted a tags = %v", got)
	}
}

func TestRenameTagNoOp(t *testing.T) {
	dir := t.TempDir()
	r, _ := loadRegistryFromPath(filepath.Join(dir, "projects.json"))
	r.Register("a", "A")
	_ = r.SetTags("a", []string{"work"})

	slugs, err := r.RenameTag("nonexistent", "x", false)
	if err != nil {
		t.Fatalf("no-op RenameTag: %v", err)
	}
	if len(slugs) != 0 {
		t.Errorf("no-op should affect 0 projects, got %v", slugs)
	}
}

func TestRenameTagConflict(t *testing.T) {
	dir := t.TempDir()
	r, _ := loadRegistryFromPath(filepath.Join(dir, "projects.json"))
	r.Register("a", "A")
	_ = r.SetTags("a", []string{"old", "new"})

	if _, err := r.RenameTag("old", "new", false); err == nil {
		t.Error("a project carrying both should error without --merge")
	}
	if got := r.Tags("a"); !slices.Equal(got, []string{"old", "new"}) {
		t.Errorf("a rejected conflict must not mutate; got %v", got)
	}

	slugs, err := r.RenameTag("old", "new", true)
	if err != nil {
		t.Fatalf("merge RenameTag: %v", err)
	}
	if !slices.Equal(slugs, []string{"a"}) {
		t.Errorf("merge affected = %v, want [a]", slugs)
	}
	if got := r.Tags("a"); !slices.Equal(got, []string{"new"}) {
		t.Errorf("merge should fold to [new], got %v", got)
	}
}

func TestRenameTagSelfNoOp(t *testing.T) {
	dir := t.TempDir()
	r, _ := loadRegistryFromPath(filepath.Join(dir, "projects.json"))
	r.Register("a", "A")
	_ = r.SetTags("a", []string{"work"})

	slugs, err := r.RenameTag("work", "work", false)
	if err != nil {
		t.Fatalf("self-rename: %v", err)
	}
	if len(slugs) != 0 {
		t.Errorf("byte-identical self-rename should be a no-op, affected %v", slugs)
	}
}

func TestRenameTagCaseChange(t *testing.T) {
	dir := t.TempDir()
	r, _ := loadRegistryFromPath(filepath.Join(dir, "projects.json"))
	r.Register("a", "A")
	_ = r.SetTags("a", []string{"work"})

	slugs, err := r.RenameTag("work", "Work", false) // case-only, not a merge
	if err != nil {
		t.Fatalf("case-change RenameTag: %v", err)
	}
	if !slices.Equal(slugs, []string{"a"}) {
		t.Errorf("case change affected = %v, want [a]", slugs)
	}
	if got := r.Tags("a"); !slices.Equal(got, []string{"Work"}) {
		t.Errorf("case change should rewrite to [Work], got %v", got)
	}
}

func TestRemoveTagMiddleOfList(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "projects.json")
	r, err := loadRegistryFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	r.Register("p", "P")
	if err := r.SetTags("p", []string{"a", "b", "c"}); err != nil {
		t.Fatal(err)
	}
	if err := r.RemoveTag("p", "b"); err != nil {
		t.Fatalf("RemoveTag: %v", err)
	}
	if got := r.Tags("p"); !slices.Equal(got, []string{"a", "c"}) {
		t.Errorf("in-memory Tags = %v, want [a c]", got)
	}

	// The removal must survive a reload from disk.
	r2, err := loadRegistryFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := r2.Tags("p"); !slices.Equal(got, []string{"a", "c"}) {
		t.Errorf("persisted Tags = %v, want [a c]", got)
	}
}

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

func TestDefaultPrefix(t *testing.T) {
	cases := map[string]string{
		"users-maiku-projects-dfc":  "dfc",
		"github-com-acme-widget":    "widget",
		"gitlab-com-group-sub-repo": "repo",
		"single":                    "single",
		"":                          "",
	}
	for in, want := range cases {
		if got := DefaultPrefix(in); got != want {
			t.Errorf("DefaultPrefix(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRegistry_PrefixFallsBackToDefault(t *testing.T) {
	dir := t.TempDir()
	r, _ := loadRegistryFromPath(filepath.Join(dir, "projects.json"))
	r.Register("users-x-acme", "Acme")
	if got := r.Prefix("users-x-acme"); got != "acme" {
		t.Errorf("Tag fallback = %q, want %q", got, "acme")
	}
}

func TestRegistry_SetPrefixPersists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "projects.json")
	r, _ := loadRegistryFromPath(path)
	r.Register("foo", "Foo")
	if err := r.SetPrefix("foo", "F"); err != nil {
		t.Fatal(err)
	}
	if r.Prefix("foo") != "F" {
		t.Errorf("Tag = %q after SetPrefix", r.Prefix("foo"))
	}
	r2, _ := loadRegistryFromPath(path)
	if r2.Prefix("foo") != "F" {
		t.Errorf("Tag = %q after reload", r2.Prefix("foo"))
	}
	// Clearing reverts to default.
	if err := r2.SetPrefix("foo", ""); err != nil {
		t.Fatal(err)
	}
	if r2.Prefix("foo") != "foo" {
		t.Errorf("Tag = %q after clear, want default", r2.Prefix("foo"))
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

func TestRegistry_SetPrefixUnknownSlugErrors(t *testing.T) {
	dir := t.TempDir()
	r, _ := loadRegistryFromPath(filepath.Join(dir, "projects.json"))
	if err := r.SetPrefix("ghost", "G"); err == nil {
		t.Errorf("expected error on SetPrefix for unknown slug")
	}
}

func TestRegistry_RegisterFallsBackOnNameCollision(t *testing.T) {
	dir := t.TempDir()
	r, _ := loadRegistryFromPath(filepath.Join(dir, "projects.json"))
	r.Register("alpha", "shared")
	r.Register("beta", "shared")

	if got := r.Name("alpha"); got != "shared" {
		t.Errorf("alpha name = %q, want shared", got)
	}
	if got := r.Name("beta"); got != "beta" {
		t.Errorf("beta should have fallen back to slug, got %q", got)
	}
}

func TestRegistry_RenameRejectsCollision(t *testing.T) {
	dir := t.TempDir()
	r, _ := loadRegistryFromPath(filepath.Join(dir, "projects.json"))
	r.Register("alpha", "Alpha")
	r.Register("beta", "Beta")

	if err := r.Rename("beta", "Alpha"); err == nil {
		t.Errorf("expected error renaming beta to a name already in use")
	}
	if err := r.Rename("beta", "alpha"); err == nil {
		t.Errorf("expected case-insensitive collision rejection")
	}
	if err := r.Rename("beta", "Beta"); err != nil {
		t.Errorf("renaming beta to its existing name should succeed: %v", err)
	}
	if err := r.Rename("beta", "Gamma"); err != nil {
		t.Errorf("renaming beta to a free name should succeed: %v", err)
	}
}

func TestRegistry_SetPrefixRejectsCollision(t *testing.T) {
	dir := t.TempDir()
	r, _ := loadRegistryFromPath(filepath.Join(dir, "projects.json"))
	r.Register("alpha", "Alpha")
	r.Register("beta", "Beta")
	if err := r.SetPrefix("alpha", "shared"); err != nil {
		t.Fatalf("first SetPrefix: %v", err)
	}
	if err := r.SetPrefix("beta", "shared"); err == nil {
		t.Errorf("expected error on tag collision")
	}
	if err := r.SetPrefix("beta", "SHARED"); err == nil {
		t.Errorf("expected case-insensitive tag collision rejection")
	}
}

func TestRegistry_LookupSlug(t *testing.T) {
	dir := t.TempDir()
	r, _ := loadRegistryFromPath(filepath.Join(dir, "projects.json"))
	r.Register("alpha-slug", "Alpha")
	r.Register("beta-slug", "Beta")

	got, err := r.LookupSlug("alpha-slug")
	if err != nil || got != "alpha-slug" {
		t.Errorf("slug match: got (%q,%v)", got, err)
	}

	got, err = r.LookupSlug("Alpha")
	if err != nil || got != "alpha-slug" {
		t.Errorf("name match: got (%q,%v)", got, err)
	}

	got, err = r.LookupSlug("ALPHA")
	if err != nil || got != "alpha-slug" {
		t.Errorf("case-insensitive name match: got (%q,%v)", got, err)
	}

	got, err = r.LookupSlug("nothing")
	if err != nil || got != "" {
		t.Errorf("miss should return empty without error: got (%q,%v)", got, err)
	}
}

func TestRegistry_TagsSetGet(t *testing.T) {
	dir := t.TempDir()
	r, _ := loadRegistryFromPath(filepath.Join(dir, "projects.json"))
	r.Register("alpha", "Alpha")

	if got := r.Tags("alpha"); got != nil {
		t.Errorf("fresh project should have nil tags, got %v", got)
	}
	if err := r.SetTags("alpha", []string{"work", "OSS", "work", "  client-a  ", ""}); err != nil {
		t.Fatalf("SetTags: %v", err)
	}
	got := r.Tags("alpha")
	want := []string{"work", "OSS", "client-a"}
	if !slicesEqual(got, want) {
		t.Errorf("Tags after SetTags = %v, want %v (dedupe + trim + drop empty)", got, want)
	}
	// Persistence + case-insensitive de-dupe across writes.
	r2, _ := loadRegistryFromPath(filepath.Join(dir, "projects.json"))
	if !slicesEqual(r2.Tags("alpha"), want) {
		t.Errorf("Tags after reload = %v, want %v", r2.Tags("alpha"), want)
	}
}

func TestRegistry_AddRemoveTagIdempotent(t *testing.T) {
	dir := t.TempDir()
	r, _ := loadRegistryFromPath(filepath.Join(dir, "projects.json"))
	r.Register("alpha", "Alpha")

	if err := r.AddTag("alpha", "work"); err != nil {
		t.Fatal(err)
	}
	if err := r.AddTag("alpha", "WORK"); err != nil {
		t.Fatalf("idempotent AddTag should be no-op, got %v", err)
	}
	if got := r.Tags("alpha"); !slicesEqual(got, []string{"work"}) {
		t.Errorf("after duplicate add, tags = %v, want [work]", got)
	}

	if err := r.RemoveTag("alpha", "missing"); err != nil {
		t.Errorf("RemoveTag of missing tag should be no-op, got %v", err)
	}
	if err := r.RemoveTag("alpha", "WORK"); err != nil {
		t.Fatal(err)
	}
	if got := r.Tags("alpha"); got != nil {
		t.Errorf("after remove, tags should be nil, got %v", got)
	}
}

func TestRegistry_AllTagsGroupsAcrossProjects(t *testing.T) {
	dir := t.TempDir()
	r, _ := loadRegistryFromPath(filepath.Join(dir, "projects.json"))
	r.Register("alpha", "Alpha")
	r.Register("beta", "Beta")
	r.Register("gamma", "Gamma")

	if err := r.SetTags("alpha", []string{"work", "oss"}); err != nil {
		t.Fatal(err)
	}
	if err := r.SetTags("beta", []string{"Work", "personal"}); err != nil {
		t.Fatal(err)
	}
	// gamma has no tags — should appear in UntaggedSlugs.

	summary := r.AllTags()
	// Expect three groups: oss (alpha), personal (beta), work (alpha+beta).
	if len(summary) != 3 {
		t.Fatalf("AllTags len = %d, want 3 (got %+v)", len(summary), summary)
	}
	byName := map[string]TagSummary{}
	for _, s := range summary {
		byName[strings.ToLower(s.Name)] = s
	}
	if got := byName["work"].Slugs; !slicesEqual(got, []string{"alpha", "beta"}) {
		t.Errorf("work slugs = %v, want [alpha beta]", got)
	}
	if got := byName["oss"].Slugs; !slicesEqual(got, []string{"alpha"}) {
		t.Errorf("oss slugs = %v", got)
	}
	if got := byName["personal"].Slugs; !slicesEqual(got, []string{"beta"}) {
		t.Errorf("personal slugs = %v", got)
	}
	if got := r.UntaggedSlugs(); !slicesEqual(got, []string{"gamma"}) {
		t.Errorf("UntaggedSlugs = %v, want [gamma]", got)
	}
}

func TestRegistry_TagsReturnsDefensiveCopy(t *testing.T) {
	dir := t.TempDir()
	r, _ := loadRegistryFromPath(filepath.Join(dir, "projects.json"))
	r.Register("alpha", "Alpha")
	if err := r.SetTags("alpha", []string{"work", "oss"}); err != nil {
		t.Fatal(err)
	}
	got := r.Tags("alpha")
	got[0] = "MUTATED"
	_ = append(got, "leaked") //nolint:ineffassign // verifying that appending to the returned slice doesn't leak into the registry

	// Original data should be untouched.
	again := r.Tags("alpha")
	if !slicesEqual(again, []string{"work", "oss"}) {
		t.Errorf("Tags should return a defensive copy; saw mutation leak: %v", again)
	}
}

func TestRegistry_TagsOnUnknownSlugErrors(t *testing.T) {
	dir := t.TempDir()
	r, _ := loadRegistryFromPath(filepath.Join(dir, "projects.json"))
	if err := r.SetTags("ghost", []string{"work"}); err == nil {
		t.Errorf("SetTags on unknown slug should error")
	}
	if err := r.AddTag("ghost", "work"); err == nil {
		t.Errorf("AddTag on unknown slug should error")
	}
	if err := r.RemoveTag("ghost", "work"); err == nil {
		t.Errorf("RemoveTag on unknown slug should error")
	}
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
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
