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

// paneContentRect is the area inside a pane's rectangle that the terminal
// itself gets: the rectangle minus its header row.
func paneContentRect(r Rect, withHeader bool) Rect {
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
	withHeader := len(rects) > 1

	// Dividers first, so pane content painted next always wins on overlap.
	if !tab.zoom {
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
		content := paneContentRect(rect, withHeader)
		l.resizePane(pane, content.W, content.H)

		focused := id == focus
		if withHeader {
			l.drawPaneHeader(canvas, rect, pane, i+1, focused)
		}
		canvas.Blit(pane.Grid.Snapshot(focused), content.X, content.Y)

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
	name := " " + pane.displayName()
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
