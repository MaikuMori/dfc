package cli

import "github.com/MaikuMori/dfc/internal/ui"

// TuiCmd launches the TUI with an initial view. It is internal (hidden, and
// named `_tui`): it exists for the `dfc _tui --state <file>` command the TUI
// reports to terminals that can resume programs after a restart. Bare `dfc`
// is the same with no flags.
type TuiCmd struct {
	Project string `name:"project" short:"p" predictor:"project" help:"Open this project slug instead of the cwd project."`
	Global  bool   `name:"global" short:"g" help:"Open the global view across every project."`
	Query   string `name:"query" short:"q" help:"Start with this filter applied."`
	Sort    string `name:"sort" enum:"modified,created" default:"modified" help:"Order within each zone (modified, created)."`
	Cursor  string `name:"cursor" help:"Put the cursor on this task ID."`
	State   string `name:"state" hidden:"" help:"Restore a saved TUI state file."`
}

func (c *TuiCmd) Run() error {
	return ui.Run(ui.Options{
		Project: c.Project,
		Global:  c.Global,
		Query:   c.Query,
		Sort:    c.Sort,
		Cursor:  c.Cursor,
		State:   c.State,
	})
}
