package ui

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"

	"github.com/MaikuMori/dfc/internal/core"
	"github.com/MaikuMori/dfc/internal/storage"
)

// newTagModel boots a Model under a fresh DFC_ROOT, seeds three projects (two
// tagged, one untagged), and returns it ready to drive global-view logic
// without touching bubbletea's program lifecycle.
func newTagModel(t *testing.T) Model {
	t.Helper()
	root := t.TempDir()
	t.Setenv(storage.EnvRoot, root)

	cr, err := core.Open(core.Options{Warn: func(string, error) {}})
	if err != nil {
		t.Fatalf("core.Open: %v", err)
	}
	t.Cleanup(func() { _ = cr.Close() })

	for _, p := range []struct{ slug, desc string }{
		{"acme", "buy milk"},
		{"beta", "ship release"},
		{"gamma", "lonely task"},
	} {
		if _, err := cr.Capture(core.CaptureInput{Slug: p.slug, Description: p.desc, DisplayName: p.slug}); err != nil {
			t.Fatalf("Capture(%s): %v", p.slug, err)
		}
	}
	if err := cr.Registry().SetTags("acme", []string{"work"}); err != nil {
		t.Fatal(err)
	}
	if err := cr.Registry().SetTags("beta", []string{"work", "personal"}); err != nil {
		t.Fatal(err)
	}
	// gamma stays untagged.

	store, err := cr.StoreFor("acme")
	if err != nil {
		t.Fatal(err)
	}
	all, _ := cr.ListAll()
	m := New(cr, "acme", store, all, nil)
	m.globalView = true
	m.width = 80
	m.height = 24
	m.relayout()
	return m
}

func TestBuildRowsCacheInvalidatesOnChange(t *testing.T) {
	m := newTagModel(t)
	m.width = 80
	if _, h := buildRows(m, 80); len(h) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(h))
	}

	// A status + mtime change must invalidate that row's cache entry.
	m.tasks[0].Status = storage.StatusDone
	m.tasks[0].Modified = m.tasks[0].Modified.Add(time.Second)
	rows, _ := buildRows(m, 80)
	if !strings.Contains(rows[0], iconDone) {
		t.Errorf("row 0 should show the done icon after a status change, got %q", rows[0])
	}
}

func TestProjectPrefixTruncatesByWidth(t *testing.T) {
	m := newTagModel(t)
	if err := m.core.Registry().SetPrefix("acme", "🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀"); err != nil {
		t.Fatal(err)
	}
	p := projectPrefix(m, storage.Task{ProjectSlug: "acme"})
	if !utf8.ValidString(p) {
		t.Errorf("prefix is not valid UTF-8 (mid-rune cut): %q", p)
	}
	if w := runewidth.StringWidth(p); w > prefixMaxLen+1 {
		t.Errorf("prefix display width %d exceeds the %d cap: %q", w, prefixMaxLen+1, p)
	}
}

func TestIsPickerMode(t *testing.T) {
	for _, md := range []mode{modeSwitch, modeCaptureTarget, modeMoveTarget, modeTagEdit} {
		if !(Model{mode: md}).isPickerMode() {
			t.Errorf("mode %v should be a picker mode", md)
		}
	}
	for _, md := range []mode{modeList, modeCapture, modeEdit, modeSearch, modeHelp} {
		if (Model{mode: md}).isPickerMode() {
			t.Errorf("mode %v should not be a picker mode", md)
		}
	}
}
