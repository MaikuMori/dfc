package ui

import (
	"fmt"
	"strings"

	"github.com/MaikuMori/dfc/internal/capture"
	"github.com/MaikuMori/dfc/internal/core"
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
	if !m.picker.Canceled() && m.picker.Selected() != "" {
		if extra, err := m.switchTo(m.picker.Selected()); err != nil {
			m.err = err
		} else if extra != nil {
			cmd = tea.Batch(cmd, extra)
		}
	}
	m.picker = Picker{}
	m.mode = modeList
	m.relayout()
	return m, cmd
}

// switchTo points the model at a different project. Returns a Cmd that
// (re)subscribes to the watcher when watch recovery happens — caller must
// batch it with any other Cmd it has in flight.
func (m *Model) switchTo(slug string) (tea.Cmd, error) {
	store, err := m.core.StoreFor(slug)
	if err != nil {
		return nil, err
	}
	tasks, err := store.List()
	if err != nil {
		return nil, err
	}
	if err := m.core.Registry().Touch(slug); err != nil {
		return nil, err
	}
	var cmd tea.Cmd
	if m.watcher != nil {
		// Removing a missing path errors quietly; only the Add must succeed.
		_ = m.watcher.Remove(m.store.Dir())
		if err := m.watcher.Add(store.Dir()); err != nil {
			return nil, err
		}
		if !m.watchLive {
			// Recovered from a previous fsErrorMsg — re-arm the subscription.
			cmd = waitForChange(m.watchSub)
		}
		m.watchLive = true
	}
	m.slug = slug
	m.store = store
	m.tasks = sortDoneFirst(tasks)
	m.cursor = 0
	if len(m.tasks) > 0 {
		m.cursor = len(m.tasks) - 1
	}
	m.expandedID = ""
	m.err = nil
	return cmd, nil
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
		m.tasks = sortDoneFirst(replaceByID(m.tasks, t))
		if i := indexByIDSlug(m.tasks, t.ID, t.ProjectSlug, m.globalView); i >= 0 {
			m.cursor = i
		}
		m.followCursor()
		if m.searchQuery != "" {
			m = m.runSearch()
		}

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

	case key.Matches(msg, keys.Switch):
		reg := m.core.Registry()
		items := ProjectPickerItems(reg, m.core.CountsByProject())
		if len(items) <= 1 {
			m.status = "only one project — nothing to switch to"
			return m, nil
		}
		m.picker = NewPicker("switch to:", items)
		m.picker.OnRename = reg.Rename
		m.picker.OnSetTag = reg.SetTag
		m.picker.OnDelete = func(target string) error {
			// Guard against deleting the project the model is currently
			// pointed at — leaves the user staring at a half-broken state
			// otherwise (watcher attached to a missing dir, etc.).
			if target == m.slug {
				return fmt.Errorf("can't delete the project you're viewing — switch first")
			}
			_, _, err := m.core.RemoveProject(target)
			return err
		}
		m.mode = modeSwitch
		m.relayout()
		return m, m.picker.Init()

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
		cmd, err := m.refreshWatches()
		if err != nil {
			m.err = err
		}
		m = m.reloadActive()
		if cmd != nil {
			return m, cmd
		}
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
	if msg.Code == tea.KeyEnter {
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

	case msg.Code == tea.KeyEnter && msg.Mod == 0:
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
	m.tasks = sortDoneFirst(append(m.tasks, res.Task))
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
