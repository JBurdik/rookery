package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/jirkab/rookery/internal/apiproto"
)

// RunWorkspace implements `rook workspace <subcommand>`.
func RunWorkspace(args []string) error {
	if len(args) == 0 {
		workspaceUsage()
		return errors.New("workspace: missing subcommand")
	}
	switch args[0] {
	case "ls", "list":
		return simpleCall(args[1:], "workspace.list", nil)
	case "new", "create":
		return workspaceNew(args[1:])
	case "focus":
		return targetCall(args[1:], "workspace focus", func(id string) any {
			return apiproto.WorkspaceCloseParams{WorkspaceID: id}
		}, "workspace.focus")
	case "rename":
		return renameCall(args[1:], "workspace rename", "workspace.rename")
	case "close", "kill":
		return targetCall(args[1:], "workspace close", func(id string) any {
			return apiproto.WorkspaceCloseParams{WorkspaceID: id}
		}, "workspace.close")
	case "-h", "--help", "help":
		workspaceUsage()
		return nil
	default:
		workspaceUsage()
		return fmt.Errorf("workspace: unknown subcommand %q", args[0])
	}
}

func workspaceNew(args []string) error {
	fs := newPaneFlags("workspace new")
	cwd := fs.set.String("cwd", "", "working directory (default: the current one)")
	empty := fs.set.Bool("empty", false, "create it without an initial shell pane")
	if err := fs.parse(args); err != nil {
		return err
	}
	dir := *cwd
	if dir == "" {
		dir, _ = os.Getwd()
	}
	return callAndPrint(fs.session, "workspace.create", apiproto.WorkspaceCreateParams{
		Name:  strings.Join(fs.args(), " "),
		Cwd:   dir,
		Empty: *empty,
	})
}

// RunTab implements `rook tab <subcommand>`.
func RunTab(args []string) error {
	if len(args) == 0 {
		tabUsage()
		return errors.New("tab: missing subcommand")
	}
	switch args[0] {
	case "ls", "list":
		return simpleCall(args[1:], "tab.list", nil)
	case "new", "create":
		return tabNew(args[1:])
	case "focus":
		return targetCall(args[1:], "tab focus", func(id string) any {
			return apiproto.TabCloseParams{TabID: id}
		}, "tab.focus")
	case "rename":
		return renameCall(args[1:], "tab rename", "tab.rename")
	case "close", "kill":
		return targetCall(args[1:], "tab close", func(id string) any {
			return apiproto.TabCloseParams{TabID: id}
		}, "tab.close")
	case "-h", "--help", "help":
		tabUsage()
		return nil
	default:
		tabUsage()
		return fmt.Errorf("tab: unknown subcommand %q", args[0])
	}
}

func tabNew(args []string) error {
	fs := newPaneFlags("tab new")
	workspace := fs.set.String("workspace", "", "workspace to create it in (default: the active one)")
	cwd := fs.set.String("cwd", "", "working directory for the first pane")
	empty := fs.set.Bool("empty", false, "create it without an initial pane")
	if err := fs.parse(args); err != nil {
		return err
	}
	rest := fs.args()
	params := apiproto.TabCreateParams{
		WorkspaceID: *workspace,
		Cwd:         *cwd,
		Empty:       *empty,
	}
	if len(rest) > 0 {
		params.Name = rest[0]
	}
	return callAndPrint(fs.session, "tab.create", params)
}

// RunGit implements `rook git` — open a git UI in a pane, in the active
// workspace's directory. The daemon picks the tool, since its PATH is the one
// that matters.
func RunGit(args []string) error {
	fs := newPaneFlags("git")
	if err := fs.parse(args); err != nil {
		return err
	}
	return callAndPrint(fs.session, "pane.git", nil)
}

// --- shared helpers ---

func simpleCall(args []string, method string, params any) error {
	fs := newPaneFlags(method)
	if err := fs.parse(args); err != nil {
		return err
	}
	return callAndPrint(fs.session, method, params)
}

// targetCall wraps the "one id, one method" shape shared by focus and close.
func targetCall(args []string, usage string, build func(string) any, method string) error {
	fs := newPaneFlags(usage)
	if err := fs.parse(args); err != nil {
		return err
	}
	id := ""
	if rest := fs.args(); len(rest) > 0 {
		id = rest[0]
	}
	if id == "" && strings.HasSuffix(usage, "focus") {
		return fmt.Errorf("usage: rook %s <id>", usage)
	}
	return callAndPrint(fs.session, method, build(id))
}

func renameCall(args []string, usage, method string) error {
	fs := newPaneFlags(usage)
	if err := fs.parse(args); err != nil {
		return err
	}
	rest := fs.args()
	if len(rest) < 2 {
		return fmt.Errorf("usage: rook %s <id> <name>", usage)
	}
	return callAndPrint(fs.session, method, apiproto.RenameParams{
		ID:   rest[0],
		Name: strings.Join(rest[1:], " "),
	})
}

func workspaceUsage() {
	fmt.Fprint(os.Stderr, `rook workspace — one per repo, task or investigation

Usage:
  rook workspace ls                    list workspaces, with agent status
  rook workspace new [name] [--cwd D]  create one (and a first shell pane)
  rook workspace focus <id>            switch to it
  rook workspace rename <id> <name>
  rook workspace close [id]            close it and everything inside

A workspace owns tabs; a tab owns a layout of panes. Ids look like w1, w1:t2,
w1:p3 — a pane id says which workspace it lives in.
`)
}

func tabUsage() {
	fmt.Fprint(os.Stderr, `rook tab — a layout of panes inside a workspace

Usage:
  rook tab ls                          list tabs in the active workspace
  rook tab new [name] [--workspace W]  create one (and a first pane)
  rook tab focus <id>                  switch to it
  rook tab rename <id> <name>
  rook tab close [id]                  close it and its panes
`)
}
