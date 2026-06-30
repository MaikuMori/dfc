package ui

import (
	"strings"
	"testing"

	"github.com/MaikuMori/dfc/internal/storage"
)

func TestProgressBadge(t *testing.T) {
	if got := progressBadge(storage.Task{Details: "no boxes here"}); got != "" {
		t.Errorf("badge for boxless task = %q, want empty", got)
	}
	if got := progressBadge(storage.Task{Details: "- [x] a\n- [ ] b\n- [ ] c"}); got != "[1/3]" {
		t.Errorf("badge = %q, want [1/3]", got)
	}
	if got := progressBadge(storage.Task{Details: "- [x] a\n- [x] b"}); got != "[2/2]" {
		t.Errorf("badge = %q, want [2/2]", got)
	}
}

func TestCheckboxGlyphsUseAppIcons(t *testing.T) {
	if markdownStyles.Task.Ticked != iconDone+" " {
		t.Errorf("ticked glyph = %q, want %q", markdownStyles.Task.Ticked, iconDone+" ")
	}
	if markdownStyles.Task.Unticked != iconOpen+" " {
		t.Errorf("unticked glyph = %q, want %q", markdownStyles.Task.Unticked, iconOpen+" ")
	}
}

func TestDimDoneTasksEmptyCheckedIsPassthrough(t *testing.T) {
	in := iconDone + " looks done\n" + iconOpen + " open"
	if out := dimDoneTasks(in, nil); out != in {
		t.Errorf("no checked items should pass through unchanged:\n got %q\nwant %q", out, in)
	}
}

func TestDimDoneTasksMutesOnlyRealCompletedItems(t *testing.T) {
	// Trailing pad mimics glamour's word-wrap fill; continuation lines wrap
	// back to column 0 just as glamour emits them.
	in := []string{
		iconDone + " write migration   ",   // 0 checked (exact)
		iconDone + " this is long that   ", // 1 checked item, wraps...
		"wraps onto two lines   ",          // 2 ...continuation of 1
		iconOpen + " open item   ",         // 3 open
		iconDone + " verified release   ",  // 4 check glyph, NOT a checkbox
		"just a paragraph   ",              // 5 prose
	}
	checked := []string{"write migration", "this is long that wraps onto two lines"}
	out := strings.Split(dimDoneTasks(strings.Join(in, "\n"), checked), "\n")

	// Passthrough: open row, the ✓-prefixed non-item, and prose are untouched.
	if out[3] != in[3] {
		t.Errorf("open row changed: %q", out[3])
	}
	if out[4] != in[4] {
		t.Errorf("✓-prefixed non-item must not be dimmed (false positive): %q", out[4])
	}
	if out[5] != in[5] {
		t.Errorf("paragraph changed: %q", out[5])
	}
	// Dimmed: the exact item, and the wrapped item plus its continuation line.
	for _, i := range []int{0, 1, 2} {
		if out[i] == in[i] {
			t.Errorf("expected line %d dimmed, unchanged: %q", i, out[i])
		}
	}
	if !strings.Contains(out[0], "write migration") || !strings.Contains(out[2], "wraps onto two lines") {
		t.Errorf("dimming dropped item text: %q / %q", out[0], out[2])
	}
}

func TestRenderRowBadgeSupersedesDetailsDot(t *testing.T) {
	task := storage.Task{Description: "Ship it", Details: "- [x] a\n- [ ] b", Status: storage.StatusOpen}
	row := renderRow(task, "", false, 40, nil)
	if !strings.Contains(row, "[1/2]") {
		t.Errorf("row missing progress badge: %q", row)
	}
	if strings.Contains(row, iconHasDetails) {
		t.Errorf("row should drop the details dot when a badge is shown: %q", row)
	}
}

func TestRenderRowKeepsDetailsDotWithoutCheckboxes(t *testing.T) {
	task := storage.Task{Description: "Note", Details: "some prose", Status: storage.StatusOpen}
	row := renderRow(task, "", false, 40, nil)
	if !strings.Contains(row, iconHasDetails) {
		t.Errorf("row missing details dot: %q", row)
	}
}
