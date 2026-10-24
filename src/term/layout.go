package term

import (
	"slices"

	"github.com/egoist/mygo/ui"

	"mux/src/herdr/endpoint"
)

const (
	// headerRows is how many rows of cells a pane's header takes. herdr
	// frames split panes, leaving a row above and below each, which the
	// header takes instead: the pane's cells show a row lower than the
	// surface has them.
	headerRows = 2
	// minHeader is the least height of a pane's header, which the rows of
	// a split pane's frame may fall short of.
	minHeader = 40
)

// paneBox is where the view shows a pane: its box and header, in the view,
// and its cells. Panes meet edge to edge, out to the view's.
type paneBox struct {
	pane    endpoint.SurfacePane
	card    ui.Rect
	header  ui.Rect
	content ui.Rect
}

// lift returns how many rows a split tab's view keeps beyond the surface
// for headers the surface leaves no room for, as herdr frames no pane with
// pane_borders off.
func lift(s *endpoint.Surface) int {
	n := 0
	for _, p := range s.Panes {
		top := int(p.Inner.Y) - int(p.Rect.Y)
		bottom := int(p.Rect.Y+p.Rect.Height) - int(p.Inner.Y+p.Inner.Height)
		n = max(n, headerRows-top-bottom)
	}
	return n
}

// lone reports whether the surface shows a single pane, which herdr does
// not frame.
func lone(s *endpoint.Surface) bool { return s == nil || len(s.Panes) <= 1 }

// frameHeader is the height of the header a split pane's frame makes room
// for: its rows above and below the pane.
func (v *View) frameHeader() float32 { return headerRows * v.cellH }

// headerHeight is the height of a pane's header.
func (v *View) headerHeight() float32 { return max(minHeader, v.frameHeader()) }

// tops returns the rows the surface's panes start at, each once: each pane
// starting at one pushes the panes below it down by the room its header
// takes beyond its frame.
func tops(s *endpoint.Surface) []uint16 {
	var rows []uint16
	for _, p := range s.Panes {
		if !slices.Contains(rows, p.Rect.Y) {
			rows = append(rows, p.Rect.Y)
		}
	}
	return rows
}

// HeaderCenter is how far below the view's top the headers along it center
// their contents.
func (v *View) HeaderCenter() float32 { return v.headerHeight() / 2 }

// origin is where the view shows the surface's first cell. A lone pane's
// cells keep a cell's room from the view's sides, as the frame's column
// keeps a split pane's.
func (v *View) origin(s *endpoint.Surface) (x, y float32) {
	if lone(s) {
		return v.cellW, v.headerHeight()
	}
	return 0, 0
}

// fit returns the size of the surface the view fits in w by h.
func (v *View) fit(s *endpoint.Surface, w, h float32) (cols, rows int) {
	ox, oy := v.origin(s)
	cols = int((w - 2*ox) / v.cellW)
	if lone(s) {
		return cols, int((h - oy) / v.cellH)
	}
	taller := (v.headerHeight() - v.frameHeader()) * float32(len(tops(s)))
	return cols, int((h-taller)/v.cellH) - lift(s)
}

func (v *View) boxes() []paneBox {
	s := v.surface
	if s == nil {
		return nil
	}
	ox, oy := v.origin(s)
	taller := v.headerHeight() - v.frameHeader()
	boxes := make([]paneBox, 0, len(s.Panes))
	for _, p := range s.Panes {
		in := p.Inner
		b := paneBox{pane: p, content: ui.Rect{
			X: ox + float32(in.X)*v.cellW, Y: oy + float32(in.Y)*v.cellH,
			W: float32(in.Width) * v.cellW, H: float32(in.Height) * v.cellH,
		}}
		if lone(s) {
			b.card = ui.Rect{W: v.width, H: v.height}
			b.header = ui.Rect{W: v.width, H: v.headerHeight()}
			boxes = append(boxes, b)
			continue
		}
		b.card = v.span(p.Rect)
		b.header = ui.Rect{X: b.card.X, Y: b.card.Y, W: b.card.W, H: v.headerHeight()}
		// The header takes the frame's top row, and the rows below the
		// pane after its cells: its bottom row, or those the view adds.
		b.content.Y += float32(headerRows-(int(in.Y)-int(p.Rect.Y)))*v.cellH + v.above(p.Rect.Y) + taller
		boxes = append(boxes, b)
	}
	return boxes
}

// span returns where a split tab's area r of cells is in the view: out to
// the view's right and bottom edges where it meets them, taking the room
// short of a whole cell there.
func (v *View) span(r endpoint.Rect) ui.Rect {
	left, right := float32(r.X)*v.cellW, float32(r.X+r.Width)*v.cellW
	top, bottom := v.rowTop(r.Y), v.rowTop(r.Y+r.Height)
	if r.X+r.Width >= v.surface.Frame.Width {
		right = v.width
	}
	if r.Y+r.Height >= v.surface.Frame.Height {
		bottom = v.height
	}
	return ui.Rect{X: left, Y: top, W: right - left, H: bottom - top}
}

// above is how far down the headers of the panes starting above row r of
// a split tab push it, being taller than their frames.
func (v *View) above(r uint16) float32 {
	n := 0
	for _, t := range tops(v.surface) {
		if t < r {
			n++
		}
	}
	return float32(n) * (v.headerHeight() - v.frameHeader())
}

// rowTop is where row r of a split tab starts in the view, from which on a
// pane starting there takes its box and header.
func (v *View) rowTop(r uint16) float32 { return float32(r)*v.cellH + v.above(r) }

// line is the line between the panes on either side of a split.
func (v *View) line(sp endpoint.Split) ui.Rect {
	a := v.span(sp.Area)
	if sp.Vertical {
		return ui.Rect{X: a.X, Y: v.rowTop(sp.Pos), W: a.W, H: 1}
	}
	return ui.Rect{X: float32(sp.Pos) * v.cellW, Y: a.Y, W: 1, H: a.H}
}

// divider is the room around a split's line, which the user drags to move
// it. It stops short of the dividers its ends meet, of the splits around
// it.
func (v *View) divider(sp endpoint.Split) ui.Rect {
	const reach = 8
	l := v.line(sp)
	if sp.Vertical {
		return ui.Rect{X: l.X + reach/2, Y: l.Y - reach/2, W: l.W - reach, H: reach}
	}
	return ui.Rect{X: l.X - reach/2, Y: l.Y + reach/2, W: reach, H: l.H - reach}
}

// cardAt returns the pane whose card is at x, y in the view.
func (v *View) cardAt(x, y float32) (*paneBox, bool) {
	boxes := v.boxes()
	for i := range boxes {
		if boxes[i].card.Contains(x, y) {
			return &boxes[i], true
		}
	}
	return nil, false
}

// at returns the pane whose cells are at x, y in the view, and the cell
// there, in the pane.
func (v *View) at(x, y float32) (b *paneBox, col, row int, ok bool) {
	boxes := v.boxes()
	for i := range boxes {
		r := boxes[i].content
		if r.Contains(x, y) {
			return &boxes[i], int((x - r.X) / v.cellW), int((y - r.Y) / v.cellH), true
		}
	}
	return nil, 0, 0, false
}

// cellRect is the box in r of a surface cell, in the pane that holds it.
func (v *View) cellRect(r ui.Rect, x, y int) ui.Rect {
	for _, b := range v.boxes() {
		in := b.pane.Inner
		if x >= int(in.X) && x < int(in.X+in.Width) && y >= int(in.Y) && y < int(in.Y+in.Height) {
			return ui.Rect{
				X: r.X + b.content.X + float32(x-int(in.X))*v.cellW, Y: r.Y + b.content.Y + float32(y-int(in.Y))*v.cellH,
				W: v.cellW, H: v.cellH,
			}
		}
	}
	ox, oy := v.origin(v.surface)
	return ui.Rect{X: r.X + ox + float32(x)*v.cellW, Y: r.Y + oy + float32(y)*v.cellH, W: v.cellW, H: v.cellH}
}
