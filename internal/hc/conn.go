// Package hc is a client of herdr-core, the terminal runtime whose command
// is hc: one framed connection, as `hc connect` carries it, with JSON-RPC on
// channel 0 and a terminal's bytes, or the event stream, on channels of
// their own (herdr-core's schema/README.md). No UI dependency.
package hc

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Generation is the protocol generation the client speaks.
const Generation = 1

// Frame kinds.
const (
	kindData   = 0
	kindCredit = 1
	kindClose  = 2
	kindResize = 3
	kindFocus  = 4
	kindFacts  = 5
)

// headerSize is a frame's header: its channel, kind and length.
const headerSize = 9

// maxPayload is the most a frame carries.
const maxPayload = 16 << 20

// ErrLost is the error of calls and streams once the connection to the
// core is gone. The core's programs most likely still run: connect again.
var ErrLost = errors.New("lost the connection to herdr-core")

// Error is an error the core answered a call with: Kind is stable (as
// not_found, conflict, unavailable), Hint says what to do.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		Kind string `json:"kind"`
		Hint string `json:"hint"`
	} `json:"data"`
}

func (e *Error) Error() string {
	if e.Data.Hint != "" {
		return e.Message + " (" + e.Data.Hint + ")"
	}
	return e.Message
}

// Kind returns the kind of the core's error err, if it is one.
func Kind(err error) string {
	if e, ok := errors.AsType[*Error](err); ok {
		return e.Data.Kind
	}
	return ""
}

// Hello is what the core says of itself as the connection starts.
type Hello struct {
	Generation int      `json:"generation"`
	Version    string   `json:"version"`
	Methods    []string `json:"methods"`
	Features   []string `json:"features"`
	Core       struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"core"`
}

// Uses are the methods the client calls: a core without one of them can't
// serve it.
var Uses = []string{"terminal.run", "terminal.attach", "terminal.kill", "terminal.rm", "events.subscribe", "doc.get", "doc.set", "server.stop"}

// Check reports why a core that said hello can't serve the client, if it
// can't.
func (h Hello) Check() error {
	if h.Generation != Generation {
		return fmt.Errorf("herdr-core %s speaks protocol generation %d; Mux speaks %d", h.Version, h.Generation, Generation)
	}
	var missing []string
	for _, m := range Uses {
		found := false
		for _, have := range h.Methods {
			found = found || have == m
		}
		if !found {
			missing = append(missing, m)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("herdr-core %s lacks %s", h.Version, strings.Join(missing, ", "))
	}
	return nil
}

// Conn is a framed connection to the core. Its methods are safe from any
// goroutine; calls run side by side.
type Conn struct {
	Hello Hello

	w         io.WriteCloser
	close     func() error
	closeOnce sync.Once

	// out holds the frames for the writer: a core that stops reading
	// never blocks a caller, as the UI.
	out  chan []byte
	lost chan struct{}

	mu      sync.Mutex
	closed  bool
	nextID  uint64
	replies map[uint64]chan reply
	streams map[uint32]*Stream
	// gone holds the channels of streams that ended, whose frames still
	// coming, as the core's answer to a detach, are dropped.
	gone map[uint32]bool
	// early holds the frames of a channel whose reply opening it the
	// caller hasn't taken yet.
	early map[uint32][]frame
}

type reply struct {
	result json.RawMessage
	err    error
}

type frame struct {
	kind    byte
	payload []byte
}

// Connect runs `bin connect`, which starts the core if none runs, and
// switches the connection to framed mode. env is the command's
// environment, nil for this process's.
func Connect(bin string, env []string) (*Conn, error) {
	cmd := exec.Command(bin, "connect")
	cmd.Env = env
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	c, err := Open(stdout, stdin, func() error {
		stdin.Close()
		cmd.Process.Kill()
		return cmd.Wait()
	})
	if err != nil {
		cmd.Process.Kill()
		cmd.Wait()
		return nil, err
	}
	return c, nil
}

// Open says hello on a connection in plain mode, which r reads and w
// writes, and switches it to framed mode. close, if any, ends it.
func Open(r io.Reader, w io.WriteCloser, close func() error) (*Conn, error) {
	br := bufio.NewReader(r)
	hello, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 0, "method": "hello",
		"params": map[string]any{"framed": true, "generation": Generation}})
	if _, err := w.Write(append(hello, '\n')); err != nil {
		return nil, err
	}
	line, err := br.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("herdr-core said no hello: %w", err)
	}
	var resp struct {
		Result Hello
		Error  *Error
	}
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil, fmt.Errorf("herdr-core said no hello: %w", err)
	}
	if resp.Error != nil {
		return nil, resp.Error
	}
	if close == nil {
		close = w.Close
	}
	c := &Conn{Hello: resp.Result, w: w, close: close, out: make(chan []byte, 256), lost: make(chan struct{}),
		replies: map[uint64]chan reply{}, streams: map[uint32]*Stream{}, gone: map[uint32]bool{}, early: map[uint32][]frame{}}
	go c.write()
	go c.read(br)
	return c, nil
}

// Lost is closed once the connection is gone.
func (c *Conn) Lost() <-chan struct{} { return c.lost }

// Close ends the connection.
func (c *Conn) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()
	return c.shut()
}

// shut ends the connection, once.
func (c *Conn) shut() (err error) {
	c.closeOnce.Do(func() { err = c.close() })
	return err
}

func encode(channel uint32, kind byte, payload []byte) []byte {
	b := make([]byte, headerSize+len(payload))
	binary.LittleEndian.PutUint32(b, channel)
	b[4] = kind
	binary.LittleEndian.PutUint32(b[5:], uint32(len(payload)))
	copy(b[headerSize:], payload)
	return b
}

// send queues a frame for the core, dropping it once the connection is
// gone.
func (c *Conn) send(channel uint32, kind byte, payload []byte) {
	select {
	case c.out <- encode(channel, kind, payload):
	case <-c.lost:
	}
}

func (c *Conn) write() {
	for {
		select {
		case b := <-c.out:
			if _, err := c.w.Write(b); err != nil {
				c.Close()
				return
			}
		case <-c.lost:
			return
		}
	}
}

func (c *Conn) read(r io.Reader) {
	defer c.drop()
	var h [headerSize]byte
	for {
		if _, err := io.ReadFull(r, h[:]); err != nil {
			return
		}
		channel, kind, n := binary.LittleEndian.Uint32(h[:]), h[4], binary.LittleEndian.Uint32(h[5:])
		if n > maxPayload {
			return
		}
		payload := make([]byte, n)
		if _, err := io.ReadFull(r, payload); err != nil {
			return
		}
		if channel == 0 {
			c.answer(payload)
			continue
		}
		c.mu.Lock()
		s := c.streams[channel]
		if s == nil && !c.gone[channel] {
			c.early[channel] = append(c.early[channel], frame{kind, payload})
		}
		c.mu.Unlock()
		if s != nil {
			s.push(frame{kind, payload})
		}
	}
}

// drop fails the calls waiting and ends the streams, as the connection is
// gone.
func (c *Conn) drop() {
	c.mu.Lock()
	c.closed = true
	replies, streams := c.replies, c.streams
	c.replies, c.streams = map[uint64]chan reply{}, map[uint32]*Stream{}
	close(c.lost)
	c.mu.Unlock()
	for _, r := range replies {
		r <- reply{err: ErrLost}
	}
	for _, s := range streams {
		s.end(ErrLost)
	}
	c.shut()
}

// answer hands a JSON-RPC message of channel 0 to the call waiting on it.
func (c *Conn) answer(payload []byte) {
	var msg struct {
		ID     *uint64
		Result json.RawMessage
		Error  *Error
	}
	if json.Unmarshal(payload, &msg) != nil || msg.ID == nil {
		return
	}
	c.mu.Lock()
	r := c.replies[*msg.ID]
	delete(c.replies, *msg.ID)
	c.mu.Unlock()
	if r == nil {
		return
	}
	if msg.Error != nil {
		r <- reply{err: msg.Error}
	} else {
		r <- reply{result: msg.Result}
	}
}

// callTimeout bounds how long a call waits for its answer.
const callTimeout = 30 * time.Second

// Call calls method with params and decodes its result into result, if
// any.
func (c *Conn) Call(method string, params, result any) error {
	raw, err := c.CallRaw(method, params)
	if err != nil {
		return err
	}
	if result == nil {
		return nil
	}
	if err := json.Unmarshal(raw, result); err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	return nil
}

// CallRaw calls method with params and returns its result as the core
// sent it.
func (c *Conn) CallRaw(method string, params any) (json.RawMessage, error) {
	if params == nil {
		params = struct{}{}
	}
	ch := make(chan reply, 1)
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, ErrLost
	}
	c.nextID++
	id := c.nextID
	c.replies[id] = ch
	c.mu.Unlock()
	req, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	if err != nil {
		c.mu.Lock()
		delete(c.replies, id)
		c.mu.Unlock()
		return nil, err
	}
	c.send(0, kindData, req)
	select {
	case r := <-ch:
		if r.err != nil {
			return nil, fmt.Errorf("%s: %w", method, r.err)
		}
		return r.result, nil
	case <-time.After(callTimeout):
		c.mu.Lock()
		delete(c.replies, id)
		c.mu.Unlock()
		return nil, fmt.Errorf("%s: no answer from herdr-core", method)
	}
}

// stream opens the stream of a channel a reply named, with the frames
// that came for it before.
func (c *Conn) stream(channel uint32, credit int) *Stream {
	s := newStream(c, channel)
	c.mu.Lock()
	early := c.early[channel]
	delete(c.early, channel)
	if c.closed {
		c.mu.Unlock()
		s.end(ErrLost)
		return s
	}
	c.streams[channel] = s
	c.mu.Unlock()
	for _, f := range early {
		s.push(f)
	}
	if credit > 0 {
		c.send(channel, kindCredit, binary.LittleEndian.AppendUint32(nil, uint32(credit)))
	}
	return s
}

// forget drops a stream that ended.
func (c *Conn) forget(channel uint32) {
	c.mu.Lock()
	delete(c.streams, channel)
	c.gone[channel] = true
	c.mu.Unlock()
}
