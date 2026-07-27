package notify

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPathPrefersCustomThenFallsBack(t *testing.T) {
	t.Setenv("ROOK_DISABLE_SOUND", "")
	dir := t.TempDir()
	custom := filepath.Join(dir, "done.aiff")
	if err := os.WriteFile(custom, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	p := New(Config{Mode: ModeSystem, DonePath: custom})
	if got := p.path(Done); got != custom {
		t.Errorf("done path = %q, want the configured %q", got, custom)
	}

	// A configured file that isn't there must not silence the ping; it falls
	// through to the platform sound.
	missing := New(Config{Mode: ModeSystem, DonePath: filepath.Join(dir, "nope.aiff")})
	if got := missing.path(Done); got == filepath.Join(dir, "nope.aiff") {
		t.Error("a missing custom sound should fall back, not be used")
	}
}

func TestThrottle(t *testing.T) {
	// The kill switch is honoured by allow(), so a developer who has it set
	// in their shell must not see this test fail.
	t.Setenv("ROOK_DISABLE_SOUND", "")

	p := New(Config{Mode: ModeSystem, MinIntervalMS: 10_000})

	// Play() is the throttled entry point; drive the gate directly so the
	// test never depends on an audio player being installed.
	if !p.allow() {
		t.Fatal("first ping should be allowed")
	}
	if p.allow() {
		t.Error("a second ping inside the interval should be suppressed")
	}

	p.last = time.Now().Add(-11 * time.Second)
	if !p.allow() {
		t.Error("a ping after the interval should be allowed again")
	}
}

func TestModeOffIsSilent(t *testing.T) {
	t.Setenv("ROOK_DISABLE_SOUND", "")

	p := New(Config{Mode: ModeOff})
	if p.allow() {
		t.Error("sound is off; nothing should be allowed through")
	}
	// The bell mode is the client's job, so the daemon-side player stays quiet.
	if New(Config{Mode: ModeBell}).allow() {
		t.Error("bell mode should not play a system sound")
	}
}

// TestDisableEnvSilencesEverything covers the kill switch itself.
func TestDisableEnvSilencesEverything(t *testing.T) {
	t.Setenv("ROOK_DISABLE_SOUND", "1")
	if New(Config{Mode: ModeSystem}).allow() {
		t.Error("ROOK_DISABLE_SOUND should suppress every ping")
	}
}

func TestNilPlayerIsSafe(t *testing.T) {
	var p *Player
	p.Play(Done) // a daemon with no player configured must not panic
}
