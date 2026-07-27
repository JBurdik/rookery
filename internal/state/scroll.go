package state

import (
	"strconv"
	"strings"
)

// Scroll / copy mode.
//
// A pane's grid keeps the live screen and a plain-text transcript of
// everything it printed. Agents can already read that transcript over the
// API (`rook pane read --scrollback`); this is the same thing for the human:
// a viewport onto the transcript, drawn in place of the live screen, with a
// line-granular selection that can be yanked to the system clipboard.
//
// ponytail: the transcript is plain text, so scrollback is drawn without
// colour and long lines are cut at the pane's width rather than wrapped. The
// live screen is the styled one and is a keypress away. Selection is by whole
// lines — character selection needs a cell-accurate history, which the
// emulator does not keep. The copied text is the untruncated line, so cutting
// the display costs nothing on the way out.
type scrollView struct {
	active bool
	// cursor is the transcript line the cursor sits on, top the first line
	// drawn. Both are indices into the transcript, which only grows at the
	// end — until it reaches its cap and the oldest lines fall off, at which
	// point everything shifts by one. Every use clamps, so the worst case is
	// the view sliding a line rather than pointing off the end.
	cursor int
	top    int
	// anchor is where a selection started, or -1 when nothing is selected.
	anchor int
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

// enterScroll puts a pane into scroll mode with the cursor on the last line,
// which is where the eye already is.
func (l *Loop) enterScroll(pane *Pane) {
	if pane == nil || pane.View.active {
		return
	}
	lines := len(pane.Grid.ScrollbackLines())
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

	lines := len(pane.Grid.ScrollbackLines())
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
	lines := len(pane.Grid.ScrollbackLines())
	_, rows := pane.Grid.Size()
	if lines == 0 {
		return
	}
	if where == "top" {
		pane.View.cursor = 0
	} else {
		pane.View.cursor = lines - 1
	}
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
	} else {
		pane.View.anchor = pane.View.cursor
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
	lines := pane.Grid.ScrollbackLines()
	from, to := pane.View.selection()
	from, to = min(max(from, 0), max(len(lines)-1, 0)), min(max(to, 0), max(len(lines)-1, 0))
	if len(lines) == 0 {
		return ""
	}
	text := strings.Join(lines[from:to+1], "\n")
	l.exitScroll(pane)
	return text
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
	}
}

// scrollSuffix is what a pane's header says while it is scrolled back: how
// far from the live screen you are, because a screen that has stopped
// updating otherwise looks like a hung program.
func scrollSuffix(p *Pane) string {
	if !p.View.active {
		return ""
	}
	behind := max(len(p.Grid.ScrollbackLines())-1-p.View.cursor, 0)
	if p.View.anchor >= 0 {
		from, to := p.View.selection()
		return " [copy " + strconv.Itoa(to-from+1) + " lines]"
	}
	return " [copy -" + strconv.Itoa(behind) + "]"
}

// scrollActive reports whether the focused pane is in scroll mode, which is
// what tells the client to route keys to its copy-mode bindings.
func (l *Loop) scrollActive() bool {
	p := l.app.panes[l.app.focusedPane()]
	return p != nil && p.View.active
}
