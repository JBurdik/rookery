package state

import (
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"time"

	"github.com/jirkab/rookery/internal/agentstatus"
	"github.com/jirkab/rookery/internal/apiproto"
	"github.com/jirkab/rookery/internal/attachproto"
)

const defaultCols, defaultRows = 80, 24

// sendGrace is how long after sending input a pane is assumed to still be
// working, whatever its screen currently says. Long enough for an agent to
// redraw, short enough that a no-op prompt settles quickly.
const sendGrace = 2 * time.Second

// enterDelay separates typed text from the Enter that submits it. Long enough
// that an agent's paste detection sees two events rather than one chunk, short
// enough to feel instant.
const enterDelay = 150 * time.Millisecond

// defaultShell is what a pane runs when no command is given.
func defaultShell() string {
	if sh := os.Getenv("SHELL"); sh != "" {
		return sh
	}
	return "/bin/sh"
}

// handleAPI dispatches one JSON-RPC request. Only ever called from Run's own
// goroutine (via the apiMsgs channel), so it can freely mutate l.app.
//
// A deferred return means "no response yet": the request is a wait, and its
// reply channel has been parked in l.app.waiters to be answered when the
// awaited condition happens (or times out). Everything else answers straight
// away — the loop must never block on anything.
func (l *Loop) handleAPI(req apiproto.Request, reply chan apiproto.Response) (apiproto.Response, bool) {
	switch req.Method {
	case "ping":
		return ok(req.ID, apiproto.PingResult{Pong: true, Protocol: apiproto.Protocol, Version: l.version}), false

	// --- workspaces ---

	case "workspace.list":
		out := make([]apiproto.WorkspaceInfo, 0, len(l.app.workspaces))
		for _, w := range l.app.workspaces {
			out = append(out, l.workspaceInfo(w))
		}
		return ok(req.ID, apiproto.WorkspaceListResult{Workspaces: out, Active: l.app.activeWS}), false

	case "workspace.create":
		var p apiproto.WorkspaceCreateParams
		if err := unmarshal(req.Params, &p); err != nil {
			return errResp(req.ID, apiproto.ErrInvalidParams, err.Error()), false
		}
		return l.workspaceCreate(req.ID, p), false

	case "workspace.close":
		var p apiproto.WorkspaceCloseParams
		if err := unmarshal(req.Params, &p); err != nil {
			return errResp(req.ID, apiproto.ErrInvalidParams, err.Error()), false
		}
		return l.workspaceClose(req.ID, p), false

	case "workspace.focus":
		var p apiproto.WorkspaceCloseParams
		if err := unmarshal(req.Params, &p); err != nil {
			return errResp(req.ID, apiproto.ErrInvalidParams, err.Error()), false
		}
		if !l.focusWorkspace(p.WorkspaceID) {
			return errResp(req.ID, apiproto.ErrNotFound, "no such workspace: "+p.WorkspaceID), false
		}
		return ok(req.ID, l.workspaceInfo(l.app.activeWorkspace())), false

	case "workspace.rename":
		var p apiproto.RenameParams
		if err := unmarshal(req.Params, &p); err != nil {
			return errResp(req.ID, apiproto.ErrInvalidParams, err.Error()), false
		}
		w := l.app.workspace(p.ID)
		if w == nil {
			return errResp(req.ID, apiproto.ErrNotFound, "no such workspace: "+p.ID), false
		}
		w.Name = p.Name
		w.Named = p.Name != ""
		l.broadcastState()
		return ok(req.ID, l.workspaceInfo(w)), false

	// --- tabs ---

	case "tab.list":
		w := l.app.activeWorkspace()
		var p apiproto.TabListParams
		_ = unmarshal(req.Params, &p)
		if p.WorkspaceID != "" {
			w = l.app.workspace(p.WorkspaceID)
		}
		if w == nil {
			return errResp(req.ID, apiproto.ErrNotFound, "no such workspace"), false
		}
		out := make([]apiproto.TabInfo, 0, len(w.tabs))
		for i, t := range w.tabs {
			out = append(out, l.tabInfo(w, t, i+1))
		}
		return ok(req.ID, apiproto.TabListResult{Tabs: out, Active: w.activeTab}), false

	case "tab.create":
		var p apiproto.TabCreateParams
		if err := unmarshal(req.Params, &p); err != nil {
			return errResp(req.ID, apiproto.ErrInvalidParams, err.Error()), false
		}
		return l.tabCreate(req.ID, p), false

	case "tab.close":
		var p apiproto.TabCloseParams
		if err := unmarshal(req.Params, &p); err != nil {
			return errResp(req.ID, apiproto.ErrInvalidParams, err.Error()), false
		}
		return l.tabClose(req.ID, p), false

	case "tab.focus":
		var p apiproto.TabCloseParams
		if err := unmarshal(req.Params, &p); err != nil {
			return errResp(req.ID, apiproto.ErrInvalidParams, err.Error()), false
		}
		if !l.focusTab(p.TabID) {
			return errResp(req.ID, apiproto.ErrNotFound, "no such tab: "+p.TabID), false
		}
		w, t := l.app.resolveTab(p.TabID)
		return ok(req.ID, l.tabInfo(w, t, 0)), false

	case "tab.rename":
		var p apiproto.RenameParams
		if err := unmarshal(req.Params, &p); err != nil {
			return errResp(req.ID, apiproto.ErrInvalidParams, err.Error()), false
		}
		w, t := l.app.resolveTab(p.ID)
		if t == nil {
			return errResp(req.ID, apiproto.ErrNotFound, "no such tab: "+p.ID), false
		}
		t.Name = p.Name
		l.broadcastState()
		return ok(req.ID, l.tabInfo(w, t, 0)), false

	// --- panes ---

	case "pane.list":
		var p apiproto.PaneListParams
		_ = unmarshal(req.Params, &p)
		ids := l.app.visiblePanes()
		if p.All {
			ids = l.app.allPanes()
		}
		panes := make([]apiproto.PaneInfo, 0, len(ids))
		for _, id := range ids {
			if pane, ok := l.app.panes[id]; ok {
				panes = append(panes, l.paneInfo(pane))
			}
		}
		return ok(req.ID, apiproto.PaneListResult{Panes: panes}), false

	case "pane.create", "pane.split":
		var p apiproto.PaneCreateParams
		if err := unmarshal(req.Params, &p); err != nil {
			return errResp(req.ID, apiproto.ErrInvalidParams, err.Error()), false
		}
		return l.paneCreate(req.ID, p), false

	case "pane.close":
		var p apiproto.PaneCloseParams
		if err := unmarshal(req.Params, &p); err != nil {
			return errResp(req.ID, apiproto.ErrInvalidParams, err.Error()), false
		}
		return l.paneClose(req.ID, p), false

	case "pane.send_keys":
		var p apiproto.PaneSendKeysParams
		if err := unmarshal(req.Params, &p); err != nil {
			return errResp(req.ID, apiproto.ErrInvalidParams, err.Error()), false
		}
		return l.paneSendKeys(req.ID, p), false

	case "pane.send_text":
		var p apiproto.PaneSendTextParams
		if err := unmarshal(req.Params, &p); err != nil {
			return errResp(req.ID, apiproto.ErrInvalidParams, err.Error()), false
		}
		return l.paneSendKeys(req.ID, apiproto.PaneSendKeysParams{PaneID: p.PaneID, Text: p.Text}), false

	case "pane.run":
		var p apiproto.PaneRunParams
		if err := unmarshal(req.Params, &p); err != nil {
			return errResp(req.ID, apiproto.ErrInvalidParams, err.Error()), false
		}
		return l.paneSendKeys(req.ID, apiproto.PaneSendKeysParams{PaneID: p.PaneID, Text: p.Text, PressEnter: true}), false

	case "pane.read":
		var p apiproto.PaneReadParams
		if err := unmarshal(req.Params, &p); err != nil {
			return errResp(req.ID, apiproto.ErrInvalidParams, err.Error()), false
		}
		return l.paneRead(req.ID, p), false

	case "pane.status":
		var p apiproto.PaneStatusParams
		if err := unmarshal(req.Params, &p); err != nil {
			return errResp(req.ID, apiproto.ErrInvalidParams, err.Error()), false
		}
		pane := l.app.resolvePane(p.PaneID)
		if pane == nil {
			return errResp(req.ID, apiproto.ErrPaneNotFound, "no such pane: "+p.PaneID), false
		}
		return ok(req.ID, paneStatusResult(pane)), false

	case "pane.focus":
		var p apiproto.PaneFocusParams
		if err := unmarshal(req.Params, &p); err != nil {
			return errResp(req.ID, apiproto.ErrInvalidParams, err.Error()), false
		}
		if !l.focusPane(p.PaneID) {
			return errResp(req.ID, apiproto.ErrPaneNotFound, "no such pane: "+p.PaneID), false
		}
		return ok(req.ID, apiproto.PaneFocusResult{PaneID: l.app.focusedPane(), Focused: true}), false

	case "pane.inspect":
		var p apiproto.PaneInspectParams
		if err := unmarshal(req.Params, &p); err != nil {
			return errResp(req.ID, apiproto.ErrInvalidParams, err.Error()), false
		}
		return l.paneInspect(req.ID, p), false

	case "pane.neighbor":
		var p apiproto.PaneNeighborParams
		if err := unmarshal(req.Params, &p); err != nil {
			return errResp(req.ID, apiproto.ErrInvalidParams, err.Error()), false
		}
		return l.paneNeighbor(req.ID, p), false

	case "pane.move":
		var p apiproto.PaneMoveParams
		if err := unmarshal(req.Params, &p); err != nil {
			return errResp(req.ID, apiproto.ErrInvalidParams, err.Error()), false
		}
		return l.paneMove(req.ID, p), false

	case "pane.rename":
		var p apiproto.PaneRenameParams
		if err := unmarshal(req.Params, &p); err != nil {
			return errResp(req.ID, apiproto.ErrInvalidParams, err.Error()), false
		}
		pane := l.app.resolvePane(p.PaneID)
		if pane == nil {
			return errResp(req.ID, apiproto.ErrPaneNotFound, "no such pane: "+p.PaneID), false
		}
		pane.Label = p.Label
		l.app.dirty = true
		l.broadcastState()
		return ok(req.ID, l.paneInfo(pane)), false

	case "pane.resize":
		var p apiproto.PaneResizeParams
		if err := unmarshal(req.Params, &p); err != nil {
			return errResp(req.ID, apiproto.ErrInvalidParams, err.Error()), false
		}
		pane := l.app.resolvePane(p.PaneID)
		if pane == nil {
			return errResp(req.ID, apiproto.ErrPaneNotFound, "no such pane: "+p.PaneID), false
		}
		l.resizeSplit(pane.ID, p.Direction, p.Amount)
		return ok(req.ID, l.paneInfo(pane)), false

	case "pane.zoom":
		var p apiproto.PaneZoomParams
		if err := unmarshal(req.Params, &p); err != nil {
			return errResp(req.ID, apiproto.ErrInvalidParams, err.Error()), false
		}
		t := l.app.activeTab()
		if t == nil {
			return errResp(req.ID, apiproto.ErrNotFound, "no active tab"), false
		}
		if p.Zoom == nil {
			t.zoom = !t.zoom
		} else {
			t.zoom = *p.Zoom
		}
		l.app.dirty = true
		l.broadcastState()
		return ok(req.ID, apiproto.PaneZoomResult{Zoomed: t.zoom}), false

	case "debug.pane":
		// Everything the status detector looks at, so a misclassified agent
		// can be diagnosed from the outside instead of by adding print
		// statements to the daemon.
		var p apiproto.PaneStatusParams
		if err := unmarshal(req.Params, &p); err != nil {
			return errResp(req.ID, apiproto.ErrInvalidParams, err.Error()), false
		}
		pane := l.app.resolvePane(p.PaneID)
		if pane == nil {
			return errResp(req.ID, apiproto.ErrPaneNotFound, "no such pane: "+p.PaneID), false
		}
		_, argv := pane.Actor.Foreground()
		reason, rule := l.paneDebugExplanation(pane)
		return ok(req.ID, apiproto.PaneDebugResult{
			PaneID:       pane.ID,
			Agent:        pane.Agent,
			AgentStatus:  string(pane.agentStatus()),
			RawState:     string(pane.AgentState),
			Seen:         pane.Seen,
			Busy:         pane.Actor.Busy(),
			Running:      pane.Running,
			Foreground:   argv,
			Title:        pane.Grid.Title(),
			Bottom:       pane.Grid.BottomLines(6),
			Reason:       reason,
			RuleID:       rule.ID,
			RuleSource:   rule.Source,
			RulePriority: rule.Priority,
			RuleRegion:   rule.Region,
		}), false

	case "pane.report":
		var p apiproto.PaneReportParams
		if err := unmarshal(req.Params, &p); err != nil {
			return errResp(req.ID, apiproto.ErrInvalidParams, err.Error()), false
		}
		return l.paneReport(req.ID, p), false

	case "fan.start":
		var p apiproto.FanStartParams
		if err := unmarshal(req.Params, &p); err != nil {
			return errResp(req.ID, apiproto.ErrInvalidParams, err.Error()), false
		}
		return l.fanStart(req.ID, p), false

	case "fan.list":
		var p apiproto.FanListParams
		_ = unmarshal(req.Params, &p)
		return l.fanList(req.ID, p), false

	case "fan.clean":
		var p apiproto.FanCleanParams
		if err := unmarshal(req.Params, &p); err != nil {
			return errResp(req.ID, apiproto.ErrInvalidParams, err.Error()), false
		}
		return l.fanClean(req.ID, p), false

	case "fan.review":
		var p apiproto.FanReviewParams
		if err := unmarshal(req.Params, &p); err != nil {
			return errResp(req.ID, apiproto.ErrInvalidParams, err.Error()), false
		}
		return l.fanReview(req.ID, p), false

	case "fan.promote":
		var p apiproto.FanPromoteParams
		if err := unmarshal(req.Params, &p); err != nil {
			return errResp(req.ID, apiproto.ErrInvalidParams, err.Error()), false
		}
		return l.fanPromote(req.ID, p), false

	case "pane.git":
		return l.openGitTool(), false

	case "wait.pane":
		var p apiproto.WaitPaneParams
		if err := unmarshal(req.Params, &p); err != nil {
			return errResp(req.ID, apiproto.ErrInvalidParams, err.Error()), false
		}
		return l.waitPane(req.ID, p, reply)

	case "wait.output":
		var p apiproto.WaitOutputParams
		if err := unmarshal(req.Params, &p); err != nil {
			return errResp(req.ID, apiproto.ErrInvalidParams, err.Error()), false
		}
		return l.waitOutput(req.ID, p, reply)

	case "server.shutdown":
		for _, id := range l.app.allPanes() {
			if pane, ok := l.app.panes[id]; ok {
				_ = pane.Actor.Close()
			}
		}
		return ok(req.ID, apiproto.ServerShutdownResult{OK: true}), false

	case "server.reload":
		if l.reload == nil {
			return errResp(req.ID, apiproto.ErrInternal, "configuration reload is unavailable"), false
		}
		if err := l.reload(); err != nil {
			return errResp(req.ID, apiproto.ErrInternal, "reload configuration: "+err.Error()), false
		}
		l.refreshAgentStatus()
		l.broadcastState()
		return ok(req.ID, apiproto.ServerReloadResult{OK: true}), false

	default:
		return errResp(req.ID, apiproto.ErrMethodNotFound, "unknown method: "+req.Method), false
	}
}

// paneDebugExplanation is the human-facing counterpart to evaluatePane. It
// deliberately uses the same live signals, while debug.pane keeps exposing
// the persisted raw and effective states for comparison.
func (l *Loop) paneDebugExplanation(pane *Pane) (string, agentstatus.RuleMatch) {
	if pane.Reported != "" && time.Since(pane.ReportedAt) < reportTTL {
		return "an integration report is authoritative while it is fresh", agentstatus.RuleMatch{}
	}

	in := agentstatus.Input{
		Title:  pane.Grid.Title(),
		Bottom: pane.Grid.BottomLines(6),
		Screen: pane.Grid.Lines(),
	}
	if pane.Agent != "" {
		v := l.agents.EvaluateAgentVerdict(pane.Agent, in)
		if v.SkipStateUpdate {
			return "the matching rule requested that this status update be skipped", v.Rule
		}
		if v.Rule.ID != "" {
			if v.State == agentstatus.Idle && time.Now().Before(pane.BusyUntil) {
				return "the matching idle rule is within the post-prompt busy grace period", v.Rule
			}
			return "the highest-priority matching manifest rule selected this state", v.Rule
		}
		return "no manifest rule matched; recognised agents default to idle", agentstatus.RuleMatch{}
	}

	v := l.agents.EvaluateVerdict("", in)
	if v.Rule.ID != "" {
		return "the highest-priority shared manifest rule selected this state", v.Rule
	}
	if isShell(pane.Running, pane.Cmd) {
		if pane.Actor.Busy() {
			return "the shell has a foreground job", agentstatus.RuleMatch{}
		}
		return "the shell has no foreground job", agentstatus.RuleMatch{}
	}
	if pane.Actor.Busy() {
		return "the process has a foreground job", agentstatus.RuleMatch{}
	}
	return "recent output activity determines this non-agent pane's state", agentstatus.RuleMatch{}
}

// --- workspaces ---

func (l *Loop) workspaceCreate(id string, p apiproto.WorkspaceCreateParams) apiproto.Response {
	cwd := p.Cwd
	if cwd == "" {
		if w := l.app.activeWorkspace(); w != nil {
			cwd = w.Cwd
		}
	}
	w := l.app.newWorkspace(p.Name, cwd)
	w.Named = p.Name != ""
	// A workspace with no tab has nowhere to put a pane, so it always starts
	// with one — the same reason `rook attach` spawns a shell rather than
	// landing you on a blank screen.
	w.addTab("")
	w.active().layout = nil

	if !p.Empty {
		if resp := l.paneCreate("ws-first-pane", apiproto.PaneCreateParams{Cwd: cwd}); resp.Error != nil {
			return resp
		}
	}
	l.app.dirty = true
	l.broadcastState()
	return ok(id, l.workspaceInfo(w))
}

func (l *Loop) workspaceClose(id string, p apiproto.WorkspaceCloseParams) apiproto.Response {
	target := p.WorkspaceID
	if target == "" {
		target = l.app.activeWS
	}
	w := l.app.workspace(target)
	if w == nil {
		return errResp(id, apiproto.ErrNotFound, "no such workspace: "+target)
	}
	for _, paneID := range w.panes() {
		if pane, ok := l.app.panes[paneID]; ok {
			_ = pane.Actor.Close()
			delete(l.app.panes, paneID)
		}
	}
	l.app.removeWorkspace(w.ID)
	l.app.dirty = true
	l.broadcastState()
	l.checkWaiters()
	return ok(id, apiproto.ClosedResult{ID: w.ID, Closed: true})
}

func (l *Loop) workspaceInfo(w *Workspace) apiproto.WorkspaceInfo {
	if w == nil {
		return apiproto.WorkspaceInfo{}
	}
	return apiproto.WorkspaceInfo{
		WorkspaceID: w.ID,
		Name:        w.displayName(),
		Cwd:         w.Cwd,
		Branch:      w.Branch,
		Tabs:        len(w.tabs),
		Panes:       len(w.panes()),
		Status:      string(l.rollup(w.panes())),
		Active:      w.ID == l.app.activeWS,
	}
}

// --- tabs ---

func (l *Loop) tabCreate(id string, p apiproto.TabCreateParams) apiproto.Response {
	w := l.app.activeWorkspace()
	if p.WorkspaceID != "" {
		w = l.app.workspace(p.WorkspaceID)
	}
	if w == nil {
		return errResp(id, apiproto.ErrNotFound, "no workspace to create a tab in")
	}

	t := w.addTab(p.Name)
	l.app.activeWS = w.ID
	if !p.Empty {
		if resp := l.paneCreate("tab-first-pane", apiproto.PaneCreateParams{Cmd: p.Cmd, Cwd: p.Cwd}); resp.Error != nil {
			return resp
		}
	}
	l.app.dirty = true
	l.broadcastState()
	return ok(id, l.tabInfo(w, t, len(w.tabs)))
}

func (l *Loop) tabClose(id string, p apiproto.TabCloseParams) apiproto.Response {
	w, t := l.app.resolveTab(p.TabID)
	if t == nil {
		return errResp(id, apiproto.ErrNotFound, "no such tab: "+p.TabID)
	}
	for _, paneID := range t.layout.Panes() {
		if pane, ok := l.app.panes[paneID]; ok {
			_ = pane.Actor.Close()
			delete(l.app.panes, paneID)
		}
	}
	w.removeTab(t.ID)
	// A workspace with no tabs left has nothing to show; drop it too, unless
	// it is the only one keeping the session alive.
	if len(w.tabs) == 0 && len(l.app.workspaces) > 1 {
		l.app.removeWorkspace(w.ID)
	}
	l.app.dirty = true
	l.broadcastState()
	l.checkWaiters()
	return ok(id, apiproto.ClosedResult{ID: t.ID, Closed: true})
}

func (l *Loop) tabInfo(w *Workspace, t *Tab, index int) apiproto.TabInfo {
	if w == nil || t == nil {
		return apiproto.TabInfo{}
	}
	if index == 0 {
		for i, tt := range w.tabs {
			if tt.ID == t.ID {
				index = i + 1
			}
		}
	}
	return apiproto.TabInfo{
		TabID:       t.ID,
		WorkspaceID: w.ID,
		Name:        t.displayName(index),
		Panes:       len(t.layout.Panes()),
		Status:      string(l.rollup(t.layout.Panes())),
		Active:      t.ID == w.activeTab,
		Zoomed:      t.zoom,
	}
}

// --- panes ---

// normalizeDirection maps the user-facing direction words onto split axes.
func normalizeDirection(d string) string {
	switch d {
	case "right", "left", "h", "horizontal":
		return dirHorizontal
	case "down", "up", "v", "vertical":
		return dirVertical
	}
	return ""
}

// autoDirection splits a wide pane side by side and a narrow or tall one top
// to bottom, so repeated splits don't produce unusable slivers. Same rule of
// thumb Herdr's agent guidance gives.
func autoDirection(r Rect) string {
	if r.W >= 2*r.H {
		return dirHorizontal
	}
	return dirVertical
}

func (l *Loop) paneCreate(id string, p apiproto.PaneCreateParams) apiproto.Response {
	// Panes need somewhere to live: a session with nothing open yet gets a
	// workspace and a tab on the spot.
	if l.app.activeWorkspace() == nil {
		cwd := p.Cwd
		if cwd == "" {
			cwd, _ = os.Getwd()
		}
		l.app.newWorkspace("", cwd).addTab("")
	}
	w := l.app.activeWorkspace()
	if w.active() == nil {
		w.addTab("")
	}
	tab := w.active()

	if p.Cmd == "" {
		p.Cmd = defaultShell()
	}
	if p.Cwd == "" {
		p.Cwd = w.Cwd
	}

	// Where does it go? The first pane in a tab takes the whole area; every
	// later one splits an existing pane, by default the focused one.
	from := p.From
	if from == "" {
		from = tab.focus
	} else if resolved := l.app.resolvePane(from); resolved != nil {
		from = resolved.ID
	}
	dir := normalizeDirection(p.Direction)

	if tab.layout != nil {
		if tab.layout.find(from) == nil {
			return errResp(id, apiproto.ErrPaneNotFound, "no such pane to split: "+from)
		}
		rect := l.app.rects()[from]
		if dir == "" {
			dir = autoDirection(rect)
		}
		if err := checkSplitFits(rect, dir); err != nil {
			return errResp(id, apiproto.ErrInvalidParams, err.Error())
		}
	}

	pane, err := l.startPane(w.newPaneID(), w, tab, p)
	if err != nil {
		return errResp(id, apiproto.ErrInternal, err.Error())
	}
	paneID := pane.ID

	if tab.layout == nil {
		tab.layout = newLeaf(paneID)
	} else {
		tab.layout.Split(from, paneID, dir)
	}

	if !p.NoFocus {
		tab.focus = paneID
		// A new pane un-zooms: creating a split you then can't see is never
		// what was meant.
		tab.zoom = false
	} else if tab.focus == "" {
		tab.focus = paneID
	}
	l.app.dirty = true
	l.emitPaneEvent(apiproto.EventPaneNew, pane)
	l.broadcastState()
	return ok(id, l.paneInfo(pane))
}

// checkSplitFits refuses a split that would leave either side too small to
// show anything, so a stray split can't wedge the layout.
func checkSplitFits(r Rect, dir string) error {
	if dir == dirHorizontal && (r.W-1)/2 < minPaneCols {
		return errors.New("not enough width to split; close a pane or zoom with prefix z")
	}
	if dir == dirVertical && (r.H-1)/2 < minPaneRows+titleRows {
		return errors.New("not enough height to split; close a pane or zoom with prefix z")
	}
	return nil
}

func (l *Loop) paneClose(id string, p apiproto.PaneCloseParams) apiproto.Response {
	pane := l.app.resolvePane(p.PaneID)
	if pane == nil {
		return errResp(id, apiproto.ErrPaneNotFound, "no such pane: "+p.PaneID)
	}
	w, tab := l.app.tabOf(pane.ID)
	if tab == nil {
		return errResp(id, apiproto.ErrPaneNotFound, "pane is not in any tab: "+pane.ID)
	}

	_ = pane.Actor.Close()
	delete(l.app.panes, pane.ID)
	tab.layout = tab.layout.Remove(pane.ID)

	if tab.focus == pane.ID {
		tab.focus = ""
		if remaining := tab.layout.Panes(); len(remaining) > 0 {
			tab.focus = remaining[0]
		}
	}
	if len(tab.layout.Panes()) <= 1 {
		tab.zoom = false
	}
	// An empty tab closes itself, and the last empty workspace with it —
	// otherwise closing your last pane leaves you staring at a blank screen
	// with no way back.
	if tab.layout == nil {
		w.removeTab(tab.ID)
		if len(w.tabs) == 0 && len(l.app.workspaces) > 1 {
			l.app.removeWorkspace(w.ID)
		}
	}

	l.app.dirty = true
	l.emitPaneEvent(apiproto.EventPaneClosed, pane)
	l.broadcastState()
	l.checkWaiters()
	return ok(id, apiproto.PaneCloseResult{PaneID: pane.ID, Closed: true})
}

// resizeStep is how far one resize keypress moves a divider, as a fraction
// of the split it belongs to.
const resizeStep = 0.05

// resizeSplit nudges the divider of the split containing paneID. left/right
// adjusts a horizontal split, up/down a vertical one.
func (l *Loop) resizeSplit(paneID, direction string, amountCells int) {
	axis := normalizeDirection(direction)
	tab := l.app.activeTab()
	if axis == "" || tab == nil {
		return
	}

	delta := resizeStep
	if amountCells != 0 {
		span := l.app.area().W
		if axis == dirVertical {
			span = l.app.area().H
		}
		if span > 0 {
			delta = float64(abs(amountCells)) / float64(span)
		}
	}
	if direction == "left" || direction == "up" {
		delta = -delta
	}
	if amountCells < 0 {
		delta = -delta
	}

	if tab.layout.Resize(paneID, delta, axis) {
		l.app.dirty = true
		l.broadcastState()
	}
}

// swapPane exchanges paneID with its neighbor in a direction, keeping both
// panes' content and process intact — only their positions/sizes in the
// split tree change. Focus stays on paneID, which now sits where the
// neighbor was.
func (l *Loop) swapPane(paneID, direction string) {
	tab := l.app.activeTab()
	if tab == nil {
		return
	}
	next := Neighbor(l.app.rects(), paneID, direction)
	if next == "" {
		return
	}
	if tab.layout.Swap(paneID, next) {
		l.app.dirty = true
		l.broadcastState()
	}
}

func (l *Loop) paneSendKeys(id string, p apiproto.PaneSendKeysParams) apiproto.Response {
	pane := l.app.resolvePane(p.PaneID)
	if pane == nil {
		return errResp(id, apiproto.ErrPaneNotFound, "no such pane: "+p.PaneID)
	}

	n, err := pane.Actor.Write([]byte(p.Text))
	if err != nil {
		return errResp(id, apiproto.ErrInternal, err.Error())
	}
	if p.PressEnter {
		// The Enter goes in a *separate* write, a beat later.
		//
		// Agent TUIs detect pastes by how the bytes arrive: text and its
		// carriage return in one chunk look like pasted content, so the CR
		// becomes a newline inside the input box and nothing is submitted.
		// This is exactly what left a briefing and a prompt sitting unsent in
		// Claude Code's composer. Writing the CR on its own, after a pause,
		// reads as a keypress.
		//
		// Written from a goroutine because the caller is the daemon's single
		// event loop and must never sleep; Actor.Write is mutex-guarded, so
		// this is safe from outside the loop.
		actor := pane.Actor
		go func() {
			time.Sleep(enterDelay)
			_, _ = actor.Write([]byte("\r"))
		}()
		n++
	}

	// Sending a prompt means work is starting. Marking the pane busy here,
	// rather than waiting for the detector's next tick, is what makes
	// "send, then wait for done" correct: otherwise the wait can match the
	// pane's leftover idle state and return before the agent even starts.
	if p.Text != "" {
		l.setAgentState(pane, agentstatus.Working)
		pane.Seen = pane.ID == l.app.focusedPane()
		pane.LastOutput = time.Now()
		pane.BusyUntil = time.Now().Add(sendGrace)
		l.app.dirty = true
		l.broadcastState()
	}
	return ok(id, apiproto.PaneSendKeysResult{OK: true, BytesWritten: n})
}

func (l *Loop) paneInspect(id string, p apiproto.PaneInspectParams) apiproto.Response {
	pane := l.app.resolvePane(p.PaneID)
	if pane == nil {
		return errResp(id, apiproto.ErrPaneNotFound, "no such pane: "+p.PaneID)
	}
	w, tab := l.app.tabOf(pane.ID)
	if w == nil || tab == nil {
		return errResp(id, apiproto.ErrPaneNotFound, "pane is not in a tab: "+pane.ID)
	}
	// A layout has coordinates only when its tab is visible. The neighbour
	// relation is nevertheless deterministic for a hidden tab using the same
	// session viewport used when it becomes visible.
	rects := tab.layout.Rects(l.app.area())
	result := apiproto.PaneInspectResult{Pane: l.paneInfo(pane), Focused: tab.focus == pane.ID, Zoomed: tab.zoom, Neighbors: map[string]string{}}
	if r, ok := rects[pane.ID]; ok {
		result.Rect = &apiproto.PaneRect{X: r.X, Y: r.Y, Width: r.W, Height: r.H}
	}
	for _, dir := range []string{"left", "right", "up", "down"} {
		if n := Neighbor(rects, pane.ID, dir); n != "" {
			result.Neighbors[dir] = n
		}
	}
	return ok(id, result)
}

func (l *Loop) paneNeighbor(id string, p apiproto.PaneNeighborParams) apiproto.Response {
	pane := l.app.resolvePane(p.PaneID)
	if pane == nil {
		return errResp(id, apiproto.ErrPaneNotFound, "no such pane: "+p.PaneID)
	}
	_, tab := l.app.tabOf(pane.ID)
	if tab == nil {
		return errResp(id, apiproto.ErrPaneNotFound, "pane is not in a tab: "+pane.ID)
	}
	if p.Direction != "left" && p.Direction != "right" && p.Direction != "up" && p.Direction != "down" {
		return errResp(id, apiproto.ErrInvalidParams, "direction must be left, right, up or down")
	}
	return ok(id, apiproto.PaneNeighborResult{PaneID: pane.ID, Direction: p.Direction, NeighborID: Neighbor(tab.layout.Rects(l.app.area()), pane.ID, p.Direction)})
}

func (l *Loop) paneMove(id string, p apiproto.PaneMoveParams) apiproto.Response {
	pane := l.app.resolvePane(p.PaneID)
	if pane == nil {
		return errResp(id, apiproto.ErrPaneNotFound, "no such pane: "+p.PaneID)
	}
	fromW, fromTab := l.app.tabOf(pane.ID)
	toW, toTab := l.app.resolveTab(p.TabID)
	if toW == nil || toTab == nil {
		return errResp(id, apiproto.ErrNotFound, "no such destination tab: "+p.TabID)
	}
	if fromTab == toTab {
		return errResp(id, apiproto.ErrInvalidParams, "pane is already in destination tab")
	}
	if fromTab.layout == nil {
		return errResp(id, apiproto.ErrInternal, "source tab has no layout")
	}

	target, dir := "", ""
	if toTab.layout != nil {
		target = p.From
		if target == "" {
			target = toTab.focus
		}
		if toTab.layout.find(target) == nil {
			return errResp(id, apiproto.ErrPaneNotFound, "no such destination pane: "+target)
		}
		dir = normalizeDirection(p.Direction)
		if dir == "" {
			dir = autoDirection(toTab.layout.Rects(l.app.area())[target])
		}
		if err := checkSplitFits(toTab.layout.Rects(l.app.area())[target], dir); err != nil {
			return errResp(id, apiproto.ErrInvalidParams, err.Error())
		}
	}

	// Remove only after validation succeeds. Retain the PTY and pane record;
	// an empty source tab is kept rather than silently deleted.
	fromTab.layout = fromTab.layout.Remove(pane.ID)
	if fromTab.focus == pane.ID {
		fromTab.focus = ""
		if fromTab.layout != nil {
			fromTab.focus = fromTab.layout.firstPane()
		}
	}
	if toTab.layout == nil {
		toTab.layout, toTab.focus = newLeaf(pane.ID), pane.ID
	} else {
		toTab.layout.Split(target, pane.ID, dir)
		if !p.NoFocus {
			toTab.focus, toTab.zoom = pane.ID, false
		}
	}
	_ = fromW
	l.app.dirty = true
	l.broadcastState()
	return ok(id, apiproto.PaneMoveResult{PaneID: pane.ID, TabID: toTab.ID})
}

// paneReport records what an agent integration says about itself.
func (l *Loop) paneReport(id string, p apiproto.PaneReportParams) apiproto.Response {
	pane := l.app.resolvePane(p.PaneID)
	if pane == nil {
		return errResp(id, apiproto.ErrPaneNotFound, "no such pane: "+p.PaneID)
	}
	if p.SessionRef != "" {
		pane.AgentSession = p.SessionRef
	}
	if p.Agent != "" && pane.Agent == "" {
		// A pane whose command rookery does not recognise is still an agent if
		// an integration is reporting for it.
		pane.Agent = p.Agent
	}

	if !p.KeepStatus {
		switch agentstatus.State(p.Status) {
		case agentstatus.Idle, agentstatus.Working, agentstatus.Blocked:
			pane.Reported = agentstatus.State(p.Status)
			pane.ReportedAt = time.Now()
			if pane.Reported == agentstatus.Working {
				// A report of "working" is a firm one: drop the post-send grace,
				// which exists only to cover the gap this report just closed.
				pane.BusyUntil = time.Time{}
			}
			l.setAgentState(pane, pane.Reported)
		case "":
			pane.Reported, pane.ReportedAt = "", time.Time{}
		default:
			return errResp(id, apiproto.ErrInvalidParams, "status must be idle, working, blocked or empty")
		}
	}

	l.app.dirty = true
	l.broadcastState()
	l.checkWaiters()
	return ok(id, apiproto.PaneReportResult{PaneID: pane.ID, Status: p.Status})
}

func (l *Loop) paneRead(id string, p apiproto.PaneReadParams) apiproto.Response {
	pane := l.app.resolvePane(p.PaneID)
	if pane == nil {
		return errResp(id, apiproto.ErrPaneNotFound, "no such pane: "+p.PaneID)
	}

	source := p.Source
	if source == "" {
		source = "screen"
	}
	format := p.Format
	if format == "" {
		format = "plain"
	}
	ansi := format == "ansi"

	var text string
	var truncated bool
	switch source {
	case "scrollback":
		text, truncated = pane.Grid.Scrollback(p.Lines, ansi)
	default:
		source = "screen"
		if ansi {
			text = pane.Grid.RenderANSI()
		} else {
			text = pane.Grid.RenderPlain()
		}
	}

	// Reading a pane counts as seeing it, the same way focusing does: an
	// agent that has collected its sibling's result shouldn't leave that
	// sibling flagged as still wanting attention.
	if !pane.Seen {
		pane.Seen = true
		l.broadcastState()
	}

	return ok(id, apiproto.PaneReadResult{
		PaneID:    pane.ID,
		Source:    source,
		Format:    format,
		Text:      text,
		Revision:  pane.Revision,
		Truncated: truncated,
	})
}

func (l *Loop) paneInfo(pane *Pane) apiproto.PaneInfo {
	info := pane.toInfo()
	if w, t := l.app.tabOf(pane.ID); w != nil {
		info.WorkspaceID, info.TabID = w.ID, t.ID
	}
	return info
}

func paneStatusResult(pane *Pane) apiproto.PaneStatusResult {
	cols, rows := pane.Grid.Size()
	result := apiproto.PaneStatusResult{
		PaneID:      pane.ID,
		Status:      pane.Status,
		Agent:       pane.Agent,
		AgentStatus: string(pane.agentStatus()),
		PID:         pane.Actor.PID(),
		Cols:        cols,
		Rows:        rows,
		StartedAt:   pane.CreatedAt.Format(time.RFC3339),
	}
	if pane.Status == "exited" {
		code := pane.ExitCode
		result.ExitCode = &code
	}
	return result
}

// --- mouse ---

// handleMouse turns a click, drag or wheel in the content area into the right
// thing: forwarding to a program that asked for mouse reporting, dragging a
// divider, focusing a pane, or scrolling.
func (l *Loop) handleMouse(m attachproto.Mouse) {
	rects := l.app.rects()

	// A drag that began on a divider keeps resizing that divider even once
	// the pointer wanders off it, which is what makes dragging feel like
	// dragging rather than a series of clicks.
	if m.Kind == "drag" && l.app.dragPane != "" {
		l.dragDivider(m)
		return
	}
	// A release ends whatever gesture was running. When that gesture was a
	// divider drag it is Rook's own and stops here; otherwise it still has to
	// reach the program that saw the press, so this falls through.
	if m.Kind == "release" {
		dividerDrag := l.app.dragPane != ""
		l.app.dragPane, l.app.dragAxis = "", ""
		if dividerDrag {
			l.app.mousePane = ""
			return
		}
	}

	if m.Kind == "press" {
		l.app.mousePane = ""
		if d := l.dividerAt(m.X, m.Y); d != nil {
			l.app.dragPane, l.app.dragAxis = d.APane, d.Dir
			l.app.dragX, l.app.dragY = m.X, m.Y
			return
		}
	}

	paneID := l.app.mouseTarget(rects, m)
	if m.Kind == "release" {
		l.app.mousePane = ""
	}
	if paneID == "" {
		return
	}
	pane := l.app.panes[paneID]
	if pane == nil {
		return
	}

	// Programs that turned on mouse reporting own the mouse inside their own
	// pane: an agent's TUI has clickable things of its own, and swallowing
	// those clicks to move focus would break it.
	if pane.Grid.MouseEnabled() {
		if m.Kind == "press" {
			l.app.mousePane = paneID
			if paneID != l.app.focusedPane() {
				l.focusPane(paneID)
			}
		}
		rect := rects[paneID]
		withBorder := l.bordersOn(len(rects))
		content := paneContentRect(rect, !withBorder && len(rects) > 1, withBorder)
		col, row := m.X-content.X+1, m.Y-content.Y+1
		if m.Kind != "press" {
			// The pointer may have left the pane mid-gesture. A real terminal
			// keeps reporting, pinned to the edge, so clamp rather than drop:
			// dropping is what leaves the button stuck down.
			col = min(max(col, 1), content.W)
			row = min(max(row, 1), content.H)
		}
		l.forwardMouse(pane, m, col, row, content.W, content.H)
		return
	}

	switch m.Kind {
	case "press":
		if paneID != l.app.focusedPane() {
			l.focusPane(paneID)
		}
	case "wheel":
		// A wheel over a program that did not ask for mouse reporting scrolls
		// the pane's own history. It used to send arrow keys, which in a shell
		// walks command history instead — the one thing a wheel should never
		// do.
		delta := -wheelLines
		if m.Button == "down" {
			delta = wheelLines
		}
		l.scrollBy(pane, delta)
	}
}

// dragDivider applies pointer movement to the split being dragged.
func (l *Loop) dragDivider(m attachproto.Mouse) {
	delta := m.X - l.app.dragX
	dir := "right"
	if l.app.dragAxis == dirVertical {
		delta = m.Y - l.app.dragY
		dir = "down"
	}
	if delta == 0 {
		return
	}
	if delta < 0 {
		dir = "left"
		if l.app.dragAxis == dirVertical {
			dir = "up"
		}
	}
	l.resizeSplit(l.app.dragPane, dir, abs(delta))
	l.app.dragX, l.app.dragY = m.X, m.Y
}

func (l *Loop) dividerAt(x, y int) *Divider {
	t := l.app.activeTab()
	if t == nil || t.zoom {
		return nil
	}
	for _, d := range t.layout.Dividers(l.app.area()) {
		if x >= d.X && x < d.X+d.W && y >= d.Y && y < d.Y+d.H {
			return &d
		}
	}
	return nil
}

func paneAt(rects map[string]Rect, x, y int) string {
	for id, r := range rects {
		if x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H {
			return id
		}
	}
	return ""
}

// mouseTarget picks which pane an event belongs to. A press goes to whatever
// sits under the pointer; the drag and release that continue a gesture a
// mouse-reporting pane claimed stay with that pane even once the pointer has
// left it.
func (a *App) mouseTarget(rects map[string]Rect, m attachproto.Mouse) string {
	if m.Kind != "press" && a.mousePane != "" {
		if _, ok := rects[a.mousePane]; ok {
			return a.mousePane
		}
	}
	return paneAt(rects, m.X, m.Y)
}

// forwardMouse re-encodes an event for the program inside a pane, in
// pane-local 1-based coordinates bounded by the pane's content size.
func (l *Loop) forwardMouse(pane *Pane, m attachproto.Mouse, col, row, w, h int) {
	if s := mouseReport(m, col, row, w, h, pane.Grid.MouseSGR()); s != "" {
		_, _ = pane.Actor.Write([]byte(s))
	}
}

// mouseReport encodes one event as the bytes a program that asked for mouse
// reporting expects on its stdin, or "" if there is nothing to report.
func mouseReport(m attachproto.Mouse, col, row, w, h int, sgr bool) string {
	if col < 1 || row < 1 || col > w || row > h {
		return ""
	}
	code, release := mouseButtonCode(m)
	if code < 0 {
		return ""
	}
	if m.Shift {
		code |= 4
	}
	if m.Alt {
		code |= 8
	}
	if m.Ctrl {
		code |= 16
	}
	return encodeMouse(code, col, row, release, sgr)
}

func mouseButtonCode(m attachproto.Mouse) (code int, release bool) {
	if m.Kind == "wheel" {
		if m.Button == "down" {
			return 65, false
		}
		return 64, false
	}
	base := 0
	switch m.Button {
	case "middle":
		base = 1
	case "right":
		base = 2
	}
	if m.Kind == "drag" {
		return base + 32, false
	}
	return base, m.Kind == "release"
}

// encodeMouse writes an X10 or SGR mouse report. SGR (1006) is preferred
// whenever the program asked for it: the legacy encoding adds 32 to each
// coordinate and packs them into single bytes, so it silently breaks past
// column 223 — which is an ordinary width for a modern terminal.
func encodeMouse(code, col, row int, release, sgr bool) string {
	if sgr {
		final := "M"
		if release {
			final = "m"
		}
		return "\x1b[<" + strconv.Itoa(code) + ";" + strconv.Itoa(col) + ";" + strconv.Itoa(row) + final
	}
	if release {
		code = 3
	}
	return "\x1b[M" + string(rune(32+code)) + string(rune(32+col)) + string(rune(32+row))
}

func ok(id string, result any) apiproto.Response {
	return apiproto.Response{ID: id, Result: result}
}

func errResp(id, code, message string) apiproto.Response {
	return apiproto.Response{ID: id, Error: &apiproto.ErrorBody{Code: code, Message: message}}
}

func unmarshal(raw json.RawMessage, v any) error {
	if len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, v)
}
