package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/MaikuMori/dfc/internal/storage"
)

func TestYankIDPopulatesStatusAndCmd(t *testing.T) {
	m := Model{
		mode: modeList,
		tasks: []storage.Task{
			{ID: "01J9X7K3M8VQNH4Z7Y3PG2T5BD", Path: "/tmp/x.md"},
		},
		cursor: 0,
	}
	out, cmd := m.updateList(tea.KeyPressMsg{Code: 'y'})
	got := out.(Model)
	if !strings.Contains(got.status, "01J9X7K3M8VQNH4Z7Y3PG2T5BD") {
		t.Fatalf("status %q does not mention id", got.status)
	}
	if cmd == nil {
		t.Fatal("expected a SetClipboard cmd, got nil")
	}
}

func TestYankPathPopulatesStatusAndCmd(t *testing.T) {
	m := Model{
		mode: modeList,
		tasks: []storage.Task{
			{ID: "01J9X7K3M8VQNH4Z7Y3PG2T5BD", Path: "/tmp/x.md"},
		},
		cursor: 0,
	}
	out, cmd := m.updateList(tea.KeyPressMsg{Code: 'Y', ShiftedCode: 'Y'})
	got := out.(Model)
	if !strings.Contains(got.status, "path") {
		t.Fatalf("status %q does not mention path", got.status)
	}
	if cmd == nil {
		t.Fatal("expected a SetClipboard cmd, got nil")
	}
}

func TestYankNoopOnEmptyList(t *testing.T) {
	m := Model{mode: modeList}
	out, cmd := m.updateList(tea.KeyPressMsg{Code: 'y'})
	got := out.(Model)
	if got.status != "" {
		t.Fatalf("expected no status, got %q", got.status)
	}
	if cmd != nil {
		t.Fatal("expected nil cmd on empty list")
	}
}
