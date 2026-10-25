package server

import (
	"bufio"
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
)

func TestAPISocket(t *testing.T) {
	got := APISocket("/home/u/.config/herdr/sessions/s/herdr-client.sock")
	if want := "/home/u/.config/herdr/sessions/s/herdr.sock"; got != want {
		t.Errorf("APISocket = %q, want %q", got, want)
	}
}

// fakeServer answers each request on a socket with reply, handing the
// method of each to methods.
func fakeServer(t *testing.T, reply string) (path string, methods chan string) {
	path = filepath.Join(t.TempDir(), "herdr.sock")
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	methods = make(chan string, 1)
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			line, _ := bufio.NewReader(conn).ReadBytes('\n')
			var req struct{ Method string }
			json.Unmarshal(line, &req)
			methods <- req.Method
			conn.Write([]byte(reply + "\n"))
			conn.Close()
		}
	}()
	return path, methods
}

func TestStop(t *testing.T) {
	path, methods := fakeServer(t, `{"id":"mux","result":{"type":"ok"}}`)
	if err := Stop(path); err != nil {
		t.Fatal(err)
	}
	if m := <-methods; m != "server.stop" {
		t.Errorf("sent %q", m)
	}
}

func TestStopRefused(t *testing.T) {
	path, _ := fakeServer(t, `{"id":"mux","error":{"code":"unsupported_method","message":"no"}}`)
	if err := Stop(path); err == nil || err.Error() != "unsupported_method: no" {
		t.Errorf("err %v", err)
	}
}

// A missing socket, as a stopped server's, is one with no server.
func TestNotRunning(t *testing.T) {
	_, err := net.Dial("unix", filepath.Join(t.TempDir(), "herdr-client.sock"))
	if !NotRunning(err) {
		t.Errorf("NotRunning(%v) = false", err)
	}
}
