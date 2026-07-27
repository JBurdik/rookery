// Package apiproto defines the JSON-RPC-style v1 control protocol spoken on
// the API socket. This package has no dependency on the daemon or TUI so a
// future GUI client (or a script) can share just the wire schema.
package apiproto

import "encoding/json"

// Protocol is bumped whenever the wire format changes incompatibly.
const Protocol = 1

// Request is one JSON-RPC-style call.
type Request struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// Response carries either Result or Error, never both.
type Response struct {
	ID     string     `json:"id"`
	Result any        `json:"result,omitempty"`
	Error  *ErrorBody `json:"error,omitempty"`
}

type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// App error codes (JSON-RPC standard codes are reserved for framing errors).
const (
	ErrPaneNotFound   = "PANE_NOT_FOUND"
	ErrNotFound       = "NOT_FOUND"
	ErrInvalidParams  = "INVALID_PARAMS"
	ErrMethodNotFound = "METHOD_NOT_FOUND"
	ErrInternal       = "INTERNAL"
	ErrTimeout        = "TIMEOUT"
)

// --- workspaces ---

type WorkspaceInfo struct {
	WorkspaceID string `json:"workspace_id"`
	Name        string `json:"name"`
	Cwd         string `json:"cwd,omitempty"`
	Branch      string `json:"branch,omitempty"`
	Tabs        int    `json:"tabs"`
	Panes       int    `json:"panes"`
	// Status is the most attention-worthy agent status inside: blocked beats
	// done beats working, so one look at the list finds the workspace that
	// needs you.
	Status string `json:"status,omitempty"`
	Active bool   `json:"active,omitempty"`
}

type WorkspaceListResult struct {
	Workspaces []WorkspaceInfo `json:"workspaces"`
	Active     string          `json:"active,omitempty"`
}

type WorkspaceCreateParams struct {
	Name string `json:"name,omitempty"`
	Cwd  string `json:"cwd,omitempty"`
	// Empty creates the workspace without its usual first shell pane.
	Empty bool `json:"empty,omitempty"`
}

type WorkspaceCloseParams struct {
	WorkspaceID string `json:"workspace_id,omitempty"`
}

// RenameParams is shared by workspace.rename and tab.rename.
type RenameParams struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ClosedResult struct {
	ID     string `json:"id"`
	Closed bool   `json:"closed"`
}

// --- tabs ---

type TabInfo struct {
	TabID       string `json:"tab_id"`
	WorkspaceID string `json:"workspace_id"`
	Name        string `json:"name"`
	Panes       int    `json:"panes"`
	Status      string `json:"status,omitempty"`
	Active      bool   `json:"active,omitempty"`
	Zoomed      bool   `json:"zoomed,omitempty"`
}

type TabListParams struct {
	WorkspaceID string `json:"workspace_id,omitempty"`
}

type TabListResult struct {
	Tabs   []TabInfo `json:"tabs"`
	Active string    `json:"active,omitempty"`
}

type TabCreateParams struct {
	WorkspaceID string `json:"workspace_id,omitempty"`
	Name        string `json:"name,omitempty"`
	Cmd         string `json:"cmd,omitempty"`
	Cwd         string `json:"cwd,omitempty"`
	Empty       bool   `json:"empty,omitempty"`
}

type TabCloseParams struct {
	TabID string `json:"tab_id,omitempty"`
}

// --- ping ---

type PingResult struct {
	Pong     bool   `json:"pong"`
	Protocol int    `json:"protocol"`
	Version  string `json:"version"`
}

// --- pane.list ---

type PaneListParams struct {
	// All lists every pane in the session rather than just the visible tab's.
	All bool `json:"all,omitempty"`
}

type PaneListResult struct {
	Panes []PaneInfo `json:"panes"`
}

type PaneInfo struct {
	PaneID      string   `json:"pane_id"`
	WorkspaceID string   `json:"workspace_id,omitempty"`
	TabID       string   `json:"tab_id,omitempty"`
	Label       string   `json:"label,omitempty"`
	Cmd         string   `json:"cmd"`
	Args        []string `json:"args,omitempty"`
	Cwd         string   `json:"cwd,omitempty"`
	Status      string   `json:"status"` // "running" | "exited"
	// Agent is the detected agent ("claude", "codex", …), empty for a plain
	// command. AgentStatus is one of unknown/idle/working/blocked/done.
	Agent       string `json:"agent,omitempty"`
	AgentStatus string `json:"agent_status"`
	ExitCode    *int   `json:"exit_code,omitempty"`
	Cols        int    `json:"cols"`
	Rows        int    `json:"rows"`
	PID         int    `json:"pid,omitempty"`
	CreatedAt   string `json:"created_at"`
	Revision    uint64 `json:"revision"`
}

// --- pane.create ---

type PaneCreateParams struct {
	Cmd   string            `json:"cmd"`
	Args  []string          `json:"args,omitempty"`
	Cwd   string            `json:"cwd,omitempty"`
	Env   map[string]string `json:"env,omitempty"`
	Label string            `json:"label,omitempty"`
	Cols  int               `json:"cols,omitempty"`
	Rows  int               `json:"rows,omitempty"`

	// From is the pane to split to make room for this one; empty means the
	// focused pane. Direction is "right"/"down" (or "h"/"v"); empty picks
	// whichever keeps both panes usable, based on the source pane's shape.
	From      string `json:"from,omitempty"`
	Direction string `json:"direction,omitempty"`
	// NoFocus creates the pane without moving the user's focus to it.
	NoFocus bool `json:"no_focus,omitempty"`
}

// --- pane.rename ---

type PaneRenameParams struct {
	PaneID string `json:"pane_id"`
	Label  string `json:"label"`
}

// --- pane.resize ---

type PaneResizeParams struct {
	PaneID string `json:"pane_id"`
	// Direction is which edge to push: "left"/"right"/"up"/"down".
	Direction string `json:"direction"`
	// Amount is in cells; 0 means a sensible default step.
	Amount int `json:"amount,omitempty"`
}

// --- pane.zoom ---

type PaneZoomParams struct {
	// Zoom is tri-state: nil toggles, true/false set it explicitly.
	Zoom *bool `json:"zoom,omitempty"`
}

type PaneZoomResult struct {
	Zoomed bool `json:"zoomed"`
}

// --- debug.pane ---

// PaneDebugResult exposes exactly what the agent-status detector sees, so a
// wrong verdict can be diagnosed from a shell.
type PaneDebugResult struct {
	PaneID      string   `json:"pane_id"`
	Agent       string   `json:"agent"`
	AgentStatus string   `json:"agent_status"`
	RawState    string   `json:"raw_state"`
	Seen        bool     `json:"seen"`
	Busy        bool     `json:"busy"`
	Running     string   `json:"running"`
	Foreground  []string `json:"foreground,omitempty"`
	Title       string   `json:"title"`
	Bottom      []string `json:"bottom"`
}

// --- manager.send ---

// ManagerSendParams hands a request to the manager agent, starting it if it
// isn't running yet.
type ManagerSendParams struct {
	Text string `json:"text"`
	// Cmd overrides which agent to start; empty uses the configured one.
	Cmd string `json:"cmd,omitempty"`
}

// ManagerSendResult reports where the request went and how many are still
// queued ahead of it.
type ManagerSendResult struct {
	PaneID string `json:"pane_id"`
	Queued int    `json:"queued"`
}

// --- wait.pane ---

// WaitPaneParams blocks until a pane reaches one of the wanted states, or
// until the timeout expires. A pane already in a wanted state returns
// immediately.
type WaitPaneParams struct {
	PaneID string `json:"pane_id"`
	// AgentStatus is the set of agent statuses to wait for
	// (idle/working/blocked/done). Empty means "wait for process exit".
	AgentStatus []string `json:"agent_status,omitempty"`
	// TimeoutMS caps the wait; 0 uses a default, negative waits forever.
	TimeoutMS int `json:"timeout_ms,omitempty"`
}

type WaitPaneResult struct {
	PaneID      string `json:"pane_id"`
	Matched     bool   `json:"matched"`
	AgentStatus string `json:"agent_status"`
	Status      string `json:"status"`
	ExitCode    *int   `json:"exit_code,omitempty"`
	TimedOut    bool   `json:"timed_out"`
	WaitedMS    int    `json:"waited_ms"`
}

// --- pane.close ---

type PaneCloseParams struct {
	PaneID string `json:"pane_id"`
	Force  bool   `json:"force,omitempty"`
}

type PaneCloseResult struct {
	PaneID string `json:"pane_id"`
	Closed bool   `json:"closed"`
}

// --- pane.send_keys ---

type PaneSendKeysParams struct {
	PaneID     string `json:"pane_id"`
	Text       string `json:"text,omitempty"`
	PressEnter bool   `json:"press_enter,omitempty"`
}

type PaneSendKeysResult struct {
	OK           bool `json:"ok"`
	BytesWritten int  `json:"bytes_written"`
}

// --- pane.read ---

type PaneReadParams struct {
	PaneID string `json:"pane_id"`
	Source string `json:"source,omitempty"` // "screen" (default) | "scrollback"
	Lines  int    `json:"lines,omitempty"`  // scrollback only; <=0 means all available
	Format string `json:"format,omitempty"` // "plain" (default) | "ansi"
}

type PaneReadResult struct {
	PaneID    string `json:"pane_id"`
	Source    string `json:"source"`
	Format    string `json:"format"`
	Text      string `json:"text"`
	Revision  uint64 `json:"revision"`
	Truncated bool   `json:"truncated"`
}

// --- pane.status ---

type PaneStatusParams struct {
	PaneID string `json:"pane_id"`
}

type PaneStatusResult struct {
	PaneID      string `json:"pane_id"`
	Status      string `json:"status"`
	Agent       string `json:"agent,omitempty"`
	AgentStatus string `json:"agent_status"`
	ExitCode    *int   `json:"exit_code,omitempty"`
	PID         int    `json:"pid,omitempty"`
	Cols        int    `json:"cols"`
	Rows        int    `json:"rows"`
	StartedAt   string `json:"started_at"`
}

// --- pane.focus ---

type PaneFocusParams struct {
	PaneID string `json:"pane_id"`
}

type PaneFocusResult struct {
	PaneID  string `json:"pane_id"`
	Focused bool   `json:"focused"`
}

// --- server.shutdown ---

type ServerShutdownResult struct {
	OK bool `json:"ok"`
}
