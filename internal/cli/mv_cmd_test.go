package cli

import (
	"strings"
	"testing"

	"github.com/MaikuMori/dfc/internal/core"
)

func TestMvCommandMovesTask(t *testing.T) {
	setupDFCRoot(t)
	task := captureFirst(t, "relocate me") // lands in "acme"

	out, err := captureStdout(t, func() error {
		return (&MvCmd{AllProjects: true, ID: task.ID, Project: "archive"}).Run()
	})
	if err != nil {
		t.Fatalf("mv: %v", err)
	}
	if !strings.Contains(out, "archive") {
		t.Errorf("output should name the destination, got %q", out)
	}

	cr, err := core.Open(core.Options{Warn: func(string, error) {}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cr.Close() }()
	moved, err := cr.Show(task.ID)
	if err != nil {
		t.Fatalf("Show: %v", err)
	}
	if moved.ProjectSlug != "archive" {
		t.Errorf("task project = %q, want archive", moved.ProjectSlug)
	}
	if !cr.Registry().Has("archive") {
		t.Errorf("destination project should be auto-registered")
	}
}

func TestMvCommandSameProjectNoOp(t *testing.T) {
	setupDFCRoot(t)
	task := captureFirst(t, "stay") // in "acme"

	out, err := captureStdout(t, func() error {
		return (&MvCmd{AllProjects: true, ID: task.ID, Project: "acme"}).Run()
	})
	if err != nil {
		t.Fatalf("mv: %v", err)
	}
	if !strings.Contains(out, "already in") {
		t.Errorf("same-project move should report a no-op, got %q", out)
	}
}

func TestMvCommandUnknownTask(t *testing.T) {
	setupDFCRoot(t)
	_, err := captureStdout(t, func() error {
		return (&MvCmd{AllProjects: true, ID: "0123456789ABCDEFGHJKMNPQRS", Project: "archive"}).Run()
	})
	if err == nil {
		t.Error("moving a non-existent task should error")
	}
}
