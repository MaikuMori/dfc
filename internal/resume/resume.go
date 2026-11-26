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
	"sync"
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
	// Launch is the project the TUI started in, kept across `P` switches.
	Launch string `json:"launch,omitempty"`
}

// State is one instance's saved view and draft.
type State struct {
	View  View  `json:"view"`
	Draft Draft `json:"draft,omitzero"`
}

// RootOf returns the dfc data root a state file lives under, or "" when path
// isn't inside a resume directory.
func RootOf(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil || filepath.Base(filepath.Dir(abs)) != "resume" {
		return ""
	}
	return filepath.Dir(filepath.Dir(abs))
}

// record is the on-disk form. Machine scopes liveness to the machine that
// wrote it: locks don't cross machines when the data root is synced.
type record struct {
	State
	Machine string `json:"machine,omitempty"`
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
	data, err := json.Marshal(record{State: s, Machine: machineID()})
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
	// Windows can't delete a file that is still open.
	_ = i.lock.Close()
	if rerr := os.Remove(lockPath(i.path)); rerr != nil && !errors.Is(rerr, fs.ErrNotExist) && err == nil {
		err = rerr
	}
	return err
}

// Claim takes over the abandoned state file at src: it must be a state file
// in this instance's directory, written on this host, whose owner is gone.
// It is renamed onto this instance's path, which this instance's lock already
// guards, so the record is never without an owner or a copy. ok is false
// when src doesn't qualify or another instance claimed it first; src is left
// untouched unless it was taken.
func (i *Instance) Claim(src string) (State, bool) {
	src, err := filepath.Abs(src)
	if err != nil || !sameDir(filepath.Dir(src), filepath.Dir(i.path)) || filepath.Ext(src) != stateExt || src == i.path {
		return State{}, false
	}
	if r, ok := readRecord(src); !ok || r.Machine != machineID() || ownerAlive(src) {
		return State{}, false
	}
	if err := os.Rename(src, i.path); err != nil {
		return State{}, false
	}
	_ = os.Remove(lockPath(src))
	r, ok := readRecord(i.path)
	return r.State, ok
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
	machine := machineID()
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
		if !ok || r.Draft.IsZero() || r.Machine != machine || ownerAlive(path) {
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
	now := time.Now()
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		path := filepath.Join(dir, e.Name())
		info, err := e.Info()
		if err != nil {
			continue
		}
		switch {
		case filepath.Ext(e.Name()) == stateExt:
			// Another machine's lock can't be seen from here.
			if r, ok := readRecord(path); ok && r.Machine != "" && r.Machine != machineID() {
				continue
			}
			if info.ModTime().Before(now.Add(-maxAge)) && !ownerAlive(path) {
				_ = os.Remove(path)
				_ = os.Remove(lockPath(path))
			}
		case filepath.Ext(e.Name()) == lockExt:
			// A lock whose state file is gone, left by a crash. Young ones may
			// belong to an instance between creating and locking it.
			state := strings.TrimSuffix(path, lockExt) + stateExt
			if _, err := os.Stat(state); os.IsNotExist(err) && info.ModTime().Before(now.Add(-time.Hour)) && !ownerAlive(state) {
				_ = os.Remove(path)
			}
		case strings.HasPrefix(e.Name(), ".") && strings.Contains(e.Name(), stateExt+".tmp-") && info.ModTime().Before(now.Add(-time.Hour)):
			// A temp file from a write that was interrupted.
			_ = os.Remove(path)
		}
	}
}

func sameDir(a, b string) bool {
	if a == b {
		return true
	}
	ai, aerr := os.Stat(a)
	bi, berr := os.Stat(b)
	return aerr == nil && berr == nil && os.SameFile(ai, bi)
}

// publishMachineID creates the machine id file, or adopts the one another
// process created first: the id is written to a temp file and hard-linked
// into place, which fails rather than replacing an existing file.
func publishMachineID(path string) (string, bool) {
	if os.MkdirAll(filepath.Dir(path), 0o700) != nil {
		return "", false
	}
	id := strings.ToLower(ulid.Make().String())
	tmp := path + "." + id
	if fsutil.WriteFileAtomic(tmp, []byte(id+"\n"), 0o600) != nil {
		return "", false
	}
	defer func() { _ = os.Remove(tmp) }()
	if os.Link(tmp, path) == nil {
		return id, true
	}
	if data, err := os.ReadFile(path); err == nil && strings.TrimSpace(string(data)) != "" {
		return strings.TrimSpace(string(data)), true
	}
	return "", false
}

func lockPath(state string) string { return strings.TrimSuffix(state, stateExt) + lockExt }

// ownerAlive reports whether a running instance holds state's lock. The probe
// takes a shared lock, which only an owner's exclusive lock refuses, so two
// instances probing at once don't mistake each other for the owner. A lock
// taken through another descriptor conflicts even within this process, so an
// instance sees its own file as alive.
func ownerAlive(state string) bool {
	f, err := os.OpenFile(lockPath(state), os.O_RDWR, 0)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	return !tryLockShared(f)
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

// EnvMachineID overrides the machine id (tests).
const EnvMachineID = "DFC_MACHINE_ID"

// machineID identifies this machine. It lives in the user config dir rather
// than the data root, which may be synced between machines, or a cache that
// may be purged, and is generated once because hostnames change with the
// network.
var machineID = sync.OnceValue(func() string {
	if v := os.Getenv(EnvMachineID); v != "" {
		return v
	}
	dir, err := os.UserConfigDir()
	if err == nil {
		path := filepath.Join(dir, "dfc", "machine-id")
		if data, err := os.ReadFile(path); err == nil && len(strings.TrimSpace(string(data))) > 0 {
			return strings.TrimSpace(string(data))
		}
		if id, ok := publishMachineID(path); ok {
			return id
		}
	}
	h, _ := os.Hostname()
	return h
})
