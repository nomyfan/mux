//go:build unix

package server

import (
	"cmp"
	"errors"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

// ErrNoHerdr is the error of a server that could not start as the user's
// login shell finds no herdr command.
var ErrNoHerdr = errors.New("the login shell finds no herdr command")

// Start starts the server, as `herdr server`, in a session of its own,
// so that it outlives the app, run by the user's login shell: an app
// opened from the Finder has the system's PATH, which lacks herdr and the
// user's own, which the programs in the server's panes need. Wait waits
// for the server to end.
func Start() (*exec.Cmd, error) {
	shell := cmp.Or(os.Getenv("SHELL"), "/bin/sh")
	cmd := exec.Command(shell, "-l", "-c", "exec herdr server")
	// Started from a herdr pane, as during development, the app has the
	// pane's variables, which are not the server's.
	for _, kv := range os.Environ() {
		switch name, _, _ := strings.Cut(kv, "="); name {
		case "HERDR_ENV", "HERDR_PANE_ID", "HERDR_TAB_ID", "HERDR_WORKSPACE_ID":
		default:
			cmd.Env = append(cmd.Env, kv)
		}
	}
	cmd.Dir, _ = os.UserHomeDir()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}

// StartError explains the error of Wait on a server that ended as it
// started.
func StartError(err error) error {
	if exit, ok := errors.AsType[*exec.ExitError](err); ok && exit.ExitCode() == 127 {
		return ErrNoHerdr
	}
	return err
}
