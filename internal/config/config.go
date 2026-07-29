// Package config loads rookery's user settings from ~/.rook. Two files,
// split by how often you touch them: config.json for behaviour, hotkeys.json
// for the keymap. Both are written out with their defaults on first run, so
// the file itself documents what can be changed.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"

	"github.com/jirkab/rookery/internal/icons"
	"github.com/jirkab/rookery/internal/notify"
)

// Dir is where rookery keeps user configuration.
func Dir() string {
	if v := os.Getenv("ROOK_CONFIG_DIR"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".rook"
	}
	return filepath.Join(home, ".rook")
}

func ConfigPath() string  { return filepath.Join(Dir(), "config.json") }
func HotkeysPath() string { return filepath.Join(Dir(), "hotkeys.json") }

// AgentsDir holds per-agent status manifests. Files here override the
// built-in ones with the same id.
func AgentsDir() string { return filepath.Join(Dir(), "agents") }

// Config is ~/.rook/config.json.
type Config struct {
	UI UI `json:"ui"`
}

type UI struct {
	// MouseCapture lets rookery handle clicks, drags and scroll. While it is
	// on, your terminal's own text selection needs shift+drag — the same
	// trade tmux and Herdr make. Toggle at runtime with the prefix + m.
	MouseCapture bool `json:"mouse_capture"`
	// SidebarVisible controls whether the sidebar starts open.
	SidebarVisible bool `json:"sidebar_visible"`
	// SidebarWidth is in columns, including the divider.
	SidebarWidth int `json:"sidebar_width"`
	// ShowTerminalsInAgents adds non-agent panes to the agents panel. Off by
	// default: that panel answers "which agent needs me", and a shell that
	// is simply sitting at a prompt is noise in that list.
	ShowTerminalsInAgents bool `json:"show_terminals_in_agents"`
	// Icons selects the glyph set: "unicode" (the default — plain symbols that
	// render in any font at a predictable width), "nerd" (a Nerd Font is
	// installed), or "ascii".
	Icons string `json:"icons"`
	// Spinner animates a working agent. "dots" is the default because it is
	// quadrant blocks, which every font has; "braille" is the familiar
	// ⠋⠙⠹ one but is absent from many Nerd Fonts, 0xProto included, so it
	// falls back to another font and can sit at the wrong width.
	// One of: dots, braille, bar, shade, line, pulse.
	Spinner string `json:"spinner"`
	// PaneBorders draws a box around each pane, with its title in the top
	// edge: "auto" (once more than one pane shares a tab), "always", "never".
	PaneBorders string `json:"pane_borders"`
	// Blink flashes a pane's border for a few seconds after its agent
	// finishes, so a result landing on the screen you are already looking at
	// still registers.
	Blink *bool `json:"blink,omitempty"`
	// ManagerCmd is the agent the manager bar talks to. It runs in its own
	// tab with rookery's CLI on PATH, so it can create panes, read them and
	// wait on them — you describe what you want, it drives the multiplexer.
	ManagerCmd string `json:"manager_cmd"`
	// Colors is the palette; anything left out keeps its default.
	Colors Colors `json:"colors"`
	// Sound pings you when an agent finishes or gets stuck.
	Sound notify.Config `json:"sound"`
}

// Hotkeys is ~/.rook/hotkeys.json: a prefix key plus action -> keys.
type Hotkeys struct {
	Prefix   string              `json:"prefix"`
	Bindings map[string][]string `json:"bindings"`
}

// Action names. These are the strings used in hotkeys.json.
const (
	ActionNewTab          = "new_tab"
	ActionNewTabNamed     = "new_tab_named"
	ActionCloseTab        = "close_tab"
	ActionNextTab         = "next_tab"
	ActionPrevTab         = "prev_tab"
	ActionRenameTab       = "rename_tab"
	ActionSplitRight      = "split_right"
	ActionSplitDown       = "split_down"
	ActionClosePane       = "close_pane"
	ActionZoom            = "zoom"
	ActionFocusLeft       = "focus_pane_left"
	ActionFocusDown       = "focus_pane_down"
	ActionFocusUp         = "focus_pane_up"
	ActionFocusRight      = "focus_pane_right"
	ActionResizeLeft      = "resize_pane_left"
	ActionResizeDown      = "resize_pane_down"
	ActionResizeUp        = "resize_pane_up"
	ActionResizeRight     = "resize_pane_right"
	ActionNewWorkspace    = "new_workspace"
	ActionNextWorkspace   = "next_workspace"
	ActionPrevWorkspace   = "prev_workspace"
	ActionCloseWorkspce   = "close_workspace"
	ActionRenameWorkspace = "rename_workspace"
	ActionRenamePane      = "rename_pane"
	ActionCopyMode        = "copy_mode"
	ActionGoto            = "goto"
	ActionToggleSidebar   = "toggle_sidebar"
	ActionFocusSidebar    = "focus_sidebar"
	ActionToggleMouse     = "toggle_mouse"
	ActionDetach          = "detach"
	ActionHelp            = "help"
	ActionGit             = "git"
	ActionManager         = "manager"
	ActionLiteralPrefix   = "literal_prefix"
)

// DefaultConfig mirrors Herdr's defaults where they apply.
func DefaultConfig() Config {
	return Config{UI: UI{
		MouseCapture:          true,
		SidebarVisible:        true,
		SidebarWidth:          22,
		ShowTerminalsInAgents: false,
		Icons:                 icons.ThemeUnicode,
		Spinner:               "dots",
		PaneBorders:           "auto",
		ManagerCmd:            "claude",
		Blink:                 boolPtr(true),
		Colors:                DefaultColors(),
		Sound:                 notify.DefaultConfig(),
	}}
}

// DefaultHotkeys is Herdr's default keymap, minus the features rookery does
// not have yet (plugins).
func DefaultHotkeys() Hotkeys {
	return Hotkeys{
		Prefix: "ctrl+b",
		Bindings: map[string][]string{
			ActionNewTab:          {"c"},
			ActionNewTabNamed:     {"C"},
			ActionCloseTab:        {"X"},
			ActionNextTab:         {"n"},
			ActionPrevTab:         {"p"},
			ActionRenameTab:       {"T"},
			ActionSplitRight:      {"v"},
			ActionSplitDown:       {"-"},
			ActionClosePane:       {"x"},
			ActionZoom:            {"z"},
			ActionFocusLeft:       {"h", "left"},
			ActionFocusDown:       {"j", "down"},
			ActionFocusUp:         {"k", "up"},
			ActionFocusRight:      {"l", "right"},
			ActionResizeLeft:      {"H", "shift+left"},
			ActionResizeDown:      {"J", "shift+down"},
			ActionResizeUp:        {"K", "shift+up"},
			ActionResizeRight:     {"L", "shift+right"},
			ActionNewWorkspace:    {"N"},
			ActionNextWorkspace:   {"w"},
			ActionPrevWorkspace:   {"W"},
			ActionCloseWorkspce:   {"D"},
			ActionRenameWorkspace: {"R"},
			ActionRenamePane:      {"r"},
			ActionCopyMode:        {"[", "esc"},
			ActionGoto:            {"f", "/"},
			ActionToggleSidebar:   {"b"},
			ActionFocusSidebar:    {"g"},
			ActionToggleMouse:     {"m"},
			ActionDetach:          {"q", "d"},
			ActionHelp:            {"?"},
			ActionGit:             {"G"},
			ActionManager:         {":", "a"},
			ActionLiteralPrefix:   {"ctrl+b"},
		},
	}
}

// Load reads both files, filling in defaults for anything missing and
// writing back any file that didn't exist yet. A malformed file is reported
// rather than silently replaced — overwriting someone's hand-edited config
// because of a stray comma would be worse than refusing to start clean.
func Load() (Config, Hotkeys, error) {
	cfg := DefaultConfig()
	keys := DefaultHotkeys()

	if err := loadOrCreate(ConfigPath(), &cfg); err != nil {
		return cfg, keys, err
	}
	if err := loadOrCreate(HotkeysPath(), &keys); err != nil {
		return cfg, keys, err
	}

	if cfg.UI.SidebarWidth < 12 {
		cfg.UI.SidebarWidth = 12
	}
	if cfg.UI.Icons == "" {
		cfg.UI.Icons = icons.ThemeUnicode
	}
	if cfg.UI.Spinner == "" {
		cfg.UI.Spinner = "dots"
	}
	if cfg.UI.Blink == nil {
		cfg.UI.Blink = boolPtr(true)
	}
	if cfg.UI.ManagerCmd == "" {
		cfg.UI.ManagerCmd = "claude"
	}
	if cfg.UI.PaneBorders == "" {
		cfg.UI.PaneBorders = "auto"
	}
	cfg.UI.Colors = cfg.UI.Colors.merge()
	if cfg.UI.Sound.Mode == "" {
		cfg.UI.Sound.Mode = notify.DefaultConfig().Mode
	}
	if cfg.UI.Sound.MinIntervalMS == 0 {
		cfg.UI.Sound.MinIntervalMS = notify.DefaultConfig().MinIntervalMS
	}
	if keys.Prefix == "" {
		keys.Prefix = "ctrl+b"
	}
	// Merge in any action the user's file predates, so an upgrade doesn't
	// leave new features unbound.
	for action, defaults := range DefaultHotkeys().Bindings {
		if _, ok := keys.Bindings[action]; !ok {
			keys.Bindings[action] = defaults
		}
	}
	return cfg, keys, nil
}

func loadOrCreate(path string, v any) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return write(path, v)
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

func write(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

// boolPtr exists because a plain bool cannot tell "the user set false" from
// "the user said nothing", and the default here is true.
func boolPtr(b bool) *bool { return &b }

// Save writes the current config back, used when a runtime toggle should
// persist.
func (c Config) Save() error { return write(ConfigPath(), c) }

// Action returns the action bound to a key, or "" if the key is unbound.
// Built once per client, so a linear map walk at startup is fine.
func (h Hotkeys) Action(key string) string {
	for action, keys := range h.Bindings {
		if slices.Contains(keys, key) {
			return action
		}
	}
	return ""
}

// KeysFor renders an action's bindings for the help overlay.
func (h Hotkeys) KeysFor(action string) []string { return h.Bindings[action] }
