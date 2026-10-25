package term

import (
	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"

	"mux/internal/herdr/endpoint"
)

// Cell modifiers, as herdr sends them.
const (
	bold          = 1
	dim           = 1 << 1
	italic        = 1 << 2
	underline     = 1 << 3
	reversed      = 1 << 6
	hidden        = 1 << 7
	strikethrough = 1 << 8
)

// palette is a theme's 256 colors: its 16, then xterm's color cube and
// grays.
type palette struct {
	theme  *terminal.Theme
	dark   bool
	colors [256]ui.Color
}

func newPalette(t *terminal.Theme, dark bool) *palette {
	p := &palette{theme: t, dark: dark}
	copy(p.colors[:16], t.Palette[:])
	steps := [6]uint8{0, 95, 135, 175, 215, 255}
	for i := range 216 {
		p.colors[16+i] = ui.RGB(steps[i/36], steps[i/6%6], steps[i%6])
	}
	for i := range 24 {
		g := uint8(8 + 10*i)
		p.colors[232+i] = ui.RGB(g, g, g)
	}
	return p
}

// hostTheme is the palette as the server takes it.
func (p *palette) hostTheme() endpoint.HostTheme {
	rgb := func(c ui.Color) endpoint.RGB { return endpoint.RGB{R: c.R, G: c.G, B: c.B} }
	t := endpoint.HostTheme{Foreground: rgb(p.theme.Foreground), Background: rgb(p.theme.Background), Dark: p.dark}
	for i, c := range p.colors {
		t.Palette[i] = rgb(c)
	}
	return t
}

// color resolves a cell color: 0 the theme's default, 1..16 its named
// colors, 0x010000XX an indexed one and 0x02RRGGBB RGB.
func (p *palette) color(v uint32, def ui.Color) (c ui.Color, isDefault bool) {
	switch v >> 24 {
	case 0:
		if n := v & 0xff; n >= 1 && n <= 16 {
			return p.colors[n-1], false
		}
	case 1:
		return p.colors[v&0xff], false
	case 2:
		return ui.RGB(uint8(v>>16), uint8(v>>8), uint8(v)), false
	}
	return def, true
}

// cellColors returns what a cell paints its glyph and its background in,
// and whether the background is the theme's own.
func (p *palette) cellColors(c *endpoint.Cell) (fg, bg ui.Color, defaultBG bool) {
	fg, _ = p.color(c.FG, p.theme.Foreground)
	bg, defaultBG = p.color(c.BG, p.theme.Background)
	if c.Modifier&reversed != 0 {
		fg, bg, defaultBG = bg, fg, false
	}
	if c.Modifier&dim != 0 {
		fg = fg.Mix(bg, 0.5)
	}
	if c.Modifier&hidden != 0 {
		fg = bg
	}
	return fg, bg, defaultBG
}
