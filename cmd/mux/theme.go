package main

import (
	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

// The app's colors are Vellum's, a warm theme in a light and a dark kind:
// the controls take its app colors (its web preview's), the terminals
// its Ghostty ones, with a true blue and cyan, bright colors of their
// own, and grays that show on either ground.

// lightTheme and darkTheme color the app's controls; Surface is the
// sidebar's ground.
var lightTheme, darkTheme = func() (light, dark *ui.Theme) {
	light = ui.LightTheme()
	light.Background = ui.Hex("#f8f3ea")
	light.Surface = ui.Hex("#f1eae1")
	light.SurfaceHover = ui.Hex("#e9e0d5")
	light.SurfacePressed = ui.Hex("#e1d7cb")
	light.Border = ui.Hex("#e5ddd1")
	light.Text = ui.Hex("#45413d")
	light.TextMuted = ui.Hex("#6f6861")
	light.Accent = ui.Hex("#b1632f")
	light.AccentHover = ui.Hex("#c08157")
	light.AccentPressed = ui.Hex("#c7906a")
	light.AccentText = ui.Hex("#ffffff")
	light.Danger = ui.Hex("#b75051")
	light.Warning = ui.Hex("#9c6400")
	light.Success = ui.Hex("#507c39")
	light.Selection = ui.Hex("#dcd2c6")
	light.Focus = light.Accent.Alpha(0.5)
	light.Scrollbar = light.Text.Alpha(0.3)

	dark = ui.DarkTheme()
	dark.Background = ui.Hex("#1a1816")
	dark.Surface = ui.Hex("#151311")
	dark.SurfaceHover = ui.Hex("#2c2925")
	dark.SurfacePressed = ui.Hex("#413b35")
	dark.Border = ui.Hex("#3e3934")
	dark.Text = ui.Hex("#d8d3cb")
	dark.TextMuted = ui.Hex("#948d85")
	dark.Accent = ui.Hex("#e6a67e")
	dark.AccentHover = ui.Hex("#bd8a69")
	dark.AccentPressed = ui.Hex("#a97b5f")
	dark.AccentText = ui.Hex("#1a1816")
	dark.Danger = ui.Hex("#e68585")
	dark.Warning = ui.Hex("#e0a84a")
	dark.Success = ui.Hex("#a6c485")
	dark.Selection = ui.Hex("#413b35")
	dark.Focus = dark.Accent.Alpha(0.5)
	dark.Scrollbar = dark.Text.Alpha(0.3)
	return light, dark
}()

// lightTerminal and darkTerminal color the panes' cells. The cursor takes
// the controls' accent; a search marks its matches in yellow, the one it is
// at in the accent.
var lightTerminal, darkTerminal = func() (light, dark *terminal.Theme) {
	light = &terminal.Theme{
		Foreground: ui.Hex("#45413d"), Background: ui.Hex("#f8f3ea"),
		Cursor: lightTheme.Accent, CursorText: ui.Hex("#f8f3ea"),
		Selection: ui.Hex("#d3c6ba"), SelectionText: ui.Hex("#45413d"),
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
