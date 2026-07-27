package termgrid

import (
	"os"
	"strings"
	"testing"
)

// faintCells counts cells carrying the faint attribute, which is what the
// string assertions below are really about — an escape-substring check passes
// for the wrong reasons the moment a colour is involved.
func faintCells(g *Grid) int {
	snap := g.Snapshot(false)
	n := 0
	for y := range snap.H {
		for x := range snap.W {
			if snap.At(x, y).Mode&attrBlink != 0 {
				n++
			}
		}
	}
	return n
}

func TestRewriteSGRParams(t *testing.T) {
	cases := []struct{ in, want, why string }{
		{"2", "5", "faint on its own"},
		{"0;2;37", "0;5;37", "faint among other parameters"},
		{"22", "22;25", "normal intensity has to clear faint as well as bold"},
		{"1", "1", "bold is untouched"},
		{"", "", "a bare reset has no parameters"},
		// The 2 in these is a colour, and rewriting it would turn a green
		// foreground into faint text and lose the colour entirely.
		{"38;5;2", "38;5;2", "256-colour index 2"},
		{"38;2;10;20;2", "38;2;10;20;2", "truecolor blue channel 2"},
		{"48;5;2", "48;5;2", "a background colour index"},
		{"38;5;2;2", "38;5;2;5", "faint after an extended colour"},
	}
	for _, c := range cases {
		if got := string(rewriteSGRParams([]byte(c.in))); got != c.want {
			t.Errorf("%s: rewriteSGRParams(%q) = %q, want %q", c.why, c.in, got, c.want)
		}
	}
}

// TestFaintSurvivesTheEmulator is the property that matters: dim text written
// into a pane comes back out dim. Claude Code draws its input placeholder this
// way, and without the rewrite it rendered like text the user had typed.
func TestFaintSurvivesTheEmulator(t *testing.T) {
	g := New(20, 2)
	g.Write([]byte("\x1b[2mhint\x1b[0m"))

	if n := faintCells(g); n != 4 {
		t.Errorf("%d of the 4 cells of \"hint\" are faint", n)
	}
	if out := g.RenderANSI(); !strings.Contains(out, "hint") {
		t.Errorf("the text itself is missing:\n%q", out)
	} else if !strings.Contains(out, "2m") {
		t.Errorf("the render does not ask the terminal for faint:\n%q", out)
	}
}

// TestFaintSplitAcrossWrites: a PTY read can end in the middle of an escape
// sequence, and rewriting half of one would corrupt it.
func TestFaintSplitAcrossWrites(t *testing.T) {
	for _, split := range []int{1, 2, 3} {
		g := New(20, 2)
		seq := "\x1b[2mhint"
		g.Write([]byte(seq[:split]))
		g.Write([]byte(seq[split:]))

		if out := g.RenderANSI(); !strings.Contains(out, "hint") {
			t.Errorf("split after %d byte(s): text lost:\n%q", split, out)
		}
		if n := faintCells(g); n != 4 {
			t.Errorf("split after %d byte(s): %d of 4 cells are faint", split, n)
		}
	}
}

// TestUnterminatedEscapeDoesNotStall: an escape that never completes must not
// hold the text behind it hostage.
func TestUnterminatedEscapeDoesNotStall(t *testing.T) {
	g := New(200, 2)
	g.Write([]byte("\x1b[" + strings.Repeat("9", maxEscTail+10)))
	g.Write([]byte("after"))
	// The "a" is eaten as the runaway sequence's final byte — a real terminal
	// does the same thing — but everything behind it has to come through.
	if out := g.RenderANSI(); !strings.Contains(out, "fter") {
		t.Errorf("bytes behind an unterminated escape never arrived:\n%q", out)
	}
}

// TestNonSGRSequencesAreUntouched: only SGR is rewritten, and a cursor move
// with a 2 in it must survive intact.
func TestNonSGRSequencesAreUntouched(t *testing.T) {
	g := New(20, 4)
	got := string(g.rewriteFaint([]byte("\x1b[2A\x1b[2J\x1b[2;3Hx\x1b]0;title\x07")))
	want := "\x1b[2A\x1b[2J\x1b[2;3Hx\x1b]0;title\x07"
	if got != want {
		t.Errorf("rewriteFaint touched a non-SGR sequence:\ngot  %q\nwant %q", got, want)
	}
}

// TestFaintSurvivesCompositing: with more than one pane on screen the daemon
// blits each grid onto a shared canvas, so that is the path the frame the user
// actually sees goes through.
func TestFaintSurvivesCompositing(t *testing.T) {
	g := New(20, 1)
	g.Write([]byte("\x1b[2mhint\x1b[0m"))

	canvas := NewCanvas(40, 2)
	canvas.Blit(g.Snapshot(false), 0, 0)

	out := canvas.RenderANSI()
	if !strings.Contains(out, "2m") || !strings.Contains(out, "hint") {
		t.Errorf("faint was lost in compositing:\n%q", out)
	}
}

// TestRealClaudeCapture replays what Claude Code actually wrote to a PTY —
// captured from v2.1.220, which asks for faint 17 times and clears it with
// SGR 22 twenty-one times.
//
// It is replayed at three chunk sizes because that is what caught the second
// bug here: vt10x keeps no state for a multi-byte character split across two
// writes, so a byte-at-a-time replay used to lose 7 of the 32 dim cells and
// mangle the welcome box. A PTY read boundary lands mid-rune for real.
func TestRealClaudeCapture(t *testing.T) {
	data, err := os.ReadFile("testdata/claude-welcome.raw")
	if err != nil {
		t.Fatal(err)
	}

	replay := func(chunk int) *Grid {
		g := New(100, 30)
		if chunk <= 0 {
			g.Write(data)
			return g
		}
		for i := 0; i < len(data); i += chunk {
			g.Write(data[i:min(i+chunk, len(data))])
		}
		return g
	}

	whole := replay(0)
	if n := faintCells(whole); n == 0 {
		t.Fatal("no faint cells at all in a capture that asks for faint 17 times")
	}
	want := whole.RenderANSI()
	for _, chunk := range []int{1, 3, 7, 64, 512} {
		g := replay(chunk)
		if faintCells(g) != faintCells(whole) {
			t.Errorf("%d-byte chunks: %d faint cells, want %d",
				chunk, faintCells(g), faintCells(whole))
		}
		if g.RenderANSI() != want {
			t.Errorf("%d-byte chunks render differently from one write", chunk)
		}
	}
}
