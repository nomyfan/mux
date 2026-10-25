package main

import (
	"cmp"
	_ "embed"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// settings are what the user set in the settings dialog, kept in the app's
// data directory across launches.
type settings struct {
	// Appearance is light, dark, or the system's.
	Appearance mygo.ThemeSource `json:"appearance,omitempty"`
	// TerminalFont is the terminals' font family, "" for the system's
	// monospaced one, and FallbackFont the one filling in the glyphs it
	// lacks, as Nerd Font icons, "" for none.
	TerminalFont string `json:"terminal_font,omitempty"`
	FallbackFont string `json:"fallback_font,omitempty"`
	// FontSize is the terminals' font size, 0 for the default.
	FontSize float64 `json:"font_size,omitempty"`
}

// The terminals' font sizes: the default, and the bounds of the control.
const (
	defaultFontSize          = 14
	minFontSize, maxFontSize = 8, 32
)

// The font controls' choices for no family of the user's: the system's
// monospaced font, and no fallback font.
const (
	systemFont = "System"
	noFont     = "None"
)

// terminalFont is the terminals' font: the fallback family, if any, fills
// in the glyphs the first lacks, before the system's fallback fonts.
func (s settings) terminalFont() ui.Font {
	families := cmp.Or(s.TerminalFont, "monospace")
	if s.FallbackFont != "" {
		families += ", " + s.FallbackFont
	}
	return ui.Font{Family: families, Size: float32(cmp.Or(s.FontSize, defaultFontSize))}
}

// appearances are the choices of the dialog's appearance control, in order.
var appearances = []mygo.ThemeSource{mygo.ThemeSystem, mygo.ThemeLight, mygo.ThemeDark}

// setThemeSource sets the app's appearance; tests, without the app
// running, replace it.
var setThemeSource = mygo.Theme.SetSource

// settingsPath is the file the settings are kept in.
func settingsPath() (string, error) {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "settings.json"), nil
}

// loadSettings reads the settings saved, else the defaults.
func loadSettings() settings {
	var s settings
	if path, err := settingsPath(); err == nil {
		if data, err := os.ReadFile(path); err == nil {
			json.Unmarshal(data, &s)
		}
	}
	return s
}

// save writes the settings for the next launch.
func (s settings) save() error {
	path, err := settingsPath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// openSettings opens the settings dialog, on the page it showed last, its
// controls showing the settings.
func (a *app) openSettings() {
	a.appearance = max(slices.Index(appearances, a.settings.Appearance), 0)
	a.terminalFamily = cmp.Or(a.settings.TerminalFont, systemFont)
	a.fallbackFamily = cmp.Or(a.settings.FallbackFont, noFont)
	a.fontSize = cmp.Or(a.settings.FontSize, defaultFontSize)
	a.settingsPage = cmp.Or(a.settingsPage, "server")
	a.settingsOpen = true
}

//go:embed icons/appearance.svg
var appearanceSVG []byte

// appearanceTile is the appearance page's icon: a painter's palette on a
// tile shaped and colored as the app's icon.
var appearanceTile = ui.MustParseSVG(appearanceSVG)

// pageItem is the settings sidebar's item choosing the page id, named
// label, with the page's icon in its own colors, larger than SidebarItem's
// line icons: the item widens its padding before its label for the icon,
// which lies in it. Add to the item with Children.
func (a *app) pageItem(c *ui.Context, id, label string, icon ui.ImageSource) ui.Element {
	t := c.Theme()
	const size = 24
	item := ui.SidebarItem(c.Key(id), id, nil, label).Padding(t.Space(2), t.Space(2), t.Space(2), t.Space(2)+size+t.Space(2.5))
	chosenLook(c, item, a.settingsPage == id)
	item.Children(func() {
		ui.Box(c).Absolute().Left(t.Space(2)).Top(0).Bottom(0).Center().Children(func() {
			ui.Image(c, icon).Size(size, size)
		})
	})
	return item
}

// settingsDialog shows the settings while the user has them open (Cmd+,),
// until the close button or Escape closes them; a click outside does not.
// Its pages list on the left, the one chosen shows on the right. A change
// takes effect at once, and is saved.
func (a *app) settingsDialog(c *ui.Context) {
	t := c.Theme()
	w, h := c.Size()
	ui.DialogBase(c, &a.settingsOpen, func(backdrop, panel ui.Element) {
		if backdrop.Clicked() {
			a.settingsOpen = true
		}
		// Modal's look, without its padding.
		backdrop.Background(ui.RGBA(0, 0, 0, 0.4))
		panel.Row().AlignItems(ui.Stretch).Width(min(720, w-t.Space(10))).Height(min(480, h-t.Space(10))).
			Radius(t.Space(2.5)).Background(t.Background).Clip().Shadow(0, 10, 30, 0, ui.RGBA(0, 0, 0, 0.3))
		// The sidebar pads its items itself; the first lines up with the
		// page's title.
		ui.Column(c).Width(180).Shrink(0).Padding(t.Space(1.5), 0, 0, 0).Background(t.Surface).Children(func() {
			ui.Sidebar(c, &a.settingsPage, func() {
				dot := t.TextMuted
				if a.conn != nil {
					dot = t.Success
				}
				a.pageItem(c, "server", "Server", appIcon).Children(func() {
					ui.Box(c).Size(8, 8).Shrink(0).Radius(4).Background(dot)
				})
				a.pageItem(c, "appearance", "Appearance", appearanceTile)
			}).Grow(1).Label("Settings")
		})
		ui.Box(c).Width(1).Shrink(0).Background(t.Border)
		ui.Column(c).Grow(1).MinWidth(0).Padding(t.Space(4), t.Space(5), t.Space(5), t.Space(5)).Gap(t.Space(4)).Children(func() {
			title := map[string]string{"server": "Server", "appearance": "Appearance"}[a.settingsPage]
			ui.Row(c).AlignItems(ui.Center).Shrink(0).Children(func() {
				ui.Text(c, title).FontSize(t.FontSize + 4).Bold().Grow(1)
				if iconButton(c, "close", closeIcon, "Close").Clicked() {
					a.settingsOpen = false
				}
			})
			switch a.settingsPage {
			case "server":
				a.serverSettings(c)
			case "appearance":
				a.appearanceSettings(c)
			}
		})
	})
}

// settingsCard groups settings in a bordered box, each row fn builds
// apart from the one before.
func settingsCard(c *ui.Context, fn func()) {
	t := c.Theme()
	ui.Column(c).Shrink(0).Padding(t.Space(1), t.Space(4)).Border(1, t.Border).Radius(t.Space(2)).Children(fn)
}

// settingsRow is a row of a settings card: a label, a description under
// it if any, and control after them, which builds the row's control.
func settingsRow(c *ui.Context, label, description string, control func()) {
	t := c.Theme()
	ui.Row(c).AlignItems(ui.Center).Gap(t.Space(4)).Padding(t.Space(2.5), 0).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(t.Space(0.5)).Children(func() {
			ui.Text(c, label)
			if description != "" {
				ui.Text(c, description).FontSize(t.FontSize - 1).TextColor(t.TextMuted)
			}
		})
		control()
	})
}

// serverSettings shows whether the herdr server runs, with a button
// starting or stopping it, and where the app finds it.
func (a *app) serverSettings(c *ui.Context) {
	t := c.Theme()
	settingsCard(c, func() {
		ui.Row(c).AlignItems(ui.Center).Gap(t.Space(3)).Padding(t.Space(2.5), 0).Children(func() {
			serverMark(c, 40)
			ui.Column(c).Grow(1).MinWidth(0).Gap(t.Space(0.5)).Children(func() {
				ui.Text(c, "herdr server").Bold()
				ui.Text(c, a.serverStatus()).FontSize(t.FontSize - 1).TextColor(t.TextMuted)
			})
			a.serverButton(c)
		})
	})
	if a.serverErr != "" {
		ui.Text(c, a.serverErr).FontSize(12).TextColor(t.Danger)
	}
	settingsCard(c, func() {
		settingsRow(c, "Socket", "Where the app connects to the server.", func() {})
		ui.Text(c, a.socket).FontSize(t.FontSize-1).TextColor(t.TextMuted).Selectable().Padding(0, 0, t.Space(2.5), 0)
	})
}

// appearanceSettings chooses the app's appearance.
func (a *app) appearanceSettings(c *ui.Context) {
	settingsCard(c, func() {
		settingsRow(c, "Theme", "Auto follows the system’s appearance.", func() {
			ui.Segmented(c.Key("appearance"), &a.appearance, "Auto", "Light", "Dark").Label("Theme").
				OnChange(func() { a.setAppearance(appearances[a.appearance]) })
		})
		ui.Divider(c)
		settingsRow(c, "Terminal Font", "The monospaced font the terminals use.", func() {
			ui.Combobox(c.Key("terminal-font"), &a.terminalFamily, a.monoFonts).Label("Terminal Font").Width(220).
				OnChange(func() { a.setFont(&a.settings.TerminalFont, a.terminalFamily, systemFont) })
		})
		ui.Divider(c)
		settingsRow(c, "Fallback Font", "Fills in what it lacks, as Nerd Font icons.", func() {
			ui.Combobox(c.Key("fallback-font"), &a.fallbackFamily, a.allFonts).Label("Fallback Font").Width(220).
				OnChange(func() { a.setFont(&a.settings.FallbackFont, a.fallbackFamily, noFont) })
		})
		ui.Divider(c)
		settingsRow(c, "Font Size", "The terminals’ text size, in points.", func() {
			ui.Row(c).Gap(c.Theme().Space(1)).AlignItems(ui.Center).Children(func() { a.fontSizeControl(c) })
		})
	})
}

// fontSizeControl edits the terminals' font size, as AppKit's field and
// stepper do: the field takes a size typed in range at once, and Up and
// Down step it, as the stepper beside it does; it shows the size again in
// full as it loses the focus.
func (a *app) fontSizeControl(c *ui.Context) {
	format := func() { a.fontSizeText = strconv.FormatFloat(a.fontSize, 'f', -1, 64) }
	step := func(by float64) {
		a.fontSize = min(max(a.fontSize+by, minFontSize), maxFontSize)
		format()
		a.setFontSize(a.fontSize)
	}
	if !a.fontSizeInput.Focused(c) {
		format()
	}
	ui.TextInput(c.Key("font-size"), &a.fontSizeText).Bind(&a.fontSizeInput).Label("Font Size").Width(c.Theme().Space(12)).
		OnTextKey(func(k ui.TextKey) bool {
			switch {
			case k.Key == ui.KeyUp && k.Mods == 0:
				step(1)
			case k.Key == ui.KeyDown && k.Mods == 0:
				step(-1)
			default:
				return false
			}
			return true
		}).
		OnChange(func() {
			if v, err := strconv.ParseFloat(strings.TrimSpace(a.fontSizeText), 64); err == nil && v >= minFontSize && v <= maxFontSize {
				a.fontSize = v
				a.setFontSize(v)
			}
		})
	stepper := ui.Stepper(c.Key("font-size-stepper"), &a.fontSize, minFontSize, maxFontSize, 1).Label("Font Size").
		OnChange(func() { step(0) })
	// The stepper is as tall as the field, as laid out last, rather than
	// a little taller: its arrows shrink to fit.
	if h := a.fontSizeInput.Bounds(c).H; h > 0 {
		stepper.Height(h)
	}
}

// setFont sets one of the terminals' font families, the setting at field,
// to family, "" as it is the control's choice for none, none, paints the
// terminals in them, and saves the choice.
func (a *app) setFont(field *string, family, def string) {
	if family == def {
		family = ""
	}
	if *field == family {
		return
	}
	*field = family
	a.term.SetFont(a.settings.terminalFont())
	if err := a.settings.save(); err != nil {
		a.err = err.Error()
	}
}

// setFontSize sets the terminals' font size, paints the terminals in it,
// and saves the choice.
func (a *app) setFontSize(size float64) {
	if size == defaultFontSize {
		size = 0
	}
	if a.settings.FontSize == size {
		return
	}
	a.settings.FontSize = size
	a.term.SetFont(a.settings.terminalFont())
	if err := a.settings.save(); err != nil {
		a.err = err.Error()
	}
}

// setAppearance shows the app light, dark, or as the system appears, and
// saves the choice.
func (a *app) setAppearance(s mygo.ThemeSource) {
	if a.settings.Appearance == s {
		return
	}
	a.settings.Appearance = s
	setThemeSource(s)
	if err := a.settings.save(); err != nil {
		a.err = err.Error()
	}
}
