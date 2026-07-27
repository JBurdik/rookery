package state

import (
	"encoding/json"
	"testing"
)

// The layout is only worth saving if it comes back the same shape, so this
// walks a two-workspace tree with a nested split through the round trip the
// file makes.
func TestSnapshotRoundTrip(t *testing.T) {
	l := &Loop{app: newApp("persist-test")}

	w := l.app.newWorkspace("api", "/tmp/api")
	w.Named = true
	tab := w.addTab("review")
	tab.layout = &Layout{
		Dir: dirHorizontal, Ratio: 0.6,
		A: newLeaf("w1:p1"),
		B: &Layout{Dir: dirVertical, Ratio: 0.5, A: newLeaf("w1:p2"), B: newLeaf("w1:p3")},
	}
	tab.focus = "w1:p2"
	tab.zoom = true
	for _, id := range []string{"w1:p1", "w1:p2", "w1:p3"} {
		l.app.panes[id] = &Pane{ID: id, Cmd: "/bin/zsh", Cwd: "/tmp/api"}
	}
	l.app.newWorkspace("", "/tmp/web").addTab("")

	data, err := json.Marshal(l.buildSnapshot())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got snapshot
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(got.Workspaces) != 2 {
		t.Fatalf("got %d workspaces, want 2", len(got.Workspaces))
	}
	if got.ActiveWS != l.app.activeWS || got.NextWS != 2 {
		t.Errorf("active/next = %q/%d, want %q/2", got.ActiveWS, got.NextWS, l.app.activeWS)
	}

	first := got.Workspaces[0]
	if first.Name != "api" || !first.Named || first.Cwd != "/tmp/api" {
		t.Errorf("workspace = %+v, want the named api workspace", first)
	}
	if len(first.Tabs) != 1 {
		t.Fatalf("got %d tabs, want 1", len(first.Tabs))
	}
	saved := first.Tabs[0]
	if saved.Name != "review" || saved.Focus != "w1:p2" || !saved.Zoom {
		t.Errorf("tab = %+v, want review/w1:p2/zoomed", saved)
	}
	if panes := saved.Layout.Panes(); len(panes) != 3 || panes[0] != "w1:p1" || panes[2] != "w1:p3" {
		t.Errorf("layout panes = %v, want the three in order", panes)
	}
	if saved.Layout.B.Dir != dirVertical {
		t.Errorf("nested split lost its direction: %+v", saved.Layout.B)
	}
	if len(got.Panes) != 3 {
		t.Errorf("got %d pane records, want 3", len(got.Panes))
	}
}

// The snapshot must hold nothing that moves on a status tick, or persist's
// "have the bytes changed" test rewrites the file four times a second on a
// busy session. Pane map order is part of that: it is random in Go.
func TestSnapshotIgnoresStatusChurn(t *testing.T) {
	l := &Loop{app: newApp("persist-test")}
	w := l.app.newWorkspace("api", "/tmp/api")
	tab := w.addTab("")
	tab.layout = &Layout{Dir: dirHorizontal, Ratio: 0.5, A: newLeaf("w1:p1"), B: newLeaf("w1:p2")}
	for _, id := range []string{"w1:p1", "w1:p2"} {
		l.app.panes[id] = &Pane{ID: id, Cmd: "/bin/zsh", Cwd: "/tmp/api"}
	}

	before, err := json.Marshal(l.buildSnapshot())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	p := l.app.panes["w1:p1"]
	p.AgentState, p.Title, p.Seen, p.Revision = "working", "Refactor the parser", false, 42

	for range 20 { // map order is random per iteration, so try it repeatedly
		after, _ := json.Marshal(l.buildSnapshot())
		if string(after) != string(before) {
			t.Fatalf("snapshot changed without a structural change:\n %s\n %s", before, after)
		}
	}

	// A real change does show up.
	w.addTab("logs")
	if changed, _ := json.Marshal(l.buildSnapshot()); string(changed) == string(before) {
		t.Error("adding a tab did not change the snapshot")
	}
}
