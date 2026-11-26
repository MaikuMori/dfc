// Package resume persists each TUI instance's view and unsent input so they
// outlive a killed terminal: one state file per running instance, named by a
// ULID, under a user-only directory. The instance holds an exclusive lock on
// a sibling `.lock` file for its lifetime; the OS drops that lock when the
// process dies, which is how a later instance tells a live owner from an
// abandoned file. Taking over a file is a rename, so only one instance wins.
package resume

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/MaikuMori/dfc/internal/fsutil"
)

// MaxAge is how long a state file survives without being saved.
const MaxAge = 30 * 24 * time.Hour

// Draft kinds.
const (
	KindCapture = "capture"
	KindEdit    = "edit"
)

const (
	stateExt = ".json"
	lockExt  = ".lock"
	claimExt = ".claim"
)

// Draft is unsubmitted input from the capture overlay or the inline editor.
type Draft struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
	// Slug is the project the draft belongs to: the capture target or the
	// edited task's project.
	Slug   string `json:"slug,omitempty"`
	TaskID string `json:"task_id,omitempty"`
}

// IsZero reports whether d holds no draft.
func (d Draft) IsZero() bool { return d == Draft{} }

// View is what the TUI was showing.
type View struct {
	Project string `json:"project,omitempty"`
	Global  bool   `json:"global,omitempty"`
	Query   string `json:"query,omitempty"`
	Sort    string `json:"sort,omitempty"`
	Cursor  string `json:"cursor,omitempty"`
}

// State is one instance's saved view and draft.
type State struct {
	// Root is the dfc data root the instance ran against, when it was
	// overridden; a resumed command may start in a fresh environment.
	Root  string `json:"root,omitempty"`
	View  View   `json:"view"`
	Draft Draft  `json:"draft,omitzero"`
}

// record is the on-disk form.
type record struct {
	State
	Host string `json:"host,omitempty"`
}

// Dir returns the resume directory under the dfc root.
func Dir(root string) string { return filepath.Join(root, "resume") }

// Instance owns one state file for the life of the process.
type Instance struct {
	path string
	lock *os.File
}

// Open creates a fresh instance in dir and locks it.
func Open(dir string) (*Instance, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, strings.ToLower(ulid.Make().String())+stateExt)
	f, err := os.OpenFile(lockPath(path), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if !tryLock(f) {
		_ = f.Close()
		return nil, errors.New("could not lock resume state")
	}
	return &Instance{path: path, lock: f}, nil
}

// Path is the instance's state file.
func (i *Instance) Path() string { return i.path }

// Save writes s atomically.
func (i *Instance) Save(s State) error {
	data, err := json.Marshal(record{State: s, Host: hostname()})
	if err != nil {
		return err
	}
	return fsutil.ReplaceFile(i.path, data, 0o600)
}

// Close releases the instance. With discard, the state file goes too;
// otherwise it stays for a later resume or recovery.
func (i *Instance) Close(discard bool) error {
	var err error
	if discard {
		if rerr := os.Remove(i.path); rerr != nil && !errors.Is(rerr, fs.ErrNotExist) {
			err = rerr
		}
	}
	_ = os.Remove(lockPath(i.path))
	_ = i.lock.Close()
	return err
}

// Claim takes over the abandoned state file at src: it must be a state file
// in this instance's directory, written on this host, whose owner is gone.
// The file is moved through a name no other instance scans, then saved as
// this instance's own. ok is false when src doesn't qualify or another
// instance claimed it first; src is left untouched unless it was taken.
func (i *Instance) Claim(src string) (State, bool) {
	src, err := filepath.Abs(src)
	if err != nil || filepath.Dir(src) != filepath.Dir(i.path) || filepath.Ext(src) != stateExt || src == i.path {
		return State{}, false
	}
	if r, ok := readRecord(src); !ok || r.Host != hostname() || ownerAlive(src) {
		return State{}, false
	}
	staging := i.path + claimExt
	if err := os.Rename(src, staging); err != nil {
		return State{}, false
	}
	defer func() { _ = os.Remove(staging) }()
	_ = os.Remove(lockPath(src))
	r, ok := readRecord(staging)
	if !ok || i.Save(r.State) != nil {
		return State{}, false
	}
	return r.State, true
}

// Read returns the state at path without claiming it.
func Read(path string) (State, bool) {
	r, ok := readRecord(path)
	return r.State, ok
}

// Orphans lists the abandoned state files in dir that hold a draft, newest
// first.
func Orphans(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	host := hostname()
	type found struct {
		path string
		mod  time.Time
	}
	var out []found
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != stateExt {
			continue
		}
		path := filepath.Join(dir, e.Name())
		r, ok := readRecord(path)
		if !ok || r.Draft.IsZero() || r.Host != host || ownerAlive(path) {
			continue
		}
		if info, err := e.Info(); err == nil {
			out = append(out, found{path, info.ModTime()})
		}
	}
	slices.SortFunc(out, func(a, b found) int { return b.mod.Compare(a.mod) })
	paths := make([]string, len(out))
	for i, f := range out {
		paths[i] = f.path
	}
	return paths
}

// Prune removes abandoned state files in dir not saved within maxAge.
func Prune(dir string, maxAge time.Duration) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-maxAge)
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != stateExt {
			continue
		}
		path := filepath.Join(dir, e.Name())
		if info, err := e.Info(); err == nil && info.ModTime().Before(cutoff) && !ownerAlive(path) {
			_ = os.Remove(path)
			_ = os.Remove(lockPath(path))
		}
	}
}

func lockPath(state string) string { return strings.TrimSuffix(state, stateExt) + lockExt }

// ownerAlive reports whether a running instance holds state's lock. A lock
// taken through another descriptor conflicts even within this process, so an
// instance sees its own file as alive.
func ownerAlive(state string) bool {
	f, err := os.OpenFile(lockPath(state), os.O_RDWR, 0)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	return !tryLock(f)
}

func readRecord(path string) (record, bool) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return record{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return record{}, false
	}
	var r record
	if json.Unmarshal(data, &r) != nil {
		return record{}, false
	}
	if k := r.Draft.Kind; !r.Draft.IsZero() && k != KindCapture && k != KindEdit {
		return record{}, false
	}
	return r, true
}

func hostname() string {
	h, _ := os.Hostname()
	return h
}
