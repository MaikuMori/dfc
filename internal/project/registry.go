package project

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// envRoot mirrors storage.EnvRoot. Duplicated to avoid an import cycle
// (storage already imports this package for Slugify).
const envRoot = "DFC_ROOT"

// dfcRoot returns the base dfc directory. Mirrors storage.Root semantics so
// the registry and the task store land in the same tree.
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

// Entry is a registry record for one project slug. The schema is intentionally
// an object so we can grow fields (alias, color, etc.) without breaking
// readers.
type Entry struct {
	Name     string    `json:"name"`
	Tag      string    `json:"tag,omitempty"`
	LastUsed time.Time `json:"last_used,omitempty"`
}

// Registry maps project slugs to their metadata. The on-disk format is a
// single JSON object keyed by slug.
type Registry struct {
	path    string
	entries map[string]Entry
}

// LoadRegistry reads (or creates) the projects.json under the dfc root.
func LoadRegistry() (*Registry, error) {
	root, err := dfcRoot()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	return loadRegistryFromPath(filepath.Join(root, "projects.json"))
}

func loadRegistryFromPath(path string) (*Registry, error) {
	reg := &Registry{path: path, entries: map[string]Entry{}}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return reg, nil
	}
	if err != nil {
		return nil, fmt.Errorf("could not read project registry at %s: %w", path, err)
	}
	if len(b) == 0 {
		return reg, nil
	}
	if err := json.Unmarshal(b, &reg.entries); err != nil {
		return nil, fmt.Errorf("could not parse project registry at %s: %w", path, err)
	}
	return reg, nil
}

// Save persists the registry to disk.
func (r *Registry) Save() error {
	if r.path == "" {
		return errors.New("project registry has no on-disk path")
	}
	if err := os.MkdirAll(filepath.Dir(r.path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(r.entries, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(r.path, b, 0o644)
}

// Name returns the friendly name for slug, falling back to the slug itself
// when no entry exists.
func (r *Registry) Name(slug string) string {
	if e, ok := r.entries[slug]; ok && e.Name != "" {
		return e.Name
	}
	return slug
}

// Has reports whether the registry already knows about this slug.
func (r *Registry) Has(slug string) bool {
	_, ok := r.entries[slug]
	return ok
}

// Register adds (or refreshes) an entry. Returns true if it was newly added.
// Existing LastUsed is preserved.
func (r *Registry) Register(slug, name string) bool {
	existing, existed := r.entries[slug]
	r.entries[slug] = Entry{Name: name, LastUsed: existing.LastUsed}
	return !existed
}

// Touch sets LastUsed = now for slug (registering it with name=slug if it
// wasn't present) and persists the registry to disk.
func (r *Registry) Touch(slug string) error {
	e := r.entries[slug]
	if e.Name == "" {
		e.Name = slug
	}
	e.LastUsed = time.Now().UTC().Truncate(time.Second)
	r.entries[slug] = e
	return r.Save()
}

// LastUsed returns the timestamp for slug, or the zero value if unknown.
func (r *Registry) LastUsed(slug string) time.Time {
	return r.entries[slug].LastUsed
}

// Rename updates the display name for slug, preserving LastUsed, and writes
// the registry to disk.
func (r *Registry) Rename(slug, name string) error {
	if !r.Has(slug) {
		return fmt.Errorf("unknown project slug %q", slug)
	}
	e := r.entries[slug]
	e.Name = name
	r.entries[slug] = e
	return r.Save()
}

// Tag returns the short prefix used in global-view rows. Falls back to the
// last `-`-separated segment of the slug when no explicit tag is set, so a
// fresh registry entry still displays something useful (e.g. slug
// `users-maiku-projects-dfc` → tag `dfc`).
func (r *Registry) Tag(slug string) string {
	if e, ok := r.entries[slug]; ok && e.Tag != "" {
		return e.Tag
	}
	return DefaultTag(slug)
}

// Unregister removes the entry for slug and writes the registry to disk.
// No-op when the slug isn't known. Project task files on disk are not
// touched here — callers wanting to wipe a project entirely orchestrate
// disk + index + registry together (see core.RemoveProject).
func (r *Registry) Unregister(slug string) error {
	if !r.Has(slug) {
		return nil
	}
	delete(r.entries, slug)
	return r.Save()
}

// SetTag writes a custom tag for slug, preserving Name and LastUsed.
// Passing "" reverts to the derived default.
func (r *Registry) SetTag(slug, tag string) error {
	if !r.Has(slug) {
		return fmt.Errorf("unknown project slug %q", slug)
	}
	e := r.entries[slug]
	e.Tag = tag
	r.entries[slug] = e
	return r.Save()
}

// DefaultTag returns the last `-`-separated segment of slug, or the slug
// itself when it has no hyphens.
func DefaultTag(slug string) string {
	if slug == "" {
		return ""
	}
	if i := strings.LastIndexByte(slug, '-'); i >= 0 {
		return slug[i+1:]
	}
	return slug
}

// Slugs returns all known slugs in alphabetical order.
func (r *Registry) Slugs() []string {
	out := make([]string, 0, len(r.entries))
	for s := range r.entries {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

