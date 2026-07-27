package state

import (
	"slices"
	"testing"
)

func TestLayoutSplitAndRects(t *testing.T) {
	l := newLeaf("p1")
	area := Rect{X: 0, Y: 0, W: 81, H: 24}

	if got := l.Rects(area)["p1"]; got != area {
		t.Fatalf("single leaf rect = %+v, want the whole area %+v", got, area)
	}

	if !l.Split("p1", "p2", dirHorizontal) {
		t.Fatal("Split on an existing pane reported failure")
	}
	rects := l.Rects(area)
	// 81 columns: 40 + 1 divider + 40.
	if want := (Rect{X: 0, Y: 0, W: 40, H: 24}); rects["p1"] != want {
		t.Errorf("p1 = %+v, want %+v", rects["p1"], want)
	}
	if want := (Rect{X: 41, Y: 0, W: 40, H: 24}); rects["p2"] != want {
		t.Errorf("p2 = %+v, want %+v", rects["p2"], want)
	}
	if d := l.Dividers(area); len(d) != 1 || d[0].X != 40 || d[0].W != 1 || d[0].H != 24 {
		t.Errorf("dividers = %+v, want one vertical line at x=40", d)
	}

	// Panes must never overlap, and must never exceed the area.
	assertNoOverlap(t, rects, area)
}

func TestLayoutNestedSplitsTileWithoutOverlap(t *testing.T) {
	l := newLeaf("p1")
	l.Split("p1", "p2", dirHorizontal)
	l.Split("p2", "p3", dirVertical)
	l.Split("p3", "p4", dirHorizontal)

	area := Rect{X: 0, Y: 0, W: 100, H: 30}
	rects := l.Rects(area)
	if len(rects) != 4 {
		t.Fatalf("got %d rects, want 4: %+v", len(rects), rects)
	}
	assertNoOverlap(t, rects, area)

	panes := l.Panes()
	slices.Sort(panes)
	if want := []string{"p1", "p2", "p3", "p4"}; !slices.Equal(panes, want) {
		t.Errorf("Panes() = %v, want %v", panes, want)
	}
}

func TestLayoutRemoveCollapsesParent(t *testing.T) {
	l := newLeaf("p1")
	l.Split("p1", "p2", dirHorizontal)
	l.Split("p2", "p3", dirVertical)

	l = l.Remove("p3")
	area := Rect{X: 0, Y: 0, W: 81, H: 24}
	rects := l.Rects(area)
	if len(rects) != 2 {
		t.Fatalf("after removing p3: %d rects, want 2", len(rects))
	}
	// p2 should have reclaimed the full height of its column.
	if rects["p2"].H != 24 {
		t.Errorf("p2 height = %d, want 24 (sibling should reclaim the space)", rects["p2"].H)
	}

	l = l.Remove("p1")
	if rects := l.Rects(area); len(rects) != 1 || rects["p2"] != area {
		t.Errorf("after removing p1: %+v, want p2 filling the area", rects)
	}

	if l = l.Remove("p2"); l != nil {
		t.Errorf("removing the last pane returned %+v, want nil", l)
	}
}

func TestLayoutResize(t *testing.T) {
	l := newLeaf("p1")
	l.Split("p1", "p2", dirHorizontal)

	if !l.Resize("p1", 0.2, dirHorizontal) {
		t.Fatal("Resize reported no change")
	}
	if l.Ratio < 0.69 || l.Ratio > 0.71 {
		t.Errorf("ratio = %v, want ~0.7", l.Ratio)
	}
	// Growing p2 shrinks the same split from the other side.
	l.Resize("p2", 0.2, dirHorizontal)
	if l.Ratio < 0.49 || l.Ratio > 0.51 {
		t.Errorf("ratio = %v, want back to ~0.5", l.Ratio)
	}
	// Clamped, never collapsing a pane to nothing.
	for range 20 {
		l.Resize("p1", -0.2, dirHorizontal)
	}
	if l.Ratio < minRatio {
		t.Errorf("ratio = %v, want clamped at >= %v", l.Ratio, minRatio)
	}
}

func TestNeighbor(t *testing.T) {
	// p1 | p2
	//    | p3
	l := newLeaf("p1")
	l.Split("p1", "p2", dirHorizontal)
	l.Split("p2", "p3", dirVertical)
	rects := l.Rects(Rect{X: 0, Y: 0, W: 81, H: 25})

	tests := []struct{ from, dir, want string }{
		{"p1", "right", "p2"},
		{"p2", "left", "p1"},
		{"p3", "left", "p1"},
		{"p2", "down", "p3"},
		{"p3", "up", "p2"},
		{"p1", "left", ""},
		{"p1", "up", ""},
		{"p2", "up", ""},
	}
	for _, tt := range tests {
		if got := Neighbor(rects, tt.from, tt.dir); got != tt.want {
			t.Errorf("Neighbor(%s, %s) = %q, want %q", tt.from, tt.dir, got, tt.want)
		}
	}
}

func assertNoOverlap(t *testing.T, rects map[string]Rect, area Rect) {
	t.Helper()
	seen := map[[2]int]string{}
	for id, r := range rects {
		if r.X < area.X || r.Y < area.Y || r.X+r.W > area.X+area.W || r.Y+r.H > area.Y+area.H {
			t.Errorf("pane %s rect %+v escapes the area %+v", id, r, area)
		}
		if r.W <= 0 || r.H <= 0 {
			t.Errorf("pane %s has an empty rect %+v", id, r)
		}
		for y := r.Y; y < r.Y+r.H; y++ {
			for x := r.X; x < r.X+r.W; x++ {
				if other, dup := seen[[2]int{x, y}]; dup {
					t.Fatalf("panes %s and %s both claim cell (%d,%d)", other, id, x, y)
				}
				seen[[2]int{x, y}] = id
			}
		}
	}
}
