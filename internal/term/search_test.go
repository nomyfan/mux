package term

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/egoist/mygo/ui"

	"mux/internal/herdr/endpoint"
)

// call is a request a view made.
type call struct {
	method string
	params map[string]any
	done   func(json.RawMessage, error)
}

// searchView shows a surface of two panes side by side, p1 focused, noting
// the requests the view makes.
func searchView(t *testing.T) (*View, *endpoint.Surface, *ui.Tester, *[]call) {
	pane := func(id string, x uint16, focused bool) endpoint.SurfacePane {
		return endpoint.SurfacePane{ID: id, Focused: focused, ContentRevision: 7,
			Rect: endpoint.Rect{X: x, Width: 40, Height: 10}, Inner: endpoint.Rect{X: x + 1, Y: 1, Width: 38, Height: 8},
			Scroll: &endpoint.Scroll{Offset: 0, Max: 50, Rows: 8}}
	}
	s := &endpoint.Surface{Frame: *frame(80, make([]string, 10)...), Panes: []endpoint.SurfacePane{pane("p1", 0, true), pane("p2", 40, false)}}
	v := New(ui.Font{Size: 14})
	var calls []call
	h := Hooks{
		Header: func(*ui.Context, endpoint.SurfacePane, ui.Rect, bool) {},
		Focus:  func(string) {},
		Resize: func(string, []bool, float64) {},
		Request: func(method string, params any, done func(json.RawMessage, error)) {
			b, _ := json.Marshal(params)
			var m map[string]any
			json.Unmarshal(b, &m)
			calls = append(calls, call{method, m, done})
		},
	}
	tt := ui.NewTester(func(c *ui.Context) { v.Build(c, s, nil, h).Fill().AutoFocus() }, 1000, 400)
	tt.Frame()
	return v, s, tt, &calls
}

func TestSearch(t *testing.T) {
	v, _, tt, calls := searchView(t)
	last := func() call { return (*calls)[len(*calls)-1] }
	tt.Key(ui.Cmd, ui.KeyF)
	if s := v.searches["p1"]; s == nil || !s.open {
		t.Fatal("Cmd+F left p1's search closed")
	}
	tt.Type("err")
	if len(*calls) == 0 {
		t.Fatal("typing after Cmd+F searched nothing: the field lacks the keyboard")
	}
	c := last()
	if c.method != "pane.copy_search" || c.params["query"] != "err" || c.params["direction"] != "backward" || c.params["content_revision"] != 7.0 {
		t.Fatalf("typing searched %+v", c)
	}
	if cur := c.params["cursor"].(map[string]any); cur["row"] != 58.0 {
		t.Errorf("searched from row %v, want the screen's bottom, 58", cur["row"])
	}
	c.done(json.RawMessage(`{"content_revision":7,"matches":[{"start":{"row":3,"col":2},"end":{"row":3,"col":4}},{"start":{"row":52,"col":0},"end":{"row":52,"col":2}}],"current":0,"current_global":4,"total":6}`), nil)
	tt.Frame()
	if !tt.HasText("5 of 6") {
		t.Errorf("no count in %q", tt.Texts())
	}
	if c := last(); c.method != "pane.scroll" || c.params["offset_from_bottom"] != 50.0 {
		t.Errorf("a match off the screen scrolled %+v, want 50 rows up", c)
	}

	// The server sent two of the six: stepping among them asks nothing.
	n := len(*calls)
	tt.Key(ui.Shift, ui.KeyEnter)
	if len(*calls) != n || !tt.HasText("6 of 6") {
		t.Errorf("Shift+Enter asked %v, showing %q; want the next match it has, 6 of 6", (*calls)[n:], tt.Texts())
	}
	tt.Key(0, ui.KeyEnter)
	if len(*calls) != n || !tt.HasText("5 of 6") {
		t.Errorf("Enter asked %v, showing %q; want the match before, 5 of 6", (*calls)[n:], tt.Texts())
	}
	// Before the first it has, it asks the server, from that one.
	if err := tt.Click("Previous Match"); err != nil {
		t.Fatal(err)
	}
	if c := last(); c.method != "pane.copy_search" || c.params["direction"] != "backward" || c.params["previous"] == nil {
		t.Errorf("Previous Match before the matches it has searched %+v, want up from the first", c)
	}
	c = last()
	c.done(json.RawMessage(`{"content_revision":7,"matches":[{"start":{"row":1,"col":0},"end":{"row":1,"col":2}},{"start":{"row":3,"col":2},"end":{"row":3,"col":4}}],"current":0,"current_global":3,"total":6}`), nil)
	tt.Frame()
	if err := tt.Click("Next Match"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("5 of 6") {
		t.Errorf("Next Match shows %q, want 5 of 6", tt.Texts())
	}
	if v.sel != nil {
		t.Error("a click on the bar began a selection")
	}
	if err := tt.Click("Clear"); err != nil {
		t.Fatal(err)
	}
	if s := v.searches["p1"]; s.query != "" || s.matches != nil {
		t.Errorf("Clear left %q, %v", s.query, s.matches)
	}
	tt.Key(0, ui.KeyEscape)
	if v.searches["p1"].open {
		t.Error("Escape left the search open")
	}
}

// With every match, stepping goes round from one end to the other.
func TestSearchWraps(t *testing.T) {
	v, _, tt, calls := searchView(t)
	tt.Key(ui.Cmd, ui.KeyF)
	tt.Type("x")
	c := (*calls)[len(*calls)-1]
	c.done(json.RawMessage(`{"content_revision":7,"matches":[{"start":{"row":51,"col":0},"end":{"row":51,"col":0}},{"start":{"row":55,"col":0},"end":{"row":55,"col":0}}],"current":1,"current_global":1,"total":2}`), nil)
	tt.Frame()
	n := len(*calls)
	tt.Key(ui.Shift, ui.KeyEnter)
	if s := v.searches["p1"]; s.current != 0 || !tt.HasText("1 of 2") || len(*calls) != n {
		t.Errorf("after the last, at %d showing %q, asking %v; want the first", s.current, tt.Texts(), (*calls)[n:])
	}
	tt.Key(0, ui.KeyEnter)
	if s := v.searches["p1"]; s.current != 1 || !tt.HasText("2 of 2") {
		t.Errorf("before the first, at %d showing %q; want the last", s.current, tt.Texts())
	}
}

// Each pane has its own search: Cmd+F opens the focused pane's, and
// the one of a pane the focus left stays open.
func TestSearchPerPane(t *testing.T) {
	v, s, tt, _ := searchView(t)
	tt.Key(ui.Cmd, ui.KeyF)
	tt.Type("one")
	s.Panes[0].Focused, s.Panes[1].Focused = false, true
	tt.Frame()
	if v.searches["p2"] != nil && v.searches["p2"].open {
		t.Fatal("p2's search opened with p1's")
	}
	tt.Key(ui.Cmd, ui.KeyF)
	tt.Type("two")
	if p1, p2 := v.searches["p1"], v.searches["p2"]; !p1.open || !p2.open || p1.query != "one" || p2.query != "two" {
		t.Errorf("searches %+v and %+v, want p1's for one and p2's for two, both open", p1, p2)
	}
}

// A scroll waits for the one before, and only the last of those waiting
// is asked for.
func TestRevealCoalesces(t *testing.T) {
	v, _, _, calls := searchView(t)
	at := func(row int) textRange { return textRange{Start: point{Row: row}, End: point{Row: row}} }
	v.reveal("p1", at(3))
	v.reveal("p1", at(10))
	v.reveal("p1", at(20))
	if len(*calls) != 1 || (*calls)[0].params["offset_from_bottom"] != 50.0 {
		t.Fatalf("asked %+v, want one scroll, to the top", *calls)
	}
	(*calls)[0].done(nil, nil)
	if len(*calls) != 2 || (*calls)[1].params["offset_from_bottom"] != 34.0 {
		t.Errorf("then asked %+v, want a scroll showing row 20 in the middle, 34 up", (*calls)[1:])
	}
}

// Matches move up the pane's rows all at once as it drops the oldest of
// its history: once its text changes, a match the screen shows is read for
// the query, and the pane searched anew if it moved.
func TestSearchMoves(t *testing.T) {
	v, s, tt, calls := searchView(t)
	last := func() call { return (*calls)[len(*calls)-1] }
	// Row 52 of p1's text shows at the screen's third row, from column 1.
	lines := make([]string, 10)
	lines[3] = " err"
	s.Frame = *frame(80, lines...)
	tt.Key(ui.Cmd, ui.KeyF)
	tt.Type("err")
	last().done(json.RawMessage(`{"content_revision":7,"matches":[{"start":{"row":40,"col":0},"end":{"row":40,"col":2}},{"start":{"row":52,"col":0},"end":{"row":52,"col":2}}],"current":1,"current_global":1,"total":2}`), nil)
	tt.Frame()

	// The pane prints below, the match staying where it was.
	s.Panes[0].ContentRevision = 8
	tt.Frame()
	n := len(*calls)
	tt.Key(0, ui.KeyEnter)
	for _, c := range (*calls)[n:] {
		if c.method == "pane.copy_search" {
			t.Errorf("with its match in place, Enter searched %+v", c)
		}
	}
	if !tt.HasText("1 of 2") {
		t.Fatalf("Enter showed %q, want the match before, 1 of 2", tt.Texts())
	}
	tt.Key(ui.Shift, ui.KeyEnter)

	// The pane drops a row of its history: row 52 now shows the next.
	lines[3], lines[2] = "", " err"
	s.Frame = *frame(80, lines...)
	s.Panes[0].ContentRevision = 9
	tt.Frame()
	if c := last(); c.method != "pane.copy_search" || c.params["previous"] != nil || c.params["content_revision"] != 9.0 {
		t.Fatalf("with the match moved, asked %+v; want a search anew", c)
	}
	if m := v.searches["p1"].matches; m != nil {
		t.Errorf("matches %v showed while moved", m)
	}
	// The text changes again under the search: it goes again.
	s.Panes[0].ContentRevision = 10
	n = len(*calls)
	last().done(nil, errors.New("stale_content: pane content changed"))
	if len(*calls) != n+1 || last().params["content_revision"] != 10.0 {
		t.Fatalf("a stale search asked %v; want it again", (*calls)[n:])
	}
	last().done(json.RawMessage(`{"content_revision":10,"matches":[{"start":{"row":51,"col":0},"end":{"row":51,"col":2}}],"current":0,"current_global":1,"total":2}`), nil)
	tt.Frame()
	if !tt.HasText("2 of 2") {
		t.Errorf("after searching anew, %q; want 2 of 2", tt.Texts())
	}
}
