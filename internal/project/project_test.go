package project

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCanonicalRemote(t *testing.T) {
	cases := map[string]string{
		"git@github.com:user/repo.git":        "github.com/user/repo",
		"https://github.com/user/repo.git":    "github.com/user/repo",
		"https://github.com/user/repo":        "github.com/user/repo",
		"ssh://git@gitlab.com/group/repo.git": "gitlab.com/group/repo",
		"git://example.com/x.git":             "example.com/x",
		"ssh://git@host:2222/u/r.git":         "host/u/r",
	}
	for in, want := range cases {
		got, err := canonicalRemote(in)
		if err != nil {
			t.Errorf("canonicalRemote(%q) error: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("canonicalRemote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolve_Remote(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	mustRun(t, dir, "git", "init", "-q")
	mustRun(t, dir, "git", "remote", "add", "origin", "git@github.com:acme/widget.git")

	r, err := Resolve(dir)
	if err != nil {
		t.Fatal(err)
	}
	if r.Source != SourceRemote {
		t.Fatalf("source = %s, want %s", r.Source, SourceRemote)
	}
	if r.Slug != "github-com-acme-widget" {
		t.Fatalf("slug = %q, want github-com-acme-widget", r.Slug)
	}
}

func TestResolve_Toplevel(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	mustRun(t, dir, "git", "init", "-q")

	r, err := Resolve(dir)
	if err != nil {
		t.Fatal(err)
	}
	if r.Source != SourceToplevel {
		t.Fatalf("source = %s, want %s", r.Source, SourceToplevel)
	}
	if r.Slug == "" {
		t.Fatal("empty slug")
	}
}

func TestResolve_Cwd(t *testing.T) {
	dir := t.TempDir()
	// Ensure no .git anywhere up the tree by working in a sub-dir.
	sub := filepath.Join(dir, "no-git-here")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	// Trick: git won't find a repo if HOME and discovery are constrained.
	// Instead, just verify Resolve returns a non-empty slug; source may
	// be Toplevel if the temp dir happens to be inside a git repo on this
	// machine. To force Cwd we can stub PATH to drop git.
	t.Setenv("PATH", "")
	r, err := Resolve(sub)
	if err != nil {
		t.Fatal(err)
	}
	if r.Source != SourceCwd {
		t.Fatalf("source = %s, want %s", r.Source, SourceCwd)
	}
	if r.Slug == "" {
		t.Fatal("empty slug")
	}
}

func mustRun(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}
