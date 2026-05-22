package cli

import (
	"encoding/json"
	"io"
	"time"

	"github.com/MaikuMori/dfc/internal/project"
	"github.com/MaikuMori/dfc/internal/storage"
)

// TaskOut is the JSON shape for any command that surfaces a single task
// (capture, show, done, reopen, edit) or one row of `ls`.
type TaskOut struct {
	ID          string    `json:"id"`
	Project     string    `json:"project"`
	Status      string    `json:"status"`
	Description string    `json:"description"`
	Details     string    `json:"details,omitempty"`
	Path        string    `json:"path"`
	Created     time.Time `json:"created"`
	Modified    time.Time `json:"modified"`
}

func taskOut(t storage.Task) TaskOut {
	return TaskOut{
		ID:          t.ID,
		Project:     t.ProjectSlug,
		Status:      string(t.Status),
		Description: t.Description,
		Details:     t.Details,
		Path:        t.Path,
		Created:     t.Created,
		Modified:    t.Modified,
	}
}

// ProjectOut is the JSON shape for `projects` / `project` rows.
type ProjectOut struct {
	Slug     string    `json:"slug"`
	Name     string    `json:"name"`
	Prefix   string    `json:"prefix"`
	Open     int       `json:"open"`
	Done     int       `json:"done"`
	LastUsed time.Time `json:"last_used,omitempty"`
	Source   string    `json:"source,omitempty"` // git-remote | git-toplevel | cwd (project current only)
	Raw      string    `json:"raw,omitempty"`    // pre-slug canonical form (project current only)
}

func projectOut(reg *project.Registry, slug string, open, done int) ProjectOut {
	return ProjectOut{
		Slug:     slug,
		Name:     reg.Name(slug),
		Prefix:   reg.Prefix(slug),
		Open:     open,
		Done:     done,
		LastUsed: reg.LastUsed(slug),
	}
}

// RmOut is the JSON shape for a successful rm.
type RmOut struct {
	ID      string `json:"id"`
	Project string `json:"project"`
	Path    string `json:"path"`
	TrashID string `json:"trash_id"`
}

// writeJSON emits one JSON document followed by a newline to w. Used for
// single-record outputs (capture, show, done, etc.) and for the per-row
// payloads of NDJSON streams (ls, projects, trash list, search).
func writeJSON(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if _, err := w.Write(b); err != nil {
		return err
	}
	_, err = w.Write([]byte{'\n'})
	return err
}

// statusGlyph is the one-character status indicator shared by every
// row-emitting subcommand (ls, search). Keeping it here is what lets
// `dfc ls` and `dfc s` stay visually consistent without coordination.
func statusGlyph(s storage.Status) string {
	if s == storage.StatusDone {
		return "✓"
	}
	return "○"
}

// shortID returns the 10-character timestamp prefix of a ULID — long
// enough to disambiguate inside a session, short enough to scan a list.
// `trash restore` and `show` both accept any unique prefix, so callers
// can echo this back to the user safely.
func shortID(id string) string {
	if len(id) > 10 {
		return id[:10]
	}
	return id
}

