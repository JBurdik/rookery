package termgrid

// Glyph attribute bits. vt10x keeps these unexported (state.go's attr*
// consts); mirrored here by inspecting the pinned module version — a small,
// stable, iota-numbered bitset that isn't expected to change.
const (
	attrReverse   = 1 << iota // 1
	attrUnderline             // 2
	attrBold                  // 4
	attrGfx                   // 8 - line-drawing charset marker, not a display attribute
	attrItalic                // 16
	attrBlink                 // 32
)

// RenderANSI returns the current screen as a styled ANSI string: exactly
// `rows` lines joined by "\n", no trailing newline.
//
// Two invariants matter, and violating either is what made every frame after
// the first render as garbage: no "\r" anywhere, and no absolute cursor
// positioning (\x1b[y;xH). Frames are embedded in a Bubble Tea View, and
// Bubble Tea's renderer does its own line splitting, carriage returns and
// cursor placement — an extra "\r\n" doubles the CR and a positioning escape
// desynchronises its line accounting for the rest of the session. The cursor
// is therefore drawn in-band, as a reverse-video cell.
func (g *Grid) RenderANSI() string {
	return g.Snapshot(true).RenderANSI()
}

// RenderPlain returns the current screen as plain text (no escape codes).
func (g *Grid) RenderPlain() string {
	return g.Snapshot(false).RenderPlain()
}

// stripANSI removes escape sequences (CSI, OSC, and lone ESC-prefixed
// sequences) from b, returning the printable text. Small hand-rolled
// scanner — enough for scrollback transcript purposes, not a full parser.
func stripANSI(b []byte) []byte {
	out := make([]byte, 0, len(b))
	for i := 0; i < len(b); i++ {
		if b[i] != 0x1b {
			out = append(out, b[i])
			continue
		}
		if i+1 >= len(b) {
			break
		}
		switch b[i+1] {
		case '[': // CSI ... ends with a byte in 0x40-0x7e
			j := i + 2
			for j < len(b) && (b[j] < 0x40 || b[j] > 0x7e) {
				j++
			}
			i = j
		case ']': // OSC ... ends with BEL or ESC \
			j := i + 2
			for j < len(b) && b[j] != 0x07 && !(b[j] == 0x1b && j+1 < len(b) && b[j+1] == '\\') {
				j++
			}
			if j < len(b) && b[j] == 0x1b {
				j++
			}
			i = j
		default:
			i++
		}
	}
	return out
}
