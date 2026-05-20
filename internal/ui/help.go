package ui

import (
	"charm.land/bubbles/v2/help"
)

// helpFull is the keymap-driven `?` overlay. The bubbles/help component
// reads each binding's WithHelp(short, desc) directly (via keyMap's
// ShortHelp / FullHelp methods in keys.go), so the help overlay stays
// in lockstep with the actual key bindings — no parallel reference
// table to drift out of sync.
var helpFull = func() help.Model {
	h := help.New()
	h.ShowAll = true
	h.FullSeparator = "    "
	h.Styles = help.DefaultStyles(hasDarkBG)
	return h
}()

// helpShort is the compact one-line footer hint used in list mode.
var helpShort = func() help.Model {
	h := help.New()
	h.ShowAll = false
	h.ShortSeparator = " · "
	h.Styles = help.DefaultStyles(hasDarkBG)
	return h
}()

// renderHelp returns the multi-section help text shown when the user
// presses `?`. width controls wrapping inside each row of FullHelp.
func renderHelp(width int) string {
	helpFull.SetWidth(width)
	return helpFull.View(keys)
}

// renderShortHelp returns the one-line footer hint shown in list mode.
func renderShortHelp(width int) string {
	helpShort.SetWidth(width)
	return helpShort.View(keys)
}
