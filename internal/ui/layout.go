package ui

import (
	"github.com/MaikuMori/dfc/internal/storage"
)

// refreshWatches computes the desired watch set (registry file + current
// project dir + all other project dirs in global view), then applies the
// diff against the watcher's current set.
func (m *Model) refreshWatches() error {
	if m.watcher == nil {
		return nil
	}
	desired := map[string]bool{}
	if regPath := storage.RegistryPath(); regPath != "" {
		desired[regPath] = true
	}
	if m.store != nil {
		desired[m.store.Dir()] = true
	}
	if m.globalView {
		for _, slug := range m.core.Registry().Slugs() {
			if slug == m.slug {
				continue
			}
			if !storage.ProjectDirExists(slug) {
				continue
			}
			s, err := m.core.StoreFor(slug)
			if err != nil {
				continue
			}
			desired[s.Dir()] = true
		}
	}
	current := map[string]bool{}
	for _, p := range m.watcher.WatchList() {
		current[p] = true
	}
	for p := range current {
		if !desired[p] {
			_ = m.watcher.Remove(p)
		}
	}
	var firstErr error
	for p := range desired {
		if current[p] {
			continue
		}
		if err := m.watcher.Add(p); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// setViewportBody updates the viewport's content only when it actually
// differs from the previous frame. Each `SetContent` re-splits the body
// into a fresh lines slice inside the viewport; skipping no-op resets
// removes a measurable chunk of work during in-block scrolls where the
// rendered string is identical frame-to-frame and only YOffset moves.
func (m *Model) setViewportBody(body string) {
	if body == m.lastBody {
		return
	}
	m.viewport.SetContent(body)
	m.lastBody = body
}

// scrollViewport moves YOffset by delta visual lines, clamped against the
// rendered content's total height. Useful for reading inside an expanded
// task that's taller than the viewport.
func (m *Model) scrollViewport(delta int) {
	if m.viewport.Height() == 0 || len(m.tasks) == 0 || delta == 0 {
		return
	}
	body, heights := renderList(*m, m.width, m.viewport.Height())
	// Reseat the viewport's content so its internal maxOffset reflects the
	// current rendering (e.g. an expanded task that grew). Without this,
	// SetYOffset would clamp against stale content from the previous frame.
	m.setViewportBody(body)
	total := sum(heights)
	if total <= m.viewport.Height() {
		return
	}
	maxOffset := total - m.viewport.Height()
	next := m.viewport.YOffset() + delta
	if next < 0 {
		next = 0
	}
	if next > maxOffset {
		next = maxOffset
	}
	m.viewport.SetYOffset(next)
}

func (m *Model) moveCursor(delta int) {
	if len(m.tasks) == 0 || delta == 0 {
		return
	}
	// When the cursor row is taller than the viewport (an expanded task with
	// a long body), j/k first scroll within the block. Only when the user
	// has read past the visible bottom (or scrolled back above the top) do
	// they advance to the next/previous task.
	if m.scrollWithinCursorRow(delta) {
		return
	}
	prev := m.cursor
	m.cursor = max(0, min(m.cursor+delta, len(m.tasks)-1))
	if m.cursor == prev {
		// Already at the edge (first/last task). Don't re-anchor the
		// viewport — that would yank a long expansion back to its top.
		return
	}
	m.followCursor()
}

// scrollWithinCursorRow nudges the viewport by one visual line when the
// cursor task overflows the viewport and we haven't yet revealed the edge
// the user is moving toward. Returns true if it handled the keypress; false
// if the caller should fall through to the normal cursor move.
//
// Only sets the viewport content when it actually scrolls — otherwise the
// caller's `followCursor` would `SetContent` a second time the same frame,
// which costs a redundant line-split inside the viewport and can show as
// flicker on long expanded tasks.
func (m *Model) scrollWithinCursorRow(delta int) bool {
	if m.viewport.Height() == 0 {
		return false
	}
	body, heights := renderList(*m, m.width, m.viewport.Height())
	if m.cursor < 0 || m.cursor >= len(heights) {
		return false
	}
	cursorHeight := heights[m.cursor]
	if cursorHeight <= m.viewport.Height() {
		return false
	}
	cursorTop := 0
	for i := 0; i < m.cursor; i++ {
		cursorTop += heights[i]
	}
	cursorBottom := cursorTop + cursorHeight - 1
	top := m.viewport.YOffset()
	bottom := top + m.viewport.Height() - 1

	if delta > 0 && cursorBottom > bottom {
		next := top + 1
		if maxTop := cursorBottom - m.viewport.Height() + 1; next > maxTop {
			next = maxTop
		}
		if next != top {
			m.setViewportBody(body)
			m.viewport.SetYOffset(next)
			return true
		}
	}
	if delta < 0 && cursorTop < top {
		next := top - 1
		if next < cursorTop {
			next = cursorTop
		}
		if next != top {
			m.setViewportBody(body)
			m.viewport.SetYOffset(next)
			return true
		}
	}
	return false
}

func (m *Model) followCursor() {
	if m.viewport.Height() == 0 || len(m.tasks) == 0 {
		return
	}
	body, heights := renderList(*m, m.width, m.viewport.Height())
	// Same reason as scrollViewport: reseat the viewport's content so its
	// internal maxOffset matches what we're about to SetYOffset against.
	m.setViewportBody(body)
	total := sum(heights)
	// When everything fits, padding inside the rendered content anchors the
	// list to the bottom and the viewport doesn't need to scroll.
	if total <= m.viewport.Height() {
		m.viewport.SetYOffset(0)
		return
	}
	// Clamp a stale YOffset against the new content bounds. Without this,
	// a viewport that briefly shrank (e.g. while a picker was open) and
	// then regrew can leave the visible window scrolled past the end of
	// content — the cursor row sticks at the visual top with empty space
	// below it.
	if maxOffset := total - m.viewport.Height(); m.viewport.YOffset() > maxOffset {
		m.viewport.SetYOffset(maxOffset)
	}
	// Visual line at which the cursor task starts.
	cursorTop := 0
	for i := 0; i < m.cursor; i++ {
		cursorTop += heights[i]
	}
	cursorHeight := heights[m.cursor]
	cursorBottom := cursorTop + cursorHeight - 1

	top := m.viewport.YOffset()
	bottom := top + m.viewport.Height() - 1

	// When the cursor row is taller than the viewport (typically a long
	// expanded task) we can't show all of it at once. Anchor at the top so
	// the user starts reading from the heading, and only re-anchor when the
	// top has scrolled out of view — PgDn/PgUp let them scroll within the
	// block.
	if cursorHeight > m.viewport.Height() {
		if cursorTop < top || cursorTop > bottom {
			m.viewport.SetYOffset(cursorTop)
		}
		return
	}

	switch {
	case cursorTop < top:
		m.viewport.SetYOffset(cursorTop)
	case cursorBottom > bottom:
		// Scroll just enough to bring the cursor's last visual line into view.
		m.viewport.SetYOffset(cursorBottom - m.viewport.Height() + 1)
	}
}

func (m *Model) relayout() {
	if m.width == 0 || m.height == 0 {
		return
	}
	listHeight := m.height - headerHeight - footerHeight
	switch m.mode {
	case modeCapture:
		// Always size to current screen width so wrap math is accurate.
		m.capArea.SetWidth(m.width)
		listHeight -= captureAreaHeight(&m.capArea)
	case modeEdit, modeSearch:
		listHeight -= inputHeight
		if w := m.width - 2; w > 0 {
			m.input.SetWidth(w)
		}
	case modeSwitch, modeCaptureTarget, modeMoveTarget, modeTagEdit, modeHelp:
		// Picker or help takes over the body; viewport doesn't need to be
		// sized to anything sensible.
		listHeight = 0
		if m.mode != modeHelp {
			m.picker.SetWidth(m.width)
		}
	}
	if listHeight < 1 {
		listHeight = 1
	}
	m.viewport.SetWidth(m.width)
	m.viewport.SetHeight(listHeight)
	if !m.isPickerMode() {
		body, _ := renderList(*m, m.width, listHeight)
		m.setViewportBody(body)
	}
	m.followCursor()
}

// isPickerMode reports whether m.picker is overlaying the body — so the task
// list shouldn't render under it and the picker should be width-sized to the
// screen. modeHelp also takes over the body but has no picker to size.
func (m Model) isPickerMode() bool {
	switch m.mode {
	case modeSwitch, modeCaptureTarget, modeMoveTarget, modeTagEdit:
		return true
	}
	return false
}

// replaceByID returns a new slice where the task with id == t.ID is replaced
// by t. If no match is found, t is appended.
func replaceByID(tasks []storage.Task, t storage.Task) []storage.Task {
	for i, x := range tasks {
		if x.ID == t.ID {
			out := make([]storage.Task, len(tasks))
			copy(out, tasks)
			out[i] = t
			return out
		}
	}
	return append(tasks, t)
}
