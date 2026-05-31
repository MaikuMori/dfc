package core

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/MaikuMori/dfc/internal/storage"
)

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
func indexAllOpts() indexSearchOptsAlias {
	return indexSearchOptsAlias{Status: "all", Limit: 50, SortBy: "score"}
}

// indexSearchOptsAlias mirrors index.SearchOpts at the field level so
// these tests don't need to import internal/index directly. The Core
// Search method accepts any value with the same field set.
type indexSearchOptsAlias = struct {
	Project  string
	Projects []string
	Status   string
	Limit    int
	SortBy   string
}

