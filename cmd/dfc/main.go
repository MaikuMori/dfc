package main

import (
	"fmt"
	"os"

	"github.com/MaikuMori/dfc/internal/cli"
	"github.com/MaikuMori/dfc/internal/ui"
	"github.com/alecthomas/kong"
	"github.com/willabides/kongplete"
)

func main() {
	var root cli.CLI
	parser := kong.Must(&root,
		kong.Name("dfc"),
		kong.Description("Quick-capture task CLI."),
		kong.UsageOnError(),
	)

	// When the shell invokes us for completion (COMP_LINE set), serve
	// completions and exit before any TUI / version / parse logic.
	kongplete.Complete(parser)

	if len(os.Args) == 1 {
		if err := ui.Run(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if os.Args[1] == "--version" {
		fmt.Println(cli.VersionString())
		return
	}

	ctx, err := parser.Parse(os.Args[1:])
	parser.FatalIfErrorf(err)

	if err := ctx.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
