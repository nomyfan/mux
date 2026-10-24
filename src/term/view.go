// Package term shows a herdr surface: it paints the cells the server laid
// out for the client's focused tab, and turns the user's keys, text and
// wheel into the endpoint's input. The server runs the terminals; nothing
// here parses a program's output.
package term

import (
	"encoding/json"
	"fmt"
	"math"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"

	"mux/src/herdr/endpoint"
)

// Endpoint is where the view sends input, the size it fits and its colors.
type Endpoint interface {
	Input(paneID string, events ...endpoint.InputEvent) error
	Resize(endpoint.Size) error
	SetHostTheme(endpoint.HostTheme) error
}

// View is the state of a surface's view across frames: its font and the
// glyphs it shaped, and what it last asked of the server.
type View struct {
	font         ui.Font
	cellW, cellH float32
	width        float32 // the view's size, as last laid out
	height       float32
	baseline     float32
	glyphs       map[glyphKey][]ui.Glyph
	light, dark  *palette
	pal          *palette
	handle       ui.Handle
	asked        endpoint.Size
	toldTheme    *palette // the colors the server was told
	preedit      string
	scroll       float32 // wheel motion short of a row
	surface      *endpoint.Surface
	ep           Endpoint
	hooks        Hooks
	drag         drag
	sel          *selection
	selecting    bool    // while the press making sel lasts
	pointerX     float32 // where the press making sel is, in the view
	pointerY     float32
	searches     map[string]*search // by pane
	titles       map[string]*ui.Handle
	titleBoxes   map[string]ui.Rect // where titles are in the view
	paneDrag     *paneDrag
	accent       ui.Color
	scrolls      map[string]*scrolling
	refocus      bool // to give the keyboard back to the view
	services     ui.Services
	focused      bool
}

type glyphKey struct {
	symbol string
	style  uint16 // bold and italic
}

// maxGlyphs bounds the cache, which output of many distinct symbols would
// otherwise grow without end.
const maxGlyphs = 4096

func New(font ui.Font) *View {
	v := &View{font: font, glyphs: map[glyphKey][]ui.Glyph{}, searches: map[string]*search{}, scrolls: map[string]*scrolling{},
		titles: map[string]*ui.Handle{}, titleBoxes: map[string]ui.Rect{},
		light: newPalette(terminal.LightTheme(), false), dark: newPalette(terminal.DarkTheme(), true)}
	m := font.Metrics()
	v.cellW = ui.Shape("M", font)[0].Advance
	v.cellH = float32(math.Ceil(float64(m.Ascent + m.Descent + m.LineGap)))
	v.baseline = float32(math.Round(float64(m.Ascent + m.LineGap/2)))
	return v
}

// SetThemes colors the terminals in light, or in dark as the app's theme
// is dark.
func (v *View) SetThemes(light, dark *terminal.Theme) {
	v.light, v.dark = newPalette(light, false), newPalette(dark, true)
}

// Reconnected forgets the size and colors the view gave a connection that
// ended, so that it gives the next one its own.
func (v *View) Reconnected() { v.asked, v.toldTheme = endpoint.Size{}, nil }

// Hooks are what the view asks of the app, for the user.
type Hooks struct {
	// Header builds a pane's header, in a row of the given box, which the
	// pointer may be over.
	Header func(c *ui.Context, p endpoint.SurfacePane, box ui.Rect, hovered bool)
	// Focus asks for the focus of a pane the user clicks.
	Focus func(paneID string)
	// Resize asks to move the split at path in the tab of a pane, to
	// ratio of its area.
	Resize func(paneID string, path []bool, ratio float64)
	// Request asks the server for method; done, if any, gets the result
	// of the response, or why there is none.
	Request func(method string, params any, done func(result json.RawMessage, err error))
	// Title builds a pane's title, as a pane the user drags shows it.
	Title func(c *ui.Context, p endpoint.SurfacePane)
	// Rearrange asks to put a pane the user dropped on another at side of
	// it.
	Rearrange func(paneID, targetID string, side Side)
}

// drag is a split's divider the user drags.
type drag struct {
	path  string  // the split's, as fmt prints it
	from  int     // its position as the drag began, in cells
	moved float32 // how far the pointer went since, in DIPs
	to    int     // the position last asked for
}

// Build shows s, sending input and the size the view fits to ep, which is
// nil while the client is not connected.
func (v *View) Build(c *ui.Context, s *endpoint.Surface, ep Endpoint, hooks Hooks) ui.Element {
	v.surface, v.ep, v.services, v.hooks = s, ep, c.Services(), hooks
	v.pal = v.light
	if c.Theme().Dark {
		v.pal = v.dark
	}
	// The view draws the terminal's background itself, on its panes'
	// cards: the room around them shows whatever is behind the view.
	e := ui.Box(c).Focusable().FocusRing(false).Clip().Bind(&v.handle).Label("Terminal")
	v.focused = e.Focused()
	if ep != nil && v.toldTheme != v.pal && ep.SetHostTheme(v.pal.hostTheme()) == nil {
		v.toldTheme = v.pal
	}
	b := v.handle.Bounds(c)
	v.width, v.height = b.W, b.H
	v.accent = c.Theme().Accent
	clear(v.titleBoxes)
	if s != nil {
		for _, p := range s.Panes {
			if h := v.titles[p.ID]; h != nil {
				r := h.Bounds(c)
				r.X, r.Y = r.X-b.X, r.Y-b.Y
				v.titleBoxes[p.ID] = r
			}
		}
	}
	if v.paneDrag != nil && v.paneDrag.moving {
		e.Cursor(ui.CursorMove)
	}
	if ep != nil && b.W > 0 && b.H > 0 {
		cols, rows := v.fit(s, b.W, b.H)
		size := endpoint.Size{
			Cols: uint16(max(1, cols)), Rows: uint16(max(1, rows)),
			CellWidth: uint32(math.Round(float64(v.cellW))), CellHeight: uint32(v.cellH),
		}
		if size != v.asked && ep.Resize(size) == nil {
			v.asked = size
		}
	}
	if cur := v.cursor(); cur != nil {
		e.TextCaret(v.cellRect(ui.Rect{}, int(cur.X), int(cur.Y)))
	}
	if v.selecting {
		// The text under the pointer moves as the pane scrolls.
		v.selectAt()
	}
	if v.refocus {
		v.refocus = false
		v.handle.Focus(c)
	}
	e.HandleInput(v.input)
	e.OnShortcut(ui.Cmd, ui.KeyF, func() { v.openSearch(c) })
	e.Draw(v.paint)
	boxes := v.boxes()
	e.Children(func() {
		for _, b := range boxes {
			r := b.content
			ui.Box(c.Key(b.pane.ID+" cells")).Absolute().Left(r.X).Top(r.Y).Size(r.W, r.H).Cursor(ui.CursorText)
			h := ui.Row(c.Key(b.pane.ID)).Absolute().Left(b.header.X).Top(b.header.Y).Size(b.header.W, b.header.H).AlignItems(ui.Center)
			if b.pane.Rect.Y == 0 {
				// Along the top of the window, as its title bar. Nothing else
				// on the header may take the press, or the window would not
				// move: the view focuses the pane as the press goes by.
				h.DragWindow()
			}
			hovered := h.Hovered()
			h.Children(func() { hooks.Header(c, b.pane, b.header, hovered) })
		}
		if s != nil {
			for _, sp := range s.Splits {
				v.divide(c, sp)
			}
		}
		v.dragChip(c)
		for _, b := range boxes {
			if s := v.searches[b.pane.ID]; s != nil && s.open {
				// The matches the screen shows must be where they show.
				if fresh, sure := v.verify(b.pane, s); sure && !fresh {
					v.resync(b.pane.ID)
				}
				v.searchBar(c, b, s)
			}
		}
	})
	return e
}

// minCells is how many cells a split keeps on either side as the user
// drags it.
const minCells = 4

// divide lets the user drag a split's divider, asking for the split to
// move each time it passes a cell, and once more as the drag ends, as the
// server may have been busy with the last.
func (v *View) divide(c *ui.Context, sp endpoint.Split) {
	key := fmt.Sprint(sp.Path)
	r := v.divider(sp)
	cursor, cell, start, size := ui.CursorResizeEW, v.cellW, int(sp.Area.X), int(sp.Area.Width)
	if sp.Vertical {
		cursor, cell, start, size = ui.CursorResizeNS, v.cellH, int(sp.Area.Y), int(sp.Area.Height)
	}
	d := ui.Box(c.Key("divider "+key)).Absolute().Left(r.X).Top(r.Y).Size(r.W, r.H).Cursor(cursor)
	move := func(pos int) {
		v.hooks.Resize(v.surface.Panes[0].ID, sp.Path, float64(pos-start)/float64(size))
	}
	dx, dy, ok := d.Dragged()
	if !ok {
		if v.drag.path == key {
			if v.drag.to != v.drag.from {
				move(v.drag.to)
			}
			v.drag = drag{}
		}
		return
	}
	if v.drag.path != key {
		v.drag = drag{path: key, from: int(sp.Pos), to: int(sp.Pos)}
	}
	if sp.Vertical {
		v.drag.moved += dy
	} else {
		v.drag.moved += dx
	}
	pos := v.drag.from + int(math.Round(float64(v.drag.moved/cell)))
	pos = min(max(pos, start+minCells), start+size-minCells)
	if pos != v.drag.to {
		v.drag.to = pos
		move(pos)
	}
}

func (v *View) cursor() *endpoint.Cursor {
	if v.surface == nil || v.surface.Frame.Cursor == nil || !v.surface.Frame.Cursor.Visible {
		return nil
	}
	return v.surface.Frame.Cursor
}

// send sends events to the pane that has the focus in the surface, which
// ends the selection: the text moves from under it.
func (v *View) send(events ...endpoint.InputEvent) {
	if v.ep == nil || v.surface == nil {
		return
	}
	v.sel, v.selecting = nil, false
	for _, p := range v.surface.Panes {
		if p.Focused {
			v.ep.Input(p.ID, events...)
			return
		}
	}
}

func (v *View) input(ev ui.InputEvent) bool {
	switch ev.Kind {
	case ui.InputKeyDown, ui.InputText, ui.InputCompose, ui.InputCommand:
		// The keyboard is the search field's while it has the focus.
		if !v.focused {
			return false
		}
	}
	switch ev.Kind {
	case ui.InputKeyDown:
		if k, ok := keyEvent(ev.Key, ev.Mods); ok {
			v.send(k)
			return true
		}
		// The text a key types comes next, unless it is the app's.
		return ev.Mods&ui.Super == 0 && keyRunes[ev.Key] != 0
	case ui.InputText:
		v.preedit = ""
		v.send(endpoint.Text(ev.Text))
		return true
	case ui.InputCompose:
		v.preedit = ev.Text
		return true
	case ui.InputCommand:
		switch ev.Text {
		case "paste":
			if text := v.services.ReadClipboard(); text != "" {
				v.send(endpoint.Paste(text))
			}
			return true
		case "copy":
			return v.copy()
		}
	case ui.InputPointerDown:
		// A click in another pane's card, its header's buttons and window
		// handle among it, focuses the pane, and the view takes the
		// keyboard focus as it would.
		if b, ok := v.cardAt(ev.X, ev.Y); ok && !b.pane.Focused {
			v.hooks.Focus(b.pane.ID)
		}
		if v.overSearch(ev.X, ev.Y) {
			return false
		}
		if v.dragInput(ev) {
			return true
		}
		// A press on a pane's cells begins a selection there, by words
		// or lines as it clicks twice or more.
		if b, _, _, ok := v.at(ev.X, ev.Y); ok && ev.Button == 0 {
			v.sel = &selection{pane: b.pane.ID, unit: min(max(ev.Clicks, 1), 3)}
			v.pointerX, v.pointerY = ev.X, ev.Y
			v.selectAt()
			v.sel.anchor = v.sel.head
			v.selecting = true
			return true
		}
	case ui.InputPointerMove:
		if v.dragInput(ev) {
			return true
		}
		if v.selecting {
			v.pointerX, v.pointerY = ev.X, ev.Y
			v.selectAt()
			return true
		}
	case ui.InputPointerUp:
		if v.selecting || v.paneDrag != nil {
			v.pointerX, v.pointerY = ev.X, ev.Y
			v.released()
			return true
		}
	case ui.InputScroll:
		return v.wheel(ev)
	}
	return false
}

// released ends the selection or the drag of a pane the press made, as the
// pointer let go at pointerX, pointerY.
func (v *View) released() {
	if v.selecting {
		v.selecting = false
		if _, _, ok := v.sel.span(); !ok {
			v.sel = nil
		}
	}
	if d := v.paneDrag; d != nil {
		if target, side, _, ok := v.dropAt(v.pointerX, v.pointerY); ok && d.moving {
			v.hooks.Rearrange(d.pane, target, side)
		}
		v.paneDrag = nil
	}
}

// selectAt moves the head of the selection the user is making to the cell
// under the pointer, the nearest of the pane's as it may leave them, which
// the text under it may have scrolled to since the pointer moved.
func (v *View) selectAt() {
	for _, b := range v.boxes() {
		if b.pane.ID != v.sel.pane {
			continue
		}
		in, r := b.pane.Inner, b.content
		col := min(max(int((v.pointerX-r.X)/v.cellW), 0), int(in.Width)-1)
		row := min(max(int((v.pointerY-r.Y)/v.cellH), 0), int(in.Height)-1)
		v.sel.head = extent(&v.surface.Frame, b.pane, int(in.X)+col, int(in.Y)+row, v.sel.unit)
	}
}

// selection returns the selection with the pane it is in, if it is still
// in the surface.
func (v *View) selection() (*selection, endpoint.SurfacePane, bool) {
	if v.sel != nil && v.surface != nil {
		for _, p := range v.surface.Panes {
			if p.ID == v.sel.pane {
				return v.sel, p, true
			}
		}
	}
	return nil, endpoint.SurfacePane{}, false
}

// copy copies the selected text, which the server reads where the screen
// does not show all of it, and reports whether there is any.
func (v *View) copy() bool {
	s, p, ok := v.selection()
	if !ok {
		return false
	}
	from, to, ok := s.span()
	if !ok {
		return false
	}
	if text, ok := screenText(&v.surface.Frame, p, from, to); ok {
		v.services.WriteClipboard(text)
		return true
	}
	params := map[string]any{"pane_id": p.ID, "anchor": from, "cursor": to}
	v.hooks.Request("pane.selection.read", params, func(result json.RawMessage, err error) {
		var r struct{ Text string }
		if err == nil && json.Unmarshal(result, &r) == nil {
			v.services.WriteClipboard(r.Text)
		}
	})
	return true
}

// wheel scrolls the pane under the pointer by whole rows: its scrollback,
// or its program when the program takes the mouse.
func (v *View) wheel(ev ui.InputEvent) bool {
	b, col, row, ok := v.at(ev.X, ev.Y)
	if v.ep == nil || !ok {
		return false
	}
	v.scroll += ev.DY
	rows := int(v.scroll / v.cellH)
	if rows == 0 {
		return true
	}
	v.scroll -= float32(rows) * v.cellH
	kind := endpoint.ScrollDown
	if rows < 0 {
		kind, rows = endpoint.ScrollUp, -rows
	}
	v.ep.Input(b.pane.ID, endpoint.Mouse{Kind: kind, Column: uint16(col), Row: uint16(row), Lines: uint16(rows)})
	return true
}
