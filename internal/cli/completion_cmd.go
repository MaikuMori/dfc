package cli

import "fmt"

type CompletionCmd struct {
	Shell string `arg:"" enum:"bash,zsh,fish" help:"Shell to emit completions for (bash, zsh, fish)."`
}

func (c CompletionCmd) Run() error {
	switch c.Shell {
	case "bash":
		fmt.Print(bashCompletion)
	case "zsh":
		fmt.Print(zshCompletion)
	case "fish":
		fmt.Print(fishCompletion)
	}
	return nil
}

// The runtime completion handler lives in cmd/dfc/main.go via kongplete; these
// scripts just register `dfc` as the completion command for the shell.
const bashCompletion = `# bash completion for dfc -*- shell-script -*-
#
# Install: eval "$(dfc completion bash)" in .bashrc, or save to
#   /etc/bash_completion.d/dfc (root) or /usr/share/bash-completion/completions/dfc.
complete -o nospace -C dfc dfc
`

const zshCompletion = `# zsh completion for dfc -*- shell-script -*-
#
# Install: source <(dfc completion zsh) in .zshrc, or save to a file on $fpath
# (e.g. ~/.zfunc/_dfc) and ensure compinit is loaded.
#
# Native completion (not bashcompinit-shimmed) so task descriptions can
# render alongside ULIDs via _describe.
_dfc() {
    local -a candidates descriptions
    local line value desc IFS=$'\n'

    for line in $(DFC_COMPLETE_FORMAT=rich COMP_LINE="$BUFFER" COMP_POINT=$CURSOR dfc 2>/dev/null); do
        if [[ "$line" == *$'\t'* ]]; then
            value="${line%%$'\t'*}"
            desc="${line#*$'\t'}"
            candidates+=("$value")
            descriptions+=("${value}:${desc}")
        else
            candidates+=("$line")
        fi
    done

    if (( ${#descriptions[@]} > 0 )); then
        _describe -t dfc 'dfc' descriptions
    elif (( ${#candidates[@]} > 0 )); then
        compadd -a candidates
    fi
}
compdef _dfc dfc
`

const fishCompletion = `# fish completion for dfc -*- shell-script -*-
#
# Install: dfc completion fish > ~/.config/fish/completions/dfc.fish
#
# Tab-separated id\tdescription lines are split natively by fish's
# 'complete -a' so descriptions show next to ULIDs.
function __complete_dfc
    set -lx COMP_LINE (commandline -cp)
    test -z (commandline -ct)
    and set COMP_LINE "$COMP_LINE "
    set -lx DFC_COMPLETE_FORMAT rich
    dfc
end
complete -f -c dfc -a "(__complete_dfc)"
`
