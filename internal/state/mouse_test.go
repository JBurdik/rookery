package state

import (
	"testing"

	"github.com/jirkab/rookery/internal/attachproto"
)

func TestEncodeMouseSGR(t *testing.T) {
	tests := []struct {
		name           string
		code, col, row int
		release, sgr   bool
		want           string
	}{
		{name: "left press", code: 0, col: 8, row: 10, sgr: true, want: "\x1b[<0;8;10M"},
		{name: "left release", code: 0, col: 8, row: 10, release: true, sgr: true, want: "\x1b[<0;8;10m"},
		{name: "wheel up", code: 64, col: 3, row: 4, sgr: true, want: "\x1b[<64;3;4M"},
		{name: "drag", code: 32, col: 1, row: 1, sgr: true, want: "\x1b[<32;1;1M"},
		// Legacy X10: every field is offset by 32 and packed into one byte,
		// so column 1 is 32+1 = '!'. This encoding is why SGR is preferred:
		// past column 223 the byte overflows and coordinates go wrong.
		{name: "legacy press", code: 0, col: 1, row: 1, want: "\x1b[M !!"},
		{name: "legacy release uses button 3", code: 0, col: 1, row: 1, release: true, want: "\x1b[M#!!"},
		{name: "legacy mid-screen", code: 0, col: 40, row: 12, want: "\x1b[M H,"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := encodeMouse(tt.code, tt.col, tt.row, tt.release, tt.sgr); got != tt.want {
				t.Errorf("encodeMouse() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMouseButtonCode(t *testing.T) {
	tests := []struct {
		m       attachproto.Mouse
		code    int
		release bool
	}{
		{m: attachproto.Mouse{Kind: "press", Button: "left"}, code: 0},
		{m: attachproto.Mouse{Kind: "press", Button: "middle"}, code: 1},
		{m: attachproto.Mouse{Kind: "press", Button: "right"}, code: 2},
		{m: attachproto.Mouse{Kind: "release", Button: "left"}, code: 0, release: true},
		{m: attachproto.Mouse{Kind: "drag", Button: "left"}, code: 32},
		{m: attachproto.Mouse{Kind: "wheel", Button: "up"}, code: 64},
		{m: attachproto.Mouse{Kind: "wheel", Button: "down"}, code: 65},
	}
	for _, tt := range tests {
		code, release := mouseButtonCode(tt.m)
		if code != tt.code || release != tt.release {
			t.Errorf("mouseButtonCode(%+v) = %d,%v; want %d,%v", tt.m, code, release, tt.code, tt.release)
		}
	}
}

func TestPaneAt(t *testing.T) {
	rects := map[string]Rect{
		"a": {X: 0, Y: 0, W: 10, H: 5},
		"b": {X: 11, Y: 0, W: 10, H: 5},
	}
	tests := []struct {
		x, y int
		want string
	}{
		{0, 0, "a"},
		{9, 4, "a"},
		{10, 0, ""}, // the divider column belongs to neither pane
		{11, 0, "b"},
		{20, 4, "b"},
		{21, 0, ""},
		{0, 5, ""},
	}
	for _, tt := range tests {
		if got := paneAt(rects, tt.x, tt.y); got != tt.want {
			t.Errorf("paneAt(%d,%d) = %q, want %q", tt.x, tt.y, got, tt.want)
		}
	}
}

// TestDividerCarriesResizeTarget pins the contract the mouse drag depends on:
// each divider names a pane on its A side, and resizing that pane is what
// moves this divider.
func TestDividerCarriesResizeTarget(t *testing.T) {
	l := newLeaf("p1")
	l.Split("p1", "p2", dirHorizontal)

	dividers := l.Dividers(Rect{W: 81, H: 24})
	if len(dividers) != 1 {
		t.Fatalf("got %d dividers, want 1", len(dividers))
	}
	if dividers[0].APane != "p1" {
		t.Errorf("divider resize target = %q, want p1", dividers[0].APane)
	}
}

// TestMouseReport pins what a program that asked for mouse reporting is
// actually handed: a whole click (press *and* release), drags, wheel steps and
// modifiers, all in pane-local coordinates. The release half is the one that
// was missing — an agent TUI that never sees the button come back up never
// registers a click at all.
func TestMouseReport(t *testing.T) {
	const w, h = 40, 12
	tests := []struct {
		name string
		m    attachproto.Mouse
		col  int
		row  int
		want string
	}{
		{name: "press", m: attachproto.Mouse{Kind: "press", Button: "left"}, col: 5, row: 3, want: "\x1b[<0;5;3M"},
		{name: "release", m: attachproto.Mouse{Kind: "release", Button: "left"}, col: 5, row: 3, want: "\x1b[<0;5;3m"},
		{name: "right press", m: attachproto.Mouse{Kind: "press", Button: "right"}, col: 1, row: 1, want: "\x1b[<2;1;1M"},
		{name: "drag", m: attachproto.Mouse{Kind: "drag", Button: "left"}, col: 9, row: 4, want: "\x1b[<32;9;4M"},
		{name: "wheel up", m: attachproto.Mouse{Kind: "wheel", Button: "up"}, col: 2, row: 2, want: "\x1b[<64;2;2M"},
		{name: "wheel down", m: attachproto.Mouse{Kind: "wheel", Button: "down"}, col: 2, row: 2, want: "\x1b[<65;2;2M"},
		{name: "ctrl+shift press", m: attachproto.Mouse{Kind: "press", Button: "left", Ctrl: true, Shift: true}, col: 2, row: 2, want: "\x1b[<20;2;2M"},
		{name: "outside the content area is dropped", m: attachproto.Mouse{Kind: "press", Button: "left"}, col: 0, row: 3},
		{name: "past the right edge is dropped", m: attachproto.Mouse{Kind: "press", Button: "left"}, col: w + 1, row: 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mouseReport(tt.m, tt.col, tt.row, w, h, true); got != tt.want {
				t.Errorf("mouseReport() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestMouseTargetKeepsGesture: a drag that leaves the pane, and the release
// that ends it, still belong to the program that saw the press.
func TestMouseTargetKeepsGesture(t *testing.T) {
	rects := map[string]Rect{
		"a": {X: 0, Y: 0, W: 10, H: 5},
		"b": {X: 10, Y: 0, W: 10, H: 5},
	}
	a := &App{mousePane: "a"}

	if got := a.mouseTarget(rects, attachproto.Mouse{Kind: "drag", X: 15, Y: 2}); got != "a" {
		t.Errorf("drag over b during a's gesture -> %q, want %q", got, "a")
	}
	if got := a.mouseTarget(rects, attachproto.Mouse{Kind: "release", X: 15, Y: 2}); got != "a" {
		t.Errorf("release over b during a's gesture -> %q, want %q", got, "a")
	}
	// A press always goes to whatever is under the pointer, and a gesture
	// whose pane has since disappeared must not pin events to a dead ID.
	if got := a.mouseTarget(rects, attachproto.Mouse{Kind: "press", X: 15, Y: 2}); got != "b" {
		t.Errorf("press over b -> %q, want %q", got, "b")
	}
	a.mousePane = "gone"
	if got := a.mouseTarget(rects, attachproto.Mouse{Kind: "release", X: 15, Y: 2}); got != "b" {
		t.Errorf("release after the gesture's pane closed -> %q, want %q", got, "b")
	}
}
