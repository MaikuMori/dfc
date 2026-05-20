# Contributing to dfc

Thanks for considering a contribution. dfc is a small, fast Go CLI/TUI — the project values terse code, files-on-disk as the source of truth, and one or two keystrokes to any action.

## Local development

Prerequisites:

- **Go 1.26+** — the project tracks the current stable Go release.
- **just** — task runner. `brew install just`.
- **golangci-lint** — `brew install golangci-lint` or see [installation](https://golangci-lint.run/usage/install/).
- **changie** — release-notes tool. `brew install changie` or `go install github.com/miniscruff/changie@latest`.

Optional for release work: **goreleaser**.

```sh
git clone https://github.com/MaikuMori/dfc
cd dfc
just check     # vet + lint + test + vuln — run this before pushing
```

To isolate task data from your real `~/.dfc/` during development:

```sh
DFC_ROOT=/tmp/dfc-smoke go run ./cmd/dfc
```

`just licenses` runs a dependency-license check (`go-licenses check`). Not part of `just check` and not in CI — run it occasionally when you add a new dep.

## Workflow

### Changesets, not commit messages

Per user-visible change, run `changie new`. It prompts for:

- **Kind** — Added, Changed, Deprecated, Removed, Fixed, Security
- **Body** — a one-line imperative summary

`changie new` writes a YAML file to `.changes/unreleased/`. Commit it alongside your code change. The release pipeline rolls these into `CHANGELOG.md` automatically.

Skip the changeset for refactors, test-only changes, and docs-only edits — those don't need release notes.

### Tests

Test files live alongside the code (`*_test.go`). Storage, parsing, and pure logic get tests; TUI changes are tested manually.

Tests must isolate state with `t.TempDir()` and `t.Setenv("DFC_ROOT", ...)` — never write under the real `~/.dfc/` from a test.

### Pull requests

Open against `master`. `just check` must pass locally; CI runs lint, tests on Linux/macOS/Windows, govulncheck, and tidy on every push and PR.

## Code layout

`cmd/dfc/main.go` is kong wiring only. Everything else is under `internal/`:

- `cli/` — one file per subcommand
- `ui/` — bubbletea TUI
- `core/` — the transactional façade every mutation flows through
- `storage/` — disk I/O against `~/.dfc/projects/<slug>/*.md`, the source of truth
- `project/` — slug resolution + `~/.dfc/projects.json` registry
- `index/` — SQLite FTS5 mirror (derivable, never the source of truth)
- `trash/`, `capture/`, `watch/` — soft-delete, shared heading/body parser, fsnotify wrapper

The dependency layering is enforced by `depguard` rules in `.golangci.yml`. Don't fight them — they encode the architecture.

## License

By contributing, you agree that your contributions are licensed under the [MIT License](LICENSE).
