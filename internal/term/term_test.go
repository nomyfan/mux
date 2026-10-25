package term

import (
	"testing"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"

	"mux/internal/herdr/endpoint"
)

func TestKeyEvent(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  ui.Key
		mods ui.Modifiers
		want endpoint.Key
		ok   bool
	}{
		{"Enter", ui.KeyEnter, 0, endpoint.Key{Code: endpoint.KeyEnter}, true},
		{"Shift+Tab", ui.KeyTab, ui.Shift, endpoint.Key{Code: endpoint.KeyBackTab, Mods: endpoint.ModShift}, true},
		{"Ctrl+C", ui.KeyC, ui.Ctrl, endpoint.Key{Code: endpoint.KeyChar, Char: 'c', Mods: endpoint.ModCtrl}, true},
		// Option+Left/Right move by word as Esc b / Esc f; other chords keep the arrow.
		{"Alt+Left", ui.KeyLeft, ui.Alt, endpoint.Key{Code: endpoint.KeyChar, Char: 'b', Mods: endpoint.ModAlt}, true},
		{"Alt+Right", ui.KeyRight, ui.Alt, endpoint.Key{Code: endpoint.KeyChar, Char: 'f', Mods: endpoint.ModAlt}, true},
		{"Shift+Alt+Left", ui.KeyLeft, ui.Shift | ui.Alt, endpoint.Key{Code: endpoint.KeyLeft, Mods: endpoint.ModShift | endpoint.ModAlt}, true},
		{"F5", ui.KeyF5, 0, endpoint.Key{Code: endpoint.KeyF, F: 5}, true},
		// Text keys type text, which comes as an InputText.
		{"a", ui.KeyA, 0, endpoint.Key{}, false},
		{"Shift+a", ui.KeyA, ui.Shift, endpoint.Key{}, false},
		{"Option+p", ui.KeyP, ui.Alt, endpoint.Key{}, false},
		// Command chords are the app's.
		{"Cmd+C", ui.KeyC, ui.Super, endpoint.Key{}, false},
		{"Cmd+Enter", ui.KeyEnter, ui.Super, endpoint.Key{}, false},
	} {
		got, ok := keyEvent(tc.key, tc.mods)
		if got != tc.want || ok != tc.ok {
			t.Errorf("%s: %+v, %v; want %+v, %v", tc.name, got, ok, tc.want, tc.ok)
		}
	}
}

func TestCellColors(t *testing.T) {
	th := terminal.DarkTheme()
	p := newPalette(th, true)
	for _, tc := range []struct {
		name      string
		cell      endpoint.Cell
		fg, bg    ui.Color
		defaultBG bool
	}{
		{"default", endpoint.Cell{}, th.Foreground, th.Background, true},
		{"named red on blue", endpoint.Cell{FG: 2, BG: 5}, th.Palette[1], th.Palette[4], false},
		{"indexed", endpoint.Cell{FG: 0x01000010, BG: 0x010000e8}, ui.RGB(0, 0, 0), ui.RGB(8, 8, 8), false},
		{"rgb", endpoint.Cell{FG: 0x02123456}, ui.RGB(0x12, 0x34, 0x56), th.Background, true},
		{"reversed", endpoint.Cell{Modifier: reversed}, th.Background, th.Foreground, false},
		{"hidden", endpoint.Cell{FG: 2, Modifier: hidden}, th.Background, th.Background, true},
	} {
		fg, bg, def := p.cellColors(&tc.cell)
		if fg != tc.fg || bg != tc.bg || def != tc.defaultBG {
			t.Errorf("%s: %v on %v (default %v); want %v on %v (default %v)", tc.name, fg, bg, def, tc.fg, tc.bg, tc.defaultBG)
		}
	}
}

func TestWide(t *testing.T) {
	for symbol, want := range map[string]bool{
		"a": false, " ": false, "": false, "é": false, "─": false,
		"中": true, "，": true, "Ａ": true, "😀": true, "👍🏽": true,
	} {
		if got := wide(symbol); got != want {
			t.Errorf("wide(%q) = %v, want %v", symbol, got, want)
		}
	}
}

// A press on another pane's header focuses it, without the header taking
// the press, which moves the window along the top.
func TestHeaderFocus(t *testing.T) {
	s := &endpoint.Surface{
		Frame: endpoint.Frame{Width: 60, Height: 20, Cells: make([]endpoint.Cell, 60*20)},
		Panes: []endpoint.SurfacePane{
			{ID: "p1", Focused: true, Rect: endpoint.Rect{X: 0, Y: 0, Width: 30, Height: 20}, Inner: endpoint.Rect{X: 1, Y: 1, Width: 28, Height: 18}},
			{ID: "p2", Rect: endpoint.Rect{X: 30, Y: 0, Width: 30, Height: 20}, Inner: endpoint.Rect{X: 31, Y: 1, Width: 28, Height: 18}},
		},
	}
	var focused string
	v := New(ui.Font{Size: 14})
	hooks := Hooks{
		Header: func(c *ui.Context, p endpoint.SurfacePane, box ui.Rect, hovered bool) { ui.Text(c, p.ID) },
		Focus:  func(id string) { focused = id },
		Resize: func(string, []bool, float64) {},
	}
	tt := ui.NewTester(func(c *ui.Context) { v.Build(c, s, nil, hooks).Fill() }, 800, 400)
	tt.Frame()
	h := v.boxes()[1].header
	tt.Press(h.X+h.W/2, h.Y+h.H/2)
	tt.Release(h.X+h.W/2, h.Y+h.H/2)
	if focused != "p2" {
		t.Errorf("a press on p2's header focused %q", focused)
	}
}

// Command with an arrow asks the server to focus the pane beside the
// focused one, with Option to swap it there; in the search field it moves
// the caret instead.
func TestPaneMoves(t *testing.T) {
	_, _, tt, calls := searchView(t)
	tt.Key(ui.Cmd, ui.KeyRight)
	tt.Key(ui.Cmd|ui.Ctrl, ui.KeyLeft)
	tt.Key(ui.Cmd|ui.Alt, ui.KeyDown)
	want := []struct{ method, direction string }{
		{"pane.focus_direction", "right"}, {"pane.swap", "down"},
	}
	if len(*calls) != len(want) {
		t.Fatalf("made %d requests, want %d", len(*calls), len(want))
	}
	for i, w := range want {
		c := (*calls)[i]
		if c.method != w.method || c.params["pane_id"] != "p1" || c.params["direction"] != w.direction {
			t.Errorf("request %d: %+v, want %v", i, c, w)
		}
	}
	tt.Key(ui.Cmd, ui.KeyF)
	tt.Frame()
	n := len(*calls)
	tt.Key(ui.Cmd, ui.KeyLeft)
	for _, c := range (*calls)[n:] {
		if c.method == "pane.focus_direction" {
			t.Errorf("Cmd+Left in the search field moved the focus: %+v", c)
		}
	}
}

// A blinking cursor hides and shows every blinkPeriod, shows again as the
// user types, and stays after blinkFor; a steady one stays.
func TestCursorBlink(t *testing.T) {
	for _, tc := range []struct {
		shape uint8
		blink bool
	}{{0, true}, {1, true}, {2, false}, {5, true}, {6, false}} {
		s := &endpoint.Surface{
			Frame: endpoint.Frame{Width: 20, Height: 5, Cells: make([]endpoint.Cell, 20*5),
				Cursor: &endpoint.Cursor{X: 2, Y: 1, Visible: true, Shape: tc.shape}},
			Panes: []endpoint.SurfacePane{
				{ID: "p1", Focused: true, Rect: endpoint.Rect{Width: 20, Height: 5}, Inner: endpoint.Rect{Width: 20, Height: 5}},
			},
		}
		v := New(ui.Font{Size: 14})
		v.Focus()
		hooks := Hooks{Header: func(c *ui.Context, p endpoint.SurfacePane, box ui.Rect, hovered bool) {}}
		tt := ui.NewTester(func(c *ui.Context) { v.Build(c, s, nil, hooks).Fill() }, 400, 200)
		tt.Frame()
		tt.Frame()
		r := v.cellRect(ui.Rect{}, 2, 1)
		// The bar's and the block's left edge.
		pixel := func() uint32 {
			i := tt.Image()
			c := i.RGBAAt(int(r.X)+1, int(r.Y+r.H/2))
			return uint32(c.R)<<16 | uint32(c.G)<<8 | uint32(c.B)
		}
		shown := pixel()
		tt.Advance(blinkPeriod)
		if hidden := pixel(); (hidden != shown) != tc.blink {
			t.Errorf("shape %d: %06x after %v, %06x before", tc.shape, hidden, blinkPeriod, shown)
		}
		tt.Type("a")
		tt.Frame()
		if got := pixel(); got != shown {
			t.Errorf("shape %d: %06x as the user types, want %06x", tc.shape, got, shown)
		}
		tt.Advance(blinkFor + blinkPeriod)
		if got := pixel(); got != shown {
			t.Errorf("shape %d: %06x after %v, want %06x", tc.shape, got, blinkFor, shown)
		}
	}
}

// recorder is an endpoint keeping the input the view sends.
type recorder struct {
	discard
	input []endpoint.InputEvent
}

func (r *recorder) Input(_ string, events ...endpoint.InputEvent) error {
	r.input = append(r.input, events...)
	return nil
}

// A trackpad scroll of several rows goes to the scrollback as one event of
// that many rows, and to a program reading the wheel, which the server
// sends one report or key per event, as that many one-row events.
func TestWheel(t *testing.T) {
	for _, tc := range []struct {
		name          string
		mouse, alt    bool
		events, lines int
	}{
		{"scrollback", false, false, 1, 3},
		{"mouse reporting", true, false, 3, 1},
		{"alternate screen", false, true, 3, 1},
	} {
		pane := endpoint.SurfacePane{ID: "p1", Rect: endpoint.Rect{Width: 20, Height: 10}, Inner: endpoint.Rect{Width: 20, Height: 10},
			MouseReporting: tc.mouse, AltScreen: tc.alt}
		s := &endpoint.Surface{Frame: endpoint.Frame{Width: 20, Height: 10, Cells: make([]endpoint.Cell, 20*10)}, Panes: []endpoint.SurfacePane{pane}}
		v := New(ui.Font{Size: 14})
		ep := &recorder{}
		var requests []map[string]any
		tt := ui.NewTester(func(c *ui.Context) { v.Build(c, s, ep, hooks(&requests)).Fill() }, 600, 300)
		tt.Frame()
		r := v.cellRect(ui.Rect{}, 5, 5)
		tt.Scroll(r.X, r.Y, 0, 3.5*v.cellH)
		if len(ep.input) != tc.events {
			t.Fatalf("%s: sent %d events, want %d", tc.name, len(ep.input), tc.events)
		}
		for _, ev := range ep.input {
			if m, ok := ev.(endpoint.Mouse); !ok || int(m.Lines) != tc.lines {
				t.Errorf("%s: sent %+v, want a scroll of %d rows", tc.name, ev, tc.lines)
			}
		}
	}
}
