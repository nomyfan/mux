package layout

import (
	"encoding/json"
	"slices"
	"testing"
)

func terminals(t *Tab) []string {
	var out []string
	for _, n := range t.Leaves() {
		out = append(out, n.Terminal)
	}
	return out
}

func TestSplitRemove(t *testing.T) {
	var l Layout
	ws, tab := l.AddWorkspace("t1")
	tab.Split("t1", "t2", Right)
	tab.Split("t2", "t3", Down)
	tab.Split("t1", "t4", Left)
	if got := terminals(tab); !slices.Equal(got, []string{"t4", "t1", "t2", "t3"}) {
		t.Fatalf("panes %v", got)
	}
	if tab.Focus != "t4" {
		t.Errorf("focus %q, want the new pane", tab.Focus)
	}
	l.Remove("t4")
	if got := terminals(tab); !slices.Equal(got, []string{"t1", "t2", "t3"}) || tab.Focus != "t1" {
		t.Fatalf("panes %v focus %q", got, tab.Focus)
	}
	l.AddTab(ws, "t5")
	for _, term := range []string{"t1", "t2", "t3"} {
		l.Remove(term)
	}
	if len(ws.Tabs) != 1 || l.Active != ws.Tabs[0].ID {
		t.Fatalf("tabs %v active %q", len(ws.Tabs), l.Active)
	}
	l.Remove("t5")
	if len(l.Workspaces) != 0 {
		t.Error("a workspace without tabs stayed")
	}
}

func TestBoxes(t *testing.T) {
	var l Layout
	_, tab := l.AddWorkspace("t1")
	tab.Split("t1", "t2", Right)
	tab.Split("t2", "t3", Down)
	boxes, dividers := tab.Boxes(Rect{0, 0, 201, 101}, 1)
	want := map[string]Rect{"t1": {0, 0, 100, 101}, "t2": {101, 0, 100, 50}, "t3": {101, 51, 100, 50}}
	for _, b := range boxes {
		if b.Rect != want[b.Terminal] {
			t.Errorf("%s at %+v, want %+v", b.Terminal, b.Rect, want[b.Terminal])
		}
	}
	if len(dividers) != 2 || dividers[0].Line != (Rect{100, 0, 1, 101}) {
		t.Errorf("dividers %+v", dividers)
	}
	if got := Neighbor(boxes, "t1", Right); got != "t2" {
		t.Errorf("right of t1: %q", got)
	}
	if got := Neighbor(boxes, "t3", Left); got != "t1" {
		t.Errorf("left of t3: %q", got)
	}
	if got := Neighbor(boxes, "t2", Down); got != "t3" {
		t.Errorf("below t2: %q", got)
	}
	if got := Neighbor(boxes, "t1", Up); got != "" {
		t.Errorf("above t1: %q", got)
	}
	tab.Zoomed, tab.Focus = true, "t3"
	if boxes, _ := tab.Boxes(Rect{0, 0, 10, 10}, 1); len(boxes) != 1 || boxes[0].Terminal != "t3" {
		t.Errorf("zoomed: %+v", boxes)
	}
}

func TestSwapMove(t *testing.T) {
	var l Layout
	_, tab := l.AddWorkspace("t1")
	tab.Split("t1", "t2", Right)
	tab.Split("t2", "t3", Down)
	tab.Pane("t1").Name = "build"
	tab.Swap("t1", "t3")
	if got := terminals(tab); !slices.Equal(got, []string{"t3", "t2", "t1"}) || tab.Pane("t1").Name != "build" {
		t.Fatalf("after swap %v", got)
	}
	tab.Move("t1", "t3", Up)
	if got := terminals(tab); !slices.Equal(got, []string{"t1", "t3", "t2"}) || tab.Pane("t1").Name != "build" {
		t.Fatalf("after move %v", got)
	}
	if !tab.Root.A.Vertical {
		t.Error("moved above, the split is not vertical")
	}
}

func TestPruneRepair(t *testing.T) {
	var l Layout
	_, tab := l.AddWorkspace("t1")
	tab.Split("t1", "t2", Right)
	l.AddWorkspace("t3")
	data, _ := json.Marshal(l)
	var back Layout
	json.Unmarshal(data, &back)
	back.Prune(func(term string) bool { return term == "t2" })
	if got := back.Terminals(); !slices.Equal(got, []string{"t2"}) || len(back.Workspaces) != 1 {
		t.Fatalf("after prune %v", got)
	}

	var broken Layout
	json.Unmarshal([]byte(`{"workspaces":[{"tabs":[{"root":{"ratio":7,"a":{"terminal":"t1"},"b":{}}},{"root":null}]},{"tabs":[]}]}`), &broken)
	broken.Repair()
	if len(broken.Workspaces) != 1 || len(broken.Workspaces[0].Tabs) != 1 {
		t.Fatalf("repaired %+v", broken)
	}
	tb := broken.Workspaces[0].Tabs[0]
	if tb.ID == "" || tb.Root.Terminal != "t1" || tb.Focus != "t1" {
		t.Errorf("repaired tab %+v", tb)
	}
}
