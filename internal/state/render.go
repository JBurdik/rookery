package state

import (
	"time"

	"github.com/jirkab/rookery/internal/agentstatus"

	"github.com/jirkab/rookery/internal/attachproto"
	"github.com/jirkab/rookery/internal/termgrid"
)

// Cell styling for the chrome the daemon draws around panes. Colours stay in
// the low range so the frame looks right on any theme.
// Chrome cells, coloured from the theme the attached client asked for so the
// daemon's pane headers match the client's sidebar.
func (l *Loop) dividerCell() termgrid.Cell {
	return termgrid.Cell{Char: ' ', FG: l.app.borderFG, BG: termgrid.DefaultBG}
}

// borderCell is a pane's box edge: accent for the focused pane, the border
// colour for the rest.
func (l *Loop) borderCell(focused bool) termgrid.Cell {
	if focused {
		return termgrid.Cell{Char: ' ', FG: l.app.accent, BG: termgrid.DefaultBG}
	}
	return termgrid.Cell{Char: ' ', FG: l.app.borderFG, BG: termgrid.DefaultBG}
}

func (l *Loop) headerCell(focused bool) termgrid.Cell {
	if focused {
		return termgrid.Cell{Char: ' ', FG: l.app.accent, BG: termgrid.DefaultBG, Mode: 4 /*bold*/}
	}
	return termgrid.Cell{Char: ' ', FG: l.app.headerFG, BG: termgrid.DefaultBG}
}

// titleRows is the height of the per-pane header, drawn only when more than
// one pane shares the tab: with a single pane the sidebar already says
// everything the header would, and the row is better spent on content.
const titleRows = 1

// statusGlyph is the one-character summary of an agent's state, drawn from
// the same theme the client uses for its sidebar so the symbols only have to
// be learned once. "Working" animates, and the frame comes from the wall
// clock so both sides land on the same one without exchanging a message.
func (l *Loop) statusGlyph(p *Pane) string {
	return l.app.icons.Status(string(p.agentStatus()), p.Status == "exited", l.app.spinner, time.Now())
}

// Border modes, as written in config.json.
const (
	BordersAuto   = "auto"   // a box per pane once more than one shares the tab
	BordersAlways = "always" // even a lone pane gets one
	BordersNever  = "never"  // the old single header row instead
)

// bordersOn decides whether to draw pane boxes for the current layout.
func (l *Loop) bordersOn(paneCount int) bool {
	switch l.app.borders {
	case BordersAlways:
		return true
	case BordersNever:
		return false
	default:
		// A lone pane needs no box: there is nothing to separate it from, and
		// the two columns and two rows are better spent on content.
		return paneCount > 1
	}
}

// paneContentRect is the area inside a pane's rectangle that the terminal
// itself gets. A bordered pane loses a cell on every side; an unbordered one
// with a header loses just the header row.
func paneContentRect(r Rect, withHeader, withBorder bool) Rect {
	if withBorder {
		return Rect{X: r.X + 1, Y: r.Y + 1, W: max(r.W-2, 1), H: max(r.H-2, 1)}
	}
	if !withHeader {
		return r
	}
	return Rect{X: r.X, Y: r.Y + titleRows, W: r.W, H: max(r.H-titleRows, 1)}
}

// buildFrame composites the active tab's panes onto one canvas and renders
// it. Panes are resized to fit their rectangle as a side effect: the layout
// is the authority on how big a terminal is, so this is the one place that
// pushes that size down into the emulator and the PTY.
func (l *Loop) buildFrame() attachproto.Frame {
	area := l.app.area()
	canvas := termgrid.NewCanvas(area.W, area.H)
	tab := l.app.activeTab()
	if tab == nil {
		return attachproto.Frame{Type: attachproto.TypeFrame, Cols: area.W, Rows: area.H, ANSI: canvas.RenderANSI()}
	}

	rects := l.app.rects()
	withBorder := l.bordersOn(len(rects))
	withHeader := !withBorder && len(rects) > 1

	// Dividers first, so pane content painted next always wins on overlap.
	// Bordered panes need none: their own edges already separate them, and
	// drawing both would put three lines of chrome between two terminals.
	if !tab.zoom && !withBorder {
		for _, d := range tab.layout.Dividers(area) {
			ch := '│'
			if d.Dir == dirVertical {
				ch = '─'
			}
			cell := l.dividerCell()
			cell.Char = ch
			canvas.Fill(d.X, d.Y, d.W, d.H, cell)
		}
	}

	cursorX, cursorY := 0, 0
	focus := tab.focus
	for i, id := range tab.layout.Panes() {
		rect, visible := rects[id]
		if !visible {
			continue // hidden behind a zoomed sibling
		}
		pane := l.app.panes[id]
		if pane == nil {
			continue
		}
		content := paneContentRect(rect, withHeader, withBorder)
		l.resizePane(pane, content.W, content.H)

		focused := id == focus
		switch {
		case withBorder:
			l.drawPaneBorder(canvas, rect, pane, i+1, focused)
		case withHeader:
			l.drawPaneHeader(canvas, rect, pane, i+1, focused)
		}
		if pane.View.active {
			l.drawScrollView(canvas, content, pane)
		} else {
			canvas.Blit(pane.Grid.Snapshot(focused), content.X, content.Y)
		}

		if focused {
			cx, cy := pane.Grid.CursorPosition()
			cursorX, cursorY = content.X+cx, content.Y+cy
		}
	}

	return attachproto.Frame{
		Type:     attachproto.TypeFrame,
		PaneID:   focus,
		Cols:     area.W,
		Rows:     area.H,
		ANSI:     canvas.RenderANSI(),
		CursorX:  cursorX,
		CursorY:  cursorY,
		Revision: l.frameRevision(),
	}
}

// drawScrollView paints a pane's cell scrollback in place of its live screen.
// Only selected cells are reversed; unselected cells retain their original
// terminal colours and attributes.
func (l *Loop) drawScrollView(canvas *termgrid.Canvas, r Rect, pane *Pane) {
	lines := pane.Grid.ScrollbackCells()

	base := termgrid.Cell{Char: ' ', FG: termgrid.DefaultFG, BG: termgrid.DefaultBG}
	canvas.Fill(r.X, r.Y, r.W, r.H, base)

	for y := range r.H {
		idx := pane.View.top + y
		if idx >= len(lines) {
			break
		}
		for x, cell := range lines[idx].Cells {
			if x >= r.W {
				break
			}
			if cell.Char == 0 {
				cell = base
			}
			if pane.View.selected(idx, x) {
				cell.Mode ^= termgrid.ModeReverse
			}
			canvas.Set(r.X+x, r.Y+y, cell)
		}
	}
}

// drawPaneBorder boxes a pane and writes its title into the top edge:
//
//	╭─ 1 ▶ claude ─────────╮
//	│                      │
//	╰──────────────────────╯
//
// The focused pane's box is drawn in the accent colour, which is a far
// clearer "you are here" than a bold header was — and putting the title in
// the top edge means the box costs one row rather than two.
func (l *Loop) drawPaneBorder(canvas *termgrid.Canvas, rect Rect, pane *Pane, index int, focused bool) {
	if rect.W < 2 || rect.H < 2 {
		return
	}
	edge := l.borderCell(focused)
	// A pane that just finished flashes its border, so a result landing on a
	// screen you are already looking at still registers.
	if l.blinking(pane) && blinkPhase(time.Now()) {
		edge.FG = l.app.doneFG
		edge.Mode |= 4 // bold
	}

	horizontal := func(y int, left, right rune) {
		cell := edge
		cell.Char = left
		canvas.Set(rect.X, y, cell)
		cell.Char = '─'
		canvas.Fill(rect.X+1, y, rect.W-2, 1, cell)
		cell.Char = right
		canvas.Set(rect.X+rect.W-1, y, cell)
	}
	horizontal(rect.Y, '╭', '╮')
	horizontal(rect.Y+rect.H-1, '╰', '╯')

	cell := edge
	cell.Char = '│'
	canvas.Fill(rect.X, rect.Y+1, 1, rect.H-2, cell)
	canvas.Fill(rect.X+rect.W-1, rect.Y+1, 1, rect.H-2, cell)

	l.drawPaneTitle(canvas, rect, pane, index, focused, edge)
}

// drawPaneTitle writes "─ 1 ▶ claude ─" into a border's top edge, with the
// status glyph in its own colour so an animating spinner still stands out.
func (l *Loop) drawPaneTitle(canvas *termgrid.Canvas, rect Rect, pane *Pane, index int, focused bool, edge termgrid.Cell) {
	prefix := " " + string(rune('0'+index%10)) + " "
	glyph := l.statusGlyph(pane)
	name := " " + pane.displayName() + scrollSuffix(pane)
	if pane.Status == "exited" {
		name += " [exited]"
	}
	// Everything must fit between the corners, with a dash of edge left over
	// so the title reads as part of the border rather than replacing it.
	budget := rect.W - 4 - len([]rune(prefix)) - len([]rune(glyph))
	if budget < 1 {
		return
	}
	name = truncate(name, budget) + " "

	text := l.headerCell(focused)
	text.BG = edge.BG
	glyphStyle := text
	if pane.agentStatus() == agentstatus.Working {
		glyphStyle.FG = l.app.spinnerFG
		glyphStyle.Mode |= 4 // bold
	}

	x := rect.X + 1
	canvas.DrawText(x, rect.Y, prefix, text)
	x += len([]rune(prefix))
	canvas.DrawText(x, rect.Y, glyph, glyphStyle)
	x += len([]rune(glyph))
	canvas.DrawText(x, rect.Y, name, text)
}

// drawPaneHeader writes "1 ▶ claude" across the top row of a pane's rect,
// bold for the focused pane and dim for the rest — the only focus indicator
// there is room for when panes are packed together.
func (l *Loop) drawPaneHeader(canvas *termgrid.Canvas, rect Rect, pane *Pane, index int, focused bool) {
	style := l.headerCell(focused)
	canvas.Fill(rect.X, rect.Y, rect.W, titleRows, style)

	// Drawn in three pieces so the status glyph can carry its own colour —
	// the spinner is the one moving thing on screen and should look like it.
	prefix := string(rune('0'+index%10)) + " "
	glyph := l.statusGlyph(pane)
	name := " " + pane.displayName() + scrollSuffix(pane)
	if pane.Status == "exited" {
		name += " [exited]"
	}

	glyphStyle := style
	if pane.agentStatus() == agentstatus.Working {
		glyphStyle.FG = l.app.spinnerFG
		glyphStyle.Mode |= 4 // bold
	}

	x := rect.X
	canvas.DrawText(x, rect.Y, truncate(prefix, rect.W), style)
	x += len([]rune(prefix))
	canvas.DrawText(x, rect.Y, truncate(glyph, max(rect.X+rect.W-x, 0)), glyphStyle)
	x += len([]rune(glyph))
	canvas.DrawText(x, rect.Y, truncate(name, max(rect.X+rect.W-x, 0)), style)
}

func truncate(s string, w int) string {
	r := []rune(s)
	if w < 0 {
		w = 0
	}
	if len(r) > w {
		return string(r[:w])
	}
	return s
}

// resizePane pushes a new size into both the emulator and the PTY, but only
// when it actually changed — a resize makes programs redraw, so doing it on
// every frame would make every pane flicker forever.
func (l *Loop) resizePane(pane *Pane, cols, rows int) {
	cols, rows = max(cols, 1), max(rows, 1)
	if c, r := pane.Grid.Size(); c == cols && r == rows {
		return
	}
	pane.Grid.Resize(cols, rows)
	_ = pane.Actor.Resize(cols, rows)
}

// frameRevision is the sum of every pane's revision, so a client can tell
// "something changed" without the daemon tracking a separate counter.
func (l *Loop) frameRevision() uint64 {
	var sum uint64
	for _, p := range l.app.panes {
		sum += p.Revision
	}
	return sum
}
