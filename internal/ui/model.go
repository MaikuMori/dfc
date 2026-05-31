package ui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/MaikuMori/dfc/internal/core"
	"github.com/MaikuMori/dfc/internal/storage"
	"github.com/MaikuMori/dfc/internal/watch"
)

type mode int

const (
	modeList mode = iota
	modeCapture
	modeEdit
	modeSearch
	modeSwitch
	modeCaptureTarget // picker that chooses a project to capture into (global view only)
	modeTagEdit       // multi-select picker that edits a project's categorical tags
	modeTagFilter     // multi-select picker that drives the global-view tag filter
	modeHelp
)

const (
	headerHeight = 1
	footerHeight = 1
	inputHeight  = 1
)

// Model is the top-level bubbletea model.
type Model struct {
	core         *core.Core
	slug         string
	store        *storage.Store
	tasks        []storage.Task
	cursor       int
	mode         mode
	input        textinput.Model
	capArea      textarea.Model // multi-line buffer used only in modeCapture
	viewport     viewport.Model
	picker       Picker
	width        int
	height       int
	err          error
	status       string // transient one-line hint, cleared on next list-mode key
	expandedID   string // task id whose details are inline-expanded; "" = none
	expandedSlug string // project slug of the expanded task (collision-safe in global)
	watcher      *watch.Watcher
	watchSub     <-chan watch.Event

	globalView    bool
	capTargetSlug string         // set transiently when capturing into a picker-chosen project; "" = current store
	lastBody      string         // last body string handed to viewport.SetContent — used to skip redundant re-splits
	searchQuery   string         // active filter; "" = no filter
	searchBase    []storage.Task // no-index fallback corpus, snapshotted on search entry
	sortKey       sortKey
	tagFilter     []string       // session-only categorical-tag filter, applied in global view
	tagFilterBase []storage.Task // pre-filter merged list, cached while the tag-filter picker is open
	tagEditSlug   string         // set transiently while modeTagEdit is running
}

// New constructs a Model bound to the given Core, project slug, store
// and initial tasks. The watcher (optional) should already be Add()'d
// to the project directory; the model takes responsibility for
// swapping the watch when the user switches projects.
func New(cr *core.Core, slug string, store *storage.Store, initial []storage.Task, watcher *watch.Watcher) Model {
	ti := textinput.New()
	ti.Prompt = "> "
	ti.CharLimit = 0
	ti.Placeholder = "task description"
	tiStyles := textinput.DefaultStyles(hasDarkBG)
	tiStyles.Focused.Prompt = stylePrompt
	tiStyles.Blurred.Prompt = stylePrompt
	ti.SetStyles(tiStyles)

	tasks := sortDoneFirst(initial, sortByModified)
	cursor := 0
	if len(tasks) > 0 {
		// Default cursor to the newest task so the eye lands where new ones
		// appear and where the user is most likely acting.
		cursor = len(tasks) - 1
	}
	return Model{
		core:     cr,
		slug:     slug,
		store:    store,
		tasks:    tasks,
		cursor:   cursor,
		mode:     modeList,
		input:    ti,
		capArea:  newCaptureArea("task description"),
		viewport: viewport.New(),
		watcher:  watcher,
		watchSub: subscribeIfLive(watcher),
	}
}

// subscribeIfLive returns the watcher's event channel, or nil when no
// watcher is attached. Keeps the New() constructor terse.
func subscribeIfLive(w *watch.Watcher) <-chan watch.Event {
	if w == nil {
		return nil
	}
	return w.Subscribe()
}

func (m Model) Init() tea.Cmd {
	return waitForChange(m.watchSub)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.relayout()
		return m, nil

	case editorFinishedMsg:
		if msg.err != nil {
			m.err = msg.err
		}
		return m.reloadActive(), nil

	case fsChangedMsg:
		prevSlugs := slugSetSnapshot(m.core.Registry())
		_ = m.core.ReloadRegistry()
		// If a project appeared/disappeared since the last event and we're
		// in global view, extend / contract the watch set.
		if m.globalView && !equalStringSets(prevSlugs, slugSetSnapshot(m.core.Registry())) {
			if err := m.refreshWatches(); err != nil {
				m.err = err
			}
		}
		if m.searchBase != nil {
			if m.globalView {
				m.searchBase = m.reloadAll().tasks
			} else {
				m.searchBase = m.reload().tasks
			}
		}
		m = m.reloadAfterFS()
		if m.mode == modeTagFilter {
			m.tagFilterBase, _ = m.core.ListAll()
		}
		// Keep the search index in step with whatever the filesystem
		// changed underneath us, off the Update goroutine so the disk
		// walk plus SQLite writes never stall a keypress. Best-effort —
		// `dfc search --reindex` is the safety net.
		return m, tea.Batch(waitForChange(m.watchSub), ensureFreshCmd(m.core))

	case fsErrorMsg:
		m.err = msg.err
		return m, nil

	case tea.PasteMsg, tea.PasteStartMsg, tea.PasteEndMsg:
		// Bracketed-paste lands here as its own message kind, not as a key
		// press — forward it to whichever input widget owns the focus so
		// pasted text gets inserted (textarea/textinput handle PasteMsg
		// natively). In list mode / pickers / help there's nowhere sensible
		// to land it, so we drop it.
		switch m.mode {
		case modeCapture:
			prevLines := m.capArea.LineCount()
			growCaptureForKey(&m.capArea)
			var cmd tea.Cmd
			m.capArea, cmd = m.capArea.Update(msg)
			resizeQuickCapture(&m.capArea)
			if m.capArea.LineCount() != prevLines {
				m.relayout()
			}
			return m, cmd
		case modeEdit, modeSearch:
			var cmd tea.Cmd
			prev := m.input.Value()
			m.input, cmd = m.input.Update(msg)
			if m.mode == modeSearch && m.input.Value() != prev {
				m.searchQuery = m.input.Value()
				m = m.runSearch()
			}
			return m, cmd
		}
		return m, nil

	case tea.KeyPressMsg:
		switch m.mode {
		case modeList:
			return m.updateList(msg)
		case modeCapture, modeEdit:
			return m.updateInput(msg)
		case modeSearch:
			return m.updateSearch(msg)
		case modeSwitch:
			return m.updateSwitch(msg)
		case modeCaptureTarget:
			return m.updateCaptureTarget(msg)
		case modeTagEdit:
			return m.updateTagEdit(msg)
		case modeTagFilter:
			return m.updateTagFilter(msg)
		case modeHelp:
			return m.updateHelp(msg)
		}
	}
	return m, nil
}

// View renders the whole frame: header, body (list or picker), optional
// input, footer. Bubble Tea v2's View returns a tea.View, which carries
// rendered content alongside per-frame terminal modes — AltScreen is set
// on every frame so the TUI lives in its own screen buffer.
func (m Model) View() tea.View {
	var v tea.View
	v.AltScreen = true
	if m.width == 0 || m.height == 0 {
		return v
	}

	var b strings.Builder
	b.WriteString(m.header())
	b.WriteByte('\n')

	if m.isPickerMode() {
		pickerView := m.picker.View()
		var hintText string
		switch {
		case m.picker.ConfirmingDelete():
			// The picker prints "press y to confirm · any other key
			// cancels" already; suppress the outer mode chords which
			// would otherwise look like they still do their thing.
			hintText = ""
		case m.picker.EditingInput():
			// The picker is showing its own "rename · …" / "new tag · …"
			// preamble — the outer footer just echoes save/cancel.
			hintText = joinBindings(keys.InputHints())
		case m.mode == modeSwitch:
			hintText = joinBindings(keys.SwitchPickerHints())
		case m.mode == modeCaptureTarget:
			hintText = joinBindings(keys.CaptureTargetHints())
		case m.mode == modeTagEdit:
			hintText = joinBindings(keys.TagEditHints())
		case m.mode == modeTagFilter:
			hintText = joinBindings(keys.TagFilterHints())
		}
		hint := styleHint.Render(hintText)
		block := pickerView + "\n" + hint
		blockLines := strings.Count(block, "\n") + 1
		pad := m.height - headerHeight - blockLines
		if pad > 0 {
			b.WriteString(strings.Repeat("\n", pad))
		}
		b.WriteString(block)
		v.SetContent(b.String())
		return v
	}

	if m.mode == modeHelp {
		body := renderHelp(m.width)
		hint := styleHint.Render("any key to close")
		bodyLines := strings.Count(body, "\n") + 1
		pad := m.height - headerHeight - bodyLines - 1 // -1 for hint row
		if pad > 0 {
			b.WriteString(strings.Repeat("\n", pad))
		}
		b.WriteString(body)
		b.WriteByte('\n')
		b.WriteString(hint)
		v.SetContent(b.String())
		return v
	}

	// The viewport content is kept in sync by every Update path that mutates
	// the list (relayout on resize, reload after fs/editor events, cursor
	// moves via followCursor, in-block scroll via scrollWithinCursorRow).
	// Re-rendering here would cause a redundant SetContent every frame and
	// shows up as micro-flicker when scrolling long expanded tasks.
	b.WriteString(m.viewport.View())
	b.WriteByte('\n')

	switch m.mode {
	case modeCapture:
		b.WriteString(m.capArea.View())
		b.WriteByte('\n')
	case modeEdit, modeSearch:
		b.WriteString(m.input.View())
		b.WriteByte('\n')
	}

	b.WriteString(m.footer())
	v.SetContent(b.String())
	return v
}

func (m Model) header() string {
	if m.globalView {
		// While the tag-filter picker is open, project a live count
		// against the picker's current selection so toggling rows updates
		// the visible total instead of waiting for the commit.
		filter, count := m.tagFilter, len(m.tasks)
		if m.mode == modeTagFilter {
			filter = m.picker.Selection()
			count = m.countTasksMatchingTagFilter(filter)
		}
		label := fmt.Sprintf("all (%d)", count)
		if len(filter) > 0 {
			label += "  " + styleHint.Render("filter: ["+strings.Join(filter, ", ")+"]")
		}
		return styleHeader.Render(label)
	}
	name := m.slug
	if m.core != nil {
		name = m.core.Registry().Name(m.slug)
	}
	return styleHeader.Render(name)
}

// countTasksMatchingTagFilter returns how many tasks from the cached
// pre-filter list (tagFilterBase, captured when the picker opened) would
// survive the given tag filter. Used for live previewing the post-commit
// count while the tag-filter picker is open, without re-reading every
// project from disk on each keystroke.
func (m Model) countTasksMatchingTagFilter(filter []string) int {
	all := m.tagFilterBase
	if len(filter) == 0 {
		return len(all)
	}
	reg := m.core.Registry()
	tf := newTagFilterSet(filter)
	n := 0
	for _, t := range all {
		if tf.matches(reg, t.ProjectSlug) {
			n++
		}
	}
	return n
}

func (m Model) footer() string {
	if m.err != nil {
		return styleError.Render(m.err.Error())
	}
	if m.status != "" {
		return styleHint.Render(m.status)
	}
	switch {
	case m.mode == modeSearch:
		return styleHint.Render(joinBindings(keys.SearchHints()))
	case m.mode == modeCapture:
		return styleHint.Render(joinBindings(keys.CaptureHints()))
	case m.mode != modeList:
		return styleHint.Render(joinBindings(keys.InputHints()))
	case m.searchQuery != "":
		return styleHint.Render(fmt.Sprintf("filter: %q · %s", m.searchQuery, joinBindings(keys.FilterHints())))
	default:
		return renderShortHelp(m.width)
	}
}
