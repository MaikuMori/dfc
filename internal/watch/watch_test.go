package watch

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func waitEvent(ch <-chan Event) bool {
	select {
	case <-ch:
		return true
	case <-time.After(2 * time.Second):
		return false
	}
}

func newWatcher(t *testing.T) (*Watcher, string) {
	t.Helper()
	w, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })
	dir := t.TempDir()
	if err := w.Add(dir); err != nil {
		t.Fatalf("Add: %v", err)
	}
	return w, dir
}

func TestDoubleCloseIsSafe(t *testing.T) {
	w, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Errorf("first Close: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

func TestWatchListAddRemove(t *testing.T) {
	w, dir := newWatcher(t)
	if n := len(w.WatchList()); n != 1 {
		t.Errorf("after Add, WatchList len = %d, want 1", n)
	}
	if err := w.Remove(dir); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if n := len(w.WatchList()); n != 0 {
		t.Errorf("after Remove, WatchList len = %d, want 0", n)
	}
}

func TestSubscribeReceivesEventOnChange(t *testing.T) {
	w, dir := newWatcher(t)
	sub := w.Subscribe()
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !waitEvent(sub) {
		t.Error("expected an event after writing a file")
	}
}

func TestMultiSubscriberFanOut(t *testing.T) {
	w, dir := newWatcher(t)
	a, b := w.Subscribe(), w.Subscribe()
	if err := os.WriteFile(filepath.Join(dir, "x.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !waitEvent(a) {
		t.Error("subscriber A received no event")
	}
	if !waitEvent(b) {
		t.Error("subscriber B received no event")
	}
}

func TestCoalesceCollapsesBurst(t *testing.T) {
	w, dir := newWatcher(t)
	sub := w.Subscribe()
	for i := range 5 {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%d.md", i)), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// A debounced burst should fan out as one (occasionally two) events,
	// never one per write.
	count := 0
	timer := time.NewTimer(700 * time.Millisecond)
	defer timer.Stop()
loop:
	for {
		select {
		case <-sub:
			count++
		case <-timer.C:
			break loop
		}
	}
	if count == 0 {
		t.Error("expected at least one event from the burst")
	}
	if count >= 5 {
		t.Errorf("burst should coalesce, got %d events", count)
	}
}

func TestSlowSubscriberDoesNotBlock(t *testing.T) {
	w, dir := newWatcher(t)
	slow := w.Subscribe() // never drained — buffers one, drops the rest
	fast := w.Subscribe()

	if err := os.WriteFile(filepath.Join(dir, "1.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !waitEvent(fast) {
		t.Fatal("fast subscriber missed the first event")
	}
	if err := os.WriteFile(filepath.Join(dir, "2.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The full slow channel must not stall the run loop — fast still gets the
	// second event.
	if !waitEvent(fast) {
		t.Error("a full subscriber channel blocked the broadcast")
	}
	_ = slow
}
