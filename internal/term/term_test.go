package term

import (
	"image/color"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"

	"mux/internal/hc"
	"mux/internal/layout"
)

// fakeStream is a terminal's stream: output fed in, input and sizes kept.
type fakeStream struct {
	out *io.PipeReader
	in  *io.PipeWriter

	mu      sync.Mutex
	typed   strings.Builder
	sizes   [][2]int
	facts   []hc.Display
	focused []bool
	closed  bool
}

func (s *fakeStream) Read(p []byte) (int, error) { return s.out.Read(p) }
func (s *fakeStream) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.typed.Write(p)
	return len(p), nil
}
func (s *fakeStream) Close() error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	return s.out.Close()
}
func (s *fakeStream) Resize(cols, rows int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sizes = append(s.sizes, [2]int{cols, rows})
}
func (s *fakeStream) Focus(f bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.focused = append(s.focused, f)
}
func (s *fakeStream) Facts(d hc.Display) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.facts = append(s.facts, d)
}
func (s *fakeStream) Typed() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.typed.String()
}

// fakeCore attaches terminals to fake streams.
type fakeCore struct {
	mu      sync.Mutex
	streams map[string]*fakeStream
	asked   map[string]hc.AttachOptions
}

func (f *fakeCore) Attach(o hc.AttachOptions) (Stream, error) {
	r, w := io.Pipe()
	s := &fakeStream{out: r, in: w}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.streams == nil {
		f.streams, f.asked = map[string]*fakeStream{}, map[string]hc.AttachOptions{}
	}
	f.streams[o.Terminal], f.asked[o.Terminal] = s, o
	return s, nil
}

func (f *fakeCore) stream(t string) *fakeStream {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.streams[t]
}

// twoPanes is a tab of t1 left of t2, t1 focused.
func twoPanes() *layout.Tab {
	var l layout.Layout
	_, tab := l.AddWorkspace("t1")
	tab.Split("t1", "t2", layout.Right)
	tab.Focus = "t1"
	return tab
}

func newView() *View {
	return New(terminal.Font{Size: 13}, terminal.LightTheme(), terminal.DarkTheme())
}

func testHooks(v *View, changed *int) Hooks {
	return Hooks{
		Header: func(c *ui.Context, term string, box ui.Rect, hovered, focused bool) {
			ui.Row(c.Key("title")).Bind(v.TitleHandle(term)).Children(func() { ui.Text(c, "title "+term) })
		},
		Title:   func(c *ui.Context, term string) { ui.Text(c, "dragging "+term) },
		Changed: func() { *changed++ },
	}
}

// waitFor runs frames until cond holds.
func waitFor(t *testing.T, tt *ui.Tester, what string, cond func() bool) {
	t.Helper()
	for range 300 {
		if cond() {
			return
		}
		tt.Frame()
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no %s", what)
}

// Panes attach as they show, at the size they fit; their output shows,
// and what is typed reaches the focused one.
func TestPanes(t *testing.T) {
	core := &fakeCore{}
	v, tab, changed := newView(), twoPanes(), 0
	tt := ui.NewTester(func(c *ui.Context) { v.Build(c, tab, core, testHooks(v, &changed)).Fill() }, 800, 400)
	waitFor(t, tt, "attach", func() bool { return core.stream("t1") != nil && core.stream("t2") != nil })
	o := core.asked["t1"]
	if o.Cols < 20 || o.Cols > 60 || o.History != history || o.Display == nil || o.Display.Cell == nil || o.Display.Background == "" {
		t.Errorf("attached with %+v", o)
	}
	go core.stream("t1").in.Write([]byte("hello \x1b[31mworld\x1b[0m\r\n"))
	waitFor(t, tt, "output", func() bool { return strings.Contains(v.Terminal("t1").Text(), "hello world") })
	tt.Type("ls")
	waitFor(t, tt, "input", func() bool { return core.stream("t1").Typed() == "ls" })
	if got := core.stream("t2").Typed(); got != "" {
		t.Errorf("t2 got %q", got)
	}
}

// Cmd+arrows move the focus to the pane beside, Cmd+Option+arrows swap
// the focused pane with it.
func TestFocusMoves(t *testing.T) {
	core := &fakeCore{}
	v, tab, changed := newView(), twoPanes(), 0
	tt := ui.NewTester(func(c *ui.Context) { v.Build(c, tab, core, testHooks(v, &changed)).Fill() }, 800, 400)
	waitFor(t, tt, "attach", func() bool { return core.stream("t2") != nil })
	tt.Key(ui.Cmd, ui.KeyRight)
	tt.Frame()
	if tab.Focus != "t2" || changed == 0 {
		t.Fatalf("focus %q after Cmd+Right", tab.Focus)
	}
	tt.Type("x")
	waitFor(t, tt, "input in t2", func() bool { return core.stream("t2").Typed() == "x" })
	tt.Key(ui.Cmd|ui.Alt, ui.KeyLeft)
	tt.Frame()
	if got := tab.Leaves()[0].Terminal; got != "t2" || tab.Focus != "t2" {
		t.Errorf("after swapping left: first pane %q, focus %q", got, tab.Focus)
	}
}

// A pane dragged by its title onto another swaps with it in the middle,
// goes beside it by its edge, and stays dropped on itself.
func TestDragPane(t *testing.T) {
	core := &fakeCore{}
	v, tab, changed := newView(), twoPanes(), 0
	tt := ui.NewTester(func(c *ui.Context) { v.Build(c, tab, core, testHooks(v, &changed)).Fill() }, 800, 400)
	waitFor(t, tt, "titles", func() bool { return len(v.titles) == 2 })
	tt.Frame()
	drag := func(from string, tx, ty float32) {
		title := v.titles[from]
		x, y := title.X+title.W/2, title.Y+title.H/2
		tt.Press(x, y)
		tt.Move(x+10, y+10)
		tt.Move(tx, ty)
		tt.Frame()
	}
	p2 := v.boxes[1].card
	center := func() color.RGBA { return tt.Image().RGBAAt(int(p2.X+p2.W/2), int(p2.Y+p2.H/2)) }
	plain := center()
	drag("t1", p2.X+p2.W/2, p2.Y+p2.H/2)
	if !tt.HasText("dragging t1") {
		t.Errorf("no chip while dragging: %q", tt.Texts())
	}
	// The zone covers the whole target, over its terminal.
	if zone, ok := tt.Find("Drop Zone"); !ok || zone != p2 {
		t.Errorf("drop zone %v, want %v", zone, p2)
	}
	if center() == plain {
		t.Error("the drop zone doesn't show over the terminal")
	}
	tt.Release(p2.X+p2.W/2, p2.Y+p2.H/2)
	tt.Frame()
	if got := tab.Leaves()[0].Terminal; got != "t2" {
		t.Fatalf("dropped in the middle, first pane %q", got)
	}
	if tt.HasText("dragging t1") {
		t.Error("the chip stayed after the drop")
	}
	// t1 is on the right now: dropped on t2's bottom edge, it goes below.
	tt.Frame()
	p := v.boxes[0].card
	drag("t1", p.X+p.W/2, p.Y+p.H-5)
	if zone, _ := tt.Find("Drop Zone"); zone != (ui.Rect{X: p.X, Y: p.Y + p.H/2, W: p.W, H: p.H / 2}) {
		t.Errorf("drop zone %v by the bottom of %v", zone, p)
	}
	tt.Release(p.X+p.W/2, p.Y+p.H-5)
	tt.Frame()
	if root := tab.Root; !root.Vertical || root.A.Terminal != "t2" || root.B.Terminal != "t1" {
		t.Errorf("dropped below: %+v %+v", root.A, root.B)
	}
	// Onto itself: nothing.
	before := changed
	tt.Frame()
	b := v.boxes[1].card
	drag("t1", b.X+b.W/2, b.Y+b.H/2)
	tt.Release(b.X+b.W/2, b.Y+b.H/2)
	if changed != before || tab.Root.B.Terminal != "t1" {
		t.Error("a pane dropped on itself moved")
	}
}

// Dragging a divider moves its split.
func TestDivider(t *testing.T) {
	core := &fakeCore{}
	v, tab, changed := newView(), twoPanes(), 0
	tt := ui.NewTester(func(c *ui.Context) { v.Build(c, tab, core, testHooks(v, &changed)).Fill() }, 801, 400)
	tt.Frame()
	tt.Frame()
	l := v.dividers[0].Line
	x, y := l.X, l.Y+l.H/2
	tt.Press(x, y)
	tt.Move(x+40, y)
	tt.Move(x+80, y)
	tt.Release(x+80, y)
	tt.Frame()
	if r := tab.Root.Ratio; r < 0.55 || r > 0.65 {
		t.Errorf("ratio %v after dragging the divider 80 right", r)
	}
	if changed == 0 {
		t.Error("the drag's end saved nothing")
	}
}

// Panes laid out as the tab first shows build again at their size, without
// anything else asking for a frame.
func TestLaysOutUnasked(t *testing.T) {
	core := &fakeCore{}
	v, changed := newView(), 0
	var tab *layout.Tab
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
			if tab != nil {
				v.Build(c, tab, core, testHooks(v, &changed)).Grow(1)
			}
		})
	}, 800, 400)
	tab = twoPanes()
	tt.Frame()
	for range 10 {
		if !tt.FramePending() {
			break
		}
		tt.Frame()
	}
	if len(v.boxes) != 2 || !tt.HasText("title t1") {
		t.Errorf("laid out %d panes, showing %q", len(v.boxes), tt.Texts())
	}
}

// A pane whose terminal attaches again, as its program restarted, takes
// the keyboard again if it had it.
func TestDropKeepsFocus(t *testing.T) {
	core := &fakeCore{}
	v, tab, changed := newView(), twoPanes(), 0
	tt := ui.NewTester(func(c *ui.Context) { v.Build(c, tab, core, testHooks(v, &changed)).Fill() }, 800, 400)
	waitFor(t, tt, "focus", func() bool { return v.Focused() == "t1" })
	v.Drop("t1")
	waitFor(t, tt, "focus again", func() bool { return v.Focused() == "t1" && tt.Focused("Terminal") })
	tt.Type("x")
	waitFor(t, tt, "input", func() bool { return core.stream("t1").Typed() == "x" })
}
