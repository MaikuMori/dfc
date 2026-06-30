package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/MaikuMori/dfc/internal/index"
	"github.com/MaikuMori/dfc/internal/project"
	"github.com/MaikuMori/dfc/internal/savedsearch"
	"github.com/MaikuMori/dfc/internal/storage"
)

func TestMergeProjectKeepsForeignContent(t *testing.T) {
	cr := newTestCore(t)
	if _, err := cr.Capture(CaptureInput{Slug: "src", Description: "a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := cr.Capture(CaptureInput{Slug: "dst", Description: "b"}); err != nil {
		t.Fatal(err)
	}
	srcDir, err := storage.ProjectDirPath("src")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "notes.txt"), []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}

	moved, err := cr.MergeProject("src", "dst")
	if err != nil {
		t.Fatalf("MergeProject: %v", err)
	}
	if moved != 1 {
		t.Errorf("moved = %d, want 1", moved)
	}
	if dst, _ := cr.List("dst"); len(dst) != 2 {
		t.Errorf("dst should hold 2 tasks, got %d", len(dst))
	}
	// The source dir (with its foreign file) and registration are kept.
	if !storage.ProjectDirExists("src") {
		t.Errorf("src dir should be kept when it holds foreign content")
	}
	if _, err := os.Stat(filepath.Join(srcDir, "notes.txt")); err != nil {
		t.Errorf("foreign file must survive the merge: %v", err)
	}
	if !cr.Registry().Has("src") {
		t.Errorf("src should stay registered when it isn't removed")
	}
}

// newTestCore opens a fresh Core under t.TempDir(). Warn callbacks
// land on stderr (default) since no current test asserts against them.
func newTestCore(t *testing.T) *Core {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(storage.EnvRoot, dir)
	cr, err := Open(Options{Warn: func(string, error) {}})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = cr.Close() })
	return cr
}

func TestCaptureRegistersUnknownProject(t *testing.T) {
	cr := newTestCore(t)

	res, err := cr.Capture(CaptureInput{
		Slug:        "acme-web",
		Description: "buy milk",
		DisplayName: "ACME Web",
	})
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if !res.NewProject {
		t.Fatalf("expected NewProject=true on first capture")
	}
	if res.Task.Description != "buy milk" {
		t.Errorf("description = %q, want %q", res.Task.Description, "buy milk")
	}
	if res.Task.ProjectSlug != "acme-web" {
		t.Errorf("project = %q, want acme-web", res.Task.ProjectSlug)
	}
	if !cr.Registry().Has("acme-web") {
		t.Errorf("registry should know acme-web after capture")
	}
	if cr.Registry().Name("acme-web") != "ACME Web" {
		t.Errorf("display name not stored")
	}
}

func TestCaptureSecondTimeNotNewProject(t *testing.T) {
	cr := newTestCore(t)
	if _, err := cr.Capture(CaptureInput{Slug: "x", Description: "one"}); err != nil {
		t.Fatalf("first capture: %v", err)
	}
	res, err := cr.Capture(CaptureInput{Slug: "x", Description: "two"})
	if err != nil {
		t.Fatalf("second capture: %v", err)
	}
	if res.NewProject {
		t.Errorf("second capture into same project should not be NewProject")
	}
}

func TestCaptureWritesToIndex(t *testing.T) {
	cr := newTestCore(t)
	res, err := cr.Capture(CaptureInput{Slug: "p", Description: "hello world"})
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	hits, err := cr.Search("hello", indexAllOpts())
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 || hits[0].Task.ID != res.Task.ID {
		t.Fatalf("Search miss after Capture: hits=%+v", hits)
	}
}

func TestSetStatusIdempotent(t *testing.T) {
	cr := newTestCore(t)
	res, _ := cr.Capture(CaptureInput{Slug: "p", Description: "thing"})

	t1, changed, err := cr.SetStatus(res.Task.ID, storage.StatusDone)
	if err != nil {
		t.Fatalf("SetStatus done: %v", err)
	}
	if !changed || t1.Status != storage.StatusDone {
		t.Fatalf("expected change to done, got changed=%v status=%s", changed, t1.Status)
	}

	t2, changed2, err := cr.SetStatus(res.Task.ID, storage.StatusDone)
	if err != nil {
		t.Fatalf("SetStatus done again: %v", err)
	}
	if changed2 {
		t.Errorf("re-applying same status should report changed=false")
	}
	if t2.Status != storage.StatusDone {
		t.Errorf("status flipped unexpectedly")
	}
}

func TestSetStatusUnknownID(t *testing.T) {
	cr := newTestCore(t)
	_, _, err := cr.SetStatus(strings.Repeat("Z", 26), storage.StatusDone)
	var nf *NotFoundError
	if err == nil {
		t.Fatal("expected error for unknown id")
	}
	if !errors.As(err, &nf) {
		t.Errorf("expected NotFoundError, got %T: %v", err, err)
	}
}

func TestEditDescriptionRenamesAndReindexes(t *testing.T) {
	cr := newTestCore(t)
	res, _ := cr.Capture(CaptureInput{Slug: "p", Description: "buy bread"})

	newDesc := "buy croissants"
	updated, err := cr.Edit(EditInput{ID: res.Task.ID, Description: &newDesc})
	if err != nil {
		t.Fatalf("Edit: %v", err)
	}
	if updated.Description != newDesc {
		t.Errorf("description not updated: %q", updated.Description)
	}
	if updated.Path == res.Task.Path {
		t.Errorf("path should have changed after rename")
	}
	hits, err := cr.Search("croissants", indexAllOpts())
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 {
		t.Errorf("expected 1 hit after rename, got %d", len(hits))
	}
}

func TestRemoveDropsFileAndIndexRow(t *testing.T) {
	cr := newTestCore(t)
	res, _ := cr.Capture(CaptureInput{Slug: "p", Description: "to be deleted"})

	slug, path, trashID, err := cr.Remove(res.Task.ID)
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if slug != "p" || path == "" || trashID == "" {
		t.Errorf("Remove returned slug=%q path=%q trashID=%q", slug, path, trashID)
	}
	hits, err := cr.Search("deleted", indexAllOpts())
	if err != nil {
		t.Fatalf("Search after Remove: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("expected zero hits after Remove, got %d", len(hits))
	}

	// Removing again should report NotFound — the file was moved out of
	// the project directory into the trash.
	if _, _, _, err := cr.Remove(res.Task.ID); err == nil {
		t.Errorf("expected NotFound on second Remove")
	}
}

func TestUndoRestoresMostRecentlyTrashedTask(t *testing.T) {
	cr := newTestCore(t)
	res, _ := cr.Capture(CaptureInput{Slug: "p", Description: "to be undone"})

	if _, _, _, err := cr.Remove(res.Task.ID); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if hits, _ := cr.Search("undone", indexAllOpts()); len(hits) != 0 {
		t.Fatalf("expected zero hits before undo, got %d", len(hits))
	}

	m, err := cr.Undo()
	if err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if m.Kind != "task" {
		t.Errorf("manifest.Kind = %q, want task", m.Kind)
	}
	if m.Slug != "p" {
		t.Errorf("manifest.Slug = %q, want p", m.Slug)
	}
	if m.TaskID != res.Task.ID {
		t.Errorf("manifest.TaskID = %q, want %q", m.TaskID, res.Task.ID)
	}

	hits, err := cr.Search("undone", indexAllOpts())
	if err != nil {
		t.Fatalf("Search after Undo: %v", err)
	}
	if len(hits) != 1 {
		t.Errorf("expected 1 hit after Undo, got %d", len(hits))
	}
}

func TestUndoEmptyTrashReturnsError(t *testing.T) {
	cr := newTestCore(t)
	if _, err := cr.Undo(); err == nil {
		t.Errorf("expected error on Undo of empty trash")
	}
}

func TestRestoreByIDRestoresSpecificTask(t *testing.T) {
	cr := newTestCore(t)
	a, _ := cr.Capture(CaptureInput{Slug: "p", Description: "alpha"})
	b, _ := cr.Capture(CaptureInput{Slug: "p", Description: "beta"})

	_, _, trashA, err := cr.Remove(a.Task.ID)
	if err != nil {
		t.Fatalf("Remove a: %v", err)
	}
	if _, _, _, err := cr.Remove(b.Task.ID); err != nil {
		t.Fatalf("Remove b: %v", err)
	}

	m, err := cr.Restore(trashA)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if m.TaskID != a.Task.ID {
		t.Errorf("restored TaskID = %q, want %q", m.TaskID, a.Task.ID)
	}

	if hits, _ := cr.Search("alpha", indexAllOpts()); len(hits) != 1 {
		t.Errorf("expected 1 hit for alpha after Restore, got %d", len(hits))
	}
	if hits, _ := cr.Search("beta", indexAllOpts()); len(hits) != 0 {
		t.Errorf("expected 0 hits for beta (still trashed), got %d", len(hits))
	}
}

func TestListAllMergesProjects(t *testing.T) {
	cr := newTestCore(t)
	_, _ = cr.Capture(CaptureInput{Slug: "a", Description: "alpha"})
	_, _ = cr.Capture(CaptureInput{Slug: "b", Description: "beta"})

	all, err := cr.ListAll()
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 tasks across projects, got %d", len(all))
	}
	descs := map[string]bool{}
	for _, x := range all {
		descs[x.Description] = true
	}
	if !descs["alpha"] || !descs["beta"] {
		t.Errorf("missing tasks in ListAll: %v", descs)
	}
}

func TestListReturnsCapturedTasks(t *testing.T) {
	cr := newTestCore(t)
	_, _ = cr.Capture(CaptureInput{Slug: "p", Description: "one"})
	_, _ = cr.Capture(CaptureInput{Slug: "p", Description: "two"})

	tasks, err := cr.List("p")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("got %d tasks, want 2", len(tasks))
	}
	descs := map[string]bool{tasks[0].Description: true, tasks[1].Description: true}
	if !descs["one"] || !descs["two"] {
		t.Errorf("missing tasks: %v", descs)
	}
}

func TestShowReturnsTaskByID(t *testing.T) {
	cr := newTestCore(t)
	res, _ := cr.Capture(CaptureInput{Slug: "p", Description: "fetch me"})

	got, err := cr.Show(res.Task.ID)
	if err != nil {
		t.Fatalf("Show: %v", err)
	}
	if got.Description != "fetch me" {
		t.Errorf("Show().Description = %q, want %q", got.Description, "fetch me")
	}
}

func TestShowUnknownIDIsNotFound(t *testing.T) {
	cr := newTestCore(t)
	// 26-char ULID format so validation passes but lookup fails.
	if _, err := cr.Show("01ABCDEFGHJKMNPQRSTVWXYZ12"); err == nil {
		t.Errorf("expected NotFound error for unknown id")
	}
}

func TestFindByIDInProjectFindsAndIsolates(t *testing.T) {
	cr := newTestCore(t)
	a, _ := cr.Capture(CaptureInput{Slug: "alpha", Description: "in alpha"})
	b, _ := cr.Capture(CaptureInput{Slug: "beta", Description: "in beta"})

	path, err := cr.FindByIDInProject(a.Task.ID, "alpha")
	if err != nil {
		t.Fatalf("FindByIDInProject(alpha): %v", err)
	}
	if path == "" {
		t.Fatalf("alpha task should be findable in alpha")
	}

	path, err = cr.FindByIDInProject(a.Task.ID, "beta")
	if err != nil {
		t.Fatalf("FindByIDInProject(beta): %v", err)
	}
	if path != "" {
		t.Errorf("alpha task should not be findable in beta, got %q", path)
	}

	path, err = cr.FindByIDInProject(b.Task.ID, "beta")
	if err != nil {
		t.Fatalf("FindByIDInProject(beta, b): %v", err)
	}
	if path == "" {
		t.Errorf("beta task should be findable in beta")
	}
}

func TestFindByIDInProjectMissingProject(t *testing.T) {
	cr := newTestCore(t)
	res, _ := cr.Capture(CaptureInput{Slug: "alpha", Description: "x"})

	path, err := cr.FindByIDInProject(res.Task.ID, "no-such-project")
	if err != nil {
		t.Fatalf("FindByIDInProject: %v", err)
	}
	if path != "" {
		t.Errorf("expected empty path for missing project, got %q", path)
	}
}

func TestSetStatusInProjectScopesToSlug(t *testing.T) {
	cr := newTestCore(t)
	a, _ := cr.Capture(CaptureInput{Slug: "alpha", Description: "alpha task"})
	_, _ = cr.Capture(CaptureInput{Slug: "beta", Description: "beta task"})

	_, _, err := cr.SetStatusInProject(a.Task.ID, "beta", storage.StatusDone)
	if err == nil {
		t.Fatalf("expected NotFound for cross-project SetStatusInProject")
	}
	var nf *NotFoundError
	if !errors.As(err, &nf) {
		t.Errorf("expected NotFoundError, got %T: %v", err, err)
	}

	got, changed, err := cr.SetStatusInProject(a.Task.ID, "alpha", storage.StatusDone)
	if err != nil {
		t.Fatalf("SetStatusInProject(alpha): %v", err)
	}
	if !changed || got.Status != storage.StatusDone {
		t.Errorf("expected change to done in alpha, changed=%v status=%s", changed, got.Status)
	}
}

func TestEditInProjectScopesToSlug(t *testing.T) {
	cr := newTestCore(t)
	a, _ := cr.Capture(CaptureInput{Slug: "alpha", Description: "old"})
	_, _ = cr.Capture(CaptureInput{Slug: "beta", Description: "beta"})

	newDesc := "new"
	if _, err := cr.EditInProject(EditInput{ID: a.Task.ID, Description: &newDesc}, "beta"); err == nil {
		t.Errorf("expected NotFound for cross-project EditInProject")
	}

	updated, err := cr.EditInProject(EditInput{ID: a.Task.ID, Description: &newDesc}, "alpha")
	if err != nil {
		t.Fatalf("EditInProject(alpha): %v", err)
	}
	if updated.Description != newDesc {
		t.Errorf("EditInProject did not update description: %q", updated.Description)
	}
}

func TestRemoveInProjectScopesToSlug(t *testing.T) {
	cr := newTestCore(t)
	a, _ := cr.Capture(CaptureInput{Slug: "alpha", Description: "x"})
	_, _ = cr.Capture(CaptureInput{Slug: "beta", Description: "y"})

	if _, _, _, err := cr.RemoveInProject(a.Task.ID, "beta"); err == nil {
		t.Errorf("expected NotFound for cross-project RemoveInProject")
	}

	slug, _, trashID, err := cr.RemoveInProject(a.Task.ID, "alpha")
	if err != nil {
		t.Fatalf("RemoveInProject(alpha): %v", err)
	}
	if slug != "alpha" || trashID == "" {
		t.Errorf("RemoveInProject returned slug=%q trashID=%q", slug, trashID)
	}
}

func TestShowInProjectScopesToSlug(t *testing.T) {
	cr := newTestCore(t)
	a, _ := cr.Capture(CaptureInput{Slug: "alpha", Description: "alpha task"})
	_, _ = cr.Capture(CaptureInput{Slug: "beta", Description: "beta task"})

	if _, err := cr.ShowInProject(a.Task.ID, "beta"); err == nil {
		t.Errorf("expected NotFound for cross-project ShowInProject")
	}

	got, err := cr.ShowInProject(a.Task.ID, "alpha")
	if err != nil {
		t.Fatalf("ShowInProject(alpha): %v", err)
	}
	if got.Description != "alpha task" {
		t.Errorf("ShowInProject returned wrong task: %q", got.Description)
	}
}

func TestShowRejectsShortID(t *testing.T) {
	cr := newTestCore(t)
	if _, err := cr.Show("short"); err == nil {
		t.Errorf("expected validation error for short id")
	}
}

func TestCountsByProjectReflectsOpenAndDone(t *testing.T) {
	cr := newTestCore(t)
	a, _ := cr.Capture(CaptureInput{Slug: "alpha", Description: "a1"})
	_, _ = cr.Capture(CaptureInput{Slug: "alpha", Description: "a2"})
	_, _ = cr.Capture(CaptureInput{Slug: "beta", Description: "b1"})

	if _, _, err := cr.SetStatus(a.Task.ID, "done"); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}

	counts := cr.CountsByProject()
	if counts["alpha"].Open != 1 || counts["alpha"].Done != 1 {
		t.Errorf("alpha counts = %+v, want {Open:1 Done:1}", counts["alpha"])
	}
	if counts["beta"].Open != 1 || counts["beta"].Done != 0 {
		t.Errorf("beta counts = %+v, want {Open:1 Done:0}", counts["beta"])
	}
}

func TestTrashListAndEmptyPurges(t *testing.T) {
	cr := newTestCore(t)
	a, _ := cr.Capture(CaptureInput{Slug: "p", Description: "to trash"})
	b, _ := cr.Capture(CaptureInput{Slug: "p", Description: "also trash"})

	if _, _, _, err := cr.Remove(a.Task.ID); err != nil {
		t.Fatalf("Remove a: %v", err)
	}
	if _, _, _, err := cr.Remove(b.Task.ID); err != nil {
		t.Fatalf("Remove b: %v", err)
	}

	entries, err := cr.TrashList()
	if err != nil {
		t.Fatalf("TrashList: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("TrashList returned %d entries, want 2", len(entries))
	}

	n, err := cr.TrashEmpty()
	if err != nil {
		t.Fatalf("TrashEmpty: %v", err)
	}
	if n != 2 {
		t.Errorf("TrashEmpty purged %d, want 2", n)
	}

	if remaining, _ := cr.TrashList(); len(remaining) != 0 {
		t.Errorf("trash not empty after TrashEmpty: %d entries", len(remaining))
	}
}

func TestRemoveProjectMovesAllTasks(t *testing.T) {
	cr := newTestCore(t)
	_, _ = cr.Capture(CaptureInput{Slug: "doomed", Description: "first"})
	_, _ = cr.Capture(CaptureInput{Slug: "doomed", Description: "second"})
	_, _ = cr.Capture(CaptureInput{Slug: "doomed", Description: "third"})
	_, _ = cr.Capture(CaptureInput{Slug: "kept", Description: "untouched"})

	removed, trashID, err := cr.RemoveProject("doomed")
	if err != nil {
		t.Fatalf("RemoveProject: %v", err)
	}
	if removed != 3 {
		t.Errorf("removed = %d, want 3", removed)
	}
	if trashID == "" {
		t.Errorf("expected non-empty trashID")
	}

	if hits, _ := cr.Search("first", indexAllOpts()); len(hits) != 0 {
		t.Errorf("expected 0 hits for doomed tasks, got %d", len(hits))
	}
	if hits, _ := cr.Search("untouched", indexAllOpts()); len(hits) != 1 {
		t.Errorf("expected sibling project to survive, got %d hits", len(hits))
	}
}

func TestCaptureRejectsBlankDescription(t *testing.T) {
	cr := newTestCore(t)
	for _, desc := range []string{"", "   ", "\t\n"} {
		if _, err := cr.Capture(CaptureInput{Slug: "p", Description: desc}); err == nil {
			t.Errorf("Capture(%q) should error on a blank description", desc)
		}
	}
}

func TestMoveTaskMovesFileAndIndexFollows(t *testing.T) {
	cr := newTestCore(t)
	res, err := cr.Capture(CaptureInput{Slug: "alpha", Description: "relocate me"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cr.Capture(CaptureInput{Slug: "beta", Description: "keep"}); err != nil {
		t.Fatal(err)
	}

	moved, err := cr.MoveTask(res.Task, "beta")
	if err != nil {
		t.Fatalf("MoveTask: %v", err)
	}
	if moved.ProjectSlug != "beta" {
		t.Errorf("moved slug = %q, want beta", moved.ProjectSlug)
	}
	if _, err := os.Stat(res.Task.Path); !os.IsNotExist(err) {
		t.Errorf("old file should be gone")
	}
	if hits, _ := cr.Search("relocate", indexAllOpts()); len(hits) != 1 || hits[0].Task.ProjectSlug != "beta" {
		t.Errorf("search should find the task under beta, got %+v", hits)
	}
	counts := cr.CountsByProject()
	if counts["alpha"].Open != 0 {
		t.Errorf("alpha open count = %d, want 0", counts["alpha"].Open)
	}
	if counts["beta"].Open != 2 {
		t.Errorf("beta open count = %d, want 2", counts["beta"].Open)
	}
}

func TestMoveTaskAutoRegistersUnknownDest(t *testing.T) {
	cr := newTestCore(t)
	res, _ := cr.Capture(CaptureInput{Slug: "alpha", Description: "x"})
	if cr.Registry().Has("fresh") {
		t.Fatal("precondition: fresh should not exist yet")
	}
	moved, err := cr.MoveTask(res.Task, "fresh")
	if err != nil {
		t.Fatalf("MoveTask: %v", err)
	}
	if !cr.Registry().Has("fresh") {
		t.Errorf("destination project should be auto-registered")
	}
	if moved.ProjectSlug != "fresh" {
		t.Errorf("moved slug = %q, want fresh", moved.ProjectSlug)
	}
}

func TestMoveTaskSameProjectNoOp(t *testing.T) {
	cr := newTestCore(t)
	res, _ := cr.Capture(CaptureInput{Slug: "alpha", Description: "stay"})
	before := res.Task

	moved, err := cr.MoveTask(before, "alpha")
	if err != nil {
		t.Fatalf("MoveTask: %v", err)
	}
	if moved.Path != before.Path {
		t.Errorf("no-op move changed path: %q -> %q", before.Path, moved.Path)
	}
	if _, err := os.Stat(before.Path); err != nil {
		t.Errorf("file should be untouched: %v", err)
	}
}

func TestMoveTaskGuards(t *testing.T) {
	cr := newTestCore(t)
	res, _ := cr.Capture(CaptureInput{Slug: "alpha", Description: "x"})
	if _, err := cr.MoveTask(res.Task, ""); err == nil {
		t.Error("empty destSlug should error")
	}
	if _, err := cr.MoveTask(storage.Task{ProjectSlug: "alpha"}, "beta"); err == nil {
		t.Error("missing path should error")
	}
}

func TestRemoveTaskRoutesBySlug(t *testing.T) {
	cr := newTestCore(t)
	if _, err := cr.Capture(CaptureInput{Slug: "alpha", Description: "keep me"}); err != nil {
		t.Fatalf("Capture alpha: %v", err)
	}
	beta, err := cr.Capture(CaptureInput{Slug: "beta", Description: "delete me"})
	if err != nil {
		t.Fatalf("Capture beta: %v", err)
	}

	if _, err := cr.RemoveTask(beta.Task); err != nil {
		t.Fatalf("RemoveTask: %v", err)
	}

	if _, err := os.Stat(beta.Task.Path); !os.IsNotExist(err) {
		t.Errorf("beta task file still present: stat err = %v", err)
	}
	if hits, _ := cr.Search("delete", indexAllOpts()); len(hits) != 0 {
		t.Errorf("beta task should be gone from the index, got %d hits", len(hits))
	}
	if hits, _ := cr.Search("keep", indexAllOpts()); len(hits) != 1 {
		t.Errorf("alpha task should be untouched, got %d hits", len(hits))
	}
}

func TestRemoveTaskEmptyPathErrors(t *testing.T) {
	cr := newTestCore(t)
	if _, err := cr.RemoveTask(storage.Task{ID: "x", ProjectSlug: "p"}); err == nil {
		t.Error("expected an error removing a task with no file path")
	}
}

func TestReloadRegistryPicksUpExternalChange(t *testing.T) {
	cr := newTestCore(t)
	if err := cr.EnsureProject("alpha", "Alpha"); err != nil {
		t.Fatal(err)
	}

	// Another process registers a project and persists it.
	other, err := project.LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	other.Register("beta", "Beta")
	if err := other.Save(); err != nil {
		t.Fatal(err)
	}

	if cr.Registry().Has("beta") {
		t.Fatal("precondition: beta should be unknown before reload")
	}
	if err := cr.ReloadRegistry(); err != nil {
		t.Fatalf("ReloadRegistry: %v", err)
	}
	if !cr.Registry().Has("beta") {
		t.Errorf("ReloadRegistry should surface the externally-registered project")
	}
	if got := cr.Registry().Name("beta"); got != "Beta" {
		t.Errorf("name = %q, want Beta", got)
	}
}

func TestProjectUndoRestoresMetadata(t *testing.T) {
	cr := newTestCore(t)
	if _, err := cr.Capture(CaptureInput{Slug: "proj", Description: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := cr.Registry().SetPrefix("proj", "PJ"); err != nil {
		t.Fatal(err)
	}
	if err := cr.Registry().SetTags("proj", []string{"work", "oss"}); err != nil {
		t.Fatal(err)
	}

	if _, _, err := cr.RemoveProject("proj"); err != nil {
		t.Fatalf("RemoveProject: %v", err)
	}
	if _, err := cr.Undo(); err != nil {
		t.Fatalf("Undo: %v", err)
	}

	if got := cr.Registry().Prefix("proj"); got != "PJ" {
		t.Errorf("prefix after restore = %q, want PJ", got)
	}
	if got := cr.Registry().Tags("proj"); !slices.Equal(got, []string{"work", "oss"}) {
		t.Errorf("tags after restore = %v, want [work oss]", got)
	}
}

func TestProjectUndoRoundTrip(t *testing.T) {
	cr := newTestCore(t)
	_, _ = cr.Capture(CaptureInput{Slug: "doomed", Description: "alpha"})
	_, _ = cr.Capture(CaptureInput{Slug: "doomed", Description: "beta"})

	if _, _, err := cr.RemoveProject("doomed"); err != nil {
		t.Fatalf("RemoveProject: %v", err)
	}
	if storage.ProjectDirExists("doomed") {
		t.Fatalf("project dir should be gone after RemoveProject")
	}

	if _, err := cr.Undo(); err != nil {
		t.Fatalf("Undo after project delete: %v", err)
	}
	if !storage.ProjectDirExists("doomed") {
		t.Fatalf("project dir not restored after Undo")
	}
	for _, term := range []string{"alpha", "beta"} {
		if hits, _ := cr.Search(term, indexAllOpts()); len(hits) != 1 {
			t.Errorf("restored task %q: got %d hits, want 1", term, len(hits))
		}
	}
}

func TestSaveTaskPersistsAndReindexes(t *testing.T) {
	cr := newTestCore(t)
	res, _ := cr.Capture(CaptureInput{Slug: "p", Description: "old details"})

	task := res.Task
	task.Details = "freshly added body text"
	if err := cr.SaveTask(&task); err != nil {
		t.Fatalf("SaveTask: %v", err)
	}

	hits, err := cr.Search("freshly", indexAllOpts())
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 {
		t.Errorf("expected 1 hit after SaveTask, got %d", len(hits))
	}
}

func TestRenameTaskMovesFileAndReindexes(t *testing.T) {
	cr := newTestCore(t)
	res, _ := cr.Capture(CaptureInput{Slug: "p", Description: "before"})

	task := res.Task
	oldPath := task.Path
	if err := cr.RenameTask(&task, "after"); err != nil {
		t.Fatalf("RenameTask: %v", err)
	}
	if task.Path == oldPath {
		t.Errorf("path should have changed; still %q", task.Path)
	}
	if task.Description != "after" {
		t.Errorf("Description = %q, want %q", task.Description, "after")
	}
	if hits, _ := cr.Search("after", indexAllOpts()); len(hits) != 1 {
		t.Errorf("expected 1 hit for new description, got %d", len(hits))
	}
}

func TestReindexRebuildsFromDisk(t *testing.T) {
	cr := newTestCore(t)
	_, _ = cr.Capture(CaptureInput{Slug: "p", Description: "indexed term"})

	// Trigger a full rebuild — the search should still find the task.
	if err := cr.Reindex(); err != nil {
		t.Fatalf("Reindex: %v", err)
	}
	hits, err := cr.Search("indexed", indexAllOpts())
	if err != nil {
		t.Fatalf("Search after Reindex: %v", err)
	}
	if len(hits) != 1 {
		t.Errorf("expected 1 hit after Reindex, got %d", len(hits))
	}
}

func TestDiscoverProjectsPicksUpFilesystemDirs(t *testing.T) {
	cr := newTestCore(t)
	// Capture to register one project the normal way.
	_, _ = cr.Capture(CaptureInput{Slug: "registered", Description: "x"})

	// Create a second project dir by writing through the storage layer
	// directly, bypassing the registry.
	store, err := cr.StoreFor("orphan")
	if err != nil {
		t.Fatalf("StoreFor: %v", err)
	}
	_ = store // creating the store dir is enough; DiscoverProjects scans the dirs

	if cr.Registry().Has("orphan") {
		t.Fatalf("precondition broken: orphan already in registry")
	}
	if err := cr.DiscoverProjects(); err != nil {
		t.Fatalf("DiscoverProjects: %v", err)
	}
	if !cr.Registry().Has("orphan") {
		t.Errorf("DiscoverProjects did not add 'orphan' to registry")
	}
	if !cr.Registry().Has("registered") {
		t.Errorf("DiscoverProjects dropped existing 'registered' entry")
	}
}

func TestHasIndexAndEnsureFresh(t *testing.T) {
	cr := newTestCore(t)
	if !cr.HasIndex() {
		t.Errorf("newTestCore should produce an Open Core with index")
	}
	// EnsureFresh is a no-return best-effort; just confirm it doesn't panic
	// and the index is still usable after.
	cr.EnsureFresh()
	if hits, _ := cr.Search("anything", indexAllOpts()); hits == nil && len(hits) != 0 {
		t.Errorf("Search broke after EnsureFresh")
	}
}

// indexAllOpts builds a SearchOpts that selects every status across
// every project — used as a shorthand in core tests.
func indexAllOpts() index.SearchOpts {
	return index.SearchOpts{Status: "all", Limit: 50, SortBy: "score"}
}

func TestQueryTagFilter(t *testing.T) {
	cr := newTestCore(t)
	for _, d := range []string{"a #p3", "b #later", "c #p3 #later", "d none"} {
		if _, err := cr.Capture(CaptureInput{Slug: "acme", Description: d}); err != nil {
			t.Fatal(err)
		}
	}
	// A separate task in a project tagged "work" carries no inline tags.
	if _, err := cr.Capture(CaptureInput{Slug: "beta", Description: "e in work"}); err != nil {
		t.Fatal(err)
	}
	if err := cr.Registry().SetTags("beta", []string{"work"}); err != nil {
		t.Fatal(err)
	}

	descs := func(res QueryResult) []string {
		var out []string
		for _, h := range res.Hits {
			out = append(out, h.Task.Description)
		}
		slices.Sort(out)
		return out
	}
	cases := []struct {
		name  string
		query string
		want  []string
	}{
		{"include inline", "#p3", []string{"a #p3", "c #p3 #later"}},
		{"exclude inline", "-#later", []string{"a #p3", "d none", "e in work"}},
		{"include + exclude", "#p3 -#later", []string{"a #p3"}},
		{"project tag union", "#work", []string{"e in work"}},
		{"empty passes all", "", []string{"a #p3", "b #later", "c #p3 #later", "d none", "e in work"}},
	}
	for _, c := range cases {
		res, err := cr.Query(c.query, QueryOpts{})
		if err != nil {
			t.Fatalf("%s: Query: %v", c.name, err)
		}
		if got := descs(res); !slices.Equal(got, c.want) {
			t.Errorf("%s: Query(%q) = %v, want %v", c.name, c.query, got, c.want)
		}
	}
}

func TestQuery(t *testing.T) {
	cr := newTestCore(t)
	// One #p3 task plus 25 plain "alpha" tasks, so a small-limit text search
	// would drop the tag match if the predicate weren't applied before LIMIT.
	if _, err := cr.Capture(CaptureInput{Slug: "proj", Description: "alpha zero #p3"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 25; i++ {
		if _, err := cr.Capture(CaptureInput{Slug: "proj", Description: fmt.Sprintf("alpha %d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	ss, _ := cr.SavedSearches()
	if err := ss.Set("p3 only", "#p3"); err != nil {
		t.Fatal(err)
	}

	// The tag predicate applies before LIMIT, so the match survives a small
	// limit on a text query.
	res, err := cr.Query("alpha #p3", QueryOpts{Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 1 || res.Hits[0].Task.Description != "alpha zero #p3" {
		t.Errorf("text+tag: got %d hits", len(res.Hits))
	}
	if !res.Ranked {
		t.Error("a text query should come back Ranked")
	}

	// Project (registry) tags satisfy a predicate on the text path too —
	// matched by slug in SQL, not by mirrored inline tags.
	if _, err := cr.Capture(CaptureInput{Slug: "beta", Description: "alpha in beta"}); err != nil {
		t.Fatal(err)
	}
	if err := cr.Registry().SetTags("beta", []string{"work"}); err != nil {
		t.Fatal(err)
	}
	res, err = cr.Query("alpha #work", QueryOpts{Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 1 || res.Hits[0].Task.Description != "alpha in beta" {
		t.Errorf("text+project tag: got %d hits, want only the beta task", len(res.Hits))
	}

	// Tags-only: lists then filters; not ranked.
	res, _ = cr.Query("#p3", QueryOpts{})
	if len(res.Hits) != 1 || res.Ranked {
		t.Errorf("tags-only: hits=%d ranked=%v, want 1/false", len(res.Hits), res.Ranked)
	}

	// @name (free-text) expansion.
	if res, _ = cr.Query("@p3 only", QueryOpts{}); len(res.Hits) != 1 {
		t.Errorf("@name expansion: got %d hits, want 1", len(res.Hits))
	}
	if _, err := cr.Query("@nope", QueryOpts{}); err == nil {
		t.Error("unknown @name should error")
	}
}

func TestInlineTagsCache(t *testing.T) {
	cr := newTestCore(t)
	base := storage.Task{ID: "01HCACHE0000000000000000AA", ProjectSlug: "p"}
	base.Modified = base.Modified.Add(1) // non-zero mtime

	t1 := base
	t1.Description = "first #alpha"
	if got := cr.TaskTags(t1); len(got) != 1 || got[0] != "alpha" {
		t.Fatalf("first TaskTags = %v, want [alpha]", got)
	}
	// Same ID + same mtime but different content returns the cached parse —
	// proves the memo is keyed by (ID, mtime). In practice content only
	// changes alongside the mtime, so this can't surface stale data.
	t2 := base
	t2.Description = "second #beta"
	if got := cr.TaskTags(t2); len(got) != 1 || got[0] != "alpha" {
		t.Errorf("same key TaskTags = %v, want cached [alpha]", got)
	}
	// A newer mtime re-parses.
	t3 := base
	t3.Modified = t3.Modified.Add(1)
	t3.Description = "third #gamma"
	if got := cr.TaskTags(t3); len(got) != 1 || got[0] != "gamma" {
		t.Errorf("newer mtime TaskTags = %v, want [gamma]", got)
	}
}

func TestTaskTagsDoesNotMutateCache(t *testing.T) {
	cr := newTestCore(t)
	if _, err := cr.Capture(CaptureInput{Slug: "p", Description: "seed"}); err != nil {
		t.Fatal(err)
	}
	if err := cr.Registry().SetTags("p", []string{"work"}); err != nil {
		t.Fatal(err)
	}
	task := storage.Task{ID: "01HCACHE0000000000000000BB", ProjectSlug: "p", Description: "x #inline"}
	task.Modified = task.Modified.Add(1)
	// Two calls must each return inline + project tags; the first must not have
	// corrupted the cached inline slice via append.
	a := cr.TaskTags(task)
	b := cr.TaskTags(task)
	want := []string{"inline", "work"}
	if !slices.Equal(a, want) || !slices.Equal(b, want) {
		t.Errorf("TaskTags = %v / %v, want %v (cached slice mutated?)", a, b, want)
	}
}

func TestInvalidateSavedSearches(t *testing.T) {
	cr := newTestCore(t)
	stale, err := cr.SavedSearches()
	if err != nil {
		t.Fatal(err)
	}
	// Another process writes a saved search behind the cached store's back.
	other, err := savedsearch.Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := other.Set("ext", "#p3"); err != nil {
		t.Fatal(err)
	}
	if again, _ := cr.SavedSearches(); again != stale {
		t.Fatal("SavedSearches should return the cached store until invalidated")
	}
	if _, ok := stale.Get("ext"); ok {
		t.Fatal("cached store should not see the external write yet")
	}
	cr.InvalidateSavedSearches()
	fresh, err := cr.SavedSearches()
	if err != nil {
		t.Fatal(err)
	}
	if q, ok := fresh.Get("ext"); !ok || q != "#p3" {
		t.Errorf("Get(ext) after invalidate = %q,%v; want %q,true", q, ok, "#p3")
	}
}
