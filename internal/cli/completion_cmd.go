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
# (e.g. ~/.zfunc/_dfc) and ensure compinit + bashcompinit are loaded.
autoload -U +X bashcompinit && bashcompinit
complete -o nospace -C dfc dfc
`

const fishCompletion = `# fish completion for dfc -*- shell-script -*-
#
# Install: dfc completion fish > ~/.config/fish/completions/dfc.fish
function __complete_dfc
    set -lx COMP_LINE (commandline -cp)
    test -z (commandline -ct)
    and set COMP_LINE "$COMP_LINE "
    dfc
end
complete -f -c dfc -a "(__complete_dfc)"
`
