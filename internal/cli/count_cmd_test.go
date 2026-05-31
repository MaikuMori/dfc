package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/MaikuMori/dfc/internal/core"
	"github.com/MaikuMori/dfc/internal/storage"
)

func TestPromptCount(t *testing.T) {
	cases := []struct {
		open, done int
		want       string
	}{
		{2, 0, "2○"},
		{2, 1, "2○ 1✓"},
		{0, 1, "1✓"},
		{0, 0, ""},
	}
	for _, c := range cases {
		if got := promptCount(c.open, c.done); got != c.want {
			t.Errorf("promptCount(%d,%d) = %q, want %q", c.open, c.done, got, c.want)
		}
	}
}

func markFirstDone(t *testing.T, slug string) {
	t.Helper()
	cr, err := core.Open(core.Options{Warn: func(string, error) {}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cr.Close() }()
	tasks, err := cr.List(slug)
	if err != nil || len(tasks) == 0 {
		t.Fatalf("List(%s): %v (n=%d)", slug, err, len(tasks))
	}
	if _, _, err := cr.SetStatus(tasks[0].ID, storage.StatusDone); err != nil {
		t.Fatal(err)
	}
}

func TestCountPromptAndHuman(t *testing.T) {
	setupDFCRoot(t)
	captureNamed(t, "acme", "a")
	captureNamed(t, "acme", "b")
	markFirstDone(t, "acme") // 1 open, 1 done

	prompt, err := captureStdout(t, func() error {
		return (&CountCmd{Format: "prompt", AllProjects: true}).Run()
	})
	if err != nil {
		t.Fatalf("count prompt: %v", err)
	}
	if strings.TrimSpace(prompt) != "1○ 1✓" {
		t.Errorf("prompt = %q, want '1○ 1✓'", strings.TrimSpace(prompt))
	}

	human, err := captureStdout(t, func() error {
		return (&CountCmd{AllProjects: true}).Run()
	})
	if err != nil {
		t.Fatalf("count human: %v", err)
	}
	if !strings.Contains(human, "1 open") || !strings.Contains(human, "1 done") {
		t.Errorf("human = %q, want '1 open · 1 done'", strings.TrimSpace(human))
	}
}

func TestCountJSON(t *testing.T) {
	setupDFCRoot(t)
	captureNamed(t, "acme", "a")
	captureNamed(t, "acme", "b")
	markFirstDone(t, "acme")
	_ = (&TagsAddCmd{Project: "acme", Tags: []string{"work"}}).Run()

	out, err := captureStdout(t, func() error {
		return (&CountCmd{Format: "json", AllProjects: true}).Run()
	})
	if err != nil {
		t.Fatalf("count json: %v", err)
	}
	var got countOut
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal %q: %v", out, err)
	}
	if got.Open != 1 || got.Done != 1 || got.DoneToday != 1 {
		t.Errorf("counts = %+v, want open=1 done=1 done_today=1", got)
	}
	if got.ByTag["work"] != 1 {
		t.Errorf("by_tag = %v, want {work:1}", got.ByTag)
	}
}

func TestCountFlagConflict(t *testing.T) {
	setupDFCRoot(t)
	if _, err := captureStdout(t, func() error {
		return (&CountCmd{AllProjects: true, Project: "acme"}).Run()
	}); err == nil {
		t.Error("-a and -p together should error")
	}
}
