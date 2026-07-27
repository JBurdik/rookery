package notify

import "testing"

// TestAppleQuote covers the one place agent-controlled text crosses into
// another language. A pane title comes from a terminal title sequence, so it
// is whatever the program in the pane decided to send — including quotes,
// backslashes and newlines, all of which would otherwise end the AppleScript
// string literal and leave the rest to be interpreted as code.
func TestAppleQuote(t *testing.T) {
	tests := []struct{ in, want string }{
		{`plain`, `"plain"`},
		{`say "hi"`, `"say \"hi\""`},
		{`back\slash`, `"back\\slash"`},
		{"two\nlines", `"two lines"`},
		{"carriage\rreturn", `"carriage return"`},
		// The shape that would actually break out: close the string, then run
		// something. Quoting must leave it inert.
		{`" & (do shell script "touch /tmp/pwned") & "`,
			`"\" & (do shell script \"touch /tmp/pwned\") & \""`},
	}
	for _, tt := range tests {
		if got := appleQuote(tt.in); got != tt.want {
			t.Errorf("appleQuote(%q) = %s, want %s", tt.in, got, tt.want)
		}
	}
}

func TestDesktopRespectsConfig(t *testing.T) {
	// Nothing to assert about the banner itself without a display; what
	// matters is that a disabled or nil player never tries.
	New(Config{Desktop: false}).Desktop("t", "b")
	var p *Player
	p.Desktop("t", "b")
}
