package main

import (
	"bytes"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"mux/src/herdr/endpoint"
	"mux/src/term"
)

// The sidebar groups tabs by workspace, a tab with an agent showing it and
// its state, others the directory of their first pane, unless named.
func TestSidebar(t *testing.T) {
	a := &app{
		term:         term.New(settings{}.terminalFont()),
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
	if _, ok := tt.Find("New Workspace"); ok {
		t.Error("a New Workspace button, where Cmd+N makes one")
	}
	for _, text := range []string{"/srv/api", "/var/log"} {
		if tt.HasText(text) {
			t.Errorf("%q in %q, where the tab shows its agent or name", text, tt.Texts())
		}
	}
}

// Cmd+B and the title bar's button hide the sidebar and show it again.
func TestToggleSidebar(t *testing.T) {
	a := &app{term: term.New(settings{}.terminalFont()), sidebarWidth: 240}
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
	a := &app{term: term.New(settings{}.terminalFont()), sidebarWidth: 240}
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
		term:         term.New(settings{}.terminalFont()),
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
		term:         term.New(settings{}.terminalFont()),
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
		term:         term.New(settings{}.terminalFont()),
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
		term:         term.New(settings{}.terminalFont()),
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
		term:         term.New(settings{}.terminalFont()),
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
		term:         term.New(settings{}.terminalFont()),
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
	a := &app{term: term.New(settings{}.terminalFont()), sidebarWidth: 240, snap: endpoint.Snapshot{
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
		term:         term.New(settings{}.terminalFont()),
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

// Cmd+, opens the settings, whose appearance takes effect as it is chosen,
// and is saved for the next launch.
func TestSettingsAppearance(t *testing.T) {
	mygo.App.SetPath(mygo.PathUserData, t.TempDir())
	var source mygo.ThemeSource
	setThemeSource = func(s mygo.ThemeSource) { source = s }
	t.Cleanup(func() { setThemeSource = mygo.Theme.SetSource })
	a := &app{term: term.New(settings{}.terminalFont()), sidebarWidth: 240}
	tt := ui.NewTester(a.view, 1000, 600)
	tt.Key(ui.Cmd, ui.KeyComma)
	if !tt.HasText("herdr server") {
		t.Fatalf("no server settings after Cmd+, in %q", tt.Texts())
	}
	if err := tt.Click("Appearance"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Dark"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.settings.Appearance != mygo.ThemeDark || source != mygo.ThemeDark {
		t.Fatalf("appearance %q, set %q, after choosing Dark", a.settings.Appearance, source)
	}
	if s := loadSettings(); s.Appearance != mygo.ThemeDark {
		t.Errorf("saved appearance %q, not dark", s.Appearance)
	}
	// A click outside keeps them open; the close button closes them.
	tt.ClickAt(20, 580)
	if !a.settingsOpen {
		t.Fatal("a click outside closed the settings")
	}
	if err := tt.Click("Close"); err != nil {
		t.Fatal(err)
	}
	if a.settingsOpen {
		t.Fatal("the close button left the settings open")
	}
	tt.Key(ui.Cmd, ui.KeyComma)
	tt.Key(0, ui.KeyEscape)
	if a.settingsOpen {
		t.Fatal("Escape left the settings open")
	}
	// Opened again, the dialog shows the choice.
	a.appearance = 0
	tt.Key(ui.Cmd, ui.KeyComma)
	if a.appearance != 2 {
		t.Errorf("appearance control at %d, not Dark", a.appearance)
	}
}

// Without a server, the panes' room offers to start one, as the settings
// do, and the sidebar no longer shows what the server showed.
func TestServerDown(t *testing.T) {
	a := &app{term: term.New(settings{}.terminalFont()), sidebarWidth: 240, serverDown: true, err: "dial unix: no such file"}
	tt := ui.NewTester(a.view, 1000, 600)
	if !tt.HasText("The herdr server isn’t running") {
		t.Fatalf("no notice in %q", tt.Texts())
	}
	if _, ok := tt.Find("Start Server"); !ok {
		t.Error("no Start Server button")
	}
	if tt.HasText(a.err) {
		t.Error("the dial error shows beside the notice")
	}
	tt.Key(ui.Cmd, ui.KeyComma)
	if !tt.HasText("Not running") {
		t.Errorf("no server status in %q", tt.Texts())
	}
}

// The settings choose the terminals' font among the monospaced families,
// and save it for the next launch.
func TestSettingsTerminalFont(t *testing.T) {
	mygo.App.SetPath(mygo.PathUserData, t.TempDir())
	a := &app{term: term.New(settings{}.terminalFont()), sidebarWidth: 240, monoFonts: []string{systemFont, "Berkeley Mono", "Menlo"},
		allFonts: []string{noFont, "Menlo", "Symbols Nerd Font"}}
	tt := ui.NewTester(a.view, 1000, 600)
	tt.Key(ui.Cmd, ui.KeyComma)
	if err := tt.Click("Appearance"); err != nil {
		t.Fatal(err)
	}
	if f := (settings{}).terminalFont().Family; f != "monospace" {
		t.Errorf("default terminal font families %q", f)
	}
	if a.terminalFamily != systemFont || a.fallbackFamily != noFont {
		t.Errorf("font controls at %q and %q, not the defaults", a.terminalFamily, a.fallbackFamily)
	}
	// The control lines up with the theme's on the right of its row.
	dark, _ := tt.Find("Dark")
	label, _ := tt.Find("Terminal Font")
	note, _ := tt.Find("The monospaced font the terminals use.")
	tt.ClickAt(dark.X, (label.Y+note.Y+note.H)/2)
	if err := tt.Click("Menlo"); err != nil {
		t.Fatalf("%v in %q", err, tt.Texts())
	}
	tt.Frame()
	if a.settings.TerminalFont != "Menlo" {
		t.Fatalf("terminal font %q after choosing Menlo", a.settings.TerminalFont)
	}
	// The fallback font's control lies below.
	label, _ = tt.Find("Fallback Font")
	note, _ = tt.Find("Fills in what it lacks, as Nerd Font icons.")
	tt.ClickAt(dark.X, (label.Y+note.Y+note.H)/2)
	if err := tt.Click("Symbols Nerd Font"); err != nil {
		t.Fatalf("%v in %q", err, tt.Texts())
	}
	tt.Frame()
	s := loadSettings()
	if s.TerminalFont != "Menlo" || s.FallbackFont != "Symbols Nerd Font" {
		t.Errorf("saved fonts %q and %q", s.TerminalFont, s.FallbackFont)
	}
	if f := s.terminalFont().Family; f != "Menlo, Symbols Nerd Font" {
		t.Errorf("terminal font families %q", f)
	}
}

// The settings change the terminals' font size as typed in its field, by
// steps of the arrows there or of the stepper beside it, and save it.
func TestSettingsFontSize(t *testing.T) {
	mygo.App.SetPath(mygo.PathUserData, t.TempDir())
	a := &app{term: term.New(settings{}.terminalFont()), sidebarWidth: 240}
	tt := ui.NewTester(a.view, 1000, 600)
	tt.Key(ui.Cmd, ui.KeyComma)
	if err := tt.Click("Appearance"); err != nil {
		t.Fatal(err)
	}
	if a.fontSize != defaultFontSize {
		t.Errorf("size control at %v, not the default", a.fontSize)
	}
	saved := func() float64 { return loadSettings().FontSize }
	// The field lies under the theme control's last segment, the stepper
	// right of it.
	dark, _ := tt.Find("Dark")
	label, _ := tt.Find("Font Size")
	note, _ := tt.Find("The terminals’ text size, in points.")
	y := (label.Y + note.Y + note.H) / 2
	tt.ClickAt(dark.X, y)
	tt.Key(0, ui.KeyUp)
	tt.Key(0, ui.KeyUp)
	tt.Frame()
	if saved() != 16 {
		t.Errorf("saved size %v after two steps up", saved())
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("20")
	tt.Frame()
	if saved() != 20 || a.settings.terminalFont().Size != 20 {
		t.Errorf("saved size %v after typing 20", saved())
	}
	tt.Type("0")
	tt.Frame()
	if saved() != 20 {
		t.Errorf("saved size %v after typing 200, out of range", saved())
	}
	tt.ClickAt(dark.X+dark.W+6, y+6)
	tt.Frame()
	if saved() != 19 || a.fontSizeText != "19" {
		t.Errorf("saved size %v, field %q, after the stepper's down arrow", saved(), a.fontSizeText)
	}
}

// Cmd+T makes a tab in the focused workspace, showing its tabs, and
// Cmd+N a workspace, each giving the keyboard to the terminal, wherever
// the focus is; Cmd+W closes the focused tab once confirmed.
func TestFileCommands(t *testing.T) {
	a := &app{
		term:         term.New(settings{}.terminalFont()),
		sidebarWidth: 240,
		collapsed:    map[string]bool{"w2": true},
		snap: endpoint.Snapshot{
			FocusedWorkspace: "w2", FocusedTab: "w2:t1",
			Workspaces: []endpoint.Workspace{{ID: "w1", Label: "api"}, {ID: "w2", Label: "web"}},
			Tabs:       []endpoint.Tab{{ID: "w1:t1", Workspace: "w1"}, {ID: "w2:t1", Workspace: "w2"}},
			Panes: []endpoint.Pane{{ID: "w1:p1", Workspace: "w1", Tab: "w1:t1", Cwd: "/srv/api"},
				{ID: "w2:p1", Workspace: "w2", Tab: "w2:t1", Cwd: "/srv/web"}},
		},
	}
	tt := ui.NewTester(a.view, 1000, 600)
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyT)
	if a.collapsed["w2"] {
		t.Error("Cmd+T in the terminal left w2's tabs hidden")
	}

	a.collapse("w2", true)
	if err := tt.Click("/srv/api"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tt.Focused("Terminal") {
		t.Fatal("a click on a tab left the keyboard to the terminal")
	}
	tt.Key(ui.Cmd, ui.KeyT)
	tt.Frame()
	if a.collapsed["w2"] || !tt.Focused("Terminal") {
		t.Errorf("Cmd+T in the sidebar: w2 collapsed %v, terminal focused %v", a.collapsed["w2"], tt.Focused("Terminal"))
	}

	if err := tt.Click("/srv/api"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyN)
	tt.Frame()
	if !tt.Focused("Terminal") {
		t.Error("Cmd+N in the sidebar left the keyboard there")
	}

	tt.Key(ui.Cmd, ui.KeyW)
	if !tt.HasText("Close “/srv/web”?") {
		t.Errorf("Cmd+W: no confirmation in %q", tt.Texts())
	}
}

// Cmd+Shift+[ and ] go around the focused workspace's tabs, Cmd+1 to 8 to
// one of them, Cmd+9 to its last.
func TestSwitchTab(t *testing.T) {
	a := &app{snap: endpoint.Snapshot{
		FocusedWorkspace: "w2", FocusedTab: "w2:t1",
		Tabs: []endpoint.Tab{{ID: "w1:t1", Workspace: "w1"}, {ID: "w2:t1", Workspace: "w2"},
			{ID: "w2:t2", Workspace: "w2"}, {ID: "w2:t3", Workspace: "w2"}},
	}}
	for _, c := range []struct {
		got, want string
	}{
		{a.tabBeside(1), "w2:t2"}, {a.tabBeside(-1), "w2:t3"}, {a.tabBeside(3), "w2:t1"},
		{a.tabNumber(2), "w2:t2"}, {a.tabNumber(9), "w2:t3"}, {a.tabNumber(4), ""},
	} {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
	a.snap.FocusedTab = ""
	if got := a.tabBeside(1); got != "" {
		t.Errorf("with no tab focused, the next is %q", got)
	}
}
