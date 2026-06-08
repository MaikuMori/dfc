---
name: dfc
description: Quick-capture task CLI for projects. Use whenever the user mentions a task or todo — adding, listing, searching, completing, or removing.
---

# dfc

Quick-capture task CLI. Project scope is auto-derived from the current working directory (git remote → git toplevel → cwd path) and can always be overridden with `-p <slug>` or `-a` (all projects).

Use it when the user asks to:
- jot down a task ("remind me to ...", "add a todo", "track that I need to ...")
- list, search, or filter tasks
- mark something done / reopen / edit / delete / move between projects
- inspect, rename, set the prefix of, or merge a project
- rename a tag across every project

## Output modes

Every command except the interactive TUI (`dfc`) emits human text by default and structured JSON with `--json`. **Always pass `--json` when parsing programmatically** — the human format may change without notice.

## Subtask checklists

When a task breaks into trackable steps, write them in the task file's **details** as a GFM task list — `- [ ]` for a step still to do, `- [x]` for a done one. **Prefer this over prose whenever a task is really a checklist of steps.** dfc counts the boxes and shows a `[done/total]` badge after the title (in the TUI list and `dfc ls`) and dims completed items in the expanded view, so the human sees progress at a glance.

```
printf 'Ship the migration\n\n- [ ] write migration\n- [ ] test on staging\n- [ ] backfill\n' | dfc c -
```

**Keep the checklist current as you work.** The task file is the source of truth — as you finish each subtask, mark it `- [x]` in the file so the badge advances; don't batch the updates to the end. Edit the file **in place** rather than rewriting the whole body (which would clobber the other lines): get its path with `dfc show <id> --json | jq -r .path`, then flip that one line's `- [ ]` to `- [x]`. The mtime sort / watcher picks the change up automatically. Markers may be `-`, `*`, `+`, or `1.`; `[X]` also counts as done. Items inside fenced code blocks are ignored.

## Commands

All flags below are exhaustive. Aliases shown in parens.

### `dfc c` / `dfc capture` — create a task

```
dfc c [<description>...] [-p <slug>] [-d <details>] [--json]
dfc c -                    # read description+details from stdin
dfc c "desc" -d -          # description on argv, details from stdin
dfc c                      # TTY: open inline prompt; non-TTY: error
```

- `-p, --project <slug>`: target project (overrides cwd resolution; auto-registers).
- `-d, --details <body>`: optional body. `-` reads from stdin.
- `--json`: emit the created `TaskOut`.

### `dfc cc` — fuzzy-pick a project, then capture

Interactive only (requires a TTY). Discovers any project dir under `~/.dfc/projects/` so the picker covers projects you've never touched from the CLI. Single-project workspaces skip the picker.

### `dfc ls` / `dfc list` — list tasks

```
dfc ls [-p <slug-or-name> | -a] [--tag <name>]... [--status open|done|all]
       [--sort modified|created] [--json]
```

- `-p, --project <slug-or-name>`: list one project (defaults to cwd). Accepts a slug or display name (case-insensitive).
- `-a, --all`: merge every registered project. Mutually exclusive with `-p`.
- `--tag <name>`: filter to projects carrying this tag. Repeatable or comma-separated; OR semantics. `(untagged)` matches projects with no tags.
- `--status`: `open`, `done`, or `all` (default `all`).
- `--sort`: `modified` (default — file mtime) or `created` (frontmatter timestamp). Newest-first.
- `--json`: NDJSON, one `TaskOut` per line.

Human format: `<26-char ULID>  <○|✓>  [<prefix>] <description> [<done>/<total>]` (prefix only in `-a` mode; the `[done/total]` badge appears only when the task has a `- [ ]` checklist — see [Subtask checklists](#subtask-checklists)).

### `dfc s` / `dfc search` — full-text search (cwd-local by default)

### `dfc ss` — full-text search across every project

```
dfc s  <query>... [-p <slug-or-name>] [-a] [--tag <name>]... [--status ...] [-n 20]
                  [--sort score|modified|created] [--json] [--reindex]
dfc ss <query>... [-p <slug-or-name>]      [--tag <name>]... [--status ...] [-n 20]
                  [--sort score|modified|created] [--json] [--reindex]
```

- Query language: bare words are AND-ed and prefix-matched (`mac` → matches `macos`). Quoted `"exact phrase"` is exact. `-word` negates. `description:foo` / `details:bar` scope to a field.
- `-p` wins over `-a`/scope defaults. Accepts slug or display name (case-insensitive).
- `--tag <name>`: restrict to projects carrying this tag. Repeatable or comma-separated; OR semantics.
- `-n, --limit`: cap hits (default 20).
- `--sort`: `score` (default, BM25), `modified` (file mtime), or `created` (frontmatter timestamp).
- `--json`: NDJSON `{task: TaskOut, score, snippet}` per line.
- `--reindex`: drop & rebuild the index from disk before querying. Use only if results seem stale.

Human output groups by project when hits span >1 project. Description is bolded with `<mark>...</mark>` highlights replaced by ANSI inversion on TTY. ID rides on the last description line as a dim `[01KRJF7R2R]` (10-char ULID prefix — copy the full one from `--json`).

### `dfc show <26-char ULID>` — print one task

```
dfc show <26-char ULID> [-a|--all-projects] [--json]
```

### `dfc done <26-char ULID>` / `dfc reopen <26-char ULID>` — toggle status

```
dfc done   <26-char ULID> [-a|--all-projects] [--json]
dfc reopen <26-char ULID> [-a|--all-projects] [--json]
```

No-op when already in the target state (returns the row unchanged).

### `dfc edit <26-char ULID>` — rewrite description and/or details

```
dfc edit <26-char ULID> [--description <text>] [-d <body>|--details <body>|-]
                        [-a|--all-projects] [--json]
```

- `--description <text>`: rewrite the H1 heading and rename the on-disk file to match. No short flag.
- `-d, --details <body>`: replace the entire body. `-` reads from stdin.
- At least one of the two must be supplied.

**For surgical edits, edit the markdown file directly.** `dfc edit --details` overwrites the whole body, which destroys content when you only wanted to change one line. For typo fixes, paragraph additions, or any partial change, read the task's path and Edit the file in place:

```
dfc show <26-char ULID> --json | jq -r .path
```

Each task file is YAML frontmatter (keep `id` and `created` untouched; `status` is fine to flip) + a single H1 (the description) + an optional body. After an external edit, the watcher / mtime sort picks the change up automatically; if a search seems stale, pass `--reindex` to `dfc s`. The on-disk filename's slug is cosmetic — you don't need to rename the file when you change the H1.

Reserve `dfc edit` itself for: wholesale body rewrites (you intend to replace everything), or programmatic description renames (`--description "new title"` also fixes the filename slug).

### `dfc rm <26-char ULID>` — soft-delete a task

```
dfc rm <26-char ULID> [-a|--all-projects] [--json]
```

Moves the task to `~/.dfc/trash/` (recoverable via `dfc undo` until the TTL sweep gets it). `--json` returns a `RmOut` with the trash entry id.

### `dfc mv` / `dfc move` — move a task to another project

```
dfc mv <26-char ULID> <project> [-a|--all-projects] [--json]
```

Relocates the task's file into `<project>` and re-indexes it (the ULID is unchanged). `<project>` is a slug or display name, **created if new** (like `dfc c -p`). Moving into the project it's already in is a no-op (`already in <project>`). `--json` emits the moved `TaskOut` carrying its new `project`/`path`. Same TUI action as pressing `m` on a task.

**ID-lookup scope for `show / done / reopen / edit / rm / mv`**: by default the ULID is looked up only inside the cwd-resolved project; pass `-a` / `--all-projects` to search every project. Off-project IDs without `-a` error with a `pass --all-projects` hint. (For `mv`, the *source* is scoped this way; the *destination* is the explicit `<project>` argument.)

### `dfc undo` — restore the most recently trashed entry

```
dfc undo [--json]
```

Brings back the last task or project removed via `dfc rm` / TUI delete / `dfc trash empty`-survivors. Returns the trash `Manifest`.

### `dfc trash list|restore|empty` — inspect / surgically restore / purge

```
dfc trash list                       [--json]
dfc trash restore <id-or-prefix>     [--json]
dfc trash empty
```

`restore` accepts a unique prefix of the trash entry id (shown by `trash list`).

### `dfc projects` — manage projects

```
dfc projects [list]     [--tag <name>]... [--json]            # list (default)
dfc projects rename     <slug-or-name> <new-display-name>     [--json]
dfc projects set-prefix <slug-or-name> [<prefix>]            [--json]
dfc projects merge      <src> <dst>                          [--json]
```

- **(default / `list`)**: every registered project, ordered by recency (last-used desc, then slug asc). `--tag` filters to projects carrying that tag (repeatable / comma-separated, OR semantics; `(untagged)` for projects with none). `--json` emits one `ProjectOut` per line (NDJSON).
- **`rename <slug-or-name> <name>`**: set the display name (mirrors the TUI picker's ctrl+r). Errors if the name collides with another project. `--json` emits the updated `ProjectOut`.
- **`set-prefix <slug-or-name> [<prefix>]`**: set the global-view prefix (mirrors ctrl+p). Omit the prefix to revert to the derived default. Errors if it collides with another project's effective prefix. `--json` emits the updated `ProjectOut`.
- **`merge <src> <dst>`**: move every task from `src` into `dst`, then remove and deregister `src` — its files are *relocated*, not trashed. `dst` is created if new; `src` must exist; `src == dst` errors. If the `src` dir holds non-task files (a synced `.git`, notes), it's left intact with a warning instead of deleted. `--json` emits `{src, dst, moved}`.
- Slug-or-name args are case-insensitive; `merge`'s `dst` may be a brand-new slug.

### `dfc tags` — manage project categorical tags

```
dfc tags                                        # show cwd project's tags (alias: tags show)
dfc tags show [-p <slug-or-name>] [--json]
dfc tags add  [-p <slug-or-name>] <tag>...      # append (idempotent)
dfc tags rm   [-p <slug-or-name>] <tag>...      # remove (case-insensitive)
dfc tags set    [-p <slug-or-name>] <tag>...    # replace full list (empty list clears)
dfc tags rename <old> <new> [--merge]           # rename a tag across every project
dfc tags ls     [--json]                        # every distinct tag with project counts
```

- Default target is the cwd-resolved project; `--project/-p` overrides (slug or display name, case-insensitive).
- Tag args accept comma-separated *or* repeated values: `dfc tags add work,oss` ≡ `dfc tags add work oss`.
- Tag names are stored case-preserving but compared case-insensitively. Many-to-many across projects (a tag like `work` lives on every project you tag with it; the registry stores it once per project).
- `tags rename <old> <new>` is **cross-registry**: it rewrites every project carrying `old` (case-insensitive match) to `new` (written with the given casing). No-op (exit 0) when no project carries `old`. A project carrying *both* `old` and `new` errors unless you pass `--merge` (which folds them). `--json` emits `{renamed: <count>, projects: [<slug>...]}`.
- `tags show` / `tags add` / `tags rm` / `tags set` emit `{slug, tags}` with `--json`. `tags ls` emits one `{name, projects}` object per tag (NDJSON).

### `dfc project` — print the project resolved from cwd

```
dfc project [--json]
```

Useful for "which slug am I in right now?". JSON also includes `source` (`git-remote` | `git-toplevel` | `cwd`) and `raw` (pre-slug canonical form).

### `dfc count` — task counts for status lines

```
dfc count [-p <slug-or-name> | -a] [--format human|prompt|json]
```

Scope defaults to the cwd project (`-p` one project, `-a` all). Adds no state on disk — open/done come from the index, `done_today` from task mtimes.

- `--format human` (default): `<open> open · <done> done`.
- `--format prompt`: just the open count for shell prompts / tmux — `12○`, or empty when there are none. Built for [starship](https://starship.rs) `[custom.dfc] command = "dfc count --format prompt"`.
- `--format json`: `{open, done, done_today, by_tag}`. `done_today` = done tasks last modified today; `by_tag` maps each in-scope tag to its open-task count.

### `dfc` — interactive TUI

Bubbletea TUI. Requires a TTY. Press `?` inside for a key reference. Not invoked from agents — use the subcommands above.

## JSON shapes

`TaskOut` (capture / show / ls / done / reopen / edit):
```json
{
  "id": "01KRJF7R2RNCANPQGEXG610N8P",
  "project": "github-com-acme-widget",
  "status": "open",
  "description": "buy milk",
  "details": "",
  "path": "/Users/me/.dfc/projects/github-com-acme-widget/01krjf7r2r-buy-milk.md",
  "created": "2026-05-14T14:22:01Z",
  "modified": "2026-05-14T14:22:01Z"
}
```

`SearchHit` (search / ss):
```json
{ "task": { /* TaskOut */ }, "score": 4.5, "snippet": "buy <mark>milk</mark> and bread" }
```

`ProjectOut` (projects list / project / projects rename / projects set-prefix):
```json
{
  "slug": "github-com-acme-widget",
  "name": "Acme Widget",
  "prefix": "widget",
  "open": 3,
  "done": 12,
  "last_used": "2026-05-14T20:46:44Z",
  "source": "git-remote",
  "raw": "github.com/acme/widget"
}
```
`open`/`done` are the task counts. `source` and `raw` are present only in `project` (cwd) output; `last_used` is omitted when the project was never used. (Tags live under `dfc tags`, not in this row.)

`RmOut` (rm):
```json
{ "id": "01K...", "project": "github-com-acme-widget", "path": "/.../*.md", "trash_id": "01K..." }
```

`projects merge`:
```json
{ "src": "github-com-acme-old", "dst": "github-com-acme-widget", "moved": 7 }
```

`tags rename`:
```json
{ "renamed": 3, "projects": ["github-com-acme-widget", "users-me-notes", "..."] }
```
`renamed` is the project count; `projects` is `[]` on a no-op. (`projects rename` / `set-prefix` and `mv` reuse `ProjectOut` / `TaskOut` above.)

## Filesystem layout

- `~/.dfc/projects/<slug>/<10-char-ts>-<desc-slug>.md` — one file per task.
- `~/.dfc/projects.json` — project registry (name, prefix, tags, last_used).
- `~/.dfc/index.db` — SQLite FTS5 search index (write-through, rebuildable via `--reindex`).
- `~/.dfc/trash/<ulid>/` — soft-deleted tasks and projects, swept on a TTL.
- Override the root with `DFC_ROOT=/path` (tests, scratch environments).

Each markdown file:
```markdown
---
id: 01J9X7K3M8VQNH4Z7Y3PG2T5BD
status: open
created: 2026-05-13T14:22:01Z
---

# Description heading

Optional details body.
```

## Common flows

- **Capture a task you just thought of, in the current project:**
  `dfc c "rebuild the docs pipeline"`

- **Capture into another project without cd'ing:**
  `dfc c -p github-com-acme-widget "ship the migration"`

- **Pipe an agent buffer in as description + details:**
  `printf 'short title\n\nlong\nbody\n' | dfc c -`

- **List open tasks across everything:**
  `dfc ls -a --status open`

- **List open tasks across the "work" projects only:**
  `dfc ls -a --status open --tag work`

- **Tag the current project as work + oss:**
  `dfc tags add work oss`

- **Find tasks mentioning a phrase across every project:**
  `dfc ss "session token"`

- **Search scoped to a tag group:**
  `dfc ss "migration" --tag work`

- **Find tasks in the current project, JSON for further parsing:**
  `dfc s migration --json`

- **Move a task to another project (e.g. archive it):**
  `dfc mv -a 01KRJF7R2RNCANPQGEXG610N8P archive`

- **Fold a retired project's tasks into another, then drop it:**
  `dfc projects merge github-com-acme-old github-com-acme-widget`

- **Rename a tag everywhere at once:**
  `dfc tags rename wip in-progress`

- **Resolve the slug to use programmatically:**
  `dfc project --json | jq -r .slug`

- **Mark several done in a script:**
  `dfc ls -a --status open --json | jq -r 'select(.description|test("(?i)stale")) | .id' | xargs -n1 dfc done`

## Gotchas

- **IDs are full 26-char ULIDs.** Search output shows a dim 10-char prefix; `--json` gives the full ID. `done`, `reopen`, `edit`, `rm`, `show` require the full 26 chars.
- **ID lookup is cwd-scoped by default.** `done / reopen / edit / rm / show` look up the ULID only inside the cwd-resolved project; pass `-a` / `--all-projects` to search every project. Errors include a hint if you forget.
- **`dfc c` with no description and no TTY errors out.** In agent contexts always pass a description (or `-` for stdin).
- **`-a` and `-p` are mutually exclusive** on `ls`.
- **`-p` accepts slug or display name** (case-insensitive). `dfc ls -p "MaikuMori/auto"` and `dfc ls -p github-com-maikumori-auto` are equivalent.
- **Search index lag.** Write-through keeps it fresh on every mutation, but an external editor that bypasses the CLI can drift. Either let `EnsureFresh` catch it on the next query (automatic) or force with `--reindex`.
- **Prefer direct file edits for partial changes.** `dfc edit --details` replaces the whole body; for adding a paragraph or fixing a typo, get the path from `dfc show --json` and Edit the markdown directly. See the `dfc edit` section for details.
- **Status enum** is exactly `open` | `done`. There is no in-progress / cancelled.
- **`rm` is soft.** Deleted tasks live in `~/.dfc/trash/` and are recoverable via `dfc undo` until the TTL sweep. Use `dfc trash empty` to purge irrevocably.
- **Project slugs are deterministic from cwd**, so don't fabricate them — use `dfc project --json` or pass `-p` with a value the user gave you.
