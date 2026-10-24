package main

import (
	"bytes"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"mux/src/herdr/endpoint"
	"mux/src/term"
)

// The sidebar groups tabs by workspace, a tab with an agent showing it and
// its state, others the directory of their first pane, unless named.
func TestSidebar(t *testing.T) {
	a := &app{
		term:         term.New(terminalFont),
		sidebarWidth: 240,
		snap: endpoint.Snapshot{
			Workspaces: []endpoint.Workspace{{ID: "w1", Label: "api"}, {ID: "w2", Label: "web"}},
			Tabs: []endpoint.Tab{
				{ID: "w1:t1", Workspace: "w1", Label: "1"},
				{ID: "w2:t1", Workspace: "w2", Label: "1"},
				{ID: "w2:t2", Workspace: "w2", Label: "logs"},
			},
			Panes: []endpoint.Pane{
				{ID: "w1:p1", Workspace: "w1", Tab: "w1:t1", Cwd: "/srv/api"},
				{ID: "w1:p2", Workspace: "w1", Tab: "w1:t1", Cwd: "/srv/api"},
				{ID: "w2:p1", Workspace: "w2", Tab: "w2:t1", Cwd: "/srv/web"},
				{ID: "w2:p2", Workspace: "w2", Tab: "w2:t2", Cwd: "/var/log"},
			},
			Agents: []endpoint.Agent{{Pane: "w1:p2", Agent: "claude", Status: "blocked"}},
		},
	}
	tt := ui.NewTester(a.view, 1000, 600)
	for _, text := range []string{"api", "web", "claude", "blocked", "/srv/web", "logs"} {
		if !tt.HasText(text) {
			t.Errorf("no %q in %q", text, tt.Texts())
		}
	}
	if _, ok := tt.Find("New Workspace"); !ok {
		t.Error("no New Workspace button")
	}
	for _, text := range []string{"/srv/api", "/var/log"} {
		if tt.HasText(text) {
			t.Errorf("%q in %q, where the tab shows its agent or name", text, tt.Texts())
		}
	}
}

// Cmd+B and the title bar's button hide the sidebar and show it again.
func TestToggleSidebar(t *testing.T) {
	a := &app{term: term.New(terminalFont), sidebarWidth: 240}
	tt := ui.NewTester(a.view, 1000, 600)
	tt.Key(ui.Cmd, ui.KeyB)
	if !a.sidebarHidden {
		t.Fatal("Cmd+B left the sidebar shown")
	}
	if err := tt.Click("Show Sidebar"); err != nil {
		t.Fatal(err)
	}
	if a.sidebarHidden {
		t.Fatal("the button left the sidebar hidden")
	}
}

// Dragging the divider resizes the sidebar, within its bounds.
func TestResizeSidebar(t *testing.T) {
	a := &app{term: term.New(terminalFont), sidebarWidth: 240}
	tt := ui.NewTester(a.view, 1000, 600)
	tt.Press(240, 300)
	tt.Move(300, 300)
	tt.Release(300, 300)
	if a.sidebarWidth != 300 {
		t.Errorf("width %v after dragging 60 DIPs", a.sidebarWidth)
	}
	tt.Press(300, 300)
	tt.Move(900, 300)
	tt.Release(900, 300)
	if a.sidebarWidth != maxSidebarWidth {
		t.Errorf("width %v after dragging past the maximum", a.sidebarWidth)
	}
}

// A click on a workspace's name hides its tabs, and another shows them.
func TestCollapseWorkspace(t *testing.T) {
	a := &app{
		term:         term.New(terminalFont),
		sidebarWidth: 240,
		snap: endpoint.Snapshot{
			Workspaces: []endpoint.Workspace{{ID: "w1", Label: "api"}, {ID: "w2", Label: "web"}},
			Tabs:       []endpoint.Tab{{ID: "w1:t1", Workspace: "w1"}, {ID: "w2:t1", Workspace: "w2"}},
			Panes: []endpoint.Pane{
				{ID: "w1:p1", Workspace: "w1", Tab: "w1:t1", Cwd: "/srv/api"},
				{ID: "w2:p1", Workspace: "w2", Tab: "w2:t1", Cwd: "/srv/web"},
			},
		},
	}
	tt := ui.NewTester(a.view, 1000, 600)
	if err := tt.Click("api"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("/srv/api") {
		t.Fatal("the tabs hid at once, rather than sliding up")
	}
	tt.Advance(time.Second)
	if tt.HasText("/srv/api") || !tt.HasText("/srv/web") {
		t.Fatalf("after collapsing api: %q", tt.Texts())
	}
	if err := tt.Click("api"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(time.Second)
	if !tt.HasText("/srv/api") {
		t.Errorf("after expanding api: %q", tt.Texts())
	}
}

// A workspace's menu renames it in place, and closes it once confirmed.
func TestWorkspaceMenu(t *testing.T) {
	a := &app{
		term:         term.New(terminalFont),
		sidebarWidth: 240,
		snap: endpoint.Snapshot{
			Workspaces: []endpoint.Workspace{{ID: "w1", Label: "api"}},
			Tabs:       []endpoint.Tab{{ID: "w1:t1", Workspace: "w1"}},
			Panes:      []endpoint.Pane{{ID: "w1:p1", Workspace: "w1", Tab: "w1:t1", Cwd: "/srv/api"}},
		},
	}
	tt := ui.NewTester(a.view, 1000, 600)
	if err := tt.RightClick("api"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Rename"); err != nil {
		t.Fatalf("%v in %q", err, tt.Menu())
	}
	tt.Frame()
	if a.renaming != "w1" || a.newLabel != "api" || !tt.Focused("Workspace Name") {
		t.Fatalf("Rename edits %q from %q, field focused %v", a.renaming, a.newLabel, tt.Focused("Workspace Name"))
	}
	tt.Key(0, ui.KeyEscape)
	if a.renaming != "" || !tt.HasText("api") {
		t.Errorf("Escape left %q renaming, showing %q", a.renaming, tt.Texts())
	}

	if err := tt.RightClick("api"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Close Workspace…"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("Close “api”?") {
		t.Fatalf("no confirmation in %q", tt.Texts())
	}
	if err := tt.Click("Cancel"); err != nil {
		t.Fatal(err)
	}
	if a.closing != nil || tt.HasText("Close “api”?") {
		t.Errorf("Cancel left %+v closing", a.closing)
	}
}

// A tab's menu renames it in place, its row as tall as its item, from its
// own name, none for a tab herdr numbers.
func TestRenameTab(t *testing.T) {
	a := &app{
		term:         term.New(terminalFont),
		sidebarWidth: 240,
		snap: endpoint.Snapshot{
			Workspaces: []endpoint.Workspace{{ID: "w1", Label: "api"}},
			Tabs: []endpoint.Tab{{ID: "w1:t1", Workspace: "w1", Number: 1, Label: "1"},
				{ID: "w1:t2", Workspace: "w1", Number: 2, Label: "2"}, {ID: "w1:t3", Workspace: "w1", Number: 3, Label: "logs"}},
			Panes: []endpoint.Pane{
				{ID: "w1:p1", Workspace: "w1", Tab: "w1:t1", Cwd: "/srv/api"},
				{ID: "w1:p2", Workspace: "w1", Tab: "w1:t2", Cwd: "/var/log"},
				{ID: "w1:p3", Workspace: "w1", Tab: "w1:t3", Cwd: "/var/log"},
			},
		},
	}
	tt := ui.NewTester(a.view, 1000, 600)
	below, _ := tt.Find("/var/log")
	if err := tt.RightClick("/srv/api"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Rename"); err != nil {
		t.Fatalf("%v in %q", err, tt.Menu())
	}
	tt.Frame()
	if a.renaming != "w1:t1" || a.newLabel != "" || !tt.Focused("Tab Name") {
		t.Fatalf("Rename edits %q from %q, field focused %v", a.renaming, a.newLabel, tt.Focused("Tab Name"))
	}
	if r, _ := tt.Find("/var/log"); r.Y != below.Y {
		t.Errorf("the tab below moved from %v to %v", below.Y, r.Y)
	}
	tt.Key(0, ui.KeyEscape)
	if a.renaming != "" || !tt.HasText("/srv/api") {
		t.Errorf("Escape left %q renaming, showing %q", a.renaming, tt.Texts())
	}

	if err := tt.RightClick("logs"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Rename"); err != nil {
		t.Fatalf("%v in %q", err, tt.Menu())
	}
	tt.Frame()
	if a.renaming != "w1:t3" || a.newLabel != "logs" {
		t.Errorf("Rename edits %q from %q, want logs's name", a.renaming, a.newLabel)
	}
}

// A tab shows what its active pane runs: the one it last focused, else its
// first agent's; and the state of its agent most needing the user.
func TestTabActivePane(t *testing.T) {
	a := &app{
		term:         term.New(terminalFont),
		sidebarWidth: 240,
		// The tab's alone, without the list of agents.
		collapsed: map[string]bool{agentsSection: true},
		snap: endpoint.Snapshot{
			Workspaces: []endpoint.Workspace{{ID: "w1", Label: "api"}},
			Tabs:       []endpoint.Tab{{ID: "w1:t1", Workspace: "w1", Label: "1"}},
			Panes: []endpoint.Pane{
				{ID: "w1:p1", Tab: "w1:t1", Cwd: "/srv/api"},
				{ID: "w1:p2", Tab: "w1:t1"},
				{ID: "w1:p3", Tab: "w1:t1"},
			},
			Agents: []endpoint.Agent{
				{Pane: "w1:p2", Agent: "claude", Status: "done"},
				{Pane: "w1:p3", Agent: "codex", Status: "blocked"},
			},
		},
	}
	tt := ui.NewTester(a.view, 1000, 600)
	if !tt.HasText("claude") || !tt.HasText("blocked") || tt.HasText("codex") {
		t.Errorf("a tab not yet shown: %q, want its first agent", tt.Texts())
	}
	a.tabFocus = map[string]string{"w1:t1": "w1:p3"}
	tt.Frame()
	if !tt.HasText("codex") || !tt.HasText("blocked") || tt.HasText("claude") {
		t.Errorf("a tab that focused codex: %q", tt.Texts())
	}
	a.tabFocus["w1:t1"] = "w1:p1"
	tt.Frame()
	if !tt.HasText("/srv/api") || tt.HasText("codex") {
		t.Errorf("a tab that focused a shell: %q", tt.Texts())
	}
	// Whichever pane it focused, it shows the state most needing the user.
	if !tt.HasText("blocked") || tt.HasText("done") {
		t.Errorf("a tab with an agent blocked: %q", tt.Texts())
	}
}

// Closing a tab, by its button or its menu, asks the user first.
func TestCloseTabConfirms(t *testing.T) {
	a := &app{
		term:         term.New(terminalFont),
		sidebarWidth: 240,
		snap: endpoint.Snapshot{
			Workspaces: []endpoint.Workspace{{ID: "w1", Label: "api"}},
			Tabs:       []endpoint.Tab{{ID: "w1:t1", Workspace: "w1", Label: "logs"}},
			Panes:      []endpoint.Pane{{ID: "w1:p1", Workspace: "w1", Tab: "w1:t1", Cwd: "/var/log"}},
		},
	}
	tt := ui.NewTester(a.view, 1000, 600)
	r, _ := tt.Find("logs")
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
	if err := tt.Click("Close Tab"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("Close “logs”?") {
		t.Fatalf("no confirmation in %q", tt.Texts())
	}
	if err := tt.Click("Cancel"); err != nil {
		t.Fatal(err)
	}
	if a.closing != nil {
		t.Errorf("Cancel left %+v closing", a.closing)
	}

	if err := tt.RightClick("logs"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Close Tab…"); err != nil {
		t.Fatalf("%v in %q", err, tt.Menu())
	}
	if c := a.closing; c == nil || c.method != "tab.close" || c.params["tab_id"] != "w1:t1" || !tt.HasText("Close “logs”?") {
		t.Errorf("the menu's Close Tab… asks %+v, showing %q", c, tt.Texts())
	}
}

// A pane's title renames it in place, from its own name, which no name
// clears; meanwhile the title drags the pane no more.
func TestRenamePane(t *testing.T) {
	a := &app{
		term:         term.New(terminalFont),
		sidebarWidth: 240,
		snap: endpoint.Snapshot{
			Workspaces: []endpoint.Workspace{{ID: "w1", Label: "api"}},
			Tabs:       []endpoint.Tab{{ID: "w1:t1", Workspace: "w1", Label: "server"}},
			Panes:      []endpoint.Pane{{ID: "w1:p1", Workspace: "w1", Tab: "w1:t1", Label: "build", Cwd: "/srv/api"}},
		},
		surface: &endpoint.Surface{Frame: endpoint.Frame{Width: 80, Height: 20, Cells: make([]endpoint.Cell, 80*20)},
			Panes: []endpoint.SurfacePane{{ID: "w1:p1", Focused: true, Rect: endpoint.Rect{Width: 80, Height: 20},
				Inner: endpoint.Rect{X: 1, Y: 1, Width: 78, Height: 18}}}},
	}
	tt := ui.NewTester(a.view, 1000, 600)
	tt.Frame()
	if err := tt.RightClick("build"); err != nil {
		t.Fatalf("%v in %q", err, tt.Texts())
	}
	if err := tt.ChooseMenuItem("Rename"); err != nil {
		t.Fatalf("%v in %q", err, tt.Menu())
	}
	tt.Frame()
	if a.renaming != "w1:p1" || a.newLabel != "build" || !tt.Focused("Pane Name") {
		t.Fatalf("Rename edits %q from %q, field focused %v", a.renaming, a.newLabel, tt.Focused("Pane Name"))
	}
	// A press in the field places the caret, rather than dragging the pane.
	r, _ := tt.Find("Pane Name")
	tt.ClickAt(r.X+10, r.Y+r.H/2)
	tt.Frame()
	if a.renaming != "w1:p1" || !tt.Focused("Pane Name") {
		t.Errorf("a click in the field left renaming %q, field focused %v", a.renaming, tt.Focused("Pane Name"))
	}
	tt.Key(0, ui.KeyEscape)
	if a.renaming != "" || !tt.HasText("build") {
		t.Errorf("Escape left %q renaming, showing %q", a.renaming, tt.Texts())
	}
}

// A moving status dot moves as the window paints again, without the view
// built again.
func TestStatusDotMoves(t *testing.T) {
	a := &app{term: term.New(terminalFont), sidebarWidth: 240, snap: endpoint.Snapshot{
		Workspaces: []endpoint.Workspace{{ID: "w1", Label: "mux"}},
		Tabs:       []endpoint.Tab{{ID: "w1:t1", Workspace: "w1", Label: "1"}},
		Panes:      []endpoint.Pane{{ID: "p1", Tab: "w1:t1"}},
		Agents:     []endpoint.Agent{{Pane: "p1", Agent: "claude", Status: "working"}},
	}}
	builds := 0
	tt := ui.NewTester(func(c *ui.Context) { builds++; a.view(c) }, 600, 300)
	tt.Frame()
	before, n := tt.Image(), builds
	tt.Advance(time.Second / 2)
	if builds != n {
		t.Errorf("the dot's frames built the view %d times", builds-n)
	}
	if after := tt.Image(); bytes.Equal(before.Pix, after.Pix) {
		t.Error("the dot did not move")
	}
}

// The sidebar lists the agents after the workspaces, by their pane's name
// or their own, beside their workspace; choosing one chooses its pane.
func TestAgentsList(t *testing.T) {
	a := &app{
		term:         term.New(terminalFont),
		sidebarWidth: 300,
		snap: endpoint.Snapshot{
			Workspaces: []endpoint.Workspace{{ID: "w1", Label: "api"}, {ID: "w2", Label: "web"}},
			Tabs:       []endpoint.Tab{{ID: "w1:t1", Workspace: "w1", Label: "1"}, {ID: "w2:t1", Workspace: "w2", Label: "1"}},
			Panes: []endpoint.Pane{
				{ID: "w1:p1", Workspace: "w1", Tab: "w1:t1"},
				{ID: "w2:p1", Workspace: "w2", Tab: "w2:t1", Label: "reviewer"},
			},
			Agents: []endpoint.Agent{
				{Pane: "w1:p1", Agent: "claude", Status: "working"},
				{Pane: "w2:p1", Agent: "codex", Status: "blocked"},
			},
		},
		// The workspaces' tabs hidden, the agents' names show once.
		collapsed: map[string]bool{"w1": true, "w2": true},
	}
	tt := ui.NewTester(a.view, 1000, 600)
	for _, text := range []string{"Agents", "claude", "reviewer"} {
		if !tt.HasText(text) {
			t.Errorf("no %q in %q", text, tt.Texts())
		}
	}
	if err := tt.Click("reviewer"); err != nil {
		t.Fatal(err)
	}
	if a.chosen != "w2:p1" {
		t.Errorf("choosing reviewer chose %q", a.chosen)
	}
	if err := tt.Click("Agents"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(time.Second)
	tt.Frame()
	if tt.HasText("reviewer") {
		t.Errorf("collapsed, the agents show: %q", tt.Texts())
	}
}
