// Package term shows a tab's panes, each a terminal of the core in
// MyGo's terminal plugin (libghostty-vt), which reads the bytes the core
// relays from the program and encodes what is typed. Where the panes go
// is the layout's: their headers, the dividers between them, dragging
// them about and moving the focus among them are this package's.
package term

import (
	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"

	"mux/internal/layout"
)

// HeaderHeight is the height of a pane's header.
const HeaderHeight = 40

// View is the state of the panes' view across frames: the terminals of
// the panes, attached as they first show, and where they showed.
type View struct {
	font        terminal.Font
	light, dark *terminal.Theme
	panes       map[string]*pane
	err         string // why a terminal could not be made
	handle      ui.Handle
	hooks       Hooks
	tab         *layout.Tab
	shown       string // the tab last built
	att         Attacher
	boxes       []paneBox
	dividers    []layout.Divider
	width       float32
	height      float32
	grow        float32 // how much wider it is to settle, as Settling says
	grew        float32 // grow as of the last layout
	// focused is the pane whose terminal had the keyboard as of the last
	// frame; wanted the one the tab focused then. refocus gives the
	// keyboard to the tab's focused pane as the view builds next.
	focused  string
	wanted   string
	refocus  bool
	drag     drag
	paneDrag *paneDrag
	titles   map[string]ui.Rect // where the panes' titles are in the view
	pointerX float32
	pointerY float32
	accent   ui.Color
}

// pane is a pane's terminal, and its view's state.
type pane struct {
	terminal string
	term     *terminal.Terminal
	conn     *conn
	view     ui.Handle // its terminal's view
	title    ui.Handle // its header's title, which drags it
	search   search
}

// paneBox is where a pane shows: its card, its header along the card's
// top, and its terminal under it, in the view.
type paneBox struct {
	terminal string
	card     ui.Rect
	header   ui.Rect
	content  ui.Rect
}

// Hooks are what the view asks of the app.
type Hooks struct {
	// Header builds a pane's header, in a row of the given box, which the
	// pointer may be over; focused while its pane has the focus.
	Header func(c *ui.Context, terminal string, box ui.Rect, hovered, focused bool)
	// Title builds a pane's title, as a pane the user drags shows it.
	Title func(c *ui.Context, terminal string)
	// Changed tells the app the tab changed: the focus, a split's ratio,
	// where the panes are.
	Changed func()
}

// New returns a view of terminals in font, colored in light, or in dark
// as the window is.
func New(font terminal.Font, light, dark *terminal.Theme) *View {
	return &View{font: font, light: light, dark: dark, panes: map[string]*pane{}, titles: map[string]ui.Rect{}, refocus: true}
}

// SetFont changes the terminals' font; their size in cells follows.
func (v *View) SetFont(font terminal.Font) {
	v.font = font
	for _, p := range v.panes {
		p.term.SetFont(font)
	}
}

// Font returns the terminals' font.
func (v *View) Font() terminal.Font { return v.font }

// SetThemes changes the terminals' colors.
func (v *View) SetThemes(light, dark *terminal.Theme) {
	v.light, v.dark = light, dark
	for _, p := range v.panes {
		p.term.SetTheme(light, dark)
	}
}

// Focus gives the keyboard to the tab's focused pane as the view builds
// next.
func (v *View) Focus() { v.refocus = true }

// Settling tells the view it is to grow by dw in width as a transition
// around it, such as the sidebar sliding, settles: the panes take the
// width they settle at at once, rather than at every step, at which each
// program would draw itself again.
func (v *View) Settling(dw float32) { v.grow = dw }

// HeaderCenter is how far below the view's top the headers along it center
// their contents.
func (v *View) HeaderCenter() float32 { return HeaderHeight / 2 }

// Drop closes the terminal of a pane, if any, which attaches again as it
// shows next: as its program restarted.
func (v *View) Drop(terminal string) {
	if p := v.panes[terminal]; p != nil {
		p.term.Close()
		delete(v.panes, terminal)
	}
}

// Retain closes the terminals of the panes not among terminals.
func (v *View) Retain(terminals []string) {
	keep := map[string]bool{}
	for _, t := range terminals {
		keep[t] = true
	}
	for t := range v.panes {
		if !keep[t] {
			v.Drop(t)
		}
	}
}

// Reset closes every pane's terminal, as the connection they were
// attached by is gone.
func (v *View) Reset() {
	for t := range v.panes {
		v.Drop(t)
	}
	v.err = ""
}

// Focused returns the terminal of the pane whose terminal has the
// keyboard, "" if none.
func (v *View) Focused() string { return v.focused }

// Terminal returns the terminal of a pane shown, nil if none.
func (v *View) Terminal(terminal string) *terminal.Terminal {
	if p := v.panes[terminal]; p != nil {
		return p.term
	}
	return nil
}

// paneOf returns the pane of a terminal, made as it first shows.
func (v *View) paneOf(addr string) *pane {
	if p := v.panes[addr]; p != nil {
		return p
	}
	if v.att == nil {
		return nil
	}
	p := &pane{terminal: addr, conn: newConn(v.att, addr)}
	term, err := terminal.New(terminal.Options{Conn: p.conn, Font: v.font, Theme: v.light, DarkTheme: v.dark,
		OnSearch: func(s terminal.SearchStatus) { p.search.status = s }})
	if err != nil {
		v.err = err.Error()
		return nil
	}
	p.term = term
	v.panes[addr] = p
	return p
}

// Build shows a tab's panes, attaching their terminals with att, nil
// while the app has no connection.
func (v *View) Build(c *ui.Context, tab *layout.Tab, att Attacher, hooks Hooks) ui.Element {
	v.tab, v.att, v.hooks = tab, att, hooks
	t := c.Theme()
	v.accent = t.Accent
	bg := v.light.Background
	if t.Dark {
		bg = v.dark.Background
	}
	e := ui.Box(c).Clip().Bind(&v.handle).Label("Panes")
	b := v.handle.Bounds(c)
	v.width, v.height = b.W, b.H
	// The bounds are the last layout's, which the growth then goes with.
	v.layOut(b.W+v.grew, b.H)
	v.grew = v.grow
	if tab != nil && tab.ID != v.shown {
		v.shown, v.refocus = tab.ID, true
	}
	v.trackFocus(c)
	v.findTitles(c)
	if v.paneDrag != nil && v.paneDrag.moving {
		e.Cursor(ui.CursorMove)
	}
	e.HandleInput(v.input)
	e.OnShortcut(ui.Cmd, ui.KeyF, func() { v.openSearch(c) })
	for _, a := range arrows {
		e.OnShortcut(ui.Cmd, a.key, func() { v.moveFocus(a.direction) })
		e.OnShortcut(ui.Cmd|ui.Alt, a.key, func() { v.swap(a.direction) })
	}
	e.Draw(func(p *ui.Painter, r ui.Rect) {
		p.Fill(r, bg, 0)
		for _, d := range v.dividers {
			l := d.Line
			p.Fill(ui.Rect{X: r.X + l.X, Y: r.Y + l.Y, W: l.W, H: l.H}, t.Border, 0)
		}
		v.paintDrop(p, r)
	})
	e.Children(func() {
		if v.err != "" {
			ui.Text(c, v.err).TextColor(t.Danger).Absolute().Left(16).Top(HeaderHeight + 8)
		}
		for _, b := range v.boxes {
			v.paneView(c, b)
		}
		for i := range v.dividers {
			v.divide(c, v.dividers[i])
		}
		v.dragChip(c)
		for _, b := range v.boxes {
			if p := v.panes[b.terminal]; p != nil && p.search.open {
				v.searchBar(c, b, p)
			}
		}
	})
	return e
}

// paneView shows a pane: its header, and its terminal under it.
func (v *View) paneView(c *ui.Context, b paneBox) {
	if p := v.paneOf(b.terminal); p != nil {
		r := b.content
		terminal.View(c.Key(b.terminal), p.term).Bind(&p.view).Absolute().Left(r.X).Top(r.Y).Size(r.W, r.H)
	}
	h := ui.Row(c.Key(b.terminal + " header")).Absolute().Left(b.header.X).Top(b.header.Y).Height(b.header.H).AlignItems(ui.Center)
	// The view's width is the last frame's, behind it while it grows or
	// shrinks: a header out to its edge keeps to it.
	h.Width(max(0, min(b.header.W, v.width-b.header.X)))
	if b.header.Y == 0 {
		// Along the top of the window, as its title bar. Nothing else on
		// the header may take the press, or the window would not move:
		// the view focuses the pane as the press goes by.
		h.DragWindow()
	}
	hovered := h.Hovered()
	focused := v.tab != nil && v.tab.Focus == b.terminal
	h.Children(func() { v.hooks.Header(c, b.terminal, b.header, hovered, focused) })
}

// trackFocus follows the keyboard: a click in a pane's terminal focuses
// the pane in the tab, and the tab's focus, moved by the app, takes the
// keyboard to its pane.
func (v *View) trackFocus(c *ui.Context) {
	if v.tab == nil {
		return
	}
	actual := ""
	for _, b := range v.boxes {
		if p := v.panes[b.terminal]; p != nil && p.view.Focused(c) {
			actual = b.terminal
		}
	}
	if actual != v.focused {
		if p := v.panes[v.focused]; p != nil {
			p.conn.Focus(false)
		}
		if p := v.panes[actual]; p != nil {
			p.conn.Focus(true)
		}
		if actual != "" && actual != v.tab.Focus && v.tab.Pane(actual) != nil {
			v.tab.Focus = actual
			v.changed()
		}
		v.focused = actual
	}
	if v.refocus || v.tab.Focus != v.wanted {
		if p := v.panes[v.tab.Focus]; p != nil {
			p.view.Focus(c)
			v.refocus = false
		}
	}
	v.wanted = v.tab.Focus
}

func (v *View) changed() {
	if v.hooks.Changed != nil {
		v.hooks.Changed()
	}
}

// layOut lays the tab's panes out in w by h.
func (v *View) layOut(w, h float32) {
	v.boxes, v.dividers = nil, nil
	if v.tab == nil || w <= 0 || h <= 0 {
		return
	}
	boxes, dividers := v.tab.Boxes(layout.Rect{W: w, H: h}, 1)
	v.dividers = dividers
	for _, b := range boxes {
		r := ui.Rect(b.Rect)
		header := ui.Rect{X: r.X, Y: r.Y, W: r.W, H: min(HeaderHeight, r.H)}
		v.boxes = append(v.boxes, paneBox{terminal: b.Terminal, card: r, header: header,
			content: ui.Rect{X: r.X, Y: r.Y + header.H, W: r.W, H: max(0, r.H-header.H)}})
	}
}

// arrows are the arrow keys, by the directions they say.
var arrows = []struct {
	key       ui.Key
	direction layout.Direction
}{{ui.KeyLeft, layout.Left}, {ui.KeyRight, layout.Right}, {ui.KeyUp, layout.Up}, {ui.KeyDown, layout.Down}}

func (v *View) layoutBoxes() []layout.Box {
	out := make([]layout.Box, len(v.boxes))
	for i, b := range v.boxes {
		out[i] = layout.Box{Terminal: b.terminal, Rect: layout.Rect(b.card)}
	}
	return out
}

// moveFocus focuses the pane beside the focused one toward d, if any.
func (v *View) moveFocus(d layout.Direction) {
	if v.tab == nil {
		return
	}
	if next := layout.Neighbor(v.layoutBoxes(), v.tab.Focus, d); next != "" {
		v.tab.Focus = next
		v.changed()
	}
}

// swap swaps the focused pane with the one beside it toward d, if any.
func (v *View) swap(d layout.Direction) {
	if v.tab == nil {
		return
	}
	if next := layout.Neighbor(v.layoutBoxes(), v.tab.Focus, d); next != "" && v.tab.Swap(v.tab.Focus, next) {
		v.refocus = true
		v.changed()
	}
}

// drag is a split's divider the user drags.
type drag struct {
	split *layout.Node
	from  float64 // its ratio as the drag began
	moved float32 // how far the pointer went since, in DIPs
}

// minPane is how narrow or short a split keeps either side as the user
// drags it, in DIPs.
const minPane = 80

// divide lets the user drag a split's divider, moving the split as the
// pointer goes; the drag's end saves it.
func (v *View) divide(c *ui.Context, d layout.Divider) {
	const reach = 8
	l, cursor := d.Line, ui.CursorResizeEW
	r := ui.Rect{X: l.X - reach/2, Y: l.Y + reach/2, W: reach, H: l.H - reach}
	if d.Split.Vertical {
		cursor = ui.CursorResizeNS
		r = ui.Rect{X: l.X + reach/2, Y: l.Y - reach/2, W: l.W - reach, H: reach}
	}
	box := ui.Box(c.Key(dividerKey(d))).Absolute().Left(r.X).Top(r.Y).Size(r.W, r.H).Cursor(cursor)
	dx, dy, ok := box.Dragged()
	if !ok {
		if v.drag.split == d.Split {
			v.drag = drag{}
			v.changed()
		}
		return
	}
	if v.drag.split != d.Split {
		v.drag = drag{split: d.Split, from: d.Split.Ratio}
	}
	size, moved := d.Area.W-1, dx
	if d.Split.Vertical {
		size, moved = d.Area.H-1, dy
	}
	v.drag.moved += moved
	if size <= 0 {
		return
	}
	pos := float32(v.drag.from)*size + v.drag.moved
	pos = min(max(pos, min(minPane, size/2)), max(size-minPane, size/2))
	d.Split.Ratio = float64(pos / size)
}

// dividerKey is a divider's key: its split's place among the tab's, as
// the tree's leaves on its sides name it.
func dividerKey(d layout.Divider) string {
	return "divider " + firstLeaf(d.Split.A) + " " + firstLeaf(d.Split.B)
}

func firstLeaf(n *layout.Node) string {
	for n != nil && !n.Leaf() {
		n = n.A
	}
	if n == nil {
		return ""
	}
	return n.Terminal
}

// cardAt returns the pane whose card is at x, y in the view.
func (v *View) cardAt(x, y float32) (*paneBox, bool) {
	for i := range v.boxes {
		if v.boxes[i].card.Contains(x, y) {
			return &v.boxes[i], true
		}
	}
	return nil, false
}

func (v *View) input(ev ui.InputEvent) bool {
	switch ev.Kind {
	case ui.InputPointerDown:
		// A press on a pane's header, its buttons and window handle among
		// it, focuses the pane; a press in its terminal is the terminal's.
		if b, ok := v.cardAt(ev.X, ev.Y); ok && v.tab != nil && v.tab.Focus != b.terminal {
			v.tab.Focus = b.terminal
			v.changed()
		}
		if v.overSearch(ev.X, ev.Y) {
			return false
		}
		return v.dragInput(ev)
	case ui.InputPointerMove:
		return v.dragInput(ev)
	case ui.InputPointerUp:
		if v.paneDrag != nil {
			v.pointerX, v.pointerY = ev.X, ev.Y
			v.released()
			return true
		}
	}
	return false
}

// released ends the drag of a pane, as the pointer let go at pointerX,
// pointerY.
func (v *View) released() {
	d := v.paneDrag
	if d == nil || !d.moving || v.tab == nil {
		v.paneDrag = nil
		return
	}
	target, side, _, ok := v.dropAt(v.pointerX, v.pointerY)
	v.paneDrag = nil
	if !ok {
		return
	}
	moved := false
	switch side {
	case SideCenter:
		moved = v.tab.Swap(d.pane, target)
	case SideLeft:
		moved = v.tab.Move(d.pane, target, layout.Left)
	case SideRight:
		moved = v.tab.Move(d.pane, target, layout.Right)
	case SideTop:
		moved = v.tab.Move(d.pane, target, layout.Up)
	case SideBottom:
		moved = v.tab.Move(d.pane, target, layout.Down)
	}
	if moved {
		v.tab.Focus, v.refocus = d.pane, true
		v.changed()
	}
}
