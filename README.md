# dfc, don't forget cli

> Quick-capture task CLI & TUI for you and your minions.

[![CI](https://github.com/MaikuMori/dfc/actions/workflows/ci.yml/badge.svg)](https://github.com/MaikuMori/dfc/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/MaikuMori/dfc)](https://github.com/MaikuMori/dfc/releases/latest)
[![Go Report Card](https://goreportcard.com/badge/github.com/MaikuMori/dfc)](https://goreportcard.com/report/github.com/MaikuMori/dfc)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

<p align="center">
  <img src="docs/assets/demo.gif" alt="demo" width="1200">
</p>

`dfc` is a small, fast task tracker that lives on your disk. Tasks are plain markdown files under `~/.dfc/projects/<slug>/`.

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

### Minion skill

The skill manifest lives at [`skill/SKILL.md`](skill/SKILL.md). Three install paths, pick whichever fits:

**1. Point your minion at the GitHub URL.** Most minions (Claude Code, Codex, …) can fetch + persist a remote file. Prompt with:

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

## Why

When you're:

- juggling 10 projects,
- in a meeting,
- or actively doing something else,

`dfc` lets you capture tasks quickly without interrupting your workflow.

`dfc` doesn't try to replace a real task manager — it's the inbox in front of one, so ideas survive until you deal with them. It also doubles as an external todo list for minions that work across projects.

Key features:

- **Files on disk.** No database lock-in, no cloud, no account. Open the markdown in any editor.
- **Project-aware.** Tasks are grouped into projects by directory. Projects are auto-detected from your current working directory.
- **Power-user-first.** One or two keystrokes to any action.
- **Tags & checklists.** Inline `#tags` filter and search across projects; `- [ ]` checklists get a `[done/total]` badge.
- **Minion-friendly.** Every CLI command supports `--json`. A skill manifest for your minions lives in [`skill/SKILL.md`](skill/SKILL.md).

## Why not x?

Probably because either `x` does too much or is not fast enough to invoke. Also I want project grouping to just work without having to think about it.

## Shell completion

```sh
# bash
eval "$(dfc completion bash)"
# zsh — add to .zshrc, or save to a file on $fpath:
source <(dfc completion zsh)
# fish
dfc completion fish > ~/.config/fish/completions/dfc.fish
```

## Status line

`dfc count --format prompt` prints the cwd project's open-task count (`12○`, or nothing when there are none) — drop it into [starship](https://starship.rs):

```toml
# ~/.config/starship.toml — appends the segment to the end of the first line
format = "$all${custom.dfc}$line_break$character"

[custom.dfc]
command = "dfc count --format prompt"
when = true
format = " [$output]($style)"
style = "bold yellow"
```

`--format json` (`{open, done, done_today, by_tag}`) feeds fancier prompts and tmux status lines; bare `dfc count` is human-readable. Add `-a` to count every project instead of the cwd one.

## Documentation

- [`skill/SKILL.md`](skill/SKILL.md) — teaches minions to use the CLI.
- [`CONTRIBUTING.md`](CONTRIBUTING.md) — local dev setup, changeset workflow, code + storage layout.
- [`CHANGELOG.md`](CHANGELOG.md) — release notes.

## AI policy

AI is a tool. If I buy a house, I don't care whether the builder used a golden hammer or a stone — as long as it's built well. If the screws are hammered in and the trim is polished with an axe, obviously something went wrong. Same for contributions: use whatever tools you're comfortable with, but you own the result — understand what you're submitting and make sure it holds up. PRs that read like nobody checked them will be closed.

## License

MIT — see [LICENSE](LICENSE).
