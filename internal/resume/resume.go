// Package resume persists unsent TUI input so it outlives a killed terminal:
// one draft file per running instance, named by a ULID, under a user-only
// directory. Each file records the process that owns it; a later instance
// takes over a draft whose owner is gone by renaming the file onto its own
// path, so only one instance can recover it.
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

// MaxAge is how long a draft file survives without being saved or loaded.
const MaxAge = 30 * 24 * time.Hour

// Draft kinds.
const (
	KindCapture = "capture"
	KindEdit    = "edit"
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

// record is the on-disk form: the draft plus the process that owns it.
type record struct {
	Draft
	PID  int    `json:"pid"`
	Host string `json:"host,omitempty"`
}

// Orphan is a draft whose owning process is no longer running on this host.
type Orphan struct {
	Path  string
	Draft Draft
}

// IsZero reports whether d holds no draft.
func (d Draft) IsZero() bool { return d == Draft{} }

// Dir returns the resume directory under the dfc root.
func Dir(root string) string { return filepath.Join(root, "resume") }

// NewPath returns a fresh per-instance draft path inside dir.
func NewPath(dir string) string {
	return filepath.Join(dir, strings.ToLower(ulid.Make().String())+".json")
}

func readRecord(path string) (record, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return record{}, false
	}
	var r record
	if json.Unmarshal(data, &r) != nil || (r.Kind != KindCapture && r.Kind != KindEdit) {
		return record{}, false
	}
	return r, true
}

// Load reads the draft at path. A missing or unreadable file yields a zero
// Draft.
func Load(path string) Draft {
	r, _ := readRecord(path)
	return r.Draft
}

// Claim moves the draft at src onto dst, taking it over for this process,
// and returns it. It yields a zero Draft when src is missing, unreadable, or
// claimed first by another instance.
func Claim(src, dst string) Draft {
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return Draft{}
	}
	if err := os.Rename(src, dst); err != nil {
		return Draft{}
	}
	r, ok := readRecord(dst)
	if !ok {
		_ = os.Remove(dst)
		return Draft{}
	}
	if Save(dst, r.Draft) != nil {
		return Draft{}
	}
	return r.Draft
}

// Orphans lists the drafts in dir whose owner is no longer running on this
// host, newest first.
func Orphans(dir string) []Orphan {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	host := hostname()
	type found struct {
		Orphan
		mod time.Time
	}
	var out []found
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		path := filepath.Join(dir, e.Name())
		r, ok := readRecord(path)
		if !ok || r.Host != host || r.PID == os.Getpid() || processAlive(r.PID) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, found{Orphan{Path: path, Draft: r.Draft}, info.ModTime()})
	}
	slices.SortFunc(out, func(a, b found) int { return b.mod.Compare(a.mod) })
	orphans := make([]Orphan, len(out))
	for i, f := range out {
		orphans[i] = f.Orphan
	}
	return orphans
}

func hostname() string {
	h, _ := os.Hostname()
	return h
}

// Save writes d to path atomically, owned by this process, or removes path
// when d is zero.
func Save(path string, d Draft) error {
	if d.IsZero() {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(record{Draft: d, PID: os.Getpid(), Host: hostname()})
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(path, data, 0o600)
}

// Prune removes draft files in dir not saved within maxAge.
func Prune(dir string, maxAge time.Duration) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-maxAge)
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		if info, err := e.Info(); err == nil && info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}
