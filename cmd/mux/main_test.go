package main

import (
	"bytes"
	"runtime"
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
	note, _ := tt.Find("Draws text heavier, as Ghostty’s font-thicken does.")
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

// Cmd+Shift+[ and ] go around the shown workspace's tabs, Cmd+1 to 8 to
// one of them, Cmd+9 to its last.
func TestSwitchTab(t *testing.T) {
	a := newApp(layout.Layout{Active: "a", Workspaces: []*layout.Workspace{
		{ID: "w1", Tabs: []*layout.Tab{tab("x", "t0")}},
		{ID: "w2", Tabs: []*layout.Tab{tab("a", "t1"), tab("b", "t2"), tab("c", "t3")}},
	}})
	for _, c := range []struct {
		got, want string
	}{
		{a.tabBeside(1), "b"}, {a.tabBeside(-1), "c"}, {a.tabBeside(3), "a"},
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
		if got := gitBranch(c.dir); got != c.want {
			t.Errorf("gitBranch(%s) = %q, want %q", c.dir, got, c.want)
		}
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
