package ui

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/MaikuMori/dfc/internal/index"
	"github.com/MaikuMori/dfc/internal/project"
	"github.com/sahilm/fuzzy"
)

// ProjectPickerItems builds picker rows for every project in reg, ordered
// by recent use (most recent first), with name as the secondary key.
// Empty when the registry is empty. counts (optional, may be nil) provides
// open/done tallies that render alongside each row.
func ProjectPickerItems(reg *project.Registry, counts map[string]index.ProjectCounts) []PickerItem {
	slugs := reg.Slugs()
	items := make([]PickerItem, 0, len(slugs))
	for _, s := range slugs {
		c := counts[s]
		items = append(items, PickerItem{
			Slug:   s,
			Name:   reg.Name(s),
			Prefix: reg.Prefix(s),
			Open:   c.Open,
			Done:   c.Done,
		})
	}
	slices.SortStableFunc(items, func(a, b PickerItem) int {
		la, lb := reg.LastUsed(a.Slug), reg.LastUsed(b.Slug)
		if !la.Equal(lb) {
			return lb.Compare(la)
		}
		return cmp.Compare(a.Name, b.Name)
	})
	return items
}

// PickerItem is one row offered to the user. Both Name and Slug feed the
// fuzzy index so typing either matches. Prefix / Open / Done render in
// project-picker contexts; Count renders in many-select contexts (e.g.
// the project-count for a tag in the tag picker).
type PickerItem struct {
	Slug   string
	Name   string
	Prefix string
	Open   int
	Done   int
	Count  int
}

// pickerMode controls how the picker treats `enter` and `space`.
type pickerMode int

const (
	modeSelectOne  pickerMode = iota // single-select — project switcher, capture-target picker
	modeSelectMany                   // multi-select — filter overlay, tag editor
)

const pickerMaxRows = 8

// editKind tracks the picker's secondary in-place edit mode.
type editKind int

const (
	editNone          editKind = iota
	editRename                 // ctrl+r — change Name
	editPrefix                 // ctrl+p — change Prefix
	editConfirmDelete          // ctrl+shift+d — confirm full project deletion
	editAddItem                // n — many-mode "new item" prompt
)

// Picker is a reusable inline filter-and-pick component. It is NOT a
// top-level tea.Model: it doesn't emit tea.Quit. Callers embed it and check
// Done()/Selected()/Canceled() after each Update.
//
// Optional secondary edits:
//   - OnRename    + ctrl+r: change the highlighted item's Name.
//   - OnSetPrefix + ctrl+p: change the highlighted item's Prefix.
//
// In edit mode the input is pre-loaded with the current value; enter commits,
// esc cancels back to the filter.
type Picker struct {
	items    []PickerItem
	haystack []string
	input    textinput.Model
	matches  []int
	cursor   int
	selected string
	done     bool
	canceled bool
	prompt   string

	mode   pickerMode
	chosen map[string]bool // many-mode: keyed by item.Slug

	OnRename    func(slug, name string) error
	OnSetPrefix func(slug, prefix string) error
	// EnableTagEdit lets ctrl+t in single-mode exit the picker with
	// WantTagEditSlug set to the highlighted row. Callers wire this when
	// they want to open a follow-up tag editor for the chosen project;
	// off by default so standalone pickers (`dfc cc`) don't surprise-quit
	// on a stray ctrl+t.
	EnableTagEdit   bool
	WantTagEditSlug string
	// OnDelete is called after the user confirms deletion via ctrl+shift+d
	// followed by y. The picker also drops the row from its own items
	// slice and recomputes matches, so the deleted project disappears
	// immediately without a round-trip through the caller.
	OnDelete func(slug string) error
	// OnAdd, when non-nil in many-mode, enables a `n` keybinding that
	// opens an inline prompt for a brand-new item. The committed value is
	// appended to the picker's items as a new row and marked chosen. The
	// caller's handler decides whether to persist (e.g. into the
	// registry) on Done — the picker itself only updates its in-memory
	// list.
	OnAdd func(name string)

	edit        editKind
	editIdx     int    // index into items being edited
	savedFilter string // filter value to restore on edit cancel
	editErr     error
}

// NewPicker builds a Picker. The initial match order matches the order in
// items, so caller-supplied ordering (e.g. by recency) is preserved when the
// filter is empty.
func NewPicker(prompt string, items []PickerItem) Picker {
	ti := textinput.New()
	ti.Prompt = "> "
	ti.Placeholder = "filter"
	// Without an explicit Width the placeholder gets truncated to a single
	// rune in v2 (its rendering buffer is sized to Width+1). Callers can
	// resize via Picker.SetWidth once they know the terminal dimensions.
	ti.SetWidth(40)
	tiStyles := textinput.DefaultStyles(hasDarkBG)
	tiStyles.Focused.Prompt = stylePrompt
	tiStyles.Blurred.Prompt = stylePrompt
	ti.SetStyles(tiStyles)
	ti.Focus()

	haystack := make([]string, len(items))
	for i, it := range items {
		haystack[i] = it.Name + "\t" + it.Slug
	}

	p := Picker{
		items:    items,
		haystack: haystack,
		input:    ti,
		prompt:   prompt,
	}
	p.recomputeMatches()
	return p
}

// FocusSlug parks the cursor on the row whose Slug matches the given
// value, if any. No-op when the slug isn't in the item list or the
// current filter hides it. Used to re-open a picker pointing at the
// same project the user just left.
func (p *Picker) FocusSlug(slug string) {
	for i, m := range p.matches {
		if p.items[m].Slug == slug {
			p.cursor = i
			return
		}
	}
}

// SetMode switches the picker to single- or multi-select. Multi-mode
// enables `space` to toggle the row under the cursor, `c` to clear all
// selections, and `n` (when OnAdd is wired) to add a new item. `enter`
// still ends the picker session; in many-mode the caller reads
// Selection() instead of Selected().
func (p *Picker) SetMode(mode pickerMode) {
	p.mode = mode
	if mode == modeSelectMany && p.chosen == nil {
		p.chosen = map[string]bool{}
	}
}

// PreselectMany seeds the multi-select set with the given slugs. Only
// meaningful in many-mode. Pass nil/empty to clear.
func (p *Picker) PreselectMany(slugs []string) {
	if p.chosen == nil {
		p.chosen = map[string]bool{}
	}
	clear(p.chosen)
	for _, s := range slugs {
		p.chosen[s] = true
	}
}

// Selection returns the chosen slugs in the picker's current display
// order (top-to-bottom, which is highest-match-index to lowest). Only
// meaningful after Done() in many-mode.
func (p Picker) Selection() []string {
	out := make([]string, 0, len(p.chosen))
	for _, it := range p.items {
		if p.chosen[it.Slug] {
			out = append(out, it.Slug)
		}
	}
	return out
}

// AppendItem adds a row to the picker (used by the many-mode "new item"
// flow). The row goes to the end of items so the new match shows on top
// of the bottom-up render. Auto-selects in many-mode.
func (p *Picker) AppendItem(name string) {
	slug := name
	p.items = append(p.items, PickerItem{Slug: slug, Name: name})
	p.haystack = append(p.haystack, name+"\t"+slug)
	if p.mode == modeSelectMany {
		if p.chosen == nil {
			p.chosen = map[string]bool{}
		}
		p.chosen[slug] = true
	}
	p.recomputeMatches()
}

// Init returns a Cmd to start textinput blinking. Safe to ignore when
// embedded.
func (p Picker) Init() tea.Cmd { return textinput.Blink }

// Update advances the picker. When Done() flips to true the caller should
// stop forwarding messages and read Selected()/Canceled().
func (p Picker) Update(msg tea.Msg) (Picker, tea.Cmd) {
	if p.edit != editNone {
		return p.updateEdit(msg)
	}
	if k, ok := msg.(tea.KeyPressMsg); ok {
		switch k.String() {
		case "ctrl+c", "esc":
			p.done = true
			p.canceled = true
			return p, nil
		case "enter":
			if p.mode == modeSelectMany {
				p.done = true
				return p, nil
			}
			if p.cursor >= 0 && p.cursor < len(p.matches) {
				p.selected = p.items[p.matches[p.cursor]].Slug
			}
			p.done = true
			return p, nil
		case " ", "space":
			if p.mode == modeSelectMany && p.cursor >= 0 && p.cursor < len(p.matches) {
				slug := p.items[p.matches[p.cursor]].Slug
				if p.chosen == nil {
					p.chosen = map[string]bool{}
				}
				if p.chosen[slug] {
					delete(p.chosen, slug)
				} else {
					p.chosen[slug] = true
				}
				return p, nil
			}
		case "ctrl+l":
			if p.mode == modeSelectMany && len(p.chosen) > 0 {
				clear(p.chosen)
				return p, nil
			}
		case "ctrl+n":
			if p.mode == modeSelectMany && p.OnAdd != nil {
				return p.enterEdit(editAddItem), nil
			}
		// Bottom-up display: index 0 is at the bottom (best/most-recent),
		// so visually "up" walks toward higher indices and "down" walks
		// toward 0.
		case "up":
			if p.cursor < len(p.matches)-1 {
				p.cursor++
			}
			return p, nil
		case "down":
			if p.cursor > 0 {
				p.cursor--
			}
			return p, nil
		case "ctrl+r":
			if p.OnRename != nil && p.cursor >= 0 && p.cursor < len(p.matches) {
				return p.enterEdit(editRename), nil
			}
			return p, nil
		case "ctrl+p":
			if p.OnSetPrefix != nil && p.cursor >= 0 && p.cursor < len(p.matches) {
				return p.enterEdit(editPrefix), nil
			}
			return p, nil
		case "ctrl+t":
			if p.EnableTagEdit && p.cursor >= 0 && p.cursor < len(p.matches) {
				p.WantTagEditSlug = p.items[p.matches[p.cursor]].Slug
				p.done = true
				return p, nil
			}
			return p, nil
		case "ctrl+shift+d":
			if p.OnDelete != nil && p.cursor >= 0 && p.cursor < len(p.matches) {
				return p.enterEdit(editConfirmDelete), nil
			}
			return p, nil
		}
	}
	var cmd tea.Cmd
	prev := p.input.Value()
	p.input, cmd = p.input.Update(msg)
	if p.input.Value() != prev {
		p.recomputeMatches()
		// Typing a filter should park the cursor on the best/most-recent
		// match, which lives at the bottom of the bottom-up render.
		p.cursor = 0
	}
	return p, cmd
}

func (p Picker) enterEdit(kind editKind) Picker {
	p.savedFilter = p.input.Value()
	p.editErr = nil
	p.edit = kind
	if kind == editAddItem {
		// editAddItem isn't tied to a specific row.
		p.input.SetValue("")
		p.input.Placeholder = "new tag"
		return p
	}
	idx := p.matches[p.cursor]
	p.editIdx = idx
	switch kind {
	case editRename:
		p.input.SetValue(p.items[idx].Name)
		p.input.CursorEnd()
		p.input.Placeholder = "name"
	case editPrefix:
		p.input.SetValue(p.items[idx].Prefix)
		p.input.CursorEnd()
		p.input.Placeholder = "prefix"
	case editConfirmDelete:
		// Don't pre-load the textinput — we want a single y/n keystroke,
		// not editable text. The View renders a static prompt instead.
		p.input.SetValue("")
	}
	return p
}

// exitEdit leaves the secondary edit mode and restores the pre-edit
// filter (whatever the user had typed before pressing ^r / ^t / ^⇧d).
// Every caller wants the filter restored — there's no "wipe filter on
// exit" path — so the behavior is unconditional.
func (p Picker) exitEdit() Picker {
	p.edit = editNone
	p.editErr = nil
	// Reset the placeholder back to "filter" — editAddItem retitles it
	// to "new tag" while the prompt is open, and without resetting the
	// stale label leaks back into the filter view.
	p.input.Placeholder = "filter"
	p.input.SetValue(p.savedFilter)
	p.input.CursorEnd()
	p.recomputeMatches()
	return p
}

func (p Picker) updateEdit(msg tea.Msg) (Picker, tea.Cmd) {
	if p.edit == editConfirmDelete {
		return p.updateConfirmDelete(msg)
	}
	if k, ok := msg.(tea.KeyPressMsg); ok {
		switch k.String() {
		case "esc", "ctrl+c":
			return p.exitEdit(), nil
		case "enter":
			value := strings.TrimSpace(p.input.Value())
			if p.edit == editAddItem {
				if value == "" {
					return p.exitEdit(), nil
				}
				if p.OnAdd != nil {
					p.OnAdd(value)
				}
				p.AppendItem(value)
				return p.exitEdit(), nil
			}
			slug := p.items[p.editIdx].Slug
			switch p.edit {
			case editRename:
				if value == "" {
					return p.exitEdit(), nil
				}
				if p.OnRename != nil {
					if err := p.OnRename(slug, value); err != nil {
						p.editErr = err
						return p, nil
					}
				}
				p.items[p.editIdx].Name = value
				p.haystack[p.editIdx] = value + "\t" + slug
			case editPrefix:
				// Empty prefix is meaningful — reverts to default; allow it.
				if p.OnSetPrefix != nil {
					if err := p.OnSetPrefix(slug, value); err != nil {
						p.editErr = err
						return p, nil
					}
				}
				p.items[p.editIdx].Prefix = value
			}
			return p.exitEdit(), nil
		}
	}
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	return p, cmd
}

// updateConfirmDelete handles the y/N prompt that gates ctrl+shift+d.
// Only y (or Y) confirms — any other key cancels back to the filter so
// stray keystrokes during typing can't drop a project by accident.
func (p Picker) updateConfirmDelete(msg tea.Msg) (Picker, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return p, nil
	}
	if k.String() != "y" && k.String() != "Y" {
		return p.exitEdit(), nil
	}
	slug := p.items[p.editIdx].Slug
	if p.OnDelete != nil {
		if err := p.OnDelete(slug); err != nil {
			p.editErr = err
			return p, nil
		}
	}
	// Drop the item locally so the picker reflects the deletion without a
	// caller-driven refresh. We rebuild haystack/matches the same way
	// NewPicker did.
	p.items = append(p.items[:p.editIdx], p.items[p.editIdx+1:]...)
	p.haystack = append(p.haystack[:p.editIdx], p.haystack[p.editIdx+1:]...)
	p = p.exitEdit()
	if p.cursor >= len(p.matches) && len(p.matches) > 0 {
		p.cursor = len(p.matches) - 1
	}
	return p, nil
}

// pickerCountsLabel renders the right-edge open/done summary for a picker
// row. Empty when the project has no tasks indexed — keeps fresh registry
// entries from looking like 0/0 stubs.
func pickerCountsLabel(it PickerItem) string {
	if it.Open == 0 && it.Done == 0 {
		return ""
	}
	return fmt.Sprintf("· %d open · %d done", it.Open, it.Done)
}

func (p *Picker) recomputeMatches() {
	q := strings.TrimSpace(p.input.Value())
	if q == "" {
		p.matches = make([]int, len(p.items))
		for i := range p.items {
			p.matches[i] = i
		}
	} else {
		results := fuzzy.Find(q, p.haystack)
		p.matches = p.matches[:0]
		for _, r := range results {
			p.matches = append(p.matches, r.Index)
		}
	}
	if p.cursor >= len(p.matches) {
		p.cursor = max(0, len(p.matches)-1)
	}
}

// View renders the picker as a multi-line string. Callers compose this into
// their own layout.
func (p Picker) View() string {
	var b strings.Builder
	switch {
	case p.edit == editRename:
		b.WriteString(styleHint.Render("rename · " + p.items[p.editIdx].Slug))
		b.WriteByte('\n')
	case p.edit == editPrefix:
		b.WriteString(styleHint.Render("set prefix · " + p.items[p.editIdx].Slug))
		b.WriteByte('\n')
	case p.edit == editAddItem:
		b.WriteString(styleHint.Render("new tag · enter saves · esc cancels"))
		b.WriteByte('\n')
	case p.edit == editConfirmDelete:
		name := p.items[p.editIdx].Name
		slug := p.items[p.editIdx].Slug
		label := name
		if name != slug {
			label = name + " · " + slug
		}
		b.WriteString(styleError.Render("delete " + label + "? this wipes every task in the project."))
		b.WriteByte('\n')
		b.WriteString(styleHint.Render("press y to confirm · any other key cancels"))
		b.WriteByte('\n')
	case p.prompt != "":
		b.WriteString(styleHint.Render(p.prompt))
		b.WriteByte('\n')
	}

	// Bottom-up rendering: pick a window of up to pickerMaxRows around the
	// cursor, then walk it from highest index (visually top) down to lowest
	// (visually bottom, next to the input).
	start, end := 0, len(p.matches)
	if end > pickerMaxRows {
		start = p.cursor - pickerMaxRows/2
		if start < 0 {
			start = 0
		}
		end = start + pickerMaxRows
		if end > len(p.matches) {
			end = len(p.matches)
			start = end - pickerMaxRows
		}
	}

	for i := end - 1; i >= start; i-- {
		it := p.items[p.matches[i]]
		mark := ""
		if p.mode == modeSelectMany {
			if p.chosen[it.Slug] {
				mark = "● "
			} else {
				mark = "○ "
			}
		}
		counts := pickerCountsLabel(it)
		countLabel := ""
		if p.mode == modeSelectMany && it.Count > 0 {
			countLabel = fmt.Sprintf(" (%d)", it.Count)
		}
		if i == p.cursor {
			line := "> " + mark + it.Name + countLabel
			if it.Prefix != "" {
				line += "  [" + it.Prefix + "]"
			}
			if counts != "" {
				line += "  " + counts
			}
			b.WriteString(styleCursor.Render(line))
		} else {
			b.WriteString("  " + mark + it.Name)
			if countLabel != "" {
				b.WriteString(styleHint.Render(countLabel))
			}
			if it.Prefix != "" {
				b.WriteString(styleHint.Render("  [" + it.Prefix + "]"))
			}
			if counts != "" {
				b.WriteString(styleHint.Render("  " + counts))
			}
		}
		b.WriteByte('\n')
	}

	if len(p.matches) == 0 && p.edit == editNone {
		b.WriteString(styleEmpty.Render("  no matches"))
		b.WriteByte('\n')
	}

	if p.editErr != nil {
		b.WriteString(styleError.Render("error: " + p.editErr.Error()))
		b.WriteByte('\n')
	}

	// The y/N prompt swallows the input — we already render the static
	// "press y to confirm" hint above, so the textinput would just be a
	// visually empty `> _` line.
	if p.edit != editConfirmDelete {
		b.WriteString(p.input.View())
	}
	return b.String()
}

// SetWidth resizes the picker's textinput to match the surrounding layout.
// The model calls this from relayout so the placeholder/value render at
// full width rather than getting truncated to a single rune.
func (p *Picker) SetWidth(w int) {
	if w < 4 {
		w = 4
	}
	p.input.SetWidth(w - 2) // subtract prompt width
}

// EditingInput reports whether the picker has opened an inline text-
// input sub-mode (rename, set-prefix, or new-item-prompt). Callers use
// this to swap the outer footer hints for plain input hints (the picker
// renders its own one-line preamble for the sub-mode).
func (p Picker) EditingInput() bool {
	return p.edit == editRename || p.edit == editPrefix || p.edit == editAddItem
}

// ConfirmingDelete reports whether the picker is in the y/N gate for
// project deletion. The picker prints its own "press y to confirm · any
// other key cancels" line; callers use this to suppress the outer
// mode-specific hint chords which would otherwise look like they still
// did their advertised action.
func (p Picker) ConfirmingDelete() bool { return p.edit == editConfirmDelete }

func (p Picker) Done() bool       { return p.done }
func (p Picker) Canceled() bool   { return p.canceled }
func (p Picker) Selected() string { return p.selected }

// --- standalone Pick() wrapper, for `dfc cc`. ---

type pickerProgram struct{ p Picker }

func (pp pickerProgram) Init() tea.Cmd { return pp.p.Init() }
func (pp pickerProgram) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if sz, ok := msg.(tea.WindowSizeMsg); ok {
		pp.p.SetWidth(sz.Width)
		return pp, nil
	}
	var cmd tea.Cmd
	pp.p, cmd = pp.p.Update(msg)
	if pp.p.done {
		return pp, tea.Quit
	}
	return pp, cmd
}
func (pp pickerProgram) View() tea.View {
	return tea.NewView(pp.p.View())
}

// Pick runs the picker as a standalone inline program. ok is false on cancel.
// onRename / onSetPrefix, if non-nil, enable ctrl+r and ctrl+p edit modes.
func Pick(prompt string, items []PickerItem, onRename, onSetPrefix func(slug, value string) error) (slug string, ok bool, err error) {
	if len(items) == 0 {
		return "", false, nil
	}
	picker := NewPicker(prompt, items)
	picker.OnRename = onRename
	picker.OnSetPrefix = onSetPrefix
	final, runErr := tea.NewProgram(pickerProgram{p: picker}).Run()
	if runErr != nil {
		return "", false, runErr
	}
	p := final.(pickerProgram).p
	if p.canceled || p.selected == "" {
		return "", false, nil
	}
	return p.selected, true, nil
}
