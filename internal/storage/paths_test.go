package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProjectDirExists(t *testing.T) {
	root := t.TempDir()
	t.Setenv(EnvRoot, root)

	if ProjectDirExists("never-created") {
		t.Fatal("expected false for missing slug")
	}
	// Probing must NOT create the directory.
	if _, err := os.Stat(filepath.Join(root, "projects", "never-created")); !os.IsNotExist(err) {
		t.Fatalf("ProjectDirExists should not create; got %v", err)
	}

	// Create via Open and probe again.
	if _, err := Open("real"); err != nil {
		t.Fatal(err)
	}
	if !ProjectDirExists("real") {
		t.Fatal("expected true after Open")
	}
	// Empty slug → false, no crash.
	if ProjectDirExists("") {
		t.Fatal("empty slug should be false")
	}
}

func TestRegistryPath(t *testing.T) {
	root := t.TempDir()
	t.Setenv(EnvRoot, root)
	want := filepath.Join(root, "projects.json")
	if got := RegistryPath(); got != want {
		t.Errorf("RegistryPath() = %q, want %q", got, want)
	}
}

// mockHome unsets DFC_ROOT and points HOME/USERPROFILE at a temp dir so
// os.UserHomeDir() returns it on every supported platform.
func mockHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv(EnvRoot, "")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

func TestRootDefaultsToHomeDfc(t *testing.T) {
	home := mockHome(t)

	got, err := Root()
	if err != nil {
		t.Fatalf("Root: %v", err)
	}
	want := filepath.Join(home, ".dfc")
	if got != want {
		t.Errorf("Root() = %q, want %q", got, want)
	}
	info, err := os.Stat(got)
	if err != nil {
		t.Fatalf("Root() did not create %q: %v", got, err)
	}
	if !info.IsDir() {
		t.Errorf("%q exists but is not a directory", got)
	}
}

func TestProjectsRootDefaultsToHomeDfc(t *testing.T) {
	home := mockHome(t)
	want := filepath.Join(home, ".dfc", "projects")
	if got := projectsRoot(); got != want {
		t.Errorf("projectsRoot() = %q, want %q", got, want)
	}
}

func TestRegistryPathDefaultsToHomeDfc(t *testing.T) {
	home := mockHome(t)
	want := filepath.Join(home, ".dfc", "projects.json")
	if got := RegistryPath(); got != want {
		t.Errorf("RegistryPath() = %q, want %q", got, want)
	}
}

func TestProjectDirDefaultsToHomeDfc(t *testing.T) {
	home := mockHome(t)
	got, err := ProjectDir("acme-api")
	if err != nil {
		t.Fatalf("ProjectDir: %v", err)
	}
	want := filepath.Join(home, ".dfc", "projects", "acme-api")
	if got != want {
		t.Errorf("ProjectDir() = %q, want %q", got, want)
	}
	if _, err := os.Stat(got); err != nil {
		t.Errorf("ProjectDir() did not create %q: %v", got, err)
	}
}

func TestProjectDirRejectsEmptySlug(t *testing.T) {
	mockHome(t)
	if _, err := ProjectDir(""); err == nil {
		t.Error("expected error for empty slug")
	}
}

func TestProjectDirExistsHandlesMissingHome(t *testing.T) {
	// With both env vars cleared, UserHomeDir falls back to getpwuid on
	// Unix — which usually succeeds. The point of this test is just to
	// confirm ProjectDirExists never panics regardless of resolution result.
	t.Setenv(EnvRoot, "")
	if ProjectDirExists("nope") {
		t.Error("unexpected ProjectDirExists=true for non-existent slug")
	}
}
