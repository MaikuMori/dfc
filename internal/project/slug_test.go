package project

import "testing"

func TestSlugify(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"github.com/user/repo", "github-com-user-repo"},
		{"/Users/miks/code/My App", "users-miks-code-my-app"},
		{"  weird---input!!  ", "weird-input"},
		{"ALL_CAPS_123", "all-caps-123"},
		{"", ""},
		{"---", ""},
		{"a", "a"},
		{"a-b", "a-b"},
		{"a  b", "a-b"},
		{"gitlab.com/group/sub/repo", "gitlab-com-group-sub-repo"},
	}
	for _, c := range cases {
		got := Slugify(c.in)
		if got != c.want {
			t.Errorf("Slugify(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
