Output ../assets/social.gif

Set FontSize 32
Set Width 2560
Set Height 1080
Set Padding 36
Set Margin 40
Set MarginFill "#11111b"
Set BorderRadius 18
Set Theme "Catppuccin Mocha"
Set TypingSpeed 5ms

Env DFC_ROOT "/tmp/dfc-social"
Env PS1 "$ "

# Hidden: clean state + one git-initialized project + body fixture +
# pre-seeded captures. cwd-based captures align with the slug the TUI resolves later.
Hide
Type "rm -rf /tmp/dfc-social /tmp/acme-api && mkdir -p /tmp/acme-api && git -C /tmp/acme-api init -q && cp body.md /tmp/body.md && cd /tmp/acme-api"
Enter
Sleep 200ms

Type "dfc c 'stop the daily cron from emailing the ex-intern'"
Enter
Sleep 250ms
Type "dfc c 'name the variable that is just called data #naming'"
Enter
Sleep 250ms
Type `dfc c "untangle the mock that mocks the mock"`
Enter
Sleep 250ms
# Detailed task last → newest → cursor lands on it after the TUI opens.
Type `dfc c "the rate limiter is rate-limiting itself #p1" -d "$(cat /tmp/body.md)"`
Enter
Sleep 100ms

Type "clear"
Enter
Sleep 200ms
Show

# Open the per-project TUI, jump to the newest, expand the detailed body, hold.
Type "dfc"
Enter
Sleep 1.4s

Type "G"
Sleep 600ms

Tab
Sleep 5s
