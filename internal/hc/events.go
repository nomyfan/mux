package hc

import (
	"bufio"
	"encoding/json"
	"fmt"
	"slices"
)

// Event is a notification of the event stream: an event, a heartbeat, or
// missed, which brings a fresh snapshot of the terminals.
type Event struct {
	// Method is event, heartbeat or missed.
	Method string
	Seq    uint64
	// Type is an event's kind: created, lost, restarted, keep, finished,
	// removed, status, turn, title, labels, size, foreground or doc.
	Type     string
	Terminal string
	Data     json.RawMessage
	// Terminals is missed's snapshot.
	Terminals []Terminal
}

// DocChange is the data of a doc event: the document's name and version.
type DocChange struct {
	Name    string `json:"name"`
	Version uint64 `json:"version"`
}

// Events is the event stream of a subscription.
type Events struct {
	s     *Stream
	lines *bufio.Reader
	// Seq and Terminals are the snapshot the subscription began with:
	// events with Seq above it follow.
	Seq       uint64
	Terminals []Terminal
}

// Subscribe subscribes to every terminal's changes, and the documents'.
func (c *Conn) Subscribe() (*Events, error) {
	var r struct {
		Channel   *uint32    `json:"channel"`
		Seq       uint64     `json:"seq"`
		Terminals []Terminal `json:"terminals"`
	}
	if err := c.Call("events.subscribe", nil, &r); err != nil {
		return nil, err
	}
	if r.Channel == nil {
		return nil, fmt.Errorf("events.subscribe: no channel")
	}
	s := c.stream(*r.Channel, streamCredit)
	return &Events{s: s, lines: bufio.NewReaderSize(s, 64<<10), Seq: r.Seq, Terminals: r.Terminals}, nil
}

// Next returns the next notification, ErrLost once the connection is
// gone.
func (e *Events) Next() (Event, error) {
	for {
		line, err := e.lines.ReadBytes('\n')
		if err != nil {
			return Event{}, err
		}
		if ev, ok := parseEvent(line); ok {
			return ev, nil
		}
	}
}

// Close ends the subscription.
func (e *Events) Close() error { return e.s.Close() }

// parseEvent reads a line of the event stream.
func parseEvent(line []byte) (Event, bool) {
	var msg struct {
		Method string `json:"method"`
		Params struct {
			Seq       uint64          `json:"seq"`
			Type      string          `json:"type"`
			Terminal  string          `json:"terminal"`
			Data      json.RawMessage `json:"data"`
			Terminals []Terminal      `json:"terminals"`
		} `json:"params"`
	}
	if json.Unmarshal(line, &msg) != nil || msg.Method == "" {
		return Event{}, false
	}
	p := msg.Params
	return Event{Method: msg.Method, Seq: p.Seq, Type: p.Type, Terminal: p.Terminal, Data: p.Data, Terminals: p.Terminals}, true
}

// Terminals are the core's terminals as the event stream keeps them, in
// its order (herdr-core's schema/README.md, Events).
type Terminals struct {
	Seq  uint64
	List []Terminal
}

// Find returns the terminal at address.
func (ts *Terminals) Find(address string) (Terminal, bool) {
	if i := ts.index(address); i >= 0 {
		return ts.List[i], true
	}
	return Terminal{}, false
}

func (ts *Terminals) index(address string) int {
	return slices.IndexFunc(ts.List, func(t Terminal) bool { return t.Address == address })
}

// Put adds a terminal's record, or replaces it, as a call's result has it.
func (ts *Terminals) Put(t Terminal) {
	if i := ts.index(t.Address); i >= 0 {
		ts.List[i] = t
	} else {
		ts.List = append(ts.List, t)
	}
}

// Apply applies a notification, reporting whether the terminals changed.
// Events not above the snapshot's sequence were in it.
func (ts *Terminals) Apply(ev Event) bool {
	switch ev.Method {
	case "missed":
		ts.Seq, ts.List = ev.Seq, slices.Clone(ev.Terminals)
		return true
	case "event":
	default:
		return false
	}
	if ev.Seq <= ts.Seq && ts.Seq > 0 {
		return false
	}
	ts.Seq = ev.Seq
	i := ts.index(ev.Terminal)
	switch ev.Type {
	case "created", "restarted":
		var t Terminal
		if json.Unmarshal(ev.Data, &t) != nil {
			return false
		}
		ts.Put(t)
		return true
	case "removed":
		if i >= 0 {
			ts.List = slices.Delete(ts.List, i, i+1)
		}
		return i >= 0
	case "labels":
		var d struct {
			Labels map[string]string `json:"labels"`
			Record *Terminal         `json:"record"`
		}
		if json.Unmarshal(ev.Data, &d) != nil {
			return false
		}
		if d.Record != nil {
			ts.Put(*d.Record)
			return true
		}
		if i >= 0 {
			ts.List[i].Labels = d.Labels
		}
		return i >= 0
	}
	if i < 0 {
		return false
	}
	t := &ts.List[i]
	switch ev.Type {
	case "finished", "lost":
		var d struct {
			Reason string `json:"reason"`
		}
		json.Unmarshal(ev.Data, &d)
		t.Reason, t.Foreground = d.Reason, nil
		if ev.Type == "finished" {
			t.State = "finished"
		}
	case "status":
		var d struct {
			Status   *Status `json:"status"`
			Reported bool    `json:"reported"`
		}
		if json.Unmarshal(ev.Data, &d) != nil {
			return false
		}
		t.Status, t.Reported = d.Status, d.Reported
	case "title":
		var d struct {
			Title string `json:"title"`
		}
		json.Unmarshal(ev.Data, &d)
		t.Title = d.Title
	case "keep":
		var d struct {
			Keep bool `json:"keep"`
		}
		json.Unmarshal(ev.Data, &d)
		t.Keep = d.Keep
	case "size":
		var d struct {
			Cols int `json:"cols"`
			Rows int `json:"rows"`
		}
		json.Unmarshal(ev.Data, &d)
		t.Cols, t.Rows = d.Cols, d.Rows
	case "foreground":
		var f *Foreground
		if json.Unmarshal(ev.Data, &f) != nil {
			return false
		}
		t.Foreground = f
	default:
		return false
	}
	return true
}
