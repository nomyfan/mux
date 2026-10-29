// Mux is a native client of herdr-core, the terminal runtime of hc: its
// terminals in workspaces and tabs in a sidebar, with the states of their
// agents, beside the tab chosen, whose panes show the terminals with
// libghostty-vt. The core runs the programs; Mux arranges them.
package main

import (
	"cmp"
	"embed"
	"fmt"
	"log"
	"math"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"mux/internal/hc"
	"mux/internal/layout"
	"mux/internal/term"
)

type app struct {
	win *mygo.Window
	// post runs a function on the main thread: the window's Update.
	post func(func())
	// bin is the herdr-core command, "" until found; notInstalled while
	// none is. env is the environment it runs in.
	bin          string
	notInstalled bool
	env          []string
	// gate holds the connection back while the user stopped the core;
	// starting while it starts again, stopping while the user confirms
	// stopping it. serverErr is why starting or stopping it failed.
	gate      gate
	starting  bool
	stopping  bool
	serverErr string
	// conn is the connection to the core, nil while there is none; gen
	// counts connections, so that what an old one answers is dropped.
	// terms are its terminals.
	conn  *hc.Conn
	gen   int
	terms hc.Terminals
	err   string
	// lay is the layout, as of the document's layVersion; saving while a
	// write of it goes, dirty while it changed since.
	lay        layout.Layout
	layVersion uint64
	saving     bool
	dirty      bool
	branches   map[string]*branch
	// chosen is the item the sidebar shows chosen: a tab, or an agent's
	// terminal.
	chosen        string
	term          *term.View
	services      ui.Services
	sidebarWidth  float32
	sidebarHidden bool
	// collapsed holds the sections of the sidebar whose items it hides,
	// by key, and itemLists the lists of their items, which slide as they
	// hide and show.
	collapsed map[string]bool
	itemLists map[string]*ui.Handle
	// renaming is the workspace, tab or pane whose name is edited, newLabel
	// what it is edited to; editing, once renameInput has the focus.
	renaming    string
	newLabel    string
	renameInput ui.Handle
	editing     bool
	// closing is the workspace or tab the user asked to close, which they
	// confirm while confirming.
	closing    *closing
	confirming bool
	// settings are the user's, which the settings dialog shows while
	// settingsOpen, on settingsPage, its appearance control choosing
	// appearance.
	settings     settings
	settingsOpen bool
	settingsPage string
	appearance   int
	// terminalFamily and fallbackFamily are the font families the font
	// controls choose: the first among monoFonts, the system's monospaced
	// families, the other among allFonts, all its families.
	terminalFamily string
	fallbackFamily string
	monoFonts      []string
	allFonts       []string
	// fontSize is the terminals' font size the size control chooses, and
	// fontSizeText what its field shows.
	fontSize      float64
	fontSizeText  string
	fontSizeInput ui.Handle
	// previewGlyphs are the terminal settings' font preview, shaped in
	// previewFont.
	previewFont   ui.Font
	previewGlyphs [][][]ui.Glyph

	// inbox holds the events the connection read for the main thread.
	mu    sync.Mutex
	inbox []hc.Event
}

const (
	minSidebarWidth = 180
	maxSidebarWidth = 420
)

func icon(shapes string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round">` + shapes + `</svg>`))
}

var (
	sidebarIcon = icon(`<rect x="1.5" y="2.5" width="13" height="11" rx="2"/><path d="M6 2.5v11"/>`)
	shellIcon   = icon(`<path d="M3 4.5l3.5 3.5L3 11.5M8 11.5h5"/>`)
	moveIcon    = icon(`<path d="M8 1.5v13M1.5 8h13M6 3.5l2-2 2 2M6 12.5l2 2 2-2M3.5 6l-2 2 2 2M12.5 6l2 2-2 2"/>`)
	// agentIcon is for agents without a mark of their own in agentIcons.
	agentIcon = icon(`<path d="M8 1.5l1.7 4.8 4.8 1.7-4.8 1.7L8 14.5l-1.7-4.8L1.5 8l4.8-1.7z"/>`)
	// blankIcon keeps a sidebar item's icon slot for the icon laid over
	// it (iconOver).
	blankIcon = icon("")

	branchIcon     = icon(`<circle cx="4.5" cy="3.5" r="1.5"/><circle cx="4.5" cy="12.5" r="1.5"/><circle cx="11.5" cy="5.5" r="1.5"/><path d="M4.5 5v6M11.5 7c0 2.5-2 3.5-7 4"/>`)
	splitRightIcon = icon(`<rect x="1.5" y="2.5" width="13" height="11" rx="2"/><path d="M8 2.5v11"/>`)
	splitDownIcon  = icon(`<rect x="1.5" y="2.5" width="13" height="11" rx="2"/><path d="M1.5 8h13"/>`)
	zoomIcon       = icon(`<path d="M9.5 2.5h4v4M13.5 2.5l-4.5 4.5M6.5 13.5h-4v-4M2.5 13.5l4.5-4.5"/>`)
	unzoomIcon     = icon(`<path d="M13.5 6.5h-4v-4M9.5 6.5l4-4M2.5 9.5h4v4M6.5 9.5l-4 4"/>`)
	closeIcon      = icon(`<path d="M4 4l8 8M12 4l-8 8"/>`)
	moreIcon       = icon(`<path stroke-width="2" d="M3.5 8h.01M8 8h.01M12.5 8h.01"/>`)
	newTabIcon     = icon(`<path d="M8 3v10M3 8h10"/>`)
	collapseIcon   = icon(`<path d="M4 6l4 4 4-4"/>`)
	expandIcon     = icon(`<path d="M6 4l4 4-4 4"/>`)
)

//go:embed icons/agents/*.svg
var agentIconFiles embed.FS

// agentIcons are the agents' own marks in their own colors, those without
// colors drawn in currentColor, by the names agentMarks gives them.
var agentIcons = func() map[string]*ui.SVG {
	entries, _ := agentIconFiles.ReadDir("icons/agents")
	icons := make(map[string]*ui.SVG, len(entries))
	for _, e := range entries {
		data, _ := agentIconFiles.ReadFile("icons/agents/" + e.Name())
		icons[strings.TrimSuffix(e.Name(), ".svg")] = ui.MustParseSVG(data)
	}
	return icons
}()

func (a *app) view(c *ui.Context) {
	setTheme(c)
	a.services = c.Services()
	if c.Shortcut(ui.Cmd, ui.KeyB) {
		a.sidebarHidden = !a.sidebarHidden
	}
	if c.Shortcut(ui.Cmd, ui.KeyD) {
		a.splitFocused(layout.Right)
	}
	if c.Shortcut(ui.Cmd|ui.Shift, ui.KeyD) {
		a.splitFocused(layout.Down)
	}
	if c.Shortcut(ui.Cmd, ui.KeyComma) {
		a.openSettings()
	}
	if c.Shortcut(ui.Cmd|ui.Shift, ui.KeyEnter) {
		a.toggleZoom()
	}
	for _, fc := range a.fileCommands() {
		if c.Shortcut(fc.mods, fc.key) {
			fc.run()
		}
	}
	// Command-Shift-[ and ] go to the workspace's tab before and after the
	// one shown, Command-1 to 8 to its first eight, Command-9 to its last.
	if c.Shortcut(ui.Cmd|ui.Shift, ui.KeyBracketLeft) {
		a.showTab(a.tabBeside(-1))
	}
	if c.Shortcut(ui.Cmd|ui.Shift, ui.KeyBracketRight) {
		a.showTab(a.tabBeside(1))
	}
	for i, k := range []ui.Key{ui.Key1, ui.Key2, ui.Key3, ui.Key4, ui.Key5, ui.Key6, ui.Key7, ui.Key8, ui.Key9} {
		if c.Shortcut(ui.Cmd, k) {
			a.showTab(a.tabNumber(i + 1))
		}
	}
	t := c.Theme()
	bar := c.TitleBar()
	row := ui.Row(c).Fill().AlignItems(ui.Stretch)
	shown := row.Animate("sidebar", b2f(!a.sidebarHidden), 250*time.Millisecond)
	row.Children(func() {
		// The sidebar slides out to the left as it hides.
		w := a.sidebarWidth * shown
		ui.Row(c).Width(w).Shrink(0).AlignItems(ui.Stretch).Clip().Children(func() {
			ui.Column(c).Width(a.sidebarWidth).Shrink(0).Margin(0, 0, 0, w-a.sidebarWidth).Background(t.Surface).
				Children(func() { a.sidebar(c) })
		})
		if w > 0 {
			ui.Box(c).Width(1).Background(t.Border)
		}
		// The terminals take the room the sidebar leaves once it settles.
		room, settled := float32(0), float32(0)
		if w > 0 {
			room = w + 1
		}
		if !a.sidebarHidden {
			settled = a.sidebarWidth + 1
		}
		a.term.Settling(room - settled)
		// Hidden, the sidebar leaves the title room for the window controls.
		a.pane(c, 16+(1-shown)*(bar.Left+36))
		if shown == 1 {
			// A handle wider than the divider resizes the sidebar.
			handle := ui.Box(c.Key("resize")).Absolute().Left(w - 3).Top(0).Bottom(0).Width(7).Cursor(ui.CursorResizeEW)
			if dx, _, ok := handle.Dragged(); ok {
				a.sidebarWidth = min(max(a.sidebarWidth+dx, minSidebarWidth), maxSidebarWidth)
			}
		}
	})

	label := "Hide Sidebar"
	if a.sidebarHidden {
		label = "Show Sidebar"
	}
	toggle := ui.ButtonBase(c.Key("toggle")).Absolute().Left(bar.Left+8).Top((a.titleBand(c)-28)/2).Size(32, 28).
		Radius(6).Center().TextColor(t.TextMuted).Label(label).Tooltip(label)
	// A tint of the text color rather than an opaque surface, so the
	// sidebar's material shows through as under AppKit's toolbar buttons.
	if toggle.Pressed() {
		toggle.Background(t.Text.Alpha(0.14))
	} else if toggle.Hovered() {
		toggle.Background(t.Text.Alpha(0.08))
	}
	toggle.Children(func() { ui.Icon(c, sidebarIcon).Size(16, 16) })
	if toggle.Clicked() {
		a.sidebarHidden = !a.sidebarHidden
	}
	a.settingsDialog(c)
	a.confirmStopServer(c)
}

// sidebar is the workspaces' tabs, and the agents, under the room the
// window controls take.
func (a *app) sidebar(c *ui.Context) {
	ui.Box(c).Height(a.titleBand(c)).Shrink(0).DragWindow()
	if _, t := a.lay.ActiveTab(); t != nil && a.chosen == "" {
		a.chosen = t.ID
	}
	ui.Sidebar(c, &a.chosen, func() {
		prev := ""
		for _, ws := range a.lay.Workspaces {
			a.section(c, ws.ID, prev, func(open bool) { a.workspaceHeader(c, ws, open) }, func() {
				for _, tab := range ws.Tabs {
					a.tabItem(c, ws, tab)
				}
			})
			prev = ws.ID
		}
		if agents := a.agents(); len(agents) > 0 {
			a.section(c, agentsSection, prev, func(open bool) { a.agentsHeader(c, open) }, func() {
				for _, ag := range agents {
					a.agentItem(c, ag)
				}
			})
		}
	}).Grow(1).Label("Tabs and Agents").OnChange(a.showChosen)
	// Without a core, the panes' room says so.
	if a.err != "" && !a.notInstalled && !a.stopped() {
		ui.Text(c, a.err).FontSize(12).TextColor(c.Theme().Danger).Padding(8, 12)
	}
	if cl := a.closing; cl != nil {
		if ui.AlertDialog(c, &a.confirming, fmt.Sprintf("Close “%s”?", cl.name), "Its panes close, and what runs in them stops.", "Cancel", "Close") == 1 {
			a.kill(cl.terminals...)
		}
		if !a.confirming {
			a.closing = nil
		}
	}
}

// closing is a workspace or tab to close, by name, once the user confirms,
// with the terminals of its panes.
type closing struct {
	name      string
	terminals []string
}

// confirmClose asks the user to confirm closing what cl names.
func (a *app) confirmClose(cl closing) { a.closing, a.confirming = &cl, true }

// agentsSection is the key of the sidebar's section of agents.
const agentsSection = "agents"

// section shows a section of the sidebar, by key, under the header head
// builds, with the items items builds while it is open, which slide up
// under the header as they hide and down as they show; it leaves room
// after the one before, prev, if any, unless that one's items hide.
func (a *app) section(c *ui.Context, key, prev string, head func(open bool), items func()) {
	t := c.Theme()
	section := ui.Column(c.Key(key)).Gap(t.Space(0.5)).Shrink(0)
	if prev != "" && !a.collapsed[prev] {
		section.Margin(t.Space(2), 0, 0, 0)
	}
	open := !a.collapsed[key]
	shown := section.Animate("open", b2f(open), 200*time.Millisecond)
	section.Children(func() {
		head(open)
		if shown == 0 {
			return
		}
		if a.itemLists == nil {
			a.itemLists = map[string]*ui.Handle{}
		}
		h := a.itemLists[key]
		if h == nil {
			h = new(ui.Handle)
			a.itemLists[key] = h
		}
		height := h.Bounds(c).H
		body := ui.Column(c.Key("items")).Shrink(0)
		if shown < 1 {
			body.Height(height * shown).Clip()
		}
		body.Children(func() {
			ui.Column(c.Key("list")).Bind(h).Gap(t.Space(0.5)).Shrink(0).Margin(-height*(1-shown), 0, 0, 0).Children(items)
		})
	})
}

// collapse hides the items of the sidebar's section by key, or shows them.
func (a *app) collapse(key string, collapsed bool) {
	if a.collapsed == nil {
		a.collapsed = map[string]bool{}
	}
	a.collapsed[key] = collapsed
}

// agentsHeader heads the sidebar's agents, which a click on it, or its
// arrow, which shows as the pointer comes over it, shows or hides.
func (a *app) agentsHeader(c *ui.Context, open bool) {
	t := c.Theme()
	head := ui.Row(c.Key("head")).Height(28).AlignItems(ui.Center).Gap(2).Padding(0, t.Space(1), 0, t.Space(2)).Radius(t.Radius).Shrink(0)
	shown := head.Hovered() || head.FocusWithin()
	toggle := head.Clicked()
	head.Children(func() {
		ui.Text(c, "Agents").FontSize(t.FontSize - 1).FontWeight(600).TextColor(t.TextMuted).Grow(1).SingleLine()
		arrow, label := collapseIcon, "Collapse"
		if !open {
			arrow, label = expandIcon, "Expand"
		}
		toggle = iconButton(c, "toggle", arrow, label).Opacity(b2f(shown)).Clicked() || toggle
	})
	if toggle {
		a.collapse(agentsSection, open)
	}
}

// agentItem shows an agent in the sidebar by its pane's name, else its
// own, beside the workspace it works in, with its state; choosing it
// shows its pane.
func (a *app) agentItem(c *ui.Context, ag hc.Terminal) {
	t := c.Theme()
	icon, mark := a.itemIcon(ag.Address)
	item := ui.SidebarItem(c.Key("agent "+ag.Address), ag.Address, blankIcon, a.paneName(ag.Address))
	chosenLook(c, item, ag.Address == a.chosen)
	item.Children(func() {
		iconOver(c, icon, mark)
		where := a.dirOf(ag.Address)
		if ws, _, _ := a.lay.Find(ag.Address); ws != nil {
			where = a.workspaceName(ws)
		}
		ui.Text(c, where).FontSize(t.FontSize - 1).TextColor(t.TextMuted).SingleLine().Shrink(1).MinWidth(0)
		statusDot(c, ag.Status.State)
	})
}

// itemIcon returns what shows for what a terminal runs: its agent's mark,
// if it has one, else an icon, an agent's or a shell's.
func (a *app) itemIcon(address string) (icon, mark *ui.SVG) {
	s, isAgent := a.agent(address)
	if isAgent {
		if mark, ok := agentIcons[agentMarks[s.App]]; ok {
			return nil, mark
		}
		return agentIcon, nil
	}
	return shellIcon, nil
}

// drawIcon shows an agent's mark, if any, in its own colors, else icon
// in the accent.
func drawIcon(c *ui.Context, icon, mark *ui.SVG) {
	if mark != nil {
		ui.Image(c, mark).Size(16, 16)
	} else {
		ui.Icon(c, icon).Size(16, 16).TextColor(c.Theme().Accent)
	}
}

// iconOver shows an icon, or mark, over the blank icon of the sidebar item
// it is in, at the item's padding: SidebarItem tints its icon, which would
// lose a mark's colors, and on the accent of an item chosen, as the item
// no longer shows, the accent icons.
func iconOver(c *ui.Context, icon, mark *ui.SVG) {
	ui.Box(c).Absolute().Left(c.Theme().Space(2)).Top(0).Bottom(0).Center().Children(func() {
		drawIcon(c, icon, mark)
	})
}

// chosenLook shows a sidebar item chosen, if it is, on the pressed surface
// whether the sidebar has the focus or not, rather than on the accent,
// which would drown the colors of the states of agents.
func chosenLook(c *ui.Context, item ui.Element, chosen bool) {
	if t := c.Theme(); chosen {
		item.Background(t.SurfacePressed).TextColor(t.Text)
	}
}

// workspaceHeader names a workspace in the sidebar, which a click on it,
// or its arrow, shows the tabs of or hides, with buttons making a tab in
// it and opening its menu, as a secondary click on it does; the buttons
// show as the pointer comes over it, or one has the focus, as while the
// menu is open.
func (a *app) workspaceHeader(c *ui.Context, ws *layout.Workspace, open bool) {
	t := c.Theme()
	head := ui.Row(c.Key("head")).Height(28).AlignItems(ui.Center).Gap(2).Padding(0, t.Space(1), 0, t.Space(2)).Radius(t.Radius).Shrink(0).
		ContextMenu(a.workspaceMenu(ws))
	shown := head.Hovered() || head.FocusWithin()
	toggle := head.Clicked()
	name := a.workspaceName(ws)
	head.Children(func() {
		if a.renaming == ws.ID {
			a.renameField(c, "Workspace Name", ws.Name, func(name string) {
				ws.Name = name
				a.save()
			}).Placeholder(name).Grow(1).MinWidth(0).Height(24).Padding(0, 4).FontSize(t.FontSize - 1)
		} else {
			ui.Text(c, name).FontSize(t.FontSize - 1).FontWeight(600).TextColor(t.TextMuted).Grow(1).SingleLine()
		}
		// Hidden, the buttons keep their room, and the menu its button.
		actions := ui.Row(c.Key("actions")).Gap(2).Shrink(0)
		if !shown {
			actions.Opacity(0)
		}
		actions.Children(func() {
			if headerButton(c, "new-tab", newTabIcon, "New Tab") {
				a.newTab(ws.ID)
			}
			iconButton(c, "more", moreIcon, "Workspace Actions").Menu(a.workspaceMenu(ws))
			arrow, label := collapseIcon, "Collapse"
			if !open {
				arrow, label = expandIcon, "Expand"
			}
			if headerButton(c, "toggle", arrow, label) {
				toggle = true
			}
		})
	})
	if toggle {
		a.collapse(ws.ID, open)
	}
}

// tabItem shows a tab in the sidebar by its name, if it was given one,
// else by its focused pane: the pane's name, its agent, or its directory;
// with the state of the tab's agent that most needs the user, and a button
// closing it, once the user confirms, as the pointer comes over it, as its
// menu does.
func (a *app) tabItem(c *ui.Context, ws *layout.Workspace, tab *layout.Tab) {
	name, shown := a.tabNames(tab)
	icon, mark := a.itemIcon(tab.Focus)
	if a.renaming == tab.ID {
		a.renameTab(c, tab, icon, mark, name, shown)
		return
	}
	closeTab := func() { a.closeTab(tab) }
	item := ui.SidebarItem(c.Key(tab.ID), tab.ID, blankIcon, cmp.Or(name, shown)).ContextMenu(func(m *ui.Menu) {
		if m.Item("Rename").Chosen() {
			a.renaming, a.newLabel = tab.ID, name
		}
		m.Separator()
		if m.Item("Close Tab…").Chosen() {
			closeTab()
		}
	})
	chosenLook(c, item, tab.ID == a.chosen)
	hovered := item.Hovered()
	status := a.tabStatus(tab)
	item.Children(func() {
		iconOver(c, icon, mark)
		switch {
		case hovered:
			// Taller than the item's text, the button keeps out of its
			// height: it sits over a box of none, centered on the item.
			const size = 20
			ui.Box(c.Key("close")).Width(size).Height(0).Shrink(0).Children(func() {
				if iconButton(c, "close", closeIcon, "Close Tab").Absolute().Left(0).Top(-size/2).Size(size, size).Clicked() {
					closeTab()
				}
			})
		case status == "":
		default:
			statusDot(c, status)
		}
	})
}

// tabTerminals returns the terminals of a tab's panes.
func tabTerminals(tab *layout.Tab) []string {
	var out []string
	for _, n := range tab.Leaves() {
		out = append(out, n.Terminal)
	}
	return out
}

// closeTab closes a tab, once the user confirms.
func (a *app) closeTab(tab *layout.Tab) {
	name, shown := a.tabNames(tab)
	a.confirmClose(closing{cmp.Or(name, shown), tabTerminals(tab)})
}

// renameTab edits the name of a tab, if any, in place of its item, as tall
// as the item, beside its icon, or mark, if any: no name shows what the
// tab shows without one, unnamed, again.
func (a *app) renameTab(c *ui.Context, tab *layout.Tab, icon, mark *ui.SVG, name, unnamed string) {
	t := c.Theme()
	ui.Row(c.Key(tab.ID)).AlignItems(ui.Center).Gap(t.Space(2)).Padding(t.Space(1.5), t.Space(2)).Shrink(0).Children(func() {
		drawIcon(c, icon, mark)
		// A line of text sizes the row as the item's label does; the field
		// lies over it.
		ui.Box(c).Grow(1).MinWidth(0).Children(func() {
			ui.Text(c, " ").SingleLine()
			a.renameField(c, "Tab Name", name, func(name string) {
				tab.Name = name
				a.save()
			}).Placeholder(unnamed).Absolute().Left(-4).Right(0).Top(-3).Bottom(-3).Padding(0, 4)
		})
	})
}

// fileCommand is a command of the File menu, which takes its key before
// the view on macOS: there the menu runs it, elsewhere the view.
type fileCommand struct {
	label string
	mods  ui.Modifiers
	key   ui.Key
	accel string // the menu's spelling of mods and key
	run   func()
	group bool // whether a separator comes before it
}

func (a *app) fileCommands() []fileCommand {
	return []fileCommand{
		{label: "New Tab", mods: ui.Cmd, key: ui.KeyT, accel: "CmdOrCtrl+T", run: a.newTabHere},
		{label: "New Workspace", mods: ui.Cmd, key: ui.KeyN, accel: "CmdOrCtrl+N", run: a.newWorkspace},
		// Closing a tab asks first; a pane closes at once, on the chord
		// harder to press by mistake.
		{label: "Close Tab…", mods: ui.Cmd, key: ui.KeyW, accel: "CmdOrCtrl+W", run: a.closeShownTab, group: true},
		{label: "Close Pane", mods: ui.Cmd | ui.Shift, key: ui.KeyW, accel: "Shift+CmdOrCtrl+W", run: a.closeFocusedPane},
	}
}

// newTabHere makes a tab in the workspace shown, or a workspace if none.
func (a *app) newTabHere() {
	if ws, _ := a.lay.ActiveTab(); ws != nil {
		a.newTab(ws.ID)
	} else {
		a.newWorkspace()
	}
}

// menu is macOS's standard menu bar with the File menu of fileCommands,
// whose Command-W closes a tab rather than the window. The commands wait
// while a dialog shows, as the view's shortcuts do.
func (a *app) menu() *mygo.Menu {
	var file []*mygo.MenuItem
	for _, fc := range a.fileCommands() {
		if fc.group {
			file = append(file, mygo.Separator())
		}
		file = append(file, &mygo.MenuItem{Label: fc.label, Accelerator: fc.accel, Click: func(*mygo.MenuItem, *mygo.Window) {
			a.update(func() {
				if !a.settingsOpen && !a.confirming && !a.stopping {
					fc.run()
				}
			})
		}})
	}
	return mygo.NewMenu([]*mygo.MenuItem{
		{Role: mygo.RoleAppMenu},
		{Label: "File", Submenu: file},
		{Role: mygo.RoleEditMenu},
		{Role: mygo.RoleViewMenu},
		{Role: mygo.RoleWindowMenu},
	})
}

// closeFocusedPane closes the focused pane of the tab shown, as its
// header's button does.
func (a *app) closeFocusedPane() {
	if t := a.focusedTerminal(); t != "" {
		a.kill(t)
	}
}

// closeShownTab closes the tab shown, once the user confirms.
func (a *app) closeShownTab() {
	if _, tab := a.lay.ActiveTab(); tab != nil {
		a.closeTab(tab)
	}
}

// tabBeside returns the tab step places after the one shown in its
// workspace, around from the last to the first, or "" if none.
func (a *app) tabBeside(step int) string {
	ws, tab := a.lay.ActiveTab()
	if ws == nil {
		return ""
	}
	n := len(ws.Tabs)
	for i, t := range ws.Tabs {
		if t == tab {
			return ws.Tabs[((i+step)%n+n)%n].ID
		}
	}
	return ""
}

// tabNumber returns the shown workspace's nth tab, from 1, its last for
// 9, or "" if none.
func (a *app) tabNumber(n int) string {
	ws, _ := a.lay.ActiveTab()
	if ws == nil {
		return ""
	}
	if n == 9 {
		n = len(ws.Tabs)
	}
	if n < 1 || n > len(ws.Tabs) {
		return ""
	}
	return ws.Tabs[n-1].ID
}

// workspaceMenu builds the menu of a workspace's actions.
func (a *app) workspaceMenu(ws *layout.Workspace) func(m *ui.Menu) {
	return func(m *ui.Menu) {
		if m.Item("Rename").Chosen() {
			a.renaming, a.newLabel = ws.ID, ws.Name
		}
		m.Separator()
		if m.Item("Close Workspace…").Chosen() {
			a.confirmClose(closing{a.workspaceName(ws), ws.Terminals()})
		}
	}
}

// renameField edits the name, current, of what is being renamed, which
// label names, in place of it: Enter, or the focus leaving, renames it
// with rename, the name trimmed, if it changed; Escape keeps its name.
func (a *app) renameField(c *ui.Context, label, current string, rename func(name string)) ui.Element {
	done := func(save bool) {
		name := strings.TrimSpace(a.newLabel)
		if save && name != current {
			rename(name)
		}
		a.renaming, a.newLabel, a.editing = "", "", false
	}
	field := ui.TextInput(c.Key("rename"), &a.newLabel).Bind(&a.renameInput).AutoFocus().Label(label).
		OnTextKey(func(k ui.TextKey) bool {
			switch {
			case k.Key == ui.KeyEnter && k.Mods == 0:
				done(true)
			case k.Key == ui.KeyEscape && k.Mods == 0:
				done(false)
			default:
				return false
			}
			return true
		})
	// The field takes the focus a frame after it shows.
	switch focused := a.renameInput.Focused(c); {
	case focused:
		a.editing = true
	case a.editing:
		done(true)
	}
	return field
}

// pane is the tab shown, each of its panes under a header naming it.
// The headers along the top hold the window controls' room: titleLeft is
// where the top-left one's title starts.
func (a *app) pane(c *ui.Context, titleLeft float32) {
	bar := c.TitleBar()
	if a.conn == nil && (a.notInstalled || a.stopped()) {
		a.serverDownView(c)
		return
	}
	_, tab := a.lay.ActiveTab()
	if tab == nil {
		a.emptyView(c)
		return
	}
	header := func(c *ui.Context, terminal string, box ui.Rect, hovered, focused bool) {
		left, right := float32(12), float32(8)
		if box.Y == 0 {
			if box.X == 0 {
				left = max(left, titleLeft)
			}
			// The window's buttons on the right (Linux, Windows).
			right += bar.Right
		}
		a.paneHeader(c, tab, terminal, left, right, focused, focused || hovered)
	}
	hooks := term.Hooks{Header: header, Changed: a.changed,
		Title: func(c *ui.Context, terminal string) { a.paneTitle(c, terminal, false, false) }}
	a.term.Build(c, tab, a.attacher(), hooks).Grow(1).MinWidth(0)
}

// emptyView shows, in place of the panes, that there are none, with a
// button making a tab.
func (a *app) emptyView(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Grow(1).MinWidth(0).Children(func() {
		ui.Row(c).Height(a.titleBand(c)).Shrink(0).DragWindow()
		ui.Column(c).Grow(1).Center().Gap(t.Space(3)).Padding(0, t.Space(6), a.titleBand(c), t.Space(6)).Children(func() {
			if a.conn == nil {
				ui.Text(c, "Connecting to herdr-core…").TextColor(t.TextMuted)
				return
			}
			ui.Text(c, "No tabs").FontSize(t.FontSize + 2).Bold()
			if ui.PrimaryButton(c.Key("new-tab"), "New Tab").Clicked() {
				a.newTabHere()
			}
		})
	})
}

// paneHeader shows what a pane runs, where, and on which branch, with the
// actions on it while actions holds: as the pane has the focus or the
// pointer.
func (a *app) paneHeader(c *ui.Context, tab *layout.Tab, terminal string, left, right float32, focused, actions bool) {
	t := c.Theme()
	// The title's own padding lines its text up with the header's.
	ui.Row(c).Grow(1).Height(28).AlignItems(ui.Center).Gap(8).Padding(0, right, 0, left-8).MinWidth(0).Clip().Children(func() {
		if a.renaming == terminal {
			a.renamePane(c, tab, terminal)
		} else {
			// The user drags the pane by its title, which shows as much
			// as the pointer comes over it.
			title := ui.Row(c.Key("title")).Bind(a.term.TitleHandle(terminal)).AlignItems(ui.Center).Gap(8).
				Padding(3, 8).Radius(6).Shrink(1).MinWidth(0).Cursor(ui.CursorMove).
				ContextMenu(func(m *ui.Menu) {
					if m.Item("Rename").Chosen() {
						if n := tab.Pane(terminal); n != nil {
							a.renaming, a.newLabel = terminal, n.Name
						}
					}
				})
			grip := title.Hovered()
			if grip {
				title.Background(t.Text.Alpha(0.07))
			}
			title.Children(func() { a.paneTitle(c, terminal, focused, grip) })
		}
		ui.Spacer(c)
		if !actions {
			return
		}
		zoomLabel, zoom := "Zoom", zoomIcon
		if tab.Zoomed {
			zoomLabel, zoom = "Unzoom", unzoomIcon
		}
		ui.Row(c).Gap(2).Shrink(0).Children(func() {
			if headerButton(c, "split-right", splitRightIcon, "Split Right") {
				a.split(terminal, layout.Right)
			}
			if headerButton(c, "split-down", splitDownIcon, "Split Down") {
				a.split(terminal, layout.Down)
			}
			if headerButton(c, "zoom", zoom, zoomLabel) {
				tab.Focus = terminal
				a.toggleZoom()
			}
			if headerButton(c, "close", closeIcon, "Close Pane") {
				a.kill(terminal)
			}
		})
	})
}

// toggleZoom shows the focused pane of the tab shown alone, or all its
// panes again.
func (a *app) toggleZoom() {
	if _, tab := a.lay.ActiveTab(); tab != nil {
		tab.Zoomed = !tab.Zoomed
		a.term.Focus()
		a.save()
	}
}

// renamePane edits a pane's name in place of its title, which no longer
// drags the pane meanwhile, so that a press in the field is the field's:
// no name, its agent's or its program's shows for it again.
func (a *app) renamePane(c *ui.Context, tab *layout.Tab, terminal string) {
	n := tab.Pane(terminal)
	if n == nil {
		return
	}
	ui.Row(c.Key("rename")).AlignItems(ui.Center).Padding(0, 0, 0, 8).Shrink(1).MinWidth(0).Children(func() {
		a.renameField(c, "Pane Name", n.Name, func(name string) {
			if n := tab.Pane(terminal); n != nil {
				n.Name = name
				a.save()
			}
		}).Placeholder(a.unnamedPane(terminal)).Width(240).MinWidth(0).Shrink(1).Height(24).Padding(0, 6)
	})
}

// unnamedPane is what a pane shows without a name of its own.
func (a *app) unnamedPane(terminal string) string {
	if _, ok := a.agent(terminal); ok {
		return a.agentName(terminal)
	}
	if t, ok := a.terms.Find(terminal); ok && t.Foreground != nil && t.Foreground.Name != "" {
		return t.Foreground.Name
	}
	return "shell"
}

// paneTitle shows what a pane runs, where, and on which branch, with the
// mark of the agent, or a shell's; grip shows the mark of moving instead.
func (a *app) paneTitle(c *ui.Context, terminal string, focused, grip bool) {
	t := c.Theme()
	status, isAgent := a.agent(terminal)
	if icon, mark := a.itemIcon(terminal); mark != nil && !grip {
		ui.Image(c, mark).Size(16, 16)
	} else {
		if grip {
			icon = moveIcon
		}
		ui.Icon(c, icon).Size(16, 16).TextColor(t.TextMuted)
	}
	title := ui.Text(c, a.paneName(terminal)).Bold().SingleLine().Shrink(0)
	if !focused {
		title.TextColor(t.TextMuted)
	}
	if isAgent {
		statusDot(c, status.State)
	}
	ui.Text(c, a.dirOf(terminal)).TextColor(t.TextMuted).SingleLine().Shrink(1).MinWidth(0)
	if branch := a.branchOf(terminal); branch != "" {
		ui.Icon(c, branchIcon).Size(14, 14).TextColor(t.TextMuted)
		ui.Text(c, branch).TextColor(t.TextMuted).SingleLine().Shrink(1).MinWidth(0)
	}
}

// headerButton reports a click on an iconButton.
func headerButton(c *ui.Context, key string, icon *ui.SVG, label string) bool {
	return iconButton(c, key, icon, label).Clicked()
}

// iconButton is an icon button of a header, named label, tinted rather
// than filled as it is hovered, over whatever the header is drawn on.
func iconButton(c *ui.Context, key string, icon *ui.SVG, label string) ui.Element {
	t := c.Theme()
	b := ui.ButtonBase(c.Key(key)).Size(26, 24).Radius(6).Center().TextColor(t.TextMuted).Label(label).Tooltip(label)
	if b.Pressed() {
		b.Background(t.Text.Alpha(0.14))
	} else if b.Hovered() {
		b.Background(t.Text.Alpha(0.08)).TextColor(t.Text)
	}
	b.Children(func() { ui.Icon(c, icon).Size(15, 15) })
	return b
}

// titleBand is the height of the band along the top of the window that the
// window controls sit in, centered on the panes' headers along the top.
func (a *app) titleBand(c *ui.Context) float32 {
	return cmp.Or(c.TitleBar().Height, 2*a.term.HeaderCenter())
}

// statusDot shows an agent's state as a dot in a soft halo, as Coda's: a
// working agent's halo breathes, a blocked one's ripples out for the user,
// a done one's and an idle one's stay still, as all do for less motion;
// the colors, amber, red and green, tell the states apart without it.
func statusDot(c *ui.Context, status string) {
	t := c.Theme()
	color := t.TextMuted
	switch status {
	case "working":
		color = t.Warning
	case "blocked", "error":
		color = t.Danger
	case "done":
		color = t.Success
	}
	const size, core = 12, 6
	dot := ui.Box(c.Key("status")).Size(size, size).Shrink(0).Label(status).Tooltip(status)
	still := c.Preferences().ReduceMotion
	// The halo moves as the dot is painted again, without the view built
	// again: the frames of a moving drawing cost the less.
	dot.Draw(func(p *ui.Painter, r ui.Rect) {
		scale, alpha := float32(1), float32(0.3)
		switch {
		case still:
		case status == "working":
			// Out to half again and back, fading, over 2s, each way along
			// CSS's ease-in-out.
			v := ui.Bounce(ui.CubicBezier(0.42, 0, 0.58, 1))(phase(p.Now(), 2*time.Second))
			scale, alpha = 1+0.5*v, 0.35-0.25*v
			p.AnimationFrame()
		case status == "blocked":
			// Out to more than twice and gone in three quarters of 1.6s,
			// then still till it starts over.
			v := ui.CubicBezier(0, 0, 0.2, 1)(min(phase(p.Now(), 1600*time.Millisecond)/0.75, 1))
			scale, alpha = 1+1.2*v, 0.45*(1-v)
			p.AnimationFrame()
		}
		// Paths, not rounded boxes, which Fill rounds to whole pixels, so
		// that the halo grows smoothly, and on macOS curves as squircles.
		cx, cy := r.X+r.W/2, r.Y+r.H/2
		var circle ui.Path
		p.FillPath(circle.Circle(cx, cy, size/2*scale), color.Alpha(alpha))
		p.FillPath(circle.Reset().Circle(cx, cy, core/2), color)
	})
}

// phase is how far along a loop of period now is, from 0 to 1.
func phase(now time.Time, period time.Duration) float32 {
	return float32(now.UnixNano()%int64(period)) / float32(period)
}

func b2f(b bool) float32 {
	if b {
		return 1
	}
	return 0
}

// showChosen shows the tab, or agent's pane, the user chose in the
// sidebar.
func (a *app) showChosen() {
	if _, t := a.lay.Tab(a.chosen); t != nil {
		a.showTab(t.ID)
		return
	}
	if _, ok := a.terms.Find(a.chosen); ok {
		a.showTerminal(a.chosen)
	}
}

// splitFocused splits the focused pane of the tab shown.
func (a *app) splitFocused(d layout.Direction) {
	if t := a.focusedTerminal(); t != "" {
		a.split(t, d)
	}
}

func main() {
	a := &app{sidebarWidth: 240, settings: loadSettings(), env: clientEnv()}
	a.term = term.New(a.settings.terminalFont(), lightTerminal, darkTerminal)
	// Only macOS: elsewhere the menu would add a bar to the window, and the
	// view takes the File menu's keys.
	if runtime.GOOS == "darwin" {
		mygo.App.SetMenu(a.menu())
	}
	// The app starts in the appearance the user chose.
	mygo.Theme.SetSource(a.settings.Appearance)
	center := a.term.HeaderCenter()
	mygo.App.WhenReady(func() {
		win := mygo.NewWindow(mygo.WindowOptions{
			Title:     "Mux",
			Width:     1100,
			Height:    700,
			MinWidth:  600,
			MinHeight: 400,
			StateKey:  "main",
			// The traffic lights sit over the sidebar (macOS); elsewhere the
			// window controls sit in the view's title bar. Not HiddenInset:
			// its toolbar keeps the moves and release of a press begun over
			// it, as on a pane's title, once the pointer leaves it.
			TitleBarStyle: mygo.TitleBarHidden,
			// The window controls center on the panes' headers along the
			// top: macOS's buttons are 16 DIPs high.
			TrafficLightPosition: &mygo.Point{X: 18, Y: int(math.Round(float64(center) - 8))},
			TitleBarHeight:       int(math.Round(float64(2 * center))),
			Content:              ui.View(a.view),
		})
		a.win, a.post = win, win.Update
		go a.connect()
		// The settings offer the font families, after the choices for none
		// of the user's; listing them takes a while with many fonts
		// installed.
		go func() {
			mono, all := []string{systemFont}, []string{noFont}
			for _, f := range ui.FontFamilies() {
				all = append(all, f.Name)
				if f.Monospaced {
					mono = append(mono, f.Name)
				}
			}
			a.update(func() { a.monoFonts, a.allFonts = mono, all })
		}()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
