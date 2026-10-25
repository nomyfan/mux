// Package endpoint is a client of herdr's client endpoint: the protocol of
// the herdr TUI, generation 1, which herdr keeps stable across versions.
// The server lays out the client's focused tab and sends it as a grid of
// cells, then patches it as panes change; the client sends input to panes
// as keys and text, which the server encodes for each program.
package endpoint

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
)

const (
	helloKind   = "endpoint.hello.v1"
	welcomeKind = "endpoint.welcome.v1"
	// maxFrame bounds a message the server sends, as herdr's own clients do.
	maxFrame = 32 << 20
)

// DefaultSocketPath resolves the endpoint's socket as herdr's clients do:
// beside HERDR_SOCKET_PATH, else the session HERDR_SESSION names, else the
// default session's.
func DefaultSocketPath() (string, error) {
	if p := os.Getenv("HERDR_SOCKET_PATH"); p != "" {
		stem := filepath.Base(p)
		stem = stem[:len(stem)-len(filepath.Ext(stem))]
		return filepath.Join(filepath.Dir(p), stem+"-client.sock"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".config", "herdr")
	if s := os.Getenv("HERDR_SESSION"); s != "" && s != "default" {
		dir = filepath.Join(dir, "sessions", s)
	}
	return filepath.Join(dir, "herdr-client.sock"), nil
}

// Size is the client's surface: its grid in cells, and a cell in pixels,
// which programs asking the size of the window in pixels are told.
type Size struct {
	Cols, Rows            uint16
	CellWidth, CellHeight uint32
}

type hello struct {
	Generation         uint32   `json:"generation"`
	CellWidthPx        uint32   `json:"cell_width_px"`
	CellHeightPx       uint32   `json:"cell_height_px"`
	SurfaceSize        jsonSize `json:"surface_size"`
	PixelMouse         bool     `json:"pixel_mouse"`
	DirectGraphics     bool     `json:"direct_graphics"`
	EndpointKeybinding bool     `json:"endpoint_keybindings"`
	MouseCapture       bool     `json:"mouse_capture"`
	SurfaceActive      bool     `json:"surface_active"`
	SnapshotCodecs     []string `json:"snapshot_codecs"`
	SurfaceCodecs      []string `json:"surface_codecs"`
	InputCodecs        []string `json:"input_codecs"`
	BlobCodecs         []string `json:"blob_codecs"`
}

type jsonSize struct {
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
}

// Welcome is the server's answer to the client's hello.
type Welcome struct {
	Generation    uint32   `json:"generation"`
	ServerVersion string   `json:"server_version"`
	Methods       []string `json:"methods"`
	Capabilities  []string `json:"capabilities"`
	Error         *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// Conn is a connection to the endpoint. Next reads from one goroutine; the
// sending methods may be called from any.
type Conn struct {
	Welcome Welcome

	conn   net.Conn
	reader *bufio.Reader
	wmu    sync.Mutex
	nextID uint64
}

// Dial connects to the endpoint at path and says hello, asking for a
// surface of size, and returns once the server welcomes the client.
func Dial(path string, size Size) (*Conn, error) {
	nc, err := net.Dial("unix", path)
	if err != nil {
		return nil, err
	}
	c := &Conn{conn: nc, reader: bufio.NewReaderSize(nc, 64<<10)}
	data, err := json.Marshal(hello{
		Generation:     1,
		CellWidthPx:    size.CellWidth,
		CellHeightPx:   size.CellHeight,
		SurfaceSize:    jsonSize{size.Cols, size.Rows},
		SurfaceActive:  true,
		SnapshotCodecs: []string{"shell.snapshot.v1"},
		SurfaceCodecs:  []string{"shell.surface.v1"},
		InputCodecs:    []string{"shell.input.semantic.v1"},
		BlobCodecs:     []string{"shell.blob.v1"},
	})
	if err == nil {
		err = c.control(helloKind, string(data))
	}
	for err == nil {
		var msg any
		if msg, err = c.Next(); err != nil {
			break
		}
		if ctl, ok := msg.(Control); ok && ctl.Kind == welcomeKind {
			if err = json.Unmarshal([]byte(ctl.Data), &c.Welcome); err == nil && c.Welcome.Error != nil {
				err = fmt.Errorf("endpoint: %s: %s", c.Welcome.Error.Code, c.Welcome.Error.Message)
			}
			if err == nil {
				return c, nil
			}
		}
	}
	nc.Close()
	return nil, err
}

// Next waits for the server's next message: a *Snapshot, *Surface,
// *SurfacePatch, Control, Clipboard, ShellError, ResponseChunk, Shutdown,
// or Unknown for the others.
func (c *Conn) Next() (any, error) {
	var prefix [4]byte
	if _, err := io.ReadFull(c.reader, prefix[:]); err != nil {
		return nil, err
	}
	n := binary.LittleEndian.Uint32(prefix[:])
	if n == 0 || n > maxFrame {
		return nil, errors.New("endpoint: message of a bad size")
	}
	payload := make([]byte, n)
	if _, err := io.ReadFull(c.reader, payload); err != nil {
		return nil, err
	}
	return decodeServerMessage(payload)
}

func (c *Conn) Close() error { return c.conn.Close() }

func (c *Conn) send(e *encoder) error {
	frame := binary.LittleEndian.AppendUint32(make([]byte, 0, 4+len(e.buf)), uint32(len(e.buf)))
	frame = append(frame, e.buf...)
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_, err := c.conn.Write(frame)
	return err
}

func (c *Conn) control(kind, data string) error {
	e := &encoder{}
	e.variant(clientEndpointControl)
	e.string(kind)
	e.string(data)
	return c.send(e)
}

// Resize asks for a surface of a new size.
func (c *Conn) Resize(size Size) error {
	e := &encoder{}
	e.variant(clientShellResize)
	e.uint(uint64(size.CellWidth))
	e.uint(uint64(size.CellHeight))
	e.uint(uint64(size.Cols))
	e.uint(uint64(size.Rows))
	e.bool(false) // pixel_mouse
	return c.send(e)
}

// Input sends events to a pane, in order.
func (c *Conn) Input(paneID string, events ...InputEvent) error {
	e := &encoder{}
	e.variant(clientShellPaneInput)
	e.string(paneID)
	e.uint(uint64(len(events)))
	for _, ev := range events {
		ev.encode(e)
	}
	return c.send(e)
}

type RGB struct{ R, G, B uint8 }

// HostTheme is the colors the client paints the default colors and the
// palette in. The server resolves some colors itself, as a reversed
// default, and answers programs asking for colors or whether they are dark.
type HostTheme struct {
	Foreground, Background RGB
	Palette                [256]RGB
	Dark                   bool
}

// SetHostTheme tells the server the client's colors.
func (c *Conn) SetHostTheme(t HostTheme) error {
	update := func(fill func(e *encoder)) error {
		e := &encoder{}
		e.variant(clientShellHostTheme)
		fill(e)
		return c.send(e)
	}
	rgb := func(e *encoder, c RGB) { e.u8(c.R); e.u8(c.G); e.u8(c.B) }
	appearance := uint32(1) // light
	if t.Dark {
		appearance = 0
	}
	return errors.Join(
		update(func(e *encoder) { e.variant(0); e.variant(0); rgb(e, t.Foreground) }),
		update(func(e *encoder) { e.variant(0); e.variant(1); rgb(e, t.Background) }),
		update(func(e *encoder) {
			e.variant(1)
			e.uint(uint64(len(t.Palette)))
			for i, c := range t.Palette {
				e.u8(uint8(i))
				rgb(e, c)
			}
		}),
		update(func(e *encoder) { e.variant(2); e.variant(appearance) }),
	)
}

// SetFocus tells the server whether the client's window has the focus,
// which programs asking for focus reports are told.
func (c *Conn) SetFocus(focused bool) error {
	e := &encoder{}
	e.variant(clientShellFocus)
	e.bool(focused)
	return c.send(e)
}

// Request sends a request of herdr's socket API for the client's own view,
// as pane.focus moving its focus, and returns its ID, which the
// ResponseChunks of its response carry.
func (c *Conn) Request(bootID, method string, params any) (string, error) {
	c.wmu.Lock()
	c.nextID++
	id := fmt.Sprintf("mux-%d", c.nextID)
	c.wmu.Unlock()
	req, err := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
	if err != nil {
		return "", err
	}
	e := &encoder{}
	e.variant(clientShellRequest)
	e.string(bootID)
	e.string(string(req))
	return id, c.send(e)
}

// Detach ends the client's attachment; the panes keep running.
func (c *Conn) Detach() error {
	e := &encoder{}
	e.variant(clientDetach)
	return c.send(e)
}
