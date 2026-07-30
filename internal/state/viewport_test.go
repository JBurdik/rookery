package state

import (
	"strings"
	"testing"

	"github.com/jirkab/rookery/internal/attachproto"
	"github.com/jirkab/rookery/internal/termgrid"
)

// viewportLoop builds a loop with one workspace, one tab and two side by side
// panes — enough that the layout has a divider whose position depends on the
// shared area.
func viewportLoop(t *testing.T) *Loop {
	t.Helper()
	l := &Loop{app: newApp("viewport-test")}
	w := l.app.newWorkspace("ws", "/tmp")
	tab := w.addTab("t")
	tab.layout = newLeaf("w1:p1")
	tab.layout.Split("w1:p1", "w1:p2", dirHorizontal)
	tab.focus = "w1:p1"
	for _, id := range []string{"w1:p1", "w1:p2"} {
		l.app.panes[id] = &Pane{ID: id, Grid: termgrid.New(20, 5), Status: "running", View: scrollView{anchor: -1}}
	}
	return l
}

// attach registers a client the way handleAttachConnect would, without the
// socket underneath it.
func (l *Loop) attachTestClient(id uint64, cols, rows int) *attachClientConn {
	c := &attachClientConn{id: id, send: make(chan any, 64), cols: cols, rows: rows, focused: true}
	l.app.clients[id] = c
	l.recomputeViewport()
	return c
}

func TestSharedAreaIsTheSmallestClient(t *testing.T) {
	l := viewportLoop(t)
	l.attachTestClient(1, 120, 40)
	if got := l.app.area(); got.W != 120 || got.H != 40 {
		t.Fatalf("one client: area = %+v, want 120x40", got)
	}

	l.attachTestClient(2, 80, 50)
	// Per axis, not "whichever client is smaller overall": the narrow client
	// caps columns, the short one caps rows.
	if got := l.app.area(); got.W != 80 || got.H != 40 {
		t.Fatalf("two clients: area = %+v, want 80x40", got)
	}
}

func TestSharedAreaGrowsBackWhenTheSmallClientLeaves(t *testing.T) {
	l := viewportLoop(t)
	l.attachTestClient(1, 120, 40)
	l.attachTestClient(2, 80, 20)
	if got := l.app.area(); got.W != 80 || got.H != 20 {
		t.Fatalf("area = %+v, want 80x20", got)
	}

	delete(l.app.clients, 2)
	l.recomputeViewport()
	if got := l.app.area(); got.W != 120 || got.H != 40 {
		t.Fatalf("after detach: area = %+v, want 120x40", got)
	}
}

func TestSharedAreaKeptWhenNobodyIsAttached(t *testing.T) {
	l := viewportLoop(t)
	l.attachTestClient(1, 100, 30)
	delete(l.app.clients, 1)
	l.recomputeViewport()
	// Resetting to the 80x24 default here would make every program in every
	// pane redraw the moment the last client detached.
	if got := l.app.area(); got.W != 100 || got.H != 30 {
		t.Fatalf("area = %+v, want the last known 100x30", got)
	}
}

func TestEachClientGetsAFrameAtItsOwnSize(t *testing.T) {
	l := viewportLoop(t)
	big := l.attachTestClient(1, 100, 30)
	small := l.attachTestClient(2, 60, 20)
	l.app.dirty = true
	l.flushFrame()

	for _, tc := range []struct {
		name       string
		c          *attachClientConn
		cols, rows int
	}{
		{"big", big, 100, 30},
		{"small", small, 60, 20},
	} {
		frame := lastFrame(t, tc.c)
		if frame.Cols != tc.cols || frame.Rows != tc.rows {
			t.Errorf("%s client: frame = %dx%d, want %dx%d", tc.name, frame.Cols, frame.Rows, tc.cols, tc.rows)
		}
		if lines := strings.Count(frame.ANSI, "\n") + 1; lines != tc.rows {
			t.Errorf("%s client: frame has %d lines, want %d", tc.name, lines, tc.rows)
		}
	}
}

// The point of the whole change: the big client is not clipped to the small
// one's width, it just has blank space to the right of the layout.
func TestBigClientIsPaddedNotClipped(t *testing.T) {
	l := viewportLoop(t)
	l.attachTestClient(1, 100, 30)
	l.attachTestClient(2, 60, 20)

	frame := l.buildFrame(100, 30)
	rows := strings.Split(frame.ANSI, "\n")
	if len(rows) != 30 {
		t.Fatalf("frame has %d rows, want 30", len(rows))
	}
	// Rows past the shared area's height exist and are blank.
	for _, y := range []int{20, 29} {
		if strings.TrimRight(stripANSI(rows[y]), " ") != "" {
			t.Errorf("row %d = %q, want blank padding", y, rows[y])
		}
	}
}

func TestPanesAreSizedFromTheSharedAreaNotTheClient(t *testing.T) {
	l := viewportLoop(t)
	l.attachTestClient(1, 100, 30)
	l.attachTestClient(2, 60, 20)

	// Two frames at two sizes, and the PTY-side size must be the same after
	// either: one PTY per pane means one size, taken from the shared area.
	l.buildFrame(100, 30)
	first := paneSizes(l)
	l.buildFrame(60, 20)
	second := paneSizes(l)

	if first != second {
		t.Fatalf("pane sizes changed with the client rendered for: %v then %v", first, second)
	}
	// 60 columns, split horizontally: 29 + divider + 30, in 20 rows.
	if got := l.app.panes["w1:p1"]; got.Grid == nil {
		t.Fatal("pane has no grid")
	}
	cols, rows := l.app.panes["w1:p1"].Grid.Size()
	// 20 shared rows less the pane box's top and bottom edge.
	if rows != 18 {
		t.Errorf("pane rows = %d, want 18 (the shared area's 20 less its box)", rows)
	}
	if cols >= 60 {
		t.Errorf("pane cols = %d, want less than the shared width (it is one of two panes)", cols)
	}
}

func paneSizes(l *Loop) [2][2]int {
	var out [2][2]int
	for i, id := range []string{"w1:p1", "w1:p2"} {
		c, r := l.app.panes[id].Grid.Size()
		out[i] = [2]int{c, r}
	}
	return out
}

func lastFrame(t *testing.T, c *attachClientConn) attachproto.Frame {
	t.Helper()
	var frame attachproto.Frame
	found := false
	for {
		select {
		case msg := <-c.send:
			if f, ok := msg.(attachproto.Frame); ok {
				frame, found = f, true
			}
		default:
			if !found {
				t.Fatal("client got no frame")
			}
			return frame
		}
	}
}

// stripANSI drops escape sequences so a row can be checked for content.
func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b {
			for i < len(s) && s[i] != 'm' && s[i] != 'H' && s[i] != 'K' {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
