package termgrid

import (
	"strings"
	"testing"
)

// TestRenderANSIFrameInvariants pins the two properties a Bubble Tea View
// requires of an embedded frame. Breaking either one corrupts every frame
// after the first, which is the bug this test exists to catch.
func TestRenderANSIFrameInvariants(t *testing.T) {
	g := New(20, 5)
	g.Write([]byte("hello\r\n\x1b[31mred\x1b[0m\r\nthird line\r\n"))

	out := g.RenderANSI()

	if strings.Contains(out, "\r") {
		t.Errorf("frame contains a carriage return; Bubble Tea adds its own: %q", out)
	}
	// Absolute cursor positioning: ESC [ <digits> ; <digits> H (or ESC [ H).
	for _, bad := range []string{"H"} {
		for i := 0; i+1 < len(out); i++ {
			if out[i] != 0x1b || out[i+1] != '[' {
				continue
			}
			j := i + 2
			for j < len(out) && (out[j] < 0x40 || out[j] > 0x7e) {
				j++
			}
			if j < len(out) && string(out[j]) == bad {
				t.Errorf("frame contains absolute cursor positioning at offset %d: %q", i, out[i:j+1])
			}
		}
	}

	if got, want := strings.Count(out, "\n")+1, 5; got != want {
		t.Errorf("frame has %d lines, want %d (must match grid rows exactly)", got, want)
	}

	if !strings.Contains(out, "hello") || !strings.Contains(out, "third line") {
		t.Errorf("frame lost pane content: %q", out)
	}
}

func TestScrollbackTranscript(t *testing.T) {
	g := New(20, 3)
	g.Write([]byte("one\r\ntwo\r\n\x1b[32mthree\x1b[0m\r\n"))

	text, truncated := g.Scrollback(0, false)
	if strings.Contains(text, "\x1b") {
		t.Errorf("scrollback must be ANSI-stripped, got %q", text)
	}
	if want := "one\ntwo\nthree"; !strings.HasPrefix(text, want) {
		t.Errorf("scrollback = %q, want prefix %q", text, want)
	}
	if truncated {
		t.Error("scrollback reported truncated with no line limit")
	}

	if text, truncated := g.Scrollback(1, false); !truncated || strings.Contains(text, "one") {
		t.Errorf("Scrollback(1) = %q truncated=%v, want only the last line", text, truncated)
	}
}
