package endpoint

import "encoding/json"

// The types below follow herdr's src/protocol/wire.rs (Apache-2.0), whose
// field and variant order is the wire format. Fields this client has no use
// for are read and dropped.

// Client message variants.
const (
	clientDetach          = 4
	clientShellResize     = 12
	clientShellPaneInput  = 13
	clientShellRequest    = 15
	clientShellHostTheme  = 17
	clientShellFocus      = 18
	clientEndpointControl = 20
)

// Server message variants.
const (
	serverShutdown         = 3
	serverClipboard        = 5
	serverPaneSurface      = 13
	serverShellError       = 15
	serverResponseChunk    = 18
	serverPaneSurfacePatch = 19
	serverEndpointControl  = 20
)

// snapshotKind is the Control that carries a Snapshot, as JSON.
const snapshotKind = "shell.snapshot.v1"

// Snapshot is the session as the client shows it: what its own view
// focuses, and the workspaces, tabs, panes and agents. Statuses are idle,
// working, blocked, done or unknown.
type Snapshot struct {
	BootID           string      `json:"boot_id"`
	Revision         uint64      `json:"revision"`
	FocusedWorkspace string      `json:"focused_workspace_id"`
	FocusedTab       string      `json:"focused_tab_id"`
	FocusedPane      string      `json:"focused_pane_id"`
	Workspaces       []Workspace `json:"workspaces"`
	Tabs             []Tab       `json:"tabs"`
	Panes            []Pane      `json:"panes"`
	Agents           []Agent     `json:"agents"`
}

type Workspace struct {
	ID        string `json:"workspace_id"`
	ActiveTab string `json:"active_tab_id"`
	Label     string `json:"label"`
	Branch    string `json:"branch"`
	Focused   bool   `json:"focused"`
	Status    string `json:"agent_status"`
}

type Tab struct {
	ID        string `json:"tab_id"`
	Workspace string `json:"workspace_id"`
	// Number is the tab's place in its workspace, from 1, which herdr
	// names a new tab by.
	Number  int    `json:"number"`
	Label   string `json:"label"`
	Zoomed  bool   `json:"zoomed"`
	Focused bool   `json:"focused"`
	Status  string `json:"agent_status"`
}

type Pane struct {
	ID        string `json:"pane_id"`
	Workspace string `json:"workspace_id"`
	Tab       string `json:"tab_id"`
	Label     string `json:"label"`
	Cwd       string `json:"cwd"`
	Focused   bool   `json:"focused"`
}

type Agent struct {
	Pane      string `json:"pane_id"`
	Workspace string `json:"workspace_id"`
	Tab       string `json:"tab_id"`
	// Agent is the kind herdr recognizes, as claude or codex.
	Agent   string `json:"agent"`
	Name    string `json:"name"`
	Title   string `json:"terminal_title_stripped"`
	Status  string `json:"agent_status"`
	Focused bool   `json:"focused"`
}

// Surface is the client's view of its focused tab, laid out by the server:
// a grid of cells as wide and high as the client asked for, holding every
// pane of the tab and the lines between them.
type Surface struct {
	BootID             string
	ProjectionRevision uint64
	SurfaceRevision    uint64
	Frame              Frame
	Panes              []SurfacePane
	Splits             []Split
	Popup              *Popup
}

// Frame is a grid of cells, row by row.
type Frame struct {
	Cells         []Cell
	Width, Height uint16
	Cursor        *Cursor
	Hyperlinks    []string
}

// Cell is one cell of a frame. Colors are 0 for the default, 1..16 for the
// 16 named colors, 0x010000XX for an indexed one and 0x02RRGGBB for RGB.
type Cell struct {
	Symbol   string
	FG, BG   uint32
	Modifier uint16
	// Skip marks the cell a wide character before it covers.
	Skip bool
	// Hyperlink indexes the frame's Hyperlinks, -1 for none.
	Hyperlink int32
}

type Cursor struct {
	X, Y    uint16
	Visible bool
	Shape   uint8
}

type Rect struct{ X, Y, Width, Height uint16 }

type SurfacePane struct {
	ID string
	// ContentRevision changes as the pane's text does, which requests on
	// its text name to read the text they meant.
	ContentRevision uint64
	// Rect is the pane with its frame; Inner the terminal within it.
	Rect, Inner Rect
	// Scroll is how far the pane is scrolled into its history, if it has
	// one, as the alternate screen does not.
	Scroll  *Scroll
	Focused bool
	// MouseReporting is whether the pane's program takes the mouse.
	MouseReporting bool
	AltScreen      bool
}

// Scroll is how far a pane shows back in its history, in rows.
type Scroll struct {
	// Offset is how far above the bottom the screen shows, at most Max.
	Offset, Max uint64
	Rows        uint64 // the screen's
}

type Split struct {
	Vertical bool
	Pos      uint16
	Area     Rect
	HitRect  Rect
	// Path leads from the tab's root split to this one: false takes a
	// split's first side, true its second.
	Path []bool
}

type Popup struct {
	TerminalID, Title string
	Frame             Frame
}

// SurfacePatch changes the rows and cursor of the surface whose revision is
// BaseRevision, making it Revision.
type SurfacePatch struct {
	BootID             string
	ProjectionRevision uint64
	BaseRevision       uint64
	Revision           uint64
	Rows               []PatchRow
	Panes              []SurfacePane
	Cursor             *Cursor
}

type PatchRow struct {
	X, Y  uint16
	Cells []Cell
}

// Control is a message of the endpoint itself, its data JSON.
type Control struct{ Kind, Data string }

type Shutdown struct{ Reason string }

// Clipboard asks the client to put text on the clipboard, as a copy in a
// pane does.
type Clipboard struct{ Text string }

type ShellError struct{ Message string }

// ResponseChunk is part of the response to a request; the chunks of one
// joined make its JSON.
type ResponseChunk struct {
	RequestID string
	Final     bool
	Data      []byte
}

// Unknown is a message this client does not read.
type Unknown struct{ Variant uint32 }

// decodeServerMessage decodes a message's payload into one of the types
// above.
func decodeServerMessage(payload []byte) (any, error) {
	d := &decoder{buf: payload}
	var msg any
	switch v := d.variant(); v {
	case serverShutdown:
		reason := d.optString()
		msg = Shutdown{Reason: deref(reason)}
	case serverClipboard:
		msg = Clipboard{Text: d.string()}
	case serverPaneSurface:
		msg = d.surface()
	case serverShellError:
		msg = ShellError{Message: d.string()}
	case serverResponseChunk:
		d.string() // boot ID
		msg = ResponseChunk{RequestID: d.string(), Final: d.bool(), Data: d.bytes()}
	case serverPaneSurfacePatch:
		msg = d.patch()
	case serverEndpointControl:
		ctl := Control{Kind: d.string(), Data: d.string()}
		msg = ctl
		if d.err == nil && ctl.Kind == snapshotKind {
			snap := &Snapshot{}
			if err := json.Unmarshal([]byte(ctl.Data), snap); err != nil {
				return nil, err
			}
			msg = snap
		}
	default:
		return Unknown{Variant: v}, nil
	}
	if d.err == nil && len(d.buf) > 0 {
		d.fail() // trailing bytes
	}
	return msg, d.err
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (d *decoder) cells() []Cell {
	n := d.len()
	cells := make([]Cell, 0, n)
	for range n {
		c := Cell{Symbol: d.string(), FG: d.u32(), BG: d.u32(), Modifier: d.u16(), Skip: d.bool(), Hyperlink: -1}
		if link := d.optU32(); link != nil {
			c.Hyperlink = int32(*link)
		}
		cells = append(cells, c)
	}
	return cells
}

func (d *decoder) cursor() *Cursor {
	if !d.some() {
		return nil
	}
	return &Cursor{X: d.u16(), Y: d.u16(), Visible: d.bool(), Shape: d.u8()}
}

func (d *decoder) frame() Frame {
	f := Frame{Cells: d.cells(), Width: d.u16(), Height: d.u16(), Cursor: d.cursor()}
	for range d.len() {
		f.Hyperlinks = append(f.Hyperlinks, d.string())
	}
	d.bytes() // graphics
	if len(f.Cells) != int(f.Width)*int(f.Height) {
		d.fail()
	}
	return f
}

func (d *decoder) rect() Rect { return Rect{d.u16(), d.u16(), d.u16(), d.u16()} }

func (d *decoder) surfacePanes() []SurfacePane {
	n := d.len()
	panes := make([]SurfacePane, 0, n)
	for range n {
		p := SurfacePane{ID: d.string(), ContentRevision: d.u64()}
		p.Rect, p.Inner = d.rect(), d.rect()
		if d.some() { // scrollbar_rect
			d.rect()
		}
		if d.some() {
			p.Scroll = &Scroll{Offset: d.u64(), Max: d.u64(), Rows: d.u64()}
		}
		p.Focused, p.MouseReporting = d.bool(), d.bool()
		d.bool() // sgr_pixel_mouse
		p.AltScreen = d.bool()
		d.u32() // pixel_width
		d.u32() // pixel_height
		panes = append(panes, p)
	}
	return panes
}

func (d *decoder) surface() *Surface {
	s := &Surface{BootID: d.string(), ProjectionRevision: d.u64(), SurfaceRevision: d.u64(), Frame: d.frame(), Panes: d.surfacePanes()}
	for range d.len() {
		sp := Split{Vertical: d.variant() == 1, Pos: d.u16(), Area: d.rect(), HitRect: d.rect()}
		for range d.len() {
			sp.Path = append(sp.Path, d.bool())
		}
		s.Splits = append(s.Splits, sp)
	}
	if d.some() {
		p := &Popup{TerminalID: d.string(), Title: d.string()}
		for range 2 { // width, height
			if d.some() {
				if d.variant() == 0 {
					d.u16() // cells
				} else {
					d.u8() // percent
				}
			}
		}
		p.Frame = d.frame()
		d.bool() // mouse_reporting
		d.bool() // sgr_pixel_mouse
		d.u32()  // pixel_width
		d.u32()  // pixel_height
		s.Popup = p
	}
	d.graphicsScene()
	return s
}

// graphicsScene reads the images a surface places, which this client does
// not show.
func (d *decoder) graphicsScene() {
	for range d.len() { // assets
		d.graphicsAssetKey()
		d.bytes()
	}
	for range d.len() { // placements
		d.graphicsAssetKey()
		d.u32()       // logical_placement_id
		d.u16()       // x
		d.u16()       // y
		for range 8 { // cols, rows, source_x, source_y, source_width, source_height, x_offset, y_offset
			d.u32()
		}
		d.i32() // z
		d.u32() // scrollback_offset
	}
	for range d.len() { // retained_assets
		d.graphicsAssetKey()
	}
}

func (d *decoder) graphicsAssetKey() {
	switch d.variant() { // source
	case 0: // Terminal
		d.variant() // target: Pane or Popup, each with an ID
		d.string()
		d.u32() // image_id
	case 1: // PaneLayer
		d.string()
		d.string()
	default:
		d.fail()
	}
	d.u32()     // image_width
	d.u32()     // image_height
	d.variant() // format
	d.u64()     // data_len
	d.u64()     // data_fingerprint
}

func (d *decoder) patch() *SurfacePatch {
	p := &SurfacePatch{BootID: d.string(), ProjectionRevision: d.u64(), BaseRevision: d.u64(), Revision: d.u64()}
	for range d.len() {
		p.Rows = append(p.Rows, PatchRow{X: d.u16(), Y: d.u16(), Cells: d.cells()})
	}
	p.Panes = d.surfacePanes()
	p.Cursor = d.cursor()
	return p
}
