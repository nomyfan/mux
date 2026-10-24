package main

import (
	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

// The app's colors are Vellum's, a warm theme in a light and a dark kind:
// the terminals take its Ghostty colors, the rest its Zed ones.

// lightTheme and darkTheme color the app's controls; Surface is the
// sidebar's ground.
var lightTheme, darkTheme = func() (light, dark *ui.Theme) {
	light = ui.LightTheme()
	light.Background = ui.Hex("#faf8f5")
	light.Surface = ui.Hex("#f2efec")
	light.SurfaceHover = ui.Hex("#ebe5de")
	light.SurfacePressed = ui.Hex("#e0d6cc")
	light.Border = ui.Hex("#e0d6cc")
	light.Text = ui.Hex("#45413d")
	light.TextMuted = ui.Hex("#8a837b")
	light.Accent = ui.Hex("#b05918")
	light.AccentHover = ui.Hex("#9a4e15")
	light.AccentPressed = ui.Hex("#854310")
	light.AccentText = ui.Hex("#faf8f5")
	light.Danger = ui.Hex("#b75051")
	light.Warning = ui.Hex("#8e6c00")
	light.Success = ui.Hex("#507c39")
	light.Selection = ui.Hex("#d5cbc2")
	light.Focus = light.Accent.Alpha(0.5)
	light.Scrollbar = light.Text.Alpha(0.3)

	dark = ui.DarkTheme()
	dark.Background = ui.Hex("#1a1816")
	dark.Surface = ui.Hex("#151311")
	dark.SurfaceHover = ui.Hex("#24221f")
	dark.SurfacePressed = ui.Hex("#413b35")
	dark.Border = ui.Hex("#2a2723")
	dark.Text = ui.Hex("#d8d3cb")
	dark.TextMuted = ui.Hex("#7c7670")
	dark.Accent = ui.Hex("#e6a67e")
	dark.AccentHover = ui.Hex("#edb894")
	dark.AccentPressed = ui.Hex("#d9956c")
	dark.AccentText = ui.Hex("#1a1816")
	dark.Danger = ui.Hex("#e68585")
	dark.Warning = ui.Hex("#dbbc7f")
	dark.Success = ui.Hex("#a6c485")
	dark.Selection = ui.Hex("#413b35")
	dark.Focus = dark.Accent.Alpha(0.6)
	dark.Scrollbar = dark.Text.Alpha(0.3)
	return light, dark
}()

// lightTerminal and darkTerminal color the panes' cells; a search marks its
// matches in the warning color, the one it is at in the accent.
var lightTerminal, darkTerminal = func() (light, dark *terminal.Theme) {
	light = &terminal.Theme{
		Foreground: ui.Hex("#45413d"), Background: ui.Hex("#faf8f5"),
		Cursor: ui.Hex("#b05918"), CursorText: ui.Hex("#faf8f5"),
		Selection: ui.Hex("#d5cbc2"), SelectionText: ui.Hex("#45413d"),
		Palette: [16]ui.Color{
			ui.Hex("#45413d"), ui.Hex("#b75051"), ui.Hex("#507c39"), ui.Hex("#8e6c00"),
			ui.Hex("#4f6e5b"), ui.Hex("#975f87"), ui.Hex("#b05918"), ui.Hex("#ebe5de"),
			ui.Hex("#a49e97"), ui.Hex("#b75051"), ui.Hex("#507c39"), ui.Hex("#8e6c00"),
			ui.Hex("#4f6e5b"), ui.Hex("#975f87"), ui.Hex("#b05918"), ui.Hex("#faf8f5"),
		},
	}
	dark = &terminal.Theme{
		Foreground: ui.Hex("#d8d3cb"), Background: ui.Hex("#1a1816"),
		Cursor: ui.Hex("#e6a67e"), CursorText: ui.Hex("#1a1816"),
		Selection: ui.Hex("#413b35"), SelectionText: ui.Hex("#d8d3cb"),
		Palette: [16]ui.Color{
			ui.Hex("#151311"), ui.Hex("#e68585"), ui.Hex("#a6c485"), ui.Hex("#dbbc7f"),
			ui.Hex("#8fa895"), ui.Hex("#d699b6"), ui.Hex("#e6a67e"), ui.Hex("#d8d3cb"),
			ui.Hex("#5c5650"), ui.Hex("#e68585"), ui.Hex("#a6c485"), ui.Hex("#dbbc7f"),
			ui.Hex("#8fa895"), ui.Hex("#d699b6"), ui.Hex("#e6a67e"), ui.Hex("#d8d3cb"),
		},
	}
	for _, t := range []*terminal.Theme{light, dark} {
		warning, accent := t.Palette[3], t.Palette[6]
		t.Search, t.SearchText = t.Background.Mix(warning, 0.35), t.Foreground
		t.SearchSelected, t.SearchSelectedText = accent, t.Background
	}
	return light, dark
}()

// setTheme colors the frame in Vellum's light or dark kind, as the system
// appears.
func setTheme(c *ui.Context) {
	if c.Theme().Dark {
		c.SetTheme(darkTheme)
	} else {
		c.SetTheme(lightTheme)
	}
}
