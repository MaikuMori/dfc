package ui

import (
	"sort"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/MaikuMori/dfc/internal/core"
	"github.com/MaikuMori/dfc/internal/savedsearch"
	"github.com/MaikuMori/dfc/internal/storage"
)

// A saved search written by another process must become visible after the
// watcher reports a filesystem change — the fs handler drops core's cached
// store so `@name` queries and the `S` picker re-read searches.json.
func TestFSChangeReloadsSavedSearches(t *testing.T) {
	t.Setenv(storage.EnvRoot, t.TempDir())
	cr, err := core.Open(core.Options{Warn: func(string, error) {}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cr.Close() })
	if _, err := cr.Capture(core.CaptureInput{Slug: "acme", Description: "ship #p3", DisplayName: "acme"}); err != nil {
		t.Fatal(err)
	}
	if _, err := cr.SavedSearches(); err != nil { // prime the cache
		t.Fatal(err)
	}
	other, err := savedsearch.Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := other.Set("ext", "#p3"); err != nil {
		t.Fatal(err)
	}
	store, _ := cr.StoreFor("acme")
	all, _ := cr.ListAll()
	m := New(cr, "acme", store, all, nil)
	m.width, m.height = 80, 24
	m.relayout()

	updated, _ := m.Update(fsChangedMsg{})
	m = updated.(Model)

	ss, err := cr.SavedSearches()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ss.Get("ext"); !ok {
		t.Error("externally saved search not visible after fsChangedMsg")
	}
}

func TestSavedSearchPickerAppliesQuery(t *testing.T) {
	t.Setenv(storage.EnvRoot, t.TempDir())
	cr, err := core.Open(core.Options{Warn: func(string, error) {}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cr.Close() })
	for _, d := range []string{"ship #p3", "docs #later"} {
		if _, err := cr.Capture(core.CaptureInput{Slug: "acme", Description: d, DisplayName: "acme"}); err != nil {
			t.Fatal(err)
		}
	}
	ss, err := cr.SavedSearches()
	if err != nil {
		t.Fatal(err)
	}
	if err := ss.Set("p3only", "#p3"); err != nil {
		t.Fatal(err)
	}
	store, _ := cr.StoreFor("acme")
	all, _ := cr.ListAll()
	m := New(cr, "acme", store, all, nil)
	m.width, m.height = 80, 24
	m.relayout()

	opened, _ := m.openSavedSearchPicker()
	m = opened.(Model)
	if m.mode != modeSavedSearch {
		t.Fatalf("mode after open = %v, want modeSavedSearch", m.mode)
	}

	applied, _ := m.updateSavedSearch(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = applied.(Model)
	if m.mode != modeList {
		t.Errorf("mode after select = %v, want modeList", m.mode)
	}
	if m.searchQuery != "#p3" {
		t.Errorf("searchQuery = %q, want #p3", m.searchQuery)
	}
	if len(m.tasks) != 1 || m.tasks[0].Description != "ship #p3" {
		t.Errorf("filtered tasks = %v, want only [ship #p3]", m.tasks)
	}
}

func TestSaveSearchCreateFlow(t *testing.T) {
	t.Setenv(storage.EnvRoot, t.TempDir())
	cr, err := core.Open(core.Options{Warn: func(string, error) {}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cr.Close() })
	if _, err := cr.Capture(core.CaptureInput{Slug: "acme", Description: "ship #p3", DisplayName: "acme"}); err != nil {
		t.Fatal(err)
	}
	store, _ := cr.StoreFor("acme")
	all, _ := cr.ListAll()
	m := New(cr, "acme", store, all, nil)
	m.width, m.height = 80, 24
	m.relayout()

	// In the filter with a query, ctrl+s opens the name prompt.
	m.mode = modeSearch
	m.input.SetValue("#p3")
	m.searchQuery = "#p3"
	out, _ := m.updateSearch(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	m = out.(Model)
	if m.mode != modeSaveSearchName {
		t.Fatalf("ctrl+s should open the name prompt, mode = %v", m.mode)
	}

	// Type a full-text name and commit.
	m.input.SetValue("my important search")
	out, _ = m.updateSaveSearchName(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = out.(Model)
	if m.mode != modeList {
		t.Errorf("after save, mode = %v, want modeList", m.mode)
	}
	ss, _ := cr.SavedSearches()
	if q, ok := ss.Get("my important search"); !ok || q != "#p3" {
		t.Errorf("saved query = %q,%v, want #p3", q, ok)
	}
}

func TestSaveSearchOverwriteConfirm(t *testing.T) {
	t.Setenv(storage.EnvRoot, t.TempDir())
	cr, err := core.Open(core.Options{Warn: func(string, error) {}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cr.Close() })
	if _, err := cr.Capture(core.CaptureInput{Slug: "acme", Description: "x", DisplayName: "acme"}); err != nil {
		t.Fatal(err)
	}
	ss, _ := cr.SavedSearches()
	_ = ss.Set("dupe", "#a")
	store, _ := cr.StoreFor("acme")
	all, _ := cr.ListAll()
	m := New(cr, "acme", store, all, nil)
	m.width, m.height = 80, 24
	m.relayout()

	m.mode = modeSaveSearchName
	m.searchQuery = "#b"
	m.input.SetValue("dupe")

	// First enter on an existing name confirms instead of overwriting.
	out, _ := m.updateSaveSearchName(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = out.(Model)
	if m.pendingOverwrite != "dupe" || m.mode != modeSaveSearchName {
		t.Fatalf("first enter should arm overwrite confirm; pending=%q mode=%v", m.pendingOverwrite, m.mode)
	}
	if q, _ := ss.Get("dupe"); q != "#a" {
		t.Errorf("must not overwrite before confirm, got %q", q)
	}

	// Second enter commits the overwrite.
	out, _ = m.updateSaveSearchName(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = out.(Model)
	if m.mode != modeList {
		t.Errorf("after confirm, mode = %v, want modeList", m.mode)
	}
	if q, _ := ss.Get("dupe"); q != "#b" {
		t.Errorf("confirm should overwrite to #b, got %q", q)
	}
}

func TestSavedSearchPickerDelete(t *testing.T) {
	t.Setenv(storage.EnvRoot, t.TempDir())
	cr, err := core.Open(core.Options{Warn: func(string, error) {}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cr.Close() })
	if _, err := cr.Capture(core.CaptureInput{Slug: "acme", Description: "x", DisplayName: "acme"}); err != nil {
		t.Fatal(err)
	}
	ss, _ := cr.SavedSearches()
	_ = ss.Set("alpha", "#a")
	_ = ss.Set("beta", "#b")
	store, _ := cr.StoreFor("acme")
	all, _ := cr.ListAll()
	m := New(cr, "acme", store, all, nil)
	m.width, m.height = 80, 24
	m.relayout()

	out, _ := m.openSavedSearchPicker()
	m = out.(Model)
	// ctrl+shift+d asks to confirm; y deletes the highlighted row.
	out, _ = m.updateSavedSearch(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl | tea.ModShift})
	m = out.(Model)
	out, _ = m.updateSavedSearch(tea.KeyPressMsg{Code: 'y', Text: "y"})
	_ = out.(Model)

	if got := len(ss.List()); got != 1 {
		t.Errorf("after delete, %d saved searches remain, want 1", got)
	}
}

func TestSavedSearchPickerEmptyIsNoOp(t *testing.T) {
	t.Setenv(storage.EnvRoot, t.TempDir())
	cr, err := core.Open(core.Options{Warn: func(string, error) {}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cr.Close() })
	if _, err := cr.Capture(core.CaptureInput{Slug: "acme", Description: "x", DisplayName: "acme"}); err != nil {
		t.Fatal(err)
	}
	store, _ := cr.StoreFor("acme")
	all, _ := cr.ListAll()
	m := New(cr, "acme", store, all, nil)
	m.width, m.height = 80, 24
	m.relayout()

	opened, _ := m.openSavedSearchPicker()
	m = opened.(Model)
	if m.mode != modeList {
		t.Errorf("mode = %v, want modeList when there are no saved searches", m.mode)
	}
	if m.status == "" {
		t.Error("expected a status hint when there are no saved searches")
	}
}

func TestRunSearchHashtagFilter(t *testing.T) {
	t.Setenv(storage.EnvRoot, t.TempDir())
	cr, err := core.Open(core.Options{Warn: func(string, error) {}})
	if err != nil {
		t.Fatalf("core.Open: %v", err)
	}
	t.Cleanup(func() { _ = cr.Close() })
	for _, desc := range []string{
		"ship migration #p3",
		"write docs #later",
		"fix bug #p3 #later",
		"random chore",
	} {
		if _, err := cr.Capture(core.CaptureInput{Slug: "acme", Description: desc, DisplayName: "acme"}); err != nil {
			t.Fatalf("Capture: %v", err)
		}
	}
	// A project tag applies to every task in the project, so it unions with
	// each task's inline tags for filtering.
	if err := cr.Registry().SetTags("acme", []string{"work"}); err != nil {
		t.Fatal(err)
	}
	store, _ := cr.StoreFor("acme")
	all, _ := cr.ListAll()
	m := New(cr, "acme", store, all, nil)
	m.width, m.height = 80, 24
	m.relayout()

	descs := func(m Model) []string {
		var out []string
		for _, t := range m.tasks {
			out = append(out, t.Description)
		}
		sort.Strings(out)
		return out
	}
	eq := func(a, b []string) bool {
		if len(a) != len(b) {
			return false
		}
		for i := range a {
			if a[i] != b[i] {
				return false
			}
		}
		return true
	}
	cases := []struct {
		query string
		want  []string
	}{
		{"#p3", []string{"fix bug #p3 #later", "ship migration #p3"}}, // tags-only, skips FTS
		{"-#later", []string{"random chore", "ship migration #p3"}},   // exclude
		{"migration #p3", []string{"ship migration #p3"}},             // FTS text + tag
		{"#p3 -#later", []string{"ship migration #p3"}},               // include + exclude
		{"#work", []string{ // project tag unions onto every task
			"fix bug #p3 #later", "random chore", "ship migration #p3", "write docs #later",
		}},
		{"#work -#later", []string{"random chore", "ship migration #p3"}}, // project tag + content exclude
	}
	for _, c := range cases {
		m.searchQuery = c.query
		if got := descs(m.runSearch()); !eq(got, c.want) {
			t.Errorf("runSearch(%q) = %v, want %v", c.query, got, c.want)
		}
	}
}

func TestExplainQuery(t *testing.T) {
	cases := []struct{ q, want string }{
		{"", ""},
		{"   ", ""},
		{"#p3", "tagged #p3"},
		{"-#later", "not #later"},
		{"#p3 -#later", "tagged #p3, not #later"},
		{"migration #p3", `tagged #p3, matching "migration"`},
		{"foo bar", `matching "foo" "bar"`},
		{"#a #b -#c text", `tagged #a #b, not #c, matching "text"`},
	}
	for _, c := range cases {
		if got := explainQuery(c.q); got != c.want {
			t.Errorf("explainQuery(%q) = %q, want %q", c.q, got, c.want)
		}
	}
}

func TestPillTags(t *testing.T) {
	// A normal foreground run with a tag gets repainted; visible text is kept.
	body := "\x1b[38;5;252mdo #p3 now\x1b[m"
	out := pillTags(body)
	if out == body {
		t.Errorf("expected #p3 to be pilled, output unchanged: %q", out)
	}
	if got := ansiSGR.ReplaceAllString(out, ""); got != "do #p3 now" {
		t.Errorf("visible text changed: %q", got)
	}
	// A background run (heading / code block) is left untouched so its style
	// isn't broken.
	head := "\x1b[38;5;228;48;5;63;1mTitle #p3\x1b[m"
	if got := pillTags(head); got != head {
		t.Errorf("background run must be untouched: %q", got)
	}
	// A run without tags passes through unchanged.
	plain := "\x1b[38;5;252mjust text\x1b[m"
	if got := pillTags(plain); got != plain {
		t.Errorf("tagless run changed: %q", got)
	}
}

func TestStripTags(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain text no tags", "plain text no tags"},
		{"do #p3 and #later now", "do and now"},
		{"#vacation plans# at the front", "at the front"},
		{"trailing tag #ship", "trailing tag"},
		{"#only", ""},
		{"keep #3 issue numbers", "keep #3 issue numbers"}, // #3 isn't a tag
	}
	for _, c := range cases {
		if got := stripTags(c.in); got != c.want {
			t.Errorf("stripTags(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRenderRowCollectsTagsAfterTitle(t *testing.T) {
	task := storage.Task{Description: "ship it #p3 #later", Status: storage.StatusOpen}
	row := ansiSGR.ReplaceAllString(renderRow(task, "", false, 60, task.Tags()), "")
	// Title reads clean, with the tags lifted out and grouped after it.
	if !strings.Contains(row, "ship it #p3 #later") {
		t.Errorf("tags not grouped after the title: %q", row)
	}
}

func TestRenderRowTagsOnCursorAndDoneRows(t *testing.T) {
	// Cursor and done rows take the outer-style path: tags ride along as raw
	// text, not pills, but must still show.
	for _, tc := range []struct {
		name   string
		cursor bool
		status storage.Status
	}{
		{"cursor", true, storage.StatusOpen},
		{"done", false, storage.StatusDone},
		{"done+cursor", true, storage.StatusDone},
	} {
		task := storage.Task{Description: "ship it #p3 #later", Status: tc.status}
		row := ansiSGR.ReplaceAllString(renderRow(task, "", tc.cursor, 60, task.Tags()), "")
		if !strings.Contains(row, "#p3") || !strings.Contains(row, "#later") {
			t.Errorf("%s row missing collected tags: %q", tc.name, row)
		}
	}
}

func TestRenderRowTagOverflow(t *testing.T) {
	task := storage.Task{Description: "do #a #b #c #d #e", Status: storage.StatusOpen}
	row := ansiSGR.ReplaceAllString(renderRow(task, "", false, 60, task.Tags()), "")
	for _, want := range []string{"#a", "#b", "#c", "+2"} {
		if !strings.Contains(row, want) {
			t.Errorf("row missing %q: %q", want, row)
		}
	}
	for _, gone := range []string{"#d", "#e"} {
		if strings.Contains(row, gone) {
			t.Errorf("overflowed tag %q should be hidden: %q", gone, row)
		}
	}
}
