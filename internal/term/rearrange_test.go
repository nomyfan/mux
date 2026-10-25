package term

import (
	"encoding/json"
	"testing"

	"github.com/egoist/mygo/ui"

	"mux/internal/herdr/endpoint"
)

func TestDragPane(t *testing.T) {
	pane := func(id string, x uint16) endpoint.SurfacePane {
		return endpoint.SurfacePane{ID: id, Focused: id == "p1", Rect: endpoint.Rect{X: x, Width: 40, Height: 20},
			Inner: endpoint.Rect{X: x + 1, Y: 1, Width: 38, Height: 18}}
	}
	s := &endpoint.Surface{Frame: *frame(80, make([]string, 20)...), Panes: []endpoint.SurfacePane{pane("p1", 0), pane("p2", 40)}}
	v := New(ui.Font{Size: 14})
	type drop struct {
		pane, target string
		side         Side
	}
	var drops []drop
	h := Hooks{
		Header: func(c *ui.Context, p endpoint.SurfacePane, box ui.Rect, hovered bool) {
			ui.Row(c.Key("title")).Bind(v.TitleHandle(p.ID)).Children(func() { ui.Text(c, "title "+p.ID) })
		},
		Title:     func(c *ui.Context, p endpoint.SurfacePane) { ui.Text(c, "dragging "+p.ID) },
		Focus:     func(string) {},
		Resize:    func(string, []bool, float64) {},
		Request:   func(string, any, func(json.RawMessage, error)) {},
		Rearrange: func(pane, target string, side Side) { drops = append(drops, drop{pane, target, side}) },
	}
	tt := ui.NewTester(func(c *ui.Context) { v.Build(c, s, nil, h).Fill() }, 800, 400)
	tt.Frame()
	tt.Frame()
	title := v.titleBoxes["p1"]
	x, y := title.X+title.W/2, title.Y+title.H/2
	p2 := v.boxes()[1].card
	dragTo := func(tx, ty float32) {
		tt.Press(x, y)
		tt.Move(x+10, y+10)
		tt.Move(tx, ty)
		tt.Frame()
	}

	dragTo(p2.X+p2.W/2, p2.Y+p2.H/2)
	if !tt.HasText("dragging p1") {
		t.Errorf("no chip while dragging: %q", tt.Texts())
	}
	tt.Release(p2.X+p2.W/2, p2.Y+p2.H/2)
	dragTo(p2.X+p2.W-5, p2.Y+p2.H/2)
	tt.Release(p2.X+p2.W-5, p2.Y+p2.H/2)
	// Onto itself, and a click: nothing.
	dragTo(x+100, y+100)
	tt.Release(x+100, y+100)
	tt.Press(x, y)
	tt.Release(x, y)
	want := []drop{{"p1", "p2", SideCenter}, {"p1", "p2", SideRight}}
	if len(drops) != len(want) || drops[0] != want[0] || drops[1] != want[1] {
		t.Errorf("dropped %v, want %v", drops, want)
	}
	if tt.HasText("dragging p1") {
		t.Error("the chip stayed after the drop")
	}
}

// A pane dropped on itself stays, and the drag ends.
func TestDragPaneOntoItself(t *testing.T) {
	pane := func(id string, x uint16) endpoint.SurfacePane {
		return endpoint.SurfacePane{ID: id, Focused: id == "p1", Rect: endpoint.Rect{X: x, Width: 40, Height: 20},
			Inner: endpoint.Rect{X: x + 1, Y: 1, Width: 38, Height: 18}}
	}
	s := &endpoint.Surface{Frame: *frame(80, make([]string, 20)...), Panes: []endpoint.SurfacePane{pane("p1", 0), pane("p2", 40)}}
	v := New(ui.Font{Size: 14})
	dropped := 0
	h := Hooks{
		Header: func(c *ui.Context, p endpoint.SurfacePane, box ui.Rect, hovered bool) {
			ui.Row(c.Key("title")).Bind(v.TitleHandle(p.ID)).Children(func() { ui.Text(c, "title "+p.ID) })
		},
		Title:     func(c *ui.Context, p endpoint.SurfacePane) { ui.Text(c, "dragging "+p.ID) },
		Focus:     func(string) {},
		Resize:    func(string, []bool, float64) {},
		Request:   func(string, any, func(json.RawMessage, error)) {},
		Rearrange: func(string, string, Side) { dropped++ },
	}
	tt := ui.NewTester(func(c *ui.Context) { v.Build(c, s, nil, h).Fill() }, 800, 400)
	tt.Frame()
	tt.Frame()
	title := v.titleBoxes["p1"]
	x, y := title.X+title.W/2, title.Y+title.H/2
	tt.Press(x, y)
	tt.Move(x+30, y+60)
	tt.Move(x+60, y+120)
	tt.Frame()
	tt.Release(x+60, y+120)
	tt.Frame()
	if dropped != 0 || v.paneDrag != nil || tt.HasText("dragging p1") {
		t.Errorf("after a drop on itself: %d dropped, drag %+v, texts %q", dropped, v.paneDrag, tt.Texts())
	}
}
