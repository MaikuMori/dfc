package main

import (
	"testing"

	"github.com/alecthomas/kong"
	"github.com/willabides/kongplete"

	"github.com/MaikuMori/dfc/internal/cli"
)

func TestVersionFlagWorksAfterSubcommand(t *testing.T) {
	var root cli.CLI
	exited := -1
	parser := kong.Must(&root,
		kong.Name("dfc"),
		kong.Vars{"version": "dfc test"},
		kong.Exit(func(code int) { exited = code }),
	)
	if _, err := parser.Parse([]string{"s", "--version"}); err != nil {
		t.Fatalf("parse `s --version`: %v", err)
	}
	if exited != 0 {
		t.Errorf("`s --version` should fire the version flag (exit 0), got exit %d", exited)
	}
}

// TestMirrorAliasesExposesEveryAlias guards against a kongplete regression
// where node aliases get dropped from the completion tree (the original
// issue: `dfc c -p <TAB>` returned subcommand names instead of slugs).
func TestMirrorAliasesExposesEveryAlias(t *testing.T) {
	var root cli.CLI
	parser := kong.Must(&root, kong.Name("dfc"))
	cmd, err := kongplete.Command(parser, kongplete.WithPredictors(cli.Predictors()))
	if err != nil {
		t.Fatalf("kongplete.Command: %v", err)
	}

	mirrorAliases(&cmd, parser.Model.Node)

	// Expected aliases per cli.CLI struct tags.
	want := map[string]string{
		"c":    "capture",
		"list": "ls",
		"s":    "search",
	}
	for alias, canonical := range want {
		ac, ok := cmd.Sub[alias]
		if !ok {
			t.Errorf("alias %q missing from completion tree", alias)
			continue
		}
		cc, ok := cmd.Sub[canonical]
		if !ok {
			t.Fatalf("canonical %q missing — test premise broken", canonical)
		}
		// Same Sub map header → mirroring shared the nested tree (deep aliases would propagate).
		if len(ac.Sub) != len(cc.Sub) || len(ac.GlobalFlags) != len(cc.GlobalFlags) {
			t.Errorf("alias %q diverges from %q: Sub=%d/%d Flags=%d/%d",
				alias, canonical, len(ac.Sub), len(cc.Sub), len(ac.GlobalFlags), len(cc.GlobalFlags))
		}
	}
}
