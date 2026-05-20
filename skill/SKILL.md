---
name: dfc
description: Quick-capture task CLI for projects. Use whenever the user mentions a task or todo — adding, listing, searching, completing, or removing.
---

# dfc

Quick-capture task CLI. Project scope is auto-derived from the current working directory (git remote → git toplevel → cwd path) and can always be overridden with `-p <slug>` or `-a` (all projects).

Use it when the user asks to:
- jot down a task ("remind me to ...", "add a todo", "track that I need to ...")
- list, search, or filter tasks
- mark something done / reopen / edit / delete
- inspect or rename a project

## Output modes

Every command except the interactive TUI (`dfc`) emits human text by default and structured JSON with `--json`. **Always pass `--json` when parsing programmatically** — the human format may change without notice.

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
dfc ls [-p <slug> | -a] [--status open|done|all] [--json]
```

- `-p, --project <slug>`: list one project (defaults to cwd).
- `-a, --all`: merge every registered project. Mutually exclusive with `-p`.
- `--status`: `open`, `done`, or `all` (default `all`).
- `--json`: NDJSON, one `TaskOut` per line.

Human format: `<ULID>  <○|✓>  [<tag>] <description>` (tag only in `-a` mode).

### `dfc s` / `dfc search` — full-text search (cwd-local by default)

### `dfc ss` — full-text search across every project

```
dfc s  <query>... [-p <slug>] [-a] [--status ...] [-n 20] [--sort score|modified] [--json] [--reindex]
dfc ss <query>... [-p <slug>]      [--status ...] [-n 20] [--sort score|modified] [--json] [--reindex]
```

- Query language: bare words are AND-ed and prefix-matched (`mac` → matches `macos`). Quoted `"exact phrase"` is exact. `-word` negates. `description:foo` / `details:bar` scope to a field.
- `-p` wins over `-a`/scope defaults.
- `-n, --limit`: cap hits (default 20).
- `--sort`: `score` (default, BM25) or `modified` (newest first).
- `--json`: NDJSON `{task: TaskOut, score, snippet}` per line.
- `--reindex`: drop & rebuild the index from disk before querying. Use only if results seem stale.

Human output groups by project when hits span >1 project. Description is bolded with `<mark>...</mark>` highlights replaced by ANSI inversion on TTY. ID rides on the last description line as a dim `[01KRJF7R2R]` (10-char ULID prefix — copy the full one from `--json`).

### `dfc show <id>` — print one task

```
dfc show <26-char ULID> [--json]
```

### `dfc done <id>` / `dfc reopen <id>` — toggle status

```
dfc done   <26-char ULID> [--json]
dfc reopen <26-char ULID> [--json]
```

No-op when already in the target state (returns the row unchanged).

### `dfc edit <id>` — rewrite description and/or details

```
dfc edit <ULID> [-d <new-desc>] [--details <body>|-] [--json]
```

- `-d, --description`: rename the heading (and the on-disk file).
- `--details`: replace the body. `-` reads from stdin.
- At least one of the two must be supplied.

### `dfc rm <id>` — soft-delete a task

```
dfc rm <26-char ULID> [--json]
```

Moves the task to `~/.dfc/trash/` (recoverable via `dfc undo` until the TTL sweep gets it). `--json` returns a `RmOut` with the trash entry id.

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

### `dfc projects` — list registered projects

```
dfc projects [--json]
```

Ordered by recency (last-used desc, then slug asc).

### `dfc project` — print the project resolved from cwd

```
dfc project [--json]
```

Useful for "which slug am I in right now?". JSON also includes `source` (`git-remote` | `git-toplevel` | `cwd`) and `raw` (pre-slug canonical form).

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

`ProjectOut` (projects / project):
```json
{
  "slug": "github-com-acme-widget",
  "name": "Acme Widget",
  "tag": "widget",
  "last_used": "2026-05-14T20:46:44Z",
  "source": "git-remote",
  "raw": "github.com/acme/widget"
}
```
`source` and `raw` only present in `project` output.

`RmOut` (rm):
```json
{ "id": "01K...", "project": "github-com-acme-widget", "path": "/.../*.md", "trash_id": "01K..." }
```

## Filesystem layout

- `~/.dfc/projects/<slug>/<10-char-ts>-<desc-slug>.md` — one file per task.
- `~/.dfc/projects.json` — project registry (name, tag, last_used).
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

- **Find tasks mentioning a phrase across every project:**
  `dfc ss "session token"`

- **Find tasks in the current project, JSON for further parsing:**
  `dfc s migration --json`

- **Resolve the slug to use programmatically:**
  `dfc project --json | jq -r .slug`

- **Mark several done in a script:**
  `dfc ls -a --status open --json | jq -r 'select(.description|test("(?i)stale")) | .id' | xargs -n1 dfc done`

## Gotchas

- **IDs are full 26-char ULIDs.** Search output shows a dim 10-char prefix; `--json` gives the full ID. `done`, `reopen`, `edit`, `rm`, `show` require the full 26 chars.
- **`dfc c` with no description and no TTY errors out.** In agent contexts always pass a description (or `-` for stdin).
- **`-a` and `-p` are mutually exclusive** on `ls`.
- **Search index lag.** Write-through keeps it fresh on every mutation, but an external editor that bypasses the CLI can drift. Either let `EnsureFresh` catch it on the next query (automatic) or force with `--reindex`.
- **Status enum** is exactly `open` | `done`. There is no in-progress / cancelled.
- **`rm` is soft.** Deleted tasks live in `~/.dfc/trash/` and are recoverable via `dfc undo` until the TTL sweep. Use `dfc trash empty` to purge irrevocably.
- **Project slugs are deterministic from cwd**, so don't fabricate them — use `dfc project --json` or pass `-p` with a value the user gave you.
