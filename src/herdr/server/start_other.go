//go:build !unix

package server

import (
	"errors"
	"os/exec"
)

var ErrNoHerdr = errors.New("herdr runs on Unix only")

func Start() (*exec.Cmd, error) { return nil, ErrNoHerdr }

func StartError(err error) error { return err }
