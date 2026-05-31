package index

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

func TestSanitizeFTSQuery(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"single bare gets wildcard", "mac", "mac*"},
		{"two bare ANDed with wildcards", "buy milk", "buy* AND milk*"},
		{"phrase exact", `"milk and bread"`, `"milk and bread"`},
		{
			"negation only returns empty",
			"-bread",
			"",
		},
		{
			"positive plus negation",
			"buy -stale",
			"(buy*) NOT (stale)",
		},
		{
			"explicit star preserved",
			"mac*",
			"mac*",
		},
		{
			"punctuated bare term is quoted (no wildcard)",
			"buy!",
			`"buy!"`,
		},
		{"bare star dropped", "*", ""},
		{"leading star stripped", "*foo", "foo*"},
		{"colon is literal text", "12:30", `"12:30"`},
		{"column qualifier", "description:bug", "description:bug*"},
		{"empty column qualifier dropped", "description:", ""},
		{"uppercase operator quoted", "AND", `"AND"`},
		{"trailing operator stays valid", "fix AND", `fix* AND "AND"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := sanitizeFTSQuery(c.in)
			if got != c.want {
				t.Errorf("sanitizeFTSQuery(%q) = %q; want %q", c.in, got, c.want)
			}
		})
	}
}
