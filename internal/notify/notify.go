// Package notify plays a short sound when an agent wants attention.
//
// No audio library and no embedded assets: it shells out to whatever player
// the platform already has, the way Herdr does. A multiplexer that grew an
// audio decoding dependency to make a ping would be a bad trade.
package notify

import (
	"os"
	"os/exec"
	"runtime"
	"sync"
	"time"
)

// Kind is what happened.
type Kind string

const (
	// Done: an agent finished a turn and nobody has looked at the result.
	Done Kind = "done"
	// Blocked: an agent is waiting on input or a confirmation.
	Blocked Kind = "blocked"
)

// Modes, as written in config.json.
const (
	ModeOff    = "off"
	ModeSystem = "system" // a system sound file, via the platform player
	ModeBell   = "bell"   // the terminal bell, rung by the attached client
)

// Config is the sound settings, mirroring ~/.rook/config.json.
type Config struct {
	Mode        string `json:"mode"`
	DonePath    string `json:"done_path,omitempty"`
	BlockedPath string `json:"blocked_path,omitempty"`
	// MinInterval throttles a noisy session: with several agents finishing
	// at once, one ping is informative and six is a fire alarm.
	MinIntervalMS int `json:"min_interval_ms,omitempty"`
}

func DefaultConfig() Config {
	return Config{Mode: ModeSystem, MinIntervalMS: 1500}
}

// Player rate-limits and plays notification sounds.
type Player struct {
	cfg Config

	mu   sync.Mutex
	last time.Time
}

func New(cfg Config) *Player {
	if cfg.MinIntervalMS == 0 {
		cfg.MinIntervalMS = DefaultConfig().MinIntervalMS
	}
	return &Player{cfg: cfg}
}

// Play makes a sound for kind, unless sound is off, the platform has no
// player, or one was played too recently. Never blocks: playback happens in
// its own goroutine, because the caller is the daemon's single event loop and
// stalling it would freeze every pane in the session.
func (p *Player) Play(kind Kind) {
	if !p.allow() {
		return
	}
	path := p.path(kind)
	if path == "" {
		return
	}
	go playFile(path)
}

// allow is the gate Play runs through: sound enabled, not disabled by the
// environment, and not too soon after the last one. Split out so the rate
// limiting can be tested without an audio device.
func (p *Player) allow() bool {
	if p == nil || p.cfg.Mode != ModeSystem {
		return false
	}
	if os.Getenv("ROOK_DISABLE_SOUND") != "" {
		return false
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.last.IsZero() && time.Since(p.last) < time.Duration(p.cfg.MinIntervalMS)*time.Millisecond {
		return false
	}
	p.last = time.Now()
	return true
}

func (p *Player) path(kind Kind) string {
	custom := p.cfg.DonePath
	if kind == Blocked {
		custom = p.cfg.BlockedPath
	}
	if custom != "" {
		if _, err := os.Stat(custom); err == nil {
			return custom
		}
	}
	for _, candidate := range defaultSounds(kind) {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

// defaultSounds lists platform sounds to try, most-wanted first.
func defaultSounds(kind Kind) []string {
	switch runtime.GOOS {
	case "darwin":
		if kind == Blocked {
			// Blocked wants you now, so it gets the more insistent one.
			return []string{"/System/Library/Sounds/Glass.aiff", "/System/Library/Sounds/Ping.aiff"}
		}
		return []string{"/System/Library/Sounds/Ping.aiff", "/System/Library/Sounds/Pop.aiff"}
	case "linux":
		base := "/usr/share/sounds/freedesktop/stereo/"
		if kind == Blocked {
			return []string{base + "dialog-warning.oga", base + "bell.oga", base + "complete.oga"}
		}
		return []string{base + "complete.oga", base + "bell.oga"}
	}
	return nil
}

// players lists the command-line audio players to try, in order.
func players() [][]string {
	switch runtime.GOOS {
	case "darwin":
		return [][]string{{"afplay"}}
	case "linux":
		return [][]string{{"paplay"}, {"pw-play"}, {"aplay", "-q"}, {"ffplay", "-nodisp", "-autoexit", "-loglevel", "quiet"}}
	}
	return nil
}

func playFile(path string) {
	for _, argv := range players() {
		bin, err := exec.LookPath(argv[0])
		if err != nil {
			continue
		}
		cmd := exec.Command(bin, append(argv[1:], path)...)
		// A sound that fails is not worth reporting: it is a nicety, and the
		// badge in the sidebar already carried the information.
		_ = cmd.Run()
		return
	}
}
