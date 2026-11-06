package main

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"mux/internal/hc"
	"mux/internal/layout"
)

func mkdirWrite(path, data string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(data), 0o644)
}

// The app against a core of the test's own, as a user would use it: it
// makes a workspace in an empty core, shows its shell, takes what is
// typed, splits and closes panes, keeps the layout in the core, and drops
// the pane of a shell that exits. It runs only with MUX_HC_TEST=1, as it
// runs herdr-core, which must be installed.
func TestWithCore(t *testing.T) {
	if os.Getenv("MUX_HC_TEST") != "1" {
		t.Skip("set MUX_HC_TEST=1 to test against herdr-core")
	}
	if _, err := hc.Find(); err != nil {
		t.Skip(err)
	}
	// Socket paths are short: macOS takes 104 bytes.
	run, err := os.MkdirTemp("/tmp", "mux")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(run)
	dir := t.TempDir()
	posted := make(chan func(), 1024)
	a := newApp(layout.Layout{})
	a.env = append(hc.ClientEnv(os.Environ()), "HC_SESSION=muxapp", "XDG_RUNTIME_DIR="+run,
		"XDG_STATE_HOME="+dir+"/state", "XDG_CONFIG_HOME="+dir+"/config", "SHELL=/bin/sh", "PS1=$ ")
	a.post = func(fn func()) { posted <- fn }
	tt := ui.NewTester(a.view, 1000, 600)
	go a.connect()
	defer func() {
		// Stopped, the app connects no more.
		if conn := a.conn; conn != nil {
			a.gate.stop()
			conn.StopServer()
		}
	}()
	pump := func(what string, cond func() bool) {
		t.Helper()
		deadline := time.After(15 * time.Second)
		for !cond() {
			select {
			case fn := <-posted:
				fn()
			case <-time.After(20 * time.Millisecond):
			case <-deadline:
				t.Fatalf("no %s: err %q, layout %+v, texts %q", what, a.err, a.lay, tt.Texts())
			}
			tt.Frame()
		}
	}
	text := func(terminal string) string {
		if term := a.term.Terminal(terminal); term != nil {
			return term.Text()
		}
		return ""
	}

	pump("workspace", func() bool { return a.conn != nil && len(a.lay.Workspaces) == 1 })
	first := a.focusedTerminal()
	// In the user's home, not where the core started (here, the package).
	if home, _ := os.UserHomeDir(); a.cwdOf(first) != home {
		t.Errorf("the first terminal starts in %q, want %q", a.cwdOf(first), home)
	}
	pump("shell prompt", func() bool { return strings.Contains(text(first), "$") && tt.Focused("Terminal") })
	tt.Type("echo mux-$((6*7))")
	tt.Key(0, ui.KeyEnter)
	pump("output", func() bool { return strings.Contains(text(first), "mux-42") })

	tt.Key(ui.Cmd, ui.KeyD)
	pump("split", func() bool {
		_, tb := a.lay.ActiveTab()
		return len(tb.Leaves()) == 2 && tb.Focus != first
	})
	second := a.focusedTerminal()
	pump("second shell", func() bool { return strings.Contains(text(second), "$") && a.term.Focused() == second })
	tt.Type("echo second")
	tt.Key(0, ui.KeyEnter)
	pump("output in the second pane", func() bool { return strings.Contains(text(second), "second\n") })
	if strings.Contains(text(first), "second") {
		t.Error("the first pane took what was typed in the second")
	}
	// An agent done in the pane that has the keyboard shows idle, as the
	// user sees it.
	tt.Type(`printf '\033]7501;state=done:app=codex\033\\'`)
	tt.Key(0, ui.KeyEnter)
	pump("idle agent", func() bool { return a.state(second) == "idle" })

	// The layout is in the core, for the next launch.
	pump("layout saved", func() bool { return !a.saving && !a.dirty && a.layVersion > 0 })
	doc, err := a.conn.GetDoc(layout.DocName)
	if err != nil || !strings.Contains(string(doc.Value), second) {
		t.Fatalf("the core keeps %s, %v", doc.Value, err)
	}

	// The panes' terminals carry where they are, for their programs, and
	// the names the user gives, which go as the user clears them.
	labels := func(terminal string) map[string]string {
		tm, _ := a.terms.Find(terminal)
		return tm.Labels
	}
	ws, tab := a.lay.ActiveTab()
	pump("labels", func() bool {
		for _, term := range []string{first, second} {
			l := labels(term)
			if l[layout.LabelWorkspace] != ws.ID || l[layout.LabelTab] != tab.ID {
				return false
			}
		}
		return true
	})
	if _, ok := labels(first)[layout.LabelPaneName]; ok {
		t.Errorf("a pane without a name has the label: %v", labels(first))
	}
	tab.Pane(second).Name, tab.Name = "tests", "work"
	a.changed()
	pump("name labels", func() bool {
		l := labels(second)
		return l[layout.LabelPaneName] == "tests" && l[layout.LabelTabName] == "work" && labels(first)[layout.LabelTabName] == "work"
	})
	tab.Pane(second).Name = ""
	a.changed()
	pump("name label gone", func() bool {
		_, ok := labels(second)[layout.LabelPaneName]
		return !ok
	})

	// Stopped from the settings, which have the keyboard, and started in
	// place of the panes, the core comes back with its terminals, and the
	// focused pane takes the keyboard again.
	tt.Key(ui.Cmd, ui.KeyComma)
	pump("settings", func() bool { return tt.HasText("Stop Server…") && a.term.Focused() == "" })
	a.stopServer()
	tt.Key(0, ui.KeyEscape)
	pump("stopped", func() bool { return tt.HasText("herdr-core is stopped") && !a.settingsOpen })
	if err := tt.Click("Start Server"); err != nil {
		t.Fatal(err)
	}
	pump("restarted shell", func() bool {
		return a.conn != nil && strings.Contains(text(second), "$") && a.term.Focused() == second
	})
	tt.Type("echo again")
	tt.Key(0, ui.KeyEnter)
	pump("output after the restart", func() bool { return strings.Contains(text(second), "again\n") })

	// Closing a pane ends its terminal.
	tt.Key(ui.Cmd|ui.Shift, ui.KeyW)
	pump("pane closed", func() bool {
		t, _ := a.terms.Find(second)
		_, tb := a.lay.ActiveTab()
		return len(tb.Leaves()) == 1 && !t.Live()
	})

	// A shell that exits takes its pane, its tab and its workspace along.
	tt.Type("exit")
	tt.Key(0, ui.KeyEnter)
	pump("empty layout", func() bool { return len(a.lay.Workspaces) == 0 && tt.HasText("No tabs") })
	// And its record goes, as the core keeps finished terminals until
	// removed.
	pump("records removed", func() bool {
		_, ok1 := a.terms.Find(first)
		_, ok2 := a.terms.Find(second)
		return !ok1 && !ok2
	})
}

// The app with a machine its core reaches through a command that fails
// while a file says the line is down, which runs a core of the test's own
// in another session: a pane there stays while the machine is away, even
// across a restart of the app, its header saying so, and attaches again
// as the machine is back. It runs only with MUX_HC_TEST=1.
func TestMachineWithCore(t *testing.T) {
	if os.Getenv("MUX_HC_TEST") != "1" {
		t.Skip("set MUX_HC_TEST=1 to test against herdr-core")
	}
	bin, err := hc.Find()
	if err != nil {
		t.Skip(err)
	}
	run, err := os.MkdirTemp("/tmp", "mux")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(run)
	dir := t.TempDir()
	env := append(hc.ClientEnv(os.Environ()), "HC_SESSION=muxapp", "XDG_RUNTIME_DIR="+run,
		"XDG_STATE_HOME="+dir+"/state", "XDG_CONFIG_HOME="+dir+"/config", "SHELL=/bin/sh", "PS1=$ ")
	via := dir + "/via.sh"
	script := "#!/bin/sh\n[ -e " + dir + "/down ] && exit 255\necho $$ > " + dir + "/pid\nexec sh -c \"$@\"\n"
	if err := os.WriteFile(via, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	defer func() {
		// The machine's core, which outlives the line.
		stop := exec.Command(bin, "server", "stop")
		stop.Env = append(env, "HC_SESSION=muxremote")
		stop.Run()
	}()

	type running struct {
		*app
		tt     *ui.Tester
		posted chan func()
	}
	start := func() *running {
		r := &running{app: newApp(layout.Layout{}), posted: make(chan func(), 1024)}
		r.env, r.post = env, func(fn func()) { r.posted <- fn }
		r.tt = ui.NewTester(r.view, 1000, 600)
		go r.connect()
		return r
	}
	pump := func(r *running, what string, cond func() bool) {
		t.Helper()
		deadline := time.After(20 * time.Second)
		for !cond() {
			select {
			case fn := <-r.posted:
				fn()
			case <-time.After(20 * time.Millisecond):
			case <-deadline:
				t.Fatalf("no %s: err %q, panes %v, texts %q", what, r.err, r.lay.Terminals(), r.tt.Texts())
			}
			r.tt.Frame()
		}
	}
	away := func(conn *hc.Conn) bool {
		ms, _ := conn.Machines()
		return len(ms) == 1 && ms[0].State == "unreachable"
	}
	hasPane := func(r *running, terminal string) bool {
		return slices.Contains(r.lay.Terminals(), terminal)
	}

	a := start()
	pump(a, "workspace", func() bool { return a.conn != nil && len(a.lay.Workspaces) == 1 })
	if err := a.conn.Call("machine.add", map[string]any{"via": via, "name": "fake", "session": "muxremote"}, nil); err != nil {
		t.Fatal(err)
	}
	pump(a, "machine", func() bool {
		m, ok := a.machine("fake")
		return ok && m.State == "connected"
	})
	a.newWorkspaceOn("fake")
	remote := ""
	pump(a, "remote pane saved", func() bool {
		for _, term := range a.lay.Terminals() {
			if hc.MachineOf(term) == "fake" {
				remote = term
			}
		}
		return remote != "" && !a.saving && !a.dirty
	})

	// The line drops, and the app starts again meanwhile.
	if err := os.WriteFile(dir+"/down", nil, 0o644); err != nil {
		t.Fatal(err)
	}
	pid, _ := os.ReadFile(dir + "/pid")
	exec.Command("kill", strings.TrimSpace(string(pid))).Run()
	pump(a, "machine away", func() bool { return away(a.conn) })
	a.gate.stop()
	a.conn.Close()
	b := start()
	pump(b, "connection", func() bool { return b.conn != nil })
	b.showTerminal(remote)
	pump(b, "header saying the machine is away", func() bool { return b.tt.HasText("unreachable") })
	if !hasPane(b, remote) {
		t.Fatalf("the remote pane went: %v", b.lay.Terminals())
	}
	b.newTabHere()
	pump(b, "error starting a terminal there", func() bool { return b.err != "" })
	b.openSettings()
	pump(b, "machine in the settings", func() bool { return b.tt.HasText("fake") && b.tt.HasText("Connect") })
	b.settingsOpen = false

	// Back, as the user connects to it, the pane attaches again, and the
	// error goes.
	os.Remove(dir + "/down")
	b.dial("fake")
	pump(b, "machine back", func() bool {
		tm, ok := b.terms.Find(remote)
		return ok && tm.Live() && b.err == "" && !b.tt.HasText("unreachable")
	})
	if !hasPane(b, remote) {
		t.Fatalf("the remote pane went: %v", b.lay.Terminals())
	}
	pump(b, "keyboard", func() bool { return b.tt.Focused("Terminal") })
	b.tt.Type("echo back-$((40+2))")
	b.tt.Key(0, ui.KeyEnter)
	pump(b, "output there", func() bool {
		term := b.term.Terminal(remote)
		return term != nil && strings.Contains(term.Text(), "back-42")
	})
	b.gate.stop()
	b.conn.StopServer()
}

// A pane's labels go one change at a time, and the app looks at them again
// as the core's events come, however they come together: a name the core
// keeps other than the pane's, as another's change later than the app's,
// is set again.
func TestLabelsConverge(t *testing.T) {
	toCore, fromApp := io.Pipe()
	fromCore, toApp := io.Pipe()
	core := bufio.NewReader(toCore)
	go func() {
		core.ReadBytes('\n')
		io.WriteString(toApp, `{"jsonrpc":"2.0","id":0,"result":{"generation":1,"methods":["terminal.label"]}}`+"\n")
	}()
	conn, err := hc.Open(fromCore, fromApp, func() error { toApp.Close(); return fromApp.Close() })
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// request reads the next call the app makes, replying to it.
	request := func() string {
		t.Helper()
		var h [9]byte
		if _, err := io.ReadFull(core, h[:]); err != nil {
			t.Fatal(err)
		}
		p := make([]byte, binary.LittleEndian.Uint32(h[5:]))
		io.ReadFull(core, p)
		var req struct {
			ID     uint64
			Params struct{ Labels map[string]string }
		}
		json.Unmarshal(p, &req)
		reply := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":{}}`, req.ID)
		f := make([]byte, 9+len(reply))
		binary.LittleEndian.PutUint32(f[5:], uint32(len(reply)))
		copy(f[9:], reply)
		toApp.Write(f)
		return req.Params.Labels[layout.LabelPaneName]
	}

	ws := &layout.Workspace{ID: "w1", Tabs: []*layout.Tab{tab("t", "t1")}}
	l := layout.Layout{Workspaces: []*layout.Workspace{ws}, Active: "w1"}
	all := l.Labels()["t1"]
	t1 := shell("t1", "/tmp")
	t1.Labels = maps.Clone(all)
	a := newApp(l, t1)
	posted := make(chan func(), 16)
	a.conn, a.post = conn, func(fn func()) { posted <- fn }
	pane := ws.Tabs[0].Pane("t1")
	run := func() {
		t.Helper()
		select {
		case fn := <-posted:
			fn()
		case <-time.After(5 * time.Second):
			t.Fatal("no answer")
		}
	}
	// named returns the labels event of t1 named name.
	seq := uint64(0)
	named := func(name string) hc.Event {
		labels := maps.Clone(all)
		labels[layout.LabelPaneName] = name
		data, _ := json.Marshal(map[string]any{"labels": labels})
		seq++
		return hc.Event{Method: "event", Seq: seq, Type: "labels", Terminal: "t1", Data: data}
	}

	pane.Name = "b"
	a.label()
	// Renamed again while the first change goes, it waits for its answer.
	pane.Name = "c"
	a.label()
	if got := request(); got != "b" {
		t.Fatalf("sent %q first, not b", got)
	}
	run()
	if got := request(); got != "c" {
		t.Fatalf("sent %q next, not c", got)
	}
	run()

	// Its events come together with another's, later: the core keeps b.
	a.inbox = []hc.Event{named("b"), named("c"), named("b")}
	a.receive()
	if got := request(); got != "c" {
		t.Fatalf("sent %q again, not c", got)
	}
	run()
	a.inbox = []hc.Event{named("c")}
	a.receive()
	if len(a.labeling) != 0 {
		t.Errorf("still labeling %v", a.labeling)
	}
}
