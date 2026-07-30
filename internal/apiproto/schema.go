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
	// Reason describes the live detector path. Agent panes also identify the
	// manifest rule that won, when one matched.
	Reason       string `json:"reason"`
	RuleID       string `json:"rule_id,omitempty"`
	RuleSource   string `json:"rule_source,omitempty"`
	RulePriority int    `json:"rule_priority,omitempty"`
	RuleRegion   string `json:"rule_region,omitempty"`
}

// --- pane.report ---

// PaneReportParams is an agent integration telling rookery what it is doing,
// instead of leaving rookery to work it out from the screen.
type PaneReportParams struct {
	PaneID string `json:"pane_id,omitempty"`
	// Status is idle, working or blocked. Empty clears the report and hands
	// the pane back to screen detection.
	Status string `json:"status"`
	// SessionRef is the agent's own session identifier, kept for a future
	// "resume this agent" — rookery only records it today.
	SessionRef string `json:"session_ref,omitempty"`
	// Agent names the integration, for diagnosis.
	Agent string `json:"agent,omitempty"`
	// KeepStatus leaves the pane's reported status untouched. For a report
	// that only carries a session id (Codex's SessionStart, OpenCode's
	// session.updated) sending Status at all would otherwise clear it back
	// to screen detection, which is the opposite of what a session-only
	// report means.
	KeepStatus bool `json:"keep_status,omitempty"`
}

type PaneReportResult struct {
	PaneID string `json:"pane_id"`
	Status string `json:"status"`
}

// --- watch ---

// Event kinds.
const (
	EventStatus     = "agent_status" // an agent changed what it is doing
	EventPaneNew    = "pane_new"
	EventPaneClosed = "pane_closed"
	EventPaneExit   = "pane_exit" // the process ended
	EventNotify     = "notify"    // an agent wants a human
)

// Event is one line of `rook watch` output.
//
// Deliberately flat: this is consumed by shell pipelines and other agents, so
// every field is one jq hop away and there is nothing to walk into.
type Event struct {
	Kind     string `json:"kind"`
	At       string `json:"at"`
	Session  string `json:"session"`
	PaneID   string `json:"pane_id,omitempty"`
	Label    string `json:"label,omitempty"`
	Agent    string `json:"agent,omitempty"`
	Status   string `json:"status,omitempty"`
	Previous string `json:"previous,omitempty"`
	Fan      string `json:"fan,omitempty"`
	ExitCode *int   `json:"exit_code,omitempty"`
}

// WatchStarted acknowledges a stream before any events arrive.
type WatchStarted struct {
	Watching bool `json:"watching"`
}

// WatchParams filters the stream. Empty lists mean everything.
type WatchParams struct {
	Panes    []string `json:"panes,omitempty"`
	Statuses []string `json:"statuses,omitempty"`
	Kinds    []string `json:"kinds,omitempty"`
}

// --- fan ---

// FanStartParams runs one prompt past several agents at once.
type FanStartParams struct {
	Prompt string   `json:"prompt"`
	Agents int      `json:"agents,omitempty"` // default 3
	Cmd    string   `json:"cmd,omitempty"`    // default: config.json's agent.command
	Args   []string `json:"args,omitempty"`
	Name   string   `json:"name,omitempty"`
	// Worktree gives each agent its own git checkout and branch, so they
	// cannot fight over the index and their answers can be diffed.
	Worktree bool `json:"worktree,omitempty"`
	// Base is the commit-ish the branches start from; empty means current HEAD.
	Base string `json:"base,omitempty"`
}

type FanPane struct {
	PaneID      string `json:"pane_id"`
	Fan         string `json:"fan,omitempty"`
	Label       string `json:"label"`
	Branch      string `json:"branch,omitempty"`
	Worktree    string `json:"worktree,omitempty"`
	Status      string `json:"status,omitempty"`
	AgentStatus string `json:"agent_status,omitempty"`
	// Diffstat is what the agent actually changed on disk.
	Diffstat string `json:"diffstat,omitempty"`
}

type FanStartResult struct {
	Fan    string    `json:"fan"`
	Prompt string    `json:"prompt"`
	Panes  []FanPane `json:"panes"`
}

type FanListParams struct {
	Fan string `json:"fan,omitempty"`
}

type FanListResult struct {
	Fans  []string  `json:"fans"`
	Panes []FanPane `json:"panes"`
}

type FanCleanParams struct {
	Fan string `json:"fan"`
	// Force discards uncommitted work in the worktrees.
	Force bool `json:"force,omitempty"`
}

type FanCleanResult struct {
	Fan      string   `json:"fan"`
	Closed   int      `json:"closed"`
	Removed  int      `json:"removed"`
	Problems []string `json:"problems,omitempty"`
}

// FanReviewParams selects one candidate's patch, or all candidates when
// Candidate is empty. A candidate is identified by its fan label or pane ID.
type FanReviewParams struct {
	Fan       string `json:"fan"`
	Candidate string `json:"candidate,omitempty"`
	Patch     bool   `json:"patch,omitempty"`
}

type FanReviewCandidate struct {
	PaneID    string   `json:"pane_id"`
	Label     string   `json:"label"`
	Branch    string   `json:"branch"`
	Base      string   `json:"base,omitempty"`
	Commits   []string `json:"commits,omitempty"`
	Files     []string `json:"files,omitempty"`
	Diffstat  string   `json:"diffstat,omitempty"`
	Dirty     bool     `json:"dirty"`
	DirtyStat string   `json:"dirty_stat,omitempty"`
	Patch     string   `json:"patch,omitempty"`
}

type FanReviewResult struct {
	Fan        string               `json:"fan"`
	Candidates []FanReviewCandidate `json:"candidates"`
}

// FanPromoteParams fast-forwards the originating workspace to a candidate.
// Apply is deliberately required: callers can use the same endpoint to check
// whether a candidate is promotable before changing the repository.
type FanPromoteParams struct {
	Fan       string `json:"fan"`
	Candidate string `json:"candidate"`
	Apply     bool   `json:"apply,omitempty"`
}

type FanPromoteResult struct {
	Fan       string `json:"fan"`
	Candidate string `json:"candidate"`
	Branch    string `json:"branch"`
	Applied   bool   `json:"applied"`
	Message   string `json:"message"`
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

// WaitOutputParams waits until the current screen or retained scrollback
// matches Match (a literal substring) or Regex. Exactly one is required.
type WaitOutputParams struct {
	PaneID    string `json:"pane_id"`
	Match     string `json:"match,omitempty"`
	Regex     string `json:"regex,omitempty"`
	Source    string `json:"source,omitempty"`
	TimeoutMS int    `json:"timeout_ms,omitempty"`
}

type WaitOutputResult struct {
	PaneID   string `json:"pane_id"`
	Matched  bool   `json:"matched"`
	Match    string `json:"match,omitempty"`
	Source   string `json:"source"`
	Revision uint64 `json:"revision"`
	TimedOut bool   `json:"timed_out"`
	WaitedMS int    `json:"waited_ms"`
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

// PaneSendTextParams writes text exactly as supplied. It never interprets key
// names or submits the pane's composer; use pane.run to submit a prompt.
type PaneSendTextParams struct {
	PaneID string `json:"pane_id"`
	Text   string `json:"text"`
}

type PaneSendKeysParams struct {
	PaneID     string `json:"pane_id"`
	Text       string `json:"text,omitempty"`
	PressEnter bool   `json:"press_enter,omitempty"`
}

type PaneSendKeysResult struct {
	OK           bool `json:"ok"`
	BytesWritten int  `json:"bytes_written"`
}

// PaneRunParams is the intentional "send this prompt" operation. Keeping it
// separate from send_text means automation cannot submit work by accident.
type PaneRunParams struct {
	PaneID string `json:"pane_id"`
	Text   string `json:"text"`
}

// --- pane.inspect / pane.neighbor / pane.move ---

type PaneInspectParams struct {
	PaneID string `json:"pane_id"`
}
type PaneRect struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}
type PaneInspectResult struct {
	Pane      PaneInfo          `json:"pane"`
	Focused   bool              `json:"focused"`
	Zoomed    bool              `json:"zoomed"`
	Rect      *PaneRect         `json:"rect,omitempty"`
	Neighbors map[string]string `json:"neighbors"`
}
type PaneNeighborParams struct {
	PaneID    string `json:"pane_id"`
	Direction string `json:"direction"`
}
type PaneNeighborResult struct {
	PaneID     string `json:"pane_id"`
	Direction  string `json:"direction"`
	NeighborID string `json:"neighbor_id,omitempty"`
}

// PaneMoveParams moves an existing PTY into another tab. From is the
// destination pane to split, defaulting to that tab's focused pane.
type PaneMoveParams struct {
	PaneID    string `json:"pane_id"`
	TabID     string `json:"tab_id"`
	From      string `json:"from,omitempty"`
	Direction string `json:"direction,omitempty"`
	NoFocus   bool   `json:"no_focus,omitempty"`
}
type PaneMoveResult struct {
	PaneID string `json:"pane_id"`
	TabID  string `json:"tab_id"`
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

// --- server lifecycle ---

type ServerShutdownResult struct {
	OK bool `json:"ok"`
}

// ServerReloadResult confirms that a daemon re-read its configuration and
// agent manifests without interrupting any panes.
type ServerReloadResult struct {
	OK bool `json:"ok"`
}
