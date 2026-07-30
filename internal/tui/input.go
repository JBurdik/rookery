package tui

import (
	"bytes"
	"io"
	"strconv"
	"unicode"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
)

// enhancedKeyMsg is an enhanced keyboard event that Bubble Tea does not decode.
// data is always the original CSI sequence, suitable for forwarding to a PTY.
// key is present only when Rook can use the event for one of its own bindings.
type enhancedKeyMsg struct {
	data   []byte
	key    tea.KeyMsg
	hasKey bool
}

// inputBridge sits immediately in front of Bubble Tea's reader. It consumes
// Kitty CSI-u and xterm modifyOtherKeys events and sends them directly to the
// program, letting all other input (including bracketed paste) continue
// through Bubble Tea unchanged.
//
// Kitty control replies are not keystrokes and must never be delivered to a
// pane as text. Release events are likewise intentionally ignored: a PTY has
// no useful legacy representation for them, and forwarding them duplicates
// input in applications that only expect press/repeat events.
type inputBridge struct {
	r    io.Reader
	send func(tea.Msg)

	pending []byte
	output  []byte
}

func newInputBridge(r io.Reader, send func(tea.Msg)) *inputBridge {
	return &inputBridge{r: r, send: send}
}

func (b *inputBridge) Read(p []byte) (int, error) {
	for len(b.output) == 0 {
		buf := make([]byte, 4096)
		n, err := b.r.Read(buf)
		if n > 0 {
			b.output = append(b.output, b.process(buf[:n])...)
			if len(b.output) > 0 {
				break
			}
		}
		if err != nil {
			return 0, err
		}
	}
	n := copy(p, b.output)
	b.output = b.output[n:]
	return n, nil
}

func (b *inputBridge) process(p []byte) []byte {
	b.pending = append(b.pending, p...)
	var out []byte

	for len(b.pending) > 0 {
		i := bytes.Index(b.pending, []byte("\x1b["))
		if i < 0 {
			out = append(out, b.pending...)
			b.pending = nil
			break
		}
		if i > 0 {
			out = append(out, b.pending[:i]...)
			b.pending = b.pending[i:]
		}

		end := csiEnd(b.pending)
		if end == -1 {
			break // CSI split across terminal reads.
		}
		seq := b.pending[:end+1]
		b.pending = b.pending[end+1:]

		if msg, release, ok := parseEnhancedKey(seq); ok {
			if !release && b.send != nil {
				b.send(msg)
			}
			continue
		}
		if isKittyControl(seq) {
			continue
		}
		out = append(out, seq...)
	}
	return out
}

func parseEnhancedKey(seq []byte) (msg enhancedKeyMsg, release bool, ok bool) {
	if msg, release, ok = parseKittyKey(seq); ok {
		return msg, release, true
	}
	return parseModifyOtherKeys(seq)
}

func csiEnd(p []byte) int {
	if len(p) < 3 || p[0] != 0x1b || p[1] != '[' {
		return -1
	}
	for i := 2; i < len(p); i++ {
		if p[i] >= 0x40 && p[i] <= 0x7e {
			return i
		}
	}
	return -1
}

func isKittyControl(seq []byte) bool {
	if len(seq) < 4 || seq[len(seq)-1] != 'u' {
		return false
	}
	// CSI ? u, CSI ? flags u, CSI > flags u, and CSI < u are protocol
	// query/reply/push/pop traffic, not keyboard input.
	return seq[2] == '?' || seq[2] == '>' || seq[2] == '<'
}

// parseKittyKey parses CSI unicode-key[:shifted[:base]][;modifiers[:event]][;text]u.
// It accepts the protocol's optional alternate-key and associated-text fields,
// but only needs the primary key and modifier/event field to route the event.
func parseKittyKey(seq []byte) (msg enhancedKeyMsg, release bool, ok bool) {
	if len(seq) < 4 || seq[0] != 0x1b || seq[1] != '[' || seq[len(seq)-1] != 'u' || isKittyControl(seq) {
		return msg, false, false
	}
	fields := bytes.Split(seq[2:len(seq)-1], []byte(";"))
	if len(fields) == 0 || len(fields) > 3 {
		return msg, false, false
	}
	code, ok := firstCodePoint(fields[0])
	if !ok {
		return msg, false, false
	}
	mods, event := 1, 1
	if len(fields) >= 2 && len(fields[1]) > 0 {
		var valid bool
		mods, event, valid = parseModifiers(fields[1])
		if !valid {
			return msg, false, false
		}
	}
	if len(fields) == 3 && len(fields[2]) > 0 && !validCodeList(fields[2]) {
		return msg, false, false
	}
	if mods < 1 || event < 1 || event > 3 {
		return msg, false, false
	}

	msg.data = append([]byte(nil), seq...)
	if event == 3 {
		return msg, true, true
	}
	msg.key, msg.hasKey = kittyTeaKey(code, mods, fields)
	return msg, false, true
}

// parseModifyOtherKeys accepts xterm's CSI 27;modifier;code~ form. Modern
// terminals such as Ghostty can emit it for modified text keys, so treating it
// as an enhanced event avoids Bubble Tea silently discarding Ctrl/Shift+Enter
// and similar input. It has no release-event representation.
func parseModifyOtherKeys(seq []byte) (msg enhancedKeyMsg, release bool, ok bool) {
	if len(seq) < 6 || seq[0] != 0x1b || seq[1] != '[' || seq[len(seq)-1] != '~' {
		return msg, false, false
	}
	fields := bytes.Split(seq[2:len(seq)-1], []byte(";"))
	if len(fields) != 3 || string(fields[0]) != "27" {
		return msg, false, false
	}
	mods, event, valid := parseModifiers(fields[1])
	if !valid || event != 1 || mods < 1 {
		return msg, false, false
	}
	code, valid := firstCodePoint(fields[2])
	if !valid {
		return msg, false, false
	}
	msg.data = append([]byte(nil), seq...)
	msg.key, msg.hasKey = kittyTeaKey(code, mods, nil)
	return msg, false, true
}

func firstCodePoint(field []byte) (int, bool) {
	part, _, _ := bytes.Cut(field, []byte(":"))
	if len(part) == 0 {
		return 0, false
	}
	n, err := strconv.Atoi(string(part))
	return n, err == nil && n >= 0 && n <= utf8.MaxRune
}

func parseModifiers(field []byte) (mods, event int, ok bool) {
	parts := bytes.Split(field, []byte(":"))
	if len(parts) > 2 || len(parts[0]) == 0 {
		return 0, 0, false
	}
	mods, err := strconv.Atoi(string(parts[0]))
	if err != nil {
		return 0, 0, false
	}
	event = 1
	if len(parts) == 2 {
		var eventErr error
		event, eventErr = strconv.Atoi(string(parts[1]))
		if eventErr != nil {
			return 0, 0, false
		}
	}
	return mods, event, true
}

func validCodeList(field []byte) bool {
	for _, part := range bytes.Split(field, []byte(":")) {
		if _, ok := firstCodePoint(part); !ok {
			return false
		}
	}
	return true
}

func kittyTeaKey(code, mods int, fields [][]byte) (tea.KeyMsg, bool) {
	flags := mods - 1
	alt := flags&2 != 0

	// Kitty reports Ctrl+letters as their printable codepoint plus the Ctrl
	// modifier. Bubble Tea (and Rook's configured prefix) uses the equivalent
	// C0 key type, so recover it for the local command path.
	if flags&4 != 0 && code <= unicode.MaxASCII {
		ctrl := byte(code) & 0x1f
		if ctrl > 0 && ctrl <= 31 {
			return tea.KeyMsg{Type: tea.KeyType(ctrl), Alt: alt}, true
		}
	}
	if code <= 31 || code == 127 {
		return tea.KeyMsg{Type: tea.KeyType(code), Alt: alt}, true
	}
	// Kitty assigns functional keys to the Unicode private-use area. They do
	// not have a Bubble Tea equivalent, but their original bytes are retained
	// in enhancedKeyMsg and forwarded to the pane.
	if code == 0 || (code >= 0xe000 && code <= 0xf8ff) || code > utf8.MaxRune || !utf8.ValidRune(rune(code)) {
		return tea.KeyMsg{}, false
	}
	r := rune(code)
	if len(fields) == 3 && len(fields[2]) > 0 {
		if text, ok := firstCodePoint(fields[2]); ok && text != 0 {
			r = rune(text)
		}
	} else if flags&1 != 0 {
		r = unicode.ToUpper(r)
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}, Alt: alt}, true
}
