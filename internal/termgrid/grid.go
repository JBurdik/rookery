// Package termgrid wraps a terminal cell-grid emulator so the rest of the
// daemon never imports the underlying VT100 library directly.
//
// ponytail / dependency note: the plan originally called for
// charmbracelet/x/vt (richer API: built-in ANSI render + scrollback), but
// that package pulls in a newer github.com/charmbracelet/x/ansi than
// bubbletea v1's own (indirect, via x/cellbuf) dependency tolerates —
// concretely: pane.create.go build fails once both are in the same module
// graph, since Go resolves one ansi version for the whole build and
// cellbuf's code doesn't compile against the newer one. Rather than fight
// two Charm packages from different (stable vs. experimental) generations,
// this uses github.com/hinshun/vt10x instead: zero dependencies of its own,
// so it can't collide with anything bubbletea needs. Trade-off: vt10x has
// no built-in ANSI render or scrollback, so both are implemented here — see
// render.go and the scrollback ring below.
package termgrid

import (
	"slices"
	"strings"
	"sync"

	"github.com/hinshun/vt10x"
)

// Grid maintains the live screen + a plain-text scrollback transcript for
// one pane, fed by raw PTY output bytes.
type Grid struct {
	mu    sync.Mutex
	term  vt10x.Terminal
	dirty bool

	scroll    []string        // completed lines, oldest first
	pending   strings.Builder // current (not yet newline-terminated) line
	maxScroll int

	// escTail holds an escape sequence split across two PTY reads — see
	// faint.go.
	escTail []byte
}

const defaultScrollbackLines = 2000

// New creates a grid sized cols x rows.
func New(cols, rows int) *Grid {
	return &Grid{
		term:      vt10x.New(vt10x.WithSize(cols, rows)),
		maxScroll: defaultScrollbackLines,
	}
}

// Write feeds raw PTY output bytes into the emulator and appends the
// plain-text portion to the scrollback transcript.
//
// ponytail: this transcript is "everything the pane printed, ANSI stripped,
// newline-split" — not a true terminal scrollback (it won't collapse
// in-place redraws like a progress bar the way a real terminal's scroll
// region does). Good enough for "what did my agent print while I was away";
// upgrade path is future-plan.md if that ever matters.
func (g *Grid) Write(p []byte) {
	g.mu.Lock()
	defer g.mu.Unlock()
	p = g.rewriteFaint(p)
	g.term.Write(p)
	g.appendTranscript(p)
	g.dirty = true
}

func (g *Grid) appendTranscript(p []byte) {
	plain := stripANSI(p)
	for _, r := range string(plain) {
		if r == '\n' {
			g.scroll = append(g.scroll, g.pending.String())
			g.pending.Reset()
			if len(g.scroll) > g.maxScroll {
				g.scroll = g.scroll[len(g.scroll)-g.maxScroll:]
			}
			continue
		}
		if r == '\r' {
			continue
		}
		g.pending.WriteRune(r)
	}
}

// Resize changes the grid dimensions.
func (g *Grid) Resize(cols, rows int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.term.Resize(cols, rows)
	g.dirty = true
}

// TakeDirty reports whether the grid changed since the last call and clears
// the flag. Used for the daemon's push-on-change frame broadcast (full
// re-render per dirty tick, not a per-line diff — see future-plan.md).
func (g *Grid) TakeDirty() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	d := g.dirty
	g.dirty = false
	return d
}

// Snapshot copies the live screen into a Canvas, optionally drawing the
// cursor as a reverse-video cell. This is the single way pane content leaves
// the emulator: the daemon composites several snapshots onto one canvas to
// build a split-screen frame, and single-pane rendering is just the trivial
// case of that.
func (g *Grid) Snapshot(withCursor bool) *Canvas {
	g.mu.Lock()
	defer g.mu.Unlock()

	cols, rows := g.term.Size()
	cur := g.term.Cursor()
	showCursor := withCursor && g.term.CursorVisible()

	c := NewCanvas(cols, rows)
	for y := range rows {
		for x := range cols {
			src := g.term.Cell(x, y)
			cell := Cell{Char: src.Char, FG: Color(src.FG), BG: Color(src.BG), Mode: src.Mode}
			if showCursor && x == cur.X && y == cur.Y {
				cell.Mode ^= attrReverse
			}
			c.Set(x, y, cell)
		}
	}
	return c
}

// MouseEnabled reports whether the program in this pane asked the terminal
// to report mouse events. When it has, clicks and scrolls belong to it and
// the multiplexer must forward them rather than acting on them itself —
// otherwise clicking inside an agent's TUI would do nothing but move focus.
func (g *Grid) MouseEnabled() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.term.Mode()&vt10x.ModeMouseMask != 0
}

// MouseSGR reports whether the program asked for SGR-encoded mouse reports
// (the modern 1006 mode) rather than the legacy byte-packed encoding, which
// can't express coordinates past column 223.
func (g *Grid) MouseSGR() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.term.Mode()&vt10x.ModeMouseSgr != 0
}

// Title returns the terminal title the pane's program last set via OSC 0/2.
// Agents use it as a status channel — Claude Code, for one, spins a braille
// character there while it is working — so it is the cheapest high-signal
// input the status detector has.
func (g *Grid) Title() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.term.Title()
}

// BottomLines returns the last n non-empty screen lines, oldest first, with
// trailing whitespace trimmed. This is the region agent status rules match
// against: prompts, spinners and confirmation dialogs all live at the bottom
// of an agent's screen.
func (g *Grid) BottomLines(n int) []string {
	g.mu.Lock()
	defer g.mu.Unlock()

	cols, rows := g.term.Size()
	out := make([]string, 0, n)
	for y := rows - 1; y >= 0 && len(out) < n; y-- {
		var b strings.Builder
		for x := range cols {
			ch := g.term.Cell(x, y).Char
			if ch == 0 {
				ch = ' '
			}
			b.WriteRune(ch)
		}
		if line := strings.TrimRight(b.String(), " \t"); line != "" {
			out = append(out, line)
		}
	}
	slices.Reverse(out)
	return out
}

// CursorPosition returns the cursor's current column/row.
func (g *Grid) CursorPosition() (x, y int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	cur := g.term.Cursor()
	return cur.X, cur.Y
}

// Scrollback returns up to maxLines of the plain-text transcript (oldest
// first, including the current partial line). A non-positive maxLines
// returns everything available. The ansi parameter is accepted for API
// symmetry with RenderANSI/RenderPlain but scrollback is always plain text.
func (g *Grid) Scrollback(maxLines int, _ bool) (text string, truncated bool) {
	g.mu.Lock()
	defer g.mu.Unlock()

	lines := g.scroll
	if g.pending.Len() > 0 {
		lines = append(append([]string{}, g.scroll...), g.pending.String())
	}

	start := 0
	if maxLines > 0 && len(lines) > maxLines {
		start = len(lines) - maxLines
		truncated = true
	}
	return strings.Join(lines[start:], "\n"), truncated
}

// ScrollbackLines returns the transcript as lines, oldest first, including
// the current partial line. This is what the scroll/copy viewport draws:
// plain text, because that is all the transcript ever was.
func (g *Grid) ScrollbackLines() []string {
	g.mu.Lock()
	defer g.mu.Unlock()

	out := make([]string, len(g.scroll), len(g.scroll)+1)
	copy(out, g.scroll)
	if g.pending.Len() > 0 {
		out = append(out, g.pending.String())
	}
	return out
}

// Size returns the current grid dimensions.
func (g *Grid) Size() (cols, rows int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.term.Size()
}
