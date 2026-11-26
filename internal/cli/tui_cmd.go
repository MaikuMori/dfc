package cli

import "github.com/MaikuMori/dfc/internal/ui"

// TuiCmd launches the TUI with an initial view. Bare `dfc` is the same with
// no flags; the TUI reports a `dfc tui` command to terminals that can resume
// programs after a restart.
type TuiCmd struct {
	Project string `name:"project" short:"p" predictor:"project" help:"Open this project slug instead of the cwd project."`
	Global  bool   `name:"global" short:"g" help:"Open the global view across every project."`
	Query   string `name:"query" short:"q" help:"Start with this filter applied."`
	Sort    string `name:"sort" enum:"modified,created" default:"modified" help:"Order within each zone (modified, created)."`
	Cursor  string `name:"cursor" help:"Put the cursor on this task ID."`
	Draft   string `name:"draft" hidden:"" help:"Reopen a saved draft file."`
}

func (c *TuiCmd) Run() error {
	return ui.Run(ui.Options{
		Project: c.Project,
		Global:  c.Global,
		Query:   c.Query,
		Sort:    c.Sort,
		Cursor:  c.Cursor,
		Draft:   c.Draft,
	})
}
