# Changelog

All notable changes to dfc are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Release notes are managed via [changie](https://changie.dev). Add an entry with `changie new` rather than editing this file by hand; the file is regenerated on release.


## [v0.3.0] - 2026-05-31

### Added

- TUI: y yanks the task ULID and Y yanks the markdown path to the system clipboard.

### Fixed

- TUI and inline capture now treat the numpad Enter key the same as the main Enter key.
- Undo restores a soft-deleted project instead of failing.
- The project registry is saved atomically, so a crash can't truncate projects.json.
- Two captures in the same second with the same description keep separate files instead of one overwriting the other.
- Editing a task's description no longer overwrites another task that already owns the target filename.
- The tag-filter picker no longer re-reads every project from disk on each keystroke.
- Undo restores the most recently deleted item when several were deleted in the same second.
- Deleting a task or project can no longer strand or lose it if interrupted mid-delete.
- Searching in global view with an active tag filter now restricts results to the filtered projects.
- Search result highlighting handles accented and non-ASCII words correctly instead of garbling them.
- A corrupt task file no longer hides the rest of its project, and task edits are saved atomically.
- Deleting a task file outside dfc now drops it from search results instead of leaving a dead hit.
- Searches containing a colon, a bare wildcard, or an uppercase AND/OR/NOT no longer fail with a raw database error.
- Looking up a task by its lowercase ULID now finds it.
- A failed project deletion no longer drops the project's tasks from search.
- In global view, esc clears an active tag filter instead of quitting.
- The tag-edit and tag-filter pickers now size to the terminal width.
- Project prefixes with emoji or wide characters align correctly and no longer truncate mid-character in global view.
- `--version` works after a subcommand (e.g. `dfc search --version`), not just as the first argument.
- Restoring an item from trash always clears its entry, even if directory cleanup fails.
- Search reflects an external edit made in the same second as the most recent indexed change.
- Restoring a deleted project from trash restores its custom prefix and tags, not just its name.
## [v0.2.0] - 2026-05-22

### Added

- Tab completion: project slugs and display names after -p/--project, task ULIDs for done/reopen/edit/rm/show, and trash entry IDs for trash restore. zsh and fish menus show the task description next to each ULID.
- --all-projects / -a flag on done, reopen, edit, rm, and show to look up a ULID across every project.
- -p/--project accepts a project's display name (case-insensitive) in addition to its slug.
- TUI: 's' cycles the list sort between modified (file mtime) and created (frontmatter timestamp). Session-only; resets to modified on next launch. 'dfc ls' and 'dfc s/ss' grow a '--sort modified|created' flag (search also keeps the existing 'score' default).
- 'dfc tags' manages project tags. Subcommands: show / add / rm / set / ls. Default target is the cwd-resolved project; '--project/-p' overrides. Tag args accept comma-separated or repeated values.
- '--tag' filter on 'dfc ls -a', 'dfc s', 'dfc ss', 'dfc projects'. Repeatable or comma-separated, OR semantics; '(untagged)' matches projects with no tags.
- TUI: 'f' opens a multi-select tag filter in global view (header count updates live, selection is session-only). 'P → ctrl+t' opens a multi-select tag editor; 'ctrl+n' adds a new tag, 'ctrl+l' clears. Returns to the project switcher on save/cancel.

### Changed

- done, reopen, edit, rm, and show default to looking up the ULID in the cwd-resolved project. Off-project IDs error with a hint to pass --all-projects.
- Project display names and the per-project 'prefix' label are unique. Renames or prefix changes that would collide are rejected; auto-derived names fall back to the slug on collision.
- Per-project display label is named 'prefix' (renamed from 'tag'); JSON key 'prefix', TUI editor 'ctrl+p'.

### Fixed

- Shell completion now follows subcommand aliases (c, s, list), so 'dfc c -p <TAB>' suggests projects the same way 'dfc capture -p <TAB>' does.
- Marking a task done (or open) in the TUI no longer yanks the cursor to follow the task. The cursor holds its on-screen position so rapid space-toggling drains the next task under it.
- Agent skill (skill/SKILL.md): corrected the 'dfc edit' flag docs — '-d' is the short form of '--details' (not '--description'). Added explicit guidance steering agents toward direct markdown file edits for partial changes; 'dfc edit --details' replaces the whole body.
- 'dfc cc' picker and the inline prompt (used by 'dfc c' with no description) now resize to fit the terminal. They previously sat at 40 columns regardless of window width.
- 'changie next auto' caps bumps at minor while the project is pre-1.0; Changed and Removed entries no longer promote the version past v0.x.
- TUI: closing a picker no longer leaves the task list with a single row stranded at the top; the viewport offset is clamped to content.
- TUI: the picker textinput placeholder reflects the open sub-mode ('name', 'prefix', 'new tag') and resets to 'filter' on exit.
## [v0.1.0] - 2026-05-20

### Added

- Initial version