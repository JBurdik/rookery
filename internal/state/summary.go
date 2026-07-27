package state

import (
	"strconv"

	"github.com/jirkab/rookery/internal/agentstatus"
	"github.com/jirkab/rookery/internal/attachproto"
)

// buildState assembles the whole navigable tree for attached clients. It is
// sent as one message so a client never sees a half-updated tree.
func (l *Loop) buildState() attachproto.State {
	s := attachproto.State{
		Type:            attachproto.TypeState,
		Session:         l.app.Session,
		ActiveWorkspace: l.app.activeWS,
		Focus:           l.app.focusedPane(),
		Workspaces:      make([]attachproto.WorkspaceSummary, 0, len(l.app.workspaces)),
		Tabs:            []attachproto.TabSummary{},
		Panes:           []attachproto.PaneSummary{},
		Agents:          []attachproto.AgentSummary{},
	}
	if p := l.app.panes[l.app.focusedPane()]; p != nil && p.View.active {
		s.Copy, s.Selecting = true, p.View.anchor >= 0
	}

	for _, w := range l.app.workspaces {
		s.Workspaces = append(s.Workspaces, l.workspaceSummary(w))
	}

	active := l.app.activeWorkspace()
	if active == nil {
		return s
	}
	s.ActiveTab = active.activeTab
	for _, t := range active.tabs {
		s.Tabs = append(s.Tabs, attachproto.TabSummary{
			ID:     t.ID,
			Name:   t.displayName(len(s.Tabs) + 1),
			Panes:  len(t.layout.Panes()),
			Status: string(l.rollup(t.layout.Panes())),
		})
	}

	rects := l.app.rects()
	for _, id := range l.app.visiblePanes() {
		pane, ok := l.app.panes[id]
		if !ok {
			continue
		}
		r, visible := rects[id]
		if !visible {
			continue // hidden behind a zoomed sibling
		}
		sum := pane.toSummary()
		sum.X, sum.Y, sum.W, sum.H = r.X, r.Y, r.W, r.H
		sum.MouseWanted = pane.Grid.MouseEnabled()
		s.Panes = append(s.Panes, sum)
	}

	if t := l.app.activeTab(); t != nil && !t.zoom {
		for _, d := range t.layout.Dividers(l.app.area()) {
			s.Dividers = append(s.Dividers, attachproto.DividerRect{
				PaneID: d.APane,
				Dir:    d.Dir,
				X:      d.X, Y: d.Y, W: d.W, H: d.H,
			})
		}
		s.Zoomed = false
	} else if t != nil {
		s.Zoomed = t.zoom
	}

	// Agents across every workspace, so the sidebar can answer "who needs
	// me" without switching workspace first.
	for _, w := range l.app.workspaces {
		for _, t := range w.tabs {
			for i, id := range t.layout.Panes() {
				pane, ok := l.app.panes[id]
				if !ok || pane.Agent == "" {
					continue
				}
				status := pane.agentStatus()
				s.Agents = append(s.Agents, attachproto.AgentSummary{
					PaneID:    id,
					Workspace: w.displayName(),
					Tab:       t.displayName(i + 1),
					Agent:     pane.Agent,
					Title:     pane.displayName(),
					Status:    string(status),
					Unread:    status == agentstatus.Done,
				})
			}
		}
	}
	return s
}

func (l *Loop) workspaceSummary(w *Workspace) attachproto.WorkspaceSummary {
	panes := w.panes()
	agents := 0
	for _, id := range panes {
		if p, ok := l.app.panes[id]; ok && p.Agent != "" {
			agents++
		}
	}
	return attachproto.WorkspaceSummary{
		ID:     w.ID,
		Name:   w.displayName(),
		Branch: w.Branch,
		Cwd:    w.Cwd,
		Status: string(l.rollup(panes)),
		Agents: agents,
		Tabs:   len(w.tabs),
	}
}

// rollup reduces a set of panes to the one status that most deserves
// attention. Blocked beats done beats working beats idle, so a workspace or
// tab with anything waiting on you says so, whatever else is going on inside.
func (l *Loop) rollup(paneIDs []string) agentstatus.State {
	best := agentstatus.Unknown
	rank := map[agentstatus.State]int{
		agentstatus.Unknown: 0,
		agentstatus.Idle:    1,
		agentstatus.Working: 2,
		agentstatus.Done:    3,
		agentstatus.Blocked: 4,
	}
	for _, id := range paneIDs {
		pane, ok := l.app.panes[id]
		if !ok || pane.Agent == "" {
			continue // plain terminals don't drive the rollup
		}
		if s := pane.agentStatus(); rank[s] > rank[best] {
			best = s
		}
	}
	return best
}

// displayName is a tab's label, falling back to its position.
func (t *Tab) displayName(index int) string {
	if t.Name != "" {
		return t.Name
	}
	return strconv.Itoa(index)
}
