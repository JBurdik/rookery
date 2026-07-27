package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func settingsPath(home string) string {
	return filepath.Join(home, ".claude", "settings.json")
}

func write(t *testing.T, home, content string) {
	t.Helper()
	path := settingsPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, home string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(settingsPath(home))
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("wrote invalid JSON: %v\n%s", err, data)
	}
	return out
}

func TestInstallIntoEmptyHome(t *testing.T) {
	home := t.TempDir()
	st, err := Install("claude", settingsPath(home), "rook")
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if !st.Installed || st.Hooks != len(claudeSpec.Hooks) {
		t.Errorf("status = %+v, want all %d hooks installed", st, len(claudeSpec.Hooks))
	}

	settings := read(t, home)
	hooks, ok := settings["hooks"].(map[string]any)
	if !ok {
		t.Fatalf("no hooks written: %v", settings)
	}
	for _, h := range claudeSpec.Hooks {
		if _, ok := hooks[h.Event]; !ok {
			t.Errorf("hook %s missing", h.Event)
		}
	}
}

// TestInstallPreservesOtherHooks is the property that matters most: this edits
// a file the user already has hooks in, and losing one of theirs would be a
// far worse bug than not installing ours.
func TestInstallPreservesOtherHooks(t *testing.T) {
	home := t.TempDir()
	write(t, home, `{
	  "model": "opus",
	  "env": {"FOO": "bar"},
	  "hooks": {
	    "SessionStart": [
	      {"hooks": [{"type": "command", "command": "my-own-thing.sh"}]}
	    ],
	    "PreToolUse": [
	      {"matcher": "Bash", "hooks": [{"type": "command", "command": "guard.sh"}]}
	    ]
	  }
	}`)

	if _, err := Install("claude", settingsPath(home), "rook"); err != nil {
		t.Fatal(err)
	}
	settings := read(t, home)

	if settings["model"] != "opus" {
		t.Error("unrelated setting lost")
	}
	if env, ok := settings["env"].(map[string]any); !ok || env["FOO"] != "bar" {
		t.Error("env block lost")
	}

	hooks := settings["hooks"].(map[string]any)
	// An event we also use must keep the user's entry alongside ours.
	start := hooks["SessionStart"].([]any)
	if len(start) != 2 {
		t.Fatalf("SessionStart has %d entries, want the user's plus ours", len(start))
	}
	if !hasCommand(start, "my-own-thing.sh") {
		t.Error("the user's SessionStart hook was dropped")
	}
	// An event we do not touch must be untouched.
	if !hasCommand(hooks["PreToolUse"].([]any), "guard.sh") {
		t.Error("an unrelated hook event was damaged")
	}
}

func TestInstallIsIdempotent(t *testing.T) {
	home := t.TempDir()
	for range 3 {
		if _, err := Install("claude", settingsPath(home), "rook"); err != nil {
			t.Fatal(err)
		}
	}
	hooks := read(t, home)["hooks"].(map[string]any)
	for _, h := range claudeSpec.Hooks {
		entries := hooks[h.Event].([]any)
		ours := 0
		for _, e := range entries {
			if isOurs(e) {
				ours++
			}
		}
		if ours != 1 {
			t.Errorf("%s has %d rookery entries after three installs, want 1", h.Event, ours)
		}
	}
}

func TestUninstallRemovesOnlyOurs(t *testing.T) {
	home := t.TempDir()
	write(t, home, `{
	  "hooks": {
	    "Stop": [{"hooks": [{"type": "command", "command": "keep-me.sh"}]}]
	  }
	}`)
	if _, err := Install("claude", settingsPath(home), "rook"); err != nil {
		t.Fatal(err)
	}
	st, err := Uninstall("claude", settingsPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if st.Installed || st.Hooks != 0 {
		t.Errorf("still installed after uninstall: %+v", st)
	}

	hooks := read(t, home)["hooks"].(map[string]any)
	stop, ok := hooks["Stop"].([]any)
	if !ok || !hasCommand(stop, "keep-me.sh") {
		t.Fatal("uninstall removed the user's own Stop hook")
	}
	// Events where we were the only entry should be gone, not left as [].
	if _, present := hooks["UserPromptSubmit"]; present {
		t.Error("uninstall left an empty array behind")
	}
}

// TestMalformedSettingsIsRefused: replacing a config we cannot parse would
// throw away whatever the user had in it.
func TestMalformedSettingsIsRefused(t *testing.T) {
	home := t.TempDir()
	write(t, home, `{"hooks": {oops}}`)

	if _, err := Install("claude", settingsPath(home), "rook"); err == nil {
		t.Fatal("Install overwrote a settings file it could not parse")
	}
	data, err := os.ReadFile(settingsPath(home))
	if err != nil || string(data) != `{"hooks": {oops}}` {
		t.Errorf("the unparseable file was modified: %q", data)
	}
}

func TestStatusOnCleanHome(t *testing.T) {
	st, err := StatusOf("claude", settingsPath(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	if st.Installed || st.Hooks != 0 {
		t.Errorf("status on a clean home = %+v, want not installed", st)
	}
}

// TestConfigDirsPrefersTheRelocatedOne covers the case that made the first
// version write hooks into a config the agent never loads: an agent whose
// config directory has been moved by an environment variable.
func TestConfigDirsPrefersTheRelocatedOne(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "/somewhere/claude-work")
	dirs := claudeSpec.ConfigDirs("/home/me", "/repo")
	if len(dirs) == 0 || dirs[0] != "/somewhere/claude-work" {
		t.Fatalf("ConfigDirs = %v, want the relocated directory first", dirs)
	}
	if len(dirs) != 3 {
		t.Errorf("ConfigDirs = %v, want relocated, home and project", dirs)
	}

	t.Setenv("CLAUDE_CONFIG_DIR", "")
	dirs = claudeSpec.ConfigDirs("/home/me", "")
	if len(dirs) != 1 || dirs[0] != "/home/me/.claude" {
		t.Errorf("with no env var, ConfigDirs = %v, want just the home directory", dirs)
	}
}

func TestSettingsIn(t *testing.T) {
	if got := claudeSpec.SettingsIn("/x/.claude"); got != "/x/.claude/settings.json" {
		t.Errorf("SettingsIn = %q", got)
	}
}

// TestTwoConfigsAreIndependent: installing into one profile must not appear in
// another, or `status` would be lying about which config is wired up.
func TestTwoConfigsAreIndependent(t *testing.T) {
	root := t.TempDir()
	work := filepath.Join(root, "claude-work", "settings.json")
	personal := filepath.Join(root, "claude-personal", "settings.json")

	if _, err := Install("claude", work, "rook"); err != nil {
		t.Fatal(err)
	}
	workSt, _ := StatusOf("claude", work)
	personalSt, _ := StatusOf("claude", personal)
	if !workSt.Installed {
		t.Error("install did not take effect in the targeted config")
	}
	if personalSt.Installed || personalSt.Hooks != 0 {
		t.Error("install leaked into another config directory")
	}
}

func TestUnknownIntegration(t *testing.T) {
	if _, err := Install("nosuchagent", settingsPath(t.TempDir()), "rook"); err == nil {
		t.Error("Install accepted an unknown integration")
	}
}

func hasCommand(entries []any, want string) bool {
	for _, e := range entries {
		m, ok := e.(map[string]any)
		if !ok {
			continue
		}
		inner, ok := m["hooks"].([]any)
		if !ok {
			continue
		}
		for _, h := range inner {
			hm, ok := h.(map[string]any)
			if !ok {
				continue
			}
			if cmd, ok := hm["command"].(string); ok && cmd == want {
				return true
			}
		}
	}
	return false
}
