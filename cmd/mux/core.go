package main

import (
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"mux/internal/hc"
	"mux/internal/layout"
	"mux/internal/term"
)

// The app's side of herdr-core: a connection, kept by a goroutine of its
// own, the terminals as its event stream keeps them, and the layout, which
// the app keeps in a document of the core.

// gate holds the connection back while the user stopped the core, until
// they start it again.
type gate struct {
	mu      sync.Mutex
	stopped bool
	resume  chan struct{}
}

func (g *gate) stop() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.stopped {
		g.stopped, g.resume = true, make(chan struct{})
	}
}

func (g *gate) start() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.stopped {
		g.stopped = false
		close(g.resume)
	}
}

// wait waits while the core is stopped.
func (g *gate) wait() {
	g.mu.Lock()
	stopped, resume := g.stopped, g.resume
	g.mu.Unlock()
	if stopped {
		<-resume
	}
}

// connect keeps a connection to the core, connecting again a second after
// one fails or ends: `hc connect` starts the core if none runs, unless
// the user stopped it.
func (a *app) connect() {
	// Reading the login shell's environment takes a while: not before the
	// window opens.
	env := a.env
	if env == nil {
		env = clientEnv()
		a.update(func() { a.env = env })
	}
	for {
		a.gate.wait()
		err := a.session(env)
		msg := err.Error()
		a.update(func() { a.disconnected(msg) })
		time.Sleep(time.Second)
	}
}

// session connects to the core, running its client in env, and reads its
// events until the connection ends, returning why.
func (a *app) session(env []string) error {
	bin, err := hc.Find()
	if err != nil {
		a.update(func() { a.bin, a.notInstalled = "", true })
		time.Sleep(2 * time.Second)
		return err
	}
	conn, err := hc.Connect(bin, env)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := conn.Hello.Check(); err != nil {
		time.Sleep(4 * time.Second)
		return err
	}
	events, err := conn.Subscribe()
	if err != nil {
		return err
	}
	// Subscribed before reading the layout, so that no change of it goes
	// unseen.
	doc, err := conn.GetDoc(layout.DocName)
	if err != nil {
		return err
	}
	a.update(func() { a.connected(bin, conn, events, doc) })
	for {
		ev, err := events.Next()
		if err != nil {
			return err
		}
		a.mu.Lock()
		a.inbox = append(a.inbox, ev)
		first := len(a.inbox) == 1
		a.mu.Unlock()
		if first {
			a.update(a.receive)
		}
	}
}

// connected takes a new connection: the terminals as they are, and the
// layout as the core keeps it, without the panes of terminals gone since.
func (a *app) connected(bin string, conn *hc.Conn, events *hc.Events, doc hc.Doc) {
	a.bin, a.notInstalled, a.err, a.serverErr, a.starting = bin, false, "", "", false
	a.conn, a.gen = conn, a.gen+1
	a.terms = hc.Terminals{Seq: events.Seq, List: events.Terminals}
	a.term.Reset()
	a.adoptDoc(doc)
	if len(a.lay.Workspaces) == 0 {
		a.newWorkspace()
	}
}

// disconnected forgets the connection that ended: the terminals' views
// show they are disconnected until the next one.
func (a *app) disconnected(why string) {
	if a.conn != nil {
		a.conn.Close()
	}
	a.conn, a.err, a.saving, a.dirty = nil, why, false, false
	if a.stopped() {
		a.err = ""
	}
}

// stopped reports whether the user stopped the core.
func (a *app) stopped() bool {
	a.gate.mu.Lock()
	defer a.gate.mu.Unlock()
	return a.gate.stopped
}

// adoptDoc takes the layout in a document of the core, without the panes
// of terminals no longer live; changed so, it is saved.
func (a *app) adoptDoc(doc hc.Doc) {
	var l layout.Layout
	if len(doc.Value) > 0 && string(doc.Value) != "null" {
		if err := json.Unmarshal(doc.Value, &l); err != nil {
			a.err = "the layout document is broken: " + err.Error()
			l = layout.Layout{}
		}
	}
	l.Repair()
	a.lay, a.layVersion = l, doc.Version
	if a.lay.Prune(a.live) {
		a.save()
	}
	a.term.Retain(a.lay.Terminals())
	if _, t := a.lay.ActiveTab(); t != nil {
		a.chosen = t.ID
	}
}

// live reports whether a terminal is live.
func (a *app) live(address string) bool {
	t, ok := a.terms.Find(address)
	return ok && t.Live()
}

// receive takes the events the connection queued, on the main thread.
func (a *app) receive() {
	a.mu.Lock()
	evs := a.inbox
	a.inbox = nil
	a.mu.Unlock()
	pruned := false
	for _, ev := range evs {
		if ev.Type == "doc" {
			var d hc.DocChange
			if json.Unmarshal(ev.Data, &d) == nil && d.Name == layout.DocName && d.Version != a.layVersion && !a.saving {
				a.reloadDoc()
			}
			continue
		}
		a.terms.Apply(ev)
		switch ev.Type {
		case "restarted":
			// A kept terminal's new shell: its pane attaches again.
			a.term.Drop(ev.Terminal)
		case "finished":
			a.forget(ev.Terminal)
			pruned = true
		case "removed":
			pruned = true
		}
		if ev.Method == "missed" {
			pruned = true
		}
	}
	if pruned && a.lay.Prune(a.live) {
		a.term.Retain(a.lay.Terminals())
		a.save()
	}
}

// forget removes the record of a terminal of the app's that finished, as
// its shell exited: the core keeps finished terminals until removed.
func (a *app) forget(address string) {
	t, ok := a.terms.Find(address)
	if conn := a.conn; ok && conn != nil && t.Labels["mux"] != "" {
		go conn.Remove(address)
	}
}

// reloadDoc reads the layout again, as another app changed it.
func (a *app) reloadDoc() {
	conn, gen := a.conn, a.gen
	if conn == nil {
		return
	}
	go func() {
		doc, err := conn.GetDoc(layout.DocName)
		a.update(func() {
			if err == nil && gen == a.gen && !a.saving {
				a.adoptDoc(doc)
			}
		})
	}()
}

// save writes the layout to the core, one write at a time: a change while
// one goes writes again after it.
func (a *app) save() {
	a.dirty = true
	if a.saving || a.conn == nil {
		return
	}
	data, err := json.Marshal(&a.lay)
	if err != nil {
		a.err = err.Error()
		return
	}
	a.saving, a.dirty = true, false
	conn, gen, version := a.conn, a.gen, a.layVersion
	go func() {
		v, err := conn.SetDoc(layout.DocName, json.RawMessage(data), version)
		var doc hc.Doc
		if hc.Kind(err) == "conflict" {
			// Another app wrote it: its layout wins.
			doc, err = conn.GetDoc(layout.DocName)
		}
		a.update(func() {
			if gen != a.gen {
				return
			}
			a.saving = false
			switch {
			case err != nil:
				a.err = "saving the layout: " + err.Error()
			case doc.Version != 0:
				a.dirty = false
				a.adoptDoc(doc)
			default:
				a.layVersion = v
			}
			if a.dirty {
				a.save()
			}
		})
	}()
}

// update runs fn on the main thread, as the window's Update does: post,
// which tests replace.
func (a *app) update(fn func()) {
	if a.post != nil {
		a.post(fn)
	}
}

// changed saves the layout, which the user changed.
func (a *app) changed() { a.save() }

// terminalEnv is what a terminal's program is told of the terminal: as
// its emulator is Ghostty's, the core tells it it runs in Ghostty. Without
// a locale in env, as an app opened from the Finder has none, it gets
// UTF-8's, as terminal apps give theirs: in C's, shells take the bytes of
// a character for characters of their own.
func terminalEnv(env []string) map[string]string {
	vars := map[string]string{"TERM": "xterm-256color", "COLORTERM": "truecolor"}
	for _, kv := range env {
		switch name(kv) {
		case "LANG", "LC_ALL", "LC_CTYPE":
			if !strings.HasSuffix(kv, "=") {
				return vars
			}
		}
	}
	vars["LANG"] = "en_US.UTF-8"
	return vars
}

// run starts a shell in a new terminal, in cwd, "" for the core's choice,
// and gives its address to done once it runs.
func (a *app) run(cwd string, done func(address string)) {
	conn, gen := a.conn, a.gen
	if conn == nil {
		return
	}
	go func() {
		t, err := conn.Run(hc.RunOptions{Cwd: cwd, Env: terminalEnv(a.env), Labels: map[string]string{"mux": "1"}, Keep: true, NoParent: true})
		a.update(func() {
			if gen != a.gen {
				return
			}
			if err != nil {
				a.err = err.Error()
				return
			}
			a.terms.Put(t)
			done(t.Address)
		})
	}()
}

// kill ends terminals' programs and removes their records.
func (a *app) kill(addresses ...string) {
	conn := a.conn
	if conn == nil {
		return
	}
	for _, addr := range addresses {
		a.lay.Remove(addr)
	}
	a.term.Retain(a.lay.Terminals())
	a.save()
	go func() {
		for _, addr := range addresses {
			if err := conn.Kill(addr); err == nil || hc.Kind(err) != "not_found" {
				conn.Remove(addr)
			}
		}
	}()
}

// cwdOf returns where a new terminal beside a terminal starts: where its
// program is now, else where it started; "" for none.
func (a *app) cwdOf(address string) string {
	t, ok := a.terms.Find(address)
	if !ok {
		return ""
	}
	if t.Foreground != nil && t.Foreground.Cwd != "" {
		return t.Foreground.Cwd
	}
	return t.Cwd
}

// focusedTerminal returns the terminal of the focused pane of the tab
// shown.
func (a *app) focusedTerminal() string {
	if _, t := a.lay.ActiveTab(); t != nil {
		return t.Focus
	}
	return ""
}

// newTab makes a tab in a workspace and shows it, giving its pane the
// keyboard: it starts where the focused pane's program is.
func (a *app) newTab(workspaceID string) {
	if a.lay.Workspace(workspaceID) == nil {
		return
	}
	a.run(a.cwdOf(a.focusedTerminal()), func(addr string) {
		ws := a.lay.Workspace(workspaceID)
		if ws == nil {
			ws, _ = a.lay.AddWorkspace(addr)
		} else {
			a.lay.AddTab(ws, addr)
		}
		a.collapse(ws.ID, false)
		a.shown()
	})
}

// newWorkspace makes a workspace and shows it, as newTab does a tab.
func (a *app) newWorkspace() {
	a.run(a.cwdOf(a.focusedTerminal()), func(addr string) {
		a.lay.AddWorkspace(addr)
		a.shown()
	})
}

// split splits a terminal's pane, the new one, focused, going right of it
// or below it.
func (a *app) split(address string, d layout.Direction) {
	_, tab, _ := a.lay.Find(address)
	if tab == nil {
		return
	}
	id := tab.ID
	a.run(a.cwdOf(address), func(addr string) {
		if _, tab := a.lay.Tab(id); tab != nil && tab.Split(address, addr, d) {
			tab.Zoomed = false
			a.shown()
		} else {
			a.kill(addr)
		}
	})
}

// shown saves the layout, changed to show a tab or pane, and gives the
// keyboard to its focused pane.
func (a *app) shown() {
	if _, t := a.lay.ActiveTab(); t != nil {
		a.chosen = t.ID
	}
	a.term.Focus()
	a.save()
}

// showTab shows a tab.
func (a *app) showTab(id string) {
	if _, t := a.lay.Tab(id); t != nil && a.lay.Active != id {
		a.lay.Active = id
		a.shown()
	}
}

// showTerminal shows a terminal's pane, focused; a terminal without one,
// as an agent another started, gets a tab of its own in the workspace
// shown.
func (a *app) showTerminal(address string) {
	if _, tab, _ := a.lay.Find(address); tab != nil {
		a.lay.Active, tab.Focus = tab.ID, address
	} else if ws, _ := a.lay.ActiveTab(); ws != nil {
		a.lay.AddTab(ws, address)
	} else {
		a.lay.AddWorkspace(address)
	}
	a.shown()
	// The sidebar keeps the agent chosen, rather than its tab.
	a.chosen = address
}

// attacher attaches the panes' terminals with the connection, nil while
// there is none.
func (a *app) attacher() term.Attacher {
	if a.conn == nil {
		return nil
	}
	return term.Core{Conn: a.conn}
}

// stopServer stops the core, once the user confirmed; the app connects
// again, starting it, once the user asks.
func (a *app) stopServer() {
	conn := a.conn
	a.serverErr = ""
	if conn == nil {
		return
	}
	a.gate.stop()
	go func() {
		if err := conn.StopServer(); err != nil && !errors.Is(err, hc.ErrLost) {
			a.update(func() { a.serverErr = err.Error() })
		}
	}()
}

// startServer starts the core again after the user stopped it.
func (a *app) startServer() {
	a.starting, a.serverErr = true, ""
	a.gate.start()
}

// clientEnv returns the environment `hc connect` runs in, which the core
// it starts gives its programs: the user's login shell's, as an app opened
// from the Finder has the system's.
func clientEnv() []string {
	env := os.Environ()
	if os.Getppid() == 1 {
		if login, err := hc.LoginEnv(); err == nil {
			env = mergeEnv(login, env)
		}
	}
	return hc.ClientEnv(env)
}

// mergeEnv returns the variables of first, then those of second it lacks.
func mergeEnv(first, second []string) []string {
	out := slices.Clone(first)
	have := map[string]bool{}
	for _, kv := range first {
		have[name(kv)] = true
	}
	for _, kv := range second {
		if !have[name(kv)] {
			out = append(out, kv)
		}
	}
	return out
}

func name(kv string) string {
	for i := range len(kv) {
		if kv[i] == '=' {
			return kv[:i]
		}
	}
	return kv
}
