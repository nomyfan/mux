// Package layout is how Mux arranges the core's terminals: workspaces of
// tabs, each tab a tree of splits whose leaves are terminals. herdr-core
// has no layout of its own; Mux keeps this one as a document of the core
// (doc.get, doc.set), so it outlives the app. No UI dependency.
package layout

import (
	"crypto/rand"
	"encoding/hex"
	"slices"
)

// DocName is the name of the core's document Mux keeps the layout in.
const DocName = "mux"

// Layout is the document: the workspaces, in the sidebar's order, and the
// tab shown.
type Layout struct {
	Workspaces []*Workspace `json:"workspaces"`
	Active     string       `json:"active,omitempty"`
}

// Workspace is a group of tabs. Without a name, it is called after the
// folder of its first terminal.
type Workspace struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
	Tabs []*Tab `json:"tabs"`
}

// Tab is a tree of panes, one terminal each. Without a name, it is called
// after its focused pane.
type Tab struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
	Root *Node  `json:"root"`
	// Focus is the terminal of the pane last focused; Zoomed shows it
	// alone.
	Focus  string `json:"focus,omitempty"`
	Zoomed bool   `json:"zoomed,omitempty"`
}

// Node is a pane, which shows a terminal, or a split of two nodes: A left
// of B, or above it while Vertical, A taking Ratio of the room.
type Node struct {
	Terminal string  `json:"terminal,omitempty"`
	Name     string  `json:"name,omitempty"` // a pane's name, given by the user
	Vertical bool    `json:"vertical,omitempty"`
	Ratio    float64 `json:"ratio,omitempty"`
	A        *Node   `json:"a,omitempty"`
	B        *Node   `json:"b,omitempty"`
}

// Leaf reports whether the node is a pane.
func (n *Node) Leaf() bool { return n.A == nil || n.B == nil }

// NewID returns a new id for a workspace or tab, prefix first.
func NewID(prefix string) string {
	b := make([]byte, 4)
	rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

// Tab returns the tab id and its workspace.
func (l *Layout) Tab(id string) (*Workspace, *Tab) {
	for _, ws := range l.Workspaces {
		for _, t := range ws.Tabs {
			if t.ID == id {
				return ws, t
			}
		}
	}
	return nil, nil
}

// Workspace returns the workspace id.
func (l *Layout) Workspace(id string) *Workspace {
	for _, ws := range l.Workspaces {
		if ws.ID == id {
			return ws
		}
	}
	return nil
}

// Find returns the pane of a terminal, with its tab and workspace.
func (l *Layout) Find(terminal string) (*Workspace, *Tab, *Node) {
	for _, ws := range l.Workspaces {
		for _, t := range ws.Tabs {
			if n := t.Pane(terminal); n != nil {
				return ws, t, n
			}
		}
	}
	return nil, nil, nil
}

// ActiveTab returns the tab shown, and its workspace: the first tab, if
// the one shown is gone.
func (l *Layout) ActiveTab() (*Workspace, *Tab) {
	if ws, t := l.Tab(l.Active); t != nil {
		return ws, t
	}
	for _, ws := range l.Workspaces {
		if len(ws.Tabs) > 0 {
			return ws, ws.Tabs[0]
		}
	}
	return nil, nil
}

// AddWorkspace adds a workspace after the others, with a tab of a pane
// showing terminal, and shows the tab.
func (l *Layout) AddWorkspace(terminal string) (*Workspace, *Tab) {
	ws := &Workspace{ID: NewID("w")}
	l.Workspaces = append(l.Workspaces, ws)
	return ws, l.AddTab(ws, terminal)
}

// AddTab adds a tab of a pane showing terminal after a workspace's
// others, and shows it.
func (l *Layout) AddTab(ws *Workspace, terminal string) *Tab {
	t := &Tab{ID: NewID("t"), Root: &Node{Terminal: terminal}, Focus: terminal}
	ws.Tabs = append(ws.Tabs, t)
	l.Active = t.ID
	return t
}

// Remove removes the pane of a terminal: its split's other side takes the
// split's room. A tab left with no pane closes, and a workspace with no
// tab. It reports whether the terminal had a pane.
func (l *Layout) Remove(terminal string) bool {
	_, t, _ := l.Find(terminal)
	if t == nil {
		return false
	}
	if t.remove(terminal) {
		l.RemoveTab(t.ID)
		return true
	}
	if t.Focus == terminal {
		t.Focus = t.Leaves()[0].Terminal
	}
	return true
}

// RemoveTab removes a tab, and its workspace if it was the last; a tab
// shown leaves the one beside it shown.
func (l *Layout) RemoveTab(id string) {
	for wi, ws := range l.Workspaces {
		i := slices.IndexFunc(ws.Tabs, func(t *Tab) bool { return t.ID == id })
		if i < 0 {
			continue
		}
		ws.Tabs = slices.Delete(ws.Tabs, i, i+1)
		if l.Active == id {
			l.Active = ""
			if len(ws.Tabs) > 0 {
				l.Active = ws.Tabs[min(i, len(ws.Tabs)-1)].ID
			}
		}
		if len(ws.Tabs) == 0 {
			l.Workspaces = slices.Delete(l.Workspaces, wi, wi+1)
		}
		if _, t := l.ActiveTab(); t != nil && l.Active == "" {
			l.Active = t.ID
		}
		return
	}
}

// Terminals returns the terminals of a workspace's panes.
func (ws *Workspace) Terminals() []string {
	var out []string
	for _, t := range ws.Tabs {
		for _, n := range t.Leaves() {
			out = append(out, n.Terminal)
		}
	}
	return out
}

// Terminals returns the terminals of all the panes.
func (l *Layout) Terminals() []string {
	var out []string
	for _, ws := range l.Workspaces {
		out = append(out, ws.Terminals()...)
	}
	return out
}

// Prune removes the panes of the terminals that are not live, reporting
// whether it removed any.
func (l *Layout) Prune(live func(terminal string) bool) bool {
	pruned := false
	for _, terminal := range l.Terminals() {
		if !live(terminal) {
			l.Remove(terminal)
			pruned = true
		}
	}
	return pruned
}

// Repair mends a layout read from the document: nodes missing a side, a
// tab with no pane, ratios out of bounds, ids missing.
func (l *Layout) Repair() {
	for _, ws := range l.Workspaces {
		if ws.ID == "" {
			ws.ID = NewID("w")
		}
		ws.Tabs = slices.DeleteFunc(ws.Tabs, func(t *Tab) bool {
			if t == nil {
				return true
			}
			t.Root = repair(t.Root)
			return t.Root == nil
		})
		for _, t := range ws.Tabs {
			if t.ID == "" {
				t.ID = NewID("t")
			}
			if t.Pane(t.Focus) == nil {
				t.Focus = t.Leaves()[0].Terminal
			}
		}
	}
	l.Workspaces = slices.DeleteFunc(l.Workspaces, func(ws *Workspace) bool { return ws == nil || len(ws.Tabs) == 0 })
}

func repair(n *Node) *Node {
	switch {
	case n == nil:
		return nil
	case n.A == nil && n.B == nil:
		if n.Terminal == "" {
			return nil
		}
		return n
	}
	n.A, n.B = repair(n.A), repair(n.B)
	switch {
	case n.A == nil:
		return n.B
	case n.B == nil:
		return n.A
	}
	n.Terminal, n.Name = "", ""
	if n.Ratio <= 0 || n.Ratio >= 1 {
		n.Ratio = 0.5
	}
	return n
}
