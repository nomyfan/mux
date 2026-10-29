package hc

import (
	"os"
	"strings"
	"testing"
	"time"
)

// isolatedCore connects to a core of its own: a session and state of the
// test's, never the user's. It runs only with MUX_HC_TEST=1, as it runs
// herdr-core, which must be installed.
func isolatedCore(t *testing.T) *Conn {
	t.Helper()
	if os.Getenv("MUX_HC_TEST") != "1" {
		t.Skip("set MUX_HC_TEST=1 to test against herdr-core")
	}
	bin, err := Find()
	if err != nil {
		t.Skip(err)
	}
	// Socket paths are short: macOS takes 104 bytes.
	run, err := os.MkdirTemp("/tmp", "mux")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	env := append(ClientEnv(os.Environ()), "HC_SESSION=muxtest", "XDG_RUNTIME_DIR="+run,
		"XDG_STATE_HOME="+dir+"/state", "XDG_CONFIG_HOME="+dir+"/config", "HC_SOCKET=")
	c, err := Connect(bin, env)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c.StopServer()
		c.Close()
		os.RemoveAll(run)
	})
	if err := c.Hello.Check(); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestCore(t *testing.T) {
	c := isolatedCore(t)
	events, err := c.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	ts := Terminals{Seq: events.Seq, List: events.Terminals}
	term, err := c.Run(RunOptions{Argv: []string{"sh"}, Cwd: "/tmp", Labels: map[string]string{"mux": "test"}, NoParent: true})
	if err != nil {
		t.Fatal(err)
	}
	s, _, err := c.Attach(AttachOptions{Terminal: term.Address, Cols: 80, Rows: 24, Display: &Display{Cell: &[2]uint32{8, 16}}})
	if err != nil {
		t.Fatal(err)
	}
	s.Write([]byte("echo hi-$((1+2))\r"))
	out := make(chan string)
	go func() {
		var all strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := s.Read(buf)
			all.Write(buf[:n])
			if strings.Contains(all.String(), "hi-3") || err != nil {
				out <- all.String()
				return
			}
		}
	}()
	select {
	case got := <-out:
		if !strings.Contains(got, "hi-3") {
			t.Fatalf("output %q", got)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no output")
	}
	s.Resize(100, 30)
	if err := c.Kill(term.Address); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(10 * time.Second)
	sized := false
	for {
		got := make(chan Event, 1)
		go func() {
			ev, _ := events.Next()
			got <- ev
		}()
		select {
		case ev := <-got:
			ts.Apply(ev)
			sized = sized || ev.Type == "size"
		case <-deadline:
			t.Fatal("no finished event")
		}
		if r, ok := ts.Find(term.Address); ok && !r.Live() {
			break
		}
	}
	if !sized {
		t.Error("no size event after the resize")
	}

	// Documents are written by version.
	d, err := c.GetDoc("mux")
	if err != nil || d.Version != 0 {
		t.Fatalf("doc %+v %v", d, err)
	}
	v, err := c.SetDoc("mux", map[string]int{"a": 1}, 0)
	if err != nil || v == 0 {
		t.Fatalf("set %d %v", v, err)
	}
	if _, err := c.SetDoc("mux", map[string]int{"a": 2}, 0); Kind(err) != "conflict" {
		t.Errorf("stale write: %v", err)
	}
}
