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

// state is an agent's state as the app shows it: done turns idle once the
// user has looked at its pane, as in Herdr app; the program itself stays
// done till its next turn.
func (a *app) state(address string) string {
	s, ok := a.agent(address)
	if !ok {
		return ""
	}
	if s.State == "done" && a.seen[address] {
		return "idle"
	}
	return s.State
}

// look notes that the user sees the agent whose pane has the keyboard,
// as the window does, reporting whether that agent was done unseen.
func (a *app) look() bool {
	focused := a.term.Focused()
	if s, ok := a.agent(focused); !ok || s.State != "done" || a.seen[focused] {
		return false
	}
	if a.seen == nil {
		a.seen = map[string]bool{}
	}
	a.seen[focused] = true
	return true
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

// dirOf returns where a terminal's program is, ~ for home; on another
// machine, after its name, as machine:path.
func (a *app) dirOf(address string) string {
	dir := a.cwdOf(address)
	if machine := hc.MachineOf(address); machine != "" && dir != "" {
		return machine + ":" + dir
	}
	return tildePath(dir)
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
			if machine := hc.MachineOf(terms[0]); machine != "" {
				return machine + ":" + baseName(dir)
			}
			if home, _ := os.UserHomeDir(); dir == home {
				return "~"
			}
			return filepath.Base(dir)
		}
	}
	return "Workspace"
}

// baseName is the last name of a path of any machine's, Windows' too.
func baseName(dir string) string {
	names := strings.FieldsFunc(dir, isSeparator)
	if len(names) == 0 {
		return dir
	}
	return names[len(names)-1]
}

func isSeparator(r rune) bool { return r == '/' || r == '\\' }

// absPath reports whether a path of any machine's is absolute: from the
// root, or a Windows drive's.
func absPath(p string) bool {
	return strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`) ||
		len(p) > 2 && p[1] == ':' && isSeparator(rune(p[2]))
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
		if _, ok := a.agent(n.Terminal); ok {
			states = append(states, a.state(n.Terminal))
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

// branchTTL is how long a branch read stands; on another machine, whose
// files are a few round trips over ssh away, remoteBranchTTL.
const (
	branchTTL       = 3 * time.Second
	remoteBranchTTL = 10 * time.Second
)

// branchOf returns the Git branch of the checkout a terminal's program is
// in, as last read: it reads it again, off the main thread, once stale;
// on another machine, through the core.
func (a *app) branchOf(address string) string {
	machine, dir := hc.MachineOf(address), a.cwdOf(address)
	if dir == "" {
		return ""
	}
	if a.branches == nil {
		a.branches = map[string]*branch{}
	}
	key := machine + ":" + dir
	b := a.branches[key]
	if b == nil {
		b = &branch{}
		a.branches[key] = b
	}
	ttl, conn, read := branchTTL, a.conn, readFrom(dir)
	if machine != "" {
		// Away, the machine keeps the branch last read.
		if conn == nil || a.machineDown(machine) != "" {
			return b.name
		}
		ttl = remoteBranchTTL
		read = func(path string) ([]byte, error) {
			text, err := conn.ReadFile(machine, dir, path, 1)
			return []byte(text), err
		}
	}
	if !b.reading && time.Since(b.read) > ttl && a.post != nil {
		b.reading = true
		go func() {
			name := gitBranch(dir, read)
			a.update(func() { b.name, b.read, b.reading = name, time.Now(), false })
		}()
	}
	return b.name
}

// gitBranch reads the branch of the checkout dir is in, "" if none, with
// read, which reads a file by its path, absolute or from dir: a detached
// HEAD shows its commit, short. The folders above dir are ../ from it, so
// that a path of another machine's, as Windows', needs no parsing.
func gitBranch(dir string, read func(path string) ([]byte, error)) string {
	up := ""
	// dir, and a folder above it for each of its names, up to the root.
	for range len(strings.FieldsFunc(dir, isSeparator)) + 1 {
		if head, err := read(up + ".git/HEAD"); err == nil {
			return headBranch(head)
		}
		if data, err := read(up + ".git"); err == nil {
			// A worktree's .git names its directory.
			git, ok := bytes.CutPrefix(bytes.TrimSpace(data), []byte("gitdir: "))
			if !ok {
				return ""
			}
			path := string(git) + "/HEAD"
			if !absPath(path) {
				path = up + path
			}
			head, err := read(path)
			if err != nil {
				return ""
			}
			return headBranch(head)
		}
		up += "../"
	}
	return ""
}

// readFrom reads a file of this machine's by its path, absolute or from
// dir.
func readFrom(dir string) func(path string) ([]byte, error) {
	return func(path string) ([]byte, error) {
		if !filepath.IsAbs(path) {
			path = filepath.Join(dir, path)
		}
		return os.ReadFile(path)
	}
}

// headBranch is the branch a HEAD file names; a detached one's commit,
// short.
func headBranch(head []byte) string {
	head = bytes.TrimSpace(head)
	if ref, ok := bytes.CutPrefix(head, []byte("ref: refs/heads/")); ok {
		return string(ref)
	}
	return string(head[:min(len(head), 7)])
}
