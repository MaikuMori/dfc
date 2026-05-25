package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestIsEnterMatchesBothMainAndNumpad(t *testing.T) {
	cases := []struct {
		name string
		code rune
		want bool
	}{
		{"main enter", tea.KeyEnter, true},
		{"numpad enter", tea.KeyKpEnter, true},
		{"tab", tea.KeyTab, false},
		{"space", tea.KeySpace, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isEnter(tea.KeyPressMsg{Code: tc.code})
			if got != tc.want {
				t.Fatalf("isEnter(%q) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}
