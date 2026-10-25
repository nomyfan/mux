package main

import (
	_ "embed"
	"errors"

	"github.com/egoist/mygo/ui"

	"mux/src/herdr/server"
)

//go:embed icons/app.png
var appIconPNG []byte

// appIcon is the app's icon as it shows inside the app: the Dock's tile
// without its margin and shadow, which would tint what is under its
// corners. scripts/make-icon.sh renders both.
var appIcon = func() *ui.Bitmap {
	b, err := ui.DecodeBitmap(appIconPNG)
	if err != nil {
		panic(err)
	}
	return b
}()

// serverMark shows the server as the app's icon, size wide.
func serverMark(c *ui.Context, size float32) {
	ui.Image(c, appIcon).Size(size, size).Shrink(0)
}

// serverDownView shows, in place of the panes, that no herdr server runs,
// with a button starting one.
func (a *app) serverDownView(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Grow(1).MinWidth(0).Children(func() {
		ui.Row(c).Height(a.titleBand(c)).Shrink(0).DragWindow()
		ui.Column(c).Grow(1).Center().Gap(t.Space(3)).Padding(0, t.Space(6), a.titleBand(c), t.Space(6)).Children(func() {
			serverMark(c, 64)
			ui.Text(c, "The herdr server isn’t running").FontSize(t.FontSize + 2).Bold()
			ui.Text(c, "Start it to work in your workspaces.").TextColor(t.TextMuted)
			a.serverButton(c)
			if a.serverErr != "" {
				ui.Text(c, a.serverErr).FontSize(12).TextColor(t.Danger)
			}
		})
	})
}

// serverStatus says whether the server runs, and which version.
func (a *app) serverStatus() string {
	switch {
	case a.conn != nil:
		return "Running · " + a.conn.Welcome.ServerVersion
	case a.starting:
		return "Starting…"
	case a.serverDown:
		return "Not running"
	}
	return "Not connected"
}

// serverButton starts the server while none runs, or asks the user to
// confirm stopping the one running.
func (a *app) serverButton(c *ui.Context) {
	switch {
	case a.conn != nil:
		if ui.Button(c.Key("stop-server"), "Stop Server…").TextColor(c.Theme().Danger).Clicked() {
			a.stopping = true
		}
	case a.serverDown:
		label := "Start Server"
		if a.starting {
			label = "Starting…"
		}
		if ui.PrimaryButton(c.Key("start-server"), label).Disabled(a.starting).Clicked() {
			a.startServer()
		}
	}
}

// confirmStopServer asks the user to confirm stopping the server, while
// they asked to, and stops it once they do.
func (a *app) confirmStopServer(c *ui.Context) {
	if ui.AlertDialog(c, &a.stopping, "Stop the herdr server?", "All its panes close, and what runs in them stops.", "Cancel", "Stop Server") == 1 {
		a.stopServer()
	}
}

// startServer starts the server, which the connection finds as it
// listens; one that ends before says why.
func (a *app) startServer() {
	win := a.win
	a.starting, a.serverErr = true, ""
	go func() {
		cmd, err := server.Start()
		if err == nil {
			err = cmd.Wait()
		}
		if err == nil {
			err = errors.New("herdr server ended")
		}
		msg := server.StartError(err).Error()
		win.Update(func() {
			// Ended long after it started, it was stopped.
			if a.starting {
				a.starting, a.serverErr = false, msg
			}
		})
	}()
}

// stopServer asks the server to stop through its API socket; the
// connection ends with it.
func (a *app) stopServer() {
	win, socket := a.win, server.APISocket(a.socket)
	a.serverErr = ""
	go func() {
		if err := server.Stop(socket); err != nil {
			win.Update(func() { a.serverErr = err.Error() })
		}
	}()
}
