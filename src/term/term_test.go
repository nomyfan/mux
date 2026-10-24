package term

import (
	"testing"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"

	"mux/src/herdr/endpoint"
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
		{"Alt+Left", ui.KeyLeft, ui.Alt, endpoint.Key{Code: endpoint.KeyLeft, Mods: endpoint.ModAlt}, true},
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
