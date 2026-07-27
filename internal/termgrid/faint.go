package termgrid

import (
	"bytes"
	"unicode/utf8"
)

// Faint text — SGR 2 — is how every agent TUI draws the things it does not
// want you to read as content: Claude Code's input placeholder, its hints, the
// dimmed parts of a diff. vt10x's setAttr has no case for it, so it lands in
// the "unknown attribute" branch and is dropped, and a placeholder ends up
// rendered exactly like text the user typed.
//
// ponytail: the fix is a rewrite on the way in rather than a fork of vt10x.
// Faint is smuggled through the one attribute bit vt10x does track and nothing
// here has any use for — blink — and translated back to SGR 2 when the canvas
// serialises a cell (see sgr()). So the real terminal does the dimming, which
// is the only way to get it right: faint is a blend towards the background,
// not a fixed grey.
//
// The ceiling: a program that genuinely asks for blinking text gets faint text
// instead. Upgrade path is a vendored vt10x with its own faint bit, worth doing
// the day something in a pane actually blinks on purpose.

// rewriteFaint translates SGR 2 to SGR 5, and SGR 22 (normal intensity, which
// clears faint as well as bold) to SGR 22;25.
//
// It is a method because a CSI sequence can be split across two reads from the
// PTY, so an incomplete one has to be held back until the rest arrives.
func (g *Grid) rewriteFaint(p []byte) []byte {
	if len(g.escTail) > 0 {
		p = append(g.escTail, p...)
		g.escTail = nil
	}

	var out []byte
	for i := 0; i < len(p); {
		if p[i] != 0x1b {
			out = append(out, p[i])
			i++
			continue
		}
		// A bare ESC at the very end, or an ESC [ whose final byte has not
		// arrived: hold it for the next write.
		if i+1 >= len(p) {
			if g.holdTail(p[i:]) {
				return out
			}
			out = append(out, p[i])
			i++
			continue
		}
		if p[i+1] != '[' {
			out = append(out, p[i], p[i+1])
			i += 2
			continue
		}
		end := i + 2
		for end < len(p) && (p[end] < 0x40 || p[end] > 0x7e) {
			end++
		}
		if end == len(p) {
			if g.holdTail(p[i:]) {
				return out
			}
			out = append(out, p[i:]...)
			break
		}
		if p[end] == 'm' {
			out = append(out, "\x1b["...)
			out = append(out, rewriteSGRParams(p[i+2:end])...)
			out = append(out, 'm')
		} else {
			out = append(out, p[i:end+1]...)
		}
		i = end + 1
	}
	return g.holdPartialRune(out)
}

// holdPartialRune parks a multi-byte character that the chunk ended in the
// middle of.
//
// vt10x keeps no state between Write calls for this: feed it half a rune and it
// decodes the half, so a `╭` split across two PTY reads is lost and the rest of
// the box-drawing row goes with it. A read boundary landing inside a rune is
// rare but entirely normal — measured on a real Claude Code capture, replaying
// it a byte at a time loses 7 of 32 dim cells and mangles the welcome box.
//
// An escape sequence cannot follow a partial rune (ESC is 0x1b, and only
// 0x80-0xbf can continue one), so the two tails never compete for the buffer.
func (g *Grid) holdPartialRune(out []byte) []byte {
	for i := 1; i <= 3 && i <= len(out); i++ {
		b := out[len(out)-i]
		if b < utf8.RuneSelf {
			return out // plain ASCII: nothing is pending
		}
		if b&0xc0 == 0x80 {
			continue // a continuation byte; keep walking back to the lead
		}
		if utf8.FullRune(out[len(out)-i:]) || len(g.escTail) > 0 {
			return out
		}
		g.escTail = append(g.escTail[:0], out[len(out)-i:]...)
		return out[:len(out)-i]
	}
	return out
}

// maxEscTail caps how long an unterminated escape can stall the bytes behind
// it. A sequence that never completes must not freeze the pane.
const maxEscTail = 64

// holdTail parks an incomplete escape for the next write, reporting whether it
// took it. It refuses once the tail would grow past maxEscTail, which is the
// point at which "this is a split sequence" stops being the likely story.
func (g *Grid) holdTail(b []byte) bool {
	if len(b) > maxEscTail {
		return false
	}
	g.escTail = append(g.escTail[:0], b...)
	return true
}

// rewriteSGRParams maps the faint parameters inside one SGR sequence, leaving
// everything else byte-for-byte alone.
//
// The 38/48 extended-colour forms have to be skipped whole: their arguments
// are colour indices, and "2" as the third parameter of ESC[38;5;2m is the
// colour green, not a request for faint text.
func rewriteSGRParams(params []byte) []byte {
	if len(params) == 0 {
		return params
	}
	fields := bytes.Split(params, []byte(";"))
	out := make([][]byte, 0, len(fields)+1)
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		switch {
		case bytes.Equal(f, []byte("38")), bytes.Equal(f, []byte("48")):
			// Copy the selector and its arguments verbatim: 5 takes one more
			// parameter, 2 takes three.
			out = append(out, f)
			if i+1 < len(fields) {
				i++
				out = append(out, fields[i])
				n := 0
				switch {
				case bytes.Equal(fields[i], []byte("5")):
					n = 1
				case bytes.Equal(fields[i], []byte("2")):
					n = 3
				}
				for ; n > 0 && i+1 < len(fields); n-- {
					i++
					out = append(out, fields[i])
				}
			}
		case bytes.Equal(f, []byte("2")):
			out = append(out, []byte("5")) // faint -> the bit vt10x keeps
		case bytes.Equal(f, []byte("22")):
			// Normal intensity clears faint too, so it has to clear the bit
			// standing in for it.
			out = append(out, f, []byte("25"))
		default:
			out = append(out, f)
		}
	}
	return bytes.Join(out, []byte(";"))
}
