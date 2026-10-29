package term

import (
	"fmt"
	"io"
	"sync"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"

	"mux/internal/hc"
)

// Stream is an attached terminal's stream: an *hc.Stream.
type Stream interface {
	io.ReadWriteCloser
	Resize(cols, rows int)
	Focus(focused bool)
	Facts(hc.Display)
}

// Attacher attaches to the core's terminals.
type Attacher interface {
	Attach(hc.AttachOptions) (Stream, error)
}

// Core attaches to terminals with a connection to the core.
type Core struct{ Conn *hc.Conn }

func (c Core) Attach(o hc.AttachOptions) (Stream, error) {
	s, _, err := c.Conn.Attach(o)
	if err != nil {
		return nil, err
	}
	return s, nil
}

// history is how many lines of a terminal's scrollback come first as a
// pane attaches.
const history = 5000

// conn is a pane's terminal's Conn: the stream of the core's terminal,
// attached as the pane's view first tells its size, so that the first
// paint comes at that size. Until then reads wait, and what is typed
// waits too.
type conn struct {
	att      Attacher
	terminal string

	mu      sync.Mutex
	ready   *sync.Cond
	stream  Stream
	err     error // why it could not attach
	closed  bool
	pending [][]byte
	display hc.Display
}

func newConn(att Attacher, terminal string) *conn {
	c := &conn{att: att, terminal: terminal}
	c.ready = sync.NewCond(&c.mu)
	return c
}

// wait returns the stream once attached, nil once it can't be.
func (c *conn) wait() (Stream, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for c.stream == nil && c.err == nil && !c.closed {
		c.ready.Wait()
	}
	switch {
	case c.closed:
		return nil, io.EOF
	case c.err != nil:
		return nil, c.err
	}
	return c.stream, nil
}

func (c *conn) Read(p []byte) (int, error) {
	s, err := c.wait()
	if err != nil {
		return 0, err
	}
	return s.Read(p)
}

func (c *conn) Write(p []byte) (int, error) {
	c.mu.Lock()
	s := c.stream
	if s == nil {
		if c.closed || c.err != nil {
			c.mu.Unlock()
			return 0, io.ErrClosedPipe
		}
		c.pending = append(c.pending, append([]byte(nil), p...))
		c.mu.Unlock()
		return len(p), nil
	}
	c.mu.Unlock()
	return s.Write(p)
}

func (c *conn) Close() error {
	c.mu.Lock()
	c.closed = true
	s := c.stream
	c.mu.Unlock()
	c.ready.Broadcast()
	if s != nil {
		return s.Close()
	}
	return nil
}

// ResizePixels attaches at the size the view fits, the first time, and
// tells the core the size after.
func (c *conn) ResizePixels(cols, rows, cellW, cellH int) error {
	c.mu.Lock()
	cell := &[2]uint32{uint32(cellW), uint32(cellH)}
	changed := c.display.Cell == nil || *c.display.Cell != *cell
	c.display.Cell = cell
	display := c.display
	s, closed, failed := c.stream, c.closed, c.err != nil
	c.mu.Unlock()
	switch {
	case closed || failed:
		return nil
	case s != nil:
		s.Resize(cols, rows)
		if changed {
			s.Facts(display)
		}
		return nil
	}
	// The plugin tells the size from a goroutine of its own, which may
	// wait for the call.
	s, err := c.att.Attach(hc.AttachOptions{Terminal: c.terminal, Cols: cols, Rows: rows, History: history, Display: &display})
	c.mu.Lock()
	defer c.mu.Unlock()
	if err != nil {
		c.err = fmt.Errorf("attaching %s: %w", c.terminal, err)
		c.ready.Broadcast()
		return err
	}
	if c.closed {
		s.Close()
		return nil
	}
	c.stream = s
	for _, p := range c.pending {
		s.Write(p)
	}
	c.pending = nil
	c.ready.Broadcast()
	return nil
}

// UseTheme tells the core the view's colors, for the programs that ask.
func (c *conn) UseTheme(theme *terminal.Theme) error {
	if theme == nil {
		return nil
	}
	c.mu.Lock()
	c.display.Foreground, c.display.Background = hex(theme.Foreground), hex(theme.Background)
	c.display.Cursor = hex(theme.Cursor)
	if theme.Cursor.A == 0 {
		c.display.Cursor = c.display.Foreground
	}
	display, s := c.display, c.stream
	c.mu.Unlock()
	if s != nil {
		s.Facts(display)
	}
	return nil
}

// Focus tells the core the pane gained the focus, or lost it.
func (c *conn) Focus(focused bool) {
	c.mu.Lock()
	s := c.stream
	c.mu.Unlock()
	if s != nil {
		s.Focus(focused)
	}
}

// hex returns a color as #rrggbb.
func hex(col ui.Color) string {
	s := col.SRGB()
	return fmt.Sprintf("#%02x%02x%02x", s.R, s.G, s.B)
}
