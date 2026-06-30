// Package savedsearch persists named query strings under ~/.dfc/searches.json.
// A saved search is just a name mapped to a verbatim query (which may carry
// flags like `--tag work`); the search commands and the TUI expand `@name`
// back to that query.
package savedsearch

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/MaikuMori/dfc/internal/fsutil"
)

// envRoot mirrors storage.EnvRoot, duplicated to keep this a leaf package with
// no import back into storage.
const envRoot = "DFC_ROOT"

func dfcRoot() (string, error) {
	if r := os.Getenv(envRoot); r != "" {
		return r, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if home == "" {
		return "", errors.New("cannot resolve home directory")
	}
	return filepath.Join(home, ".dfc"), nil
}

// Path returns the searches.json location under the dfc root, or "" when the
// root cannot be resolved. The TUI uses it to attach its filesystem watcher so
// saved searches written by other processes show up live.
func Path() string {
	root, err := dfcRoot()
	if err != nil {
		return ""
	}
	return filepath.Join(root, "searches.json")
}

// Entry is one saved search: a name and the verbatim query it expands to.
type Entry struct {
	Name  string `json:"name"`
	Query string `json:"query"`
}

// Store maps saved-search names to their query strings, persisted as
// ~/.dfc/searches.json (a single JSON object keyed by name).
type Store struct {
	path    string
	entries map[string]string
}

// Load reads (or creates) searches.json under the dfc root.
func Load() (*Store, error) {
	root, err := dfcRoot()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	return loadFromPath(filepath.Join(root, "searches.json"))
}

func loadFromPath(path string) (*Store, error) {
	s := &Store{path: path, entries: map[string]string{}}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) || len(b) == 0 {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("could not read saved searches at %s: %w", path, err)
	}
	if err := json.Unmarshal(b, &s.entries); err != nil {
		return nil, fmt.Errorf("could not parse saved searches at %s: %w", path, err)
	}
	return s, nil
}

func (s *Store) persist() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s.entries, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return fsutil.WriteFileAtomic(s.path, b, 0o644)
}

// ValidName reports whether name is usable as a saved-search name. Names may be
// free text (spaces allowed); they only have to be non-empty after trimming.
func ValidName(name string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("saved search name cannot be empty")
	}
	return nil
}

// reloadBestEffort re-reads searches.json so a mutation doesn't clobber entries
// another process wrote since this store loaded — the TUI caches its store for
// the whole session while a concurrent `dfc searches save` may have added one.
// Best-effort: a read error leaves the in-memory map untouched. A narrow
// reload→persist race across processes remains, acceptable for a personal tool.
func (s *Store) reloadBestEffort() {
	if fresh, err := loadFromPath(s.path); err == nil {
		s.entries = fresh.entries
	}
}

// Set upserts a saved search and persists the store. Name and query are stored
// trimmed of surrounding whitespace.
func (s *Store) Set(name, query string) error {
	if err := ValidName(name); err != nil {
		return err
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return errors.New("saved search query cannot be empty")
	}
	s.reloadBestEffort()
	s.entries[strings.TrimSpace(name)] = query
	return s.persist()
}

// Remove deletes a saved search, returning whether it existed.
func (s *Store) Remove(name string) (bool, error) {
	name = strings.TrimSpace(name)
	s.reloadBestEffort()
	if _, ok := s.entries[name]; !ok {
		return false, nil
	}
	delete(s.entries, name)
	return true, s.persist()
}

// Get returns the query for name, if present.
func (s *Store) Get(name string) (string, bool) {
	q, ok := s.entries[strings.TrimSpace(name)]
	return q, ok
}

// List returns every saved search, sorted by name.
func (s *Store) List() []Entry {
	out := make([]Entry, 0, len(s.entries))
	for n, q := range s.entries {
		out = append(out, Entry{Name: n, Query: q})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
