package project

import "testing"

func TestDeriveName(t *testing.T) {
	cases := []struct {
		in   Resolved
		want string
	}{
		{Resolved{Slug: "github-com-acme-widget", Source: SourceRemote, Raw: "github.com/acme/widget"}, "acme/widget"},
		{Resolved{Slug: "gitlab-com-group-sub-repo", Source: SourceRemote, Raw: "gitlab.com/group/sub/repo"}, "sub/repo"},
		{Resolved{Slug: "users-miks-code-dfc", Source: SourceToplevel, Raw: "/Users/miks/code/dfc"}, "dfc"},
		{Resolved{Slug: "tmp-scratch", Source: SourceCwd, Raw: "/tmp/scratch"}, "scratch"},
	}
	for _, c := range cases {
		got := DeriveName(c.in)
		if got != c.want {
			t.Errorf("DeriveName(%+v) = %q, want %q", c.in, got, c.want)
		}
	}
}
