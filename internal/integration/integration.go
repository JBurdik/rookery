// Package integration installs the agent-side hooks that let an agent tell
// rookery what it is doing, instead of leaving rookery to infer it from the
// screen.
//
// Screen detection is a good heuristic and it is what makes an un-integrated
// agent work at all, but it is still reading tea leaves: "a permission dialog
// is open" is something the agent knows for certain and rookery can only guess.
// An installed integration turns those guesses into facts. Same split Herdr
// draws between screen manifests and lifecycle authority.
package integration

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// marker identifies the hooks rookery installed. Every command we write ends
// with it as a shell comment, which is how uninstall finds its own entries and
// leaves everyone else's alone.
//
// An explicit sentinel rather than matching on the command itself: the binary's
// path is whatever the user installed it as, so keying off "rook report" failed
// to recognise our own hooks the moment the binary was called anything else —
// which quietly turned uninstall into a no-op.
const marker = "# rookery-integration"

// Spec describes one agent's integration.
type Spec struct {
	ID          string
	Name        string
	Description string
	// SettingsPath returns the file to edit, given a home directory.
	SettingsPath func(home string) string
	// Hooks maps a hook event to the status it reports.
	Hooks []Hook
}

// Hook is one event-to-status mapping.
type Hook struct {
	Event  string
	Status string
	Why    string
}

// claudeSpec maps Claude Code's hook events onto rookery's three states.
//
// The mapping is the whole value of the integration: PermissionRequest is
// exactly "blocked", and Stop is exactly "the turn ended" — neither has to be
// inferred from a spinner or a dialog's wording, and neither can be fooled by
// an agent that renders something unexpected.
var claudeSpec = Spec{
	ID:          "claude",
	Name:        "Claude Code",
	Description: "authoritative status from hooks, plus the session id for a future resume",
	SettingsPath: func(home string) string {
		return filepath.Join(home, ".claude", "settings.json")
	},
	Hooks: []Hook{
		{Event: "UserPromptSubmit", Status: "working", Why: "a turn started"},
		{Event: "Stop", Status: "idle", Why: "the turn ended"},
		{Event: "StopFailure", Status: "idle", Why: "the turn ended badly, but it ended"},
		{Event: "PermissionRequest", Status: "blocked", Why: "it is waiting on you to allow something"},
		{Event: "Elicitation", Status: "blocked", Why: "an MCP server is asking for input"},
		{Event: "Notification", Status: "blocked", Why: "Claude Code raised a notification"},
		{Event: "SessionStart", Status: "idle", Why: "it is up and waiting"},
	},
}

// Specs are the agents rookery can integrate with, by id.
var Specs = map[string]Spec{
	claudeSpec.ID: claudeSpec,
}

// IDs lists the known integrations.
func IDs() []string {
	out := make([]string, 0, len(Specs))
	for id := range Specs {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// Status reports whether an integration is installed.
type Status struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Settings  string `json:"settings"`
	Installed bool   `json:"installed"`
	Hooks     int    `json:"hooks"`
	Note      string `json:"note,omitempty"`
}

// Install adds rookery's hooks to an agent's settings file.
//
// The file belongs to the user and usually already has hooks in it, so this
// merges rather than writes: existing events are appended to, our own entries
// are replaced rather than duplicated on a second run, and everything else is
// left byte-for-byte alone.
func Install(id, home, rookBin string) (Status, error) {
	spec, ok := Specs[id]
	if !ok {
		return Status{}, fmt.Errorf("unknown integration %q (have: %s)", id, strings.Join(IDs(), ", "))
	}
	path := spec.SettingsPath(home)

	settings, err := readSettings(path)
	if err != nil {
		return Status{}, err
	}

	hooks := mapOf(settings, "hooks")
	for _, h := range spec.Hooks {
		entries := arrayOf(hooks, h.Event)
		entries = dropOurs(entries)
		entries = append(entries, hookEntry(rookBin, h))
		hooks[h.Event] = entries
	}
	settings["hooks"] = hooks

	if err := writeSettings(path, settings); err != nil {
		return Status{}, err
	}
	return StatusOf(id, home)
}

// Uninstall removes only the entries rookery added.
func Uninstall(id, home string) (Status, error) {
	spec, ok := Specs[id]
	if !ok {
		return Status{}, fmt.Errorf("unknown integration %q", id)
	}
	path := spec.SettingsPath(home)

	settings, err := readSettings(path)
	if err != nil {
		return Status{}, err
	}
	hooks := mapOf(settings, "hooks")
	for _, h := range spec.Hooks {
		entries := dropOurs(arrayOf(hooks, h.Event))
		if len(entries) == 0 {
			// Leave no empty arrays behind in someone else's config.
			delete(hooks, h.Event)
			continue
		}
		hooks[h.Event] = entries
	}
	if len(hooks) == 0 {
		delete(settings, "hooks")
	} else {
		settings["hooks"] = hooks
	}

	if err := writeSettings(path, settings); err != nil {
		return Status{}, err
	}
	return StatusOf(id, home)
}

// StatusOf inspects what is currently installed.
func StatusOf(id, home string) (Status, error) {
	spec, ok := Specs[id]
	if !ok {
		return Status{}, fmt.Errorf("unknown integration %q", id)
	}
	path := spec.SettingsPath(home)
	st := Status{ID: spec.ID, Name: spec.Name, Settings: path}

	settings, err := readSettings(path)
	if err != nil {
		st.Note = err.Error()
		return st, nil
	}
	hooks := mapOf(settings, "hooks")
	for _, h := range spec.Hooks {
		for _, entry := range arrayOf(hooks, h.Event) {
			if isOurs(entry) {
				st.Hooks++
			}
		}
	}
	st.Installed = st.Hooks == len(spec.Hooks)
	if st.Hooks > 0 && !st.Installed {
		st.Note = fmt.Sprintf("partially installed (%d of %d hooks) — run install again",
			st.Hooks, len(spec.Hooks))
	}
	return st, nil
}

// hookEntry builds one settings.json hook group.
func hookEntry(rookBin string, h Hook) map[string]any {
	// The report is best-effort by design: a hook that fails must never break
	// the agent, so failure is swallowed here rather than surfacing as a hook
	// error in the middle of someone's turn.
	cmd := fmt.Sprintf("%s report --status %s --quiet || true %s", rookBin, h.Status, marker)
	return map[string]any{
		"hooks": []any{
			map[string]any{
				"type":    "command",
				"command": cmd,
				"async":   true,
				"timeout": 5,
			},
		},
	}
}

func isOurs(entry any) bool {
	m, ok := entry.(map[string]any)
	if !ok {
		return false
	}
	inner, ok := m["hooks"].([]any)
	if !ok {
		return false
	}
	for _, h := range inner {
		hm, ok := h.(map[string]any)
		if !ok {
			continue
		}
		if cmd, ok := hm["command"].(string); ok && strings.Contains(cmd, marker) {
			return true
		}
	}
	return false
}

func dropOurs(entries []any) []any {
	out := make([]any, 0, len(entries))
	for _, e := range entries {
		if !isOurs(e) {
			out = append(out, e)
		}
	}
	return out
}

func readSettings(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(string(data)) == "" {
		return map[string]any{}, nil
	}
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		// Refusing beats rewriting: this is the user's live agent config, and
		// replacing a file we cannot parse would throw away whatever is in it.
		return nil, fmt.Errorf("%s is not valid JSON (%w); fix or move it first", path, err)
	}
	return settings, nil
}

// writeSettings saves via a temporary file in the same directory, so an
// interrupted write cannot leave the user with a truncated agent config.
func writeSettings(path string, settings map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(filepath.Dir(path), ".rook-settings-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func mapOf(parent map[string]any, key string) map[string]any {
	if existing, ok := parent[key].(map[string]any); ok {
		return existing
	}
	return map[string]any{}
}

func arrayOf(parent map[string]any, key string) []any {
	if existing, ok := parent[key].([]any); ok {
		return existing
	}
	return nil
}
