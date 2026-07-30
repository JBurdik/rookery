package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/jirkab/rookery/internal/apiproto"
	"github.com/jirkab/rookery/internal/session"
)

// RunStatus implements `rook status [session]`. It is deliberately a compact
// human-facing summary: `rook pane ls` remains the machine-readable, detailed
// view when a script needs pane ids and metadata.
func RunStatus(args []string) error {
	name := sessionArg(args)
	if !session.IsLive(session.APISocketPath(name)) {
		fmt.Printf("session %q: stopped\n", name)
		return nil
	}

	panesResp, err := apiCall(name, apiproto.Request{
		ID: "status-panes", Method: "pane.list", Params: mustJSON(apiproto.PaneListParams{All: true}),
	})
	if err != nil {
		return err
	}
	if panesResp.Error != nil {
		return fmt.Errorf("%s: %s", panesResp.Error.Code, panesResp.Error.Message)
	}
	var panes apiproto.PaneListResult
	if err := decodeResult(panesResp.Result, &panes); err != nil {
		return err
	}

	workspacesResp, err := apiCall(name, apiproto.Request{ID: "status-workspaces", Method: "workspace.list"})
	if err != nil {
		return err
	}
	if workspacesResp.Error != nil {
		return fmt.Errorf("%s: %s", workspacesResp.Error.Code, workspacesResp.Error.Message)
	}
	var workspaces apiproto.WorkspaceListResult
	if err := decodeResult(workspacesResp.Result, &workspaces); err != nil {
		return err
	}

	counts := map[string]int{}
	running := 0
	for _, pane := range panes.Panes {
		if pane.Status == "running" {
			running++
		}
		if pane.AgentStatus != "" && pane.AgentStatus != "unknown" {
			counts[pane.AgentStatus]++
		}
	}
	parts := make([]string, 0, len(counts))
	for status, count := range counts {
		parts = append(parts, fmt.Sprintf("%s=%d", status, count))
	}
	sort.Strings(parts)
	agents := "none"
	if len(parts) > 0 {
		agents = strings.Join(parts, ", ")
	}
	fmt.Printf("session %q: running · workspaces=%d panes=%d active=%d agents: %s\n",
		name, len(workspaces.Workspaces), len(panes.Panes), running, agents)
	return nil
}

// RunReload implements `rook reload [session]`.
func RunReload(args []string) error {
	name := sessionArg(args)
	resp, err := apiCall(name, apiproto.Request{ID: "reload", Method: "server.reload"})
	if err != nil {
		return err
	}
	if resp.Error != nil {
		return fmt.Errorf("%s: %s", resp.Error.Code, resp.Error.Message)
	}
	fmt.Printf("session %q reloaded\n", name)
	return nil
}

// RunDelete implements `rook delete [session]`. Deleting a session is a
// separate, explicit lifecycle action: `rook kill` stops it but preserves the
// layout so a future attach can restore it.
func RunDelete(args []string) error {
	name := sessionArg(args)
	if session.IsLive(session.APISocketPath(name)) {
		return fmt.Errorf("session %q is running; stop it with `rook kill %s` before deleting it", name, name)
	}
	if err := session.Remove(name); err != nil {
		return err
	}
	fmt.Printf("session %q deleted\n", name)
	return nil
}

// RunSession provides a discoverable session namespace while preserving the
// short top-level commands people reach for most often.
func RunSession(args []string) error {
	if len(args) == 0 {
		return RunStatus(nil)
	}
	switch args[0] {
	case "ls", "list":
		return RunLs(args[1:])
	case "attach", "a":
		return RunAttach(args[1:])
	case "status", "show":
		return RunStatus(args[1:])
	case "kill", "stop":
		return RunKill(args[1:])
	case "delete", "rm", "remove":
		return RunDelete(args[1:])
	case "reload":
		return RunReload(args[1:])
	default:
		return fmt.Errorf("unknown session command %q (try ls, attach, status, kill, delete, or reload)", args[0])
	}
}
