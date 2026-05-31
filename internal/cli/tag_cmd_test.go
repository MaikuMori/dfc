package cli

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/MaikuMori/dfc/internal/project"
)

func TestTagsRenameCommand(t *testing.T) {
	setupDFCRoot(t)
	captureNamed(t, "a", "x")
	captureNamed(t, "b", "y")
	_ = (&TagsAddCmd{Project: "a", Tags: []string{"work"}}).Run()
	_ = (&TagsAddCmd{Project: "b", Tags: []string{"work"}}).Run()

	out, err := captureStdout(t, func() error {
		return (&TagsRenameCmd{Old: "work", New: "active"}).Run()
	})
	if err != nil {
		t.Fatalf("tags rename: %v", err)
	}
	if !strings.Contains(out, "2 project") {
		t.Errorf("should report 2 projects, got %q", out)
	}

	reg, err := project.LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(reg.Tags("a"), "active") || slices.Contains(reg.Tags("a"), "work") {
		t.Errorf("a should carry active not work: %v", reg.Tags("a"))
	}
}

func TestTagsRenameNoOpJSON(t *testing.T) {
	setupDFCRoot(t)
	captureNamed(t, "a", "x")

	out, err := captureStdout(t, func() error {
		return (&TagsRenameCmd{JSON: true, Old: "ghost", New: "x"}).Run()
	})
	if err != nil {
		t.Fatalf("tags rename: %v", err)
	}
	if !strings.Contains(out, `"renamed":0`) {
		t.Errorf("no-op json should report renamed:0, got %q", out)
	}
}

func TestExpandTagArgs(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"empty", nil, nil},
		{"single", []string{"work"}, []string{"work"}},
		{"comma-split", []string{"work,oss"}, []string{"work", "oss"}},
		{"repeated", []string{"work", "oss"}, []string{"work", "oss"}},
		{"mixed comma + repeated", []string{"work,oss", "personal"}, []string{"work", "oss", "personal"}},
		{"whitespace + empties", []string{"  work  , , oss ,"}, []string{"work", "oss"}},
		{"all empty drops to nil", []string{"", " , , "}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := expandTagArgs(tc.in)
			if !equalStringSlices(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMatchesTagFilter(t *testing.T) {
	dir := t.TempDir()
	regPath := filepath.Join(dir, "projects.json")
	reg := mustLoadRegistry(t, regPath)
	reg.Register("acme", "Acme")
	reg.Register("beta", "Beta")
	reg.Register("untagged-proj", "untagged-proj")
	if err := reg.SetTags("acme", []string{"work", "oss"}); err != nil {
		t.Fatal(err)
	}
	if err := reg.SetTags("beta", []string{"work", "personal"}); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name   string
		slug   string
		filter []string
		want   bool
	}{
		{"empty filter matches everything", "acme", nil, true},
		{"OR: work matches acme", "acme", []string{"work"}, true},
		{"OR: work matches beta", "beta", []string{"work"}, true},
		{"OR: nonexistent tag doesn't match", "acme", []string{"missing"}, false},
		{"OR: multiple tags union", "beta", []string{"oss", "personal"}, true},
		{"case-insensitive name match", "acme", []string{"WORK"}, true},
		{"(untagged) special case matches empty-tag projects", "untagged-proj", []string{"(untagged)"}, true},
		{"(untagged) does NOT match tagged projects", "acme", []string{"(untagged)"}, false},
		{"(untagged) + regular tag: tagged project matches via regular", "acme", []string{"(untagged)", "work"}, true},
		{"(untagged) + regular tag: untagged project still matches via untagged", "untagged-proj", []string{"(untagged)", "work"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := matchesTagFilter(reg, tc.slug, tc.filter); got != tc.want {
				t.Errorf("matchesTagFilter(%q, %v) = %v, want %v", tc.slug, tc.filter, got, tc.want)
			}
		})
	}
}

func TestProjectSlugsMatchingTags(t *testing.T) {
	dir := t.TempDir()
	reg := mustLoadRegistry(t, filepath.Join(dir, "projects.json"))
	reg.Register("acme", "Acme")
	reg.Register("beta", "Beta")
	reg.Register("gamma", "Gamma")
	if err := reg.SetTags("acme", []string{"work"}); err != nil {
		t.Fatal(err)
	}
	if err := reg.SetTags("beta", []string{"work", "personal"}); err != nil {
		t.Fatal(err)
	}

	// Empty filter returns all slugs (alphabetical via Registry.Slugs).
	got := projectSlugsMatchingTags(reg, nil)
	if !equalStringSlices(got, []string{"acme", "beta", "gamma"}) {
		t.Errorf("empty filter: got %v", got)
	}

	got = projectSlugsMatchingTags(reg, []string{"work"})
	if !equalStringSlices(got, []string{"acme", "beta"}) {
		t.Errorf("work filter: got %v", got)
	}

	got = projectSlugsMatchingTags(reg, []string{"(untagged)"})
	if !equalStringSlices(got, []string{"gamma"}) {
		t.Errorf("(untagged) filter: got %v", got)
	}

	got = projectSlugsMatchingTags(reg, []string{"personal"})
	if !equalStringSlices(got, []string{"beta"}) {
		t.Errorf("personal filter: got %v", got)
	}

	got = projectSlugsMatchingTags(reg, []string{"none-such"})
	if len(got) != 0 {
		t.Errorf("nonexistent tag should return no slugs, got %v", got)
	}
}

func TestPluralS(t *testing.T) {
	if pluralS(0) != "s" || pluralS(1) != "" || pluralS(2) != "s" {
		t.Errorf("pluralS edge cases: 0=%q 1=%q 2=%q", pluralS(0), pluralS(1), pluralS(2))
	}
}

func mustLoadRegistry(t *testing.T, path string) *project.Registry {
	t.Helper()
	t.Setenv("DFC_ROOT", filepath.Dir(path))
	r, err := project.LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	return r
}

func equalStringSlices(a, b []string) bool {
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
