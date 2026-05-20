// Package watch wraps fsnotify with project-aware path management and
// debounced fan-out to multiple subscribers.
//
// The TUI uses this today; a future menu-bar / second client subscribes
// to the same Watcher without re-implementing the fsnotify + debounce
// dance. One process, one Watcher, many subscribers.
//
// Subscribe returns a channel that receives a single Event per debounce
// window. Events carry no payload — the contract is "something under
// your watched paths changed; re-read state from disk." Channels buffer
// one event; bursts collapse into one wake-up per subscriber.
package watch

import (
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Debounce is the fixed-window coalescing interval. Editors typically
// fire 2–5 events per save; rsync-style burst restores fire many more.
// 100 ms catches every common burst with no perceptible UI lag.
const Debounce = 100 * time.Millisecond

// Event signals that at least one fsnotify event arrived within the
// debounce window. The payload is intentionally empty — subscribers
// reload from disk rather than trying to diff individual events.
type Event struct{}

// Watcher fans one fsnotify event stream out to multiple subscribers.
type Watcher struct {
	fs *fsnotify.Watcher

	mu      sync.Mutex
	subs    []chan Event
	closed  bool
	stop    chan struct{}
	doneRun chan struct{}
}

// New constructs a Watcher and starts its background fan-out loop.
// Returns the fsnotify error verbatim if the underlying watcher fails
// to initialize.
func New() (*Watcher, error) {
	fs, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	w := &Watcher{
		fs:      fs,
		stop:    make(chan struct{}),
		doneRun: make(chan struct{}),
	}
	go w.run()
	return w, nil
}

// Add attaches a filesystem path to the watch set. Wraps fsnotify.Add
// directly so callers can hand any path it accepts.
func (w *Watcher) Add(path string) error { return w.fs.Add(path) }

// Remove detaches a path. Missing paths return an error from fsnotify;
// callers usually ignore it.
func (w *Watcher) Remove(path string) error { return w.fs.Remove(path) }

// WatchList returns the current set of watched paths.
func (w *Watcher) WatchList() []string { return w.fs.WatchList() }

// Subscribe registers a new subscriber and returns the channel events
// will be delivered on. The channel is buffered to 1; if a subscriber
// falls behind, additional events drop silently rather than blocking
// the run loop. Subscriptions live until Close is called on the
// Watcher.
func (w *Watcher) Subscribe() <-chan Event {
	ch := make(chan Event, 1)
	w.mu.Lock()
	w.subs = append(w.subs, ch)
	w.mu.Unlock()
	return ch
}

// Close stops the run loop, releases the fsnotify watcher, and closes
// every subscriber channel. Safe to call more than once.
func (w *Watcher) Close() error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil
	}
	w.closed = true
	close(w.stop)
	w.mu.Unlock()
	err := w.fs.Close()
	<-w.doneRun
	w.mu.Lock()
	for _, ch := range w.subs {
		close(ch)
	}
	w.subs = nil
	w.mu.Unlock()
	return err
}

// run consumes fsnotify events, applies the debounce, and fans out to
// every subscriber. Exits when stop is closed or the fsnotify channel
// dies.
func (w *Watcher) run() {
	defer close(w.doneRun)
	for {
		select {
		case <-w.stop:
			return
		case _, ok := <-w.fs.Events:
			if !ok {
				return
			}
			w.coalesce()
			w.broadcast()
		case _, ok := <-w.fs.Errors:
			if !ok {
				return
			}
			// Errors short-circuit debounce: still broadcast so the UI
			// re-reads (worst case: redundant reload).
			w.broadcast()
		}
	}
}

// coalesce drains every additional event that arrives within the
// debounce window. We don't care about the payload, just about
// collapsing the burst into one downstream wake-up.
func (w *Watcher) coalesce() {
	timer := time.NewTimer(Debounce)
	defer timer.Stop()
	for {
		select {
		case _, ok := <-w.fs.Events:
			if !ok {
				return
			}
		case <-w.fs.Errors:
			// Discard during the burst; the next genuine error will
			// surface on its own.
		case <-timer.C:
			return
		case <-w.stop:
			return
		}
	}
}

// broadcast pushes one Event to every subscriber. Non-blocking — if a
// subscriber's channel is already full, the event is dropped for that
// subscriber. They'll re-read state on the next event regardless.
func (w *Watcher) broadcast() {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, ch := range w.subs {
		select {
		case ch <- Event{}:
		default:
		}
	}
}
