// Package attachproto defines the NDJSON frames exchanged on the attach
// socket between the daemon and a TUI (or future GUI) client. Pure data, no
// dependency on the daemon or on Bubble Tea.
package attachproto

// Frame type tags, carried in the "type" field of every message.
const (
	TypeHello    = "hello"     // client -> server, first message
	TypeHelloAck = "hello_ack" // server -> client, reply to hello
	TypeState    = "state"     // server -> client, the whole tree: workspaces/tabs/panes
	TypeFrame    = "frame"     // server -> client, rendered content area
	TypePaneExit = "pane_exit" // server -> client
	TypeNotify   = "notify"    // server -> client, an agent wants attention
	TypeErrorMsg = "error"     // server -> client

	TypeInput      = "input"       // client -> server, raw bytes for the focused pane
	TypeResize     = "resize"      // client -> server, viewport size changed
	TypeAction     = "action"      // client -> server, a named UI action
	TypeMouse      = "mouse"       // client -> server, a mouse event in the content area
	TypeFocus      = "focus"       // client -> server, focus a pane
	TypeNewPane    = "new_pane"    // client -> server, split the focused pane
	TypeClosePane  = "close_pane"  // client -> server
	TypeMoveFocus  = "move_focus"  // client -> server, focus by direction
	TypeResizePane = "resize_pane" // client -> server, nudge a divider
	TypeZoom       = "zoom"        // client -> server, toggle zoom
	// TypeClientFocus reports whether the client's terminal window has focus.
	// The daemon needs it to decide between a sound (you are here) and an OS
	// notification (you are somewhere else entirely).
	TypeClientFocus = "client_focus"
	// TypeManagerReply carries the manager agent's answer back to the command
	// bar it was asked from.
	TypeManagerReply = "manager_reply"
)

// Action names a client -> server UI command that needs no payload beyond a
// target. Keeping these as strings (rather than a message type each) means a
// new keybinding is one case in a switch, not a new frame type on both sides.
const (
	ActionNewTab       = "new_tab"
	ActionCloseTab     = "close_tab"
	ActionNextTab      = "next_tab"
	ActionPrevTab      = "prev_tab"
	ActionFocusTab     = "focus_tab"
	ActionRenameTab    = "rename_tab"
	ActionNewWorkspace = "new_workspace"
	ActionFocusWS      = "focus_workspace"
	ActionNextWS       = "next_workspace"
	ActionPrevWS       = "prev_workspace"
	ActionCloseWS      = "close_workspace"
	ActionRenameWS     = "rename_workspace"
	ActionGit          = "git"
	ActionManager      = "manager"
)

// Hello is the client's opening frame.
type Hello struct {
	Type    string `json:"type"`
	Session string `json:"session"`
	Cols    int    `json:"cols"`
	Rows    int    `json:"rows"`
	// Icons and Spinner are the client's glyph theme. The daemon draws the
	// per-pane headers itself, so it has to know which set to use or the
	// headers and the sidebar would disagree about what a "working" agent
	// looks like.
	Icons   string `json:"icons,omitempty"`
	Spinner string `json:"spinner,omitempty"`
	// Colours for the chrome the daemon draws itself: the pane headers and
	// the split dividers. Sent as the same strings config.json uses.
	Accent       string `json:"accent,omitempty"`
	HeaderFG     string `json:"header_fg,omitempty"`
	Border       string `json:"border,omitempty"`
	SpinnerColor string `json:"spinner_color,omitempty"`
	// Borders selects pane boxes: "auto", "always" or "never".
	Borders string `json:"borders,omitempty"`
	// DoneColor is the colour a finished pane's border flashes; Blink turns
	// the flash on. Both live on the client because the daemon draws the
	// borders but the user configures the look.
	DoneColor string `json:"done_color,omitempty"`
	Blink     bool   `json:"blink,omitempty"`
	// ManagerCmd is the agent the manager bar talks to.
	ManagerCmd string `json:"manager_cmd,omitempty"`
}

// HelloAck acknowledges attach. The state that follows carries everything
// else.
type HelloAck struct {
	Type    string `json:"type"`
	Session string `json:"session"`
	// Version is the daemon's build. The client compares it with its own and
	// says so if they differ: `rook` attaches to whatever daemon is already
	// running, so rebuilding the binary changes nothing until the daemon is
	// restarted — a trap that otherwise looks like "my fix did not work".
	Version string `json:"version,omitempty"`
}

// State is the whole navigable tree, sent whenever any of it changes. One
// message rather than three keeps the client's view internally consistent:
// there is never a moment where it knows about a tab whose panes haven't
// arrived yet.
type State struct {
	Type            string             `json:"type"`
	Session         string             `json:"session"`
	Workspaces      []WorkspaceSummary `json:"workspaces"`
	ActiveWorkspace string             `json:"active_workspace,omitempty"`
	Tabs            []TabSummary       `json:"tabs"`
	ActiveTab       string             `json:"active_tab,omitempty"`
	// Panes are the visible ones — the active tab's — with their rectangles,
	// so the client can hit-test a click without knowing the layout tree.
	Panes []PaneSummary `json:"panes"`
	Focus string        `json:"focus,omitempty"`
	// Agents is every detected agent across all workspaces, for the sidebar
	// panel. Plain terminals are deliberately absent: that panel answers
	// "which agent needs me", and a shell at a prompt is not an answer.
	Agents []AgentSummary `json:"agents"`
	// Dividers are the draggable split borders of the active tab.
	Dividers []DividerRect `json:"dividers,omitempty"`
	Zoomed   bool          `json:"zoomed,omitempty"`
}

type WorkspaceSummary struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Branch string `json:"branch,omitempty"`
	Cwd    string `json:"cwd,omitempty"`
	// Status rolls up the agents inside: blocked beats done beats working,
	// so one glance at the list finds the workspace that needs you.
	Status string `json:"status,omitempty"`
	Agents int    `json:"agents,omitempty"`
	Tabs   int    `json:"tabs,omitempty"`
}

type TabSummary struct {
	ID     string `json:"id"`
	Name   string `json:"name,omitempty"`
	Panes  int    `json:"panes,omitempty"`
	Status string `json:"status,omitempty"`
}

type PaneSummary struct {
	PaneID string `json:"pane_id"`
	Label  string `json:"label,omitempty"`
	Cmd    string `json:"cmd"`
	// Title is what to show: the label, else the program currently running
	// in the pane, else the spawn command.
	Title       string `json:"title,omitempty"`
	Status      string `json:"status"`
	Agent       string `json:"agent,omitempty"`
	AgentStatus string `json:"agent_status,omitempty"`
	// The pane's rectangle in the content area, for mouse hit-testing.
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
	// MouseWanted reports that the program in the pane turned on mouse
	// reporting, so the client should forward events to it instead of
	// consuming them.
	MouseWanted bool `json:"mouse_wanted,omitempty"`
}

// AgentSummary is one row of the sidebar's agents panel.
type AgentSummary struct {
	PaneID    string `json:"pane_id"`
	Workspace string `json:"workspace"`
	Tab       string `json:"tab,omitempty"`
	Agent     string `json:"agent"`
	Title     string `json:"title,omitempty"`
	Status    string `json:"status"`
	// Unread is set when the agent finished while nobody was looking. It is
	// what the sidebar's marker keys off.
	Unread bool `json:"unread,omitempty"`
}

// DividerRect is a draggable split border in the content area.
type DividerRect struct {
	PaneID string `json:"pane_id"` // a pane on the A side, the resize target
	Dir    string `json:"dir"`     // "h": vertical line; "v": horizontal line
	X      int    `json:"x"`
	Y      int    `json:"y"`
	W      int    `json:"w"`
	H      int    `json:"h"`
}

// Frame carries a rendered composite of the active tab's panes.
type Frame struct {
	Type     string `json:"type"`
	PaneID   string `json:"pane_id"`
	Cols     int    `json:"cols"`
	Rows     int    `json:"rows"`
	ANSI     string `json:"ansi"`
	CursorX  int    `json:"cursor_x"`
	CursorY  int    `json:"cursor_y"`
	Revision uint64 `json:"revision"`
}

// PaneExit notifies clients a pane's process ended.
type PaneExit struct {
	Type     string `json:"type"`
	PaneID   string `json:"pane_id"`
	ExitCode int    `json:"exit_code"`
}

// Input carries raw keyboard bytes for the focused pane.
type Input struct {
	Type string `json:"type"`
	Data string `json:"data"`
}

// Resize reports the client's content-area size.
type Resize struct {
	Type string `json:"type"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

// Focus asks the server to focus a pane.
type Focus struct {
	Type   string `json:"type"`
	PaneID string `json:"pane_id"`
}

// Action is a named UI command with an optional target and text argument.
type Action struct {
	Type   string `json:"type"`
	Action string `json:"action"`
	Target string `json:"target,omitempty"`
	Text   string `json:"text,omitempty"`
}

// Mouse is a click, drag or scroll inside the content area, in content-area
// coordinates. The daemon decides whether it lands on a pane (focus it, or
// forward it to a program that wants mouse input) or on a divider (resize).
type Mouse struct {
	Type   string `json:"type"`
	Kind   string `json:"kind"`   // "press" | "release" | "drag" | "wheel"
	Button string `json:"button"` // "left" | "middle" | "right" | "up" | "down"
	X      int    `json:"x"`
	Y      int    `json:"y"`
	Alt    bool   `json:"alt,omitempty"`
	Ctrl   bool   `json:"ctrl,omitempty"`
	Shift  bool   `json:"shift,omitempty"`
}

// NewPane splits the focused pane. Direction is "right"/"down", or empty to
// let the server pick from the pane's shape.
type NewPane struct {
	Type      string `json:"type"`
	Cmd       string `json:"cmd,omitempty"`
	Label     string `json:"label,omitempty"`
	Direction string `json:"direction,omitempty"`
}

// MoveFocus moves focus to the pane in a direction.
type MoveFocus struct {
	Type      string `json:"type"`
	Direction string `json:"direction"`
}

// ResizePane nudges the divider of the split containing the focused pane.
type ResizePane struct {
	Type      string `json:"type"`
	Direction string `json:"direction"`
}

// ClientFocus reports the client's terminal window gaining or losing focus.
type ClientFocus struct {
	Type    string `json:"type"`
	Focused bool   `json:"focused"`
}

// Zoom toggles "focused pane fills the tab".
type Zoom struct {
	Type string `json:"type"`
}

// ClosePane asks the server to kill a pane.
type ClosePane struct {
	Type   string `json:"type"`
	PaneID string `json:"pane_id"`
}

// Notify tells clients an agent needs a human: it just got blocked, or it
// finished a turn nobody was watching. The daemon plays any system sound
// itself; the client decides whether to ring its terminal bell.
type Notify struct {
	Type   string `json:"type"`
	Kind   string `json:"kind"` // "blocked" | "done"
	PaneID string `json:"pane_id"`
	Agent  string `json:"agent,omitempty"`
	Title  string `json:"title,omitempty"`
}

// ManagerReply is the manager agent's answer, for the command bar.
type ManagerReply struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ErrorFrame reports a problem to the client.
type ErrorFrame struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// ServerMsg decodes any server->client message in one shot.
type ServerMsg struct {
	Type            string             `json:"type"`
	Session         string             `json:"session,omitempty"`
	Workspaces      []WorkspaceSummary `json:"workspaces,omitempty"`
	ActiveWorkspace string             `json:"active_workspace,omitempty"`
	Tabs            []TabSummary       `json:"tabs,omitempty"`
	ActiveTab       string             `json:"active_tab,omitempty"`
	Panes           []PaneSummary      `json:"panes,omitempty"`
	Agents          []AgentSummary     `json:"agents,omitempty"`
	Dividers        []DividerRect      `json:"dividers,omitempty"`
	Zoomed          bool               `json:"zoomed,omitempty"`
	Focus           string             `json:"focus,omitempty"`
	PaneID          string             `json:"pane_id,omitempty"`
	Cols            int                `json:"cols,omitempty"`
	Rows            int                `json:"rows,omitempty"`
	ANSI            string             `json:"ansi,omitempty"`
	CursorX         int                `json:"cursor_x,omitempty"`
	CursorY         int                `json:"cursor_y,omitempty"`
	Revision        uint64             `json:"revision,omitempty"`
	ExitCode        int                `json:"exit_code,omitempty"`
	Message         string             `json:"message,omitempty"`
	Version         string             `json:"version,omitempty"`
	Text            string             `json:"text,omitempty"`
	Kind            string             `json:"kind,omitempty"`
	Agent           string             `json:"agent,omitempty"`
	Title           string             `json:"title,omitempty"`
}

// Incoming decodes any client->server message in one shot.
type Incoming struct {
	Type         string `json:"type"`
	Session      string `json:"session,omitempty"`
	Focused      bool   `json:"focused,omitempty"`
	DoneColor    string `json:"done_color,omitempty"`
	Blink        bool   `json:"blink,omitempty"`
	ManagerCmd   string `json:"manager_cmd,omitempty"`
	Cols         int    `json:"cols,omitempty"`
	Rows         int    `json:"rows,omitempty"`
	Data         string `json:"data,omitempty"`
	PaneID       string `json:"pane_id,omitempty"`
	Cmd          string `json:"cmd,omitempty"`
	Label        string `json:"label,omitempty"`
	Direction    string `json:"direction,omitempty"`
	Action       string `json:"action,omitempty"`
	Target       string `json:"target,omitempty"`
	Text         string `json:"text,omitempty"`
	Kind         string `json:"kind,omitempty"`
	Button       string `json:"button,omitempty"`
	X            int    `json:"x,omitempty"`
	Y            int    `json:"y,omitempty"`
	Icons        string `json:"icons,omitempty"`
	Spinner      string `json:"spinner,omitempty"`
	Accent       string `json:"accent,omitempty"`
	HeaderFG     string `json:"header_fg,omitempty"`
	Border       string `json:"border,omitempty"`
	SpinnerColor string `json:"spinner_color,omitempty"`
	Borders      string `json:"borders,omitempty"`
	Alt          bool   `json:"alt,omitempty"`
	Ctrl         bool   `json:"ctrl,omitempty"`
	Shift        bool   `json:"shift,omitempty"`
}
