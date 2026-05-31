default:
    @just --list

# Install the dfc binary into $GOBIN (defaults to $(go env GOPATH)/bin).
install:
    go install ./cmd/dfc
    @echo "installed dfc to $(go env GOBIN || echo $(go env GOPATH)/bin)/dfc"

# Remove the installed binary.
uninstall:
    rm -f "$$(go env GOBIN 2>/dev/null || echo $$(go env GOPATH)/bin)/dfc"
    @echo "uninstalled dfc"

# Install the dfc agent skill globally by symlinking it into each known
# agent's skill directory. The canonical content stays in <repo>/skill;
# the symlinks make it discoverable. `git pull` keeps every link fresh.
install-skill:
    mkdir -p ~/.claude/skills
    ln -sfn "$(pwd)/skill" ~/.claude/skills/dfc
    @echo "linked ~/.claude/skills/dfc -> $(pwd)/skill"

# Remove the symlinks. The canonical skill/ directory is untouched.
uninstall-skill:
    rm -f ~/.claude/skills/dfc
    @echo "unlinked ~/.claude/skills/dfc"

# Install both the binary and the skill.
install-all: install install-skill

# Build the binary into /tmp/dfc for smoke testing.
build:
    go build -o /tmp/dfc ./cmd/dfc

# Run the full test suite.
test:
    go test ./...

# Static analysis.
vet:
    go vet ./...

# Run golangci-lint across the module.
lint:
    golangci-lint run ./...

# Format code: goimports with local import grouping (internal imports last).
fmt:
    go tool goimports -w -local github.com/MaikuMori/dfc .

# Verify formatting without writing; fails if anything would change.
fmt-check:
    #!/usr/bin/env bash
    set -euo pipefail
    out=$(go tool goimports -l -local github.com/MaikuMori/dfc .)
    if [ -n "$out" ]; then
        echo "unformatted files (run \`just fmt\`):"
        echo "$out"
        exit 1
    fi

# Scan dependencies and module code for known vulnerabilities.
# Auto-installs govulncheck on first run.
vuln:
    @command -v govulncheck >/dev/null 2>&1 || go install golang.org/x/vuln/cmd/govulncheck@latest
    govulncheck ./...

# Generate a local coverage report (coverage.out + coverage.html).
cover:
    go test -race -covermode=atomic -coverprofile=coverage.out ./...
    go tool cover -html=coverage.out -o coverage.html
    @echo "wrote coverage.out and coverage.html — open coverage.html in a browser"

# Check dependency licenses. Fails on forbidden licenses (GPL/AGPL).
# --ignore: modernc.org/mathutil ships a valid BSD-3-Clause LICENSE that
# go-licenses' classifier can't recognize; manually verified.
licenses:
    @command -v go-licenses >/dev/null 2>&1 || go install github.com/google/go-licenses@latest
    go-licenses check --ignore=modernc.org/mathutil ./...

# fmt-check + vet + lint + tests + vuln in one shot.
check: fmt-check vet lint test vuln

# Remove local build, coverage, and snapshot artifacts (leaves committed docs/assets and the installed binary alone).
clean:
    rm -rf dist/ coverage.out coverage.html /tmp/dfc
    @echo "cleaned dist/, coverage.*, /tmp/dfc"

# Preview the next release's CHANGELOG section (read-only, leaves .changes/unreleased/ intact).
changelog-preview:
    changie batch auto --dry-run

# Validate the GoReleaser config without building.
release-check:
    goreleaser check

# Local snapshot release into ./dist (no upload, no publish).
# Uses `changie next auto` so the snapshot version reflects what the next
# real release would be (falls back to the latest git tag, then 0.0.0).
# Skips sign + sbom — those need syft/cosign locally and only matter in CI.
release-snapshot:
    #!/usr/bin/env bash
    set -euo pipefail
    NEXT=$(changie next auto 2>/dev/null || git describe --tags --abbrev=0 2>/dev/null || echo "v0.0.0")
    NEXT="v${NEXT#v}"  # normalize to a single leading v
    GORELEASER_CURRENT_TAG="${NEXT}" goreleaser release --snapshot --clean --skip=sign --skip=sbom

# Re-render the hero demo gif from docs/demos/hero.tape.
demo:
    cd docs/demos && vhs hero.tape
    @echo "wrote docs/assets/demo.gif"

# Render the high-res social card PNG (2560x1280) from docs/demos/social.tape.
# Pipeline: render gif → coalesce + take last frame → overlay project name + tagline.
social:
    cd docs/demos && vhs social.tape
    magick docs/assets/social.gif -coalesce -delete '0--2' -background '#11111b' -gravity south -extent 2560x1280 -gravity north -fill '#cba6f7' -font "$HOME/Library/Fonts/JetBrainsMonoNerdFont-ExtraBold.ttf" -pointsize 130 -annotate +0+30 "dfc" -fill '#cdd6f4' -font "$HOME/Library/Fonts/JetBrainsMonoNerdFont-Regular.ttf" -pointsize 44 -annotate +0+170 "don't forget cli" docs/assets/social.png
    rm docs/assets/social.gif
    @echo "wrote docs/assets/social.png"
