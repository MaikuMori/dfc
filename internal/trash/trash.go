// Package trash provides a soft-delete tier for tasks and projects.
//
// Deletes move into ~/.dfc/trash/<ulid>/ along with a manifest.json that
// captures everything needed to reverse the move (kind, slug, task id,
// original filename, display name). A retention sweeper expires entries
// older than the configured TTL; sweep is best-effort and quiet.
//
// The package owns disk shape only. Registry and index updates are
// orchestrated by core.Core, which calls into this package.
package trash

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/MaikuMori/dfc/internal/fsutil"
	"github.com/MaikuMori/dfc/internal/storage"
	"github.com/oklog/ulid/v2"
)

// DefaultTTL is the retention window for trashed entries. Anything older
// is removed by SweepExpired on each Core.Open. Override per-invocation
// with DFC_TRASH_TTL_DAYS (parsed as a positive integer; <=0 disables
// expiration).
const DefaultTTL = 14 * 24 * time.Hour

// Kind enumerates what the trash entry represents.
type Kind string

const (
	KindTask    Kind = "task"
	KindProject Kind = "project"
)

// Manifest is the JSON document stored alongside each trash entry.
type Manifest struct {
	ID          string    `json:"id"`
	Kind        Kind      `json:"kind"`
	Slug        string    `json:"slug"`
	Name        string    `json:"name,omitempty"`         // project display name (kind=project)
	Prefix      string    `json:"prefix,omitempty"`       // custom display prefix (kind=project)
	Tags        []string  `json:"tags,omitempty"`         // categorical tags (kind=project)
	TaskID      string    `json:"task_id,omitempty"`      // ULID of the trashed task (kind=task)
	Description string    `json:"description,omitempty"`  // task heading (kind=task)
	Filename    string    `json:"filename,omitempty"`     // basename inside project dir (kind=task)
	DeletedAt   time.Time `json:"deleted_at"`
}

// Entry is the high-level view of a trash item used by listings and
// restoration. Built from manifest.json on demand.
type Entry struct {
	Manifest
	Dir string // absolute path to the trash entry directory
}

// TrashRoot returns ~/.dfc/trash, creating it on demand.
func TrashRoot() (string, error) {
	root, err := storage.Root()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, "trash")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// TTLFromEnv returns the configured retention window. Falls back to
// DefaultTTL when DFC_TRASH_TTL_DAYS is unset, malformed, or non-positive.
func TTLFromEnv() time.Duration {
	raw := strings.TrimSpace(os.Getenv("DFC_TRASH_TTL_DAYS"))
	if raw == "" {
		return DefaultTTL
	}
	var days int
	if _, err := fmt.Sscanf(raw, "%d", &days); err != nil || days <= 0 {
		return DefaultTTL
	}
	return time.Duration(days) * 24 * time.Hour
}

// newID returns a fresh ULID using crypto-quality entropy. Keys both
// the trash directory name and the manifest id.
func newID() string {
	return ulid.Make().String()
}

// TrashTask moves taskPath into the trash, capturing slug + task metadata
// in the manifest. The file is renamed (atomic on a single fs); on
// success the original path no longer exists.
func TrashTask(slug, taskID, desc, taskPath string) (Manifest, error) {
	if taskPath == "" {
		return Manifest{}, errors.New("missing task file path")
	}
	root, err := TrashRoot()
	if err != nil {
		return Manifest{}, err
	}
	m := Manifest{
		ID:          newID(),
		Kind:        KindTask,
		Slug:        slug,
		TaskID:      taskID,
		Description: desc,
		Filename:    filepath.Base(taskPath),
		DeletedAt:   time.Now().UTC().Truncate(time.Second),
	}
	entryDir := filepath.Join(root, m.ID)
	if err := os.MkdirAll(entryDir, 0o755); err != nil {
		return Manifest{}, err
	}
	// Write the manifest before moving the task in. The moved file is the
	// only copy, so it must never live in an entry that List can't read —
	// and on any failure here entryDir holds no data, so removing it is safe.
	if err := writeManifest(entryDir, m); err != nil {
		_ = os.RemoveAll(entryDir)
		return Manifest{}, err
	}
	dst := filepath.Join(entryDir, m.Filename)
	if err := os.Rename(taskPath, dst); err != nil {
		_ = os.RemoveAll(entryDir)
		return Manifest{}, fmt.Errorf("could not move %s to trash: %w", taskPath, err)
	}
	return m, nil
}

// TrashProject moves an entire project directory into the trash, capturing
// the project's display name, custom prefix, and tags so a restore can put
// them back.
func TrashProject(slug, name, prefix string, tags []string, projectDir string) (Manifest, error) {
	if projectDir == "" {
		return Manifest{}, errors.New("missing project directory")
	}
	root, err := TrashRoot()
	if err != nil {
		return Manifest{}, err
	}
	m := Manifest{
		ID:        newID(),
		Kind:      KindProject,
		Slug:      slug,
		Name:      name,
		Prefix:    prefix,
		Tags:      tags,
		DeletedAt: time.Now().UTC().Truncate(time.Second),
	}
	entryDir := filepath.Join(root, m.ID)
	if err := os.MkdirAll(entryDir, 0o755); err != nil {
		return Manifest{}, err
	}
	if err := writeManifest(entryDir, m); err != nil {
		_ = os.RemoveAll(entryDir)
		return Manifest{}, err
	}
	dst := filepath.Join(entryDir, "project")
	if err := os.Rename(projectDir, dst); err != nil {
		_ = os.RemoveAll(entryDir)
		return Manifest{}, fmt.Errorf("could not move %s to trash: %w", projectDir, err)
	}
	return m, nil
}

// RestoreTask moves the trashed task file back to its original project
// directory and removes the trash entry. The caller (Core) is responsible
// for ensuring the project still exists and re-adding the task to the
// search index.
func RestoreTask(e Entry, taskDestDir string) (string, error) {
	if e.Kind != KindTask {
		return "", fmt.Errorf("trash entry is a %s, not a task", e.Kind)
	}
	src := filepath.Join(e.Dir, e.Filename)
	dst := filepath.Join(taskDestDir, e.Filename)
	if _, err := os.Stat(dst); err == nil {
		return "", fmt.Errorf("a file already exists at %s", dst)
	}
	if err := os.MkdirAll(taskDestDir, 0o755); err != nil {
		return "", err
	}
	if err := os.Rename(src, dst); err != nil {
		return "", fmt.Errorf("could not restore task: %w", err)
	}
	// Remove the manifest before the recursive cleanup: once the file is
	// back, a leftover manifest would re-list an entry whose data is gone,
	// and that entry can't be restored again. A single-file unlink is far
	// more reliable than the RemoveAll that follows.
	_ = os.Remove(manifestPath(e.Dir))
	_ = os.RemoveAll(e.Dir)
	return dst, nil
}

// RestoreProject moves the trashed project directory back under
// ~/.dfc/projects/<slug>. Errors when a project with that slug already
// exists on disk.
func RestoreProject(e Entry, projectDest string) error {
	if e.Kind != KindProject {
		return fmt.Errorf("trash entry is a %s, not a project", e.Kind)
	}
	src := filepath.Join(e.Dir, "project")
	if _, err := os.Stat(projectDest); err == nil {
		return fmt.Errorf("a project already exists at %s", projectDest)
	}
	if err := os.MkdirAll(filepath.Dir(projectDest), 0o755); err != nil {
		return err
	}
	if err := os.Rename(src, projectDest); err != nil {
		return fmt.Errorf("could not restore project: %w", err)
	}
	_ = os.Remove(manifestPath(e.Dir))
	_ = os.RemoveAll(e.Dir)
	return nil
}

// List returns every entry currently in the trash, newest first.
func List() ([]Entry, error) {
	root, err := TrashRoot()
	if err != nil {
		return nil, err
	}
	dirs, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]Entry, 0, len(dirs))
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		entryDir := filepath.Join(root, d.Name())
		m, err := readManifest(entryDir)
		if err != nil {
			// Skip malformed/partial entries silently — sweepExpired
			// will tear them down eventually.
			continue
		}
		out = append(out, Entry{Manifest: m, Dir: entryDir})
	}
	slices.SortFunc(out, func(a, b Entry) int {
		if a.DeletedAt.Equal(b.DeletedAt) {
			// ULIDs are monotonic, so the larger id is the later delete —
			// keeps same-second entries in a stable, newest-first order.
			return cmp.Compare(b.ID, a.ID)
		}
		return b.DeletedAt.Compare(a.DeletedAt)
	})
	return out, nil
}

// Find returns the entry whose ID matches. Accepts a unique prefix.
func Find(idOrPrefix string) (Entry, error) {
	if idOrPrefix == "" {
		return Entry{}, errors.New("missing trash id")
	}
	all, err := List()
	if err != nil {
		return Entry{}, err
	}
	var matches []Entry
	for _, e := range all {
		if strings.HasPrefix(e.ID, idOrPrefix) {
			matches = append(matches, e)
		}
	}
	switch len(matches) {
	case 0:
		return Entry{}, fmt.Errorf("no trash entry matching %q", idOrPrefix)
	case 1:
		return matches[0], nil
	default:
		return Entry{}, fmt.Errorf("%q matches %d trash entries — disambiguate", idOrPrefix, len(matches))
	}
}

// Empty removes every entry. Returns the count purged.
func Empty() (int, error) {
	all, err := List()
	if err != nil {
		return 0, err
	}
	n := 0
	for _, e := range all {
		if err := os.RemoveAll(e.Dir); err == nil {
			n++
		}
	}
	return n, nil
}

// SweepExpired drops every entry older than ttl. Errors on individual
// entries are swallowed — sweep is best-effort. Returns the number
// purged.
func SweepExpired(ttl time.Duration) (int, error) {
	if ttl <= 0 {
		return 0, nil
	}
	cutoff := time.Now().UTC().Add(-ttl)
	all, err := List()
	if err != nil {
		return 0, err
	}
	n := 0
	for _, e := range all {
		if e.DeletedAt.Before(cutoff) {
			if err := os.RemoveAll(e.Dir); err == nil {
				n++
			}
		}
	}
	return n, nil
}

func manifestPath(entryDir string) string {
	return filepath.Join(entryDir, "manifest.json")
}

func writeManifest(entryDir string, m Manifest) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(manifestPath(entryDir), b, 0o644)
}

func readManifest(entryDir string) (Manifest, error) {
	b, err := os.ReadFile(manifestPath(entryDir))
	if err != nil {
		return Manifest{}, err
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return Manifest{}, fmt.Errorf("bad trash manifest at %s: %w", entryDir, err)
	}
	return m, nil
}
