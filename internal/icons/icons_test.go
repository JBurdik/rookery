package icons

import (
	"testing"
	"time"
	"unicode/utf8"
)

// TestGlyphsAreSingleCell guards the layout: the sidebar pads rows by counting
// runes, so a two-rune "glyph" would push every line after it out of
// alignment.
func TestGlyphsAreSingleCell(t *testing.T) {
	for name, set := range themes {
		if name == ThemeASCII {
			continue // the ASCII fallback is allowed a two-char zoom marker
		}
		for label, glyph := range map[string]string{
			"blocked": set.Blocked, "done": set.Done, "idle": set.Idle,
			"unknown": set.Unknown, "exited": set.Exited, "unread": set.Unread,
			"workspace": set.Workspace, "branch": set.Branch, "agent": set.Agent,
			"terminal": set.Terminal, "zoom": set.Zoom, "tab": set.Tab,
		} {
			if glyph == "" {
				t.Errorf("theme %q: %s glyph is empty", name, label)
				continue
			}
			if n := utf8.RuneCountInString(glyph); n != 1 {
				t.Errorf("theme %q: %s glyph %q is %d runes, want 1", name, label, glyph, n)
			}
		}
	}
	for name, frames := range spinners {
		for _, f := range frames {
			if n := utf8.RuneCountInString(f); n != 1 {
				t.Errorf("spinner %q frame %q is %d runes, want 1", name, f, n)
			}
		}
	}
}

// TestFrameAdvancesWithTheClock covers the property the whole design rests on:
// the frame is a pure function of the wall clock, which is how the daemon's
// pane headers and the client's sidebar animate in step without exchanging
// any message about it.
func TestFrameAdvancesWithTheClock(t *testing.T) {
	frames := SpinnerFor("dots")
	base := time.UnixMilli(0)

	if got, want := Frame(frames, base), frames[0]; got != want {
		t.Errorf("Frame at t=0 = %q, want %q", got, want)
	}
	if got, want := Frame(frames, base.Add(frameInterval)), frames[1]; got != want {
		t.Errorf("Frame after one interval = %q, want %q", got, want)
	}
	// Same instant, two independent callers — the daemon and the client —
	// must land on the same frame.
	at := time.UnixMilli(1_700_000_123_456)
	daemonFrame := Frame(frames, at)
	clientFrame := Frame(SpinnerFor("dots"), at)
	if daemonFrame != clientFrame {
		t.Errorf("daemon drew %q and client drew %q for the same instant", daemonFrame, clientFrame)
	}
	// It wraps rather than running off the end.
	for i := range 40 {
		if Frame(frames, base.Add(time.Duration(i)*frameInterval)) == "" {
			t.Fatalf("empty frame at step %d", i)
		}
	}
	if Frame(nil, base) != "" {
		t.Error("Frame on an empty set should be empty, not panic")
	}
}

func TestStatus(t *testing.T) {
	set := For(ThemeUnicode)
	frames := []string{"X"}
	now := time.UnixMilli(0)

	tests := []struct{ status, want string }{
		{"working", "X"}, // the spinner frame, not a static glyph
		{"blocked", set.Blocked},
		{"done", set.Done},
		{"idle", set.Idle},
		{"nonsense", set.Unknown},
	}
	for _, tt := range tests {
		if got := set.Status(tt.status, false, frames, now); got != tt.want {
			t.Errorf("Status(%q) = %q, want %q", tt.status, got, tt.want)
		}
	}
	if got := set.Status("working", true, frames, now); got != set.Exited {
		t.Errorf("an exited pane = %q, want %q", got, set.Exited)
	}
}

// TestForFallsBackToUnicode: an unknown theme name must land on the set that
// renders in any font, not one that needs a Nerd Font installed.
func TestForFallsBackToUnicode(t *testing.T) {
	if For("nonexistent").Done != themes[ThemeUnicode].Done {
		t.Error("unknown theme should fall back to the Unicode set")
	}
	if For("").Done != themes[ThemeUnicode].Done {
		t.Error("empty theme name should fall back to the Unicode set")
	}
}
