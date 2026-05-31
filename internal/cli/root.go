package cli

import "github.com/alecthomas/kong"

// CLI is the kong-parsed root.
type CLI struct {
	VersionFlag kong.VersionFlag `name:"version" help:"Print version information and exit."`

	Capture CaptureCmd       `cmd:"" aliases:"c" help:"Capture a task. With no description, opens an inline prompt."`
	Cc      GlobalCaptureCmd `cmd:"" help:"Fuzzy-pick a project, then capture into it."`
	Ls      LsCmd            `cmd:"" aliases:"list" help:"List tasks (cwd project by default)."`
	Search  SearchCmd        `cmd:"" aliases:"s" help:"Full-text search in the current project (or -p/-a to override)."`
	Ss      SsCmd            `cmd:"" help:"Full-text search across every project."`
	Show    ShowCmd          `cmd:"" help:"Show a single task by full ULID."`
	Done    DoneCmd          `cmd:"" help:"Mark a task done."`
	Reopen  ReopenCmd        `cmd:"" help:"Mark a task open again."`
	Edit     EditCmd         `cmd:"" help:"Edit a task's description and/or details."`
	Rm       RmCmd           `cmd:"" help:"Remove a task by full ULID."`
	Projects ProjectsCmd     `cmd:"" help:"List every registered project."`
	Project  ProjectCmd      `cmd:"" help:"Show the project resolved from the current directory."`
	Tags     TagsCmd         `cmd:"" help:"Manage project categorical tags (show / add / rm / set / ls)."`
	Undo     UndoCmd         `cmd:"" help:"Restore the most recently trashed task or project."`
	Trash      TrashCmd      `cmd:"" help:"Inspect, restore, or empty the trash."`
	Version    VersionCmd    `cmd:"" help:"Show build version, commit, and date."`
	Completion CompletionCmd `cmd:"" help:"Emit shell completion script (bash, zsh, fish)."`
}
