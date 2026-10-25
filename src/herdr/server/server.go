// Package server starts and stops the herdr server: it stops it through its
// JSON API socket, which takes a request a line, and starts it as the
// herdr command does. No UI dependency.
package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"syscall"
	"time"
)

// APISocket returns the API socket beside an endpoint's client socket.
func APISocket(clientSocket string) string {
	return strings.TrimSuffix(clientSocket, "-client.sock") + ".sock"
}

// NotRunning reports whether err, of dialing a socket, says no server
// listens there: the socket is missing, or refuses connections as a server
// that stopped leaves it.
func NotRunning(err error) bool {
	return errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ECONNREFUSED)
}

// Stop asks the server at the API socket to stop. Its panes close, and
// what runs in them stops.
func Stop(apiSocket string) error {
	_, err := call(apiSocket, "server.stop", struct{}{})
	return err
}

// call sends a request to the API socket and returns the result of its
// response.
func call(apiSocket, method string, params any) (json.RawMessage, error) {
	conn, err := net.DialTimeout("unix", apiSocket, 3*time.Second)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	req, err := json.Marshal(map[string]any{"id": "mux", "method": method, "params": params})
	if err != nil {
		return nil, err
	}
	if _, err := conn.Write(append(req, '\n')); err != nil {
		return nil, err
	}
	var resp struct {
		Result json.RawMessage
		Error  *struct{ Code, Message string }
	}
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return nil, err
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("%s: %s", resp.Error.Code, resp.Error.Message)
	}
	return resp.Result, nil
}
