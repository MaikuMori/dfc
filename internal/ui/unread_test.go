package ui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// newUnreadModel returns a model with the cursor on the last task and the first
// task created after the project was last seen.
func newUnreadModel(t *testing.T) Model {
	t.Helper()
	m := newPerProjectMoveModel(t)
	if err := m.core.Registry().MarkSeen("work"); err != nil {
		t.Fatal(err)
	}
	m.tasks[0].Created = time.Now().Add(time.Hour)
	return m
}

func TestUnreadOnlyAfterLastSeen(t *testing.T) {
	m := newUnreadModel(t)
	if !m.isUnread(m.tasks[0]) {
		t.Error("task created after last_seen should be unread")
	}
	if m.isUnread(m.tasks[1]) {
		t.Error("task created before last_seen should be read")
	}
}

func TestUnreadNeverSeenProjectHasNoUnread(t *testing.T) {
	m := newPerProjectMoveModel(t)
	m.tasks[0].Created = time.Now().Add(time.Hour)
	if m.isUnread(m.tasks[0]) {
		t.Error("a project with no last_seen should show nothing as unread")
	}
}

func TestUnreadClearedWhenCursorLands(t *testing.T) {
	m := newUnreadModel(t)
	m.cursor = 1
	next, _ := m.Update(tea.KeyPressMsg{Code: 'k', Text: "k"})
	got := next.(Model)
	if got.cursor != 0 {
		t.Fatalf("cursor = %d, want 0", got.cursor)
	}
	if got.isUnread(got.tasks[0]) {
		t.Error("landing on a task should mark it read")
	}
}

func TestUnreadMarkAllKey(t *testing.T) {
	m := newUnreadModel(t)
	next, _ := m.Update(tea.KeyPressMsg{Code: 'R', Text: "R"})
	got := next.(Model)
	if got.isUnread(got.tasks[0]) {
		t.Error("R should mark every visible task read")
	}
}

func TestUnreadDoneTaskIsNotUnread(t *testing.T) {
	m := newUnreadModel(t)
	m.tasks[0].Status = "done"
	if m.isUnread(m.tasks[0]) {
		t.Error("done task should not be unread")
	}
}

func TestUnreadGlyphKeepsTextAligned(t *testing.T) {
	m := newUnreadModel(t)
	task := m.tasks[0]
	unread := ansiSGR.ReplaceAllString(renderRow(task, "", false, true, 60, nil), "")
	read := ansiSGR.ReplaceAllString(renderRow(task, "", false, false, 60, nil), "")
	if !strings.HasPrefix(unread, iconOpen+iconUnread) {
		t.Errorf("unread row = %q", unread)
	}
	if strings.Replace(unread, iconUnread, " ", 1) != read {
		t.Errorf("glyph shifted text: %q vs %q", unread, read)
	}
}
