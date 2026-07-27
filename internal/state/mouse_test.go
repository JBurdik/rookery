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
