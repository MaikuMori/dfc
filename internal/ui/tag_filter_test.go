package ui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/mattn/go-runewidth"

	"github.com/MaikuMori/dfc/internal/core"
	"github.com/MaikuMori/dfc/internal/storage"
)

// newTagModel boots a Model under a fresh DFC_ROOT, seeds three
// projects, and returns the model ready to drive the global-view +
// tag-filter logic without touching bubbletea's program lifecycle.
func newTagModel(t *testing.T) Model {
	t.Helper()
	root := t.TempDir()
	t.Setenv(storage.EnvRoot, root)

	cr, err := core.Open(core.Options{Warn: func(string, error) {}})
	if err != nil {
		t.Fatalf("core.Open: %v", err)
	}
	t.Cleanup(func() { _ = cr.Close() })

	for _, p := range []struct{ slug, desc string }{
		{"acme", "buy milk"},
		{"beta", "ship release"},
		{"gamma", "lonely task"},
	} {
		if _, err := cr.Capture(core.CaptureInput{Slug: p.slug, Description: p.desc, DisplayName: p.slug}); err != nil {
			t.Fatalf("Capture(%s): %v", p.slug, err)
		}
	}
	if err := cr.Registry().SetTags("acme", []string{"work"}); err != nil {
		t.Fatal(err)
	}
	if err := cr.Registry().SetTags("beta", []string{"work", "personal"}); err != nil {
		t.Fatal(err)
	}
	// gamma stays untagged.

	store, err := cr.StoreFor("acme")
	if err != nil {
		t.Fatal(err)
	}
	all, _ := cr.ListAll()
	m := New(cr, "acme", store, all, nil)
	m.globalView = true
	m.width = 80
	m.height = 24
	m.relayout()
	return m
}

func TestRunSearchFallbackUsesSnapshot(t *testing.T) {
	m := newTagModel(t)
	// A snapshot that does not exist on disk — if the fallback re-read the
	// corpus it would never see these tasks.
	m.searchBase = []storage.Task{
		{ID: "1", Description: "buy milk", ProjectSlug: "acme"},
		{ID: "2", Description: "walk dog", ProjectSlug: "acme"},
	}
	m.searchQuery = "milk"

	got := m.runSearchFallback()
	if len(got.tasks) != 1 || got.tasks[0].ID != "1" {
		t.Errorf("fallback should filter the snapshot to 1 task, got %d", len(got.tasks))
	}
}

func TestBuildRowsCacheInvalidatesOnChange(t *testing.T) {
	m := newTagModel(t)
	m.width = 80
	if _, h := buildRows(m, 80); len(h) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(h))
	}

	// A status + mtime change must invalidate that row's cache entry.
	m.tasks[0].Status = storage.StatusDone
	m.tasks[0].Modified = m.tasks[0].Modified.Add(time.Second)
	rows, _ := buildRows(m, 80)
	if !strings.Contains(rows[0], iconDone) {
		t.Errorf("row 0 should show the done icon after a status change, got %q", rows[0])
	}
}

func TestProjectPrefixTruncatesByWidth(t *testing.T) {
	m := newTagModel(t)
	if err := m.core.Registry().SetPrefix("acme", "🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀"); err != nil {
		t.Fatal(err)
	}
	p := projectPrefix(m, storage.Task{ProjectSlug: "acme"})
	if !utf8.ValidString(p) {
		t.Errorf("prefix is not valid UTF-8 (mid-rune cut): %q", p)
	}
	if w := runewidth.StringWidth(p); w > prefixMaxLen+1 {
		t.Errorf("prefix display width %d exceeds the %d cap: %q", w, prefixMaxLen+1, p)
	}
}

func TestTagFilterMatchesParityWithCLI(t *testing.T) {
	root := t.TempDir()
	regPath := filepath.Join(root, "projects.json")
	t.Setenv(storage.EnvRoot, root)

	cr, err := core.Open(core.Options{Warn: func(string, error) {}})
	if err != nil {
		t.Fatalf("core.Open: %v", err)
	}
	defer func() { _ = cr.Close() }()
	if _, err := cr.Capture(core.CaptureInput{Slug: "alpha", Description: "x", DisplayName: "Alpha"}); err != nil {
		t.Fatal(err)
	}
	if err := cr.Registry().SetTags("alpha", []string{"work", "oss"}); err != nil {
		t.Fatal(err)
	}
	_ = regPath

	cases := []struct {
		filter []string
		want   bool
	}{
		{nil, true},
		{[]string{"work"}, true},
		{[]string{"WORK"}, true},
		{[]string{"missing"}, false},
		{[]string{"(untagged)"}, false},
	}
	for _, tc := range cases {
		if got := tagFilterMatches(cr.Registry(), "alpha", tc.filter); got != tc.want {
			t.Errorf("filter=%v: got %v want %v", tc.filter, got, tc.want)
		}
	}
}

func TestModel_ReloadAllAppliesTagFilter(t *testing.T) {
	m := newTagModel(t)
	if len(m.tasks) != 3 {
		t.Fatalf("seed sanity: expected 3 tasks in global view, got %d", len(m.tasks))
	}

	m.tagFilter = []string{"work"}
	m = m.reloadAll()
	if len(m.tasks) != 2 {
		t.Errorf("--tag work should yield 2 tasks (acme+beta), got %d", len(m.tasks))
	}

	m.tagFilter = []string{"(untagged)"}
	m = m.reloadAll()
	if len(m.tasks) != 1 {
		t.Errorf("(untagged) should yield 1 task (gamma), got %d", len(m.tasks))
	}

	m.tagFilter = nil
	m = m.reloadAll()
	if len(m.tasks) != 3 {
		t.Errorf("empty filter should restore all 3 tasks, got %d", len(m.tasks))
	}
}

func TestModel_HeaderShowsLivePreviewCount(t *testing.T) {
	m := newTagModel(t)
	// Open the tag-filter picker the same way the f-key path does.
	m.picker = newTagFilterPicker(m.core.Registry(), nil)
	m.picker.SetWidth(m.width)
	m.mode = modeTagFilter
	m.tagFilterBase, _ = m.core.ListAll()

	// Before any selection: header should still report the full count (3).
	if got := m.header(); !strings.Contains(got, "all (3)") {
		t.Errorf("pre-select header should show all (3), got %q", got)
	}

	// Simulate selecting "work" by mutating the picker's chosen set
	// directly (the public path is space-toggle, but we bypass to keep
	// the test focused on the header math).
	m.picker.PreselectMany([]string{"work"})
	if got := m.header(); !strings.Contains(got, "all (2)") {
		t.Errorf("after selecting 'work', header should show all (2), got %q", got)
	}
	if got := m.header(); !strings.Contains(got, "filter: [work]") {
		t.Errorf("header should advertise the in-flight filter: %q", got)
	}

	// Switch to (untagged) only.
	m.picker.PreselectMany([]string{"(untagged)"})
	if got := m.header(); !strings.Contains(got, "all (1)") {
		t.Errorf("(untagged) projection should yield 1 task, got %q", got)
	}
}

func TestModel_TagFilterPreviewUsesCachedBase(t *testing.T) {
	m := newTagModel(t)
	m.picker = newTagFilterPicker(m.core.Registry(), nil)
	m.mode = modeTagFilter
	m.tagFilterBase, _ = m.core.ListAll() // caches the 3 seeded tasks

	// A capture landing after the picker opened must not change the live
	// preview, which reads the cached base rather than re-walking disk.
	if _, err := m.core.Capture(core.CaptureInput{Slug: "delta", Description: "late", DisplayName: "delta"}); err != nil {
		t.Fatal(err)
	}
	if got := m.header(); !strings.Contains(got, "all (3)") {
		t.Errorf("preview should use the cached base count (3), got %q", got)
	}
}

func TestRunSearch_GlobalViewRespectsTagFilter(t *testing.T) {
	m := newTagModel(t)
	// "task" appears only in gamma ("lonely task"), which is untagged.
	m.searchQuery = "task"
	m = m.runSearch()
	if len(m.tasks) != 1 {
		t.Fatalf("baseline: 'task' should match gamma, got %d", len(m.tasks))
	}
	// A work-tag filter excludes gamma, so the indexed search must too.
	m.tagFilter = []string{"work"}
	m = m.runSearch()
	if len(m.tasks) != 0 {
		t.Errorf("work filter should exclude gamma's 'task' hit, got %d", len(m.tasks))
	}
}

func TestIsPickerMode(t *testing.T) {
	for _, md := range []mode{modeSwitch, modeCaptureTarget, modeTagEdit, modeTagFilter} {
		if !(Model{mode: md}).isPickerMode() {
			t.Errorf("mode %v should be a picker mode", md)
		}
	}
	for _, md := range []mode{modeList, modeCapture, modeEdit, modeSearch, modeHelp} {
		if (Model{mode: md}).isPickerMode() {
			t.Errorf("mode %v should not be a picker mode", md)
		}
	}
}

func TestEscClearsTagFilter(t *testing.T) {
	m := newTagModel(t)
	m.tagFilter = []string{"work"}
	m = m.reloadAll()

	model, _ := m.updateList(tea.KeyPressMsg{Code: tea.KeyEscape})
	got := model.(Model)
	if len(got.tagFilter) != 0 {
		t.Errorf("esc should clear the tag filter, got %v", got.tagFilter)
	}
	if len(got.tasks) != 3 {
		t.Errorf("clearing the filter should restore all 3 tasks, got %d", len(got.tasks))
	}
}

func TestModel_HeaderPostCommitCount(t *testing.T) {
	m := newTagModel(t)
	// Mimic updateTagFilter's commit branch.
	m.tagFilter = []string{"work"}
	m = m.reloadAll()
	if got := m.header(); !strings.Contains(got, "all (2)") {
		t.Errorf("post-commit header should show filtered count: %q", got)
	}
	if got := m.header(); !strings.Contains(got, "filter: [work]") {
		t.Errorf("post-commit header should advertise the filter: %q", got)
	}
}
