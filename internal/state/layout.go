package state

// Layout is a binary split tree, the same shape tmux and Herdr use: every
// node is either a leaf holding one pane, or a split holding exactly two
// children and the fraction of space the first one gets.
//
// ponytail: binary, not n-ary. An n-ary tree renders identically and only
// pays off for "resize one pane, keep siblings proportional" — behaviour
// nobody has asked for yet. Splitting an already-split node just nests one
// level deeper, which is what tmux does too.
// The json tags are what the saved layout file is written with — see
// internal/state/persist.go.
type Layout struct {
	// Leaf.
	PaneID string `json:"pane,omitempty"`

	// Split. Dir is "h" (children side by side, a vertical divider between
	// them) or "v" (children stacked, a horizontal divider).
	Dir   string  `json:"dir,omitempty"`
	Ratio float64 `json:"ratio,omitempty"` // share of the space given to A, in (0,1)
	A     *Layout `json:"a,omitempty"`
	B     *Layout `json:"b,omitempty"`
}

// Rect is a pane's position on screen, in cells.
type Rect struct {
	X, Y, W, H int
}

const (
	dirHorizontal = "h"
	dirVertical   = "v"

	// A pane smaller than this can't show anything useful, so splits that
	// would create one are refused rather than rendered as a sliver.
	minPaneCols = 12
	minPaneRows = 3

	minRatio = 0.1
	maxRatio = 0.9
)

func newLeaf(paneID string) *Layout { return &Layout{PaneID: paneID} }

func (l *Layout) isLeaf() bool { return l != nil && l.Dir == "" }

// Split replaces the leaf holding paneID with a split of that leaf and a new
// leaf for newPaneID. dir is "h" (new pane to the right) or "v" (below).
// Reports false if paneID isn't in the tree.
func (l *Layout) Split(paneID, newPaneID, dir string) bool {
	target := l.find(paneID)
	if target == nil {
		return false
	}
	*target = Layout{
		Dir:   dir,
		Ratio: 0.5,
		A:     newLeaf(paneID),
		B:     newLeaf(newPaneID),
	}
	return true
}

// Remove deletes paneID's leaf, collapsing its parent split so the sibling
// takes over the whole space. Returns the surviving tree, which is nil once
// the last pane is gone.
func (l *Layout) Remove(paneID string) *Layout {
	if l == nil {
		return nil
	}
	if l.isLeaf() {
		if l.PaneID == paneID {
			return nil
		}
		return l
	}
	l.A, l.B = l.A.Remove(paneID), l.B.Remove(paneID)
	switch {
	case l.A == nil && l.B == nil:
		return nil
	case l.A == nil:
		return l.B
	case l.B == nil:
		return l.A
	}
	return l
}

// find returns the subtree that is the leaf for paneID.
func (l *Layout) find(paneID string) *Layout {
	if l == nil {
		return nil
	}
	if l.isLeaf() {
		if l.PaneID == paneID {
			return l
		}
		return nil
	}
	if n := l.A.find(paneID); n != nil {
		return n
	}
	return l.B.find(paneID)
}

// firstPane is any pane in the subtree, used to name a side of a split.
func (l *Layout) firstPane() string {
	if p := l.Panes(); len(p) > 0 {
		return p[0]
	}
	return ""
}

// Panes lists every pane in the tree, left-to-right / top-to-bottom.
func (l *Layout) Panes() []string {
	if l == nil {
		return nil
	}
	if l.isLeaf() {
		return []string{l.PaneID}
	}
	return append(l.A.Panes(), l.B.Panes()...)
}

// Resize adjusts the ratio of the split that directly contains paneID, by
// delta (positive grows the side paneID is on). Reports whether anything
// changed.
func (l *Layout) Resize(paneID string, delta float64, dir string) bool {
	if l == nil || l.isLeaf() {
		return false
	}
	if l.Dir == dir {
		if l.A.contains(paneID) {
			return l.adjust(delta)
		}
		if l.B.contains(paneID) {
			return l.adjust(-delta)
		}
	}
	return l.A.Resize(paneID, delta, dir) || l.B.Resize(paneID, delta, dir)
}

func (l *Layout) adjust(delta float64) bool {
	r := min(max(l.Ratio+delta, minRatio), maxRatio)
	if r == l.Ratio {
		return false
	}
	l.Ratio = r
	return true
}

func (l *Layout) contains(paneID string) bool { return l.find(paneID) != nil }

// Swap exchanges the positions of two panes in the tree: each leaf keeps its
// own content and process, only the PaneID each split slot holds changes.
// Reports false if either pane isn't in the tree.
func (l *Layout) Swap(paneA, paneB string) bool {
	a, b := l.find(paneA), l.find(paneB)
	if a == nil || b == nil {
		return false
	}
	a.PaneID, b.PaneID = b.PaneID, a.PaneID
	return true
}

// Rects assigns every pane a rectangle inside the given area, reserving one
// cell between siblings for the divider the renderer draws there.
func (l *Layout) Rects(area Rect) map[string]Rect {
	out := map[string]Rect{}
	l.rects(area, out)
	return out
}

func (l *Layout) rects(area Rect, out map[string]Rect) {
	if l == nil || area.W <= 0 || area.H <= 0 {
		return
	}
	if l.isLeaf() {
		out[l.PaneID] = area
		return
	}

	if l.Dir == dirHorizontal {
		avail := area.W - 1 // one column for the divider
		aw := int(float64(avail) * l.Ratio)
		aw = min(max(aw, 1), max(avail-1, 1))
		l.A.rects(Rect{X: area.X, Y: area.Y, W: aw, H: area.H}, out)
		l.B.rects(Rect{X: area.X + aw + 1, Y: area.Y, W: avail - aw, H: area.H}, out)
		return
	}

	avail := area.H - 1 // one row for the divider
	ah := int(float64(avail) * l.Ratio)
	ah = min(max(ah, 1), max(avail-1, 1))
	l.A.rects(Rect{X: area.X, Y: area.Y, W: area.W, H: ah}, out)
	l.B.rects(Rect{X: area.X, Y: area.Y + ah + 1, W: area.W, H: avail - ah}, out)
}

// Dividers returns the divider segments to draw for this layout: one per
// split node, positioned in the gap Rects left between the two children.
type Divider struct {
	Rect
	Dir string // "h": a vertical line 1 cell wide; "v": a horizontal line 1 cell tall
	// APane is any pane on the A (left/top) side of this split. Dragging the
	// divider is expressed as "resize that pane", which is the same
	// operation the keyboard bindings use, so both paths share one code
	// path in the daemon.
	APane string
}

func (l *Layout) Dividers(area Rect) []Divider {
	var out []Divider
	l.dividers(area, &out)
	return out
}

func (l *Layout) dividers(area Rect, out *[]Divider) {
	if l == nil || l.isLeaf() || area.W <= 0 || area.H <= 0 {
		return
	}
	if l.Dir == dirHorizontal {
		avail := area.W - 1
		aw := min(max(int(float64(avail)*l.Ratio), 1), max(avail-1, 1))
		*out = append(*out, Divider{
			Rect:  Rect{X: area.X + aw, Y: area.Y, W: 1, H: area.H},
			Dir:   dirHorizontal,
			APane: l.A.firstPane(),
		})
		l.A.dividers(Rect{X: area.X, Y: area.Y, W: aw, H: area.H}, out)
		l.B.dividers(Rect{X: area.X + aw + 1, Y: area.Y, W: avail - aw, H: area.H}, out)
		return
	}
	avail := area.H - 1
	ah := min(max(int(float64(avail)*l.Ratio), 1), max(avail-1, 1))
	*out = append(*out, Divider{
		Rect:  Rect{X: area.X, Y: area.Y + ah, W: area.W, H: 1},
		Dir:   dirVertical,
		APane: l.A.firstPane(),
	})
	l.A.dividers(Rect{X: area.X, Y: area.Y, W: area.W, H: ah}, out)
	l.B.dividers(Rect{X: area.X, Y: area.Y + ah + 1, W: area.W, H: avail - ah}, out)
}

// Neighbor finds the pane to move focus to when travelling in a direction
// from paneID: the pane whose rectangle is nearest on that side, measured
// from the source pane's centre. Returns "" when there is nothing that way.
func Neighbor(rects map[string]Rect, paneID, direction string) string {
	from, ok := rects[paneID]
	if !ok {
		return ""
	}
	fx, fy := from.X+from.W/2, from.Y+from.H/2

	best, bestDist := "", 0
	for id, r := range rects {
		if id == paneID {
			continue
		}
		var ahead bool
		var dist int
		switch direction {
		case "left":
			ahead, dist = r.X+r.W <= from.X, from.X-(r.X+r.W)
		case "right":
			ahead, dist = r.X >= from.X+from.W, r.X-(from.X+from.W)
		case "up":
			ahead, dist = r.Y+r.H <= from.Y, from.Y-(r.Y+r.H)
		case "down":
			ahead, dist = r.Y >= from.Y+from.H, r.Y-(from.Y+from.H)
		default:
			return ""
		}
		if !ahead {
			continue
		}
		// Break ties by how far the candidate's span is from the source
		// centre on the perpendicular axis, so "right" from a tall pane
		// picks the neighbour actually beside it rather than a far corner.
		switch direction {
		case "left", "right":
			dist = dist*1000 + abs(r.Y+r.H/2-fy)
		default:
			dist = dist*1000 + abs(r.X+r.W/2-fx)
		}
		if best == "" || dist < bestDist {
			best, bestDist = id, dist
		}
	}
	return best
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
