package term

import (
	"encoding/json"
	"testing"

	"github.com/egoist/mygo/ui"

	"mux/src/herdr/endpoint"
)

// frame lays lines out as a surface does, a wide character's second cell
// blank in the default style, as in whole surfaces.
func frame(width int, lines ...string) *endpoint.Frame {
	f := &endpoint.Frame{Width: uint16(width), Height: uint16(len(lines)), Cells: make([]endpoint.Cell, width*len(lines))}
	for i := range f.Cells {
		f.Cells[i].Symbol = " "
	}
	for y, line := range lines {
		x := 0
		for _, r := range line {
			f.Cells[y*width+x].Symbol = string(r)
			x++
			if wide(string(r)) {
				x++
			}
		}
	}
	return f
}

func TestSelectionText(t *testing.T) {
	f := frame(20, "git log -1 --stat", "中文 path/to/file:12", "  last  ")
	p := endpoint.SurfacePane{Inner: endpoint.Rect{Width: 20, Height: 3}}
	for _, tc := range []struct {
		name         string
		anchor, head [2]int
		unit         int
		want         string
	}{
		{"cells", [2]int{4, 0}, [2]int{9, 0}, 1, "log -1"},
		{"backwards", [2]int{9, 0}, [2]int{4, 0}, 1, "log -1"},
		{"a click", [2]int{4, 0}, [2]int{4, 0}, 1, ""},
		{"rows, without trailing blanks", [2]int{11, 0}, [2]int{2, 2}, 1, "--stat\n中文 path/to/file:12\n  l"},
		{"word", [2]int{5, 0}, [2]int{5, 0}, 2, "log"},
		{"word of wide characters", [2]int{1, 1}, [2]int{1, 1}, 2, "中文"},
		{"word up to a break", [2]int{8, 1}, [2]int{8, 1}, 2, "path/to/file"},
		{"from a wide character's second cell", [2]int{1, 1}, [2]int{3, 1}, 1, "中文"},
		{"line", [2]int{3, 2}, [2]int{3, 2}, 3, "  last"},
	} {
		s := &selection{unit: tc.unit,
			anchor: extent(f, p, tc.anchor[0], tc.anchor[1], tc.unit),
			head:   extent(f, p, tc.head[0], tc.head[1], tc.unit)}
		got := ""
		if from, to, ok := s.span(); ok {
			got, _ = screenText(f, p, from, to)
		}
		if got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}

// hooks are a view's hooks that note the requests it makes.
func hooks(requests *[]map[string]any) Hooks {
	return Hooks{
		Header: func(*ui.Context, endpoint.SurfacePane, ui.Rect, bool) {},
		Focus:  func(string) {},
		Resize: func(string, []bool, float64) {},
		Request: func(method string, params any, done func(json.RawMessage, error)) {
			b, _ := json.Marshal(params)
			var m map[string]any
			json.Unmarshal(b, &m)
			m["method"] = method
			*requests = append(*requests, m)
		},
	}
}

// Dragging over a pane's cells selects them, which Copy copies; a click
// ends the selection.
func TestSelectCopy(t *testing.T) {
	s := &endpoint.Surface{Frame: *frame(30, "", "hello world", "second line"), Panes: []endpoint.SurfacePane{
		{ID: "p1", Focused: true, Rect: endpoint.Rect{Width: 30, Height: 3}, Inner: endpoint.Rect{Width: 30, Height: 3}},
	}}
	v := New(ui.Font{Size: 14})
	var requests []map[string]any
	tt := ui.NewTester(func(c *ui.Context) { v.Build(c, s, nil, hooks(&requests)).Fill() }, 600, 300)
	tt.Frame()
	at := func(x, y int) (float32, float32) {
		r := v.cellRect(ui.Rect{}, x, y)
		return r.X + r.W/2, r.Y + r.H/2
	}
	tt.Press(at(6, 1))
	tt.Move(at(5, 2))
	tt.Release(at(5, 2))
	tt.Command("copy")
	if got, want := tt.Clipboard(), "world\nsecond"; got != want {
		t.Errorf("copied %q, want %q", got, want)
	}
	tt.ClickAt(at(3, 1))
	if v.sel != nil {
		t.Errorf("a click left a selection: %+v", v.sel)
	}
}

// A selection stays with its text as the pane scrolls, and the server
// reads it once the screen no longer shows it all.
func TestSelectionScrolls(t *testing.T) {
	pane := endpoint.SurfacePane{ID: "p1", Focused: true, Rect: endpoint.Rect{Width: 20, Height: 3}, Inner: endpoint.Rect{Width: 20, Height: 3},
		Scroll: &endpoint.Scroll{Offset: 0, Max: 10, Rows: 3}}
	s := &endpoint.Surface{Frame: *frame(20, "eleven", "twelve", "thirteen"), Panes: []endpoint.SurfacePane{pane}}
	v := New(ui.Font{Size: 14})
	var requests []map[string]any
	tt := ui.NewTester(func(c *ui.Context) { v.Build(c, s, nil, hooks(&requests)).Fill() }, 600, 300)
	tt.Frame()
	at := func(x, y int) (float32, float32) {
		r := v.cellRect(ui.Rect{}, x, y)
		return r.X + r.W/2, r.Y + r.H/2
	}
	tt.Press(at(0, 1))
	tt.Move(at(2, 2))
	tt.Release(at(2, 2))
	if from, to, _ := v.sel.span(); from != (point{11, 0}) || to != (point{12, 2}) {
		t.Fatalf("selected %v to %v, want rows 11 and 12 of the text", from, to)
	}

	// The pane scrolls a row up: the selected rows show a row lower.
	s.Frame = *frame(20, "ten", "eleven", "twelve")
	s.Panes[0].Scroll = &endpoint.Scroll{Offset: 1, Max: 10, Rows: 3}
	tt.Frame()
	tt.Command("copy")
	if len(requests) != 1 || requests[0]["method"] != "pane.selection.read" {
		t.Fatalf("copying text off the screen asked %v", requests)
	}
	want := map[string]any{"row": 11.0, "col": 0.0}
	if a := requests[0]["anchor"].(map[string]any); a["row"] != want["row"] || a["col"] != want["col"] {
		t.Errorf("read from %v, want %v", a, want)
	}
}

// discard is an endpoint taking whatever the view sends.
type discard struct{}

func (discard) Input(string, ...endpoint.InputEvent) error { return nil }
func (discard) Resize(endpoint.Size) error                 { return nil }
func (discard) SetHostTheme(endpoint.HostTheme) error      { return nil }

// A key sent while the press selecting lasts ends the selection.
func TestKeyWhileSelecting(t *testing.T) {
	s := &endpoint.Surface{Frame: *frame(30, "", "hello world", "second line"), Panes: []endpoint.SurfacePane{
		{ID: "p1", Focused: true, Rect: endpoint.Rect{Width: 30, Height: 3}, Inner: endpoint.Rect{Width: 30, Height: 3}},
	}}
	v := New(ui.Font{Size: 14})
	var requests []map[string]any
	tt := ui.NewTester(func(c *ui.Context) { v.Build(c, s, discard{}, hooks(&requests)).Fill().AutoFocus() }, 600, 300)
	tt.Frame()
	r := v.cellRect(ui.Rect{}, 6, 1)
	tt.Press(r.X+r.W/2, r.Y+r.H/2)
	tt.Move(r.X+40, r.Y+r.H/2)
	tt.Key(0, ui.KeyEscape)
	tt.Move(r.X+60, r.Y+r.H/2)
	tt.Frame()
	if v.sel != nil || v.selecting {
		t.Errorf("a key left the selection %+v, selecting %v", v.sel, v.selecting)
	}
}
