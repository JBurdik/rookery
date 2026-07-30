package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestLoadWritesDefaults(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ROOK_CONFIG_DIR", dir)

	cfg, keys, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.UI.MouseCapture || cfg.UI.SidebarWidth != 22 {
		t.Errorf("defaults not applied: %+v", cfg.UI)
	}
	if cfg.UI.ShowTerminalsInAgents {
		t.Error("plain terminals should stay out of the agents panel by default")
	}
	if cfg.UI.Icons != "unicode" {
		t.Errorf("icons default = %q, want unicode — it renders in any font", cfg.UI.Icons)
	}
	if cfg.UI.PaneBorders != "auto" {
		t.Errorf("pane_borders default = %q, want auto", cfg.UI.PaneBorders)
	}
	if keys.Prefix != "ctrl+b" {
		t.Errorf("prefix = %q, want ctrl+b", keys.Prefix)
	}

	for _, name := range []string{"config.json", "hotkeys.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s was not created: %v", name, err)
		}
	}
}

// TestLoadMergesNewActions covers the upgrade path: a hotkeys file written by
// an older build must not leave newly added actions unbound.
func TestLoadMergesNewActions(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ROOK_CONFIG_DIR", dir)

	old := Hotkeys{Prefix: "ctrl+a", Bindings: map[string][]string{ActionNewTab: {"t"}}}
	data, _ := json.Marshal(old)
	if err := os.WriteFile(filepath.Join(dir, "hotkeys.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	_, keys, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if keys.Prefix != "ctrl+a" {
		t.Errorf("prefix = %q, want the user's ctrl+a", keys.Prefix)
	}
	if got := keys.Bindings[ActionNewTab]; !slices.Equal(got, []string{"t"}) {
		t.Errorf("user binding overwritten: %v", got)
	}
	if len(keys.Bindings[ActionGit]) == 0 {
		t.Error("the git binding was not filled in from defaults")
	}
	if len(keys.Bindings[ActionZoom]) == 0 {
		t.Error("action missing from the user's file was not filled in from defaults")
	}
}

func TestAction(t *testing.T) {
	keys := DefaultHotkeys()
	tests := []struct{ key, want string }{
		{"c", ActionNewTab},
		{"v", ActionSplitRight},
		{"-", ActionSplitDown},
		{"h", ActionFocusLeft},
		{"left", ActionFocusLeft},
		{"q", ActionDetach},
		{"?", ActionHelp},
		{"H", ActionSwapLeft},
		{"shift+down", ActionSwapDown},
		{"r", ActionRenamePane},
		{"§", ""},
	}
	for _, tt := range tests {
		if got := keys.Action(tt.key); got != tt.want {
			t.Errorf("Action(%q) = %q, want %q", tt.key, got, tt.want)
		}
	}
}

// TestMalformedConfigIsReported: a stray comma should surface as an error,
// not silently replace a hand-edited file with defaults.
func TestMalformedConfigIsReported(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ROOK_CONFIG_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte("{oops}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(); err == nil {
		t.Error("Load accepted malformed JSON")
	}
}

func TestAgentDefaults(t *testing.T) {
	t.Setenv("ROOK_CONFIG_DIR", t.TempDir())

	cfg, _, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Agent.Command != "claude" {
		t.Errorf("agent.command = %q, want claude", cfg.Agent.Command)
	}
	if got := cfg.Agent.Argv(); !slices.Equal(got, []string{"claude"}) {
		t.Errorf("Argv() = %v, want [claude]", got)
	}
}

func TestAgentConfigured(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ROOK_CONFIG_DIR", dir)
	writeConfig(t, dir, `{"agent":{"command":"  codex  ","args":["--full-auto"]}}`)

	cfg, _, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Agent.Argv(); !slices.Equal(got, []string{"codex", "--full-auto"}) {
		t.Errorf("Argv() = %v, want [codex --full-auto]", got)
	}
}

// An older config.json has no agent section at all; it must keep working
// rather than starting a fan-out with an empty command.
func TestAgentMissingSectionFallsBack(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ROOK_CONFIG_DIR", dir)
	writeConfig(t, dir, `{"ui":{"sidebar_width":30}}`)

	cfg, _, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Agent.Command != DefaultAgentCommand {
		t.Errorf("agent.command = %q, want %q", cfg.Agent.Command, DefaultAgentCommand)
	}
	if cfg.UI.SidebarWidth != 30 {
		t.Errorf("sidebar_width = %d, want the value from the file", cfg.UI.SidebarWidth)
	}
}

// A command line in "command" would be looked up as a program of that name.
// Say what to do instead rather than failing later at exec time.
func TestAgentCommandWithFlagsRejected(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ROOK_CONFIG_DIR", dir)
	writeConfig(t, dir, `{"agent":{"command":"claude --resume"}}`)

	if _, _, err := Load(); err == nil {
		t.Fatal("Load accepted a command with flags in it")
	} else if !strings.Contains(err.Error(), "agent.args") {
		t.Errorf("error = %v, want it to point at agent.args", err)
	}
}

func writeConfig(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
