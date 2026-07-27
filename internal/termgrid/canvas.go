package termgrid

import (
	"strconv"
	"strings"
)

// Color is a terminal colour: 0-7 basic, 8-15 bright, 16-255 palette, and
// anything larger a packed 0xRRGGBB truecolor value. Mirrors the emulator's
// own encoding so a Cell can be copied straight out of the grid.
type Color uint32

// Default* mirror the emulator's sentinel colours, which render as "no SGR
// colour code at all" — i.e. the terminal's own default.
const (
	DefaultFG Color = 1 << 24
	DefaultBG Color = 1<<24 + 1
)

// ParseColor reads a colour written the way lipgloss accepts it — a palette
// index ("6", "110") or a hex string ("#7aa2f7") — into a Color. Anything
// unparseable returns the fallback, so a typo in a config file costs you one
// colour rather than the whole UI.
func ParseColor(s string, fallback Color) Color {
	if s == "" {
		return fallback
	}
	if s[0] == '#' {
		v, err := strconv.ParseUint(s[1:], 16, 32)
		if err != nil {
			return fallback
		}
		// Packed 0xRRGGBB, the same encoding the emulator uses for truecolor
		// cells, so one renderer handles both. The known wrinkle: a hex
		// colour darker than #000100 lands in the palette range and is drawn
		// as a palette index. Nobody themes a UI in near-black, and matching
		// the emulator's encoding is worth more than covering that corner.
		return Color(v)
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 || n > 255 {
		return fallback
	}
	return Color(n)
}

// Cell is one character position: its rune plus its styling.
type Cell struct {
	Char rune
	FG   Color
	BG   Color
	Mode int16 // attr* bitset
}

// Canvas is a fixed-size grid of cells that panes get composited onto before
// the whole screen is rendered to ANSI in one pass. Compositing at the cell
// level (rather than splicing pre-rendered ANSI strings) is what makes side
// by side panes possible at all: you cannot reliably cut a styled string in
// half without re-parsing every escape sequence in it.
type Canvas struct {
	W, H  int
	cells []Cell
}

func NewCanvas(w, h int) *Canvas {
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}
	c := &Canvas{W: w, H: h, cells: make([]Cell, w*h)}
	for i := range c.cells {
		c.cells[i] = Cell{Char: ' ', FG: DefaultFG, BG: DefaultBG}
	}
	return c
}

func (c *Canvas) At(x, y int) Cell {
	if x < 0 || y < 0 || x >= c.W || y >= c.H {
		return Cell{Char: ' ', FG: DefaultFG, BG: DefaultBG}
	}
	return c.cells[y*c.W+x]
}

func (c *Canvas) Set(x, y int, cell Cell) {
	if x < 0 || y < 0 || x >= c.W || y >= c.H {
		return
	}
	c.cells[y*c.W+x] = cell
}

// Blit copies src onto c with its top-left corner at (x0, y0), clipping at
// the edges.
func (c *Canvas) Blit(src *Canvas, x0, y0 int) {
	for y := range src.H {
		for x := range src.W {
			c.Set(x0+x, y0+y, src.At(x, y))
		}
	}
}

// DrawText writes s starting at (x, y) using style for every cell, clipped at
// the right edge. Only the rune is taken from s — styling always comes from
// style, so callers never have to build escape sequences by hand.
func (c *Canvas) DrawText(x, y int, s string, style Cell) {
	for i, r := range []rune(s) {
		cell := style
		cell.Char = r
		c.Set(x+i, y, cell)
	}
}

// Fill paints a rectangle with a single cell value.
func (c *Canvas) Fill(x0, y0, w, h int, cell Cell) {
	for y := y0; y < y0+h; y++ {
		for x := x0; x < x0+w; x++ {
			c.Set(x, y, cell)
		}
	}
}

// RenderANSI serialises the canvas to exactly H lines joined by "\n", with no
// carriage returns and no absolute cursor positioning — see the Grid.RenderANSI
// doc comment for why both of those are non-negotiable.
func (c *Canvas) RenderANSI() string {
	var b strings.Builder
	b.Grow(c.W * c.H * 2)

	for y := range c.H {
		first := true
		var lastFG, lastBG Color
		var lastMode int16
		for x := range c.W {
			cell := c.At(x, y)
			if first || cell.FG != lastFG || cell.BG != lastBG || cell.Mode != lastMode {
				b.WriteString(sgr(cell.FG, cell.BG, cell.Mode))
				lastFG, lastBG, lastMode = cell.FG, cell.BG, cell.Mode
				first = false
			}
			if cell.Char == 0 {
				b.WriteByte(' ')
			} else {
				b.WriteRune(cell.Char)
			}
		}
		b.WriteString("\x1b[0m")
		if y < c.H-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// RenderPlain serialises the canvas as plain text, trailing spaces trimmed.
func (c *Canvas) RenderPlain() string {
	lines := make([]string, c.H)
	for y := range c.H {
		var b strings.Builder
		for x := range c.W {
			ch := c.At(x, y).Char
			if ch == 0 {
				ch = ' '
			}
			b.WriteRune(ch)
		}
		lines[y] = strings.TrimRight(b.String(), " ")
	}
	return strings.Join(lines, "\n")
}

func sgr(fg, bg Color, mode int16) string {
	codes := []string{"0"}
	if mode&attrBold != 0 {
		codes = append(codes, "1")
	}
	if mode&attrItalic != 0 {
		codes = append(codes, "3")
	}
	if mode&attrUnderline != 0 {
		codes = append(codes, "4")
	}
	if mode&attrBlink != 0 {
		codes = append(codes, "5")
	}
	if mode&attrReverse != 0 {
		codes = append(codes, "7")
	}
	if c := colorCode(fg, true); c != "" {
		codes = append(codes, c)
	}
	if c := colorCode(bg, false); c != "" {
		codes = append(codes, c)
	}
	return "\x1b[" + strings.Join(codes, ";") + "m"
}

func colorCode(c Color, fg bool) string {
	base := 30
	if !fg {
		base = 40
	}
	switch {
	case c >= DefaultFG:
		return "" // default fg/bg/cursor sentinels: emit no colour
	case c < 8:
		return strconv.Itoa(base + int(c))
	case c < 16:
		return strconv.Itoa(base + 60 + int(c) - 8)
	case c < 256:
		return strconv.Itoa(base+8) + ";5;" + strconv.Itoa(int(c))
	default:
		return strconv.Itoa(base+8) + ";2;" +
			strconv.Itoa(int((c>>16)&0xFF)) + ";" +
			strconv.Itoa(int((c>>8)&0xFF)) + ";" +
			strconv.Itoa(int(c&0xFF))
	}
}
