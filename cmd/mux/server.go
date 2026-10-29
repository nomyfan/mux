package main

import (
	_ "embed"

	"github.com/egoist/mygo/ui"

	"mux/internal/hc"
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

// serverMark shows the core as the app's icon, size wide.
func serverMark(c *ui.Context, size float32) {
	ui.Image(c, appIcon).Size(size, size).Shrink(0)
}

// serverDownView shows, in place of the panes, that herdr-core isn't
// installed, with the command installing it, or that the user stopped it,
// with a button starting it.
func (a *app) serverDownView(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Grow(1).MinWidth(0).Children(func() {
		ui.Row(c).Height(a.titleBand(c)).Shrink(0).DragWindow()
		ui.Column(c).Grow(1).Center().Gap(t.Space(3)).Padding(0, t.Space(6), a.titleBand(c), t.Space(6)).Children(func() {
			serverMark(c, 64)
			if a.notInstalled {
				ui.Text(c, "herdr-core isn’t installed").FontSize(t.FontSize + 2).Bold()
				ui.Text(c, "Mux shows the terminals of herdr-core (hc). Install it in a terminal:").TextColor(t.TextMuted)
				ui.Text(c, hc.InstallCommand).Font("monospace").Selectable().Padding(t.Space(2), t.Space(3)).
					Background(t.Surface).Radius(t.Space(1.5))
				ui.Text(c, "Mux finds it once it is.").TextColor(t.TextMuted)
				return
			}
			ui.Text(c, "herdr-core is stopped").FontSize(t.FontSize + 2).Bold()
			ui.Text(c, "Start it to work in your workspaces.").TextColor(t.TextMuted)
			a.serverButton(c)
			if a.serverErr != "" {
				ui.Text(c, a.serverErr).FontSize(12).TextColor(t.Danger)
			}
		})
	})
}

// serverStatus says whether the core runs, and which version.
func (a *app) serverStatus() string {
	switch {
	case a.conn != nil:
		return "Running · hc " + a.conn.Hello.Version
	case a.notInstalled:
		return "Not installed"
	case a.starting:
		return "Starting…"
	case a.stopped():
		return "Stopped"
	}
	return "Not connected"
}

// serverButton starts the core while the user stopped it, or asks the
// user to confirm stopping the one running.
func (a *app) serverButton(c *ui.Context) {
	switch {
	case a.conn != nil:
		if ui.Button(c.Key("stop-server"), "Stop Server…").TextColor(c.Theme().Danger).Clicked() {
			a.stopping = true
		}
	case a.stopped():
		label := "Start Server"
		if a.starting {
			label = "Starting…"
		}
		if ui.PrimaryButton(c.Key("start-server"), label).Disabled(a.starting).Clicked() {
			a.startServer()
		}
	}
}

// confirmStopServer asks the user to confirm stopping the core, while they
// asked to, and stops it once they do.
func (a *app) confirmStopServer(c *ui.Context) {
	if ui.AlertDialog(c, &a.stopping, "Stop herdr-core?", "What runs in its terminals stops; panes kept get new shells as it starts again.", "Cancel", "Stop Server") == 1 {
		a.stopServer()
	}
}
