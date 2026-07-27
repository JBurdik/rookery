// Package agentstatus works out what a coding agent running inside a pane is
// currently doing, from the only two things a multiplexer can actually see:
// the terminal title the agent sets, and the text at the bottom of its
// screen.
//
// The approach is Herdr's: per-agent manifests of prioritised match rules,
// loaded from data files rather than compiled in, so adding an agent or fixing
// a marker is an edit to ~/.rook/agents/*.json rather than a rebuild. See
// manifest.go for the format; the built-in set is embedded so this works with
// nothing on disk.
package agentstatus

import (
	"strings"
	"unicode/utf8"
)

// State is what an agent is doing. Idle and Done are the same underlying
// state — waiting at its prompt — split by whether anyone has looked at the
// result yet; the daemon, not this package, decides that (see Pane.Seen).
type State string

const (
	Unknown State = "unknown"
	Idle    State = "idle"
	Working State = "working"
	Blocked State = "blocked"
	Done    State = "done"
)

// Input is everything the detector gets to look at.
type Input struct {
	Title  string   // OSC 0/2 terminal title the pane's program last set
	Bottom []string // last few non-empty screen lines, oldest first
}

// CleanTitle turns an agent's terminal title into something worth showing in
// a sidebar: the animation frame stripped off the front, whitespace tidied.
//
// Claude Code cycles "⠐ Count from 1 to 30" while working and settles on
// "✳ Claude Code"; the spinner is state (already shown as a status glyph),
// the rest is the task. Returns "" for a title that is only a spinner, so
// callers can fall back to the program name.
func CleanTitle(title string) string {
	t := strings.TrimSpace(title)
	for {
		r, size := utf8.DecodeRuneInString(t)
		if size == 0 || !isSpinnerRune(r) {
			break
		}
		t = strings.TrimSpace(t[size:])
	}
	// Collapse the runs of whitespace an animated title tends to leave.
	t = strings.Join(strings.Fields(t), " ")

	// Reject titles that aren't the agent talking. Until an agent's UI comes
	// up, the shell that launched it still owns the title and sets it to the
	// command line or the working directory — showing "/Users/me/.local/bin/
	// claude" as a pane name is worse than showing nothing and falling back
	// to the program name.
	if strings.ContainsAny(t, "/\\") || strings.HasPrefix(t, "-") {
		return ""
	}
	return t
}

func isSpinnerRune(r rune) bool {
	switch {
	case r >= 0x2800 && r <= 0x28FF: // braille
		return true
	case r >= 0x25A0 && r <= 0x25FF: // geometric shapes: ● ○ ◐ ▪ …
		return true
	case r >= 0x2591 && r <= 0x2593: // shade blocks
		return true
	case r >= 0x1F311 && r <= 0x1F320: // moon phases
		return true
	}
	switch r {
	case '✳', '✻', '✽', '✢', '·', '*', '✶', '✷', '✸', '✹', '✺', '⋆', '◜', '◝', '◞', '◟':
		return true
	}
	return false
}
