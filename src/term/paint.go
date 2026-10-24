package term

import (
	"cmp"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"golang.org/x/text/width"

	"mux/src/herdr/endpoint"
)

func (v *View) paint(p *ui.Painter, r ui.Rect) {
	s := v.surface
	if s == nil {
		p.Fill(r, v.pal.theme.Background, 0)
		return
	}
	boxes := v.boxes()
	for i := range boxes {
		b := &boxes[i]
		v.paintCard(p, r, b)
		in, content := b.pane.Inner, b.content
		t, width := top(b.pane), int(in.Width)
		// The matches and selection the screen shows, in their rows.
		var matches []textRange
		current := -1
		if search := v.searches[b.pane.ID]; search != nil && search.open {
			for i, m := range search.matches {
				if m.End.Row >= t && m.Start.Row < t+int(in.Height) {
					if i == search.current {
						current = len(matches)
					}
					matches = append(matches, m)
				}
			}
		}
		var from, to point
		selected := false
		if sel, _, ok := v.selection(); ok && sel.pane == b.pane.ID {
			from, to, selected = sel.span()
		}
		var marks []mark
		for y := range int(in.Height) {
			marks = marks[:0]
			for i, m := range matches {
				if x0, x1, ok := cols(m.Start, m.End, width, t+y); ok {
					marks = append(marks, v.searchMark(x0, x1, i == current))
				}
			}
			if x0, x1, ok := cols(from, to, width, t+y); selected && ok {
				marks = append(marks, v.selectionMark(x0, x1))
			}
			start := (int(in.Y)+y)*int(s.Frame.Width) + int(in.X)
			row := s.Frame.Cells[start : start+width]
			v.paintRow(p, r.X+content.X, r.Y+content.Y+float32(y)*v.cellH, row, marks)
		}
	}
	v.paintLines(p, r)
	v.paintDrop(p, r)
	v.paintCursor(p, r)
}

// mark colors cells x0 to x1 of a row over their own background, and
// their text, unless fg is zero.
type mark struct {
	x0, x1 int
	bg, fg ui.Color
}

// searchMark marks a match of the search, the one the user is at as
// current, in the theme's colors, else Ghostty's.
func (v *View) searchMark(x0, x1 int, current bool) mark {
	th := v.pal.theme
	if current {
		return mark{x0, x1, cmp.Or(th.SearchSelected, ui.RGB(0xff, 0xa7, 0x26)), cmp.Or(th.SearchSelectedText, ui.RGB(0, 0, 0))}
	}
	return mark{x0, x1, cmp.Or(th.Search, ui.RGB(0xff, 0xe0, 0x82)), cmp.Or(th.SearchText, ui.RGB(0, 0, 0))}
}

// selectionMark marks selected cells in the theme's colors, else a third
// of the foreground over them.
func (v *View) selectionMark(x0, x1 int) mark {
	th := v.pal.theme
	return mark{x0, x1, cmp.Or(th.Selection, th.Foreground.Alpha(1.0/3)), th.SelectionText}
}

// paintCard draws a pane's box, its header on the terminal's background as
// its cells are, a little toward the foreground while another pane has
// the focus; herdr's own frame is not drawn, its cells being outside the
// pane's.
func (v *View) paintCard(p *ui.Painter, r ui.Rect, b *paneBox) {
	th := v.pal.theme
	card := b.card
	card.X, card.Y = card.X+r.X, card.Y+r.Y
	bg := th.Background
	if !b.pane.Focused {
		bg = bg.Mix(th.Foreground, 0.06)
	}
	p.Fill(card, bg, 0)
}

// paintLines draws the lines between the panes of a split tab.
func (v *View) paintLines(p *ui.Painter, r ui.Rect) {
	for _, sp := range v.surface.Splits {
		l := v.line(sp)
		l.X, l.Y = l.X+r.X, l.Y+r.Y
		p.Fill(l, v.pal.theme.Foreground.Alpha(0.12), 0)
	}
}

// paintRow draws a row of cells from x, y: their backgrounds where they
// have their own, the marks over them, then their glyphs and lines.
func (v *View) paintRow(p *ui.Painter, x0, y float32, row []endpoint.Cell, marks []mark) {
	start := -1
	var run ui.Color
	flush := func(end int) {
		if start >= 0 {
			p.Fill(ui.Rect{X: x0 + float32(start)*v.cellW, Y: y, W: float32(end-start) * v.cellW, H: v.cellH}, run, 0)
		}
		start = -1
	}
	var bg ui.Color
	isDefault, covered := true, false
	for x := range row {
		// A wide character's second cell takes its background: herdr
		// sends the cell blank in the default style in whole surfaces,
		// though in its style in patches.
		if !covered {
			_, bg, isDefault = v.pal.cellColors(&row[x])
		}
		covered = !covered && wide(row[x].Symbol)
		switch {
		case isDefault:
			flush(x)
		case start < 0:
			start, run = x, bg
		case bg != run:
			flush(x)
			start, run = x, bg
		}
	}
	flush(len(row))
	for _, m := range marks {
		p.Fill(ui.Rect{X: x0 + float32(m.x0)*v.cellW, Y: y, W: float32(m.x1-m.x0+1) * v.cellW, H: v.cellH}, m.bg, 0)
	}
	covered = false
	for x := range row {
		c := &row[x]
		if covered {
			covered = false
			continue
		}
		covered = wide(c.Symbol)
		if c.Skip || c.Symbol == "" || c.Symbol == " " {
			continue
		}
		fg, _, _ := v.pal.cellColors(c)
		for _, m := range marks {
			if x >= m.x0 && x <= m.x1 && m.fg != (ui.Color{}) {
				fg = m.fg
			}
		}
		cx, w := x0+float32(x)*v.cellW, v.cellW
		if covered {
			w *= 2
		}
		p.Glyphs(v.shape(c.Symbol, c.Modifier), cx, y+v.baseline, fg)
		if c.Modifier&underline != 0 {
			p.Fill(ui.Rect{X: cx, Y: y + v.cellH - 2, W: w, H: 1}, fg, 0)
		}
		if c.Modifier&strikethrough != 0 {
			p.Fill(ui.Rect{X: cx, Y: y + v.cellH/2, W: w, H: 1}, fg, 0)
		}
	}
}

// wide reports whether a cell's symbol takes two columns, as East Asian
// wide and fullwidth characters, emoji among them, do.
func wide(symbol string) bool {
	r, _ := utf8.DecodeRuneInString(symbol)
	k := width.LookupRune(r).Kind()
	return k == width.EastAsianWide || k == width.EastAsianFullwidth
}

// paintCursor draws the cursor in its shape (DECSCUSR: a block for 0 to 2,
// an underline for 3 and 4, a bar for 5 and 6), hollow while the view does
// not have the focus, and an input method's composition at it.
func (v *View) paintCursor(p *ui.Painter, r ui.Rect) {
	cur := v.cursor()
	if cur == nil {
		return
	}
	th := v.pal.theme
	color := th.Cursor
	if color == (ui.Color{}) {
		color = th.Foreground
	}
	box := v.cellRect(r, int(cur.X), int(cur.Y))
	if v.preedit != "" {
		glyphs := ui.Shape(v.preedit, v.font)
		width := v.cellW
		if n := len(glyphs); n > 0 {
			width = max(width, glyphs[n-1].X+glyphs[n-1].Advance)
		}
		p.Fill(ui.Rect{X: box.X, Y: box.Y, W: width, H: v.cellH}, th.Background, 0)
		p.Glyphs(glyphs, box.X, box.Y+v.baseline, th.Foreground)
		p.Fill(ui.Rect{X: box.X, Y: box.Y + v.cellH - 2, W: width, H: 1}, th.Foreground, 0)
		return
	}
	if !v.focused {
		p.Stroke(box, color, 0, 1)
		return
	}
	switch cur.Shape {
	case 3, 4:
		p.Fill(ui.Rect{X: box.X, Y: box.Y + v.cellH - 2, W: v.cellW, H: 2}, color, 0)
	case 5, 6:
		p.Fill(ui.Rect{X: box.X, Y: box.Y, W: 2, H: v.cellH}, color, 0)
	default:
		p.Fill(box, color, 0)
		// The cell's glyph again, on the block.
		c := &v.surface.Frame.Cells[int(cur.Y)*int(v.surface.Frame.Width)+int(cur.X)]
		if c.Symbol != "" && c.Symbol != " " && !c.Skip {
			text := th.CursorText
			if text == (ui.Color{}) {
				text = th.Background
			}
			p.Glyphs(v.shape(c.Symbol, c.Modifier), box.X, box.Y+v.baseline, text)
		}
	}
}

// shape returns the glyphs of a cell's symbol in its style, which the
// cache keeps: shaping is the costly part of painting a grid.
func (v *View) shape(symbol string, mods uint16) []ui.Glyph {
	k := glyphKey{symbol, mods & (bold | italic)}
	if g, ok := v.glyphs[k]; ok {
		return g
	}
	if len(v.glyphs) >= maxGlyphs {
		clear(v.glyphs)
	}
	font := v.font
	if k.style&bold != 0 {
		font.Weight = 700
	}
	font.Italic = k.style&italic != 0
	g := ui.Shape(symbol, font)
	v.glyphs[k] = g
	return g
}
