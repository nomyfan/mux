package main

import (
	"os"
	"path/filepath"
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

	// The layout is in the core, for the next launch.
	pump("layout saved", func() bool { return !a.saving && !a.dirty && a.layVersion > 0 })
	doc, err := a.conn.GetDoc(layout.DocName)
	if err != nil || !strings.Contains(string(doc.Value), second) {
		t.Fatalf("the core keeps %s, %v", doc.Value, err)
	}

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
}
