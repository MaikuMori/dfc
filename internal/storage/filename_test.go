package storage

import "testing"

func TestFilenameSlug(t *testing.T) {
	cases := []struct {
		ts, desc, want string
	}{
		{"01krhb3zpr", "Buy milk", "01krhb3zpr-buy-milk"},
		{"01krhb3zpr", "  ", "01krhb3zpr-task"},
		{"ABC", "Mixed Case", "abc-mixed-case"},
	}
	for _, c := range cases {
		got := FilenameSlug(c.ts, c.desc)
		if got != c.want {
			t.Errorf("FilenameSlug(%q,%q) = %q, want %q", c.ts, c.desc, got, c.want)
		}
	}
}

func TestFilenameSlug_TruncatesOnHyphen(t *testing.T) {
	long := "one two three four five six seven eight nine ten eleven twelve"
	out := FilenameSlug("xyz0000000", long)
	// 10 + 1 + MaxDescSlugLen upper bound.
	if len(out) > 10+1+MaxDescSlugLen {
		t.Errorf("too long: %d %q", len(out), out)
	}
	if out[len(out)-1] == '-' {
		t.Errorf("trailing hyphen: %q", out)
	}
}

func TestFilenameTimestamp(t *testing.T) {
	if got := FilenameTimestamp("01krhb3zpr-buy-milk"); got != "01krhb3zpr" {
		t.Errorf("got %q", got)
	}
	if got := FilenameTimestamp("nohyphen"); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}
