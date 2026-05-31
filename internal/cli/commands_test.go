package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MaikuMori/dfc/internal/core"
	"github.com/MaikuMori/dfc/internal/storage"
)

// setupDFCRoot points DFC_ROOT at a fresh temp dir and silences the
// stderr-bound warn hook from Core.Open so test logs stay clean.
func setupDFCRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv(storage.EnvRoot, root)
	return root
}

// captureStdout swaps os.Stdout for a pipe, runs fn, and returns whatever
// fn printed. Restores stdout in a defer so other tests don't leak.
func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	done := make(chan struct{})
	var buf bytes.Buffer
	go func() {
		_, _ = io.Copy(&buf, r)
		close(done)
	}()
	runErr := fn()
	_ = w.Close()
	<-done
	os.Stdout = orig
	return buf.String(), runErr
}

// captureFirst runs the equivalent of `dfc c -p acme "desc"` and returns
// the saved task. Every test that uses it lands tasks in the same "acme"
// project, so the slug is baked in.
func captureFirst(t *testing.T, desc string) storage.Task {
	t.Helper()
	cr, err := core.Open(core.Options{Warn: func(string, error) {}})
	if err != nil {
		t.Fatalf("core.Open: %v", err)
	}
	defer func() { _ = cr.Close() }()
	res, err := cr.Capture(core.CaptureInput{
		Slug:        "acme",
		Description: desc,
		DisplayName: "acme",
	})
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	return res.Task
}

func TestCaptureCmd_ExplicitProject(t *testing.T) {
	setupDFCRoot(t)
	cmd := &CaptureCmd{
		Project:     "acme",
		Description: []string{"buy", "milk"},
	}
	out, err := captureStdout(t, cmd.Run)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.HasPrefix(out, "captured") {
		t.Errorf("output = %q", out)
	}

	cr, _ := core.Open(core.Options{Warn: func(string, error) {}})
	defer func() { _ = cr.Close() }()
	tasks, err := cr.List("acme")
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].Description != "buy milk" {
		t.Errorf("tasks = %+v", tasks)
	}
}

func TestCaptureCmd_StdinDescription(t *testing.T) {
	setupDFCRoot(t)

	r, w, _ := os.Pipe()
	_, _ = w.WriteString("heading line\n\nbody line 1\nbody line 2\n")
	_ = w.Close()
	origStdin := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = origStdin }()

	cmd := &CaptureCmd{
		Project:     "acme",
		Description: []string{"-"},
	}
	_, err := captureStdout(t, cmd.Run)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	cr, _ := core.Open(core.Options{Warn: func(string, error) {}})
	defer func() { _ = cr.Close() }()
	tasks, _ := cr.List("acme")
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].Description != "heading line" {
		t.Errorf("description = %q", tasks[0].Description)
	}
	if !strings.Contains(tasks[0].Details, "body line 1") {
		t.Errorf("details = %q", tasks[0].Details)
	}
}

func TestCaptureCmd_JSON(t *testing.T) {
	setupDFCRoot(t)
	cmd := &CaptureCmd{
		Project:     "acme",
		Description: []string{"hello"},
		JSON:        true,
	}
	out, err := captureStdout(t, cmd.Run)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out, `"description":"hello"`) {
		t.Errorf("json output missing description: %q", out)
	}
	if !strings.Contains(out, `"project":"acme"`) {
		t.Errorf("json output missing project: %q", out)
	}
}

func TestRmCmd_RemovesTask(t *testing.T) {
	setupDFCRoot(t)
	saved := captureFirst(t, "doomed task")

	cmd := &RmCmd{ID: saved.ID, AllProjects: true}
	_, err := captureStdout(t, cmd.Run)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	cr, _ := core.Open(core.Options{Warn: func(string, error) {}})
	defer func() { _ = cr.Close() }()
	tasks, _ := cr.List("acme")
	if len(tasks) != 0 {
		t.Errorf("expected task removed, got %+v", tasks)
	}
}

func TestRmCmd_UnknownID(t *testing.T) {
	setupDFCRoot(t)
	cmd := &RmCmd{ID: "01J9X7K3M8VQNH4Z7Y3PG2T5BD", AllProjects: true}
	_, err := captureStdout(t, cmd.Run)
	if err == nil {
		t.Fatal("expected error for unknown id")
	}
	if !strings.Contains(err.Error(), "no task with id") {
		t.Errorf("expected not-found message, got %v", err)
	}
}

func TestRmCmd_BadIDLength(t *testing.T) {
	setupDFCRoot(t)
	cmd := &RmCmd{ID: "not-a-ulid", AllProjects: true}
	_, err := captureStdout(t, cmd.Run)
	if err == nil {
		t.Fatal("expected error for invalid id")
	}
}

func TestSearchCmd_FindsByDescription(t *testing.T) {
	setupDFCRoot(t)
	captureFirst(t, "buy milk and bread")
	captureFirst(t, "take out trash")

	cmd := &SearchCmd{
		Project: "acme",
		Status:  "all",
		Limit:   10,
		Sort:    "score",
		Query:   []string{"milk"},
	}
	out, err := captureStdout(t, cmd.Run)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out, "milk") {
		t.Errorf("output missing match: %q", out)
	}
	if strings.Contains(out, "trash") {
		t.Errorf("unrelated row leaked into output: %q", out)
	}
}

func TestSearchCmd_MissingQuery(t *testing.T) {
	setupDFCRoot(t)
	cmd := &SearchCmd{Project: "acme", Status: "all", Limit: 10, Sort: "score"}
	_, err := captureStdout(t, cmd.Run)
	if err == nil {
		t.Fatal("expected error when query missing")
	}
	if !strings.Contains(err.Error(), "query") {
		t.Errorf("error should mention missing query, got %v", err)
	}
}

func TestSearchCmd_JSONOutput(t *testing.T) {
	setupDFCRoot(t)
	captureFirst(t, "search me")

	cmd := &SearchCmd{
		Project: "acme",
		Status:  "all",
		Limit:   10,
		Sort:    "score",
		JSON:    true,
		Query:   []string{"search"},
	}
	out, err := captureStdout(t, cmd.Run)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out, `"task":{`) {
		t.Errorf("json output should wrap task: %q", out)
	}
}

func TestLsCmd_ListsByProject(t *testing.T) {
	setupDFCRoot(t)
	captureFirst(t, "first task")
	captureFirst(t, "second task")

	cmd := &LsCmd{Project: "acme", Status: "all"}
	out, err := captureStdout(t, cmd.Run)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out, "first task") || !strings.Contains(out, "second task") {
		t.Errorf("ls output missing rows: %q", out)
	}
}

func TestLsCmd_AllAndProjectMutuallyExclusive(t *testing.T) {
	setupDFCRoot(t)
	cmd := &LsCmd{Project: "acme", All: true, Status: "all"}
	_, err := captureStdout(t, cmd.Run)
	if err == nil {
		t.Fatal("expected error for --all + --project")
	}
}

// captureInCwdProject puts a task into whatever slug project.Resolve
// returns for the current working directory. Use this in tests that
// exercise the cwd-default ID lookup on done/edit/rm/show/reopen.
func captureInCwdProject(t *testing.T, desc string) storage.Task {
	t.Helper()
	slug, err := resolveCwdSlug()
	if err != nil {
		t.Fatalf("resolveCwdSlug: %v", err)
	}
	cr, err := core.Open(core.Options{Warn: func(string, error) {}})
	if err != nil {
		t.Fatalf("core.Open: %v", err)
	}
	defer func() { _ = cr.Close() }()
	res, err := cr.Capture(core.CaptureInput{Slug: slug, Description: desc, DisplayName: slug})
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	return res.Task
}

func TestDoneCmd_CwdDefault(t *testing.T) {
	setupDFCRoot(t)
	task := captureInCwdProject(t, "in cwd")

	cmd := &DoneCmd{ID: task.ID}
	if _, err := captureStdout(t, cmd.Run); err != nil {
		t.Fatalf("Run: %v", err)
	}

	cr, _ := core.Open(core.Options{Warn: func(string, error) {}})
	defer func() { _ = cr.Close() }()
	got, err := cr.Show(task.ID)
	if err != nil {
		t.Fatalf("Show: %v", err)
	}
	if got.Status != storage.StatusDone {
		t.Errorf("status = %s, want done", got.Status)
	}
}

func TestDoneCmd_OffProjectErrorsWithHint(t *testing.T) {
	setupDFCRoot(t)
	saved := captureFirst(t, "in acme")

	cmd := &DoneCmd{ID: saved.ID}
	_, err := captureStdout(t, cmd.Run)
	if err == nil {
		t.Fatal("expected error for off-cwd ID")
	}
	if !strings.Contains(err.Error(), "--all-projects") {
		t.Errorf("hint missing in error: %v", err)
	}
}

func TestDoneCmd_AllProjectsFlagFindsAcrossProjects(t *testing.T) {
	setupDFCRoot(t)
	saved := captureFirst(t, "in acme")

	cmd := &DoneCmd{ID: saved.ID, AllProjects: true}
	if _, err := captureStdout(t, cmd.Run); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestReopenCmd_CwdDefault(t *testing.T) {
	setupDFCRoot(t)
	task := captureInCwdProject(t, "to be reopened")
	if _, _, err := func() (storage.Task, bool, error) {
		cr, _ := core.Open(core.Options{Warn: func(string, error) {}})
		defer func() { _ = cr.Close() }()
		return cr.SetStatus(task.ID, storage.StatusDone)
	}(); err != nil {
		t.Fatalf("SetStatus done: %v", err)
	}

	cmd := &ReopenCmd{ID: task.ID}
	if _, err := captureStdout(t, cmd.Run); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestEditCmd_CwdDefaultAndOffProject(t *testing.T) {
	setupDFCRoot(t)
	task := captureInCwdProject(t, "edit me")
	newDesc := "edited"

	cmd := &EditCmd{ID: task.ID, Description: &newDesc}
	if _, err := captureStdout(t, cmd.Run); err != nil {
		t.Fatalf("cwd Run: %v", err)
	}

	saved := captureFirst(t, "other project")
	off := &EditCmd{ID: saved.ID, Description: &newDesc}
	_, err := captureStdout(t, off.Run)
	if err == nil || !strings.Contains(err.Error(), "--all-projects") {
		t.Fatalf("expected hint error for off-cwd Edit, got: %v", err)
	}

	allFlag := &EditCmd{ID: saved.ID, Description: &newDesc, AllProjects: true}
	if _, err := captureStdout(t, allFlag.Run); err != nil {
		t.Fatalf("--all-projects Edit: %v", err)
	}
}

func TestRmCmd_CwdDefaultAndOffProject(t *testing.T) {
	setupDFCRoot(t)
	task := captureInCwdProject(t, "rm cwd")
	cmd := &RmCmd{ID: task.ID}
	if _, err := captureStdout(t, cmd.Run); err != nil {
		t.Fatalf("cwd Run: %v", err)
	}

	saved := captureFirst(t, "rm in acme")
	off := &RmCmd{ID: saved.ID}
	_, err := captureStdout(t, off.Run)
	if err == nil || !strings.Contains(err.Error(), "--all-projects") {
		t.Fatalf("expected hint error for off-cwd Rm, got: %v", err)
	}
}

func TestShowCmd_CwdDefaultAndOffProject(t *testing.T) {
	setupDFCRoot(t)
	task := captureInCwdProject(t, "show me")
	cmd := &ShowCmd{ID: task.ID}
	if _, err := captureStdout(t, cmd.Run); err != nil {
		t.Fatalf("cwd Run: %v", err)
	}

	saved := captureFirst(t, "in acme")
	off := &ShowCmd{ID: saved.ID}
	_, err := captureStdout(t, off.Run)
	if err == nil || !strings.Contains(err.Error(), "--all-projects") {
		t.Fatalf("expected hint error for off-cwd Show, got: %v", err)
	}
}

func TestCaptureCmd_AcceptsDisplayName(t *testing.T) {
	setupDFCRoot(t)

	// Seed a project with a friendly name distinct from its slug.
	cr, _ := core.Open(core.Options{Warn: func(string, error) {}})
	if _, err := cr.Capture(core.CaptureInput{Slug: "github-com-acme-widget", Description: "seed", DisplayName: "Acme/Widget"}); err != nil {
		_ = cr.Close()
		t.Fatalf("seed capture: %v", err)
	}
	_ = cr.Close()

	// Capture again using the display name — should resolve back to the same slug.
	cmd := &CaptureCmd{Project: "Acme/Widget", Description: []string{"second"}}
	if _, err := captureStdout(t, cmd.Run); err != nil {
		t.Fatalf("Run: %v", err)
	}

	cr2, _ := core.Open(core.Options{Warn: func(string, error) {}})
	defer func() { _ = cr2.Close() }()
	tasks, _ := cr2.List("github-com-acme-widget")
	if len(tasks) != 2 {
		t.Errorf("expected both captures in one slug, got %d tasks", len(tasks))
	}
}

func TestCaptureCmd_NameLookupCaseInsensitive(t *testing.T) {
	setupDFCRoot(t)

	cr, _ := core.Open(core.Options{Warn: func(string, error) {}})
	if _, err := cr.Capture(core.CaptureInput{Slug: "github-com-acme-widget", Description: "seed", DisplayName: "Acme/Widget"}); err != nil {
		_ = cr.Close()
		t.Fatalf("seed capture: %v", err)
	}
	_ = cr.Close()

	cmd := &CaptureCmd{Project: "acme/widget", Description: []string{"second"}}
	if _, err := captureStdout(t, cmd.Run); err != nil {
		t.Fatalf("Run: %v", err)
	}
	cr2, _ := core.Open(core.Options{Warn: func(string, error) {}})
	defer func() { _ = cr2.Close() }()
	tasks, _ := cr2.List("github-com-acme-widget")
	if len(tasks) != 2 {
		t.Errorf("case-insensitive lookup should land in the canonical slug, got %d tasks", len(tasks))
	}
}

func TestLsCmd_UnknownProjectErrors(t *testing.T) {
	setupDFCRoot(t)
	cmd := &LsCmd{Project: "no-such-thing", Status: "all"}
	_, err := captureStdout(t, cmd.Run)
	if err == nil || !strings.Contains(err.Error(), "unknown project") {
		t.Errorf("expected unknown-project error, got %v", err)
	}
}

func TestLsCmd_AcceptsDisplayName(t *testing.T) {
	setupDFCRoot(t)
	cr, _ := core.Open(core.Options{Warn: func(string, error) {}})
	if _, err := cr.Capture(core.CaptureInput{Slug: "github-com-acme-widget", Description: "a task", DisplayName: "Acme/Widget"}); err != nil {
		_ = cr.Close()
		t.Fatalf("seed: %v", err)
	}
	_ = cr.Close()

	cmd := &LsCmd{Project: "Acme/Widget", Status: "all"}
	out, err := captureStdout(t, cmd.Run)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out, "a task") {
		t.Errorf("expected task to appear when looked up by display name; got %q", out)
	}
}

// captureNamed seeds a task into an arbitrary slug. Lets the tag-flow
// tests build a registry with several distinct projects without all of
// them mapping to the cwd-resolved slug.
func captureNamed(t *testing.T, slug, desc string) {
	t.Helper()
	cr, _ := core.Open(core.Options{Warn: func(string, error) {}})
	defer func() { _ = cr.Close() }()
	if _, err := cr.Capture(core.CaptureInput{Slug: slug, Description: desc, DisplayName: slug}); err != nil {
		t.Fatalf("Capture(%s): %v", slug, err)
	}
}

func TestTagsAddShowRm(t *testing.T) {
	setupDFCRoot(t)
	captureNamed(t, "acme", "buy milk")

	add := &TagsAddCmd{Project: "acme", Tags: []string{"work", "oss"}}
	out, err := captureStdout(t, add.Run)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if !strings.Contains(out, "work") || !strings.Contains(out, "oss") {
		t.Errorf("add output missing tags: %q", out)
	}

	show := &TagsShowCmd{Project: "acme"}
	out, err = captureStdout(t, show.Run)
	if err != nil {
		t.Fatalf("Show: %v", err)
	}
	if !strings.Contains(out, "work") || !strings.Contains(out, "oss") {
		t.Errorf("show missing tags: %q", out)
	}

	rm := &TagsRmCmd{Project: "acme", Tags: []string{"oss"}}
	if _, err := captureStdout(t, rm.Run); err != nil {
		t.Fatalf("Rm: %v", err)
	}
	show2 := &TagsShowCmd{Project: "acme"}
	out, _ = captureStdout(t, show2.Run)
	if strings.Contains(out, "oss") {
		t.Errorf("rm should have dropped oss: %q", out)
	}
}

func TestTagsSetReplacesEntireList(t *testing.T) {
	setupDFCRoot(t)
	captureNamed(t, "acme", "x")
	_ = (&TagsAddCmd{Project: "acme", Tags: []string{"work", "oss"}}).Run()

	set := &TagsSetCmd{Project: "acme", Tags: []string{"deploy", "billing"}}
	if _, err := captureStdout(t, set.Run); err != nil {
		t.Fatalf("Set: %v", err)
	}
	show := &TagsShowCmd{Project: "acme"}
	out, _ := captureStdout(t, show.Run)
	if strings.Contains(out, "work") || strings.Contains(out, "oss") {
		t.Errorf("set should have replaced the tag list: %q", out)
	}
	if !strings.Contains(out, "deploy") || !strings.Contains(out, "billing") {
		t.Errorf("set didn't install new tags: %q", out)
	}
}

func TestTagsLsAggregates(t *testing.T) {
	setupDFCRoot(t)
	captureNamed(t, "acme", "x")
	captureNamed(t, "beta", "y")
	_ = (&TagsAddCmd{Project: "acme", Tags: []string{"work"}}).Run()
	_ = (&TagsAddCmd{Project: "beta", Tags: []string{"work", "personal"}}).Run()

	out, err := captureStdout(t, (&TagsLsCmd{}).Run)
	if err != nil {
		t.Fatalf("Ls: %v", err)
	}
	if !strings.Contains(out, "work") || !strings.Contains(out, "personal") {
		t.Errorf("ls missing tag rows: %q", out)
	}
	if !strings.Contains(out, "acme") || !strings.Contains(out, "beta") {
		t.Errorf("ls missing project slugs: %q", out)
	}
}

func TestLsCmd_TagFilter(t *testing.T) {
	setupDFCRoot(t)
	captureNamed(t, "acme", "buy milk")
	captureNamed(t, "beta", "ship release")
	captureNamed(t, "gamma", "lonely task")
	_ = (&TagsAddCmd{Project: "acme", Tags: []string{"work"}}).Run()
	_ = (&TagsAddCmd{Project: "beta", Tags: []string{"work", "personal"}}).Run()

	// --tag work → acme + beta tasks, not gamma.
	cmd := &LsCmd{All: true, Tag: []string{"work"}, Status: "all"}
	out, err := captureStdout(t, cmd.Run)
	if err != nil {
		t.Fatalf("ls --tag work: %v", err)
	}
	if !strings.Contains(out, "buy milk") || !strings.Contains(out, "ship release") {
		t.Errorf("expected acme+beta tasks: %q", out)
	}
	if strings.Contains(out, "lonely task") {
		t.Errorf("gamma should be filtered out: %q", out)
	}

	// --tag (untagged) → only gamma.
	cmd2 := &LsCmd{All: true, Tag: []string{"(untagged)"}, Status: "all"}
	out, _ = captureStdout(t, cmd2.Run)
	if !strings.Contains(out, "lonely task") {
		t.Errorf("(untagged) should match gamma: %q", out)
	}
	if strings.Contains(out, "buy milk") || strings.Contains(out, "ship release") {
		t.Errorf("(untagged) should exclude tagged projects: %q", out)
	}
}

func TestProjectsCmd_TagFilter(t *testing.T) {
	setupDFCRoot(t)
	captureNamed(t, "acme", "x")
	captureNamed(t, "beta", "y")
	_ = (&TagsAddCmd{Project: "acme", Tags: []string{"work"}}).Run()

	cmd := &ProjectsLsCmd{Tag: []string{"work"}}
	out, err := captureStdout(t, cmd.Run)
	if err != nil {
		t.Fatalf("projects --tag work: %v", err)
	}
	if !strings.Contains(out, "acme") {
		t.Errorf("acme missing from --tag work: %q", out)
	}
	if strings.Contains(out, "beta") {
		t.Errorf("beta should be filtered out: %q", out)
	}
}

func TestSearchCmd_TagFilter(t *testing.T) {
	setupDFCRoot(t)
	captureNamed(t, "acme", "shared term")
	captureNamed(t, "beta", "shared term")
	_ = (&TagsAddCmd{Project: "acme", Tags: []string{"work"}}).Run()

	// Use JSON so we can read the full task ID + project per hit. The
	// human format only prints a 10-char prefix, which can collide on
	// same-millisecond captures.
	cmd := &SearchCmd{All: true, Tag: []string{"work"}, Query: []string{"shared"}, Status: "all", Limit: 10, Sort: "score", JSON: true}
	out, err := captureStdout(t, cmd.Run)
	if err != nil {
		t.Fatalf("search --tag work: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 hit, got %d: %q", len(lines), out)
	}
	if !strings.Contains(lines[0], `"project":"acme"`) {
		t.Errorf("hit should be from acme: %q", lines[0])
	}
	if strings.Contains(lines[0], `"project":"beta"`) {
		t.Errorf("beta should be filtered out: %q", lines[0])
	}
}

func TestRoot_DFCEnvRootHonored(t *testing.T) {
	// Confirm that captured tasks actually land under DFC_ROOT (the
	// foundation every other test relies on).
	root := setupDFCRoot(t)
	captureFirst(t, "anchor")

	got, _ := filepath.Glob(filepath.Join(root, "projects", "acme", "*.md"))
	if len(got) != 1 {
		t.Errorf("expected one task file under %s, got %v", root, got)
	}
}
