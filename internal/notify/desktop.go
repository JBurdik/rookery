package notify

import (
	"os/exec"
	"runtime"
	"strings"
)

// Desktop posts an OS notification. Used when nobody is looking at the
// terminal: a sound you might miss is not enough on its own, and a banner is
// the only thing that reaches you in another app.
//
// Fire and forget, in its own goroutine: the caller is the daemon's single
// event loop, and `osascript` takes a good fraction of a second.
func (p *Player) Desktop(title, body string) {
	if p == nil || !p.cfg.Desktop {
		return
	}
	go postDesktop(title, body)
}

func postDesktop(title, body string) {
	switch runtime.GOOS {
	case "darwin":
		// terminal-notifier is nicer when present (it can carry an icon and a
		// click action); osascript is always there.
		if bin, err := exec.LookPath("terminal-notifier"); err == nil {
			_ = exec.Command(bin, "-title", title, "-message", body, "-group", "dev.rookery").Run()
			return
		}
		script := "display notification " + appleQuote(body) + " with title " + appleQuote(title)
		_ = exec.Command("osascript", "-e", script).Run()

	case "linux":
		if bin, err := exec.LookPath("notify-send"); err == nil {
			_ = exec.Command(bin, "--app-name=rookery", title, body).Run()
		}
	}
}

// appleQuote wraps a string as an AppleScript literal.
//
// This is the one place a pane title — which is agent-controlled text, taken
// from a terminal title sequence — crosses into another language, so quoting
// is not cosmetic: without it a title containing a quote would end the string
// and the rest would be interpreted as AppleScript.
func appleQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	// Newlines would also terminate the literal.
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	return `"` + s + `"`
}
