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

	cmd := &RmCmd{ID: saved.ID}
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
	cmd := &RmCmd{ID: "01J9X7K3M8VQNH4Z7Y3PG2T5BD"}
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
	cmd := &RmCmd{ID: "not-a-ulid"}
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
