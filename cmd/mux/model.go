package main

import (
	"bytes"
	"cmp"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mux/internal/hc"
	"mux/internal/layout"
)

// What the sidebar and the panes' headers call things, from the layout
// and the terminals' records.

// agentNames are the names of the agents that report their status, by the
// app they report (OSC 7501's app), and agentMarks the files of their
// marks in icons/agents.
var (
	agentNames = map[string]string{"claude-code": "Claude Code", "codex": "Codex"}
	agentMarks = map[string]string{"claude-code": "claude", "claude": "claude", "codex": "codex"}
)

// agent returns what a terminal's program reports of itself, if it does:
// those that do are agents.
func (a *app) agent(address string) (*hc.Status, bool) {
	t, ok := a.terms.Find(address)
	if !ok || t.Status == nil {
		return nil, false
	}
	return t.Status, true
}

// agentName names an agent by the app it reports, else its program.
func (a *app) agentName(address string) string {
	if s, ok := a.agent(address); ok && s.App != "" {
		return cmp.Or(agentNames[s.App], s.App)
	}
	if t, ok := a.terms.Find(address); ok && t.Foreground != nil {
		return t.Foreground.Name
	}
	return ""
}

// agents returns the terminals whose programs report their status, in the
// core's order.
func (a *app) agents() []hc.Terminal {
	var out []hc.Terminal
	for _, t := range a.terms.List {
		if t.Live() && t.Status != nil {
			out = append(out, t)
		}
	}
	return out
}

// paneName is what a pane is called: the name it was given, else its
// agent's, else its program's, else "shell".
func (a *app) paneName(address string) string {
	if _, _, n := a.lay.Find(address); n != nil && n.Name != "" {
		return n.Name
	}
	if _, ok := a.agent(address); ok {
		return a.agentName(address)
	}
	if t, ok := a.terms.Find(address); ok && t.Foreground != nil && t.Foreground.Name != "" {
		return t.Foreground.Name
	}
	return "shell"
}

// dirOf returns where a terminal's program is, ~ for home.
func (a *app) dirOf(address string) string {
	return tildePath(a.cwdOf(address))
}

func tildePath(dir string) string {
	if home, err := os.UserHomeDir(); err == nil && dir != "" && strings.HasPrefix(dir, home) {
		return "~" + strings.TrimPrefix(dir, home)
	}
	return dir
}

// workspaceName is what a workspace is called: its name, else the folder
// of its first pane's program.
func (a *app) workspaceName(ws *layout.Workspace) string {
	if ws.Name != "" {
		return ws.Name
	}
	if terms := ws.Terminals(); len(terms) > 0 {
		if dir := a.cwdOf(terms[0]); dir != "" {
			if home, _ := os.UserHomeDir(); dir == home {
				return "~"
			}
			return filepath.Base(dir)
		}
	}
	return "Workspace"
}

// tabNames returns the name a tab was given, if any, and what it shows
// without one: its focused pane's name, else where it is.
func (a *app) tabNames(tab *layout.Tab) (name, shown string) {
	focus := tab.Focus
	if _, _, n := a.lay.Find(focus); n != nil && n.Name != "" {
		return tab.Name, n.Name
	}
	if _, ok := a.agent(focus); ok {
		return tab.Name, a.agentName(focus)
	}
	return tab.Name, cmp.Or(a.dirOf(focus), focus)
}

// tabStatus is the state of the tab's agent that most needs the user.
func (a *app) tabStatus(tab *layout.Tab) string {
	var states []string
	for _, n := range tab.Leaves() {
		if s, ok := a.agent(n.Terminal); ok {
			states = append(states, s.State)
		}
	}
	return urgentStatus(states)
}

// urgentStatus is the state that most needs the user: an agent waiting on
// them, else one failed, else one working, else one done.
func urgentStatus(states []string) string {
	rank := map[string]int{"blocked": 4, "error": 3, "working": 2, "done": 1}
	status := ""
	for _, s := range states {
		if status == "" || rank[s] > rank[status] {
			status = s
		}
	}
	return status
}

// branch is a folder's Git branch, as last read.
type branch struct {
	name    string
	read    time.Time
	reading bool
}

// branchTTL is how long a branch read stands.
const branchTTL = 3 * time.Second

// branchOf returns the Git branch of the checkout a terminal's program is
// in, as last read: it reads it again, off the main thread, once stale.
func (a *app) branchOf(address string) string {
	dir := a.cwdOf(address)
	if dir == "" {
		return ""
	}
	if a.branches == nil {
		a.branches = map[string]*branch{}
	}
	b := a.branches[dir]
	if b == nil {
		b = &branch{}
		a.branches[dir] = b
	}
	if !b.reading && time.Since(b.read) > branchTTL && a.post != nil {
		b.reading = true
		go func() {
			name := gitBranch(dir)
			a.update(func() { b.name, b.read, b.reading = name, time.Now(), false })
		}()
	}
	return b.name
}

// gitBranch reads the branch of the checkout dir is in, "" if none: a
// detached HEAD shows its commit, short.
func gitBranch(dir string) string {
	for d := dir; ; d = filepath.Dir(d) {
		git := filepath.Join(d, ".git")
		if fi, err := os.Stat(git); err == nil {
			if !fi.IsDir() {
				// A worktree's .git names its directory.
				data, err := os.ReadFile(git)
				if err != nil {
					return ""
				}
				rest, ok := bytes.CutPrefix(bytes.TrimSpace(data), []byte("gitdir: "))
				if !ok {
					return ""
				}
				git = string(rest)
				if !filepath.IsAbs(git) {
					git = filepath.Join(d, git)
				}
			}
			head, err := os.ReadFile(filepath.Join(git, "HEAD"))
			if err != nil {
				return ""
			}
			head = bytes.TrimSpace(head)
			if ref, ok := bytes.CutPrefix(head, []byte("ref: refs/heads/")); ok {
				return string(ref)
			}
			return string(head[:min(len(head), 7)])
		}
		if filepath.Dir(d) == d {
			return ""
		}
	}
}
