package term

import (
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
)

// quadrants are the cell's quarters the quadrant characters U+2596 to
// U+259F fill: 1 the upper left, 2 the upper right, 4 the lower left and
// 8 the lower right.
var quadrants = [...]uint8{4, 8, 1, 1 | 4 | 8, 1 | 8, 1 | 2 | 4, 1 | 2 | 8, 2, 2 | 4, 2 | 4 | 8}

// paintBlock draws a block element (U+2580 to U+259F) as rectangles of its
// cell, as Ghostty does, and reports whether symbol is one. The font's
// glyphs for them end within a pixel, so their antialiased edges would
// show as seams between blocks side by side, as in Claude Code's mascot;
// Fill rounds the rectangles' edges to whole pixels.
func paintBlock(p *ui.Painter, cell ui.Rect, symbol string, c ui.Color) bool {
	r, n := utf8.DecodeRuneInString(symbol)
	if n != len(symbol) {
		return false
	}
	// fill fills the part of the cell from x0, y0 to x1, y1, in fractions
	// of its width and height.
	fill := func(x0, y0, x1, y1 float32) {
		p.Fill(ui.Rect{X: cell.X + x0*cell.W, Y: cell.Y + y0*cell.H, W: (x1 - x0) * cell.W, H: (y1 - y0) * cell.H}, c, 0)
	}
	switch {
	case r == 0x2580: // upper half
		fill(0, 0, 1, 0.5)
	case r >= 0x2581 && r <= 0x2588: // lower one eighth to the full block
		fill(0, 1-float32(r-0x2580)/8, 1, 1)
	case r >= 0x2589 && r <= 0x258f: // left seven eighths to one eighth
		fill(0, 0, float32(0x2590-r)/8, 1)
	case r == 0x2590: // right half
		fill(0.5, 0, 1, 1)
	case r >= 0x2591 && r <= 0x2593: // light, medium and dark shades
		p.Fill(cell, c.Alpha(float32(r-0x2590)/4), 0)
	case r == 0x2594: // upper one eighth
		fill(0, 0, 1, 1.0/8)
	case r == 0x2595: // right one eighth
		fill(7.0/8, 0, 1, 1)
	case r >= 0x2596 && r <= 0x259f:
		q := quadrants[r-0x2596]
		for i := range 4 {
			if q&(1<<i) != 0 {
				x, y := float32(i%2)/2, float32(i/2)/2
				fill(x, y, x+0.5, y+0.5)
			}
		}
	default:
		return false
	}
	return true
}
