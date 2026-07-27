package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/jirkab/rookery/internal/apiproto"
)

// RunPane implements `rook pane <subcommand>` — the surface an agent drives
// rookery through from a shell. Everything prints the raw API result as indented
// JSON so it can be piped into jq (or read straight out of a tool call)
// without a second parsing dialect to learn.
func RunPane(args []string) error {
	if len(args) == 0 {
		paneUsage()
		return errors.New("pane: missing subcommand")
	}

	switch args[0] {
	case "ls", "list":
		return paneList(args[1:])
	case "new", "create", "split":
		return paneNew(args[1:])
	case "send", "send-keys", "run":
		return paneSend(args[1:])
	case "read":
		return paneRead(args[1:])
	case "status":
		return paneStatus(args[1:])
	case "focus":
		return paneFocus(args[1:])
	case "rename":
		return paneRename(args[1:])
	case "zoom":
		return paneZoom(args[1:])
	case "current":
		return paneCurrent(args[1:])
	case "kill", "close":
		return paneKill(args[1:])
	case "-h", "--help", "help":
		paneUsage()
		return nil
	default:
		paneUsage()
		return fmt.Errorf("pane: unknown subcommand %q", args[0])
	}
}

func paneList(args []string) error {
	fs := newPaneFlags("pane ls")
	if err := fs.parse(args); err != nil {
		return err
	}
	return callAndPrint(fs.session, "pane.list", nil)
}

func paneNew(args []string) error {
	fs := newPaneFlags("pane new")
	label := fs.set.String("label", "", "human-readable pane label (shown in the sidebar)")
	cwd := fs.set.String("cwd", "", "working directory for the command")
	cols := fs.set.Int("cols", 0, "initial terminal width")
	rows := fs.set.Int("rows", 0, "initial terminal height")
	direction := fs.set.String("direction", "", "split direction: right | down (default: fits the pane's shape)")
	from := fs.set.String("from", "", "pane to split (default: the focused pane)")
	noFocus := fs.set.Bool("no-focus", false, "create the pane without moving focus to it")
	current := fs.set.Bool("current", false, "split the calling pane ($ROOK_PANE) rather than the focused one")
	var env envFlag
	fs.set.Var(&env, "env", "extra environment variable KEY=VALUE (repeatable)")
	if err := fs.parse(args); err != nil {
		return err
	}

	source := *from
	if *current && source == "" {
		source = os.Getenv(PaneEnvVar)
	}

	// Everything after the flags is the command line to run; a leading "--"
	// is conventional but optional.
	cmdline := fs.args()
	params := apiproto.PaneCreateParams{
		Label: *label, Cwd: *cwd, Cols: *cols, Rows: *rows, Env: env,
		Direction: *direction, From: source, NoFocus: *noFocus,
	}
	if len(cmdline) > 0 {
		params.Cmd, params.Args = cmdline[0], cmdline[1:]
	}
	return callAndPrint(fs.session, "pane.create", params)
}

func paneRename(args []string) error {
	fs := newPaneFlags("pane rename")
	if err := fs.parse(args); err != nil {
		return err
	}
	rest := fs.args()
	if len(rest) < 2 {
		return errors.New("usage: rook pane rename <pane-id> <label>")
	}
	return callAndPrint(fs.session, "pane.rename", apiproto.PaneRenameParams{
		PaneID: rest[0],
		Label:  strings.Join(rest[1:], " "),
	})
}

func paneZoom(args []string) error {
	fs := newPaneFlags("pane zoom")
	on := fs.set.Bool("on", false, "zoom in (default: toggle)")
	off := fs.set.Bool("off", false, "zoom out (default: toggle)")
	if err := fs.parse(args); err != nil {
		return err
	}
	params := apiproto.PaneZoomParams{}
	switch {
	case *on:
		params.Zoom = boolPtr(true)
	case *off:
		params.Zoom = boolPtr(false)
	}
	return callAndPrint(fs.session, "pane.zoom", params)
}

// paneCurrent reports the pane the caller is running inside, so an agent can
// answer "which one am I?" without parsing the environment itself.
func paneCurrent(args []string) error {
	fs := newPaneFlags("pane current")
	if err := fs.parse(args); err != nil {
		return err
	}
	paneID := os.Getenv(PaneEnvVar)
	if paneID == "" {
		return errors.New("not running inside a rookery pane ($" + PaneEnvVar + " is unset)")
	}
	return callAndPrint(fs.session, "pane.status", apiproto.PaneStatusParams{PaneID: paneID})
}

func boolPtr(b bool) *bool { return &b }

func paneSend(args []string) error {
	fs := newPaneFlags("pane send")
	noEnter := fs.set.Bool("no-enter", false, "do not append a carriage return")
	if err := fs.parse(args); err != nil {
		return err
	}
	rest := fs.args()
	if len(rest) < 1 {
		return errors.New("usage: rook pane send <pane-id> [text...]")
	}

	// Remaining words are joined with spaces so an unquoted prompt still
	// arrives intact: `rook pane send p2 fix the failing test`.
	params := apiproto.PaneSendKeysParams{
		PaneID:     rest[0],
		Text:       strings.Join(rest[1:], " "),
		PressEnter: !*noEnter,
	}
	return callAndPrint(fs.session, "pane.send_keys", params)
}

func paneRead(args []string) error {
	fs := newPaneFlags("pane read")
	scrollback := fs.set.Bool("scrollback", false, "read the scrollback transcript instead of the live screen")
	lines := fs.set.Int("lines", 0, "with --scrollback: last N lines (0 = all retained)")
	ansi := fs.set.Bool("ansi", false, "keep ANSI styling (default: plain text)")
	raw := fs.set.Bool("raw", false, "print just the pane text, not the JSON envelope")
	if err := fs.parse(args); err != nil {
		return err
	}
	paneID, err := fs.firstArg("pane read")
	if err != nil {
		return err
	}

	params := apiproto.PaneReadParams{PaneID: paneID, Lines: *lines}
	if *scrollback {
		params.Source = "scrollback"
	}
	if *ansi {
		params.Format = "ansi"
	}

	resp, err := apiCall(fs.session, apiproto.Request{ID: "pane-read", Method: "pane.read", Params: mustJSON(params)})
	if err != nil {
		return err
	}
	if resp.Error != nil {
		return respError(resp)
	}
	if !*raw {
		return printJSON(resp.Result)
	}
	var result apiproto.PaneReadResult
	if err := decodeResult(resp.Result, &result); err != nil {
		return err
	}
	fmt.Println(result.Text)
	return nil
}

func paneStatus(args []string) error {
	fs := newPaneFlags("pane status")
	if err := fs.parse(args); err != nil {
		return err
	}
	paneID, err := fs.firstArg("pane status")
	if err != nil {
		return err
	}
	return callAndPrint(fs.session, "pane.status", apiproto.PaneStatusParams{PaneID: paneID})
}

func paneFocus(args []string) error {
	fs := newPaneFlags("pane focus")
	if err := fs.parse(args); err != nil {
		return err
	}
	paneID, err := fs.firstArg("pane focus")
	if err != nil {
		return err
	}
	return callAndPrint(fs.session, "pane.focus", apiproto.PaneFocusParams{PaneID: paneID})
}

func paneKill(args []string) error {
	fs := newPaneFlags("pane kill")
	if err := fs.parse(args); err != nil {
		return err
	}
	paneID, err := fs.firstArg("pane kill")
	if err != nil {
		return err
	}
	return callAndPrint(fs.session, "pane.close", apiproto.PaneCloseParams{PaneID: paneID})
}

// RunAPI implements `rook api <method> [json-params]` — the escape hatch for
// any method the typed subcommands above don't wrap.
func RunAPI(args []string) error {
	fs := newPaneFlags("api")
	if err := fs.parse(args); err != nil {
		return err
	}
	rest := fs.args()
	if len(rest) == 0 {
		return errors.New(`usage: rook api <method> ['{"json":"params"}']`)
	}

	req := apiproto.Request{ID: "cli-api", Method: rest[0]}
	if len(rest) > 1 {
		if !json.Valid([]byte(rest[1])) {
			return fmt.Errorf("params is not valid JSON: %s", rest[1])
		}
		req.Params = json.RawMessage(rest[1])
	}

	resp, err := apiCall(fs.session, req)
	if err != nil {
		return err
	}
	if resp.Error != nil {
		return respError(resp)
	}
	return printJSON(resp.Result)
}

// --- shared plumbing ---

// paneFlags is the common shape of every pane subcommand: a --session flag
// plus positional arguments.
type paneFlags struct {
	set     *flag.FlagSet
	session string
	sessArg *string
	rest    []string
}

func newPaneFlags(name string) *paneFlags {
	fs := &paneFlags{set: flag.NewFlagSet(name, flag.ContinueOnError)}
	fs.sessArg = fs.set.String("session", "", "session name (default: "+defaultSessionName()+")")
	return fs
}

// parse accepts flags before *and* after positional arguments, so
// `rook pane read p1 --raw` behaves the way anyone would expect (Go's flag
// package stops at the first non-flag argument and would silently ignore
// --raw). Everything after a literal `--` is left alone: that's the wrapped
// command line for `pane new`, whose own flags must not be eaten here.
func (f *paneFlags) parse(args []string) error {
	head, tail := args, []string(nil)
	for i, a := range args {
		if a == "--" {
			head, tail = args[:i], args[i+1:]
			break
		}
	}

	for {
		if err := f.set.Parse(head); err != nil {
			return err
		}
		positional := f.set.Args()
		if len(positional) == 0 {
			break
		}
		f.rest = append(f.rest, positional[0])
		head = positional[1:]
	}
	f.rest = append(f.rest, tail...)

	f.session = *f.sessArg
	if f.session == "" {
		f.session = defaultSessionName()
	}
	return nil
}

// args returns the positional arguments, in their original order.
func (f *paneFlags) args() []string { return f.rest }

func (f *paneFlags) firstArg(cmd string) (string, error) {
	if len(f.rest) < 1 {
		return "", fmt.Errorf("usage: rook %s <pane-id>", cmd)
	}
	return f.rest[0], nil
}

// envFlag collects repeated -env KEY=VALUE flags.
type envFlag map[string]string

func (e *envFlag) String() string { return "" }

func (e *envFlag) Set(v string) error {
	k, val, found := strings.Cut(v, "=")
	if !found || k == "" {
		return fmt.Errorf("expected KEY=VALUE, got %q", v)
	}
	if *e == nil {
		*e = envFlag{}
	}
	(*e)[k] = val
	return nil
}

func callAndPrint(session, method string, params any) error {
	req := apiproto.Request{ID: "cli-" + method, Method: method}
	if params != nil {
		req.Params = mustJSON(params)
	}
	resp, err := apiCall(session, req)
	if err != nil {
		return err
	}
	if resp.Error != nil {
		return respError(resp)
	}
	return printJSON(resp.Result)
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func respError(resp apiproto.Response) error {
	return fmt.Errorf("%s: %s", resp.Error.Code, resp.Error.Message)
}

func paneUsage() {
	fmt.Fprint(os.Stderr, `rook pane — drive panes from a script or an agent

Usage:
  rook pane ls                                   list panes (JSON)
  rook pane new [flags] [-- cmd args...]         spawn a pane (no cmd = $SHELL)
  rook pane send <pane> [text...]                type into a pane, then Enter
  rook pane read <pane> [flags]                  read a pane's screen or scrollback
  rook pane status <pane>                        one pane's run and agent state
  rook pane current                              the pane this process runs in
  rook pane focus <pane>                         show this pane in attached clients
  rook pane rename <pane> <label>                change a pane's sidebar label
  rook pane zoom [--on|--off]                    focused pane fills the screen
  rook pane kill <pane>                          terminate a pane

Common flags:
  --session NAME    target session (default: $ROOK_SESSION, else "default")

pane new (alias: pane split) — a new pane splits an existing one:
  --direction right|down   where to put it (default: fits the pane's shape)
  --from PANE              which pane to split (default: the focused one)
  --current                split the calling pane ($ROOK_PANE)
  --no-focus               leave the user's focus where it is
  --label NAME             sidebar label       --cwd DIR   working directory
  --env K=V                extra env (repeatable)

pane read:
  --scrollback      read the transcript instead of the live screen
  --lines N         with --scrollback, last N lines
  --ansi            keep styling       --raw          print text, not JSON

Examples:
  rook pane new --label reviewer --no-focus -- claude
  rook pane send p2 review the current diff
  rook wait agent-status p2 --status done,blocked --timeout 120000
  rook pane read p2 --scrollback --lines 60 --raw
  rook api pane.list
`)
}
