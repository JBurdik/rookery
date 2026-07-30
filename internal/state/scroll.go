package state

import (
	"strconv"
	"strings"

	"github.com/jirkab/rookery/internal/termgrid"
)

// Scroll / copy mode.
//
// A pane's grid retains physical terminal rows as styled cells. The legacy
// transcript remains available to the API; this viewport instead uses those
// cells so scrollback looks like the terminal did and selections have stable
// row/column coordinates, including soft wraps.
type scrollView struct {
	active bool
	// cursor is the transcript line the cursor sits on, top the first line
	// drawn. Both are indices into the transcript, which only grows at the
	// end — until it reaches its cap and the oldest lines fall off, at which
	// point everything shifts by one. Every use clamps, so the worst case is
	// the view sliding a line rather than pointing off the end.
	cursor int
	column int
	top    int
	// anchor is where a selection started, or -1 when nothing is selected.
	anchor       int
	anchorColumn int
}

// wheelLines is how far one notch of the wheel moves, matching what a
// terminal emulator does with its own scrollback.
const wheelLines = 3

// selection returns the inclusive line range currently selected: the cursor
// line alone when no anchor has been dropped.
func (v scrollView) selection() (from, to int) {
	from, to = v.cursor, v.cursor
	if v.anchor >= 0 {
		from, to = min(v.anchor, v.cursor), max(v.anchor, v.cursor)
	}
	return from, to
}

func (v scrollView) selected(line, column int) bool {
	if v.anchor < 0 {
		return false
	}
	startLine, startColumn, endLine, endColumn := v.anchor, v.anchorColumn, v.cursor, v.column
	if startLine > endLine || startLine == endLine && startColumn > endColumn {
		startLine, startColumn, endLine, endColumn = endLine, endColumn, startLine, startColumn
	}
	if line < startLine || line > endLine {
		return false
	}
	if startLine == endLine {
		return column >= startColumn && column <= endColumn
	}
	if line == startLine {
		return column >= startColumn
	}
	if line == endLine {
		return column <= endColumn
	}
	return true
}

// enterScroll puts a pane into scroll mode with the cursor on the last line,
// which is where the eye already is.
func (l *Loop) enterScroll(pane *Pane) {
	if pane == nil || pane.View.active {
		return
	}
	lines := len(pane.Grid.ScrollbackCells())
	_, rows := pane.Grid.Size()
	pane.View = scrollView{
		active: true,
		cursor: max(lines-1, 0),
		top:    max(lines-rows, 0),
		anchor: -1,
	}
	l.app.dirty = true
	l.broadcastState()
}

// exitScroll returns a pane to the live screen.
func (l *Loop) exitScroll(pane *Pane) {
	if pane == nil || !pane.View.active {
		return
	}
	pane.View = scrollView{}
	l.app.dirty = true
	l.broadcastState()
}

// scrollBy moves the cursor by delta lines, entering scroll mode if it isn't
// already on, and leaving it again when the cursor walks back off the bottom
// — scrolling down past the end means "I am done looking at history".
func (l *Loop) scrollBy(pane *Pane, delta int) {
	if pane == nil {
		return
	}
	if !pane.View.active {
		if delta >= 0 {
			return // already live; there is nothing below the bottom
		}
		l.enterScroll(pane)
	}

	lines := len(pane.Grid.ScrollbackCells())
	_, rows := pane.Grid.Size()
	if lines == 0 {
		l.exitScroll(pane)
		return
	}

	next := pane.View.cursor + delta
	if next > lines-1 && pane.View.anchor < 0 {
		// Off the bottom with nothing selected: back to the live screen.
		l.exitScroll(pane)
		return
	}
	pane.View.cursor = min(max(next, 0), lines-1)
	pane.View.column = min(pane.View.column, scrollLineWidth(pane, pane.View.cursor)-1)
	pane.View.top = clampTop(pane.View.top, pane.View.cursor, lines, rows)
	l.app.dirty = true
}

// scrollTo jumps the cursor to the top or bottom of the transcript.
func (l *Loop) scrollTo(pane *Pane, where string) {
	if pane == nil {
		return
	}
	if !pane.View.active {
		l.enterScroll(pane)
	}
	lines := len(pane.Grid.ScrollbackCells())
	_, rows := pane.Grid.Size()
	if lines == 0 {
		return
	}
	if where == "top" {
		pane.View.cursor = 0
	} else {
		pane.View.cursor = lines - 1
	}
	pane.View.column = min(pane.View.column, scrollLineWidth(pane, pane.View.cursor)-1)
	pane.View.top = clampTop(pane.View.top, pane.View.cursor, lines, rows)
	l.app.dirty = true
}

// toggleSelect starts a selection at the cursor, or clears the one already
// running.
func (l *Loop) toggleSelect(pane *Pane) {
	if pane == nil || !pane.View.active {
		return
	}
	if pane.View.anchor >= 0 {
		pane.View.anchor = -1
		pane.View.anchorColumn = 0
	} else {
		pane.View.anchor = pane.View.cursor
		pane.View.anchorColumn = pane.View.column
	}
	l.app.dirty = true
	l.broadcastState()
}

// copySelection returns the selected lines and leaves scroll mode. The client
// puts the text on the clipboard: it is the process that owns a terminal, and
// OSC 52 is how you reach a clipboard through one — including over SSH, which
// no clipboard library manages.
func (l *Loop) copySelection(pane *Pane) string {
	if pane == nil || !pane.View.active {
		return ""
	}
	lines := pane.Grid.ScrollbackCells()
	if len(lines) == 0 {
		return ""
	}
	from, to := pane.View.selection()
	from, to = min(max(from, 0), len(lines)-1), min(max(to, 0), len(lines)-1)
	if pane.View.anchor < 0 {
		text := scrollText(lines[from].Cells, 0, len(lines[from].Cells)-1)
		l.exitScroll(pane)
		return text
	}
	text := copyCells(lines, pane.View.anchor, pane.View.anchorColumn, pane.View.cursor, pane.View.column)
	l.exitScroll(pane)
	return text
}

func scrollLineWidth(pane *Pane, line int) int {
	lines := pane.Grid.ScrollbackCells()
	if line < 0 || line >= len(lines) || len(lines[line].Cells) == 0 {
		return 1
	}
	return len(lines[line].Cells)
}

func scrollText(cells []termgrid.Cell, from, to int) string {
	if len(cells) == 0 || to < from {
		return ""
	}
	from, to = max(from, 0), min(to, len(cells)-1)
	runes := make([]rune, 0, to-from+1)
	for _, cell := range cells[from : to+1] {
		ch := cell.Char
		if ch == 0 {
			ch = ' '
		}
		runes = append(runes, ch)
	}
	return strings.TrimRight(string(runes), " ")
}

func copyCells(lines []termgrid.ScrollbackLine, startLine, startColumn, endLine, endColumn int) string {
	if startLine > endLine || startLine == endLine && startColumn > endColumn {
		startLine, startColumn, endLine, endColumn = endLine, endColumn, startLine, startColumn
	}
	startLine, endLine = max(startLine, 0), min(endLine, len(lines)-1)
	var b strings.Builder
	for line := startLine; line <= endLine; line++ {
		from, to := 0, len(lines[line].Cells)-1
		if line == startLine {
			from = startColumn
		}
		if line == endLine {
			to = endColumn
		}
		b.WriteString(scrollText(lines[line].Cells, from, to))
		if line < endLine && !lines[line].Wrapped {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// clampTop keeps the cursor line inside the drawn window, scrolling the
// window only as far as it has to.
func clampTop(top, cursor, lines, rows int) int {
	if rows < 1 {
		rows = 1
	}
	if cursor < top {
		top = cursor
	}
	if cursor >= top+rows {
		top = cursor - rows + 1
	}
	return min(max(top, 0), max(lines-1, 0))
}

// scrollCommand applies one movement from the client's copy-mode bindings.
func (l *Loop) scrollCommand(pane *Pane, what string) {
	if pane == nil {
		return
	}
	_, rows := pane.Grid.Size()
	page := max(rows-1, 1)
	switch what {
	case "up":
		l.scrollBy(pane, -1)
	case "down":
		l.scrollBy(pane, 1)
	case "page_up":
		l.scrollBy(pane, -page)
	case "page_down":
		l.scrollBy(pane, page)
	case "top", "bottom":
		l.scrollTo(pane, what)
	case "left":
		if pane.View.active {
			pane.View.column = max(pane.View.column-1, 0)
			l.app.dirty = true
		}
	case "right":
		if pane.View.active {
			pane.View.column = min(pane.View.column+1, scrollLineWidth(pane, pane.View.cursor)-1)
			l.app.dirty = true
		}
	}
}

// scrollSuffix is what a pane's header says while it is scrolled back: how
// far from the live screen you are, because a screen that has stopped
// updating otherwise looks like a hung program.
func scrollSuffix(p *Pane) string {
	if !p.View.active {
		return ""
	}
	behind := max(len(p.Grid.ScrollbackCells())-1-p.View.cursor, 0)
	if p.View.anchor >= 0 {
		return " [copy selection]"
	}
	return " [copy -" + strconv.Itoa(behind) + "]"
}

// scrollActive reports whether the focused pane is in scroll mode, which is
// what tells the client to route keys to its copy-mode bindings.
func (l *Loop) scrollActive() bool {
	p := l.app.panes[l.app.focusedPane()]
	return p != nil && p.View.active
}
