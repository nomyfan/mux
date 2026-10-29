package hc

import (
	"encoding/json"
	"fmt"
)

// Terminal is the core's record of a terminal (the schema's Terminal),
// with the fields the client reads.
type Terminal struct {
	// Address is the terminal's, forever on this core: t7, or rohan/t7
	// on another machine the core added.
	Address string            `json:"address"`
	Name    string            `json:"name,omitempty"`
	Argv    []string          `json:"argv,omitempty"`
	Cwd     string            `json:"cwd,omitempty"` // where it started
	State   string            `json:"state"`         // live or finished
	Reason  string            `json:"reason,omitempty"`
	Title   string            `json:"title,omitempty"`
	Labels  map[string]string `json:"labels,omitempty"`
	Parent  string            `json:"parent,omitempty"`
	Keep    bool              `json:"keep,omitempty"`
	Cols    int               `json:"cols,omitempty"`
	Rows    int               `json:"rows,omitempty"`
	// Foreground is what runs in it now and where, nil once it finished.
	Foreground *Foreground `json:"foreground,omitempty"`
	// Status is what its program last said it does (OSC 7501), nil if it
	// never said, or cleared it.
	Status   *Status `json:"status,omitempty"`
	Reported bool    `json:"reported,omitempty"`
}

// Live reports whether the terminal's program runs, or a kept terminal
// waits for its new shell.
func (t Terminal) Live() bool { return t.State == "live" }

// Foreground is the foreground process group's leader of a terminal, and
// its working directory.
type Foreground struct {
	PID  int      `json:"pid"`
	Name string   `json:"name"`
	Argv []string `json:"argv"`
	Cwd  string   `json:"cwd"`
}

// Status is a program's report of what it does (OSC 7501), in its own
// words: State is idle, working, done, blocked or error; App names the
// program, as claude-code or codex.
type Status struct {
	State    string `json:"state"`
	Kind     string `json:"kind,omitempty"`
	Progress *int   `json:"progress,omitempty"`
	App      string `json:"app,omitempty"`
	Title    string `json:"title,omitempty"`
	Msg      string `json:"msg,omitempty"`
}

// RunOptions are terminal.run's params that the client sets.
type RunOptions struct {
	// Argv is the program and its arguments; none runs the machine's own
	// shell.
	Argv   []string          `json:"argv,omitempty"`
	Cwd    string            `json:"cwd,omitempty"`
	Env    map[string]string `json:"env,omitempty"`
	Labels map[string]string `json:"labels,omitempty"`
	// Keep replaces a lost program with a shell in place, as after the
	// machine restarts.
	Keep bool `json:"keep,omitempty"`
	// NoParent starts it with no parent, though the client runs in a
	// terminal of the core.
	NoParent bool `json:"no_parent,omitempty"`
}

// Run starts a program in a new terminal, returning its record.
func (c *Conn) Run(o RunOptions) (Terminal, error) {
	var t Terminal
	err := c.Call("terminal.run", o, &t)
	return t, err
}

// Kill ends a terminal's program.
func (c *Conn) Kill(address string) error {
	return c.Call("terminal.kill", map[string]string{"terminal": address}, nil)
}

// Remove removes a finished terminal.
func (c *Conn) Remove(address string) error {
	return c.Call("terminal.rm", map[string]string{"terminal": address}, nil)
}

// AttachOptions are terminal.attach's params.
type AttachOptions struct {
	Terminal string `json:"terminal"`
	Cols     int    `json:"cols"`
	Rows     int    `json:"rows"`
	// History is how many lines of scrollback come first.
	History int      `json:"history,omitempty"`
	Display *Display `json:"display,omitempty"`
	// Resumed is for a viewer back after a dropped line: it sets the size
	// only once active again.
	Resumed bool `json:"resumed,omitempty"`
}

// Attach attaches to a terminal: the stream brings its first paint, then
// its program's output, and takes what is typed.
func (c *Conn) Attach(o AttachOptions) (*Stream, Terminal, error) {
	var r struct {
		Channel  *uint32  `json:"channel"`
		Terminal Terminal `json:"terminal"`
	}
	if err := c.Call("terminal.attach", o, &r); err != nil {
		return nil, Terminal{}, err
	}
	if r.Channel == nil {
		return nil, Terminal{}, fmt.Errorf("terminal.attach: no channel")
	}
	return c.stream(*r.Channel, 0), r.Terminal, nil
}

// Doc is an opaque JSON document the core keeps, and its version: zero
// for one never written.
type Doc struct {
	Value   json.RawMessage `json:"value"`
	Version uint64          `json:"version"`
}

// GetDoc reads the document name.
func (c *Conn) GetDoc(name string) (Doc, error) {
	var d Doc
	err := c.Call("doc.get", map[string]string{"name": name}, &d)
	return d, err
}

// SetDoc writes the document name if its version is still version,
// returning the new one; another writer's version since fails with kind
// conflict.
func (c *Conn) SetDoc(name string, value any, version uint64) (uint64, error) {
	var r struct {
		Version uint64 `json:"version"`
	}
	err := c.Call("doc.set", map[string]any{"name": name, "value": value, "version": version}, &r)
	return r.Version, err
}

// StopServer stops the core and its programs; kept terminals get new
// shells as it starts again.
func (c *Conn) StopServer() error { return c.Call("server.stop", nil, nil) }
