package term

import (
	"github.com/egoist/mygo/ui"
)

// Side is where a pane dropped on another goes: in its place, the other
// taking the dropped one's, or beside it, the two sharing its box.
type Side int

const (
	SideCenter Side = iota
	SideLeft
	SideRight
	SideTop
	SideBottom
)

// edge is how close to a pane's side, as a share of its box, a pane
// dropped on it goes beside it there, rather than in its place.
const edge = 0.25

// paneDrag is a pane the user drags by its title.
type paneDrag struct {
	pane         string
	fromX, fromY float32 // where the press began, in the view
	x, y         float32 // where the pointer is
	moving       bool    // since the pointer went far enough from the press
}

// TitleHandle returns the handle of the title of a pane's header, which
// the user drags the pane by: the header binds its title to it.
func (v *View) TitleHandle(paneID string) *ui.Handle {
	h := v.titles[paneID]
	if h == nil {
		h = new(ui.Handle)
		v.titles[paneID] = h
	}
	return h
}

// titleAt returns the pane whose title is at x, y in the view.
func (v *View) titleAt(x, y float32) (string, bool) {
	for id, r := range v.titleBoxes {
		if r.Contains(x, y) {
			return id, true
		}
	}
	return "", false
}

// dropAt returns the pane a pane dropped at x, y in the view goes to, the
// side of it, and the part of its box that shows where; ok is false over
// no pane but the dragged one.
func (v *View) dropAt(x, y float32) (target string, side Side, box ui.Rect, ok bool) {
	b, found := v.cardAt(x, y)
	if !found || v.paneDrag == nil || b.pane.ID == v.paneDrag.pane {
		return "", 0, ui.Rect{}, false
	}
	c := b.card
	fx, fy := (x-c.X)/c.W, (y-c.Y)/c.H
	side, near := SideCenter, float32(edge)
	for _, s := range []struct {
		side Side
		d    float32
	}{{SideLeft, fx}, {SideRight, 1 - fx}, {SideTop, fy}, {SideBottom, 1 - fy}} {
		if s.d < near {
			side, near = s.side, s.d
		}
	}
	switch side {
	case SideLeft:
		c.W /= 2
	case SideRight:
		c.X, c.W = c.X+c.W/2, c.W/2
	case SideTop:
		c.H /= 2
	case SideBottom:
		c.Y, c.H = c.Y+c.H/2, c.H/2
	}
	return b.pane.ID, side, c, true
}

// dragInput takes the press on a pane's title, and the pointer's moves
// while the user drags the pane by it, reporting whether it took the
// event; the release ends the drag (released).
func (v *View) dragInput(ev ui.InputEvent) bool {
	switch ev.Kind {
	case ui.InputPointerDown:
		if id, ok := v.titleAt(ev.X, ev.Y); ok && ev.Button == 0 {
			v.paneDrag = &paneDrag{pane: id, fromX: ev.X, fromY: ev.Y, x: ev.X, y: ev.Y}
			return true
		}
	case ui.InputPointerMove:
		if d := v.paneDrag; d != nil {
			const far = 4
			d.x, d.y = ev.X, ev.Y
			v.pointerX, v.pointerY = ev.X, ev.Y
			if dx, dy := d.x-d.fromX, d.y-d.fromY; dx*dx+dy*dy > far*far {
				d.moving = true
			}
			return true
		}
	}
	return false
}

// paintDrop shows where the pane the user drags goes, if dropped.
func (v *View) paintDrop(p *ui.Painter, r ui.Rect) {
	d := v.paneDrag
	if d == nil || !d.moving {
		return
	}
	if _, _, box, ok := v.dropAt(d.x, d.y); ok {
		box.X, box.Y = box.X+r.X, box.Y+r.Y
		p.Fill(box, v.accent.Alpha(0.25), 0)
		p.Stroke(box, v.accent, 0, 2)
	}
}

// dragChip shows the title of the pane the user drags beside the pointer.
func (v *View) dragChip(c *ui.Context) {
	d := v.paneDrag
	if d == nil || !d.moving {
		return
	}
	p, ok := v.pane(d.pane)
	if !ok {
		return
	}
	t := c.Theme()
	ui.Row(c.Key("drag chip")).Absolute().Left(d.x+12).Top(d.y+12).AlignItems(ui.Center).Gap(8).Padding(4, 10).
		Background(t.Surface).Radius(8).Border(1, t.Border).Shadow(0, 4, 12, 0, ui.RGB(0, 0, 0).Alpha(0.2)).
		Children(func() { v.hooks.Title(c, p) })
}
