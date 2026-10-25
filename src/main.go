// Mux is a native client of herdr: the session's tabs in a sidebar, with
// the states of their agents, beside the tab chosen, which herdr lays out
// and the app paints.
package main

import (
	"cmp"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"mux/src/herdr/endpoint"
	"mux/src/herdr/server"
	"mux/src/term"
)

type app struct {
	socket string
	win    *mygo.Window
	// serverDown is set as no server listens on the socket; starting while
	// the app starts one, and stopping while the user confirms stopping it.
	// serverErr is why starting or stopping it failed.
	serverDown bool
	starting   bool
	stopping   bool
	serverErr  string
	// conn is the connection to herdr's client endpoint, nil while there is
	// none; snap and surface are what it last showed.
	conn    *endpoint.Conn
	snap    endpoint.Snapshot
	surface *endpoint.Surface
	err     string
	// chosen is the tab the sidebar shows chosen: the one the client's
	// view shows, or one the user chose that the server is focusing.
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
	// tabFocus holds the pane each tab last focused, as the snapshots
	// told: herdr focuses it again as the tab shows.
	tabFocus map[string]string
	// renaming is the workspace whose name the sidebar edits, newLabel
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

	// queue holds the requests waiting to be sent: the endpoint takes one
	// at a time, refusing another until it has answered (endpoint_busy).
	// sent is the one it is answering, if any.
	queue []*reply
	sent  *reply

	// inbox holds the messages the connection read for the main thread.
	mu    sync.Mutex
	inbox []any
}

// reply is a request, and its response as its chunks come, for done.
type reply struct {
	method string
	params any
	id     string
	data   []byte
	done   func(result json.RawMessage, err error)
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

	branchIcon       = icon(`<circle cx="4.5" cy="3.5" r="1.5"/><circle cx="4.5" cy="12.5" r="1.5"/><circle cx="11.5" cy="5.5" r="1.5"/><path d="M4.5 5v6M11.5 7c0 2.5-2 3.5-7 4"/>`)
	splitRightIcon   = icon(`<rect x="1.5" y="2.5" width="13" height="11" rx="2"/><path d="M8 2.5v11"/>`)
	splitDownIcon    = icon(`<rect x="1.5" y="2.5" width="13" height="11" rx="2"/><path d="M1.5 8h13"/>`)
	zoomIcon         = icon(`<path d="M9.5 2.5h4v4M13.5 2.5l-4.5 4.5M6.5 13.5h-4v-4M2.5 13.5l4.5-4.5"/>`)
	unzoomIcon       = icon(`<path d="M13.5 6.5h-4v-4M9.5 6.5l4-4M2.5 9.5h4v4M6.5 9.5l-4 4"/>`)
	closeIcon        = icon(`<path d="M4 4l8 8M12 4l-8 8"/>`)
	moreIcon         = icon(`<path stroke-width="2" d="M3.5 8h.01M8 8h.01M12.5 8h.01"/>`)
	newTabIcon       = icon(`<path d="M8 3v10M3 8h10"/>`)
	newWorkspaceIcon = icon(`<path d="M1.5 4.5a1 1 0 011-1h3l1.5 1.5h6.5a1 1 0 011 1v6.5a1 1 0 01-1 1h-11a1 1 0 01-1-1z"/><path d="M8 7.5v4M6 9.5h4"/>`)
	collapseIcon     = icon(`<path d="M4 6l4 4 4-4"/>`)
	expandIcon       = icon(`<path d="M6 4l4 4-4 4"/>`)
)

//go:embed icons/agents/*.svg
var agentIconFiles embed.FS

// agentIcons are the agents' own marks in their own colors, those without
// colors drawn in currentColor, by the kind herdr names them with.
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
		a.splitFocused("right")
	}
	if c.Shortcut(ui.Cmd|ui.Shift, ui.KeyD) {
		a.splitFocused("down")
	}
	if c.Shortcut(ui.Cmd, ui.KeyComma) {
		a.openSettings()
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
				Children(func() { a.sidebar(c, w) })
		})
		if w > 0 {
			ui.Box(c).Width(1).Background(t.Border)
		}
		// The terminal takes the room the sidebar leaves once it settles.
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

// sidebar is the session's panes by workspace, under the room the window
// controls take, as much of it as shows: w wide.
func (a *app) sidebar(c *ui.Context, w float32) {
	ui.Row(c).Height(a.titleBand(c)).Shrink(0).AlignItems(ui.Center).Padding(0, 8, 0, 0).DragWindow().Children(func() {
		ui.Spacer(c)
		// The button fades out as the sliding sidebar takes it toward the
		// window controls and the sidebar's toggle, rather than across them.
		const fade = 24
		controls := c.TitleBar().Left + 8 + 32
		b := iconButton(c, "new-workspace", newWorkspaceIcon, "New Workspace")
		b.Opacity(min(max((w-8-26-controls-4)/fade, 0), 1))
		if b.Clicked() {
			a.newWorkspace()
		}
	})
	ui.Sidebar(c, &a.chosen, func() {
		prev := ""
		for _, ws := range a.snap.Workspaces {
			a.section(c, ws.ID, prev, func(open bool) { a.workspaceHeader(c, ws, open) }, func() {
				for _, tab := range a.snap.Tabs {
					if tab.Workspace == ws.ID {
						a.tabItem(c, tab)
					}
				}
			})
			prev = ws.ID
		}
		if len(a.snap.Agents) > 0 {
			a.section(c, agentsSection, prev, func(open bool) { a.agentsHeader(c, open) }, func() {
				for _, ag := range a.snap.Agents {
					a.agentItem(c, ag)
				}
			})
		}
	}).Grow(1).Label("Tabs and Agents").OnChange(a.focusChosen)
	// Without a server, the panes' room says so.
	if a.err != "" && !a.serverDown {
		ui.Text(c, a.err).FontSize(12).TextColor(c.Theme().Danger).Padding(8, 12)
	}
	if cl := a.closing; cl != nil {
		if ui.AlertDialog(c, &a.confirming, fmt.Sprintf("Close “%s”?", cl.name), "Its panes close, and what runs in them stops.", "Cancel", "Close") == 1 {
			a.request(cl.method, cl.params, nil)
		}
		if !a.confirming {
			a.closing = nil
		}
	}
}

// closing is a workspace or tab to close, by name, once the user confirms,
// with the request closing it.
type closing struct {
	name   string
	method string
	params map[string]any
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
// focuses its pane.
func (a *app) agentItem(c *ui.Context, ag endpoint.Agent) {
	t := c.Theme()
	pane := a.snapPane(ag.Pane)
	icon, mark := itemIcon(ag, true)
	item := ui.SidebarItem(c.Key("agent "+ag.Pane), ag.Pane, blankIcon, cmp.Or(pane.Label, ag.Agent, paneLabel(pane)))
	chosenLook(c, item, ag.Pane == a.chosen)
	item.Children(func() {
		iconOver(c, icon, mark)
		for _, ws := range a.snap.Workspaces {
			if ws.ID == pane.Workspace {
				ui.Text(c, ws.Label).FontSize(t.FontSize - 1).TextColor(t.TextMuted).SingleLine().Shrink(1).MinWidth(0)
			}
		}
		statusDot(c, ag.Status)
	})
}

// itemIcon returns what shows for what an agent, if any, isAgent, runs:
// the agent's mark, if it has one, else an icon, a shell's or an agent's.
func itemIcon(agent endpoint.Agent, isAgent bool) (icon, mark *ui.SVG) {
	switch mark, ok := agentIcons[agent.Agent]; {
	case ok:
		return nil, mark
	case isAgent:
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
// or its arrow, shows the panes of or hides, with buttons making a tab in
// it and opening its menu, as a secondary click on it does; the buttons
// show as the pointer comes over it, or one has the focus, as while the
// menu is open.
func (a *app) workspaceHeader(c *ui.Context, ws endpoint.Workspace, open bool) {
	t := c.Theme()
	head := ui.Row(c.Key("head")).Height(28).AlignItems(ui.Center).Gap(2).Padding(0, t.Space(1), 0, t.Space(2)).Radius(t.Radius).Shrink(0).
		ContextMenu(a.workspaceMenu(ws))
	shown := head.Hovered() || head.FocusWithin()
	toggle := head.Clicked()
	head.Children(func() {
		if a.renaming == ws.ID {
			a.renameField(c, "Workspace Name", ws.Label, func(name string) {
				// herdr would name it nothing.
				if name != "" {
					a.request("workspace.rename", map[string]any{"workspace_id": ws.ID, "label": name}, nil)
				}
			}).Grow(1).MinWidth(0).Height(24).Padding(0, 4).FontSize(t.FontSize - 1)
		} else {
			ui.Text(c, ws.Label).FontSize(t.FontSize - 1).FontWeight(600).TextColor(t.TextMuted).Grow(1).SingleLine()
		}
		// Hidden, the buttons keep their room, and the menu its button.
		actions := ui.Row(c.Key("actions")).Gap(2).Shrink(0)
		if !shown {
			actions.Opacity(0)
		}
		actions.Children(func() {
			if headerButton(c, "new-tab", newTabIcon, "New Tab") {
				a.request("tab.create", map[string]any{"workspace_id": ws.ID, "focus": true}, nil)
				// The new tab's pane shows.
				a.collapse(ws.ID, false)
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
// else by its active pane: the pane's name, its agent, or its directory;
// with the state of the tab's agent that most needs the user, and a button
// closing it, once the user confirms, as the pointer comes over it, as its
// menu does.
func (a *app) tabItem(c *ui.Context, tab endpoint.Tab) {
	pane := a.activePane(tab.ID)
	agent, isAgent := a.agentIn(pane.ID)
	var agents []endpoint.Agent
	for _, p := range a.snap.Panes {
		if ag, ok := a.agentIn(p.ID); ok && p.Tab == tab.ID {
			agents = append(agents, ag)
		}
	}
	status := urgentStatus(agents)
	shown := paneLabel(pane)
	if isAgent && pane.Label == "" {
		shown = cmp.Or(agent.Agent, shown)
	}
	// herdr names a tab by its number, until it is given a name.
	name := tab.Label
	if _, err := strconv.Atoi(name); err == nil {
		name = ""
	}
	label := cmp.Or(name, shown)
	icon, mark := itemIcon(agent, isAgent)
	if a.renaming == tab.ID {
		a.renameTab(c, tab, icon, mark, name, shown)
		return
	}
	closeTab := func() { a.confirmClose(closing{label, "tab.close", map[string]any{"tab_id": tab.ID}}) }
	item := ui.SidebarItem(c.Key(tab.ID), tab.ID, blankIcon, label).ContextMenu(func(m *ui.Menu) {
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

// urgentStatus is the state of the agent that most needs the user: one
// waiting on them, else one working, else one done.
func urgentStatus(agents []endpoint.Agent) string {
	rank := map[string]int{"blocked": 3, "working": 2, "done": 1}
	status := ""
	for _, ag := range agents {
		if status == "" || rank[ag.Status] > rank[status] {
			status = ag.Status
		}
	}
	return status
}

// activePane returns a tab's active pane: the one it last focused, as the
// app saw, else, as the tab has not shown since the app started, its first
// agent's, else its first.
func (a *app) activePane(tabID string) endpoint.Pane {
	var focused, firstAgent, first endpoint.Pane
	for _, p := range a.snap.Panes {
		if p.Tab != tabID {
			continue
		}
		if first.ID == "" {
			first = p
		}
		if _, ok := a.agentIn(p.ID); ok && firstAgent.ID == "" {
			firstAgent = p
		}
		if p.ID == a.tabFocus[tabID] {
			focused = p
		}
	}
	return cmp.Or(focused, firstAgent, first)
}

// renameTab edits the name of a tab, if any, in place of its item, as tall
// as the item, beside its icon, or mark, if any: no name, which herdr
// cannot give, names it by its number again, as herdr does a new tab, and
// what it shows without a name, unnamed, shows for it again.
func (a *app) renameTab(c *ui.Context, tab endpoint.Tab, icon, mark *ui.SVG, name, unnamed string) {
	t := c.Theme()
	ui.Row(c.Key(tab.ID)).AlignItems(ui.Center).Gap(t.Space(2)).Padding(t.Space(1.5), t.Space(2)).Shrink(0).Children(func() {
		drawIcon(c, icon, mark)
		// A line of text sizes the row as the item's label does; the field
		// lies over it.
		ui.Box(c).Grow(1).MinWidth(0).Children(func() {
			ui.Text(c, " ").SingleLine()
			a.renameField(c, "Tab Name", name, func(name string) {
				a.request("tab.rename", map[string]any{"tab_id": tab.ID, "label": cmp.Or(name, strconv.Itoa(tab.Number))}, nil)
			}).Placeholder(unnamed).Absolute().Left(-4).Right(0).Top(-3).Bottom(-3).Padding(0, 4)
		})
	})
}

// newWorkspace asks the user for a folder, in which it makes a workspace,
// named after the folder, which it focuses.
func (a *app) newWorkspace() {
	win := a.win
	go func() {
		home, _ := os.UserHomeDir()
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: win, Title: "New Workspace",
			Message: "Choose the folder the workspace works in.", ButtonLabel: "Create",
			DefaultPath: home, Directory: true, CreateDirectories: true})
		win.Update(func() {
			switch {
			case err != nil:
				a.err = err.Error()
			case len(paths) > 0:
				a.request("workspace.create", map[string]any{"cwd": paths[0], "focus": true}, nil)
			}
		})
	}()
}

// workspaceMenu builds the menu of a workspace's actions.
func (a *app) workspaceMenu(ws endpoint.Workspace) func(m *ui.Menu) {
	return func(m *ui.Menu) {
		if m.Item("Rename").Chosen() {
			a.renaming, a.newLabel = ws.ID, ws.Label
		}
		m.Separator()
		if m.Item("Close Workspace…").Chosen() {
			a.confirmClose(closing{ws.Label, "workspace.close", map[string]any{"workspace_id": ws.ID}})
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

// pane is the focused tab, each of its panes under a header naming it.
// The headers along the top hold the window controls' room: titleLeft is
// where the top-left one's title starts.
func (a *app) pane(c *ui.Context, titleLeft float32) {
	bar := c.TitleBar()
	var ep term.Endpoint
	if a.conn != nil {
		ep = a.conn
	}
	header := func(c *ui.Context, p endpoint.SurfacePane, box ui.Rect, hovered bool) {
		left, right := float32(12), float32(8)
		if p.Rect.Y == 0 {
			if p.Rect.X == 0 {
				left = max(left, titleLeft-box.X)
			}
			// The window's buttons on the right (Linux, Windows).
			if p.Rect.X+p.Rect.Width >= a.surface.Frame.Width {
				right += bar.Right
			}
		}
		a.paneHeader(c, p, left, right, p.Focused || hovered)
	}
	resize := func(paneID string, path []bool, ratio float64) {
		// The root split's path is empty, not missing.
		a.request("layout.set_split_ratio", map[string]any{"pane_id": paneID, "path": append([]bool{}, path...), "ratio": ratio}, nil)
	}
	if a.conn == nil && a.serverDown {
		a.serverDownView(c)
		return
	}
	hooks := term.Hooks{Header: header, Focus: a.focusPane, Resize: resize, Request: a.request,
		Title:     func(c *ui.Context, p endpoint.SurfacePane) { a.paneTitle(c, p, false) },
		Rearrange: a.rearrange}
	a.term.Build(c, a.surface, ep, hooks).Grow(1).MinWidth(0).AutoFocus()
}

// paneHeader shows what a pane runs, where, and on which branch, with the
// actions on it while actions holds: as the pane has the focus or the
// pointer.
func (a *app) paneHeader(c *ui.Context, sp endpoint.SurfacePane, left, right float32, actions bool) {
	t := c.Theme()
	p := a.snapPane(sp.ID)
	// The title's own padding lines its text up with the header's.
	ui.Row(c).Grow(1).Height(28).AlignItems(ui.Center).Gap(8).Padding(0, right, 0, left-8).MinWidth(0).Clip().Children(func() {
		if a.renaming == sp.ID {
			a.renamePane(c, p)
		} else {
			// The user drags the pane by its title, which shows as much
			// as the pointer comes over it.
			title := ui.Row(c.Key("title")).Bind(a.term.TitleHandle(sp.ID)).AlignItems(ui.Center).Gap(8).
				Padding(3, 8).Radius(6).Shrink(1).MinWidth(0).Cursor(ui.CursorMove).
				ContextMenu(func(m *ui.Menu) {
					if m.Item("Rename").Chosen() {
						a.renaming, a.newLabel = sp.ID, p.Label
					}
				})
			grip := title.Hovered()
			if grip {
				title.Background(t.Text.Alpha(0.07))
			}
			title.Children(func() { a.paneTitle(c, sp, grip) })
		}
		ui.Spacer(c)
		if !actions {
			return
		}
		zoomed := false
		for _, tab := range a.snap.Tabs {
			if tab.ID == p.Tab {
				zoomed = tab.Zoomed
			}
		}
		zoomLabel, zoom := "Zoom", zoomIcon
		if zoomed {
			zoomLabel, zoom = "Unzoom", unzoomIcon
		}
		ui.Row(c).Gap(2).Shrink(0).Children(func() {
			if headerButton(c, "split-right", splitRightIcon, "Split Right") {
				a.split(p.ID, "right")
			}
			if headerButton(c, "split-down", splitDownIcon, "Split Down") {
				a.split(p.ID, "down")
			}
			if headerButton(c, "zoom", zoom, zoomLabel) {
				a.request("pane.zoom", map[string]any{"pane_id": p.ID, "mode": "toggle"}, nil)
			}
			if headerButton(c, "close", closeIcon, "Close Pane") {
				a.request("pane.close", map[string]any{"pane_id": p.ID}, nil)
			}
		})
	})
}

// snapPane returns what the snapshot says of a pane.
func (a *app) snapPane(id string) endpoint.Pane {
	for _, p := range a.snap.Panes {
		if p.ID == id {
			return p
		}
	}
	return endpoint.Pane{}
}

// renamePane edits a pane's name in place of its title, which no longer
// drags the pane meanwhile, so that a press in the field is the field's:
// no name, its agent's or "shell" shows for it again.
func (a *app) renamePane(c *ui.Context, p endpoint.Pane) {
	ui.Row(c.Key("rename")).AlignItems(ui.Center).Padding(0, 0, 0, 8).Shrink(1).MinWidth(0).Children(func() {
		a.renameField(c, "Pane Name", p.Label, func(name string) {
			a.request("pane.rename", map[string]any{"pane_id": p.ID, "label": name}, nil)
		}).Placeholder(a.paneName(p)).Width(240).MinWidth(0).Shrink(1).Height(24).Padding(0, 6)
	})
}

// paneName is what a pane is called without a name of its own: its
// agent, or "shell".
func (a *app) paneName(p endpoint.Pane) string {
	if agent, ok := a.agentIn(p.ID); ok && agent.Agent != "" {
		return agent.Agent
	}
	return "shell"
}

// paneTitle shows what a pane runs, where, and on which branch, with the
// mark of the agent, or a shell's; grip shows the mark of moving instead.
func (a *app) paneTitle(c *ui.Context, sp endpoint.SurfacePane, grip bool) {
	t := c.Theme()
	p := a.snapPane(sp.ID)
	agent, isAgent := a.agentIn(p.ID)
	name, dir := cmp.Or(p.Label, a.paneName(p)), paneLabel(endpoint.Pane{Cwd: p.Cwd})
	if mark, ok := agentIcons[agent.Agent]; ok && !grip {
		ui.Image(c, mark).Size(16, 16)
	} else {
		icon := shellIcon
		if grip {
			icon = moveIcon
		}
		ui.Icon(c, icon).Size(16, 16).TextColor(t.TextMuted)
	}
	title := ui.Text(c, name).Bold().SingleLine().Shrink(0)
	if !sp.Focused {
		title.TextColor(t.TextMuted)
	}
	if isAgent {
		statusDot(c, agent.Status)
	}
	ui.Text(c, dir).TextColor(t.TextMuted).SingleLine().Shrink(1).MinWidth(0)
	if branch := a.branchOf(p.Workspace); branch != "" {
		ui.Icon(c, branchIcon).Size(14, 14).TextColor(t.TextMuted)
		ui.Text(c, branch).TextColor(t.TextMuted).SingleLine().Shrink(1).MinWidth(0)
	}
}

// rearrange puts a pane the user dropped on another at side of it: in its
// place, swapping them, or beside it. The server moves a pane beside
// another only to another tab, so a new pane takes its place beside the
// other, the pane swaps places with it, and it closes, leaving the pane
// there; the requests go one after another.
func (a *app) rearrange(paneID, targetID string, side term.Side) {
	swap := func(source, target string) {
		a.request("pane.swap", map[string]any{"source_pane_id": source, "target_pane_id": target}, nil)
	}
	if side == term.SideCenter {
		swap(paneID, targetID)
		return
	}
	direction := "right"
	if side == term.SideTop || side == term.SideBottom {
		direction = "down"
	}
	a.request("pane.split", map[string]any{"target_pane_id": targetID, "direction": direction}, func(result json.RawMessage, err error) {
		var r struct {
			Pane struct {
				ID string `json:"pane_id"`
			}
		}
		if err != nil || json.Unmarshal(result, &r) != nil || r.Pane.ID == "" {
			return
		}
		if side == term.SideLeft || side == term.SideTop {
			swap(targetID, r.Pane.ID)
		}
		swap(paneID, r.Pane.ID)
		a.request("pane.close", map[string]any{"pane_id": r.Pane.ID}, nil)
		a.focusPane(paneID)
	})
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

// branchOf returns the Git branch of a workspace's checkout.
func (a *app) branchOf(workspaceID string) string {
	for _, ws := range a.snap.Workspaces {
		if ws.ID == workspaceID {
			return ws.Branch
		}
	}
	return ""
}

// titleBand is the height of the band along the top of the window that the
// window controls sit in, centered on the panes' headers along the top.
func (a *app) titleBand(c *ui.Context) float32 {
	return cmp.Or(c.TitleBar().Height, 2*a.term.HeaderCenter())
}

// agentIn returns the agent herdr recognizes in the pane, if any.
func (a *app) agentIn(paneID string) (endpoint.Agent, bool) {
	for _, ag := range a.snap.Agents {
		if ag.Pane == paneID {
			return ag, true
		}
	}
	return endpoint.Agent{}, false
}

// paneLabel names a pane: the name it was given, else its directory.
func paneLabel(p endpoint.Pane) string {
	dir := p.Cwd
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(dir, home) {
		dir = "~" + strings.TrimPrefix(dir, home)
	}
	return cmp.Or(p.Label, dir, p.ID)
}

// statusDot shows an agent's state as a dot in a soft halo, as Coda's: a
// working agent's halo breathes, a blocked one's ripples out for the user,
// a done one's and an idle one's stay still, as all do for less motion.
func statusDot(c *ui.Context, status string) {
	t := c.Theme()
	color := t.TextMuted
	switch status {
	case "working":
		color = t.Accent
	case "blocked":
		color = t.Warning
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

// focusChosen asks the server to focus the tab, or agent's pane, the user
// chose in the sidebar.
func (a *app) focusChosen() {
	if a.snapPane(a.chosen).ID != "" {
		a.focusPane(a.chosen)
		return
	}
	a.request("tab.focus", map[string]string{"tab_id": a.chosen}, nil)
}

// split splits a pane, the new one, focused, going right of it or below
// it as direction is "right" or "down".
func (a *app) split(paneID, direction string) {
	a.request("pane.split", map[string]any{"target_pane_id": paneID, "direction": direction, "focus": true}, nil)
}

// splitFocused splits the focused pane of the tab shown.
func (a *app) splitFocused(direction string) {
	if a.surface == nil {
		return
	}
	for _, p := range a.surface.Panes {
		if p.Focused {
			a.split(p.ID, direction)
		}
	}
}

func (a *app) focusPane(id string) { a.request("pane.focus", map[string]string{"pane_id": id}, nil) }

// request sends a request of herdr's API for the client's view.
// request asks the server for method; done, if any, gets the result of
// the response, or why there is none.
func (a *app) request(method string, params any, done func(result json.RawMessage, err error)) {
	if done == nil {
		done = func(json.RawMessage, error) {}
	}
	if a.conn == nil {
		done(nil, errors.New("not connected"))
		return
	}
	a.queue = append(a.queue, &reply{method: method, params: params, done: done})
	a.send()
}

// send sends the first request waiting, once the endpoint has answered the
// one before.
func (a *app) send() {
	for a.sent == nil && len(a.queue) > 0 && a.conn != nil {
		r := a.queue[0]
		a.queue = a.queue[1:]
		id, err := a.conn.Request(a.snap.BootID, r.method, r.params)
		if err != nil {
			r.done(nil, err)
			continue
		}
		r.id, a.sent = id, r
	}
}

// answer hands a reply's response to it once its last chunk comes.
func (a *app) answer(c endpoint.ResponseChunk) {
	r := a.sent
	if r == nil || r.id != c.RequestID {
		return
	}
	r.data = append(r.data, c.Data...)
	if !c.Final {
		return
	}
	a.sent = nil
	defer a.send()
	var resp struct {
		Result json.RawMessage
		Error  *struct{ Code, Message string }
	}
	switch err := json.Unmarshal(r.data, &resp); {
	case err != nil:
		r.done(nil, err)
	case resp.Error != nil:
		r.done(nil, fmt.Errorf("%s: %s", resp.Error.Code, resp.Error.Message))
	default:
		r.done(resp.Result, nil)
	}
}

// connect keeps a connection to the endpoint, connecting again a second
// after one fails or ends.
func (a *app) connect(win *mygo.Window) {
	for {
		// The view asks for its own size once it has the connection.
		conn, err := endpoint.Dial(a.socket, endpoint.Size{Cols: 80, Rows: 24, CellWidth: 8, CellHeight: 16})
		if err == nil {
			win.Update(func() {
				a.conn, a.err, a.serverDown, a.starting, a.serverErr = conn, "", false, false, ""
				a.term.Reconnected()
			})
			err = a.read(win, conn)
			conn.Close()
		} else if server.NotRunning(err) {
			// What the server showed went with it.
			win.Update(func() { a.serverDown, a.snap = true, endpoint.Snapshot{} })
		}
		msg := err.Error()
		// The requests the connection took get no response.
		win.Update(func() { a.conn, a.surface, a.err, a.queue, a.sent = nil, nil, msg, nil, nil })
		time.Sleep(time.Second)
	}
}

// read queues the connection's messages for the main thread until it
// ends. A burst of output comes as many patches: one update takes them all.
func (a *app) read(win *mygo.Window, conn *endpoint.Conn) error {
	for {
		msg, err := conn.Next()
		if err != nil {
			return err
		}
		a.mu.Lock()
		a.inbox = append(a.inbox, msg)
		first := len(a.inbox) == 1
		a.mu.Unlock()
		if first {
			win.Update(a.receive)
		}
	}
}

// receive takes the messages the connection queued, on the main thread.
func (a *app) receive() {
	a.mu.Lock()
	msgs := a.inbox
	a.inbox = nil
	a.mu.Unlock()
	for _, msg := range msgs {
		switch m := msg.(type) {
		case *endpoint.Snapshot:
			a.snap, a.chosen = *m, m.FocusedTab
			if m.FocusedTab != "" {
				if a.tabFocus == nil {
					a.tabFocus = map[string]string{}
				}
				a.tabFocus[m.FocusedTab] = m.FocusedPane
			}
		case *endpoint.Surface:
			a.surface = m
		case *endpoint.SurfacePatch:
			if a.surface == nil || a.surface.Apply(m) != nil {
				// Out of step with the server: a new connection starts
				// from a whole surface.
				if a.conn != nil {
					a.conn.Close()
				}
				return
			}
		case endpoint.Clipboard:
			a.services.WriteClipboard(m.Text)
		case endpoint.ResponseChunk:
			a.answer(m)
		case endpoint.ShellError:
			a.err = m.Message
		}
	}
}

func main() {
	// Started from a herdr pane, as during development, the app inherits
	// the pane's HERDR_SOCKET_PATH, which would win over HERDR_SESSION.
	if os.Getenv("HERDR_ENV") == "1" {
		os.Unsetenv("HERDR_SOCKET_PATH")
	}
	socket, err := endpoint.DefaultSocketPath()
	if err != nil {
		log.Fatal(err)
	}
	a := &app{socket: socket, sidebarWidth: 240, settings: loadSettings()}
	a.term = term.New(a.settings.terminalFont())
	// The app starts in the appearance the user chose.
	mygo.Theme.SetSource(a.settings.Appearance)
	a.term.SetThemes(lightTerminal, darkTerminal)
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
		a.win = win
		go a.connect(win)
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
			win.Update(func() { a.monoFonts, a.allFonts = mono, all })
		}()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
