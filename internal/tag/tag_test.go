package tag

import (
	"reflect"
	"testing"
)

func TestFromMarkdown(t *testing.T) {
	cases := []struct {
		name string
		body string
		want []string
	}{
		{"none", "just a plain description", nil},
		{"bare", "ship the thing #later", []string{"later"}},
		{"multiple", "a #p3 task that is #later", []string{"p3", "later"}},
		{"nested", "cook #recipes/italian tonight", []string{"recipes/italian"}},
		{"multiword", "plan the #vacation plans# now", []string{"vacation plans"}},
		{"multiword nested", "do #school projects/biology 101# please", []string{"school projects/biology 101"}},
		{"spaceless close", "tag #foo# here", []string{"foo"}},
		{"three bare keep separate", "#a #b #c", []string{"a", "b", "c"}},
		{"escaped is literal", `a \#nottag here`, nil},
		{"adjacency needs boundary", "word#tag not a tag", nil},
		{"pure numeric rejected", "see #3 and #2026 done", nil},
		{"alnum kept", "priority #p3 only", []string{"p3"}},
		{"stops at punctuation", "we should #ship. today", []string{"ship"}},
		{"case-insensitive unique", "#Work and #work", []string{"Work"}},
		{"inside code span skipped", "use `#nope` but tag #yes", []string{"yes"}},
		{"start of text", "#first thing", []string{"first"}},
		{"trailing hyphen trimmed", "see #thread- now", []string{"thread"}},
		{"trailing underscore trimmed", "ping #later_ then", []string{"later"}},
		{"trailing slash trimmed", "ref #a/b/ end", []string{"a/b"}},
		{"internal separators kept", "do #a/b-c_d now", []string{"a/b-c_d"}},
		{"multiple trailing separators trimmed", "x #goal--/ y", []string{"goal"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := FromMarkdown(tc.body); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("FromMarkdown(%q) = %v, want %v", tc.body, got, tc.want)
			}
		})
	}
}

func TestFromMarkdownSkipsFencedCode(t *testing.T) {
	body := "real #tag\n\n```\n#notatag inside fence\n```\n"
	if got := FromMarkdown(body); !reflect.DeepEqual(got, []string{"tag"}) {
		t.Errorf("FromMarkdown() = %v, want [tag]", got)
	}
}

func TestSpans(t *testing.T) {
	s := "do #p3 then #vacation plans# ok"
	got := Spans(s)
	want := []Span{
		{Name: "p3", Start: 3, End: 6},
		{Name: "vacation plans", Start: 12, End: 28},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Spans() = %#v, want %#v", got, want)
	}
	// The ranges must cover the literal tag text including '#' delimiters.
	if s[got[0].Start:got[0].End] != "#p3" {
		t.Errorf("span0 covers %q, want %q", s[got[0].Start:got[0].End], "#p3")
	}
	if s[got[1].Start:got[1].End] != "#vacation plans#" {
		t.Errorf("span1 covers %q, want %q", s[got[1].Start:got[1].End], "#vacation plans#")
	}
}

func TestSpansEscapeAndAdjacency(t *testing.T) {
	if got := Spans(`a \#escaped and word#joined`); got != nil {
		t.Errorf("Spans found tags in escaped/adjacent input: %#v", got)
	}
}

// A hash anchor in prose paints only the tag, not the trailing separator and
// the text glued after it.
func TestSpansTrimsTrailingSeparator(t *testing.T) {
	s := "deep link #thread-<id> now"
	got := Spans(s)
	want := []Span{{Name: "thread", Start: 10, End: 17}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Spans() = %#v, want %#v", got, want)
	}
	if s[got[0].Start:got[0].End] != "#thread" {
		t.Errorf("painted span = %q, want %q", s[got[0].Start:got[0].End], "#thread")
	}
}

func TestHas(t *testing.T) {
	set := []string{"p3", "journal/2024", "Work"}
	cases := []struct {
		want string
		ok   bool
	}{
		{"p3", true},
		{"P3", true},      // case-insensitive
		{"journal", true}, // hierarchy: matches journal/2024
		{"journal/2024", true},
		{"work", true},
		{"later", false},
		{"jour", false}, // prefix without '/' boundary does not match
	}
	for _, c := range cases {
		if got := Has(set, c.want); got != c.ok {
			t.Errorf("Has(%v, %q) = %v, want %v", set, c.want, got, c.ok)
		}
	}
}
