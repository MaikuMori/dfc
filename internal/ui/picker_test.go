package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// keyPress synthesizes a printable-rune KeyPressMsg the same way bubbletea
// delivers terminal key events.
func keyPress(s string) tea.KeyPressMsg {
	switch s {
	case "space":
		return tea.KeyPressMsg{Code: ' ', Text: " "}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	}
	if len(s) == 1 {
		return tea.KeyPressMsg{Code: rune(s[0]), Text: s}
	}
	return tea.KeyPressMsg{Text: s}
}

func TestPicker_ManyModeToggleAndCommit(t *testing.T) {
	items := []PickerItem{
		{Slug: "work", Name: "work", Count: 3},
		{Slug: "personal", Name: "personal", Count: 1},
		{Slug: "oss", Name: "oss", Count: 2},
	}
	p := NewPicker("tags", items)
	p.SetMode(modeSelectMany)

	// Cursor starts at 0 → the bottom row visually, which is items[matches[0]] == "work" (insertion order).
	// Toggle it on.
	p, _ = p.Update(keyPress("space"))
	// Move up the bottom-up list (cursor++) to next row, toggle.
	p, _ = p.Update(keyPress("up"))
	p, _ = p.Update(keyPress("space"))
	// Commit.
	p, _ = p.Update(keyPress("enter"))

	if !p.Done() {
		t.Fatalf("enter in many-mode should mark Done")
	}
	got := p.Selection()
	if len(got) != 2 {
		t.Fatalf("expected 2 selected, got %v", got)
	}
}

func TestPicker_ManyModeClearKey(t *testing.T) {
	items := []PickerItem{{Slug: "a", Name: "a"}, {Slug: "b", Name: "b"}}
	p := NewPicker("", items)
	p.SetMode(modeSelectMany)
	p.PreselectMany([]string{"a", "b"})

	p, _ = p.Update(tea.KeyPressMsg{Code: 'l', Mod: tea.ModCtrl})
	if len(p.Selection()) != 0 {
		t.Errorf("ctrl+l should clear the selection, got %v", p.Selection())
	}
}

func TestPicker_ManyModeAddItem(t *testing.T) {
	items := []PickerItem{{Slug: "work", Name: "work"}}
	p := NewPicker("", items)
	p.SetMode(modeSelectMany)
	added := ""
	p.OnAdd = func(name string) { added = name }

	// ctrl+n opens the add-item prompt; type "personal"; enter.
	p, _ = p.Update(tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl})
	for _, r := range "personal" {
		p, _ = p.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	p, _ = p.Update(keyPress("enter"))

	if added != "personal" {
		t.Errorf("OnAdd not called with expected value: %q", added)
	}
	if !p.chosen["personal"] {
		t.Errorf("new item should be auto-selected after add")
	}
	if len(p.items) != 2 {
		t.Errorf("new item should be appended to items list, got %d", len(p.items))
	}
}

func TestPicker_EnableTagEditSignal(t *testing.T) {
	items := []PickerItem{{Slug: "alpha", Name: "Alpha"}, {Slug: "beta", Name: "Beta"}}
	p := NewPicker("", items)
	p.EnableTagEdit = true

	p, _ = p.Update(tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})
	if !p.Done() {
		t.Fatalf("ctrl+t should exit the picker when EnableTagEdit is on")
	}
	if p.WantTagEditSlug == "" {
		t.Errorf("WantTagEditSlug should be set")
	}
	if p.Selected() != "" {
		t.Errorf("Selected should be empty — caller distinguishes via WantTagEditSlug")
	}
}

func TestPicker_TagEditDisabledByDefault(t *testing.T) {
	items := []PickerItem{{Slug: "alpha", Name: "Alpha"}}
	p := NewPicker("", items)
	// EnableTagEdit defaults to false.
	p, _ = p.Update(tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})
	if p.Done() {
		t.Errorf("ctrl+t should be inert without EnableTagEdit")
	}
}

func TestPicker_EditPrefixPlaceholder(t *testing.T) {
	items := []PickerItem{{Slug: "alpha", Name: "Alpha", Prefix: ""}}
	p := NewPicker("", items)
	p.OnSetPrefix = func(string, string) error { return nil }
	if p.input.Placeholder != "filter" {
		t.Fatalf("seed placeholder = %q", p.input.Placeholder)
	}
	p, _ = p.Update(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	if p.input.Placeholder != "prefix" {
		t.Errorf("editPrefix should set placeholder = %q, got %q", "prefix", p.input.Placeholder)
	}
	p, _ = p.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if p.input.Placeholder != "filter" {
		t.Errorf("exitEdit should restore placeholder to 'filter', got %q", p.input.Placeholder)
	}
}

func TestPicker_ExitEditResetsPlaceholder(t *testing.T) {
	items := []PickerItem{{Slug: "work", Name: "work"}}
	p := NewPicker("", items)
	p.SetMode(modeSelectMany)
	p.OnAdd = func(string) {}
	// Sanity: the picker starts with "filter" as the placeholder.
	if p.input.Placeholder != "filter" {
		t.Fatalf("starting placeholder = %q", p.input.Placeholder)
	}
	// Open the new-tag prompt — placeholder flips to "new tag".
	p, _ = p.Update(tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl})
	if p.input.Placeholder != "new tag" {
		t.Fatalf("editAddItem should set placeholder to 'new tag', got %q", p.input.Placeholder)
	}
	// Esc out — placeholder must reset to "filter".
	p, _ = p.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if p.input.Placeholder != "filter" {
		t.Errorf("exitEdit should restore placeholder to 'filter', got %q", p.input.Placeholder)
	}
}

func TestPicker_FocusSlug(t *testing.T) {
	items := []PickerItem{
		{Slug: "alpha", Name: "Alpha"},
		{Slug: "beta", Name: "Beta"},
		{Slug: "gamma", Name: "Gamma"},
	}
	p := NewPicker("", items)
	p.FocusSlug("beta")
	if p.cursor != 1 {
		t.Errorf("cursor should land on beta (index 1), got %d", p.cursor)
	}
	// Unknown slug = no-op (cursor stays put).
	p.cursor = 0
	p.FocusSlug("not-a-slug")
	if p.cursor != 0 {
		t.Errorf("unknown slug should leave cursor at 0, got %d", p.cursor)
	}
}

func TestPicker_SingleModeUnchanged(t *testing.T) {
	items := []PickerItem{{Slug: "alpha", Name: "alpha"}, {Slug: "beta", Name: "beta"}}
	p := NewPicker("", items)
	// Default mode is single-select.
	p, _ = p.Update(keyPress("space"))
	// Space in single-select is a no-op (no chosen set).
	if len(p.chosen) != 0 {
		t.Errorf("space should not mutate selection in single-select mode")
	}
	p, _ = p.Update(keyPress("enter"))
	if p.Selected() == "" {
		t.Errorf("enter should commit a single selection")
	}
}
