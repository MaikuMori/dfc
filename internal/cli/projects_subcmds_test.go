package cli

import (
	"strings"
	"testing"

	"github.com/MaikuMori/dfc/internal/core"
	"github.com/MaikuMori/dfc/internal/project"
	"github.com/MaikuMori/dfc/internal/storage"
)

func openMutCore(t *testing.T) *core.Core {
	t.Helper()
	cr, err := core.Open(core.Options{Warn: func(string, error) {}})
	if err != nil {
		t.Fatalf("core.Open: %v", err)
	}
	return cr
}

func TestProjectsRename_ChangesName(t *testing.T) {
	setupDFCRoot(t)
	captureNamed(t, "acme", "x")

	out, err := captureStdout(t, func() error {
		return (&ProjectsRenameCmd{Project: "acme", Name: "Acme Corp"}).Run()
	})
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if !strings.Contains(out, "Acme Corp") {
		t.Errorf("output should mention the new name, got %q", out)
	}

	cr := openMutCore(t)
	defer func() { _ = cr.Close() }()
	if got := cr.Registry().Name("acme"); got != "Acme Corp" {
		t.Errorf("name = %q, want Acme Corp", got)
	}
}

func TestProjectsRename_JSON(t *testing.T) {
	setupDFCRoot(t)
	captureNamed(t, "acme", "x")
	out, err := captureStdout(t, func() error {
		return (&ProjectsRenameCmd{JSON: true, Project: "acme", Name: "Acme Corp"}).Run()
	})
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if !strings.Contains(out, `"name":"Acme Corp"`) || !strings.Contains(out, `"slug":"acme"`) {
		t.Errorf("json should carry slug+name, got %q", out)
	}
}

func TestProjectsRename_UnknownProject(t *testing.T) {
	setupDFCRoot(t)
	if _, err := captureStdout(t, func() error {
		return (&ProjectsRenameCmd{Project: "ghost", Name: "X"}).Run()
	}); err == nil {
		t.Error("renaming an unknown project should error")
	}
}

func TestProjectsRename_NameCollision(t *testing.T) {
	setupDFCRoot(t)
	captureNamed(t, "acme", "x")
	captureNamed(t, "beta", "y")
	_, err := captureStdout(t, func() error {
		return (&ProjectsRenameCmd{Project: "acme", Name: "beta"}).Run()
	})
	if err == nil || !strings.Contains(err.Error(), "already used") {
		t.Errorf("expected a name-collision error, got %v", err)
	}
}

func TestProjectsSetPrefix_ChangesAndReverts(t *testing.T) {
	setupDFCRoot(t)
	captureNamed(t, "acme", "x")

	if _, err := captureStdout(t, func() error {
		return (&ProjectsSetPrefixCmd{Project: "acme", Prefix: "ac"}).Run()
	}); err != nil {
		t.Fatalf("set-prefix: %v", err)
	}
	cr := openMutCore(t)
	if got := cr.Registry().Prefix("acme"); got != "ac" {
		t.Errorf("prefix = %q, want ac", got)
	}
	_ = cr.Close()

	// Empty prefix reverts to the derived default.
	if _, err := captureStdout(t, func() error {
		return (&ProjectsSetPrefixCmd{Project: "acme", Prefix: ""}).Run()
	}); err != nil {
		t.Fatalf("revert: %v", err)
	}
	cr2 := openMutCore(t)
	defer func() { _ = cr2.Close() }()
	if got := cr2.Registry().Prefix("acme"); got != project.DefaultPrefix("acme") {
		t.Errorf("prefix after revert = %q, want derived default %q", got, project.DefaultPrefix("acme"))
	}
}

func TestProjectsSetPrefix_Collision(t *testing.T) {
	setupDFCRoot(t)
	captureNamed(t, "acme", "x")
	captureNamed(t, "beta", "y")

	if _, err := captureStdout(t, func() error {
		return (&ProjectsSetPrefixCmd{Project: "acme", Prefix: "shared"}).Run()
	}); err != nil {
		t.Fatalf("first set-prefix: %v", err)
	}
	_, err := captureStdout(t, func() error {
		return (&ProjectsSetPrefixCmd{Project: "beta", Prefix: "shared"}).Run()
	})
	if err == nil || !strings.Contains(err.Error(), "already used") {
		t.Errorf("expected a prefix-collision error, got %v", err)
	}
}

func TestProjectsMerge_FoldsAndRemovesSource(t *testing.T) {
	setupDFCRoot(t)
	captureNamed(t, "src", "a")
	captureNamed(t, "src", "b")
	captureNamed(t, "dst", "c")

	out, err := captureStdout(t, func() error {
		return (&ProjectsMergeCmd{Src: "src", Dst: "dst"}).Run()
	})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if !strings.Contains(out, "2 task") {
		t.Errorf("output should report 2 moved, got %q", out)
	}

	cr := openMutCore(t)
	defer func() { _ = cr.Close() }()
	if dst, _ := cr.List("dst"); len(dst) != 3 {
		t.Errorf("dst should hold 3 tasks, got %d", len(dst))
	}
	if storage.ProjectDirExists("src") {
		t.Errorf("src dir should be gone after merge")
	}
	if cr.Registry().Has("src") {
		t.Errorf("src should be deregistered after merge")
	}
}

func TestProjectsMerge_SrcEqualsDst(t *testing.T) {
	setupDFCRoot(t)
	captureNamed(t, "acme", "x")
	_, err := captureStdout(t, func() error {
		return (&ProjectsMergeCmd{Src: "acme", Dst: "acme"}).Run()
	})
	if err == nil || !strings.Contains(err.Error(), "same project") {
		t.Errorf("merging a project into itself should error, got %v", err)
	}
}

func TestProjectsMerge_UnknownSrc(t *testing.T) {
	setupDFCRoot(t)
	captureNamed(t, "dst", "x")
	if _, err := captureStdout(t, func() error {
		return (&ProjectsMergeCmd{Src: "ghost", Dst: "dst"}).Run()
	}); err == nil {
		t.Error("merging an unknown source should error")
	}
}

func TestProjectsMerge_DstFilenameCollision(t *testing.T) {
	setupDFCRoot(t)
	// Same description in both projects → same filename stem; the move must
	// disambiguate rather than overwrite.
	captureNamed(t, "src", "dup")
	captureNamed(t, "dst", "dup")

	cr := openMutCore(t)
	srcTasks, _ := cr.List("src")
	srcID := srcTasks[0].ID
	_ = cr.Close()

	if _, err := captureStdout(t, func() error {
		return (&ProjectsMergeCmd{Src: "src", Dst: "dst"}).Run()
	}); err != nil {
		t.Fatalf("merge: %v", err)
	}

	cr2 := openMutCore(t)
	defer func() { _ = cr2.Close() }()
	if dst, _ := cr2.List("dst"); len(dst) != 2 {
		t.Fatalf("dst should hold both tasks, got %d", len(dst))
	}
	if _, err := cr2.Show(srcID); err != nil {
		t.Errorf("the moved task should still resolve by id: %v", err)
	}
}

func TestProjectsMerge_EmptyProject(t *testing.T) {
	setupDFCRoot(t)
	captureNamed(t, "src", "only")
	captureNamed(t, "dst", "keep")

	// Empty src by trashing its sole task, leaving the dir present-but-empty.
	cr := openMutCore(t)
	srcTasks, _ := cr.List("src")
	if _, _, _, err := cr.RemoveInProject(srcTasks[0].ID, "src"); err != nil {
		t.Fatal(err)
	}
	_ = cr.Close()

	out, err := captureStdout(t, func() error {
		return (&ProjectsMergeCmd{Src: "src", Dst: "dst"}).Run()
	})
	if err != nil {
		t.Fatalf("merge empty: %v", err)
	}
	if !strings.Contains(out, "0 task") {
		t.Errorf("empty merge should report 0 moved, got %q", out)
	}
	cr2 := openMutCore(t)
	defer func() { _ = cr2.Close() }()
	if cr2.Registry().Has("src") {
		t.Errorf("empty src should be deregistered")
	}
}
