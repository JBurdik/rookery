package state

import (
	"strconv"
	"strings"

	"github.com/jirkab/rookery/internal/icons"
	"github.com/jirkab/rookery/internal/termgrid"
)

// attachClientConn is the loop's-eye view of one attached client. send is
// drained by that client's own writer goroutine (owned by internal/attachserver).
type attachClientConn struct {
	id   uint64
	send chan any
	cols int
	rows int
	// focused is whether this client's terminal window has focus. A sound is
	// enough when you are looking at the terminal; an OS notification is what
	// reaches you when you are in another app entirely.
	focused bool
}

// App is the daemon's in-memory state. Only internal/state/loop.go ever
// mutates it — see loop.go's doc comment for the single-goroutine rule.
//
// The shape is Herdr's: a session holds workspaces, a workspace holds tabs, a
// tab holds a layout of panes. Panes live in one flat map keyed by their
// fully-qualified id ("w1:p3") so any lookup is one hop regardless of how
// deep the tree gets.
type App struct {
	Session string

	workspaces []*Workspace
	activeWS   string
	nextWS     int

	panes map[string]*Pane

	// viewport is the size of the drawing area, as reported by the most
	// recently resized client.
	//
	// ponytail: one viewport for all clients, last writer wins. Per-client
	// viewports mean per-client layouts and per-client frames, which is a
	// substantially larger daemon. Noted in future-plan.md.
	viewCols, viewRows int

	// dirty marks chrome-level changes (focus, layout, agent status) that
	// need a repaint even when no pane produced any output.
	dirty bool

	// An in-progress divider drag. Held across events so the pointer can
	// wander off the divider mid-drag and still keep resizing it, which is
	// what makes dragging feel like dragging.
	dragPane     string
	dragAxis     string
	dragX, dragY int

	// Glyph theme, adopted from the attached client so pane headers match the
	// sidebar. spinnerFrame is the last animation frame drawn, used to decide
	// when an animating pane needs a repaint.
	icons        icons.Set
	spinner      []string
	spinnerFrame string
	// Colours for the chrome the daemon draws: pane headers and dividers.
	accent    termgrid.Color
	headerFG  termgrid.Color
	borderFG  termgrid.Color
	spinnerFG termgrid.Color
	doneFG    termgrid.Color
	// borders is the pane-box mode the attached client asked for: auto,
	// always or never.
	borders string
	// blink flashes a finished pane's border; blinkOn is the last phase drawn.
	blink   bool
	blinkOn bool
	// managerCmd is the agent the manager bar starts, from the client's config.
	managerCmd string
	// managerAwaiting is set between asking the manager something and its turn
	// ending, so only a reply to an actual question reaches the bar.
	managerAwaiting bool
	// nextFan numbers unnamed fan-out runs.
	nextFan int

	// Event streams (`rook watch`). watchDropped counts events discarded
	// because a consumer was too slow, so it can be reported rather than
	// hidden.
	watchers     []*watcher
	nextWatcher  uint64
	watchDropped int
	// sendQueue holds text waiting for a pane to be ready for input, keyed by
	// pane id — see queueSend for why writing immediately does not work.
	sendQueue map[string][]queuedSend

	clients map[uint64]*attachClientConn
	waiters []*waiter
}

func newApp(session string) *App {
	return &App{
		Session:   session,
		panes:     make(map[string]*Pane),
		clients:   make(map[uint64]*attachClientConn),
		viewCols:  defaultCols,
		viewRows:  defaultRows,
		icons:     icons.For(icons.ThemeNerd),
		spinner:   icons.SpinnerFor("dots"),
		accent:    110,
		headerFG:  244,
		borderFG:  238,
		spinnerFG: 208,
		doneFG:    2,
		borders:   BordersAuto,
		blink:     true,
	}
}

// anyFocused reports whether a human is currently looking at any attached
// client.
//
// A client that has never reported focus counts as focused: plenty of
// terminals do not support focus reporting, and silently swallowing every
// notification on those is worse than occasionally sending one you did not
// need.
func (a *App) anyFocused() bool {
	for _, c := range a.clients {
		if c.focused {
			return true
		}
	}
	return false
}

// --- workspaces ---

func (a *App) newWorkspace(name, cwd string) *Workspace {
	a.nextWS++
	w := &Workspace{
		ID:   "w" + strconv.Itoa(a.nextWS),
		Name: name,
		Cwd:  cwd,
	}
	w.Branch = gitBranch(cwd)
	if w.Branch == "" && cwd != "" {
		w.Branch = gitBranchSlow(cwd)
	}
	a.workspaces = append(a.workspaces, w)
	a.activeWS = w.ID
	return w
}

func (a *App) workspace(id string) *Workspace {
	for _, w := range a.workspaces {
		if w.ID == id {
			return w
		}
	}
	return nil
}

func (a *App) activeWorkspace() *Workspace { return a.workspace(a.activeWS) }

func (a *App) activeTab() *Tab {
	w := a.activeWorkspace()
	if w == nil {
		return nil
	}
	return w.active()
}

func (a *App) removeWorkspace(id string) {
	for i, w := range a.workspaces {
		if w.ID != id {
			continue
		}
		a.workspaces = append(a.workspaces[:i], a.workspaces[i+1:]...)
		if a.activeWS == id {
			a.activeWS = ""
			if len(a.workspaces) > 0 {
				a.activeWS = a.workspaces[min(i, len(a.workspaces)-1)].ID
			}
		}
		return
	}
}

// --- lookups ---

// findTab resolves a tab id from any workspace.
func (a *App) findTab(id string) (*Workspace, *Tab) {
	for _, w := range a.workspaces {
		if t := w.tab(id); t != nil {
			return w, t
		}
	}
	return nil, nil
}

// tabOf returns the tab a pane belongs to.
func (a *App) tabOf(paneID string) (*Workspace, *Tab) {
	for _, w := range a.workspaces {
		for _, t := range w.tabs {
			for _, p := range t.layout.Panes() {
				if p == paneID {
					return w, t
				}
			}
		}
	}
	return nil, nil
}

// resolvePane accepts a fully-qualified id ("w1:p3"), a bare pane id ("p3",
// meaning the active workspace), or "" for the focused pane. Bare ids exist
// because typing the workspace prefix for every call gets old fast when you
// only have one workspace open, which is the common case.
func (a *App) resolvePane(id string) *Pane {
	if id == "" {
		return a.panes[a.focusedPane()]
	}
	if p, ok := a.panes[id]; ok {
		return p
	}
	if !strings.Contains(id, ":") && a.activeWS != "" {
		if p, ok := a.panes[a.activeWS+":"+id]; ok {
			return p
		}
	}
	return nil
}

// resolveTab accepts "w1:t2", a bare "t2" in the active workspace, or "" for
// the active tab.
func (a *App) resolveTab(id string) (*Workspace, *Tab) {
	if id == "" {
		return a.activeWorkspace(), a.activeTab()
	}
	if w, t := a.findTab(id); t != nil {
		return w, t
	}
	if !strings.Contains(id, ":") && a.activeWS != "" {
		return a.findTab(a.activeWS + ":" + id)
	}
	return nil, nil
}

// focusedPane is the pane keystrokes go to: the focus of the active tab.
func (a *App) focusedPane() string {
	if t := a.activeTab(); t != nil {
		return t.focus
	}
	return ""
}

// visiblePanes lists the panes of the active tab, in layout order.
func (a *App) visiblePanes() []string {
	if t := a.activeTab(); t != nil {
		return t.layout.Panes()
	}
	return nil
}

// allPanes lists every pane in the session, workspace then tab order. Used
// for anything that has to touch all of them: shutdown, status refresh, the
// agents panel.
func (a *App) allPanes() []string {
	var out []string
	for _, w := range a.workspaces {
		out = append(out, w.panes()...)
	}
	return out
}

// area is the rectangle the active tab's layout is drawn into.
func (a *App) area() Rect {
	return Rect{W: a.viewCols, H: a.viewRows}
}

// rects returns where each visible pane sits. A zoomed tab reports only its
// focused pane, filling everything.
func (a *App) rects() map[string]Rect {
	t := a.activeTab()
	if t == nil {
		return nil
	}
	if t.zoom {
		if _, ok := a.panes[t.focus]; ok {
			return map[string]Rect{t.focus: a.area()}
		}
	}
	return t.layout.Rects(a.area())
}
