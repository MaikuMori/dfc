package ui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MaikuMori/dfc/internal/watch"
)

// TestWaitForChange_FiresOnWrite confirms the cmd unblocks within the
// debounce window after a file write in the watched directory.
func TestWaitForChange_FiresOnWrite(t *testing.T) {
	w, err := watch.New()
	if err != nil {
		t.Skipf("watch unavailable: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })

	dir := t.TempDir()
	if err := w.Add(dir); err != nil {
		t.Fatalf("Add: %v", err)
	}
	sub := w.Subscribe()

	type result struct {
		msg interface{}
	}
	done := make(chan result, 1)
	go func() {
		cmd := waitForChange(sub)
		done <- result{msg: cmd()}
	}()

	// Give the goroutine a moment to enter the select before we write.
	time.Sleep(20 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(dir, "hello.md"), []byte("hi"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	select {
	case r := <-done:
		if _, ok := r.msg.(fsChangedMsg); !ok {
			t.Fatalf("expected fsChangedMsg, got %T (%v)", r.msg, r.msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for fsChangedMsg")
	}
}

// TestWaitForChange_NilSub returns nil rather than panicking.
func TestWaitForChange_NilSub(t *testing.T) {
	if waitForChange(nil) != nil {
		t.Fatal("expected nil cmd for nil subscription")
	}
}
