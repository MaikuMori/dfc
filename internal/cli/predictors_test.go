package cli

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/MaikuMori/dfc/internal/core"
	"github.com/MaikuMori/dfc/internal/storage"
	"github.com/posener/complete"
)

func TestScopeFromArgs(t *testing.T) {
	cases := []struct {
		name        string
		completed   []string
		wantAll     bool
		wantSlug    string
		ignoreSlug  bool // when wantAll is true; slug is intentionally empty
		cwdFallback bool // expect slug == resolveCwdSlug() output
	}{
		{name: "empty falls back to cwd", cwdFallback: true},
		{name: "short -a flag", completed: []string{"dfc", "done", "-a"}, wantAll: true, ignoreSlug: true},
		{name: "long --all-projects", completed: []string{"dfc", "done", "--all-projects"}, wantAll: true, ignoreSlug: true},
		{name: "ls-style --all", completed: []string{"dfc", "ls", "--all"}, wantAll: true, ignoreSlug: true},
		{name: "short -p slug", completed: []string{"dfc", "c", "-p", "acme"}, wantSlug: "acme"},
		{name: "long --project slug", completed: []string{"dfc", "c", "--project", "acme"}, wantSlug: "acme"},
		{name: "--project=acme", completed: []string{"dfc", "c", "--project=acme"}, wantSlug: "acme"},
		{name: "-p=acme", completed: []string{"dfc", "c", "-p=acme"}, wantSlug: "acme"},
		{name: "-a wins over -p when both present", completed: []string{"dfc", "done", "-a", "-p", "acme"}, wantAll: true, ignoreSlug: true},
	}

	cwdSlug, err := resolveCwdSlug()
	if err != nil {
		t.Fatalf("resolveCwdSlug: %v", err)
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := scopeFromArgs(complete.Args{Completed: tc.completed})
			if got.AllProjects != tc.wantAll {
				t.Errorf("AllProjects = %v, want %v", got.AllProjects, tc.wantAll)
			}
			if tc.ignoreSlug {
				return
			}
			want := tc.wantSlug
			if tc.cwdFallback {
				want = cwdSlug
			}
			if got.Slug != want {
				t.Errorf("Slug = %q, want %q", got.Slug, want)
			}
		})
	}
}

func TestPredictProjectsSortsByLastUsed(t *testing.T) {
	setupDFCRoot(t)

	cr, _ := core.Open(core.Options{Warn: func(string, error) {}})
	defer func() { _ = cr.Close() }()
	if _, err := cr.Capture(core.CaptureInput{Slug: "alpha-slug", Description: "x", DisplayName: "Alpha"}); err != nil {
		t.Fatalf("Capture alpha: %v", err)
	}
	// Registry.Touch truncates LastUsed to whole seconds, so space the
	// second capture out enough to land on a later second.
	time.Sleep(1100 * time.Millisecond)
	if _, err := cr.Capture(core.CaptureInput{Slug: "beta-slug", Description: "y", DisplayName: "Beta"}); err != nil {
		t.Fatalf("Capture beta: %v", err)
	}

	got := predictProjects(complete.Args{})
	if len(got) < 2 {
		t.Fatalf("expected at least 2 candidates, got %v", got)
	}
	if got[0] != "Beta" {
		t.Errorf("most recent project should sort first; got %v", got)
	}
}

func TestPredictProjectsPrefersShellSafeName(t *testing.T) {
	setupDFCRoot(t)
	cr, _ := core.Open(core.Options{Warn: func(string, error) {}})
	defer func() { _ = cr.Close() }()
	// Shell-safe name: candidate is the name.
	if _, err := cr.Capture(core.CaptureInput{Slug: "github-com-acme-widget", Description: "x", DisplayName: "Acme/Widget"}); err != nil {
		t.Fatalf("Capture widget: %v", err)
	}
	// Name with whitespace falls back to slug.
	if _, err := cr.Capture(core.CaptureInput{Slug: "github-com-acme-fancy", Description: "y", DisplayName: "Fancy Project"}); err != nil {
		t.Fatalf("Capture fancy: %v", err)
	}

	got := predictProjects(complete.Args{})
	if !contains(got, "Acme/Widget") {
		t.Errorf("shell-safe name should be the candidate; got %v", got)
	}
	if !contains(got, "github-com-acme-fancy") {
		t.Errorf("unsafe name should fall back to slug; got %v", got)
	}
	if contains(got, "Fancy Project") {
		t.Errorf("unsafe name should not appear as a candidate: %v", got)
	}
}

func TestPredictProjectsRichDescription(t *testing.T) {
	setupDFCRoot(t)
	cr, _ := core.Open(core.Options{Warn: func(string, error) {}})
	defer func() { _ = cr.Close() }()
	if _, err := cr.Capture(core.CaptureInput{Slug: "github-com-acme-widget", Description: "x", DisplayName: "Acme/Widget"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	t.Setenv(EnvCompleteFormat, "rich")
	got := predictProjects(complete.Args{})
	want := "Acme/Widget\tgithub-com-acme-widget"
	if !contains(got, want) {
		t.Errorf("rich predict missing %q: %v", want, got)
	}
}

func TestScopeFromArgsResolvesDisplayName(t *testing.T) {
	setupDFCRoot(t)
	cr, _ := core.Open(core.Options{Warn: func(string, error) {}})
	if _, err := cr.Capture(core.CaptureInput{Slug: "github-com-acme-widget", Description: "x", DisplayName: "Acme/Widget"}); err != nil {
		_ = cr.Close()
		t.Fatalf("seed: %v", err)
	}
	_ = cr.Close()

	got := scopeFromArgs(complete.Args{Completed: []string{"dfc", "c", "-p", "Acme/Widget"}})
	if got.Slug != "github-com-acme-widget" {
		t.Errorf("expected name to resolve to canonical slug, got %q", got.Slug)
	}
}

func TestPredictTaskOpenScopedToCwd(t *testing.T) {
	setupDFCRoot(t)
	openTask := captureInCwdProject(t, "open in cwd")
	otherTask := captureFirst(t, "in acme")

	// cwd scope: only the cwd task shows up
	got := predictTasksFn(scopeTaskOpen)(complete.Args{})
	if !contains(got, openTask.ID) {
		t.Errorf("cwd predict missing cwd task %s: %v", openTask.ID, got)
	}
	if contains(got, otherTask.ID) {
		t.Errorf("cwd predict leaked acme task %s: %v", otherTask.ID, got)
	}

	// --all-projects: both show up
	gotAll := predictTasksFn(scopeTaskOpen)(complete.Args{Completed: []string{"dfc", "done", "--all-projects"}})
	if !contains(gotAll, openTask.ID) || !contains(gotAll, otherTask.ID) {
		t.Errorf("--all-projects predict missing one: %v", gotAll)
	}

	// -p acme scope: only acme task
	gotP := predictTasksFn(scopeTaskOpen)(complete.Args{Completed: []string{"dfc", "done", "-p", "acme"}})
	if contains(gotP, openTask.ID) {
		t.Errorf("-p acme leaked cwd task: %v", gotP)
	}
	if !contains(gotP, otherTask.ID) {
		t.Errorf("-p acme missing acme task: %v", gotP)
	}
}

func TestPredictTaskFiltersByStatus(t *testing.T) {
	setupDFCRoot(t)
	openTask := captureInCwdProject(t, "still open")
	doneTask := captureInCwdProject(t, "going to be done")

	cr, _ := core.Open(core.Options{Warn: func(string, error) {}})
	defer func() { _ = cr.Close() }()
	if _, _, err := cr.SetStatus(doneTask.ID, storage.StatusDone); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}

	gotOpen := predictTasksFn(scopeTaskOpen)(complete.Args{})
	if !contains(gotOpen, openTask.ID) || contains(gotOpen, doneTask.ID) {
		t.Errorf("task-open should only include the open task: %v", gotOpen)
	}

	gotDone := predictTasksFn(scopeTaskDone)(complete.Args{})
	if contains(gotDone, openTask.ID) || !contains(gotDone, doneTask.ID) {
		t.Errorf("task-done should only include the done task: %v", gotDone)
	}

	gotAny := predictTasksFn(scopeTaskAny)(complete.Args{})
	if !contains(gotAny, openTask.ID) || !contains(gotAny, doneTask.ID) {
		t.Errorf("task-any should include both: %v", gotAny)
	}
}

func TestPredictRichFormatEmitsTabSeparatedDescription(t *testing.T) {
	setupDFCRoot(t)
	task := captureInCwdProject(t, "buy milk")

	t.Setenv(EnvCompleteFormat, "rich")
	got := predictTasksFn(scopeTaskOpen)(complete.Args{})
	want := task.ID + "\tbuy milk"
	if !contains(got, want) {
		t.Errorf("rich predict missing %q: %v", want, got)
	}
}

func TestPredictPlainFormatEmitsBareID(t *testing.T) {
	setupDFCRoot(t)
	task := captureInCwdProject(t, "buy milk")

	got := predictTasksFn(scopeTaskOpen)(complete.Args{})
	if !contains(got, task.ID) {
		t.Errorf("plain predict missing ID: %v", got)
	}
	for _, s := range got {
		if strings.Contains(s, "\t") {
			t.Errorf("plain predict leaked a tab: %q", s)
		}
	}
}

func TestPredictTrashIDsLists(t *testing.T) {
	setupDFCRoot(t)
	saved := captureFirst(t, "going to trash")
	cr, _ := core.Open(core.Options{Warn: func(string, error) {}})
	defer func() { _ = cr.Close() }()
	_, _, trashID, err := cr.Remove(saved.ID)
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}

	got := predictTrashIDs(complete.Args{})
	if !contains(got, trashID) {
		t.Errorf("trash-id predict missing %s: %v", trashID, got)
	}
}

func TestTruncateAddsEllipsisOnlyWhenOverflowing(t *testing.T) {
	if got := truncate("short"); got != "short" {
		t.Errorf("short string mutated: %q", got)
	}
	long := strings.Repeat("a", 80)
	got := truncate(long)
	if runeLen(got) != 60 || !strings.HasSuffix(got, "…") {
		t.Errorf("truncate(80→60 runes) = %q (len %d runes)", got, runeLen(got))
	}
}

func runeLen(s string) int { return len([]rune(s)) }

func contains(haystack []string, needle string) bool {
	return slices.Contains(haystack, needle)
}
