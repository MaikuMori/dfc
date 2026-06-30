package query

import (
	"reflect"
	"testing"
)

func TestTokenize(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []Token
	}{
		{"empty", "", nil},
		{"whitespace only", "  \t\n ", nil},
		{"bare term", "milk", []Token{{Value: "milk"}}},
		{"two terms", "milk bread", []Token{{Value: "milk"}, {Value: "bread"}}},
		{
			"phrase",
			`"milk and bread"`,
			[]Token{{Value: "milk and bread", Phrase: true}},
		},
		{
			"negation",
			"-bread",
			[]Token{{Value: "bread", Negate: true}},
		},
		{
			"mixed",
			`buy "milk and bread" -stale`,
			[]Token{
				{Value: "buy"},
				{Value: "milk and bread", Phrase: true},
				{Value: "stale", Negate: true},
			},
		},
		{
			"unterminated phrase",
			`buy "unterminated`,
			[]Token{
				{Value: "buy"},
				{Value: "unterminated", Phrase: true},
			},
		},
		{
			"standalone dash is a term",
			"-",
			[]Token{{Value: "-"}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Tokenize(c.in)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("Tokenize(%q) = %+v; want %+v", c.in, got, c.want)
			}
		})
	}
}

func TestPositiveTerms(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"milk", []string{"milk"}},
		{"Milk", []string{"milk"}},
		{"milk bread", []string{"milk", "bread"}},
		{"milk -bread", []string{"milk"}},
		{`"two words" extra`, []string{"two words", "extra"}},
		{"trailing*", []string{"trailing"}},
		{"-bread", nil},
	}
	for _, c := range cases {
		got := PositiveTerms(c.in)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("PositiveTerms(%q) = %v; want %v", c.in, got, c.want)
		}
	}
}

func TestSplit(t *testing.T) {
	cases := []struct {
		q                string
		text             string
		include, exclude []string
	}{
		{"migration", "migration", nil, nil},
		{"#p3", "", []string{"p3"}, nil},
		{"-#later", "", nil, []string{"later"}},
		{"#p3 migration", "migration", []string{"p3"}, nil},
		{"migration -#later", "migration", nil, []string{"later"}},
		{"#a/b nested", "nested", []string{"a/b"}, nil},
		{`"#literal" text`, `"#literal" text`, nil, nil},
		{"#P3", "", []string{"p3"}, nil},
		{"-word #tag", "-word", []string{"tag"}, nil},
	}
	for _, c := range cases {
		got := Split(c.q)
		if got.Text != c.text || !reflect.DeepEqual(got.Include, c.include) || !reflect.DeepEqual(got.Exclude, c.exclude) {
			t.Errorf("Split(%q) = {%q %v %v}, want {%q %v %v}",
				c.q, got.Text, got.Include, got.Exclude, c.text, c.include, c.exclude)
		}
	}
}

// Split's tag names come from the same grammar as task content (tag.Match):
// what isn't a tag in a task body isn't a tag predicate in a query either.
func TestSplitMatchesContentGrammar(t *testing.T) {
	cases := []struct {
		q                string
		text             string
		include, exclude []string
	}{
		{"#ship. today", "#ship. today", nil, nil}, // glued punctuation: not a tag
		{"#3 fix", "#3 fix", nil, nil},             // pure numeric: not a tag
		{"-#2026", "-#2026", nil, nil},
		{"#a -b#", "", []string{"a -b"}, nil},             // '-' is a tag character in a multi-word run
		{"#a b#c", "b#c", []string{"a"}, nil},             // run closing mid-token falls back to the bare tag
		{"#two   words#", "", []string{"two words"}, nil}, // tokenizing collapses inner runs of spaces
	}
	for _, c := range cases {
		got := Split(c.q)
		if got.Text != c.text || !reflect.DeepEqual(got.Include, c.include) || !reflect.DeepEqual(got.Exclude, c.exclude) {
			t.Errorf("Split(%q) = {%q %v %v}, want {%q %v %v}",
				c.q, got.Text, got.Include, got.Exclude, c.text, c.include, c.exclude)
		}
	}
}

func TestSplitWrapped(t *testing.T) {
	cases := []struct {
		q                string
		text             string
		include, exclude []string
	}{
		{"#foo#", "", []string{"foo"}, nil},
		{"#vacation plans#", "", []string{"vacation plans"}, nil},
		{"-#two words#", "", nil, []string{"two words"}},
		{"#a/b c# rest", "rest", []string{"a/b c"}, nil},
		{"#a #b#", "", []string{"a", "b"}, nil}, // not one multi-word tag
		{"text #multi word# more", "text more", []string{"multi word"}, nil},
		{`"#foo#" lit`, `"#foo#" lit`, nil, nil}, // quoted stays literal text
	}
	for _, c := range cases {
		got := Split(c.q)
		if got.Text != c.text || !reflect.DeepEqual(got.Include, c.include) || !reflect.DeepEqual(got.Exclude, c.exclude) {
			t.Errorf("Split(%q) = {%q %v %v}, want {%q %v %v}",
				c.q, got.Text, got.Include, got.Exclude, c.text, c.include, c.exclude)
		}
	}
}
