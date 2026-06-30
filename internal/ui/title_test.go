package ui

import "testing"

func TestWindowTitlePerProjectNameFirst(t *testing.T) {
	m := Model{slug: "github-com-acme-widget"}
	if got, want := m.windowTitle(), "github-com-acme-widget — dfc"; got != want {
		t.Fatalf("windowTitle() = %q, want %q", got, want)
	}
}

func TestWindowTitleGlobalView(t *testing.T) {
	m := Model{globalView: true}
	if got, want := m.windowTitle(), "all — dfc"; got != want {
		t.Fatalf("windowTitle() = %q, want %q", got, want)
	}
}
