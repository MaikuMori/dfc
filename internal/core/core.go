// Package core is the transactional facade over storage, index, and
// project registry. CLI commands, the TUI, and the future C-shared
// library all funnel mutations through Core so write-through and any
// other cross-cutting concerns live in one place rather than being
// reimplemented by every client.
package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/MaikuMori/dfc/internal/capture"
	"github.com/MaikuMori/dfc/internal/index"
	"github.com/MaikuMori/dfc/internal/project"
	"github.com/MaikuMori/dfc/internal/storage"
	"github.com/MaikuMori/dfc/internal/trash"
)

// Core owns the long-lived handles a dfc client needs: the project
// registry, the search index, and a cache of per-project stores.
type Core struct {
	reg      *project.Registry
	idx      *index.Index // nil when Open failed
	indexErr error

	mu     sync.Mutex
	stores map[string]*storage.Store

	warn func(op string, err error)
}

// Options controls how Open constructs a Core.
type Options struct {
	// Warn is called for non-fatal background errors (currently:
	// index upsert/delete drift). nil installs a stderr default.
	Warn func(op string, err error)
}

// Open loads the registry and opens the search index. Index open
// failures are non-fatal: mutations still succeed; Search returns an
// error until the index recovers. Callers should defer Close.
func Open(opts Options) (*Core, error) {
	reg, err := project.LoadRegistry()
	if err != nil {
		return nil, err
	}
	c := &Core{
		reg:    reg,
		stores: map[string]*storage.Store{},
		warn:   opts.Warn,
	}
	if c.warn == nil {
		c.warn = defaultWarn
	}
	// Trash sweep is best-effort. Misconfigured TTL or missing dir does
	// not gate Core.Open; the worst case is older items linger.
	if _, err := trash.SweepExpired(trash.TTLFromEnv()); err != nil {
		c.warn("trash sweep", err)
	}
	dbPath, err := index.DefaultPath()
	if err != nil {
		c.indexErr = err
		c.warn("index path", err)
		return c, nil
	}
	idx, err := index.Open(dbPath)
	if err != nil {
		c.indexErr = err
		c.warn("index open", err)
		return c, nil
	}
	c.idx = idx
	return c, nil
}

func defaultWarn(op string, err error) {
	fmt.Fprintf(os.Stderr, "%s: %v\n", op, err)
}

// Close releases the index handle.
func (c *Core) Close() error {
	if c.idx == nil {
		return nil
	}
	return c.idx.Close()
}

// Registry returns the underlying project registry for callers that
// need to read or mutate metadata directly (rename, tag).
func (c *Core) Registry() *project.Registry { return c.reg }

// HasIndex reports whether the search index opened successfully. The
// TUI uses this to pick between FTS5-backed search and the in-memory
// fallback before issuing a query.
func (c *Core) HasIndex() bool { return c.idx != nil }

// EnsureFresh runs the index's drift sync. Best-effort: errors are
// surfaced via the warn hook, never returned.
func (c *Core) EnsureFresh() {
	if c.idx == nil {
		return
	}
	if err := c.idx.EnsureFresh(); err != nil {
		c.warn("index sync", err)
	}
}

// StoreFor returns the cached *storage.Store for slug, opening on
// demand. Exposed for callers that need filesystem coordinates
// (e.g. the TUI's watcher attach uses Store.Dir()).
func (c *Core) StoreFor(slug string) (*storage.Store, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if s, ok := c.stores[slug]; ok {
		return s, nil
	}
	s, err := storage.Open(slug)
	if err != nil {
		return nil, err
	}
	c.stores[slug] = s
	return s, nil
}

// ReloadRegistry re-reads the registry from disk. The TUI calls this
// after an out-of-process invocation changes the file underneath it.
func (c *Core) ReloadRegistry() error {
	reg, err := project.LoadRegistry()
	if err != nil {
		return err
	}
	c.reg = reg
	return nil
}

// EnsureProject registers slug with displayName when the registry
// doesn't yet know about it. Empty displayName falls back to slug.
func (c *Core) EnsureProject(slug, displayName string) error {
	if c.reg.Has(slug) {
		return nil
	}
	name := displayName
	if name == "" {
		name = slug
	}
	c.reg.Register(slug, name)
	return c.reg.Save()
}

// CaptureInput is the parameter object for Core.Capture.
type CaptureInput struct {
	Slug        string
	Description string
	Details     string
	DisplayName string // used to register a previously unknown slug
}

// CaptureResult is the outcome of Capture.
type CaptureResult struct {
	Task       storage.Task
	NewProject bool // true when the slug had no on-disk directory before this call
}

// Capture writes a new task, mirrors it into the index, and touches
// the registry. The project is auto-registered when unknown.
func (c *Core) Capture(in CaptureInput) (CaptureResult, error) {
	if in.Slug == "" {
		return CaptureResult{}, errors.New("missing project slug")
	}
	isNew := !storage.ProjectDirExists(in.Slug)
	if err := c.EnsureProject(in.Slug, in.DisplayName); err != nil {
		return CaptureResult{}, err
	}
	store, err := c.StoreFor(in.Slug)
	if err != nil {
		return CaptureResult{}, err
	}
	t, filenameSlug, err := capture.New(in.Description, capture.Options{})
	if err != nil {
		return CaptureResult{}, err
	}
	t.Details = in.Details
	saved, err := store.Create(t, filenameSlug)
	if err != nil {
		return CaptureResult{}, err
	}
	c.upsertIndex(saved)
	if err := c.reg.Touch(in.Slug); err != nil {
		return CaptureResult{}, err
	}
	return CaptureResult{Task: saved, NewProject: isNew}, nil
}

// SetStatus flips a task's status by ULID, scanning every project for
// the ID. Returns the post-update task and changed=false when the task
// was already in the requested state.
func (c *Core) SetStatus(id string, status storage.Status) (storage.Task, bool, error) {
	return c.setStatus(id, "", status)
}

// SetStatusInProject is SetStatus restricted to a single project slug.
// Returns NotFoundError when the ID isn't in that project, even if it
// exists elsewhere.
func (c *Core) SetStatusInProject(id, slug string, status storage.Status) (storage.Task, bool, error) {
	return c.setStatus(id, slug, status)
}

func (c *Core) setStatus(id, scopeSlug string, status storage.Status) (storage.Task, bool, error) {
	if err := validateID(id); err != nil {
		return storage.Task{}, false, err
	}
	slug, path, err := c.findByIDScoped(id, scopeSlug)
	if err != nil {
		return storage.Task{}, false, err
	}
	if path == "" {
		return storage.Task{}, false, NotFound(id)
	}
	store, err := c.StoreFor(slug)
	if err != nil {
		return storage.Task{}, false, err
	}
	loaded, err := store.Load(path)
	if err != nil {
		return storage.Task{}, false, err
	}
	if loaded.Status == status {
		return loaded, false, nil
	}
	loaded.Status = status
	if err := store.Save(&loaded); err != nil {
		return storage.Task{}, false, err
	}
	c.upsertIndex(loaded)
	return loaded, true, nil
}

// EditInput updates the fields whose pointers are non-nil.
type EditInput struct {
	ID          string
	Description *string
	Details     *string
}

// Edit applies the requested changes, scanning every project for the ID.
func (c *Core) Edit(in EditInput) (storage.Task, error) {
	return c.edit(in, "")
}

// EditInProject restricts the lookup to a single slug. Returns
// NotFoundError when the ID isn't in that project.
func (c *Core) EditInProject(in EditInput, slug string) (storage.Task, error) {
	return c.edit(in, slug)
}

func (c *Core) edit(in EditInput, scopeSlug string) (storage.Task, error) {
	if err := validateID(in.ID); err != nil {
		return storage.Task{}, err
	}
	if in.Description == nil && in.Details == nil {
		return storage.Task{}, errors.New("nothing to change")
	}
	slug, path, err := c.findByIDScoped(in.ID, scopeSlug)
	if err != nil {
		return storage.Task{}, err
	}
	if path == "" {
		return storage.Task{}, NotFound(in.ID)
	}
	store, err := c.StoreFor(slug)
	if err != nil {
		return storage.Task{}, err
	}
	t, err := store.Load(path)
	if err != nil {
		return storage.Task{}, err
	}
	if in.Details != nil {
		t.Details = *in.Details
	}
	if in.Description != nil {
		if err := store.RenameForDescription(&t, *in.Description); err != nil {
			return storage.Task{}, err
		}
	} else {
		if err := store.Save(&t); err != nil {
			return storage.Task{}, err
		}
	}
	c.upsertIndex(t)
	return t, nil
}

// Remove soft-deletes a task by ULID, scanning every project for the
// ID. Moves the file into the trash (recoverable via Undo / Restore
// until the TTL sweep gets it) and drops the index row. Returns the
// slug, path, and trash entry id so callers can echo a "deleted X ·
// u to undo" hint.
func (c *Core) Remove(id string) (slug, path, trashID string, err error) {
	return c.remove(id, "")
}

// RemoveInProject restricts Remove to a single project slug.
func (c *Core) RemoveInProject(id, slug string) (string, string, string, error) {
	return c.remove(id, slug)
}

func (c *Core) remove(id, scopeSlug string) (slug, path, trashID string, err error) {
	if err := validateID(id); err != nil {
		return "", "", "", err
	}
	slug, path, err = c.findByIDScoped(id, scopeSlug)
	if err != nil {
		return "", "", "", err
	}
	if path == "" {
		return "", "", "", NotFound(id)
	}
	t, err := c.loadByPath(slug, path)
	if err != nil {
		return "", "", "", err
	}
	m, err := trash.TrashTask(slug, t.ID, t.Description, path)
	if err != nil {
		return "", "", "", err
	}
	c.deleteIndex(id)
	return slug, path, m.ID, nil
}

// RemoveProject soft-deletes a project: moves its directory into the
// trash, drops every task's index row, evicts the cached store, and
// unregisters. Returns task count moved plus the trash entry id.
func (c *Core) RemoveProject(slug string) (removed int, trashID string, err error) {
	if slug == "" {
		return 0, "", errors.New("missing project slug")
	}
	if storage.ProjectDirExists(slug) {
		store, err := c.StoreFor(slug)
		if err != nil {
			return 0, "", err
		}
		tasks, err := store.List()
		if err != nil {
			return 0, "", err
		}
		m, err := trash.TrashProject(slug, c.reg.Name(slug), store.Dir())
		if err != nil {
			return 0, "", err
		}
		for _, t := range tasks {
			c.deleteIndex(t.ID)
			removed++
		}
		trashID = m.ID
		c.mu.Lock()
		delete(c.stores, slug)
		c.mu.Unlock()
	}
	if err := c.reg.Unregister(slug); err != nil {
		return removed, trashID, err
	}
	return removed, trashID, nil
}

// RemoveTask is the TUI's mutation path: caller already holds the Task
// and just wants the side effects (trash move + index drop). Returns the
// trash entry id.
func (c *Core) RemoveTask(t storage.Task) (string, error) {
	if t.Path == "" {
		return "", errors.New("task has no file path")
	}
	m, err := trash.TrashTask(t.ProjectSlug, t.ID, t.Description, t.Path)
	if err != nil {
		return "", err
	}
	c.deleteIndex(t.ID)
	return m.ID, nil
}

// loadByPath is a thin helper for Remove — we know the slug already so
// we skip the FindByID scan and load directly via the cached store.
func (c *Core) loadByPath(slug, path string) (storage.Task, error) {
	store, err := c.StoreFor(slug)
	if err != nil {
		return storage.Task{}, err
	}
	return store.Load(path)
}

// Undo restores the most recently trashed entry — task or project. The
// returned Manifest tells the caller what came back. Errors when trash
// is empty or when restoration conflicts with current on-disk state
// (e.g. project slug re-created since the delete).
func (c *Core) Undo() (trash.Manifest, error) {
	entries, err := trash.List()
	if err != nil {
		return trash.Manifest{}, err
	}
	if len(entries) == 0 {
		return trash.Manifest{}, errors.New("trash is empty")
	}
	return c.restore(entries[0])
}

// Restore brings a specific trash entry back. trashIDOrPrefix can be a
// full ULID or any unique prefix.
func (c *Core) Restore(trashIDOrPrefix string) (trash.Manifest, error) {
	e, err := trash.Find(trashIDOrPrefix)
	if err != nil {
		return trash.Manifest{}, err
	}
	return c.restore(e)
}

func (c *Core) restore(e trash.Entry) (trash.Manifest, error) {
	switch e.Kind {
	case trash.KindTask:
		// Project must exist for the task to land somewhere meaningful.
		// We auto-register against the slug rather than erroring — the
		// user already expressed intent to keep this task.
		if err := c.EnsureProject(e.Slug, e.Slug); err != nil {
			return trash.Manifest{}, err
		}
		store, err := c.StoreFor(e.Slug)
		if err != nil {
			return trash.Manifest{}, err
		}
		path, err := trash.RestoreTask(e, store.Dir())
		if err != nil {
			return trash.Manifest{}, err
		}
		t, err := store.Load(path)
		if err != nil {
			return trash.Manifest{}, err
		}
		c.upsertIndex(t)
		return e.Manifest, nil

	case trash.KindProject:
		dest, err := storage.ProjectDirPath(e.Slug)
		if err != nil {
			return trash.Manifest{}, err
		}
		if err := trash.RestoreProject(e, dest); err != nil {
			return trash.Manifest{}, err
		}
		if e.Name != "" {
			c.reg.Register(e.Slug, e.Name)
		} else {
			c.reg.Register(e.Slug, e.Slug)
		}
		if err := c.reg.Save(); err != nil {
			return trash.Manifest{}, err
		}
		// Re-index every task we just restored.
		store, err := c.StoreFor(e.Slug)
		if err != nil {
			return trash.Manifest{}, err
		}
		tasks, err := store.List()
		if err != nil {
			return e.Manifest, err
		}
		for _, t := range tasks {
			c.upsertIndex(t)
		}
		return e.Manifest, nil
	}
	return trash.Manifest{}, fmt.Errorf("unknown trash entry kind %q", e.Kind)
}

// TrashList returns the current trash entries, newest first.
func (c *Core) TrashList() ([]trash.Entry, error) {
	return trash.List()
}

// TrashEmpty wipes every trashed entry. Returns the count purged.
func (c *Core) TrashEmpty() (int, error) {
	return trash.Empty()
}

// SaveTask is the TUI's mutation path for status toggles: caller
// already loaded and mutated the Task; Core persists it and mirrors
// to the index.
func (c *Core) SaveTask(t *storage.Task) error {
	if t == nil {
		return errors.New("nil task")
	}
	store, err := c.StoreFor(t.ProjectSlug)
	if err != nil {
		return err
	}
	if err := store.Save(t); err != nil {
		return err
	}
	c.upsertIndex(*t)
	return nil
}

// RenameTask is the TUI's edit path: caller hands a Task and a new
// description; Core renames the file and mirrors the index.
func (c *Core) RenameTask(t *storage.Task, newDesc string) error {
	if t == nil {
		return errors.New("nil task")
	}
	store, err := c.StoreFor(t.ProjectSlug)
	if err != nil {
		return err
	}
	if err := store.RenameForDescription(t, newDesc); err != nil {
		return err
	}
	c.upsertIndex(*t)
	return nil
}

// Show returns a task by full ULID, scanning every project.
func (c *Core) Show(id string) (storage.Task, error) {
	return c.show(id, "")
}

// ShowInProject restricts Show to a single project slug.
func (c *Core) ShowInProject(id, slug string) (storage.Task, error) {
	return c.show(id, slug)
}

func (c *Core) show(id, scopeSlug string) (storage.Task, error) {
	if err := validateID(id); err != nil {
		return storage.Task{}, err
	}
	slug, path, err := c.findByIDScoped(id, scopeSlug)
	if err != nil {
		return storage.Task{}, err
	}
	if path == "" {
		return storage.Task{}, NotFound(id)
	}
	store, err := c.StoreFor(slug)
	if err != nil {
		return storage.Task{}, err
	}
	return store.Load(path)
}

// FindByIDInProject looks up a task by ULID in a single project slug.
// Returns ("", nil) when the slug has no on-disk directory or the ID
// isn't present in it.
func (c *Core) FindByIDInProject(id, slug string) (path string, err error) {
	if !storage.ProjectDirExists(slug) {
		return "", nil
	}
	store, err := c.StoreFor(slug)
	if err != nil {
		return "", err
	}
	return store.FindByID(id)
}

// findByIDScoped routes between all-projects and single-slug lookup. An
// empty scopeSlug means "scan every project" (FindByID); a non-empty
// scopeSlug restricts to that one project.
func (c *Core) findByIDScoped(id, scopeSlug string) (slug, path string, err error) {
	if scopeSlug == "" {
		return c.FindByID(id)
	}
	path, err = c.FindByIDInProject(id, scopeSlug)
	if err != nil {
		return "", "", err
	}
	return scopeSlug, path, nil
}

// FindByID walks every project under the dfc root and returns the
// slug + path of the task whose frontmatter id matches. Returns
// ("","",nil) when no match exists.
func (c *Core) FindByID(id string) (slug, path string, err error) {
	root, err := storage.Root()
	if err != nil {
		return "", "", err
	}
	projectsDir := filepath.Join(root, "projects")
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", "", nil
		}
		return "", "", err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		store, err := c.StoreFor(e.Name())
		if err != nil {
			return "", "", err
		}
		found, err := store.FindByID(id)
		if err != nil {
			return "", "", err
		}
		if found != "" {
			return e.Name(), found, nil
		}
	}
	return "", "", nil
}

// List returns tasks for one slug, oldest-modified first (matches
// storage.Store.List).
func (c *Core) List(slug string) ([]storage.Task, error) {
	if !storage.ProjectDirExists(slug) {
		return nil, nil
	}
	store, err := c.StoreFor(slug)
	if err != nil {
		return nil, err
	}
	return store.List()
}

// ListAll merges tasks from every registered project. Per-project
// errors are non-fatal; the first one is returned alongside whatever
// rows did load.
func (c *Core) ListAll() ([]storage.Task, error) {
	var all []storage.Task
	var firstErr error
	for _, slug := range c.reg.Slugs() {
		if !storage.ProjectDirExists(slug) {
			continue
		}
		store, err := c.StoreFor(slug)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		ts, err := store.List()
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		all = append(all, ts...)
	}
	return all, firstErr
}

// CountsByProject returns the open/done tally for every project that has
// rows in the index. Cheap — one grouped query against tasks_meta. Empty
// (not nil-erroring) when the index is unavailable so callers don't have
// to special-case missing data.
func (c *Core) CountsByProject() map[string]index.ProjectCounts {
	if c.idx == nil {
		return map[string]index.ProjectCounts{}
	}
	out, err := c.idx.CountsByProject()
	if err != nil {
		c.warn("index counts", err)
		return map[string]index.ProjectCounts{}
	}
	return out
}

// Search delegates to the index. Returns an error when the index
// isn't available (TUI callers should fall back to an in-memory
// filter; CLI callers surface the error).
func (c *Core) Search(q string, opts index.SearchOpts) ([]index.SearchHit, error) {
	if c.idx == nil {
		if c.indexErr != nil {
			return nil, fmt.Errorf("search index unavailable: %w", c.indexErr)
		}
		return nil, errors.New("search index unavailable")
	}
	return c.idx.Search(q, opts)
}

// Reindex drops and rebuilds the index from disk.
func (c *Core) Reindex() error {
	if c.idx == nil {
		if c.indexErr != nil {
			return fmt.Errorf("search index unavailable: %w", c.indexErr)
		}
		return errors.New("search index unavailable")
	}
	return c.idx.Reindex()
}

// DiscoverProjects walks the projects directory and registers any
// slug not yet known to the registry. Used by `dfc cc` so the picker
// shows projects with files on disk even if they were never auto-
// registered.
func (c *Core) DiscoverProjects() error {
	root, err := storage.Root()
	if err != nil {
		return err
	}
	projectsDir := filepath.Join(root, "projects")
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	changed := false
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if c.reg.Has(e.Name()) {
			continue
		}
		c.reg.Register(e.Name(), e.Name())
		changed = true
	}
	if changed {
		return c.reg.Save()
	}
	return nil
}

func (c *Core) upsertIndex(t storage.Task) {
	if c.idx == nil {
		return
	}
	if err := c.idx.Upsert(t); err != nil {
		c.warn("index upsert", err)
	}
}

func (c *Core) deleteIndex(id string) {
	if c.idx == nil {
		return
	}
	if err := c.idx.Delete(id); err != nil {
		c.warn("index delete", err)
	}
}

// NotFoundError is returned by id-based methods when no task matches.
type NotFoundError struct{ ID string }

func (e *NotFoundError) Error() string { return fmt.Sprintf("no task with id %s", e.ID) }

// NotFound builds a NotFoundError for id.
func NotFound(id string) error { return &NotFoundError{ID: id} }

// validateID rejects malformed task IDs early so callers don't need to
// re-implement the length check at every entry point.
func validateID(id string) error {
	if len(id) != storage.ULIDLen {
		return fmt.Errorf("task id must be a %d-character ULID (got %d)", storage.ULIDLen, len(id))
	}
	return nil
}
