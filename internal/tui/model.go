package tui

import (
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jirkab/rookery/internal/attachproto"
	"github.com/jirkab/rookery/internal/config"
	"github.com/jirkab/rookery/internal/icons"
	"github.com/jirkab/rookery/internal/ndjson"
	"github.com/jirkab/rookery/internal/notify"
)

type model struct {
	conn    net.Conn
	writer  *ndjson.Writer
	program *tea.Program
	session string

	cfg         config.Config
	keys        config.Hotkeys
	icons       icons.Set
	spinner     []string
	p           palette
	configPath  string
	hotkeysPath string

	width, height int
	helloSent     bool

	state attachproto.State
	frame attachproto.Frame

	// prefixMode is "the next key is a rookery command". navMode is the
	// keyboard equivalent of clicking in the sidebar: a cursor you move with
	// j/k and commit with enter.
	prefixMode bool
	navMode    bool
	navIndex   int
	helpMode   bool
	// promptMode collects a line of text for rename commands.
	promptMode   bool
	promptLabel  string
	promptAction string
	promptTarget string
	promptText   string

	sidebarHidden bool
	mouseOn       bool

	// Hit-test tables, rebuilt every render so a click can be resolved
	// against exactly what is on screen right now.
	sidebarRows []sidebarRow
	tabHits     []tabHit

	clientVersion string
	// spinning tracks whether the animation clock is already running, so a
	// burst of state updates can't stack up a tick per message.
	spinning bool

	statusMsg string
	fatalErr  error
}

func newModel(sessionName string, conn net.Conn, cfg config.Config, keys config.Hotkeys) *model {
	return &model{
		conn:          conn,
		writer:        ndjson.NewWriter(conn),
		session:       sessionName,
		cfg:           cfg,
		keys:          keys,
		icons:         icons.For(cfg.UI.Icons),
		spinner:       icons.SpinnerFor(cfg.UI.Spinner),
		p:             newPalette(cfg.UI.Colors),
		configPath:    config.ConfigPath(),
		hotkeysPath:   config.HotkeysPath(),
		sidebarHidden: !cfg.UI.SidebarVisible,
		mouseOn:       cfg.UI.MouseCapture,
	}
}

func (m *model) Init() tea.Cmd { return spinnerTick() }

// spinnerTickMsg drives the sidebar's animation.
type spinnerTickMsg struct{}

// spinnerTick re-arms the animation clock. It is only rescheduled while
// something is actually spinning, so an idle session does no work between
// keystrokes.
func spinnerTick() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return spinnerTickMsg{} })
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if !m.helloSent {
			m.helloSent = true
			m.send(attachproto.Hello{
				Type: attachproto.TypeHello, Session: m.session,
				Cols: m.paneCols(), Rows: m.paneRows(),
				Icons: m.cfg.UI.Icons, Spinner: m.cfg.UI.Spinner,
				Accent:   m.cfg.UI.Colors.Accent,
				HeaderFG: m.cfg.UI.Colors.HeaderFG,
				Border:   m.cfg.UI.Colors.Border,
			})
		} else {
			m.send(attachproto.Resize{Type: attachproto.TypeResize, Cols: m.paneCols(), Rows: m.paneRows()})
		}
		return m, nil

	case spinnerTickMsg:
		m.spinning = m.anyWorking()
		if m.spinning {
			return m, spinnerTick()
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.MouseMsg:
		return m.handleMouse(msg)

	case attachproto.ServerMsg:
		model, cmd := m.handleServerMsg(msg)
		// Start the animation clock when an agent starts working, and only
		// if it isn't already running — a burst of state updates must not
		// stack up one tick per message.
		if !m.spinning && m.anyWorking() {
			m.spinning = true
			return model, tea.Batch(cmd, spinnerTick())
		}
		return model, cmd

	case connErrMsg:
		m.fatalErr = msg.err
		return m, tea.Quit
	}
	return m, nil
}

func (m *model) sidebarWidth() int {
	if m.sidebarHidden {
		return 0
	}
	return m.cfg.UI.SidebarWidth
}

func (m *model) paneRows() int {
	return max(m.height-chromeRows, 1)
}

func (m *model) paneCols() int {
	return max(m.width-m.sidebarWidth(), minPaneCols)
}

// --- keys ---

func (m *model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	switch {
	case m.helpMode:
		m.helpMode = false
		return m, nil
	case m.promptMode:
		return m.handlePromptKey(msg, key)
	case m.navMode:
		return m.handleNavKey(key)
	case m.prefixMode:
		m.prefixMode = false
		m.statusMsg = ""
		return m.handleAction(m.keys.Action(key), key)
	}

	if key == m.keys.Prefix {
		m.prefixMode = true
		m.statusMsg = "prefix — " + m.keys.Prefix + " ? for help"
		return m, nil
	}

	m.statusMsg = ""
	if data := keyToBytes(msg); len(data) > 0 {
		m.send(attachproto.Input{Type: attachproto.TypeInput, Data: string(data)})
	}
	return m, nil
}

// handleAction runs one prefix command. Everything the mouse can do routes
// through the same actions, so the two input paths can't drift apart.
func (m *model) handleAction(action, key string) (tea.Model, tea.Cmd) {
	switch action {
	case config.ActionDetach:
		return m, tea.Quit
	case config.ActionHelp:
		m.helpMode = true
	case config.ActionLiteralPrefix:
		// prefix prefix sends the prefix key through, so a nested
		// multiplexer or a readline user isn't locked out.
		m.send(attachproto.Input{Type: attachproto.TypeInput, Data: "\x02"})
	case config.ActionNewTab:
		m.act(attachproto.ActionNewTab, "", "")
	case config.ActionNewTabNamed:
		m.startPrompt("new tab name", attachproto.ActionNewTab, "")
	case config.ActionCloseTab:
		m.act(attachproto.ActionCloseTab, m.state.ActiveTab, "")
	case config.ActionNextTab:
		m.act(attachproto.ActionNextTab, "", "")
	case config.ActionPrevTab:
		m.act(attachproto.ActionPrevTab, "", "")
	case config.ActionRenameTab:
		m.startPrompt("rename tab", attachproto.ActionRenameTab, m.state.ActiveTab)
	case config.ActionSplitRight:
		m.send(attachproto.NewPane{Type: attachproto.TypeNewPane, Direction: "right"})
	case config.ActionSplitDown:
		m.send(attachproto.NewPane{Type: attachproto.TypeNewPane, Direction: "down"})
	case config.ActionClosePane:
		m.send(attachproto.ClosePane{Type: attachproto.TypeClosePane, PaneID: m.state.Focus})
	case config.ActionZoom:
		m.send(attachproto.Zoom{Type: attachproto.TypeZoom})
	case config.ActionFocusLeft:
		m.move("left")
	case config.ActionFocusDown:
		m.move("down")
	case config.ActionFocusUp:
		m.move("up")
	case config.ActionFocusRight:
		m.move("right")
	case config.ActionResizeLeft:
		m.resize("left")
	case config.ActionResizeDown:
		m.resize("down")
	case config.ActionResizeUp:
		m.resize("up")
	case config.ActionResizeRight:
		m.resize("right")
	case config.ActionNewWorkspace:
		m.act(attachproto.ActionNewWorkspace, "", "")
	case config.ActionNextWorkspace:
		m.act(attachproto.ActionNextWS, "", "")
	case config.ActionPrevWorkspace:
		m.act(attachproto.ActionPrevWS, "", "")
	case config.ActionCloseWorkspce:
		m.act(attachproto.ActionCloseWS, m.state.ActiveWorkspace, "")
	case config.ActionToggleSidebar:
		m.sidebarHidden = !m.sidebarHidden
		m.sendResize()
	case config.ActionFocusSidebar:
		m.navMode, m.navIndex = true, m.firstNavRow()
		m.statusMsg = "sidebar — j/k move · enter select · esc exit"
	case config.ActionToggleMouse:
		m.mouseOn = !m.mouseOn
		m.statusMsg = "mouse capture " + onOff(m.mouseOn)
		return m, m.mouseCmd()
	default:
		// Unbound: 1-9 jump to a tab, matching every multiplexer ever.
		if id := m.tabIndexKey(key); id != "" {
			m.act(attachproto.ActionFocusTab, id, "")
		}
	}
	return m, nil
}

// handleNavKey drives the sidebar cursor.
func (m *model) handleNavKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc", "q", "ctrl+c":
		m.navMode, m.statusMsg = false, ""
	case "j", "down":
		m.navIndex = m.nextNavRow(m.navIndex, 1)
	case "k", "up":
		m.navIndex = m.nextNavRow(m.navIndex, -1)
	case "enter", " ", "l", "right":
		if m.navIndex >= 0 && m.navIndex < len(m.sidebarRows) {
			row := m.sidebarRows[m.navIndex]
			m.activateSidebarRow(row)
		}
		m.navMode, m.statusMsg = false, ""
	}
	return m, nil
}

func (m *model) activateSidebarRow(row sidebarRow) {
	switch row.kind {
	case "workspace":
		m.act(attachproto.ActionFocusWS, row.target, "")
	case "agent":
		m.send(attachproto.Focus{Type: attachproto.TypeFocus, PaneID: row.target})
	}
}

// firstNavRow is the first selectable row; blank rows and headings aren't.
func (m *model) firstNavRow() int {
	for i, r := range m.sidebarRows {
		if r.kind != "" {
			return i
		}
	}
	return 0
}

func (m *model) nextNavRow(from, delta int) int {
	for i := from + delta; i >= 0 && i < len(m.sidebarRows); i += delta {
		if m.sidebarRows[i].kind != "" {
			return i
		}
	}
	return from
}

func (m *model) handlePromptKey(msg tea.KeyMsg, key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc", "ctrl+c":
		m.promptMode, m.promptText, m.statusMsg = false, "", ""
	case "enter":
		if m.promptText != "" {
			m.act(m.promptAction, m.promptTarget, m.promptText)
		}
		m.promptMode, m.promptText, m.statusMsg = false, "", ""
	case "backspace":
		if r := []rune(m.promptText); len(r) > 0 {
			m.promptText = string(r[:len(r)-1])
		}
	default:
		if msg.Type == tea.KeyRunes {
			m.promptText += string(msg.Runes)
		} else if key == " " {
			m.promptText += " "
		}
	}
	return m, nil
}

func (m *model) startPrompt(label, action, target string) {
	m.promptMode, m.promptLabel, m.promptAction, m.promptTarget, m.promptText = true, label, action, target, ""
}

func (m *model) move(dir string) {
	m.send(attachproto.MoveFocus{Type: attachproto.TypeMoveFocus, Direction: dir})
}

func (m *model) resize(dir string) {
	m.send(attachproto.ResizePane{Type: attachproto.TypeResizePane, Direction: dir})
}

func (m *model) act(action, target, text string) {
	m.send(attachproto.Action{Type: attachproto.TypeAction, Action: action, Target: target, Text: text})
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// --- mouse ---

// handleMouse resolves a click against what was last drawn: the sidebar, the
// tab strip, or the content area (which the daemon hit-tests, since it owns
// the layout).
func (m *model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if !m.mouseOn {
		return m, nil
	}
	kind, button := mouseKind(msg)
	if kind == "" {
		return m, nil
	}

	// Tab strip.
	if msg.Y == 0 {
		if kind == "press" {
			x := msg.X - m.sidebarWidth()
			for _, h := range m.tabHits {
				if x >= h.from && x < h.to {
					m.act(attachproto.ActionFocusTab, h.id, "")
					break
				}
			}
		}
		return m, nil
	}

	// Sidebar.
	if msg.X < m.sidebarWidth() {
		if kind == "press" {
			if row := msg.Y - headerRows; row >= 0 && row < len(m.sidebarRows) {
				m.activateSidebarRow(m.sidebarRows[row])
			}
		}
		return m, nil
	}

	// Content area: the daemon decides between focusing a pane, dragging a
	// divider, and forwarding to a program that asked for mouse reporting.
	m.send(attachproto.Mouse{
		Type:   attachproto.TypeMouse,
		Kind:   kind,
		Button: button,
		X:      msg.X - m.sidebarWidth(),
		Y:      msg.Y - headerRows,
		Alt:    msg.Alt,
		Ctrl:   msg.Ctrl,
		Shift:  msg.Shift,
	})
	return m, nil
}

func mouseKind(msg tea.MouseMsg) (kind, button string) {
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		return "wheel", "up"
	case tea.MouseButtonWheelDown:
		return "wheel", "down"
	}

	button = "left"
	switch msg.Button {
	case tea.MouseButtonMiddle:
		button = "middle"
	case tea.MouseButtonRight:
		button = "right"
	}

	switch msg.Action {
	case tea.MouseActionPress:
		return "press", button
	case tea.MouseActionRelease:
		return "release", button
	case tea.MouseActionMotion:
		return "drag", button
	}
	return "", ""
}

// mouseCmd turns terminal mouse reporting on or off to match m.mouseOn.
func (m *model) mouseCmd() tea.Cmd {
	if m.mouseOn {
		return tea.EnableMouseCellMotion
	}
	return tea.DisableMouse
}

func (m *model) sendResize() {
	m.send(attachproto.Resize{Type: attachproto.TypeResize, Cols: m.paneCols(), Rows: m.paneRows()})
}

// --- server messages ---

func (m *model) handleServerMsg(msg attachproto.ServerMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case attachproto.TypeHelloAck:
		// `rook` attaches to whatever daemon is already running, so a rebuilt
		// binary changes nothing until the daemon restarts. Say so rather
		// than letting it look like the new behaviour is missing.
		if msg.Version != "" && m.clientVersion != "" && msg.Version != m.clientVersion {
			m.statusMsg = "daemon is v" + msg.Version + ", this client is v" + m.clientVersion +
				" — run `rook kill` and reattach to pick up the new build"
		}
	case attachproto.TypeState:
		m.state = attachproto.State{
			Type: msg.Type, Session: msg.Session,
			Workspaces: msg.Workspaces, ActiveWorkspace: msg.ActiveWorkspace,
			Tabs: msg.Tabs, ActiveTab: msg.ActiveTab,
			Panes: msg.Panes, Agents: msg.Agents, Dividers: msg.Dividers,
			Focus: msg.Focus, Zoomed: msg.Zoomed,
		}
	case attachproto.TypeFrame:
		m.frame = attachproto.Frame{
			Type: msg.Type, PaneID: msg.PaneID, Cols: msg.Cols, Rows: msg.Rows,
			ANSI: msg.ANSI, CursorX: msg.CursorX, CursorY: msg.CursorY, Revision: msg.Revision,
		}
	case attachproto.TypeNotify:
		// The daemon already played any system sound; the bell has to come
		// from here, because it is this process that owns the terminal.
		what := "finished"
		if msg.Kind == "blocked" {
			what = "needs input"
		}
		m.statusMsg = m.p.alert.Render(m.icons.Agent + " " + msg.Title + " " + what)
		if m.cfg.UI.Sound.Mode == notify.ModeBell {
			return m, bell
		}

	case attachproto.TypePaneExit:
		m.statusMsg = "pane " + msg.PaneID + " exited (code " + strconv.Itoa(msg.ExitCode) + ")"
	case attachproto.TypeErrorMsg:
		m.statusMsg = "error: " + msg.Message
	}
	return m, nil
}

// bell rings the terminal directly. It cannot go through tea.Printf, which
// prints *above* the program — meaningless in an alternate screen. BEL is a
// non-printing control code, so writing it past the renderer is safe.
func bell() tea.Msg {
	_, _ = os.Stdout.WriteString("\a")
	return nil
}

func (m *model) send(v any) {
	_ = m.writer.WriteJSON(v)
}

// --- view ---

// View lays out exactly m.height lines: the sidebar heading and tab strip on
// the first row, sidebar panels beside pane content, and a status line that
// stays blank unless there is something to say.
//
// The status row is always reserved even when empty. Showing it only on
// demand would change the pane height on every prefix keypress, and resizing
// a PTY makes every full-screen program in it redraw — a visibly worse trade
// than one quiet row.
func (m *model) View() string {
	if m.fatalErr != nil {
		return m.p.errorStyle.Render("connection lost: "+m.fatalErr.Error()) + "\n"
	}
	if m.width == 0 {
		return ""
	}

	rows := m.paneRows()
	lines := make([]string, 0, m.height)

	if m.sidebarHidden {
		lines = append(lines, m.renderTabs(m.width))
	} else {
		lines = append(lines, m.sidebarLine(m.p.sidebarHeading, "spaces")+m.renderTabs(m.paneCols()))
	}

	content := m.frameLines(rows)
	if m.sidebarHidden {
		m.sidebarRows = m.sidebarRows[:0]
		lines = append(lines, content...)
	} else {
		sidebar := m.renderSidebar(rows)
		for i := range rows {
			lines = append(lines, sidebar[i]+content[i])
		}
	}
	lines = append(lines, m.renderStatus())

	// The help floats on top of all that, so the panes, sidebar and tabs stay
	// visible behind it.
	if m.helpMode {
		lines = m.overlay(lines, m.renderHelp(m.width), m.width, m.height)
	}

	return strings.Join(lines, "\n")
}

func (m *model) renderStatus() string {
	if m.promptMode {
		return m.promptLabel + ": " + m.promptText + "▌"
	}
	if m.statusMsg != "" {
		return m.p.help.Render(m.statusMsg)
	}
	return ""
}

// frameLines returns exactly rows lines of pane content, padding out whatever
// the daemon last sent.
//
// ponytail: lines are used at whatever width the daemon rendered them, never
// truncated — trimming a styled ANSI line correctly needs a real escape-code
// parser, and a width mismatch only lasts the single frame between a resize
// and the server's reply.
func (m *model) frameLines(rows int) []string {
	out := make([]string, rows)
	var lines []string
	if m.frame.ANSI != "" {
		lines = strings.Split(m.frame.ANSI, "\n")
	}
	blank := strings.Repeat(" ", m.paneCols())
	for i := range rows {
		if i < len(lines) {
			out[i] = lines[i]
		} else {
			out[i] = blank
		}
	}
	return out
}
