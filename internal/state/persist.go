package state

import (
	"encoding/json"
	"os"
	"slices"
	"time"

	"github.com/jirkab/rookery/internal/agentstatus"
	"github.com/jirkab/rookery/internal/apiproto"
	"github.com/jirkab/rookery/internal/pty"
	"github.com/jirkab/rookery/internal/session"
	"github.com/jirkab/rookery/internal/termgrid"
)

// Layout persistence.
//
// Restarting the daemon is routine here — a new binary changes nothing until
// the old daemon goes away, which is why `just install` kills it — and until
// now that took every workspace, tab and split with it. The tree is saved
// beside the session's sockets and read back at startup.
//
// ponytail: panes come back as a shell in the directory they were in, not as
// whatever was running in them. The structure is what is tedious to rebuild;
// relaunching eight agents (or a `tail -f`, or a half-finished `git rebase`)
// because a daemon restarted is a thing a multiplexer should never do on its
// own. The commands are recorded anyway, so "restore the command too" is a
// flag away if it ever turns out to be wanted.

type snapshot struct {
	Session    string     `json:"session"`
	Workspaces []wsSnap   `json:"workspaces"`
	ActiveWS   string     `json:"active_workspace,omitempty"`
	NextWS     int        `json:"next_workspace"`
	Panes      []paneSnap `json:"panes"`
}

type wsSnap struct {
	ID        string    `json:"id"`
	Name      string    `json:"name,omitempty"`
	Named     bool      `json:"named,omitempty"`
	Cwd       string    `json:"cwd,omitempty"`
	Tabs      []tabSnap `json:"tabs"`
	ActiveTab string    `json:"active_tab,omitempty"`
	NextTab   int       `json:"next_tab"`
	NextPane  int       `json:"next_pane"`
}

type tabSnap struct {
	ID     string  `json:"id"`
	Name   string  `json:"name,omitempty"`
	Layout *Layout `json:"layout,omitempty"`
	Focus  string  `json:"focus,omitempty"`
	Zoom   bool    `json:"zoom,omitempty"`
}

type paneSnap struct {
	ID    string   `json:"id"`
	Label string   `json:"label,omitempty"`
	Cmd   string   `json:"cmd,omitempty"`
	Args  []string `json:"args,omitempty"`
	Cwd   string   `json:"cwd,omitempty"`
	// Agent and SessionRef are integration metadata, not live state. Keeping
	// them lets an explicit pane.resume work after a daemon restart.
	Agent      string `json:"agent,omitempty"`
	SessionRef string `json:"session_ref,omitempty"`
}

// buildSnapshot captures the structure — and only the structure. Nothing in
// here changes on a status tick, which is what lets persist() decide whether
// to write by comparing bytes.
func (l *Loop) buildSnapshot() snapshot {
	s := snapshot{Session: l.app.Session, ActiveWS: l.app.activeWS, NextWS: l.app.nextWS}
	for _, w := range l.app.workspaces {
		ws := wsSnap{
			ID: w.ID, Name: w.Name, Named: w.Named, Cwd: w.Cwd,
			ActiveTab: w.activeTab, NextTab: w.nextTab, NextPane: w.nextPane,
		}
		for _, t := range w.tabs {
			ws.Tabs = append(ws.Tabs, tabSnap{ID: t.ID, Name: t.Name, Layout: t.layout, Focus: t.focus, Zoom: t.zoom})
		}
		s.Workspaces = append(s.Workspaces, ws)
	}
	// In id order, not map order: persist decides whether to write by
	// comparing bytes, and a map's random iteration would make every
	// snapshot differ from the last one.
	ids := make([]string, 0, len(l.app.panes))
	for id := range l.app.panes {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		p := l.app.panes[id]
		s.Panes = append(s.Panes, paneSnap{ID: p.ID, Label: p.Label, Cmd: p.Cmd, Args: p.Args, Cwd: p.Cwd, Agent: p.Agent, SessionRef: p.AgentSession})
	}
	return s
}

// persist writes the tree out when it has actually changed. It is called from
// broadcastState — the one funnel every structural change already goes
// through — and the byte comparison is what keeps four status ticks a second
// from turning into four file writes a second.
func (l *Loop) persist() {
	data, err := json.Marshal(l.buildSnapshot())
	if err != nil || string(data) == l.lastSnapshot {
		return
	}
	l.lastSnapshot = string(data)
	// A failed write costs the layout, not the session: there is nothing
	// useful to do about it at this point, and the daemon must keep running.
	_ = os.WriteFile(session.LayoutPath(l.app.Session), data, 0o600)
}

// Restore rebuilds the tree saved by a previous daemon, spawning a shell for
// each pane that was open. Called once before Run; a session that has
// anything in it already is left alone.
func (l *Loop) Restore() {
	if len(l.app.workspaces) > 0 {
		return
	}
	data, err := os.ReadFile(session.LayoutPath(l.app.Session))
	if err != nil {
		return
	}
	var snap snapshot
	if json.Unmarshal(data, &snap) != nil {
		return
	}

	saved := map[string]paneSnap{}
	for _, p := range snap.Panes {
		saved[p.ID] = p
	}

	l.app.nextWS = snap.NextWS
	for _, ws := range snap.Workspaces {
		w := &Workspace{
			ID: ws.ID, Name: ws.Name, Named: ws.Named, Cwd: ws.Cwd,
			activeTab: ws.ActiveTab, nextTab: ws.NextTab, nextPane: ws.NextPane,
		}
		w.Branch = gitBranch(ws.Cwd)
		for _, ts := range ws.Tabs {
			t := &Tab{ID: ts.ID, Name: ts.Name, layout: ts.Layout, focus: ts.Focus, zoom: ts.Zoom}
			for _, paneID := range t.layout.Panes() {
				// A layout leaf with no matching pane record is a file written
				// by an older (or corrupted) rookery. Drop the leaf rather than
				// spawning a nameless pane for it.
				p, known := saved[paneID]
				if !known || paneID == "" {
					t.layout = t.layout.Remove(paneID)
					continue
				}
				if _, err := l.startPane(paneID, w, t, apiproto.PaneCreateParams{
					Label: p.Label, Cwd: p.Cwd, Agent: p.Agent, AgentSession: p.SessionRef,
				}); err != nil {
					// A pane whose directory has since been deleted must not
					// take the rest of the layout down with it.
					t.layout = t.layout.Remove(paneID)
				}
			}
			if t.layout == nil {
				continue
			}
			if _, ok := l.app.panes[t.focus]; !ok {
				t.focus = t.layout.Panes()[0]
			}
			w.tabs = append(w.tabs, t)
		}
		if len(w.tabs) == 0 {
			continue
		}
		if w.tab(w.activeTab) == nil {
			w.activeTab = w.tabs[0].ID
		}
		l.app.workspaces = append(l.app.workspaces, w)
	}

	l.app.activeWS = snap.ActiveWS
	if l.app.workspace(l.app.activeWS) == nil && len(l.app.workspaces) > 0 {
		l.app.activeWS = l.app.workspaces[0].ID
	}
}

// startPane spawns a pane's process and registers it. Shared by pane.create
// and by layout restore, so a restored pane is in every respect an ordinary
// one — same environment, same detection, same lifecycle.
func (l *Loop) startPane(paneID string, w *Workspace, tab *Tab, p apiproto.PaneCreateParams) (*Pane, error) {
	if p.Cmd == "" {
		p.Cmd = defaultShell()
	}
	if p.Cwd == "" {
		p.Cwd = w.Cwd
	}
	cols, rows := p.Cols, p.Rows
	if cols <= 0 || rows <= 0 {
		cols, rows = l.app.viewCols, l.app.viewRows
	}
	if cols <= 0 || rows <= 0 {
		cols, rows = defaultCols, defaultRows
	}

	// Every pane learns where it is, so an agent running inside one can call
	// back into `rook pane ...` without being told anything.
	env := os.Environ()
	for k, v := range p.Env {
		env = append(env, k+"="+v)
	}
	env = append(env,
		"ROOK_SESSION="+l.app.Session,
		"ROOK_PANE="+paneID,
		"ROOK_TAB="+tab.ID,
		"ROOK_WORKSPACE="+w.ID,
		"ROOK_ENV=1",
	)

	actor, err := pty.Spawn(p.Cmd, p.Args, p.Cwd, env, cols, rows,
		func(data []byte) { l.NotifyPTYOutput(paneID, data) },
		func(_ error, code int) { l.NotifyPTYExit(paneID, code) },
	)
	if err != nil {
		return nil, err
	}

	pane := &Pane{
		ID:           paneID,
		Label:        p.Label,
		Cmd:          p.Cmd,
		Args:         p.Args,
		Cwd:          p.Cwd,
		Grid:         termgrid.New(cols, rows),
		Actor:        actor,
		Status:       "running",
		CreatedAt:    time.Now(),
		Agent:        l.agents.Agent(p.Cmd, p.Args),
		AgentSession: p.AgentSession,
		AgentState:   agentstatus.Working, // starting up counts as busy
		Seen:         true,
		LastOutput:   time.Now(),
	}
	if pane.Agent == "" {
		// A restored shell cannot be re-detected as its old agent, but its
		// persisted metadata is still useful as a resume source.
		pane.Agent = p.Agent
	}
	l.app.panes[paneID] = pane
	return pane, nil
}
