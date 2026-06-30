package core

import (
	"github.com/MaikuMori/dfc/internal/index"
	"github.com/MaikuMori/dfc/internal/storage"
	"github.com/MaikuMori/dfc/internal/tag"
)

// tagCacheEntry memoizes a task's parsed inline tags. modified is the task's
// mtime when parsed; a newer mtime invalidates the entry (content only changes
// on save, which bumps the mtime).
type tagCacheEntry struct {
	modified int64
	tags     []string
}

// TaskTags returns a task's effective tag set for filtering: its inline #tags
// unioned with its project's categorical tags, which apply to every task in the
// project. The result is a fresh slice (the cached inline tags are never
// mutated).
func (c *Core) TaskTags(t storage.Task) []string {
	inline := c.inlineTags(t)
	proj := c.reg.Tags(t.ProjectSlug)
	out := make([]string, 0, len(inline)+len(proj))
	out = append(out, inline...)
	out = append(out, proj...)
	return out
}

// InlineTags returns a task's parsed inline #tags (description and details),
// memoized by (ID, mtime). The returned slice is shared and must not be mutated
// by callers. Distinct from TaskTags, which also unions in project tags.
func (c *Core) InlineTags(t storage.Task) []string {
	return c.inlineTags(t)
}

// inlineTags returns a task's parsed inline #tags, memoized by (ID, mtime) so a
// live filter doesn't re-parse every task's markdown on each keystroke. The
// returned slice is shared and must not be mutated by callers.
func (c *Core) inlineTags(t storage.Task) []string {
	key, mod := t.ID, t.Modified.UnixNano()
	c.tagCacheMu.Lock()
	defer c.tagCacheMu.Unlock()
	if e, ok := c.tagCache[key]; ok && e.modified == mod {
		return e.tags
	}
	tags := t.Tags()
	c.tagCache[key] = tagCacheEntry{modified: mod, tags: tags}
	return tags
}

// tagFilters maps tag predicates to index filters. The index mirrors only
// inline #tags, so each predicate pre-resolves the projects whose registry
// tags already satisfy it — their tasks match by slug in SQL, which is why a
// `dfc projects tag` change never requires a reindex.
func (c *Core) tagFilters(include, exclude []string) []index.TagFilter {
	if len(include)+len(exclude) == 0 {
		return nil
	}
	out := make([]index.TagFilter, 0, len(include)+len(exclude))
	for _, name := range include {
		out = append(out, c.tagFilter(name, false))
	}
	for _, name := range exclude {
		out = append(out, c.tagFilter(name, true))
	}
	return out
}

func (c *Core) tagFilter(name string, exclude bool) index.TagFilter {
	f := index.TagFilter{Tag: name, Exclude: exclude}
	for _, slug := range c.reg.Slugs() {
		if tag.Has(c.reg.Tags(slug), name) {
			f.Projects = append(f.Projects, slug)
		}
	}
	return f
}

// taskMatchesTags reports whether a task carries every include tag and none of
// the exclude tags, matched against its effective tags with hierarchy.
func (c *Core) taskMatchesTags(t storage.Task, include, exclude []string) bool {
	tags := c.TaskTags(t)
	for _, inc := range include {
		if !tag.Has(tags, inc) {
			return false
		}
	}
	for _, exc := range exclude {
		if tag.Has(tags, exc) {
			return false
		}
	}
	return true
}
