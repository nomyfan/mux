package hc

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ErrNotInstalled is Find's error where no herdr-core is installed.
var ErrNotInstalled = errors.New("herdr-core (hc) isn't installed")

// InstallCommand installs herdr-core.
const InstallCommand = "curl -fsSL https://releases.herdr.dev/core/install.sh | sh"

// Find returns the herdr-core command: herdr-core or hc on PATH, else
// where its installer and package managers put it. An hc counts only if
// its version says so: some window managers ship an unrelated hc.
func Find() (string, error) {
	var candidates []string
	for _, name := range []string{"herdr-core", "hc"} {
		if p, err := exec.LookPath(name); err == nil {
			candidates = append(candidates, p)
		}
	}
	home, _ := os.UserHomeDir()
	for _, dir := range []string{filepath.Join(home, ".local", "bin"), "/opt/homebrew/bin", "/usr/local/bin", filepath.Join(home, ".cargo", "bin")} {
		candidates = append(candidates, filepath.Join(dir, "herdr-core"), filepath.Join(dir, "hc"))
	}
	for _, p := range candidates {
		if Version(p) != "" {
			return p, nil
		}
	}
	return "", ErrNotInstalled
}

// Version returns the version of the herdr-core at bin, as hc 0.1.13, ""
// if bin isn't one.
func Version(bin string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "--version").Output()
	if v := strings.TrimSpace(string(out)); err == nil && strings.HasPrefix(v, "hc ") {
		return v
	}
	return ""
}

// LoginEnv returns the environment of the user's login shell: an app
// opened from the Finder has the system's, which lacks the user's PATH,
// and the core, which the app starts, gives its environment to the
// programs it runs.
func LoginEnv() ([]string, error) {
	shell := cmp.Or(os.Getenv("SHELL"), "/bin/sh")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// The markers keep out what the shell's profile prints.
	const mark = "\x00mux-env\x00"
	cmd := exec.CommandContext(ctx, shell, "-l", "-c", `printf '\0mux-env\0'; env -0; printf '\0mux-env\0'`)
	cmd.Stdin = nil
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	_, rest, ok := bytes.Cut(out, []byte(mark))
	if !ok {
		return nil, errors.New("the login shell printed no environment")
	}
	body, _, ok := bytes.Cut(rest, []byte(mark))
	if !ok {
		return nil, errors.New("the login shell printed no environment")
	}
	var env []string
	for _, kv := range bytes.Split(body, []byte{0}) {
		if s := string(kv); strings.Contains(s, "=") {
			env = append(env, s)
		}
	}
	return env, nil
}

// ClientEnv returns the environment the core's client command runs in,
// from env: none of the variables of a terminal of a core, as the app has
// when started from one during development, which would make the app's
// terminals that one's children.
func ClientEnv(env []string) []string {
	var out []string
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if name == "HC_TERMINAL_ID" || strings.HasPrefix(name, "HERDR_") {
			continue
		}
		out = append(out, kv)
	}
	return out
}
