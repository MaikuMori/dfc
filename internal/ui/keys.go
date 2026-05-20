package ui

import (
	"strings"

	"charm.land/bubbles/v2/key"
)

type keyMap struct {
	Up               key.Binding
	Down             key.Binding
	PageUp           key.Binding
	PageDn           key.Binding
	Top              key.Binding
	Bottom           key.Binding
	Toggle           key.Binding
	Capture          key.Binding
	NewlineInCapture key.Binding
	Edit             key.Binding
	EditExt          key.Binding
	Delete           key.Binding
	Undo             key.Binding
	Expand           key.Binding
	Search           key.Binding
	SearchCommit     key.Binding
	SearchClear      key.Binding
	Switch           key.Binding
	PickerRename     key.Binding
	PickerSetTag     key.Binding
	PickerDelete     key.Binding
	Global           key.Binding
	Help             key.Binding
	Confirm          key.Binding
	Cancel           key.Binding
	Quit             key.Binding
}

var keys = keyMap{
	Up:     key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("k", "up")),
	Down:   key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("j", "down")),
	PageUp: key.NewBinding(key.WithKeys("pgup", "ctrl+u"), key.WithHelp("pgup", "scroll up")),
	PageDn: key.NewBinding(key.WithKeys("pgdown", "ctrl+d"), key.WithHelp("pgdn", "scroll down")),
	Top:    key.NewBinding(key.WithKeys("g", "home"), key.WithHelp("g", "top")),
	Bottom: key.NewBinding(key.WithKeys("G", "end"), key.WithHelp("G", "bottom")),
	Expand: key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "expand")),

	Capture:          key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "capture")),
	NewlineInCapture: key.NewBinding(key.WithKeys("alt+enter", "shift+enter", "ctrl+j"), key.WithHelp("⇧↵", "newline")),
	Edit:             key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit")),
	EditExt:          key.NewBinding(key.WithKeys("E"), key.WithHelp("E", "$EDITOR")),
	Toggle:           key.NewBinding(key.WithKeys("space"), key.WithHelp("␣", "toggle")),
	Delete:           key.NewBinding(key.WithKeys("D"), key.WithHelp("D", "delete")),
	Undo:             key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "undo delete")),

	Search:       key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
	SearchCommit: key.NewBinding(key.WithKeys("enter"), key.WithHelp("↵", "apply")),
	SearchClear:  key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "clear")),

	Switch:       key.NewBinding(key.WithKeys("P"), key.WithHelp("P", "switch")),
	PickerRename: key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("^r", "rename")),
	PickerSetTag: key.NewBinding(key.WithKeys("ctrl+t"), key.WithHelp("^t", "set tag")),
	PickerDelete: key.NewBinding(key.WithKeys("ctrl+shift+d"), key.WithHelp("^⇧d", "delete project")),
	Global:       key.NewBinding(key.WithKeys("A"), key.WithHelp("A", "all")),

	Help:    key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
	Confirm: key.NewBinding(key.WithKeys("enter"), key.WithHelp("↵", "save")),
	Cancel:  key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
	Quit:    key.NewBinding(key.WithKeys("q", "esc", "ctrl+c"), key.WithHelp("q", "quit")),
}

// ShortHelp drives the always-visible footer hint. Keep it to the keys a
// user reaches for several times a minute.
func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Capture, k.Search, k.Toggle, k.Global, k.Help, k.Quit}
}

// FullHelp drives the `?` overlay. Rows group related actions; columns
// align inside each row.
func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.PageUp, k.PageDn, k.Top, k.Bottom, k.Expand},
		{k.Capture, k.NewlineInCapture, k.Edit, k.EditExt, k.Toggle, k.Delete, k.Undo},
		{k.Search, k.SearchCommit, k.SearchClear},
		{k.Switch, k.PickerRename, k.PickerSetTag, k.PickerDelete, k.Global},
		{k.Help, k.Quit},
	}
}

// Modal footer hints. Each is a list of bindings the user can act on
// inside that mode. Rendered by joinBindings so the help text stays in
// lockstep with the canonical WithHelp(...) strings declared on each
// binding above — no parallel constants to drift.
func (k keyMap) InputHints() []key.Binding {
	return []key.Binding{k.Confirm, k.Cancel}
}

func (k keyMap) CaptureHints() []key.Binding {
	return []key.Binding{k.Confirm, k.NewlineInCapture, k.Cancel}
}

func (k keyMap) SearchHints() []key.Binding {
	return []key.Binding{k.SearchCommit, k.SearchClear}
}

func (k keyMap) FilterHints() []key.Binding {
	return []key.Binding{k.Search, k.SearchClear, k.Quit}
}

func (k keyMap) SwitchPickerHints() []key.Binding {
	return []key.Binding{k.Confirm, k.PickerRename, k.PickerSetTag, k.PickerDelete, k.Cancel}
}

func (k keyMap) CaptureTargetHints() []key.Binding {
	return []key.Binding{k.Confirm, k.Cancel}
}

// joinBindings renders the given bindings as a one-line "k  desc · k desc"
// footer hint. Uses each binding's WithHelp(...) text so the rendered string
// reflects whatever key actually fires the action.
func joinBindings(bs []key.Binding) string {
	parts := make([]string, 0, len(bs))
	for _, b := range bs {
		if !b.Enabled() {
			continue
		}
		h := b.Help()
		if h.Key == "" && h.Desc == "" {
			continue
		}
		parts = append(parts, h.Key+" "+h.Desc)
	}
	return strings.Join(parts, " · ")
}
