// Package state is the daemon's brain: a single goroutine owns all pane and
// layout mutation. Every other goroutine (PTY readers, API connections,
// attach connections) only ever pushes an event onto a channel — this is
// the Go analogue of Bubble Tea's single-threaded Update, applied at the
// daemon level, so AppState never needs a mutex.
package state

import (
	"os/exec"
	"time"

	"github.com/jirkab/rookery/internal/agentstatus"
	"github.com/jirkab/rookery/internal/apiproto"
	"github.com/jirkab/rookery/internal/attachproto"
	"github.com/jirkab/rookery/internal/icons"
	"github.com/jirkab/rookery/internal/notify"
	"github.com/jirkab/rookery/internal/termgrid"
)

const (
	flushInterval  = 33 * time.Millisecond  // ~30fps frame broadcast
	statusInterval = 250 * time.Millisecond // agent-status re-evaluation
	// idleAfter is how long a pane running an unrecognised command must stay
	// quiet before it counts as idle rather than working.
	idleAfter = 900 * time.Millisecond
	// reportTTL is how long an integration's report stays authoritative. Long
	// enough to cover a slow turn, short enough that an agent killed mid-turn
	// (its hooks never firing again) falls back to the detector rather than
	// looking busy forever.
	reportTTL = 10 * time.Minute
	// blinkInterval is one half-cycle of the attention blink; blinkFor is how
	// long a finished pane keeps blinking. Long enough to catch an eye that
	// was elsewhere, short enough not to become wallpaper.
	blinkInterval = 400 * time.Millisecond
	blinkFor      = 4 * time.Second
)

type ptyOutputMsg struct {
	paneID string
	data   []byte
}

type ptyExitMsg struct {
	paneID   string
	exitCode int
}

type apiMsg struct {
	req   apiproto.Request
	reply chan apiproto.Response
}

// ClientTheme is the look a connecting client asks for. The daemon draws the
// pane headers and dividers itself, so it has to be told, or its chrome would
// clash with the sidebar the client draws.
type ClientTheme struct {
	Icons        string
	Spinner      string
	Accent       string
	HeaderFG     string
	Border       string
	SpinnerColor string
	// Borders is the pane-box mode: auto, always or never.
	Borders string
	// DoneColor is what a finished pane's border flashes.
	DoneColor string
	// Blink enables that flash at all.
	Blink bool
}

type attachConnectMsg struct {
	clientID uint64
	send     chan any
	cols     int
	rows     int
	theme    ClientTheme
}

type attachDisconnectMsg struct{ clientID uint64 }

type clientFocusMsg struct {
	clientID uint64
	focused  bool
}

type attachInputMsg struct {
	clientID uint64
	data     string
}

type attachResizeMsg struct {
	clientID uint64
	cols     int
	rows     int
}

// attachCmdMsg is every other client -> server message. They all reduce to
// "do this named thing, maybe to this target", so one channel and one struct
// beats a channel per verb.
type attachCmdMsg struct {
	clientID uint64
	kind     string
	target   string
	text     string
	dir      string
	mouse    attachproto.Mouse
}

// Loop runs the daemon's central event loop.
type Loop struct {
	app     *App
	version string
	sound   *notify.Player
	agents  *agentstatus.Registry
	reload  func() error

	// lastSnapshot is the layout as it was last written to disk, so an
	// unchanged tree costs a marshal rather than a file write.
	lastSnapshot string

	ptyMsgs     chan ptyOutputMsg
	ptyExits    chan ptyExitMsg
	apiMsgs     chan apiMsg
	attachConn  chan attachConnectMsg
	attachDisc  chan attachDisconnectMsg
	attachIn    chan attachInputMsg
	attachSize  chan attachResizeMsg
	attachCmds  chan attachCmdMsg
	clientFocus chan clientFocusMsg
	watchAdd    chan watchAddMsg
	watchDel    chan chan apiproto.Event
}

// SetSound installs the notification player. The daemon owns it rather than
// the client so a ping still happens when you have detached — which is
// exactly when you most want to be told an agent got stuck.
func (l *Loop) SetSound(p *notify.Player) { l.sound = p }

// SetAgentRegistry installs the agent manifests. Defaults are used if this is
// never called, so a daemon started without config still recognises agents.
func (l *Loop) SetAgentRegistry(r *agentstatus.Registry) {
	if r != nil {
		l.agents = r
	}
}

// SetReloader supplies the daemon-owned configuration reload operation. It is
// called from the loop goroutine, which keeps sound and agent registry changes
// serialized with every other state change.
func (l *Loop) SetReloader(reload func() error) { l.reload = reload }

// NewLoop creates a Loop for the given session name. version is reported by
// ping and shown in `rook ls`.
func NewLoop(session, version string) *Loop {
	registry, _ := agentstatus.Load("")
	return &Loop{
		app:         newApp(session),
		version:     version,
		agents:      registry,
		ptyMsgs:     make(chan ptyOutputMsg, 256),
		ptyExits:    make(chan ptyExitMsg, 16),
		apiMsgs:     make(chan apiMsg),
		attachConn:  make(chan attachConnectMsg, 4),
		attachDisc:  make(chan attachDisconnectMsg, 4),
		attachIn:    make(chan attachInputMsg, 256),
		attachSize:  make(chan attachResizeMsg, 16),
		attachCmds:  make(chan attachCmdMsg, 64),
		clientFocus: make(chan clientFocusMsg, 8),
		watchAdd:    make(chan watchAddMsg, 4),
		watchDel:    make(chan chan apiproto.Event, 4),
	}
}

// Run blocks, processing events until server.shutdown is handled.
func (l *Loop) Run() {
	frames := time.NewTicker(flushInterval)
	defer frames.Stop()
	status := time.NewTicker(statusInterval)
	defer status.Stop()

	for {
		select {
		case m := <-l.ptyMsgs:
			l.handlePTYOutput(m)
		case m := <-l.ptyExits:
			l.handlePTYExit(m)
		case m := <-l.apiMsgs:
			resp, deferred := l.handleAPI(m.req, m.reply)
			if !deferred {
				m.reply <- resp
			}
			if m.req.Method == "server.shutdown" {
				l.failPendingWaiters()
				l.closeWatchers()
				return
			}
		case m := <-l.attachConn:
			l.handleAttachConnect(m)
		case m := <-l.attachDisc:
			// Closing the channel is the loop's job: it is the only writer,
			// and it must stop writing before the channel goes away. Doing it
			// here is what makes that ordering guaranteed rather than lucky.
			if c, ok := l.app.clients[m.clientID]; ok {
				delete(l.app.clients, m.clientID)
				close(c.send)
			}
		case m := <-l.attachIn:
			l.handleAttachInput(m)
		case m := <-l.attachSize:
			l.handleAttachResize(m)
		case m := <-l.attachCmds:
			l.handleAttachCmd(m)
		case m := <-l.watchAdd:
			l.handleWatchAdd(m)
		case ch := <-l.watchDel:
			l.handleWatchDel(ch)
		case m := <-l.clientFocus:
			if c, ok := l.app.clients[m.clientID]; ok {
				c.focused = m.focused
				if m.focused {
					// Coming back to the terminal counts as seeing whatever is
					// on screen, which is what clears the badges.
					l.markVisibleSeen()
					l.broadcastState()
				}
			}
		case <-status.C:
			l.refreshAgentStatus()
			l.pumpSends()
			l.expireWaiters()
		case <-frames.C:
			l.flushFrame()
		}
	}
}

// --- submission API, called from other goroutines ---

func (l *Loop) NotifyPTYOutput(paneID string, data []byte) {
	l.ptyMsgs <- ptyOutputMsg{paneID: paneID, data: data}
}

func (l *Loop) NotifyPTYExit(paneID string, exitCode int) {
	l.ptyExits <- ptyExitMsg{paneID: paneID, exitCode: exitCode}
}

// SubmitAPI dispatches req and blocks for the response. Safe to call
// concurrently from many API connection goroutines.
func (l *Loop) SubmitAPI(req apiproto.Request) apiproto.Response {
	reply := make(chan apiproto.Response, 1)
	l.apiMsgs <- apiMsg{req: req, reply: reply}
	return <-reply
}

func (l *Loop) NotifyAttachConnect(clientID uint64, send chan any, cols, rows int, theme ClientTheme) {
	l.attachConn <- attachConnectMsg{
		clientID: clientID, send: send, cols: cols, rows: rows, theme: theme,
	}
}

func (l *Loop) NotifyAttachDisconnect(clientID uint64) {
	l.attachDisc <- attachDisconnectMsg{clientID: clientID}
}

func (l *Loop) NotifyAttachInput(clientID uint64, data string) {
	l.attachIn <- attachInputMsg{clientID: clientID, data: data}
}

func (l *Loop) NotifyAttachResize(clientID uint64, cols, rows int) {
	l.attachSize <- attachResizeMsg{clientID: clientID, cols: cols, rows: rows}
}

// NotifyAttachCmd submits any non-input client message: focus, splits, tab
// and workspace commands, mouse events.
func (l *Loop) NotifyAttachCmd(clientID uint64, kind, target, text, dir string) {
	l.attachCmds <- attachCmdMsg{clientID: clientID, kind: kind, target: target, text: text, dir: dir}
}

// NotifyClientFocus reports a client's terminal gaining or losing focus.
func (l *Loop) NotifyClientFocus(clientID uint64, focused bool) {
	l.clientFocus <- clientFocusMsg{clientID: clientID, focused: focused}
}

func (l *Loop) NotifyAttachMouse(clientID uint64, m attachproto.Mouse) {
	l.attachCmds <- attachCmdMsg{clientID: clientID, kind: attachproto.TypeMouse, mouse: m}
}

// --- handlers, only ever called from Run's own goroutine ---

func (l *Loop) handlePTYOutput(m ptyOutputMsg) {
	pane, ok := l.app.panes[m.paneID]
	if !ok {
		return
	}
	pane.Grid.Write(m.data)
	pane.Revision++
	pane.LastOutput = time.Now()
}

func (l *Loop) handlePTYExit(m ptyExitMsg) {
	pane, ok := l.app.panes[m.paneID]
	if !ok {
		return
	}
	pane.Status = "exited"
	pane.ExitCode = m.exitCode
	l.emitPaneEvent(apiproto.EventPaneExit, pane)
	l.app.dirty = true
	l.broadcastControl(attachproto.PaneExit{Type: attachproto.TypePaneExit, PaneID: pane.ID, ExitCode: m.exitCode})
	l.broadcastState()
	l.checkWaiters()
}

func (l *Loop) handleAttachConnect(m attachConnectMsg) {
	// Assume focused until told otherwise: a terminal that cannot report
	// focus would otherwise look permanently away.
	l.app.clients[m.clientID] = &attachClientConn{
		id: m.clientID, send: m.send, cols: m.cols, rows: m.rows, focused: true,
	}
	l.setViewport(m.cols, m.rows)
	// Adopt the client's glyph theme so the pane headers the daemon draws
	// match the sidebar the client draws.
	if m.theme.Icons != "" {
		l.app.icons = icons.For(m.theme.Icons)
	}
	if m.theme.Spinner != "" {
		l.app.spinner = icons.SpinnerFor(m.theme.Spinner)
	}
	l.app.accent = termgrid.ParseColor(m.theme.Accent, l.app.accent)
	l.app.headerFG = termgrid.ParseColor(m.theme.HeaderFG, l.app.headerFG)
	l.app.borderFG = termgrid.ParseColor(m.theme.Border, l.app.borderFG)
	l.app.spinnerFG = termgrid.ParseColor(m.theme.SpinnerColor, l.app.spinnerFG)
	l.app.doneFG = termgrid.ParseColor(m.theme.DoneColor, l.app.doneFG)
	l.app.blink = m.theme.Blink
	if m.theme.Borders != "" {
		l.app.borders = m.theme.Borders
	}
	l.app.dirty = true

	send(m.send, attachproto.HelloAck{Type: attachproto.TypeHelloAck, Session: l.app.Session, Version: l.version})
	send(m.send, l.buildState())
	if len(l.app.panes) > 0 {
		send(m.send, l.buildFrame())
	}
}

func (l *Loop) handleAttachInput(m attachInputMsg) {
	pane := l.app.panes[l.app.focusedPane()]
	if pane == nil {
		return
	}
	// Typing at a pane you scrolled back means you are done reading: jump to
	// the live screen and let the keystroke through, rather than swallowing it
	// and looking wedged.
	l.exitScroll(pane)
	pane.Seen = true
	_, _ = pane.Actor.Write([]byte(m.data))
}

func (l *Loop) handleAttachResize(m attachResizeMsg) {
	c, ok := l.app.clients[m.clientID]
	if !ok {
		return
	}
	c.cols, c.rows = m.cols, m.rows
	l.setViewport(m.cols, m.rows)
}

func (l *Loop) setViewport(cols, rows int) {
	if cols <= 0 || rows <= 0 || (l.app.viewCols == cols && l.app.viewRows == rows) {
		return
	}
	l.app.viewCols, l.app.viewRows = cols, rows
	l.app.dirty = true
}

// handleAttachCmd routes every non-input client message. Each case is a thin
// shim onto the same operations the API socket exposes, so the TUI and an
// agent driving `rook` from a shell can never drift apart in behaviour.
func (l *Loop) handleAttachCmd(m attachCmdMsg) {
	var resp apiproto.Response

	switch m.kind {
	case attachproto.TypeFocus:
		l.focusPane(m.target)
	case attachproto.TypeNewPane:
		resp = l.paneCreate("attach", apiproto.PaneCreateParams{Cmd: m.text, Label: m.target, Direction: m.dir})
	case attachproto.TypeClosePane:
		resp = l.paneClose("attach", apiproto.PaneCloseParams{PaneID: m.target})
	case attachproto.TypeMoveFocus:
		if next := Neighbor(l.app.rects(), l.app.focusedPane(), m.dir); next != "" {
			l.focusPane(next)
		}
	case attachproto.TypeResizePane:
		l.resizeSplit(l.app.focusedPane(), m.dir, 0)
	case attachproto.TypeSwapPane:
		l.swapPane(l.app.focusedPane(), m.dir)
	case attachproto.TypeZoom:
		if t := l.app.activeTab(); t != nil {
			t.zoom = !t.zoom
			l.app.dirty = true
			l.broadcastState()
		}
	case attachproto.TypeMouse:
		l.handleMouse(m.mouse)
	case attachproto.ActionNewTab:
		resp = l.tabCreate("attach", apiproto.TabCreateParams{Name: m.text})
	case attachproto.ActionCloseTab:
		resp = l.tabClose("attach", apiproto.TabCloseParams{TabID: m.target})
	case attachproto.ActionNextTab:
		l.cycleTab(1)
	case attachproto.ActionPrevTab:
		l.cycleTab(-1)
	case attachproto.ActionFocusTab:
		l.focusTab(m.target)
	case attachproto.ActionRenameTab:
		if _, t := l.app.resolveTab(m.target); t != nil {
			t.Name = m.text
			l.broadcastState()
		}
	case attachproto.ActionScrollMode:
		l.enterScroll(l.app.panes[l.app.focusedPane()])
	case attachproto.ActionScroll:
		l.scrollCommand(l.app.panes[l.app.focusedPane()], m.text)
	case attachproto.ActionScrollExit:
		l.exitScroll(l.app.panes[l.app.focusedPane()])
	case attachproto.ActionCopySelect:
		l.toggleSelect(l.app.panes[l.app.focusedPane()])
	case attachproto.ActionCopyYank:
		if text := l.copySelection(l.app.panes[l.app.focusedPane()]); text != "" {
			if c, cok := l.app.clients[m.clientID]; cok {
				send(c.send, attachproto.Copy{Type: attachproto.TypeCopy, Text: text})
			}
		}
	case attachproto.ActionGit:
		resp = l.openGitTool()
	case attachproto.ActionNewWorkspace:
		resp = l.workspaceCreate("attach", apiproto.WorkspaceCreateParams{Name: m.text})
	case attachproto.ActionFocusWS:
		l.focusWorkspace(m.target)
	case attachproto.ActionNextWS:
		l.cycleWorkspace(1)
	case attachproto.ActionPrevWS:
		l.cycleWorkspace(-1)
	case attachproto.ActionCloseWS:
		resp = l.workspaceClose("attach", apiproto.WorkspaceCloseParams{WorkspaceID: m.target})
	case attachproto.ActionRenameWS:
		target := m.target
		if target == "" {
			target = l.app.activeWS
		}
		if w := l.app.workspace(target); w != nil {
			w.Name = m.text
			w.Named = m.text != ""
			l.broadcastState()
		}
	case attachproto.ActionRenamePane:
		target := m.target
		if target == "" {
			target = l.app.focusedPane()
		}
		if p, ok := l.app.panes[target]; ok {
			p.Label = m.text
			l.broadcastState()
		}
	}

	if resp.Error != nil {
		l.sendError(m.clientID, resp.Error.Message)
	}
}

// focusPane switches focus and marks the pane seen, which is what turns a
// "done" agent back into a plain "idle" one. Focusing a pane in another
// workspace or tab switches to it, so a click in the agents panel always
// lands somewhere visible.
func (l *Loop) focusPane(paneID string) bool {
	pane := l.app.resolvePane(paneID)
	if pane == nil {
		return false
	}
	w, t := l.app.tabOf(pane.ID)
	if w == nil || t == nil {
		return false
	}
	l.app.activeWS = w.ID
	w.activeTab = t.ID
	t.focus = pane.ID
	l.markVisibleSeen()
	l.app.dirty = true
	l.broadcastState()
	return true
}

func (l *Loop) focusTab(tabID string) bool {
	w, t := l.app.resolveTab(tabID)
	if w == nil || t == nil {
		return false
	}
	l.app.activeWS = w.ID
	w.activeTab = t.ID
	l.markVisibleSeen()
	l.app.dirty = true
	l.broadcastState()
	return true
}

// markVisibleSeen clears the unread flag on every pane now on screen, so a
// badge disappears the moment you switch to it rather than on the next status
// tick a quarter-second later.
func (l *Loop) markVisibleSeen() {
	for id := range l.app.rects() {
		if pane := l.app.panes[id]; pane != nil {
			pane.Seen = true
		}
	}
}

func (l *Loop) focusWorkspace(id string) bool {
	w := l.app.workspace(id)
	if w == nil {
		return false
	}
	l.app.activeWS = w.ID
	l.markVisibleSeen()
	l.app.dirty = true
	l.broadcastState()
	return true
}

func (l *Loop) cycleTab(delta int) {
	w := l.app.activeWorkspace()
	if w == nil || len(w.tabs) == 0 {
		return
	}
	cur := 0
	for i, t := range w.tabs {
		if t.ID == w.activeTab {
			cur = i
			break
		}
	}
	l.focusTab(w.tabs[(cur+delta+len(w.tabs))%len(w.tabs)].ID)
}

func (l *Loop) cycleWorkspace(delta int) {
	if len(l.app.workspaces) == 0 {
		return
	}
	cur := 0
	for i, w := range l.app.workspaces {
		if w.ID == l.app.activeWS {
			cur = i
			break
		}
	}
	l.focusWorkspace(l.app.workspaces[(cur+delta+len(l.app.workspaces))%len(l.app.workspaces)].ID)
}

// gitTools are the terminal git UIs worth opening, best first. Spawning one of
// these beats building a git UI into rookery: they already exist, they are
// better than anything a multiplexer would grow on the side, and a pane is
// exactly the right place to put one.
var gitTools = [][]string{
	{"lazygit"},
	{"gitui"},
	{"tig"},
	// Last resort: a shell in the repo with the status already printed, which
	// is at least a working starting point.
	{"sh", "-c", "git status && exec ${SHELL:-/bin/sh}"},
}

// openGitTool opens a git UI in a new pane, in the active workspace's
// directory — the repo you are looking at, not wherever the daemon started.
func (l *Loop) openGitTool() apiproto.Response {
	cwd := ""
	if w := l.app.activeWorkspace(); w != nil {
		cwd = w.Cwd
	}
	for _, argv := range gitTools {
		if _, err := exec.LookPath(argv[0]); err != nil {
			continue
		}
		return l.paneCreate("git", apiproto.PaneCreateParams{
			Cmd:   argv[0],
			Args:  argv[1:],
			Cwd:   cwd,
			Label: "git",
		})
	}
	return errResp("git", apiproto.ErrInternal, "no git UI found (install lazygit, gitui or tig)")
}

func (l *Loop) sendError(clientID uint64, message string) {
	if c, ok := l.app.clients[clientID]; ok {
		send(c.send, attachproto.ErrorFrame{Type: attachproto.TypeErrorMsg, Message: message})
	}
}

// refreshRunningCommand tracks what is actually running in a pane and
// re-detects the agent from it. Without this, an agent you start by typing
// `claude` into a shell pane is invisible to rookery: the pane's spawn
// command is the shell, so it would keep showing up as "zsh" with no agent
// and could never reach the "done" state. Reports whether anything changed.
func (l *Loop) refreshRunningCommand(pane *Pane) bool {
	pid, argv := pane.Actor.Foreground()
	if pid <= 0 || len(argv) == 0 {
		return false
	}

	agent := l.agents.Agent(argv[0], argv[1:])
	// Show the agent's name when there is one, so `node /…/claude` reads as
	// "claude" rather than "node"; otherwise the plain executable name.
	running := agent
	if running == "" {
		running = baseName(argv[0])
	}
	if running == pane.Running {
		return false
	}
	pane.Running = running

	// The spawn command still wins when it was an agent itself: a pane
	// started as `claude` that shells out to `git` for two seconds should
	// not stop being a Claude pane.
	if spawned := l.agents.Agent(pane.Cmd, pane.Args); spawned == "" {
		pane.Agent = agent
	}
	return true
}

// evaluatePane decides what a pane is doing. Which signal to trust depends
// entirely on what is running in it, and getting that wrong is why status
// used to stick on "working":
//
//   - An agent repaints constantly — spinners, context meters, token
//     counters — so "printed something recently" is always true. Only its own
//     on-screen markers mean anything, and their absence means idle.
//   - A shell prints nothing at all while `sleep 30` runs, so activity says
//     nothing either. The kernel's foreground process group is exact: a shell
//     running a job is working, a shell at its prompt is idle. It also
//     ignores a prompt that repaints itself (powerlevel10k redraws its clock
//     every second, which the activity heuristic read as permanent work).
//   - Anything else is its own job: the process is the work, so recent output
//     is the only signal available.
func (l *Loop) evaluatePane(pane *Pane) agentstatus.State {
	// An integration reporting for this pane is authoritative. Herdr draws the
	// same line: when hooks are firing, the screen manifest is not consulted
	// for that pane, because a hook on "the permission dialog opened" knows
	// something no amount of reading the screen can be sure of.
	if pane.Reported != "" && time.Since(pane.ReportedAt) < reportTTL {
		return pane.Reported
	}

	in := agentstatus.Input{Title: pane.Grid.Title(), Bottom: pane.Grid.BottomLines(6), Screen: pane.Grid.Lines()}

	if pane.Agent != "" {
		verdict := l.agents.EvaluateAgentVerdict(pane.Agent, in)
		if verdict.SkipStateUpdate {
			// The winning rule asked for this tick to be ignored (a
			// transcript viewer layered over the agent's own screen) —
			// keep whatever the pane already reported.
			return pane.AgentState
		}
		state := verdict.State
		// Within the grace period after a prompt, only a positive marker can
		// contradict "working" — a blank idle screen just means the agent
		// has not redrawn yet.
		if state == agentstatus.Idle && time.Now().Before(pane.BusyUntil) {
			return agentstatus.Working
		}
		return state
	}
	if s := l.agents.Evaluate("", in); s != agentstatus.Unknown {
		return s
	}
	if isShell(pane.Running, pane.Cmd) {
		if pane.Actor.Busy() {
			return agentstatus.Working
		}
		return agentstatus.Idle
	}
	if pane.Actor.Busy() || time.Since(pane.LastOutput) < idleAfter {
		return agentstatus.Working
	}
	return agentstatus.Idle
}

var shells = map[string]bool{
	"sh": true, "bash": true, "zsh": true, "fish": true, "dash": true,
	"ksh": true, "tcsh": true, "csh": true, "nu": true, "xonsh": true,
	"elvish": true, "pwsh": true, "powershell": true,
}

// isShell reports whether a pane is sitting at a shell prompt rather than
// running something. Both the live foreground command and the spawn command
// are checked, since a pane started as a shell is still a shell pane.
func isShell(running, cmd string) bool {
	return shells[baseName(running)] || shells[baseName(cmd)]
}

// refreshAgentStatus re-evaluates every pane's agent state. Running it on a
// timer, rather than on every chunk of PTY output, keeps the cost bounded and
// independent of how chatty the agents are.
func (l *Loop) refreshAgentStatus() {
	changed := false
	visible := l.app.rects()

	for _, pane := range l.app.panes {
		if pane.Status == "exited" {
			continue
		}
		// Anything on screen is being seen, so its badge clears itself. This
		// is what makes switching to a tab (or splitting an agent into view)
		// dismiss the unread marker without having to focus that exact pane —
		// you can read a pane you aren't typing into.
		if _, onScreen := visible[pane.ID]; onScreen && !pane.Seen {
			pane.Seen = true
			changed = true
		}
		if l.refreshRunningCommand(pane) {
			changed = true
		}
		if dir := pane.Actor.Cwd(); dir != "" && dir != pane.Cwd {
			pane.Cwd = dir
			changed = true
		}
		// An agent's title is its current task, and it changes as the task
		// does, so it is tracked on the same tick as the status.
		if pane.Agent != "" {
			if title := agentstatus.CleanTitle(pane.Grid.Title()); title != pane.Title {
				pane.Title = title
				changed = true
			}
		}
		next := l.evaluatePane(pane)
		if next == pane.AgentState {
			continue
		}
		switch {
		case next == agentstatus.Idle && pane.AgentState != agentstatus.Unknown:
			pane.DoneAt = time.Now()
			// A turn just ended. If the pane wasn't on screen, that result is
			// unseen — which is exactly what "done" means.
			_, onScreen := visible[pane.ID]
			pane.Seen = onScreen
		case next == agentstatus.Working:
			// Fresh work started; there is no result to miss yet. Blocked is
			// deliberately not handled here — it is its own attention state,
			// and marking it seen would swallow the "done" that follows when
			// the agent finishes the turn unwatched.
			pane.Seen = true
		}
		l.setAgentState(pane, next)
		changed = true

		// Ping only for the transitions that mean "a human is needed": an
		// agent that just got stuck, or one that finished with nobody
		// watching. Anything else would train you to ignore the sound.
		switch {
		case next == agentstatus.Blocked:
			l.alert(pane, notify.Blocked)
		case next == agentstatus.Idle && !pane.Seen:
			l.alert(pane, notify.Done)
		}
	}
	if l.refreshWorkspaceDirs() {
		changed = true
	}
	if changed {
		l.app.dirty = true
		l.broadcastState()
		l.checkWaiters()
	}
}

// refreshWorkspaceDirs lets a workspace follow the directory its focused pane
// is in, so `cd` re-reads the git branch — and renames the workspace too,
// unless you named it yourself.
//
// The directory follows even for a named workspace: the name was your choice,
// but a stale branch is just wrong.
func (l *Loop) refreshWorkspaceDirs() bool {
	changed := false
	for _, w := range l.app.workspaces {
		t := w.active()
		if t == nil {
			continue
		}
		pane := l.app.panes[t.focus]
		if pane == nil {
			continue
		}
		if w.setCwd(pane.Cwd) {
			changed = true
		}
	}
	return changed
}

// alert plays a sound and tells attached clients, which ring their own
// terminal bell and surface a line in the status bar.
func (l *Loop) alert(pane *Pane, kind notify.Kind) {
	if pane.Agent == "" {
		return // a shell going quiet is not news
	}
	l.sound.Play(kind)
	if !l.app.anyFocused() {
		// Nobody is looking at the terminal, so a sound alone can be missed
		// entirely — this is the case an OS notification exists for.
		what := "finished"
		if kind == notify.Blocked {
			what = "needs your input"
		}
		l.sound.Desktop(pane.displayName()+" "+what, l.app.Session+" · "+pane.Agent)
	}
	l.broadcastControl(attachproto.Notify{
		Type:   attachproto.TypeNotify,
		Kind:   string(kind),
		PaneID: pane.ID,
		Agent:  pane.Agent,
		Title:  pane.displayName(),
	})
}

// flushFrame broadcasts a fresh composite frame when anything changed since
// the last one.
func (l *Loop) flushFrame() {
	dirty := l.app.dirty || l.spinnerAdvanced() || l.blinkAdvanced()
	for _, pane := range l.app.panes {
		if pane.Grid.TakeDirty() {
			// Only visible panes force a repaint; a background tab scrolling
			// its output shouldn't cost the foreground 30 frames a second.
			if l.isVisible(pane.ID) {
				dirty = true
			}
		}
	}
	if !dirty {
		return
	}
	l.app.dirty = false

	if len(l.app.panes) == 0 {
		l.broadcastControl(attachproto.Frame{Type: attachproto.TypeFrame})
		return
	}
	frame := l.buildFrame()
	for _, c := range l.app.clients {
		sendDroppable(c.send, frame)
	}
}

// spinnerAdvanced reports whether the spinner needs a repaint: only when the
// frame actually changed and something on screen is animating, so an idle
// session still costs nothing between keystrokes.
// blinkPhase is the on/off half of the attention blink, from the wall clock so
// the daemon's borders and the client's sidebar blink together.
func blinkPhase(now time.Time) bool {
	return (now.UnixMilli()/blinkInterval.Milliseconds())%2 == 0
}

// blinking reports whether a pane should be drawing attention right now.
func (l *Loop) blinking(pane *Pane) bool {
	if pane.DoneAt.IsZero() || !l.app.blink {
		return false
	}
	return time.Since(pane.DoneAt) < blinkFor
}

// blinkAdvanced reports whether a blink needs a repaint: only while a visible
// pane is inside its blink window and the phase actually flipped.
func (l *Loop) blinkAdvanced() bool {
	phase := blinkPhase(time.Now())
	if phase == l.app.blinkOn {
		return false
	}
	for id := range l.app.rects() {
		if p := l.app.panes[id]; p != nil && l.blinking(p) {
			l.app.blinkOn = phase
			return true
		}
	}
	return false
}

func (l *Loop) spinnerAdvanced() bool {
	frame := icons.Frame(l.app.spinner, time.Now())
	if frame == l.app.spinnerFrame {
		return false
	}
	working := false
	for id := range l.app.rects() {
		if p := l.app.panes[id]; p != nil && p.agentStatus() == agentstatus.Working {
			working = true
			break
		}
	}
	if !working {
		return false
	}
	l.app.spinnerFrame = frame
	return true
}

func (l *Loop) isVisible(paneID string) bool {
	_, ok := l.app.rects()[paneID]
	return ok
}

func (l *Loop) broadcastState() {
	// Every structural change already funnels through here, which makes it
	// the one place the layout has to be saved from.
	l.persist()
	l.broadcastControl(l.buildState())
}

// broadcastControl sends a control-plane message to every attached client,
// blocking briefly if a client's writer is behind — these are infrequent and
// must not be silently dropped the way frames can be.
func (l *Loop) broadcastControl(v any) {
	for _, c := range l.app.clients {
		send(c.send, v)
	}
}

func send(ch chan any, v any) {
	ch <- v
}

// sendDroppable is used for the per-tick Frame broadcast: if a client's
// writer goroutine is behind, drop this frame rather than stall the whole
// daemon — the next tick will just resend the (by-then newer) full frame.
func sendDroppable(ch chan any, v any) {
	select {
	case ch <- v:
	default:
	}
}
