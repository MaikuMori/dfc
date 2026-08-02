Output ../assets/demo.gif

Set Shell "bash"
Set FontSize 14
Set Width 1200
Set Height 760
Set Padding 16
Set Theme "Catppuccin Mocha"
Set TypingSpeed 40ms

Env DFC_ROOT "/tmp/dfc-demo"
Env PS1 "$ "

# Clean state + two git-initialized project dirs + body fixture.
Hide
Type "rm -rf /tmp/dfc-demo /tmp/acme-api /tmp/acme-web && mkdir -p /tmp/acme-api /tmp/acme-web && git -C /tmp/acme-api init -q && git -C /tmp/acme-web init -q && cp body.md /tmp/body.md && clear"
Enter
Sleep 300ms
Show

# ---- CLI captures across two projects, three flavors ----
Type "cd /tmp/acme-api"
Enter
Sleep 800ms

# Regular.
Type "dfc c 'stop the daily cron from emailing the ex-intern'"
Sleep 300ms
Enter
Sleep 1.2s

# Interactive — no args, inline prompt opens.
Type "dfc c"
Sleep 300ms
Enter
Sleep 1.4s
Type "name 'data' literally anything else"
Sleep 500ms
Enter
Sleep 1.6s

# cd into the other project for a regular capture there.
Type "cd /tmp/acme-web"
Enter
Sleep 800ms
Type "dfc c 'make the cookie banner slightly less beige #design'"
Sleep 300ms
Enter
Sleep 1.4s

# Cross-project picker — no cd needed.
Type "dfc cc"
Sleep 300ms
Enter
Sleep 1.4s
Type "api"
Sleep 500ms
Enter
Sleep 700ms
Type "untangle the mock that mocks the mock"
Sleep 500ms
Enter
Sleep 1.6s

# Hidden: stash the detailed task into acme-api (its rich body shines on Tab-expand).
Hide
Type `dfc c "the rate limiter is rate-limiting itself #p1" -d "$(cat /tmp/body.md)" -p acme-api`
Enter
Sleep 200ms
Type "cd /tmp/acme-api && clear"
Enter
Sleep 200ms
Show

# ---- TUI launch — cursor on the newest task (the detailed one) ----
Type "dfc"
Enter
Sleep 2.4s

# Expand to reveal the rendered markdown body.
Tab
Sleep 3.4s

# Move up and toggle a different task done (expansion stays put).
Up
Sleep 700ms
Space
Sleep 2.4s

# ---- Global view across both projects ----
Type "A"
Sleep 2.8s

# ---- Search across all projects ----
Type "/"
Sleep 300ms
Type "limit"
Sleep 700ms
Enter
Sleep 2.4s
Escape
Sleep 1.6s

# ---- Inline capture from global view (picker first) ----
Type "c"
Sleep 1.2s
Type "web"
Sleep 700ms
Enter
Sleep 600ms
Type "wire the VHS demo into the README before someone beats us to it"
Sleep 500ms
Enter
Sleep 2.4s

# ---- Quit ----
Type "q"
Sleep 2s

# ---- Agents drive the same CLI with --json ----
Type "clear"
Enter
Sleep 400ms

Type "# agents use the same CLI with --json"
Sleep 1.2s
Enter
Sleep 1s

Type "dfc c 'audit PR #142; the regex /[A-Z]/ is suspiciously /[A-z]/' -p acme-api --json | jq"
Sleep 300ms
Enter
Sleep 2.8s

Type "dfc ss limit --json | jq -r '.task.description'"
Sleep 300ms
Enter
Sleep 2.8s

# ---- Closing: full surface via less ----
Type "# ... and more, try it yourself:"
Sleep 1.2s
Enter
Sleep 600ms

Type "dfc --help | less"
Sleep 300ms
Enter
Sleep 1.8s
Space
Sleep 1.4s
Space
Sleep 1.4s
Type "q"
Sleep 4s
