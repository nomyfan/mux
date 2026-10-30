package main

import (
	"cmp"
	_ "embed"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"

	"mux/internal/hc"
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
	// FontSmoothing draws the terminals' text thicker, with macOS's font
	// smoothing at its strongest, as Ghostty's font-thicken; without it,
	// as Ghostty does by default.
	FontSmoothing bool `json:"font_smoothing,omitempty"`
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
func (s settings) terminalFont() terminal.Font {
	families := cmp.Or(s.TerminalFont, "monospace")
	if s.FallbackFont != "" {
		families += ", " + s.FallbackFont
	}
	return terminal.Font{Family: families, Size: float32(cmp.Or(s.FontSize, defaultFontSize)), Thicken: s.FontSmoothing}
}

// previewFont is the terminals' font as the settings' preview draws it.
func (s settings) previewFont() ui.Font {
	f := s.terminalFont()
	return ui.Font{Family: f.Family, Size: f.Size, Antialiased: !f.Thicken}
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

// appearanceIcon is the appearance page's icon, drawn after System
// Settings': a ringed circle half light and half dark, around a smaller one
// the other way round, on the app icon's dark tile, as the other pages'
// icons are.
var appearanceIcon = ui.MustParseSVG(appearanceSVG)

//go:embed icons/terminal.svg
var terminalSVG []byte

// terminalIcon is the terminal page's icon: a prompt on a dark tile.
var terminalIcon = ui.MustParseSVG(terminalSVG)

// settingsPages are the settings' page ids and titles.
var settingsPages = map[string]string{"server": "Server", "appearance": "Appearance", "terminal": "Terminal"}

// pageItem is the settings sidebar's item choosing the page id, named
// label, with the page's icon in its own colors, larger than SidebarItem's
// line icons and as large as the System Settings' (20pt): the item widens
// its padding before its label for the icon, which lies in it. Add to the
// item with Children.
func (a *app) pageItem(c *ui.Context, id, label string, icon ui.ImageSource) ui.Element {
	t := c.Theme()
	const size = 20
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
		// Modal's look, without its padding, with a border. On a dark
		// ground the shadow hardly shows, so the backdrop darkens more.
		dim := float32(0.4)
		if t.Dark {
			dim = 0.6
		}
		backdrop.Background(ui.RGBA(0, 0, 0, dim))
		panel.Row().Border(1, t.Border).AlignItems(ui.Stretch).Width(min(720, w-t.Space(10))).Height(min(480, h-t.Space(10))).
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
				a.pageItem(c, "appearance", "Appearance", appearanceIcon)
				a.pageItem(c, "terminal", "Terminal", terminalIcon)
			}).Grow(1).Label("Settings")
		})
		ui.Box(c).Width(1).Shrink(0).Background(t.Border)
		// The page scrolls under its title, as the terminal page's preview
		// grows with the font.
		ui.Column(c).Grow(1).MinWidth(0).Padding(t.Space(4), 0, 0, 0).Gap(t.Space(4)).Children(func() {
			title := settingsPages[a.settingsPage]
			ui.Row(c).AlignItems(ui.Center).Shrink(0).Padding(0, t.Space(5)).Children(func() {
				ui.Text(c, title).FontSize(t.FontSize + 4).Bold().Grow(1)
				if iconButton(c, "close", closeIcon, "Close").Clicked() {
					a.settingsOpen = false
				}
			})
			ui.Scroll(c.Key(a.settingsPage)).Grow(1).Children(func() {
				ui.Column(c).Padding(0, t.Space(5), t.Space(5), t.Space(5)).Gap(t.Space(4)).Children(func() {
					switch a.settingsPage {
					case "server":
						a.serverSettings(c)
					case "appearance":
						a.appearanceSettings(c)
					case "terminal":
						a.terminalSettings(c)
					}
				})
			})
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

// serverSettings shows whether herdr-core runs, with a button starting or
// stopping it, and where the app finds it.
func (a *app) serverSettings(c *ui.Context) {
	t := c.Theme()
	settingsCard(c, func() {
		ui.Row(c).AlignItems(ui.Center).Gap(t.Space(3)).Padding(t.Space(2.5), 0).Children(func() {
			serverMark(c, 40)
			ui.Column(c).Grow(1).MinWidth(0).Gap(t.Space(0.5)).Children(func() {
				ui.Text(c, "herdr-core").Bold()
				ui.Text(c, a.serverStatus()).FontSize(t.FontSize - 1).TextColor(t.TextMuted)
			})
			a.serverButton(c)
		})
	})
	if a.serverErr != "" {
		ui.Text(c, a.serverErr).FontSize(12).TextColor(t.Danger)
	}
	settingsCard(c, func() {
		settingsRow(c, "Command", "The hc the app runs to connect to the core.", func() {})
		bin := a.bin
		if bin == "" {
			bin = "Not found. Install it with: " + hc.InstallCommand
		}
		ui.Text(c, bin).FontSize(t.FontSize-1).TextColor(t.TextMuted).Selectable().Padding(0, 0, t.Space(2.5), 0)
	})
}

var (
	//go:embed icons/appearance-auto.svg
	appearanceAutoSVG []byte
	//go:embed icons/appearance-light.svg
	appearanceLightSVG []byte
	//go:embed icons/appearance-dark.svg
	appearanceDarkSVG []byte
)

// appearancePictures are the appearance choices' pictures, in the order of
// appearances: the window drawn small in each, as System Settings draws
// macOS's, Auto's split along the diagonal as the README's screenshot is.
// scripts/make-appearance-previews.sh draws them.
var appearancePictures = [...]*ui.SVG{
	ui.MustParseSVG(appearanceAutoSVG), ui.MustParseSVG(appearanceLightSVG), ui.MustParseSVG(appearanceDarkSVG),
}

// appearanceSettings chooses the app's appearance among pictures of the
// window in each.
func (a *app) appearanceSettings(c *ui.Context) {
	t := c.Theme()
	settingsCard(c, func() {
		settingsRow(c, "Theme", "Auto follows the system’s appearance.", func() {})
		ui.RadioGroup(c.Key("appearance"), func() {
			for i, label := range []string{"Auto", "Light", "Dark"} {
				a.appearanceChoice(c, i, label)
			}
		}).Row().Gap(t.Space(5)).Padding(t.Space(1), 0, t.Space(4), 0).Label("Theme")
	})
}

// appearanceChoice is the choice of appearances[i], named label: its
// picture, ringed in the accent while chosen, over its name.
func (a *app) appearanceChoice(c *ui.Context, i int, label string) {
	t := c.Theme()
	chosen := a.appearance == i
	ring, name := ui.Color{}, t.TextMuted
	if chosen {
		ring, name = t.Accent, t.Text
	}
	ui.RadioBase(c.Key(label), &a.appearance, i).Column().AlignItems(ui.Center).Gap(t.Space(1.5)).Label(label).
		OnChange(func() { a.setAppearance(appearances[a.appearance]) }).
		Children(func() {
			// The ring stands off the picture, whose corners are 6 round.
			ui.Box(c).Padding(2).Radius(10).Border(2, ring).Children(func() {
				ui.Image(c, appearancePictures[i]).Size(120, 76)
			})
			ui.Text(c, label).FontSize(t.FontSize - 1).TextColor(name)
		})
}

// terminalSettings chooses the terminals' fonts and how they are drawn,
// under a preview of them.
func (a *app) terminalSettings(c *ui.Context) {
	settingsCard(c, func() {
		ui.Box(c).Shrink(0).Padding(c.Theme().Space(3), 0, c.Theme().Space(1.5), 0).Children(func() { a.fontPreview(c) })
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
		// Font smoothing is macOS's; elsewhere the switch would change nothing.
		if runtime.GOOS == "darwin" {
			ui.Divider(c)
			settingsRow(c, "Thicken Text", "Draws text heavier, as Ghostty’s font-thicken does.", func() {
				ui.Switch(c.Key("font-smoothing"), &a.settings.FontSmoothing).Label("Thicken Text").
					OnChange(a.applyFont)
			})
		}
	})
}

// previewRun is a stretch of the font preview's text, in a color of the
// terminals' palette (-1 for their foreground), bold or not.
type previewRun struct {
	text  string
	color int
	bold  bool
}

// previewText is the font preview's text, by line: a starship prompt, whose
// Nerd Font icons show the fallback font, and go test's report, its tabs
// laid out as spaces to the next tab stop, as terminals do.
var previewText = [][]previewRun{
	{{"mux", 6, true}, {" on ", -1, false}, {"\ue0a0 main", 5, true}, {" via ", -1, false},
		{"🐹 v1.27.1", 6, true}, {" via ", -1, false}, {"\ue718 v22.21.1", 2, true}},
	{{"➜ ", 2, true}, {"go test ./...", -1, false}},
	{{"ok      mux/cmd/mux     0.387s", -1, false}},
	{{"ok      mux/internal/hc     (cached)", -1, false}},
	{{"ok      mux/internal/term       0.412s", -1, false}},
}

// fontPreview shows the preview text in the terminals' font, drawn as the
// terminals draw it, on their background. It shapes the text again only
// as the font changes.
func (a *app) fontPreview(c *ui.Context) {
	th := c.Theme()
	font := a.settings.previewFont()
	if a.previewGlyphs == nil || a.previewFont != font {
		a.previewFont = font
		a.previewGlyphs = make([][][]ui.Glyph, len(previewText))
		for i, line := range previewText {
			for _, run := range line {
				f := font
				if run.bold {
					f.Weight = 700
				}
				a.previewGlyphs[i] = append(a.previewGlyphs[i], ui.Shape(run.text, f))
			}
		}
	}
	glyphs := a.previewGlyphs
	pal := lightTerminal
	if th.Dark {
		pal = darkTerminal
	}
	m := font.Metrics()
	lineH := float32(math.Ceil(float64(m.Ascent + m.Descent + m.LineGap)))
	baseline := float32(math.Round(float64(m.Ascent + m.LineGap/2)))
	pad := th.Space(3)
	radius := th.Space(1.5)
	ui.Box(c).Height(float32(len(glyphs))*lineH+2*pad).Radius(radius).Border(1, th.Border).
		Background(pal.Background).Draw(func(p *ui.Painter, r ui.Rect) {
		// Lines too wide for the box, in a large font, end at its border.
		inner := ui.Rect{X: r.X + 1, Y: r.Y + 1, W: r.W - 2, H: r.H - 2}
		p.Clip(inner, radius-1, func() {
			for i, line := range glyphs {
				x, y := r.X+pad, r.Y+pad+float32(i)*lineH+baseline
				for j, g := range line {
					color := pal.Foreground
					if k := previewText[i][j].color; k >= 0 {
						color = pal.Palette[k]
					}
					p.Glyphs(g, x, y, color)
					if n := len(g); n > 0 {
						x += g[n-1].X + g[n-1].Advance
					}
				}
			}
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
	a.applyFont()
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
	a.applyFont()
}

// applyFont paints the terminals in the font the settings set, and saves
// them.
func (a *app) applyFont() {
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
