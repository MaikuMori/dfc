package storage

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Store is a per-project task store backed by a directory of .md files.
type Store struct {
	slug string
	dir  string
}

// Open returns a store for the given project slug, ensuring the directory
// exists.
func Open(projectSlug string) (*Store, error) {
	dir, err := ProjectDir(projectSlug)
	if err != nil {
		return nil, err
	}
	return &Store{slug: projectSlug, dir: dir}, nil
}

// Dir returns the absolute project directory backing this store.
func (s *Store) Dir() string { return s.dir }

// tagOwn stamps t with the store's slug so callers can route per-task
// operations back to the right Store without re-parsing the path.
func (s *Store) tagOwn(t *Task) { t.ProjectSlug = s.slug }

// pathFor returns the on-disk path for the given filename slug (no extension).
func (s *Store) pathFor(filenameSlug string) string {
	return filepath.Join(s.dir, filenameSlug+".md")
}

// Create writes a new task file. It fails if filenameSlug is empty.
func (s *Store) Create(t Task, filenameSlug string) (Task, error) {
	if filenameSlug == "" {
		return t, errors.New("missing filename slug")
	}
	b, err := Marshal(t)
	if err != nil {
		return t, err
	}
	path, err := s.createUnique(filenameSlug, t.ID, b)
	if err != nil {
		return t, err
	}
	t.Path = path
	if err := stampMtime(&t); err != nil {
		return t, err
	}
	s.tagOwn(&t)
	return t, nil
}

// createUnique writes b to a fresh file for filenameSlug, never overwriting an
// existing task. A filename's leading 10 characters are only the ULID's
// timestamp, and capture truncates that timestamp to the second, so two
// captures in one project in the same second with the same description would
// otherwise collide and the second would clobber the first. On collision it
// retries with a disambiguator drawn from the ULID's random suffix, which
// keeps the file matchable by FindByID's "<timestamp>-*.md" glob.
func (s *Store) createUnique(filenameSlug, id string, b []byte) (string, error) {
	for _, slug := range candidateSlugs(filenameSlug, id) {
		path := s.pathFor(slug)
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("could not write %s: %w", path, err)
		}
		if _, err := f.Write(b); err != nil {
			_ = f.Close()
			return "", fmt.Errorf("could not write %s: %w", path, err)
		}
		if err := f.Close(); err != nil {
			return "", fmt.Errorf("could not write %s: %w", path, err)
		}
		return path, nil
	}
	return "", fmt.Errorf("could not find a free filename for %s", filenameSlug)
}

// candidateSlugs returns the filename stems to try for a new task: the plain
// slug first, then the slug with progressively longer suffixes from the ULID's
// random portion. The last candidate carries the full random suffix, which is
// unique to the task and therefore always free.
func candidateSlugs(base, id string) []string {
	out := []string{base}
	if len(id) <= 10 {
		return out
	}
	tail := strings.ToLower(id[10:])
	for n := 4; n < len(tail); n += 4 {
		out = append(out, base+"-"+tail[:n])
	}
	return append(out, base+"-"+tail)
}

// Save overwrites the task file at t.Path and refreshes t.Modified from the
// filesystem so callers can re-sort without re-reading the directory.
func (s *Store) Save(t *Task) error {
	if t.Path == "" {
		return errors.New("task has no file path")
	}
	b, err := Marshal(*t)
	if err != nil {
		return err
	}
	if err := os.WriteFile(t.Path, b, 0o644); err != nil {
		return err
	}
	if err := stampMtime(t); err != nil {
		return err
	}
	s.tagOwn(t)
	return nil
}

// Load reads and parses the task at the given path.
func (s *Store) Load(path string) (Task, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Task{}, err
	}
	t, err := Unmarshal(b)
	if err != nil {
		return Task{}, fmt.Errorf("could not parse %s: %w", path, err)
	}
	t.Path = path
	if err := stampMtime(&t); err != nil {
		return Task{}, err
	}
	s.tagOwn(&t)
	return t, nil
}

// List returns all tasks in the store sorted by filesystem modification time
// ascending (oldest mtime first). The UI is responsible for any zoning on
// top of this.
func (s *Store) List() ([]Task, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	tasks := make([]Task, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".md" {
			continue
		}
		t, err := s.Load(filepath.Join(s.dir, e.Name()))
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].Modified.Before(tasks[j].Modified)
	})
	return tasks, nil
}

// stampMtime stats t.Path and updates t.Modified.
func stampMtime(t *Task) error {
	info, err := os.Stat(t.Path)
	if err != nil {
		return fmt.Errorf("could not stat %s: %w", t.Path, err)
	}
	t.Modified = info.ModTime()
	return nil
}

// Delete hard-removes the task file at path. Production code goes through
// trash.TrashTask for soft delete; this stays as the low-level primitive
// for tests and any future "wipe without recovery" path.
func (s *Store) Delete(path string) error { return os.Remove(path) }

// FindByID looks up a task in this store by full ULID. It narrows the search
// using the ULID's 10-character timestamp prefix (which is the leading part
// of the filename), then parses each candidate's frontmatter to confirm.
// Returns "", nil if no match exists.
func (s *Store) FindByID(id string) (string, error) {
	if len(id) < 10 {
		return "", fmt.Errorf("task id too short: %q", id)
	}
	pattern := filepath.Join(s.dir, strings.ToLower(id[:10])+"-*.md")
	candidates, err := filepath.Glob(pattern)
	if err != nil {
		return "", err
	}
	for _, p := range candidates {
		t, err := s.Load(p)
		if err != nil {
			continue
		}
		if t.ID == id {
			return p, nil
		}
	}
	return "", nil
}

// RenameForDescription renames the file backing t to match newDesc, keeping
// the existing timestamp prefix. It writes the task body (so on-disk content
// matches the supplied Task) and updates t.Path in place. A no-op rename
// (slug unchanged) is allowed and still triggers a Save so body edits land.
func (s *Store) RenameForDescription(t *Task, newDesc string) error {
	if t.Path == "" {
		return errors.New("task has no file path")
	}
	stem := filepath.Base(t.Path)
	stem = stem[:len(stem)-len(filepath.Ext(stem))]
	ts := FilenameTimestamp(stem)
	if ts == "" {
		return fmt.Errorf("cannot parse timestamp from filename %q", t.Path)
	}

	t.Description = newDesc
	newPath := filepath.Join(s.dir, FilenameSlug(ts, newDesc)+".md")
	if newPath != t.Path {
		if err := renameNoReplace(t.Path, newPath); err != nil {
			if errors.Is(err, fs.ErrExist) {
				return fmt.Errorf("a task file already exists at %s", filepath.Base(newPath))
			}
			return fmt.Errorf("could not rename %s to %s: %w", t.Path, newPath, err)
		}
		t.Path = newPath
	}
	return s.Save(t)
}
