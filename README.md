# dfc, don't forget cli

> Quick-capture task CLI and TUI.

<p align="center">
  <img src="docs/assets/demo.gif" alt="demo" width="1200">
</p>

`dfc` is a small, fast task tracker that lives on your disk. Tasks are plain markdown files under `~/.dfc/projects/<slug>/`. A TUI gives you a fuzzy picker, search, and a global view across every project; a CLI gives shells and agents the same surface area.

## Why

When you're:

- juggling 10 projects;
- your're in a meeting;
- or actively doing something else;

 `dfc` lets you capture tasks quickly without interrupting your workflow.

`dfc` is not a replacement for a dedicated task management system; it's a quick-capture tool so you don't forget. You can also use it as external todo list for agents that can work across projects.

Key features:

- **Files on disk.** No database lock-in, no cloud, no account. Open the markdown in any editor.
- **Project-aware.** Tasks are grouped into projects by directory. Projects are auto-detected from your current working directory.
- **Power-user-first.** One or two keystrokes to any action.
- **Agent-friendly.** Every CLI command supports `--json`. An agent skill manifest lives in [`skill/SKILL.md`](skill/SKILL.md).

## Why not x?

Probably because either `x` does too much or is not fast enough to invoke. Also I want project grouping to just work without having to think about it.

## Install

```sh
go install github.com/MaikuMori/dfc/cmd/dfc@latest
```

From source:

```sh
git clone https://github.com/MaikuMori/dfc
cd dfc
just install
```

### Agent skill

The skill manifest lives at [`skill/SKILL.md`](skill/SKILL.md). Three install paths, pick whichever fits:

**1. Point your agent at the GitHub URL.** Most agents (Claude Code, Cursor, …) can fetch + persist a remote file. Prompt with:

> Install the dfc skill from `https://raw.githubusercontent.com/MaikuMori/dfc/master/skill/SKILL.md` into your skills directory.

**2. Curl it directly** (Claude Code path shown; swap the dir for other runtimes):

```sh
mkdir -p ~/.claude/skills/dfc
curl -fsSL https://raw.githubusercontent.com/MaikuMori/dfc/master/skill/SKILL.md \
  -o ~/.claude/skills/dfc/SKILL.md
```

**3. From a clone** (symlink so `git pull` keeps the skill fresh):

```sh
cd dfc
just install-skill        # links skill/ → ~/.claude/skills/dfc
just install-all          # binary + skill in one shot
```

## Quickstart

```sh
# Capture a task in the current project (auto-detected from cwd)
dfc c "wire up retry on the upload endpoint"

# Open the TUI
dfc

# List open tasks
dfc ls

# Search across every project
dfc ss "retry"

# Mark done by ULID
dfc done 01J9X7K3M8VQNH4Z7Y3PG2T5BD
```

Full command reference: `dfc --help`.

## Shell completion

```sh
# bash
eval "$(dfc completion bash)"
# zsh — add to .zshrc, or save to a file on $fpath:
source <(dfc completion zsh)
# fish
dfc completion fish > ~/.config/fish/completions/dfc.fish
```

## Storage

```text
~/.dfc/
├── projects/<slug>/<ulid>-<desc>.md    # tasks: YAML frontmatter + markdown body
├── projects.json                       # registry: slug → name, tag, last_used
├── index.db                            # SQLite FTS5 mirror for search
└── trash/<ulid>/                       # soft-deleted tasks (undo with `dfc undo`)
```

## Documentation

- [`skill/SKILL.md`](skill/SKILL.md) — teaches agents to use the CLI.
- [`CONTRIBUTING.md`](CONTRIBUTING.md) — local dev setup, changeset workflow, code layout.
- [`CHANGELOG.md`](CHANGELOG.md) — release notes.

## AI Policy

AI is a tool. If I buy a house, I don't care if you used a golden hammer or a stone as long as it is done well. If all the screws are hammered in and everything is polished with an axe obviously something is wrong. Use whatever tools you're comfortable with as long as the end result is of good quality.

## License

MIT — see [LICENSE](LICENSE).
