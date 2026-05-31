package main

import (
	"fmt"
	"os"

	"github.com/alecthomas/kong"
	"github.com/posener/complete"
	"github.com/willabides/kongplete"

	"github.com/MaikuMori/dfc/internal/cli"
	"github.com/MaikuMori/dfc/internal/ui"
)

func main() {
	var root cli.CLI
	parser := kong.Must(&root,
		kong.Name("dfc"),
		kong.Description("Quick-capture task CLI."),
		kong.UsageOnError(),
		kong.Vars{"version": cli.VersionString()},
	)

	// When the shell invokes us for completion (COMP_LINE set), serve
	// completions and exit before any TUI / version / parse logic.
	completeWithAliases(parser, kongplete.WithPredictors(cli.Predictors()))

	if len(os.Args) == 1 {
		if err := ui.Run(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	ctx, err := parser.Parse(os.Args[1:])
	parser.FatalIfErrorf(err)

	if err := ctx.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// completeWithAliases is kongplete.Complete plus alias mirroring. kong
// records `aliases:"..."` on each Node, but kongplete's nodeCommand only
// registers the canonical Name in the completion tree. We post-process
// the tree so each alias points at the same subcommand and inherits its
// flags, positional predictors, and nested subtree.
func completeWithAliases(parser *kong.Kong, opts ...kongplete.Option) {
	if parser == nil {
		return
	}
	cmd, err := kongplete.Command(parser, opts...)
	if err != nil {
		parser.Errorf("error running command completion: %v", err)
		parser.Exit(1)
		return
	}
	mirrorAliases(&cmd, parser.Model.Node)
	cmp := complete.New(parser.Model.Name, cmd)
	cmp.Out = parser.Stdout
	if cmp.Complete() {
		parser.Exit(0)
	}
}

// mirrorAliases walks the kong tree and copies the completion subcommand
// for each Name into every Alias key. complete.Command.Sub is a map, so
// recursive copies share the same nested map references — aliases at
// deeper levels propagate without explicit re-assignment.
func mirrorAliases(cmd *complete.Command, node *kong.Node) {
	if cmd == nil || node == nil {
		return
	}
	for _, child := range node.Children {
		if child == nil || child.Hidden {
			continue
		}
		sub, ok := cmd.Sub[child.Name]
		if !ok {
			continue
		}
		for _, alias := range child.Aliases {
			cmd.Sub[alias] = sub
		}
		mirrorAliases(&sub, child)
	}
}
