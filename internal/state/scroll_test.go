package state

import (
	"strings"
	"testing"

	"github.com/jirkab/rookery/internal/termgrid"
)

// scrollPane builds a pane with n lines of transcript and a 3-row screen, so
// the window is smaller than the history — the only case worth testing.
func scrollPane(t *testing.T, n int) (*Loop, *Pane) {
	t.Helper()
	grid := termgrid.New(20, 3)
	for i := range n {
		grid.Write([]byte("line" + string(rune('0'+i%10)) + "\r\n"))
	}
	pane := &Pane{ID: "w1:p1", Grid: grid, View: scrollView{anchor: -1}}
	l := &Loop{app: newApp("scroll-test")}
	l.app.panes[pane.ID] = pane
	return l, pane
}

func TestEnterScrollStartsAtTheEnd(t *testing.T) {
	l, p := scrollPane(t, 10)
	l.enterScroll(p)

	if !p.View.active {
		t.Fatal("scroll mode did not turn on")
	}
	if p.View.cursor != 9 {
		t.Errorf("cursor = %d, want the last line (9)", p.View.cursor)
	}
	if p.View.top != 7 {
		t.Errorf("top = %d, want the window's worth from the end (7)", p.View.top)
	}
	if p.View.anchor != -1 {
		t.Errorf("anchor = %d, want no selection", p.View.anchor)
	}
}

func TestScrollBySlidesTheWindow(t *testing.T) {
	l, p := scrollPane(t, 10)
	l.enterScroll(p)
	l.scrollBy(p, -5)

	if p.View.cursor != 4 {
		t.Errorf("cursor = %d, want 4", p.View.cursor)
	}
	// The cursor moved above the window, so the window follows it up.
	if p.View.top != 4 {
		t.Errorf("top = %d, want the cursor line (4)", p.View.top)
	}

	// Past the start clamps rather than going negative.
	l.scrollBy(p, -99)
	if p.View.cursor != 0 || p.View.top != 0 {
		t.Errorf("cursor/top = %d/%d, want 0/0 at the top", p.View.cursor, p.View.top)
	}
}

func TestScrollingPastTheBottomReturnsToLive(t *testing.T) {
	l, p := scrollPane(t, 10)
	l.enterScroll(p)
	l.scrollBy(p, -2)
	l.scrollBy(p, 99)

	if p.View.active {
		t.Error("scrolling past the last line should leave scroll mode")
	}
}

func TestSelectionHoldsThePaneInScrollMode(t *testing.T) {
	l, p := scrollPane(t, 10)
	l.enterScroll(p)
	l.scrollBy(p, -4) // line 5
	l.toggleSelect(p)
	l.scrollBy(p, 99) // would normally exit

	if !p.View.active {
		t.Fatal("a running selection must not be dropped by scrolling to the end")
	}

	text := l.copySelection(p)
	lines := strings.Split(text, "\n")
	if len(lines) != 5 {
		t.Fatalf("copied %d lines (%q), want 5 (line5..line9)", len(lines), text)
	}
	if lines[0] != "line5" || lines[4] != "line9" {
		t.Errorf("copied %q, want line5..line9", text)
	}
	if p.View.active {
		t.Error("copying should return the pane to the live screen")
	}
}

func TestCopyWithoutSelectionTakesTheCursorLine(t *testing.T) {
	l, p := scrollPane(t, 10)
	l.enterScroll(p)
	l.scrollBy(p, -3)

	if got := l.copySelection(p); got != "line6" {
		t.Errorf("copied %q, want the cursor line \"line6\"", got)
	}
}

func TestScrollSuffixReportsDistanceAndSelection(t *testing.T) {
	l, p := scrollPane(t, 10)
	if got := scrollSuffix(p); got != "" {
		t.Errorf("a live pane has no suffix, got %q", got)
	}

	l.enterScroll(p)
	l.scrollBy(p, -4)
	if got := scrollSuffix(p); got != " [copy -4]" {
		t.Errorf("suffix = %q, want \" [copy -4]\"", got)
	}

	l.toggleSelect(p)
	l.scrollBy(p, -1)
	if got := scrollSuffix(p); got != " [copy 2 lines]" {
		t.Errorf("suffix = %q, want \" [copy 2 lines]\"", got)
	}
}

func TestDrawScrollViewPaintsTheWindowAndTheSelection(t *testing.T) {
	l, p := scrollPane(t, 10)
	l.enterScroll(p)
	l.scrollBy(p, -4) // cursor on line5, window shows line3..line5
	l.toggleSelect(p)
	l.scrollBy(p, -1) // selection is line4..line5

	canvas := termgrid.NewCanvas(20, 3)
	l.drawScrollView(canvas, Rect{W: 20, H: 3}, p)

	row := func(y int) string {
		var b []rune
		for x := range 5 {
			b = append(b, canvas.At(x, y).Char)
		}
		return string(b)
	}
	// The cursor sits on line4 with the anchor on line5, and the window
	// followed the cursor up: line4, line5, line6.
	if got := row(0); got != "line4" {
		t.Errorf("top row = %q, want line4", got)
	}
	if got := row(2); got != "line6" {
		t.Errorf("bottom row = %q, want line6", got)
	}
	// Selected lines are banded, unselected ones are not.
	if canvas.At(0, 2).Mode&termgrid.ModeReverse != 0 {
		t.Error("line6 is outside the selection and should not be banded")
	}
	for _, y := range []int{0, 1} {
		// The band runs past the end of the text, not just under it.
		if canvas.At(19, y).Mode&termgrid.ModeReverse == 0 {
			t.Errorf("row %d should be banded to the full width", y)
		}
	}
}

func TestWheelDoesNotScrollALivePane(t *testing.T) {
	l, p := scrollPane(t, 10)
	// Wheeling down on a pane that is already live has nothing to show.
	l.scrollBy(p, wheelLines)
	if p.View.active {
		t.Error("wheeling down on a live pane should do nothing")
	}
	l.scrollBy(p, -wheelLines)
	if !p.View.active || p.View.cursor != 6 {
		t.Errorf("wheeling up should enter scroll mode at line 6, got active=%v cursor=%d",
			p.View.active, p.View.cursor)
	}
}
