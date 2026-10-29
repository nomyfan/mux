package hc

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"sync"
)

// Stream is a channel of the connection: a terminal's bytes, from
// terminal.attach, or the event stream, from events.subscribe. Read takes
// what the core sent, crediting it for each frame read whole.
type Stream struct {
	c       *Conn
	channel uint32

	mu     sync.Mutex
	more   *sync.Cond
	frames [][]byte
	err    error // why it ended, once it did
	reason json.RawMessage
}

// streamCredit is the credit a stream starts with beyond the core's own:
// enough for a full screen's repaint of a large window without a pause.
const streamCredit = 1 << 20

func newStream(c *Conn, channel uint32) *Stream {
	s := &Stream{c: c, channel: channel}
	s.more = sync.NewCond(&s.mu)
	return s
}

// push takes a frame the core sent on the stream's channel.
func (s *Stream) push(f frame) {
	switch f.kind {
	case kindData:
		if len(f.payload) == 0 {
			return
		}
		s.mu.Lock()
		s.frames = append(s.frames, f.payload)
		s.mu.Unlock()
		s.more.Broadcast()
	case kindClose:
		s.mu.Lock()
		s.reason = f.payload
		s.mu.Unlock()
		s.end(io.EOF)
		s.c.forget(s.channel)
	}
}

// end ends the stream with err, once what came before is read.
func (s *Stream) end(err error) {
	s.mu.Lock()
	if s.err == nil {
		s.err = err
	}
	s.mu.Unlock()
	s.more.Broadcast()
}

// Read reads what the core sent: io.EOF once the core closed the stream,
// as its program ended (see Reason), ErrLost once the connection is gone.
func (s *Stream) Read(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for len(s.frames) == 0 && s.err == nil {
		s.more.Wait()
	}
	if len(s.frames) == 0 {
		return 0, s.err
	}
	n := copy(p, s.frames[0])
	if n < len(s.frames[0]) {
		s.frames[0] = s.frames[0][n:]
	} else {
		s.frames = s.frames[1:]
	}
	if s.err == nil {
		s.c.send(s.channel, kindCredit, binary.LittleEndian.AppendUint32(nil, uint32(n)))
	}
	return n, nil
}

// Write sends p as typed: keys, already encoded as the program asked.
func (s *Stream) Write(p []byte) (int, error) {
	if err := s.ended(); err != nil {
		return 0, err
	}
	for rest := p; len(rest) > 0; {
		n := min(len(rest), maxPayload)
		s.c.send(s.channel, kindData, rest[:n])
		rest = rest[n:]
	}
	return len(p), nil
}

// ended returns why the stream ended, nil while it goes on.
func (s *Stream) ended() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if errors.Is(s.err, errDetached) {
		return io.ErrClosedPipe
	}
	return s.err
}

var errDetached = errors.New("detached")

// Close detaches: the core ends the stream, and the program goes on.
func (s *Stream) Close() error {
	if s.ended() == nil {
		s.c.send(s.channel, kindClose, nil)
	}
	s.end(errDetached)
	s.c.forget(s.channel)
	return nil
}

// Reason returns why the core closed the stream, as it said:
// {"reason":"exited","exit_code":0,"signal":null}, nil if it didn't.
func (s *Stream) Reason() json.RawMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reason
}

// Resize tells the core the viewer's size in cells.
func (s *Stream) Resize(cols, rows int) {
	if s.ended() != nil {
		return
	}
	p := binary.LittleEndian.AppendUint16(nil, uint16(min(max(cols, 1), 0xffff)))
	p = binary.LittleEndian.AppendUint16(p, uint16(min(max(rows, 1), 0xffff)))
	s.c.send(s.channel, kindResize, p)
}

// Focus tells the core the viewer gained the focus, or lost it.
func (s *Stream) Focus(focused bool) {
	if s.ended() != nil {
		return
	}
	b := byte(0)
	if focused {
		b = 1
	}
	s.c.send(s.channel, kindFocus, []byte{b})
}

// Display is what the viewer's terminal looks like, for the core to answer
// programs' questions of it: their colors, and the size of a cell in
// pixels.
type Display struct {
	Foreground string     `json:"fg,omitempty"` // #rrggbb
	Background string     `json:"bg,omitempty"`
	Cursor     string     `json:"cursor,omitempty"`
	Cell       *[2]uint32 `json:"cell,omitempty"` // width, height
}

// Facts tells the core what the viewer's terminal looks like now.
func (s *Stream) Facts(d Display) {
	if s.ended() != nil {
		return
	}
	p, _ := json.Marshal(d)
	s.c.send(s.channel, kindFacts, p)
}
