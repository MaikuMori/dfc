package capture

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/MaikuMori/dfc/internal/storage"
)

// fixedEntropy returns a deterministic byte stream.
type fixedEntropy struct{ b *bytes.Buffer }

func (f *fixedEntropy) Read(p []byte) (int, error) { return f.b.Read(p) }

func TestNew_Basic(t *testing.T) {
	now := time.Date(2026, 5, 13, 14, 0, 0, 0, time.UTC)
	opts := Options{
		Now:     func() time.Time { return now },
		Entropy: &fixedEntropy{b: bytes.NewBuffer(bytes.Repeat([]byte{0x42}, 32))},
	}
	task, slug, err := New("  Buy milk  ", opts)
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != storage.StatusOpen {
		t.Errorf("status = %s", task.Status)
	}
	if task.Description != "Buy milk" {
		t.Errorf("description = %q", task.Description)
	}
	if !task.Created.Equal(now) {
		t.Errorf("created = %v", task.Created)
	}
	if task.ID == "" {
		t.Error("empty id")
	}
	if !strings.HasSuffix(slug, "-buy-milk") {
		t.Errorf("slug = %q", slug)
	}
	wantPrefix := strings.ToLower(task.ID[:10]) + "-"
	if !strings.HasPrefix(slug, wantPrefix) {
		t.Errorf("slug %q does not start with timestamp prefix %q", slug, wantPrefix)
	}
}

func TestNew_EmptyDescription(t *testing.T) {
	opts := Options{
		Now:     func() time.Time { return time.Unix(0, 0).UTC() },
		Entropy: &fixedEntropy{b: bytes.NewBuffer(bytes.Repeat([]byte{0x01}, 32))},
	}
	_, slug, err := New("   ", opts)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(slug, "-task") {
		t.Errorf("expected fallback slug, got %q", slug)
	}
}

func TestSplitHeading(t *testing.T) {
	cases := []struct {
		name      string
		in        string
		wantHead  string
		wantBody  string
	}{
		{"empty", "", "", ""},
		{"blank only", "   \n\t\n", "", ""},
		{"single line", "buy milk", "buy milk", ""},
		{"heading then body", "buy milk\n\ndetails here", "buy milk", "details here"},
		{"leading blanks", "\n\n  buy milk  \n\ndetails", "buy milk", "details"},
		{"body with blank line", "title\n\nbody1\n\nbody2", "title", "body1\n\nbody2"},
		{"trailing whitespace stripped", "title\nbody\n  \n\n", "title", "body"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h, b := SplitHeading(c.in)
			if h != c.wantHead || b != c.wantBody {
				t.Errorf("SplitHeading(%q) = (%q, %q); want (%q, %q)", c.in, h, b, c.wantHead, c.wantBody)
			}
		})
	}
}

func TestNew_LongDescription(t *testing.T) {
	opts := Options{
		Now:     func() time.Time { return time.Unix(0, 0).UTC() },
		Entropy: &fixedEntropy{b: bytes.NewBuffer(bytes.Repeat([]byte{0x01}, 32))},
	}
	long := strings.Repeat("a-very-long-fragment ", 10)
	_, slug, err := New(long, opts)
	if err != nil {
		t.Fatal(err)
	}
	// 10-char timestamp prefix + "-" + MaxDescSlugLen upper bound.
	if len(slug) > 10+1+storage.MaxDescSlugLen {
		t.Errorf("slug too long: %d %q", len(slug), slug)
	}
}
