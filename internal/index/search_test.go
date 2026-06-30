package index

import "testing"

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
		{
			"hyphenated bare term is quoted",
			"oauth2-proxy",
			`"oauth2-proxy"`,
		},
		{
			"hyphenated negation is quoted",
			"oauth2-proxy -well-known",
			`("oauth2-proxy") NOT ("well-known")`,
		},
		{
			"hyphenated column qualifier is quoted",
			"description:foo-bar",
			`description:"foo-bar"`,
		},
		{
			"underscore stays a bare wildcard term",
			"foo_bar",
			"foo_bar*",
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
