package main

import (
	"bytes"
	"os"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"mux/internal/hc"
	"mux/internal/layout"
	"mux/internal/term"
)

// newApp is an app showing a layout of terminals, with no connection.
func newApp(l layout.Layout, terms ...hc.Terminal) *app {
	return &app{term: term.New(settings{}.terminalFont(), lightTerminal, darkTerminal), sidebarWidth: 240,
		lay: l, terms: hc.Terminals{List: terms}}
}

// shell is a live terminal running a shell in cwd.
func shell(address, cwd string) hc.Terminal {
	return hc.Terminal{Address: address, State: "live", Cwd: cwd, Foreground: &hc.Foreground{Name: "zsh", Cwd: cwd}}
}

// agent is a live terminal whose program reports its state as app.
func agent(address, cwd, app, state string) hc.Terminal {
	t := shell(address, cwd)
	t.Foreground.Name, t.Status = "node", &hc.Status{State: state, App: app}
	return t
}

// tab is a tab of panes side by side, the first focused.
func tab(id string, terminals ...string) *layout.Tab {
	t := &layout.Tab{ID: id, Root: &layout.Node{Terminal: terminals[0]}, Focus: terminals[0]}
	for i := 1; i < len(terminals); i++ {
		t.Split(terminals[i-1], terminals[i], layout.Right)
	}
	t.Focus = terminals[0]
	return t
}

// The sidebar groups tabs by workspace, a tab with an agent focused
// showing it and its state, others the directory of their focused pane,
// unless named.
func TestSidebar(t *testing.T) {
	api := tab("tab1", "t2", "t1")
	logs := tab("tab3", "t4")
	logs.Name = "logs"
	a := newApp(layout.Layout{Active: "tab2", Workspaces: []*layout.Workspace{
		{ID: "w1", Name: "api", Tabs: []*layout.Tab{api}},
		{ID: "w2", Tabs: []*layout.Tab{tab("tab2", "t3"), logs}},
	}}, shell("t1", "/srv/api"), agent("t2", "/srv/api", "claude-code", "blocked"), shell("t3", "/srv/web"), shell("t4", "/var/log"))
	a.collapse(agentsSection, true)
	tt := ui.NewTester(a.view, 1000, 600)
	for _, text := range []string{"api", "web", "Claude Code", "blocked", "/srv/web", "logs"} {
		if !tt.HasText(text) {
			t.Errorf("no %q in %q", text, tt.Texts())
		}
	}
	for _, text := range []string{"/srv/api", "/var/log"} {
		if tt.HasText(text) {
			t.Errorf("%q in %q, where the tab shows its agent or name", text, tt.Texts())
		}
	}
}

// Cmd+B and the title bar's button hide the sidebar and show it again.
func TestToggleSidebar(t *testing.T) {
	a := newApp(layout.Layout{})
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
	a := newApp(layout.Layout{})
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

// twoWorkspaces is api's tab of /srv/api and web's of /srv/web, web's
// shown.
func twoWorkspaces() *app {
	return newApp(layout.Layout{Active: "tab2", Workspaces: []*layout.Workspace{
		{ID: "w1", Tabs: []*layout.Tab{tab("tab1", "t1")}},
		{ID: "w2", Tabs: []*layout.Tab{tab("tab2", "t2")}},
	}}, shell("t1", "/srv/api"), shell("t2", "/srv/web"))
}

// A workspace is called after the folder of its first pane, and a click on
// its name hides its tabs, and another shows them.
func TestCollapseWorkspace(t *testing.T) {
	a := twoWorkspaces()
	tt := ui.NewTester(a.view, 1000, 600)
	if err := tt.Click("api"); err != nil {
		t.Fatalf("%v in %q", err, tt.Texts())
	}
	if !tt.HasText("/srv/api") {
		t.Fatal("the tabs hid at once, rather than sliding up")
	}
	tt.Advance(time.Second)
	if tt.HasText("/srv/api") {
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
	a := twoWorkspaces()
	tt := ui.NewTester(a.view, 1000, 600)
	if err := tt.RightClick("api"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Rename"); err != nil {
		t.Fatalf("%v in %q", err, tt.Menu())
	}
	tt.Frame()
	if a.renaming != "w1" || a.newLabel != "" || !tt.Focused("Workspace Name") {
		t.Fatalf("Rename edits %q from %q, field focused %v", a.renaming, a.newLabel, tt.Focused("Workspace Name"))
	}
	tt.Type("backend")
	tt.Key(0, ui.KeyEnter)
	if a.renaming != "" || a.lay.Workspaces[0].Name != "backend" || !tt.HasText("backend") {
		t.Errorf("Enter left %q renaming, named %q, showing %q", a.renaming, a.lay.Workspaces[0].Name, tt.Texts())
	}

	if err := tt.RightClick("backend"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Close Workspace…"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("Close “backend”?") || len(a.closing.terminals) != 1 || a.closing.terminals[0] != "t1" {
		t.Fatalf("no confirmation in %q, closing %+v", tt.Texts(), a.closing)
	}
	if err := tt.Click("Cancel"); err != nil {
		t.Fatal(err)
	}
	if a.closing != nil || tt.HasText("Close “backend”?") {
		t.Errorf("Cancel left %+v closing", a.closing)
	}
}

// A tab's menu renames it in place, from its own name, none for a tab
// called after its pane.
func TestRenameTab(t *testing.T) {
	a := twoWorkspaces()
	tt := ui.NewTester(a.view, 1000, 600)
	if err := tt.RightClick("/srv/api"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Rename"); err != nil {
		t.Fatalf("%v in %q", err, tt.Menu())
	}
	tt.Frame()
	if a.renaming != "tab1" || a.newLabel != "" || !tt.Focused("Tab Name") {
		t.Fatalf("Rename edits %q from %q, field focused %v", a.renaming, a.newLabel, tt.Focused("Tab Name"))
	}
	tt.Type("server")
	tt.Key(0, ui.KeyEnter)
	if _, tb := a.lay.Tab("tab1"); tb.Name != "server" || !tt.HasText("server") {
		t.Errorf("tab named %q, showing %q", tb.Name, tt.Texts())
	}
}

// Closing a tab, by its button or its menu, asks the user first.
func TestCloseTabConfirms(t *testing.T) {
	a := twoWorkspaces()
	tt := ui.NewTester(a.view, 1000, 600)
	r, _ := tt.Find("/srv/api")
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
	if err := tt.Click("Close Tab"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("Close “/srv/api”?") {
		t.Fatalf("no confirmation in %q", tt.Texts())
	}
	if err := tt.Click("Cancel"); err != nil {
		t.Fatal(err)
	}
	if a.closing != nil {
		t.Errorf("Cancel left %+v closing", a.closing)
	}
	if err := tt.RightClick("/srv/api"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Close Tab…"); err != nil {
		t.Fatalf("%v in %q", err, tt.Menu())
	}
	if c := a.closing; c == nil || len(c.terminals) != 1 || c.terminals[0] != "t1" {
		t.Errorf("the menu's Close Tab… asks %+v", c)
	}
}

// As the tab chosen closes, or the pane of the agent chosen, the tab shown
// in its place shows chosen in the sidebar.
func TestClosedTabLeavesShownChosen(t *testing.T) {
	a := newApp(layout.Layout{Active: "tab1", Workspaces: []*layout.Workspace{
		{ID: "w1", Tabs: []*layout.Tab{tab("tab1", "t1"), tab("tab2", "t2"), tab("tab3", "t3")}},
	}}, shell("t1", "/srv/api"), agent("t2", "/srv/web", "codex", "idle"), shell("t3", "/srv/db"))
	tt := ui.NewTester(a.view, 1000, 600)
	if a.chosen != "tab1" {
		t.Fatalf("chose %q", a.chosen)
	}
	gone := func(address string) {
		a.terms.List = slices.DeleteFunc(a.terms.List, func(t hc.Terminal) bool { return t.Address == address })
		a.lay.Prune(a.live)
		tt.Frame()
	}
	gone("t1")
	if a.lay.Active != "tab2" || a.chosen != "tab2" {
		t.Errorf("tab1 closed: showing %q, chosen %q", a.lay.Active, a.chosen)
	}
	a.showTerminal("t2")
	tt.Frame()
	if a.chosen != "t2" {
		t.Fatalf("choosing the agent chose %q", a.chosen)
	}
	gone("t2")
	if a.lay.Active != "tab3" || a.chosen != "tab3" {
		t.Errorf("the agent's pane closed: showing %q, chosen %q", a.lay.Active, a.chosen)
	}
}

// A pane's title renames it in place, from its own name, which no name
// clears; meanwhile the title drags the pane no more.
func TestRenamePane(t *testing.T) {
	a := twoWorkspaces()
	// The sidebar calls the tab by its own name, the header the pane by its.
	a.lay.Workspaces[1].Tabs[0].Name = "server"
	a.lay.Workspaces[1].Tabs[0].Root.Name = "build"
	tt := ui.NewTester(a.view, 1000, 600)
	tt.Frame()
	if err := tt.RightClick("build"); err != nil {
		t.Fatalf("%v in %q", err, tt.Texts())
	}
	if err := tt.ChooseMenuItem("Rename"); err != nil {
		t.Fatalf("%v in %q", err, tt.Menu())
	}
	tt.Frame()
	if a.renaming != "t2" || a.newLabel != "build" || !tt.Focused("Pane Name") {
		t.Fatalf("Rename edits %q from %q, field focused %v", a.renaming, a.newLabel, tt.Focused("Pane Name"))
	}
	// A press in the field places the caret, rather than dragging the pane.
	r, _ := tt.Find("Pane Name")
	tt.ClickAt(r.X+10, r.Y+r.H/2)
	tt.Frame()
	if a.renaming != "t2" || !tt.Focused("Pane Name") {
		t.Errorf("a click in the field left renaming %q, field focused %v", a.renaming, tt.Focused("Pane Name"))
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Key(0, ui.KeyBackspace)
	tt.Key(0, ui.KeyEnter)
	if n := a.lay.Workspaces[1].Tabs[0].Root; a.renaming != "" || n.Name != "" || !tt.HasText("zsh") {
		t.Errorf("cleared, renaming %q, named %q, showing %q", a.renaming, n.Name, tt.Texts())
	}
}

// A moving status dot moves as the window paints again, without the view
// built again.
func TestStatusDotMoves(t *testing.T) {
	a := newApp(layout.Layout{Workspaces: []*layout.Workspace{{ID: "w1", Tabs: []*layout.Tab{tab("tab1", "t1")}}}},
		agent("t1", "/src", "claude-code", "working"))
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

// A done agent the user looked at shows idle, also as it reports done
// again, and done once more after its next turn.
func TestDoneTurnsIdle(t *testing.T) {
	a := newApp(layout.Layout{Workspaces: []*layout.Workspace{{ID: "w1", Tabs: []*layout.Tab{tab("tab1", "t1", "t2")}}}},
		agent("t1", "/srv/api", "claude-code", "done"), agent("t2", "/srv/web", "codex", "done"))
	a.seen = map[string]bool{"t2": true}
	if a.state("t1") != "done" || a.state("t2") != "idle" {
		t.Fatalf("t1 %q, t2 %q", a.state("t1"), a.state("t2"))
	}
	status := func(state string) {
		a.inbox = []hc.Event{{Method: "event", Seq: a.terms.Seq + 1, Type: "status", Terminal: "t2",
			Data: []byte(`{"status":{"state":"` + state + `","app":"codex"}}`)}}
		a.receive()
	}
	status("done")
	if got := a.state("t2"); got != "idle" {
		t.Errorf("done again, the agent is %q", got)
	}
	status("working")
	status("done")
	if got := a.state("t2"); got != "done" {
		t.Errorf("after its next turn, the agent is %q", got)
	}
}

// The sidebar lists the agents after the workspaces, by their pane's name
// or their own, beside their workspace; choosing one shows its pane, and
// one without a pane gets a tab.
func TestAgentsList(t *testing.T) {
	web := tab("tab2", "t2")
	web.Root.Name = "reviewer"
	a := newApp(layout.Layout{Active: "tab1", Workspaces: []*layout.Workspace{
		{ID: "w1", Name: "api", Tabs: []*layout.Tab{tab("tab1", "t1")}},
		{ID: "w2", Name: "web", Tabs: []*layout.Tab{web}},
	}}, agent("t1", "/srv/api", "claude-code", "working"), agent("t2", "/srv/web", "codex", "blocked"),
		agent("t9", "/srv/web", "codex", "idle"))
	a.terms.List[2].Parent = "t1"
	// The workspaces' tabs hidden, the agents' names show once.
	a.collapsed = map[string]bool{"w1": true, "w2": true}
	a.sidebarWidth = 300
	tt := ui.NewTester(a.view, 1000, 600)
	for _, text := range []string{"Agents", "Claude Code", "reviewer", "Codex"} {
		if !tt.HasText(text) {
			t.Errorf("no %q in %q", text, tt.Texts())
		}
	}
	if err := tt.Click("reviewer"); err != nil {
		t.Fatal(err)
	}
	if a.chosen != "t2" || a.lay.Active != "tab2" {
		t.Errorf("choosing reviewer chose %q, showing %q", a.chosen, a.lay.Active)
	}
	if err := tt.Click("Codex"); err != nil {
		t.Fatal(err)
	}
	if ws, tb, _ := a.lay.Find("t9"); tb == nil || ws.ID != "w2" || a.lay.Active != tb.ID {
		t.Errorf("an agent without a pane: %+v", a.lay)
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
	a := newApp(layout.Layout{})
	tt := ui.NewTester(a.view, 1000, 600)
	tt.Key(ui.Cmd, ui.KeyComma)
	if !tt.HasText("herdr-core") {
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
}

// Without herdr-core, the panes' room says how to install it; stopped,
// it offers to start it, as the settings do.
func TestServerDown(t *testing.T) {
	a := newApp(layout.Layout{})
	a.notInstalled, a.err = true, hc.ErrNotInstalled.Error()
	tt := ui.NewTester(a.view, 1000, 600)
	if !tt.HasText("herdr-core isn’t installed") || !tt.HasText(hc.InstallCommand) {
		t.Fatalf("no notice in %q", tt.Texts())
	}
	if tt.HasText(a.err) {
		t.Error("the error shows beside the notice")
	}
	a.notInstalled = false
	a.gate.stop()
	tt.Frame()
	if !tt.HasText("herdr-core is stopped") {
		t.Fatalf("no notice in %q", tt.Texts())
	}
	if err := tt.Click("Start Server"); err != nil {
		t.Fatal(err)
	}
	if a.stopped() || !a.starting {
		t.Error("Start Server left the core stopped")
	}
	tt.Key(ui.Cmd, ui.KeyComma)
	if !tt.HasText("Starting…") {
		t.Errorf("no server status in %q", tt.Texts())
	}
}

// The settings choose the terminals' font among the monospaced families,
// and save it for the next launch.
func TestSettingsTerminalFont(t *testing.T) {
	mygo.App.SetPath(mygo.PathUserData, t.TempDir())
	a := newApp(layout.Layout{})
	a.monoFonts, a.allFonts = []string{systemFont, "Berkeley Mono", "Menlo"}, []string{noFont, "Menlo", "Symbols Nerd Font"}
	tt := ui.NewTester(a.view, 1000, 600)
	openTerminalSettings(tt)
	if f := (settings{}).terminalFont().Family; f != "monospace" {
		t.Errorf("default terminal font families %q", f)
	}
	if a.terminalFamily != systemFont || a.fallbackFamily != noFont {
		t.Errorf("font controls at %q and %q, not the defaults", a.terminalFamily, a.fallbackFamily)
	}
	// The control lies right of its label.
	label, _ := tt.Find("Terminal Font")
	note, _ := tt.Find("The monospaced font the terminals use.")
	tt.ClickAt(label.X+label.W+30, (label.Y+note.Y+note.H)/2)
	if err := tt.Click("Menlo"); err != nil {
		t.Fatalf("%v in %q", err, tt.Texts())
	}
	tt.Frame()
	if a.settings.TerminalFont != "Menlo" || a.term.Font().Family != "Menlo" {
		t.Fatalf("terminal font %q after choosing Menlo", a.settings.TerminalFont)
	}
	// The fallback font's control lies below.
	label, _ = tt.Find("Fallback Font")
	note, _ = tt.Find("Fills in what it lacks, as Nerd Font icons.")
	tt.ClickAt(label.X+label.W+30, (label.Y+note.Y+note.H)/2)
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
	a := newApp(layout.Layout{})
	tt := ui.NewTester(a.view, 1000, 600)
	openTerminalSettings(tt)
	if a.fontSize != defaultFontSize {
		t.Errorf("size control at %v, not the default", a.fontSize)
	}
	saved := func() float64 { return loadSettings().FontSize }
	// The field lies right of the label, 48 wide, the stepper right of it.
	label, _ := tt.Find("Font Size")
	note, _ := tt.Find("The terminals’ text size, in points.")
	x, y := label.X+label.W+16, (label.Y+note.Y+note.H)/2
	tt.ClickAt(x+10, y)
	tt.Key(0, ui.KeyUp)
	tt.Key(0, ui.KeyUp)
	tt.Frame()
	if saved() != 16 {
		t.Errorf("saved size %v after two steps up", saved())
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("20")
	tt.Frame()
	if saved() != 20 || a.settings.terminalFont().Size != 20 || a.term.Font().Size != 20 {
		t.Errorf("saved size %v after typing 20", saved())
	}
	tt.Type("0")
	tt.Frame()
	if saved() != 20 {
		t.Errorf("saved size %v after typing 200, out of range", saved())
	}
	// The preview above grew with the size, moving the row down.
	label, _ = tt.Find("Font Size")
	note, _ = tt.Find("The terminals’ text size, in points.")
	tt.ClickAt(x+48+4+6, (label.Y+note.Y+note.H)/2+6)
	tt.Frame()
	if saved() != 19 || a.fontSizeText != "19" {
		t.Errorf("saved size %v, field %q, after the stepper's down arrow", saved(), a.fontSizeText)
	}
}

// On macOS, the settings thicken the terminals' text, or not, as by
// default, and save the choice.
func TestSettingsThicken(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("thickening is macOS's")
	}
	mygo.App.SetPath(mygo.PathUserData, t.TempDir())
	a := newApp(layout.Layout{})
	tt := ui.NewTester(a.view, 1000, 600)
	openTerminalSettings(tt)
	if a.settings.terminalFont().Thicken {
		t.Error("the terminals' text thickened by default")
	}
	// The switch lies at the right of its row, as the controls above end.
	label, _ := tt.Find("Thicken Text")
	note, _ := tt.Find("Draws text heavier, with macOS font smoothing at full strength.")
	tt.ClickAt(label.X+label.W+30, (label.Y+note.Y+note.H)/2)
	tt.Frame()
	if s := loadSettings(); !s.FontSmoothing || !s.terminalFont().Thicken {
		t.Errorf("saved thickening %v after turning it on", s.FontSmoothing)
	}
}

// openTerminalSettings opens the settings on the terminal page, whose item
// lies below the appearance page's.
func openTerminalSettings(tt *ui.Tester) {
	tt.Key(ui.Cmd, ui.KeyComma)
	server, _ := tt.Find("Server")
	appearance, _ := tt.Find("Appearance")
	tt.ClickAt(appearance.X+appearance.W/2, 2*appearance.Y-server.Y+appearance.H/2)
}

// Cmd+Shift+[ and ] go around all the tabs, past a workspace's last to the
// next one's first, opening it if collapsed; Cmd+1 to 8 go to one of the
// shown workspace's tabs, Cmd+9 to its last.
func TestSwitchTab(t *testing.T) {
	a := newApp(layout.Layout{Active: "a", Workspaces: []*layout.Workspace{
		{ID: "w1", Tabs: []*layout.Tab{tab("x", "t0")}},
		{ID: "w2", Tabs: []*layout.Tab{tab("a", "t1"), tab("b", "t2"), tab("c", "t3")}},
	}})
	for _, c := range []struct {
		got, want string
	}{
		{a.tabBeside(1), "b"}, {a.tabBeside(-1), "x"}, {a.tabBeside(3), "x"}, {a.tabBeside(4), "a"},
		{a.tabNumber(2), "b"}, {a.tabNumber(9), "c"}, {a.tabNumber(4), ""},
	} {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
	tt := ui.NewTester(a.view, 1000, 600)
	tt.Key(ui.Cmd|ui.Shift, ui.KeyBracketRight)
	if a.lay.Active != "b" {
		t.Errorf("Cmd+Shift+] showed %q", a.lay.Active)
	}
	tt.Key(ui.Cmd, ui.Key9)
	if a.lay.Active != "c" {
		t.Errorf("Cmd+9 showed %q", a.lay.Active)
	}
	a.collapse("w1", true)
	tt.Key(ui.Cmd|ui.Shift, ui.KeyBracketRight)
	if a.lay.Active != "x" || a.collapsed["w1"] {
		t.Errorf("Cmd+Shift+] from the last tab showed %q, w1 collapsed: %v", a.lay.Active, a.collapsed["w1"])
	}
	tt.Key(ui.Cmd|ui.Shift, ui.KeyBracketLeft)
	if a.lay.Active != "c" {
		t.Errorf("Cmd+Shift+[ from the first tab showed %q", a.lay.Active)
	}
}

// A tab dragged in the sidebar goes before the tab it is dropped on the
// upper half of, after one it is dropped on the lower half of, of its
// workspace or another, and after a workspace's tabs dropped on its name;
// a workspace it leaves with no tab closes.
func TestDragTab(t *testing.T) {
	a := newApp(layout.Layout{Active: "tab1", Workspaces: []*layout.Workspace{
		{ID: "w1", Name: "api", Tabs: []*layout.Tab{tab("tab1", "t1"), tab("tab3", "t3")}},
		{ID: "w2", Name: "web", Tabs: []*layout.Tab{tab("tab2", "t2")}},
	}}, shell("t1", "/srv/api"), shell("t2", "/srv/web"), shell("t3", "/srv/docs"))
	a.collapse(agentsSection, true)
	tt := ui.NewTester(a.view, 1000, 600)
	order := func() string {
		var out []string
		for _, ws := range a.lay.Workspaces {
			var tabs []string
			for _, t := range ws.Tabs {
				tabs = append(tabs, t.ID)
			}
			out = append(out, ws.ID+":"+strings.Join(tabs, ","))
		}
		return strings.Join(out, " ")
	}
	drag := func(from, to string, lower bool) {
		t.Helper()
		f, ok := tt.Find(from)
		if !ok {
			t.Fatalf("no %q in %q", from, tt.Texts())
		}
		r, ok := tt.Find(to)
		if !ok {
			t.Fatalf("no %q in %q", to, tt.Texts())
		}
		y := r.Y + 2
		if lower {
			y = r.Y + r.H - 2
		}
		tt.Press(f.X+10, f.Y+f.H/2)
		tt.Move(f.X+20, f.Y+f.H/2+10)
		tt.Move(r.X+10, y)
		tt.Release(r.X+10, y)
		tt.Advance(time.Second)
	}
	// A drag of a single move, its release in the frame it starts in.
	f, _ := tt.Find("/srv/docs")
	r, _ := tt.Find("/srv/api")
	tt.Press(f.X+10, f.Y+f.H/2)
	tt.Move(r.X+10, r.Y+2)
	tt.Release(r.X+10, r.Y+2)
	tt.Advance(time.Second)
	if got := order(); got != "w1:tab3,tab1 w2:tab2" {
		t.Fatalf("before the first tab: %s", got)
	}
	drag("/srv/docs", "/srv/web", true)
	if got := order(); got != "w1:tab1 w2:tab2,tab3" {
		t.Fatalf("after another workspace's tab: %s", got)
	}
	// Released in the item's other half before a frame showed the line
	// there, the tab goes where the pointer let go.
	f, _ = tt.Find("/srv/docs")
	r, _ = tt.Find("/srv/web")
	tt.Press(f.X+10, f.Y+f.H/2)
	tt.Move(f.X+20, f.Y+f.H/2-10)
	tt.Move(r.X+10, r.Y+r.H-2)
	tt.Release(r.X+10, r.Y+2)
	if got := order(); got != "w1:tab1 w2:tab3,tab2" {
		t.Fatalf("released above the line shown below: %s", got)
	}
	a.collapse("w2", true)
	drag("/srv/api", "web", false)
	if got := order(); got != "w2:tab3,tab2,tab1" || a.collapsed["w2"] {
		t.Fatalf("dropped on a workspace's name: %s, collapsed %v", got, a.collapsed["w2"])
	}
	if a.lay.Active != "tab1" || a.chosen != "tab1" {
		t.Errorf("moved, the tab shown is %q, chosen %q", a.lay.Active, a.chosen)
	}
}

func TestGitBranch(t *testing.T) {
	dir := t.TempDir()
	write := func(path, data string) {
		t.Helper()
		if err := mkdirWrite(dir+"/"+path, data); err != nil {
			t.Fatal(err)
		}
	}
	write("repo/.git/HEAD", "ref: refs/heads/main\n")
	write("repo/src/x", "")
	write("wt/.git", "gitdir: ../repo/.git/worktrees/wt\n")
	write("repo/.git/worktrees/wt/HEAD", "0123456789abcdef\n")
	for _, c := range []struct{ dir, want string }{
		{dir + "/repo/src", "main"}, {dir + "/wt", "0123456"}, {dir, ""},
	} {
		if got := gitBranch(c.dir, readFrom(c.dir)); got != c.want {
			t.Errorf("gitBranch(%s) = %q, want %q", c.dir, got, c.want)
		}
	}
}

// On another machine, as Windows, the branch is read by paths from the
// folder, which the core resolves there, up to the root and no further.
func TestRemoteGitBranch(t *testing.T) {
	for _, c := range []struct {
		dir   string
		files map[string]string
		want  string
		reads int
	}{
		{`C:\Users\me\repo\src`, map[string]string{"../.git/HEAD": "ref: refs/heads/dev\r\n"}, "dev", 3},
		{`C:\wt`, map[string]string{".git": "gitdir: C:/repo/.git/worktrees/wt\n", "C:/repo/.git/worktrees/wt/HEAD": "0123456789abcdef\n"}, "0123456", 3},
		{`C:\Users`, nil, "", 6},
	} {
		reads := 0
		read := func(path string) ([]byte, error) {
			reads++
			if data, ok := c.files[path]; ok {
				return []byte(data), nil
			}
			return nil, os.ErrNotExist
		}
		if got := gitBranch(c.dir, read); got != c.want || reads != c.reads {
			t.Errorf("gitBranch(%s) = %q in %d reads, want %q in %d", c.dir, got, reads, c.want, c.reads)
		}
	}
}

// A terminal of another machine's shows where it is after the machine's
// name, and its workspace is called after its folder there.
func TestRemoteNames(t *testing.T) {
	a := newApp(layout.Layout{Active: "tab1", Workspaces: []*layout.Workspace{
		{ID: "w1", Tabs: []*layout.Tab{tab("tab1", "windows/t3")}},
	}}, shell("windows/t3", `C:\Users\me\proj`))
	if got := a.dirOf("windows/t3"); got != `windows:C:\Users\me\proj` {
		t.Errorf("dirOf = %q", got)
	}
	if got := a.workspaceName(a.lay.Workspaces[0]); got != "windows:proj" {
		t.Errorf("workspaceName = %q", got)
	}
}

// A terminal's program gets UTF-8's locale where the app has none.
func TestTerminalEnv(t *testing.T) {
	if got := terminalEnv([]string{"PATH=/bin"})["LANG"]; got != "en_US.UTF-8" {
		t.Errorf("without a locale, LANG %q", got)
	}
	if got, ok := terminalEnv([]string{"LC_CTYPE=de_DE.UTF-8"})["LANG"]; ok {
		t.Errorf("with a locale, LANG %q", got)
	}
}

// With other machines added, the sidebar's New Workspace button offers
// them, those not connected disabled with their state.
func TestNewWorkspaceMenu(t *testing.T) {
	a := newApp(layout.Layout{})
	a.machines = []hc.Machine{{Name: "windows", State: "connected"}, {Name: "rohan", State: "needs_login"}}
	tt := ui.NewTester(a.view, 1000, 600)
	if err := tt.Click("New Workspace"); err != nil {
		t.Fatal(err)
	}
	want := []string{localMachine, "-", "windows", "rohan (needs login)"}
	if got := tt.Menu(); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("menu %q, want %q", got, want)
	}
	if err := tt.ChooseMenuItem("rohan (needs login)"); err == nil {
		t.Error("chose a machine that is not connected")
	}
	if err := tt.ChooseMenuItem("windows"); err != nil {
		t.Error(err)
	}
}

// With other machines added, a workspace's New Tab button offers them as
// New Workspace does.
func TestNewTabMenu(t *testing.T) {
	a := newApp(layout.Layout{Active: "tab1", Workspaces: []*layout.Workspace{
		{ID: "w1", Tabs: []*layout.Tab{tab("tab1", "t1")}},
	}}, shell("t1", "/srv"))
	tt := ui.NewTester(a.view, 1000, 600)
	if err := tt.Click("New Tab"); err != nil {
		t.Fatal(err)
	}
	if got := tt.Menu(); len(got) != 0 {
		t.Errorf("a menu %q without machines", got)
	}
	a.machines = []hc.Machine{{Name: "windows", State: "connected"}, {Name: "rohan", State: "needs_login"}}
	tt.Frame()
	if err := tt.Click("New Tab"); err != nil {
		t.Fatal(err)
	}
	want := []string{localMachine, "-", "windows", "rohan (needs login)"}
	if got := tt.Menu(); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("menu %q, want %q", got, want)
	}
	if err := tt.ChooseMenuItem("rohan (needs login)"); err == nil {
		t.Error("chose a machine that is not connected")
	}
	if err := tt.ChooseMenuItem("windows"); err != nil {
		t.Error(err)
	}
}

// A new tab starts beside its workspace's focused pane, that of the tab
// shown or else of its first tab, rather than the shown tab's of another
// workspace; on another machine than that pane's, in that machine's home.
func TestNewTabBeside(t *testing.T) {
	a := newApp(layout.Layout{Active: "tab2", Workspaces: []*layout.Workspace{
		{ID: "w1", Tabs: []*layout.Tab{tab("tab1", "t1"), tab("tab2", "t2")}},
		{ID: "w2", Tabs: []*layout.Tab{tab("tab3", "windows/t3"), tab("tab4", "t4")}},
	}}, shell("t1", "/srv"), shell("t2", "/src"), shell("windows/t3", `C:\proj`), shell("t4", "/tmp"))
	w1, w2 := a.lay.Workspaces[0], a.lay.Workspaces[1]
	if got := a.focusedIn(w1); got != "t2" {
		t.Errorf("shown workspace's pane %q, want t2", got)
	}
	if got := a.focusedIn(w2); got != "windows/t3" {
		t.Errorf("other workspace's pane %q, want windows/t3", got)
	}
	for _, c := range []struct{ machine, beside, cwd string }{
		{"windows", "windows/t3", `C:\proj`},
		{"", "windows/t3", ""},
		{"", "t2", "/src"},
		{"windows", "t2", ""},
		{"", "", ""},
	} {
		if got := a.cwdOn(c.machine, c.beside); got != c.cwd {
			t.Errorf("on %q beside %q: %q, want %q", c.machine, c.beside, got, c.cwd)
		}
	}
}

// The core lists no terminal of a machine it can't reach: their panes stay
// till it is back, and their headers say it is away. A machine connected,
// or one the core no longer has, lists all its terminals: one missing is
// gone.
func TestMachineAway(t *testing.T) {
	l := func() layout.Layout {
		return layout.Layout{Active: "tab1", Workspaces: []*layout.Workspace{
			{ID: "w1", Tabs: []*layout.Tab{tab("tab1", "t1", "windows/t3")}},
		}}
	}
	for _, c := range []struct {
		machines []hc.Machine
		kept     bool
	}{
		{[]hc.Machine{{Name: "windows", State: "unreachable"}}, true},
		{[]hc.Machine{{Name: "windows", State: "connected"}}, false},
		{nil, false},
	} {
		a := newApp(l(), shell("t1", "/srv"))
		a.machines = c.machines
		a.lay.Prune(a.live)
		if kept := len(a.lay.Terminals()) == 2; kept != c.kept {
			t.Errorf("machines %+v: kept %v, want %v", c.machines, kept, c.kept)
		}
	}

	a := newApp(l(), shell("t1", "/srv"))
	a.machines = []hc.Machine{{Name: "windows", State: "needs_login"}}
	tt := ui.NewTester(a.view, 1000, 600)
	if !tt.HasText("needs login") {
		t.Errorf("no machine state in %q", tt.Texts())
	}
}

// An error of a machine that could not start a terminal goes as the
// machine is back; another error stays.
func TestMachineErrorClears(t *testing.T) {
	a := newApp(layout.Layout{})
	down := []hc.Machine{{Name: "windows", State: "unreachable"}}
	up := []hc.Machine{{Name: "windows", State: "connected"}}
	a.err = "terminal.run: windows is unreachable"
	a.machineErr = machineErr{"windows", a.err}
	a.setMachines(down)
	if a.err == "" {
		t.Fatal("the error went while the machine is away")
	}
	a.setMachines(up)
	if a.err != "" {
		t.Errorf("error %q once the machine is back", a.err)
	}
	a.err = "saving the layout: conflict"
	a.setMachines(up)
	if a.err == "" {
		t.Error("another error went")
	}
}

// The server settings list the core's machines, each with its state, host
// and hc; one not connected says why, with a button connecting to it, and
// one waiting for a login the command logging in. A core without machines
// has no list.
func TestMachineSettings(t *testing.T) {
	a := newApp(layout.Layout{})
	tt := ui.NewTester(a.view, 1200, 900)
	tt.Key(ui.Cmd, ui.KeyComma)
	if tt.HasText("Machines") {
		t.Errorf("a list of machines without any: %q", tt.Texts())
	}
	a.machines = []hc.Machine{
		{Name: "windows", Host: "win", Version: "0.1.18", State: "connected"},
		{Name: "rohan", Host: "rohan", Version: "0.1.17", State: "unreachable", Why: "cannot reach rohan: timed out"},
		{Name: "box", Host: "box", Version: "0.1.18", State: "needs_login", Temporary: true},
	}
	tt.Frame()
	for _, text := range []string{"Machines", "windows", "connected · win · hc 0.1.18", "unreachable · rohan · hc 0.1.17",
		"cannot reach rohan: timed out", "needs login · box · hc 0.1.18 · temporary", "hc machine login box"} {
		if !tt.HasText(text) {
			t.Errorf("no %q in %q", text, tt.Texts())
		}
	}
	n := 0
	for _, text := range tt.Texts() {
		if text == "Connect" {
			n++
		}
	}
	if n != 2 {
		t.Errorf("%d Connect buttons, want one for each machine not connected", n)
	}
}
