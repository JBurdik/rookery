// Package icons holds the glyph sets the UI draws with, so switching between
// a Nerd Font, plain Unicode and pure ASCII is one config value rather than
// scattered literals.
//
// Glyph choices are not guesswork: the Nerd Font set was checked against the
// cmap of 0xProto Nerd Font Mono, which carries the Codicon, Octicon,
// Font Awesome and Material ranges used here. Notably it has **no braille**
// (U+2800–U+28FF), so the obvious braille dot spinner would render from a
// fallback font at the wrong width and jitter the sidebar every frame. The
// default spinner is quadrant blocks, which are in the font and rotate the
// same way. Braille is still available for fonts that do have it — see
// SpinnerFor.
package icons

import "time"

// Theme names, as written in config.json.
const (
	ThemeNerd    = "nerd"
	ThemeUnicode = "unicode"
	ThemeASCII   = "ascii"
)

// Set is one theme's glyphs. Every field is one display cell wide.
type Set struct {
	Working   string // replaced by the spinner frame at render time
	Blocked   string
	Done      string
	Idle      string
	Unknown   string
	Exited    string
	Unread    string
	Workspace string
	Branch    string
	Agent     string
	Terminal  string
	Zoom      string
	Tab       string
}

var themes = map[string]Set{
	ThemeNerd: {
		Blocked:   "", //  fa-warning — this one wants you
		Done:      "", //  cod-check
		Idle:      "", //  cod-primitive_dot
		Unknown:   "", //  cod-question
		Exited:    "", //  cod-circle_slash
		Unread:    "", //  oct-dot_fill
		Workspace: "", //  oct-repo
		Branch:    "", //  dev-git_branch
		Agent:     "", //  cod-robot
		Terminal:  "", //  cod-terminal
		Zoom:      "", //  cod-screen_full
		Tab:       "", //  cod-multiple_windows
	},
	ThemeUnicode: {
		Blocked:   "!",
		Done:      "✓",
		Idle:      "●",
		Unknown:   "·",
		Exited:    "○",
		Unread:    "•",
		Workspace: "▪",
		Branch:    "⑂",
		Agent:     "◆",
		Terminal:  "▸",
		Zoom:      "⛶",
		Tab:       "▭",
	},
	ThemeASCII: {
		Blocked:   "!",
		Done:      "*",
		Idle:      "o",
		Unknown:   ".",
		Exited:    "x",
		Unread:    "*",
		Workspace: "#",
		Branch:    "/",
		Agent:     "@",
		Terminal:  ">",
		Zoom:      "[]",
		Tab:       "-",
	},
}

// For returns a theme by name, falling back to Nerd Font glyphs.
func For(theme string) Set {
	if s, ok := themes[theme]; ok {
		return s
	}
	return themes[ThemeNerd]
}

// Spinner frame sets, keyed by the name used in config.json.
var spinners = map[string][]string{
	// Quadrant blocks: present in 0xProto, rotates like the braille one.
	"dots": {"▖", "▘", "▝", "▗"},
	// The classic braille spinner. Only for fonts that actually have the
	// braille block — most Nerd Fonts do not.
	"braille": {"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"},
	"bar":     {"▁", "▂", "▃", "▄", "▅", "▆", "▇", "▆", "▅", "▄", "▃", "▂"},
	"shade":   {"░", "▒", "▓", "█", "▓", "▒"},
	"line":    {"|", "/", "-", "\\"},
	"pulse":   {"·", "•", "●", "•"},
}

// SpinnerFor returns a named frame set, defaulting to the one that is safe in
// a Nerd Font.
func SpinnerFor(name string) []string {
	if s, ok := spinners[name]; ok {
		return s
	}
	return spinners["dots"]
}

// SpinnerNames lists the available spinners, for documentation and errors.
func SpinnerNames() []string {
	return []string{"dots", "braille", "bar", "shade", "line", "pulse"}
}

// frameInterval is how long one spinner frame is held.
const frameInterval = 120 * time.Millisecond

// Frame picks the current animation frame from the wall clock.
//
// Deriving it from the time rather than from a counter is what lets the
// daemon (which draws pane headers) and the client (which draws the sidebar)
// animate in step without exchanging a single message about it.
func Frame(frames []string, now time.Time) string {
	if len(frames) == 0 {
		return ""
	}
	i := now.UnixMilli() / int64(frameInterval/time.Millisecond)
	return frames[int(i%int64(len(frames)))]
}

// Status returns the glyph for an agent status, animating "working".
func (s Set) Status(status string, paneExited bool, frames []string, now time.Time) string {
	if paneExited {
		return s.Exited
	}
	switch status {
	case "working":
		return Frame(frames, now)
	case "blocked":
		return s.Blocked
	case "done":
		return s.Done
	case "idle":
		return s.Idle
	default:
		return s.Unknown
	}
}
