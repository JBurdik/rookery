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
	_ "embed"
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
//
// The settings *path* is deliberately not part of a Spec: an agent can have
// several live configurations — a home directory, a relocated config dir, a
// per-project one — and which to edit is the caller's decision, not ours.
type Spec struct {
	ID          string
	Name        string
	Description string
	// ConfigEnv is the environment variable that relocates this agent's config
	// directory, if it has one.
	ConfigEnv string
	// ConfigDirName is the directory the agent keeps settings and skills in,
	// relative to a home or project directory.
	ConfigDirName string
	// SettingsFile is the file inside that directory.
	SettingsFile string
	// Hooks maps a hook event to the status it reports.
	Hooks []Hook
}

// Hook is one event-to-status mapping.
type Hook struct {
	Event  string
	Status string
	Why    string
	// SessionRefKey, when set, makes this hook report the agent's session id
	// instead of a status: the event's JSON payload (piped to the hook on
	// stdin) is read for this field. Used where an agent's hooks give session
	// identity but not full lifecycle coverage, so status stays with screen
	// detection instead of being clobbered by a partial report.
	SessionRefKey string
}

// claudeSpec maps Claude Code's hook events onto rookery's three states.
//
// The mapping is the whole value of the integration: PermissionRequest is
// exactly "blocked", and Stop is exactly "the turn ended" — neither has to be
// inferred from a spinner or a dialog's wording, and neither can be fooled by
// an agent that renders something unexpected.
var claudeSpec = Spec{
	ID:            "claude",
	Name:          "Claude Code",
	Description:   "authoritative status from hooks, plus the session id for a future resume",
	ConfigEnv:     "CLAUDE_CONFIG_DIR",
	ConfigDirName: ".claude",
	SettingsFile:  "settings.json",
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

// codexSpec covers Codex CLI. Its hooks don't cover every lifecycle
// transition (a permission cancellation or user interrupt doesn't reliably
// fire one), so unlike Claude this integration reports session identity
// only — status stays with screen detection, same split Herdr draws for
// Codex.
var codexSpec = Spec{
	ID:            "codex",
	Name:          "Codex CLI",
	Description:   "the session id at start, for a future resume; status still comes from the screen",
	ConfigEnv:     "CODEX_HOME",
	ConfigDirName: ".codex",
	SettingsFile:  "hooks.json",
	Hooks: []Hook{
		{Event: "SessionStart", SessionRefKey: "session_id", Why: "Codex's own session id becomes available"},
	},
}

// opencodeSpec covers OpenCode. Unlike Codex, OpenCode's plugin events cover
// every lifecycle transition, so this one is authoritative status same as
// Claude's hooks — it just arrives through a JS plugin file instead of a
// settings.json entry, so it gets its own install/uninstall/status path
// rather than the generic hooks-merge one.
var opencodeSpec = Spec{
	ID:            "opencode",
	Name:          "OpenCode",
	Description:   "authoritative status and session id from a plugin",
	ConfigDirName: filepath.Join(".config", "opencode"),
	SettingsFile:  filepath.Join("plugin", "rook-agent-state.js"),
}

// piSpec is a global TypeScript extension. Pi intentionally exposes its
// lifecycle through the extension API rather than a hooks JSON file; this
// gives Rook authoritative working/idle events and a session reference without
// scraping Pi's terminal UI.
var piSpec = Spec{
	ID:            "pi",
	Name:          "Pi",
	Description:   "authoritative working/idle status and session reference from a Pi extension",
	ConfigEnv:     "PI_CODING_AGENT_DIR",
	ConfigDirName: filepath.Join(".pi", "agent"),
	SettingsFile:  filepath.Join("extensions", "rook-agent-state.ts"),
}

// Specs are the agents rookery can integrate with, by id.
var Specs = map[string]Spec{
	claudeSpec.ID:   claudeSpec,
	codexSpec.ID:    codexSpec,
	opencodeSpec.ID: opencodeSpec,
	piSpec.ID:       piSpec,
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

// ConfigDirs lists every place this agent's configuration could live, most
// likely first.
//
// Agents genuinely do have several at once — a relocated config directory for
// work, another for personal, plus whatever is in the repo — so "install the
// integration" has to be able to name which one. Guessing wrong is worse than
// asking: it writes hooks into a config that is never loaded, and reports
// success.
func (s Spec) ConfigDirs(home, project string) []string {
	var dirs []string
	seen := map[string]bool{}
	add := func(dir string) {
		if dir == "" || seen[dir] {
			return
		}
		seen[dir] = true
		dirs = append(dirs, dir)
	}

	if s.ConfigEnv != "" {
		add(os.Getenv(s.ConfigEnv))
	}
	if home != "" {
		add(filepath.Join(home, s.ConfigDirName))
	}
	if project != "" {
		add(filepath.Join(project, s.ConfigDirName))
	}
	return dirs
}

// SettingsIn returns the settings file inside a config directory.
func (s Spec) SettingsIn(dir string) string {
	return filepath.Join(dir, s.SettingsFile)
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
func Install(id, path, rookBin string) (Status, error) {
	spec, ok := Specs[id]
	if !ok {
		return Status{}, fmt.Errorf("unknown integration %q (have: %s)", id, strings.Join(IDs(), ", "))
	}
	if id == "opencode" {
		return opencodeInstall(path, rookBin)
	}
	if id == "pi" {
		return piInstall(path, rookBin)
	}

	settings, err := readSettings(path)
	if err != nil {
		return Status{}, err
	}

	hooks := mapOf(settings, "hooks")
	for _, h := range spec.Hooks {
		entries := arrayOf(hooks, h.Event)
		entries = dropOurs(entries)
		entries = append(entries, hookEntry(rookBin, spec.ID, h))
		hooks[h.Event] = entries
	}
	settings["hooks"] = hooks

	if err := writeSettings(path, settings); err != nil {
		return Status{}, err
	}

	if id == "codex" {
		// hooks.json lists the hooks; config.toml is what turns them on.
		if err := ensureCodexHooksFeature(filepath.Dir(path)); err != nil {
			return Status{}, err
		}
	}
	return StatusOf(id, path)
}

// Uninstall removes only the entries rookery added.
func Uninstall(id, path string) (Status, error) {
	spec, ok := Specs[id]
	if !ok {
		return Status{}, fmt.Errorf("unknown integration %q", id)
	}
	if id == "opencode" {
		return opencodeUninstall(path)
	}
	if id == "pi" {
		return piUninstall(path)
	}

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
	return StatusOf(id, path)
}

// StatusOf inspects what is installed in one settings file.
func StatusOf(id, path string) (Status, error) {
	spec, ok := Specs[id]
	if !ok {
		return Status{}, fmt.Errorf("unknown integration %q", id)
	}
	if id == "opencode" {
		return opencodeStatus(path)
	}
	if id == "pi" {
		return piStatus(path)
	}
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

// hookEntry builds one settings.json (or hooks.json) hook group.
func hookEntry(rookBin, agentID string, h Hook) map[string]any {
	// The report is best-effort by design: a hook that fails must never break
	// the agent, so failure is swallowed here rather than surfacing as a hook
	// error in the middle of someone's turn.
	var cmd string
	if h.SessionRefKey != "" {
		cmd = fmt.Sprintf("%s report --agent %s --session-ref-stdin %s --quiet || true %s",
			rookBin, agentID, h.SessionRefKey, marker)
	} else {
		cmd = fmt.Sprintf("%s report --agent %s --status %s --quiet || true %s",
			rookBin, agentID, h.Status, marker)
	}
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

// ensureCodexHooksFeature turns on Codex's hooks feature flag, without which
// hooks.json is inert. Line-based rather than a full TOML round-trip: this is
// the user's own config.toml, and reformatting whatever else they wrote by
// hand is a worse outcome than a slightly naive edit.
func ensureCodexHooksFeature(dir string) error {
	path := filepath.Join(dir, "config.toml")
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	updated, changed := setTOMLFeatureHooks(string(data))
	if !changed {
		return nil
	}
	return os.WriteFile(path, []byte(updated), 0o644)
}

func setTOMLFeatureHooks(content string) (string, bool) {
	lines := strings.Split(content, "\n")
	inFeatures, featuresLine, hooksLine := false, -1, -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			inFeatures = trimmed == "[features]"
			if inFeatures && featuresLine == -1 {
				featuresLine = i
			}
			continue
		}
		if inFeatures && strings.HasPrefix(trimmed, "hooks") && strings.Contains(trimmed, "=") {
			hooksLine = i
		}
	}

	switch {
	case hooksLine != -1:
		if strings.TrimSpace(lines[hooksLine]) == "hooks = true" {
			return content, false
		}
		lines[hooksLine] = "hooks = true"
	case featuresLine != -1:
		out := append([]string{}, lines[:featuresLine+1]...)
		out = append(out, "hooks = true")
		lines = append(out, lines[featuresLine+1:]...)
	default:
		if strings.TrimSpace(content) != "" {
			lines = append(lines, "")
		}
		lines = append(lines, "[features]", "hooks = true")
	}

	result := strings.Join(lines, "\n")
	if !strings.HasSuffix(result, "\n") {
		result += "\n"
	}
	return result, true
}

// opencodeMarker identifies the plugin file rookery installed, the same way
// marker identifies our entries inside a JSON hooks file.
const opencodeMarker = "// rookery-integration"

//go:embed assets/opencode-agent-state.js
var opencodePluginAsset string

const piMarker = "// rookery-pi-integration"

//go:embed assets/pi-rook-agent-state.ts
var piExtensionAsset string

// opencodeInstall drops in the plugin file wholesale: unlike the JSON-hooks
// agents there is no existing file to merge with, so reinstalling simply
// overwrites it.
func opencodeInstall(path, rookBin string) (Status, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Status{}, err
	}
	content := strings.Replace(opencodePluginAsset, "__ROOK_BIN__", rookBin, 1)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return Status{}, err
	}
	return opencodeStatus(path)
}

// opencodeUninstall removes the plugin file only if it is ours, so a plugin
// the user wrote (or renamed) by hand at the same path is left alone.
func opencodeUninstall(path string) (Status, error) {
	if data, err := os.ReadFile(path); err == nil && strings.Contains(string(data), opencodeMarker) {
		if err := os.Remove(path); err != nil {
			return Status{}, err
		}
	}
	return opencodeStatus(path)
}

func opencodeStatus(path string) (Status, error) {
	spec := Specs["opencode"]
	st := Status{ID: spec.ID, Name: spec.Name, Settings: path}
	data, err := os.ReadFile(path)
	if err != nil {
		return st, nil
	}
	if strings.Contains(string(data), opencodeMarker) {
		st.Installed = true
		st.Hooks = 1
	}
	return st, nil
}

// piInstall places one auto-discovered global extension in Pi's agent
// directory. Pi discovers ~/.pi/agent/extensions/*.ts automatically, so this
// does not rewrite the user's settings.json or extension list.
func piInstall(path, rookBin string) (Status, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Status{}, err
	}
	content := strings.Replace(piExtensionAsset, "__ROOK_BIN__", rookBin, 1)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return Status{}, err
	}
	return piStatus(path)
}

func piUninstall(path string) (Status, error) {
	if data, err := os.ReadFile(path); err == nil && strings.Contains(string(data), piMarker) {
		if err := os.Remove(path); err != nil {
			return Status{}, err
		}
	}
	return piStatus(path)
}

func piStatus(path string) (Status, error) {
	spec := Specs["pi"]
	st := Status{ID: spec.ID, Name: spec.Name, Settings: path}
	data, err := os.ReadFile(path)
	if err != nil {
		return st, nil
	}
	if strings.Contains(string(data), piMarker) {
		st.Installed = true
		st.Hooks = 1
	}
	return st, nil
}
