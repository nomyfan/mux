package hc

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

// fakeCore is the core's end of a connection: it answers hello, then
// reads and writes frames.
type fakeCore struct {
	t *testing.T
	r *bufio.Reader
	w io.WriteCloser
}

func newFake(t *testing.T) (*Conn, *fakeCore) {
	t.Helper()
	toCore, fromClient := io.Pipe()
	fromCore, toClient := io.Pipe()
	f := &fakeCore{t: t, r: bufio.NewReader(toCore), w: toClient}
	go func() {
		line, _ := f.r.ReadBytes('\n')
		var hello struct{ Params struct{ Framed bool } }
		json.Unmarshal(line, &hello)
		if !hello.Params.Framed {
			t.Error("hello asked for no framed mode")
		}
		io.WriteString(toClient, `{"jsonrpc":"2.0","id":0,"result":{"generation":1,"version":"0.1.13","methods":["terminal.run"],"features":[],"core":{"id":"x","name":"vm"}}}`+"\n")
	}()
	c, err := Open(fromCore, fromClient, func() error { toClient.Close(); return fromClient.Close() })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c, f
}

func (f *fakeCore) frame() (uint32, byte, []byte) {
	var h [headerSize]byte
	if _, err := io.ReadFull(f.r, h[:]); err != nil {
		f.t.Fatal(err)
	}
	p := make([]byte, binary.LittleEndian.Uint32(h[5:]))
	io.ReadFull(f.r, p)
	return binary.LittleEndian.Uint32(h[:]), h[4], p
}

// request reads the next call, skipping other frames.
func (f *fakeCore) request() (id uint64, method string, params json.RawMessage) {
	for {
		ch, kind, p := f.frame()
		if ch != 0 || kind != kindData {
			continue
		}
		var req struct {
			ID     uint64
			Method string
			Params json.RawMessage
		}
		json.Unmarshal(p, &req)
		return req.ID, req.Method, req.Params
	}
}

func (f *fakeCore) send(channel uint32, kind byte, payload string) {
	f.w.Write(encode(channel, kind, []byte(payload)))
}

func (f *fakeCore) reply(id uint64, result string) {
	f.send(0, kindData, `{"jsonrpc":"2.0","id":`+jsonNum(id)+`,"result":`+result+`}`)
}

func jsonNum(n uint64) string { b, _ := json.Marshal(n); return string(b) }

func TestHelloCheck(t *testing.T) {
	c, _ := newFake(t)
	if c.Hello.Version != "0.1.13" || c.Hello.Core.Name != "vm" {
		t.Errorf("hello = %+v", c.Hello)
	}
	if err := c.Hello.Check(); err == nil || !strings.Contains(err.Error(), "terminal.attach") {
		t.Errorf("Check = %v, want the methods it lacks", err)
	}
	full := Hello{Generation: 1, Methods: Uses}
	if err := full.Check(); err != nil {
		t.Error(err)
	}
	full.Generation = 2
	if err := full.Check(); err == nil {
		t.Error("a newer generation passed")
	}
}

// Calls run side by side: their answers may come in any order.
func TestCalls(t *testing.T) {
	c, f := newFake(t)
	type result struct {
		t   Terminal
		err error
	}
	done := make(chan result, 2)
	go func() {
		t, err := c.Run(RunOptions{Cwd: "/a"})
		done <- result{t, err}
	}()
	id1, m1, p1 := f.request()
	go func() {
		err := c.Kill("t9")
		done <- result{err: err}
	}()
	id2, m2, _ := f.request()
	if m1 != "terminal.run" || m2 != "terminal.kill" || !strings.Contains(string(p1), `"cwd":"/a"`) {
		t.Fatalf("got %s %s %s", m1, p1, m2)
	}
	f.send(0, kindData, `{"jsonrpc":"2.0","id":`+jsonNum(id2)+`,"error":{"code":-32000,"message":"no terminal t9","data":{"kind":"not_found","hint":"see hc ls"}}}`)
	if r := <-done; Kind(r.err) != "not_found" || !strings.Contains(r.err.Error(), "see hc ls") {
		t.Errorf("kill: %v", r.err)
	}
	f.reply(id1, `{"address":"t3","state":"live"}`)
	if r := <-done; r.err != nil || r.t.Address != "t3" {
		t.Errorf("run: %+v", r)
	}
}

// A stream takes the frames that came before its reply was taken, credits
// what it reads, and ends as the core closes it.
func TestStream(t *testing.T) {
	c, f := newFake(t)
	got := make(chan *Stream)
	go func() {
		s, _, err := c.Attach(AttachOptions{Terminal: "t1", Cols: 80, Rows: 24})
		if err != nil {
			t.Error(err)
		}
		got <- s
	}()
	id, method, _ := f.request()
	if method != "terminal.attach" {
		t.Fatal(method)
	}
	f.send(5, kindData, "first paint")
	f.reply(id, `{"channel":5,"terminal":{"address":"t1","state":"live"}}`)
	s := <-got
	buf := make([]byte, 5)
	n, err := s.Read(buf)
	if string(buf[:n]) != "first" || err != nil {
		t.Fatalf("read %q %v", buf[:n], err)
	}
	if ch, kind, p := f.frame(); ch != 5 || kind != kindCredit || binary.LittleEndian.Uint32(p) != 5 {
		t.Errorf("credit frame %d %d %v", ch, kind, p)
	}
	s.Write([]byte("ls\r"))
	if ch, kind, p := f.frame(); ch != 5 || kind != kindData || string(p) != "ls\r" {
		t.Errorf("input frame %d %d %q", ch, kind, p)
	}
	s.Resize(100, 30)
	if _, kind, p := f.frame(); kind != kindResize || binary.LittleEndian.Uint16(p) != 100 || binary.LittleEndian.Uint16(p[2:]) != 30 {
		t.Errorf("resize frame %d %v", kind, p)
	}
	s.Facts(Display{Background: "#000000", Cell: &[2]uint32{8, 16}})
	if _, kind, p := f.frame(); kind != kindFacts || string(p) != `{"bg":"#000000","cell":[8,16]}` {
		t.Errorf("facts frame %d %s", kind, p)
	}
	f.send(5, kindClose, `{"reason":"exited","exit_code":0}`)
	rest, err := io.ReadAll(s)
	if string(rest) != " paint" || err != nil {
		t.Errorf("rest %q %v", rest, err)
	}
	if !strings.Contains(string(s.Reason()), "exited") {
		t.Errorf("reason %s", s.Reason())
	}
}

// Once the connection is gone, calls fail at once and streams end.
func TestLost(t *testing.T) {
	c, f := newFake(t)
	go func() {
		id, _, _ := f.request()
		f.reply(id, `{"channel":2,"terminal":{"address":"t1","state":"live"}}`)
	}()
	s, _, err := c.Attach(AttachOptions{Terminal: "t1", Cols: 1, Rows: 1})
	if err != nil {
		t.Fatal(err)
	}
	f.w.Close()
	select {
	case <-c.Lost():
	case <-time.After(time.Second):
		t.Fatal("not lost")
	}
	if _, err := s.Read(make([]byte, 1)); !errors.Is(err, ErrLost) {
		t.Errorf("read: %v", err)
	}
	if err := c.Kill("t1"); !errors.Is(err, ErrLost) {
		t.Errorf("call: %v", err)
	}
}

func TestEventsApply(t *testing.T) {
	ts := Terminals{Seq: 5, List: []Terminal{{Address: "t1", State: "live"}}}
	ev := func(line string) Event {
		e, ok := parseEvent([]byte(line))
		if !ok {
			t.Fatalf("no event in %s", line)
		}
		return e
	}
	if ts.Apply(ev(`{"method":"event","params":{"seq":5,"type":"title","terminal":"t1","data":{"title":"old"}}}`)) {
		t.Error("an event of the snapshot applied")
	}
	ts.Apply(ev(`{"method":"event","params":{"seq":6,"type":"created","terminal":"t2","data":{"address":"t2","state":"live","labels":{"mux":"1"}}}}`))
	ts.Apply(ev(`{"method":"event","params":{"seq":7,"type":"status","terminal":"t2","data":{"status":{"state":"blocked","kind":"permission","app":"claude-code"},"records":{},"reported":true}}}`))
	ts.Apply(ev(`{"method":"event","params":{"seq":8,"type":"foreground","terminal":"t2","data":{"pid":7,"name":"claude","argv":["claude"],"cwd":"/src"}}}`))
	t2, ok := ts.Find("t2")
	if !ok || t2.Status == nil || t2.Status.App != "claude-code" || t2.Foreground == nil || t2.Foreground.Cwd != "/src" || t2.Labels["mux"] != "1" {
		t.Fatalf("t2 = %+v", t2)
	}
	ts.Apply(ev(`{"method":"event","params":{"seq":9,"type":"finished","terminal":"t1","data":{"reason":"exited","exit_code":0}}}`))
	if t1, _ := ts.Find("t1"); t1.Live() || t1.Reason != "exited" {
		t.Errorf("t1 = %+v", t1)
	}
	ts.Apply(ev(`{"method":"event","params":{"seq":10,"type":"removed","terminal":"t1","data":{}}}`))
	if _, ok := ts.Find("t1"); ok {
		t.Error("t1 not removed")
	}
	ts.Apply(ev(`{"method":"missed","params":{"seq":20,"terminals":[{"address":"t9","state":"live"}]}}`))
	if len(ts.List) != 1 || ts.List[0].Address != "t9" || ts.Seq != 20 {
		t.Errorf("after missed: %+v", ts)
	}
}

func TestClientEnv(t *testing.T) {
	got := ClientEnv([]string{"PATH=/bin", "HC_TERMINAL_ID=t3", "HERDR_ENV=1", "HC_SESSION=x"})
	if strings.Join(got, " ") != "PATH=/bin HC_SESSION=x" {
		t.Errorf("ClientEnv = %v", got)
	}
}
