package ui

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/MaikuMori/dfc/internal/capture"
	"github.com/MaikuMori/dfc/internal/core"
	"github.com/MaikuMori/dfc/internal/project"
	"github.com/MaikuMori/dfc/internal/storage"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

func (m Model) updateCaptureTarget(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.picker, cmd = m.picker.Update(msg)
	if !m.picker.Done() {
		return m, cmd
	}
	if m.picker.Canceled() || m.picker.Selected() == "" {
		// Cancel: drop the picker, stay in global list.
		m.picker = Picker{}
		m.mode = modeList
		m.capTargetSlug = ""
		m.relayout()
		return m, nil
	}
	m.capTargetSlug = m.picker.Selected()
	m.picker = Picker{}
	m.mode = modeCapture
	m.beginCapture()
	m.relayout()
	return m, textarea.Blink
}

func (m Model) updateHelp(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		return m, tea.Quit
	}
	// Any other keystroke dismisses help and returns to the list.
	m.mode = modeList
	m.relayout()
	return m, nil
}

func (m Model) updateSwitch(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.picker, cmd = m.picker.Update(msg)
	if !m.picker.Done() {
		return m, cmd
	}
	// ctrl+t on the switcher hands off to the tag editor for the
	// highlighted project (the switcher closes; tag picker opens).
	if slug := m.picker.WantTagEditSlug; slug != "" {
		m.tagEditSlug = slug
		m.picker = newTagEditPicker(m.core.Registry(), slug)
		m.mode = modeTagEdit
		m.relayout()
		return m, m.picker.Init()
	}
	if !m.picker.Canceled() && m.picker.Selected() != "" {
		if err := m.switchTo(m.picker.Selected()); err != nil {
			m.err = err
		}
	}
	m.picker = Picker{}
	m.mode = modeList
	m.relayout()
	return m, cmd
}

// updateTagEdit is the modeTagEdit dispatcher. The picker is a multi-
// select sub-picker over the registry's known tags, prefilled with the
// target project's current tag list. Enter commits via SetTags; esc
// discards. Either way the user is returned to the project switcher
// they came from, cursor parked on the just-edited slug.
func (m *Model) updateTagEdit(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.picker, cmd = m.picker.Update(msg)
	if !m.picker.Done() {
		return m, cmd
	}
	if !m.picker.Canceled() {
		slug := m.tagEditSlug
		if slug != "" {
			if err := m.core.Registry().SetTags(slug, m.picker.Selection()); err != nil {
				m.err = err
			}
		}
	}
	focus := m.tagEditSlug
	m.tagEditSlug = ""
	if cmdSwitch, ok := m.openProjectSwitcher(focus); ok {
		return m, tea.Batch(cmd, cmdSwitch)
	}
	// Fall back to the list view (single-project workspaces shouldn't
	// have been able to enter tag-edit, but defend anyway).
	m.picker = Picker{}
	m.mode = modeList
	m.relayout()
	return m, cmd
}

// openProjectSwitcher builds (or rebuilds) the modeSwitch picker. When
// focusSlug is non-empty, the cursor parks on that slug so callers
// returning from a sub-modal (tag editor) don't lose the user's place.
// Returns ok=false when there's nothing to switch to (≤1 project) — the
// status hint is set as a side effect.
func (m *Model) openProjectSwitcher(focusSlug string) (tea.Cmd, bool) {
	reg := m.core.Registry()
	items := ProjectPickerItems(reg, m.core.CountsByProject())
	if len(items) <= 1 {
		m.status = "only one project — nothing to switch to"
		return nil, false
	}
	p := NewPicker("switch to:", items)
	p.OnRename = reg.Rename
	p.OnSetPrefix = reg.SetPrefix
	p.EnableTagEdit = true
	p.OnDelete = func(target string) error {
		if target == m.slug {
			return fmt.Errorf("can't delete the project you're viewing — switch first")
		}
		_, _, err := m.core.RemoveProject(target)
		return err
	}
	if focusSlug != "" {
		p.FocusSlug(focusSlug)
	}
	m.picker = p
	m.mode = modeSwitch
	m.relayout()
	return m.picker.Init(), true
}

// updateTagFilter is the modeTagFilter dispatcher. Same multi-picker
// shape as the tag editor; on commit the chosen tags become the
// session-only filter applied to the global task list.
func (m Model) updateTagFilter(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.picker, cmd = m.picker.Update(msg)
	if !m.picker.Done() {
		return m, cmd
	}
	if !m.picker.Canceled() {
		m.tagFilter = m.picker.Selection()
		m = m.reloadActive()
	}
	m.picker = Picker{}
	m.mode = modeList
	m.tagFilterBase = nil
	m.relayout()
	return m, cmd
}

// newTagFilterPicker builds the multi-picker that drives the global-view
// tag filter. Reuses tagPickerItems with the synthetic (untagged) row.
func newTagFilterPicker(reg *project.Registry, current []string) Picker {
	items := tagPickerItems(reg, true)
	picker := NewPicker("filter tags", items)
	picker.SetMode(modeSelectMany)
	picker.PreselectMany(current)
	return picker
}

// newTagEditPicker builds a many-select picker over reg.AllTags() with
// the target project's current tag list preselected. Stores the target
// slug on the Model via tagEditSlug.
func newTagEditPicker(reg *project.Registry, targetSlug string) Picker {
	items := tagPickerItems(reg, false)
	picker := NewPicker("tags · "+targetSlug, items)
	picker.SetMode(modeSelectMany)
	picker.PreselectMany(reg.Tags(targetSlug))
	picker.OnAdd = func(string) {} // presence signals "n" is enabled; no side effect
	return picker
}

// tagPickerItems returns the PickerItems that represent the registry's
// known tags, sorted by usage desc then name asc. When includeUntagged
// is true, a synthetic "(untagged)" row is appended (its count is the
// number of projects with no tags); used by the filter overlay only.
func tagPickerItems(reg *project.Registry, includeUntagged bool) []PickerItem {
	summaries := reg.AllTags()
	items := make([]PickerItem, 0, len(summaries)+1)
	for _, s := range summaries {
		items = append(items, PickerItem{Slug: s.Name, Name: s.Name, Count: len(s.Slugs)})
	}
	slices.SortStableFunc(items, func(a, b PickerItem) int {
		if a.Count != b.Count {
			return cmp.Compare(b.Count, a.Count)
		}
		return cmp.Compare(a.Name, b.Name)
	})
	if includeUntagged {
		untagged := reg.UntaggedSlugs()
		if len(untagged) > 0 {
			items = append(items, PickerItem{Slug: "(untagged)", Name: "(untagged)", Count: len(untagged)})
		}
	}
	return items
}

// switchTo points the model at a different project and repoints the
// watcher at the new project's directory.
func (m *Model) switchTo(slug string) error {
	store, err := m.core.StoreFor(slug)
	if err != nil {
		return err
	}
	tasks, err := store.List()
	if err != nil {
		return err
	}
	if err := m.core.Registry().Touch(slug); err != nil {
		return err
	}
	if m.watcher != nil {
		// Removing a missing path errors quietly; only the Add must succeed.
		_ = m.watcher.Remove(m.store.Dir())
		if err := m.watcher.Add(store.Dir()); err != nil {
			return err
		}
	}
	m.slug = slug
	m.store = store
	m.tasks = sortDoneFirst(tasks, m.sortKey)
	m.cursor = 0
	if len(m.tasks) > 0 {
		m.cursor = len(m.tasks) - 1
	}
	m.expandedID = ""
	m.err = nil
	return nil
}

func (m Model) updateList(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// Any list-mode keystroke implicitly dismisses the previous status hint.
	m.status = ""
	switch {
	// esc with an active filter clears the filter (and stays in the
	// list). esc with no filter falls through to Quit.
	case msg.Code == tea.KeyEsc && m.searchQuery != "":
		m.searchQuery = ""
		return m.reloadActive(), nil

	case msg.Code == tea.KeyEsc && m.globalView && len(m.tagFilter) > 0:
		m.tagFilter = nil
		return m.reloadActive(), nil

	case key.Matches(msg, keys.Quit):
		return m, tea.Quit

	case key.Matches(msg, keys.Up):
		m.moveCursor(-1)
	case key.Matches(msg, keys.Down):
		m.moveCursor(+1)
	case key.Matches(msg, keys.PageUp):
		m.scrollViewport(-m.viewport.Height() + 1)
	case key.Matches(msg, keys.PageDn):
		m.scrollViewport(+m.viewport.Height() - 1)
	case key.Matches(msg, keys.Top):
		m.cursor = 0
		m.followCursor()
	case key.Matches(msg, keys.Bottom):
		if len(m.tasks) > 0 {
			m.cursor = len(m.tasks) - 1
		}
		m.followCursor()

	case key.Matches(msg, keys.Toggle):
		if len(m.tasks) == 0 {
			return m, nil
		}
		// Hold the cursor at its on-screen position so rapid marking
		// drains the task under it instead of teleporting along with
		// each toggled item.
		prev := m.cursor
		t := m.tasks[m.cursor]
		if t.Status == storage.StatusDone {
			t.Status = storage.StatusOpen
		} else {
			t.Status = storage.StatusDone
		}
		if err := m.core.SaveTask(&t); err != nil {
			m.err = err
			return m, nil
		}
		m.err = nil
		m.tasks = sortDoneFirst(replaceByID(m.tasks, t), m.sortKey)
		if prev >= len(m.tasks) {
			prev = len(m.tasks) - 1
		}
		if prev < 0 {
			prev = 0
		}
		m.cursor = prev
		m.followCursor()
		if m.searchQuery != "" {
			m = m.runSearch()
		}

	case key.Matches(msg, keys.Filter):
		if !m.globalView {
			m.status = "f only works in global view (press A first)"
			return m, nil
		}
		reg := m.core.Registry()
		if len(reg.AllTags()) == 0 {
			m.status = "no tags yet — add some via the project switcher (P → ctrl+t)"
			return m, nil
		}
		m.picker = newTagFilterPicker(reg, m.tagFilter)
		m.mode = modeTagFilter
		m.tagFilterBase, _ = m.core.ListAll()
		m.relayout()
		return m, m.picker.Init()

	case key.Matches(msg, keys.Sort):
		m.sortKey = m.sortKey.next()
		// Re-sort in place, keeping the cursor on the same task by ID so
		// the user's eye doesn't lose its place when the order flips.
		var currentID, currentSlug string
		if m.cursor < len(m.tasks) {
			currentID = m.tasks[m.cursor].ID
			currentSlug = m.tasks[m.cursor].ProjectSlug
		}
		m.tasks = sortDoneFirst(m.tasks, m.sortKey)
		if currentID != "" {
			if i := indexByIDSlug(m.tasks, currentID, currentSlug, m.globalView); i >= 0 {
				m.cursor = i
			}
		}
		m.followCursor()
		m.status = "sort: " + m.sortKey.String()

	case key.Matches(msg, keys.Capture):
		if m.globalView {
			// In global view we don't have a "current project" to capture
			// into; offer a picker for the target first, then drop into
			// the capture overlay.
			items := ProjectPickerItems(m.core.Registry(), m.core.CountsByProject())
			if len(items) == 0 {
				m.status = "no projects to capture into"
				return m, nil
			}
			m.picker = NewPicker("capture to:", items)
			m.picker.OnRename = nil // keep ctrl+r inert during target choice
			m.mode = modeCaptureTarget
			m.relayout()
			return m, m.picker.Init()
		}
		m.mode = modeCapture
		m.beginCapture()
		m.relayout()
		return m, textarea.Blink

	case key.Matches(msg, keys.Edit):
		if len(m.tasks) == 0 {
			return m, nil
		}
		m.mode = modeEdit
		m.input.Reset()
		m.input.SetValue(m.tasks[m.cursor].Description)
		m.input.CursorEnd()
		m.input.Focus()
		m.relayout()
		return m, textinput.Blink

	case key.Matches(msg, keys.EditExt):
		if len(m.tasks) == 0 {
			return m, nil
		}
		return m, openInEditor(m.tasks[m.cursor].Path)

	case key.Matches(msg, keys.Expand):
		if len(m.tasks) == 0 {
			return m, nil
		}
		t := m.tasks[m.cursor]
		// Collapsing is always allowed; expanding only makes sense when
		// there's something to show.
		alreadyExpanded := m.expandedID == t.ID &&
			(!m.globalView || m.expandedSlug == t.ProjectSlug)
		switch {
		case alreadyExpanded:
			m.expandedID = ""
			m.expandedSlug = ""
		case strings.TrimSpace(t.Details) != "":
			m.expandedID = t.ID
			m.expandedSlug = t.ProjectSlug
		default:
			m.expandedID = ""
			m.expandedSlug = ""
			m.status = "no details"
		}
		m.followCursor()
		return m, nil

	case key.Matches(msg, keys.Undo):
		restored, err := m.core.Undo()
		if err != nil {
			m.status = "nothing to undo"
			return m, nil
		}
		switch restored.Kind {
		case "project":
			m.status = "restored project " + restored.Slug
		default:
			m.status = "restored task in " + restored.Slug
		}
		return m.reloadActive(), nil

	case key.Matches(msg, keys.Delete):
		if len(m.tasks) == 0 {
			return m, nil
		}
		t := m.tasks[m.cursor]
		if _, err := m.core.RemoveTask(t); err != nil {
			m.err = err
			return m, nil
		}
		m.err = nil
		m.status = "moved to trash · u to undo"
		m.tasks = append(m.tasks[:m.cursor], m.tasks[m.cursor+1:]...)
		if m.cursor >= len(m.tasks) {
			m.cursor = max(0, len(m.tasks)-1)
		}
		m.followCursor()
		if m.searchQuery != "" {
			m = m.runSearch()
		}
		return m, nil

	case key.Matches(msg, keys.Yank):
		if len(m.tasks) == 0 {
			return m, nil
		}
		id := m.tasks[m.cursor].ID
		m.status = "copied id " + id
		return m, tea.SetClipboard(id)

	case key.Matches(msg, keys.YankPath):
		if len(m.tasks) == 0 {
			return m, nil
		}
		path := m.tasks[m.cursor].Path
		m.status = "copied path"
		return m, tea.SetClipboard(path)

	case key.Matches(msg, keys.Switch):
		cmd, ok := m.openProjectSwitcher("")
		if !ok {
			return m, nil
		}
		return m, cmd

	case key.Matches(msg, keys.Global):
		// Refresh the registry so a project that appeared since launch
		// (via the projects.json watch or otherwise) is counted.
		_ = m.core.ReloadRegistry()
		if len(m.core.Registry().Slugs()) <= 1 {
			m.status = "only one project — nothing to globalize"
			return m, nil
		}
		m.globalView = !m.globalView
		m.expandedID = ""
		m.expandedSlug = ""
		if err := m.refreshWatches(); err != nil {
			m.err = err
		}
		m = m.reloadActive()
		return m, nil

	case key.Matches(msg, keys.Help):
		m.mode = modeHelp
		m.relayout()
		return m, nil

	case key.Matches(msg, keys.Search):
		m.mode = modeSearch
		m.input.Reset()
		m.input.Placeholder = "filter"
		m.input.SetValue(m.searchQuery)
		m.input.CursorEnd()
		m.input.Focus()
		m.relayout()
		return m, textinput.Blink
	}
	return m, nil
}

// updateSearch is the modeSearch dispatcher: keystrokes drive a live
// FTS5 filter, enter commits (filter stays applied in list mode),
// esc clears the filter and exits.
func (m Model) updateSearch(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		return m, tea.Quit
	}
	if msg.Code == tea.KeyEsc {
		m.searchQuery = ""
		m.exitInput()
		// exitInput resets the placeholder; restore the capture default.
		m.input.Placeholder = "task description"
		return m.reloadActive(), nil
	}
	if isEnter(msg) {
		m.exitInput()
		m.input.Placeholder = "task description"
		return m, nil
	}
	var cmd tea.Cmd
	prev := m.input.Value()
	m.input, cmd = m.input.Update(msg)
	if m.input.Value() != prev {
		m.searchQuery = m.input.Value()
		m = m.runSearch()
	}
	return m, cmd
}

func (m Model) updateInput(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.mode == modeCapture {
		return m.updateCapture(msg)
	}
	switch {
	case key.Matches(msg, keys.Cancel):
		m.exitInput()
		return m, nil

	case key.Matches(msg, keys.Confirm):
		text := strings.TrimSpace(m.input.Value())
		if text == "" {
			m.exitInput()
			return m, nil
		}
		if err := m.commitEdit(text); err != nil {
			m.err = err
		}
		m.exitInput()
		if m.searchQuery != "" {
			m = m.runSearch()
		}
		return m, nil

	case msg.String() == "ctrl+c":
		return m, tea.Quit
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// updateCapture drives the multi-line capture overlay. Enter submits the
// buffer (first non-blank line = description, rest = details body);
// alt+enter / ctrl+j fall through to the textarea's InsertNewline binding
// so the user can type a heading + body before committing.
func (m Model) updateCapture(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Cancel):
		m.exitInput()
		return m, nil

	case isEnter(msg) && msg.Mod == 0:
		text := strings.TrimRight(m.capArea.Value(), " \t\n")
		if strings.TrimSpace(text) == "" {
			m.exitInput()
			return m, nil
		}
		desc, details := capture.SplitHeading(text)
		if err := m.commitCapture(desc, details); err != nil {
			m.err = err
		}
		m.exitInput()
		if m.searchQuery != "" {
			m = m.runSearch()
		}
		return m, nil

	case msg.String() == "ctrl+c":
		return m, tea.Quit
	}

	prevLines := m.capArea.LineCount()
	growCaptureForKey(&m.capArea)
	var cmd tea.Cmd
	m.capArea, cmd = m.capArea.Update(msg)
	resizeQuickCapture(&m.capArea)
	if m.capArea.LineCount() != prevLines {
		// Overlay grew or shrunk a row — re-flow the list height so the
		// textarea always sits flush above the footer.
		m.relayout()
	}
	return m, cmd
}

// beginCapture readies the multi-line capture overlay. Called from the
// list's `c` handler and from the capture-target picker's completion path.
func (m *Model) beginCapture() {
	m.capArea.Reset()
	if m.width > 0 {
		m.capArea.SetWidth(m.width)
	}
	m.capArea.SetHeight(1)
	m.capArea.Focus()
}

func (m *Model) commitCapture(desc, details string) error {
	target := m.slug
	if m.capTargetSlug != "" {
		target = m.capTargetSlug
	}
	res, err := m.core.Capture(core.CaptureInput{
		Slug:        target,
		Description: desc,
		Details:     details,
		DisplayName: m.core.Registry().Name(target),
	})
	if err != nil {
		return err
	}
	m.tasks = sortDoneFirst(append(m.tasks, res.Task), m.sortKey)
	if i := indexByIDSlug(m.tasks, res.Task.ID, res.Task.ProjectSlug, m.globalView); i >= 0 {
		m.cursor = i
	}
	m.followCursor()
	return nil
}

func (m *Model) commitEdit(desc string) error {
	t := m.tasks[m.cursor]
	if err := m.core.RenameTask(&t, desc); err != nil {
		return err
	}
	m.tasks = replaceByID(m.tasks, t)
	return nil
}

func (m *Model) exitInput() {
	m.input.Blur()
	m.input.Reset()
	m.capArea.Blur()
	m.capArea.Reset()
	m.capArea.SetHeight(1)
	m.mode = modeList
	// Always drop any pending capture target — picker cancel, capture
	// cancel, commit success, and commit failure all flow through here.
	m.capTargetSlug = ""
	m.relayout()
}
