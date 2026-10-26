package main

import (
	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

// The app's colors are Vellum's, a warm theme in a light and a dark kind:
// the controls take its Zed colors, the terminals its Ghostty ones, but
// with a true blue and cyan, bright colors of their own, and grays that
// show on either ground.

// lightTheme and darkTheme color the app's controls; Surface is the
// sidebar's ground.
var lightTheme, darkTheme = func() (light, dark *ui.Theme) {
	light = ui.LightTheme()
	light.Background = ui.Hex("#faf8f5")
	light.Surface = ui.Hex("#efeae4")
	light.SurfaceHover = ui.Hex("#e7e0d8")
	light.SurfacePressed = ui.Hex("#e0d6cc")
	light.Border = ui.Hex("#cbbdae")
	light.Text = ui.Hex("#45413d")
	light.TextMuted = ui.Hex("#8a837b")
	light.Accent = ui.Hex("#b8622a")
	light.AccentHover = ui.Hex("#a2561f")
	light.AccentPressed = ui.Hex("#8e4b1a")
	light.AccentText = ui.Hex("#faf8f5")
	light.Danger = ui.Hex("#b75051")
	light.Warning = ui.Hex("#b3760f")
	light.Success = ui.Hex("#507c39")
	light.Selection = ui.Hex("#d5cbc2")
	light.Focus = light.Accent.Alpha(0.5)
	light.Scrollbar = light.Text.Alpha(0.3)

	dark = ui.DarkTheme()
	dark.Background = ui.Hex("#1a1816")
	dark.Surface = ui.Hex("#151311")
	dark.SurfaceHover = ui.Hex("#24221f")
	dark.SurfacePressed = ui.Hex("#413b35")
	dark.Border = ui.Hex("#3e3934")
	dark.Text = ui.Hex("#d8d3cb")
	dark.TextMuted = ui.Hex("#7c7670")
	dark.Accent = ui.Hex("#cf9370")
	dark.AccentHover = ui.Hex("#d9a07f")
	dark.AccentPressed = ui.Hex("#c4865f")
	dark.AccentText = ui.Hex("#1a1816")
	dark.Danger = ui.Hex("#e68585")
	dark.Warning = ui.Hex("#e0a84a")
	dark.Success = ui.Hex("#a6c485")
	dark.Selection = ui.Hex("#413b35")
	dark.Focus = dark.Accent.Alpha(0.6)
	dark.Scrollbar = dark.Text.Alpha(0.3)
	return light, dark
}()

// lightTerminal and darkTerminal color the panes' cells. The cursor takes
// the controls' accent; a search marks its matches in yellow, the one it is
// at in the accent.
var lightTerminal, darkTerminal = func() (light, dark *terminal.Theme) {
	light = &terminal.Theme{
		Foreground: ui.Hex("#45413d"), Background: ui.Hex("#faf8f5"),
		Cursor: lightTheme.Accent, CursorText: ui.Hex("#faf8f5"),
		Selection: ui.Hex("#d5cbc2"), SelectionText: ui.Hex("#45413d"),
		Palette: [16]ui.Color{
			ui.Hex("#45413d"), ui.Hex("#b75051"), ui.Hex("#507c39"), ui.Hex("#956900"),
			ui.Hex("#4973a3"), ui.Hex("#975f87"), ui.Hex("#327b7b"), ui.Hex("#7a736c"),
			ui.Hex("#958d85"), ui.Hex("#cd6163"), ui.Hex("#519132"), ui.Hex("#ac7900"),
			ui.Hex("#4e85c4"), ui.Hex("#b867a2"), ui.Hex("#2b9090"), ui.Hex("#a8a097"),
		},
	}
	dark = &terminal.Theme{
		Foreground: ui.Hex("#d8d3cb"), Background: ui.Hex("#1a1816"),
		Cursor: darkTheme.Accent, CursorText: ui.Hex("#1a1816"),
		Selection: ui.Hex("#413b35"), SelectionText: ui.Hex("#d8d3cb"),
		Palette: [16]ui.Color{
			ui.Hex("#4f4943"), ui.Hex("#e68585"), ui.Hex("#a6c485"), ui.Hex("#dbbc7f"),
			ui.Hex("#8badd3"), ui.Hex("#d699b6"), ui.Hex("#7ebdb8"), ui.Hex("#d8d3cb"),
			ui.Hex("#6e6862"), ui.Hex("#eaa9a7"), ui.Hex("#c1d2a7"), ui.Hex("#e3cda3"),
			ui.Hex("#afc3da"), ui.Hex("#e0b6c7"), ui.Hex("#a7cec8"), ui.Hex("#f2ede6"),
		},
	}
	for _, t := range []*terminal.Theme{light, dark} {
		yellow := t.Palette[3]
		t.Search, t.SearchText = t.Background.Mix(yellow, 0.35), t.Foreground
		t.SearchSelected, t.SearchSelectedText = t.Cursor, t.Background
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
