package cli

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestHighlightTermsUnicode(t *testing.T) {
	cases := []struct {
		name, s, term, marked string
	}{
		{"ascii prefix", "macOS rocks", "mac", "macOS"},
		{"accented word", "café au lait", "caf", "café"},
		{"special fold", "İİİ trip", "i", "İİİ"},
		{"mid-string accent", "a café here", "caf", "café"},
	}
	for _, tc := range cases {
		got := highlightTerms(tc.s, []string{tc.term})
		if !utf8.ValidString(got) {
			t.Errorf("%s: output is not valid UTF-8: %q", tc.name, got)
		}
		want := markStart + tc.marked + markEnd
		if !strings.Contains(got, want) {
			t.Errorf("%s: highlightTerms(%q, %q) = %q; want it to contain %q",
				tc.name, tc.s, tc.term, got, want)
		}
	}
}
