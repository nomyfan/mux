package term

import (
	"fmt"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

// search is the text the user searches a pane's terminal for, scrollback
// and all, as the terminal finds it. Each pane has its own.
type search struct {
	open   bool
	query  string
	field  ui.Handle
	bar    ui.Rect // where the bar showed, in the view
	status terminal.SearchStatus
}

// overSearch reports whether x, y in the view is on a search's bar, whose
// controls take the pointer there.
func (v *View) overSearch(x, y float32) bool {
	for _, b := range v.boxes {
		if p := v.panes[b.terminal]; p != nil && p.search.open && p.search.bar.Contains(x, y) {
			return true
		}
	}
	return false
}

// openSearch opens the focused pane's search, or gives its field the
// keyboard again.
func (v *View) openSearch(c *ui.Context) {
	if v.tab == nil {
		return
	}
	p := v.panes[v.tab.Focus]
	if p == nil {
		return
	}
	if !p.search.open {
		p.search.open = true
		if p.search.query != "" {
			p.term.Search(p.search.query)
		}
	}
	p.search.field.Focus(c)
}

// closeSearch hides a pane's search and its matches, giving the keyboard
// back to the terminal.
func (v *View) closeSearch(p *pane) {
	p.search.open = false
	p.term.EndSearch()
	v.refocus = true
}

// searchBar shows a pane's search at the top right of its card: its field,
// how many matches it found, and buttons going from one to the next. Enter
// goes up the text to the match before, Shift+Enter down, and Escape
// closes it.
func (v *View) searchBar(c *ui.Context, b paneBox, p *pane) {
	t, s := c.Theme(), &p.search
	w := min(320, b.card.W-16)
	s.bar = ui.Rect{X: b.card.X + b.card.W - w - 8, Y: b.header.Y + b.header.H + 4, W: w, H: 32}
	bar := ui.Row(c.Key(b.terminal+" search")).Absolute().Left(s.bar.X).Top(s.bar.Y).Size(s.bar.W, s.bar.H).
		AlignItems(ui.Center).Gap(2).Padding(0, 4, 0, 8).Background(t.Surface).Radius(8).Border(1, t.Border).
		Shadow(0, 4, 12, 0, ui.RGB(0, 0, 0).Alpha(0.12))
	bar.Children(func() {
		ui.Icon(c, searchIcon).Size(14, 14).TextColor(t.TextMuted).Shrink(0)
		ui.TextInput(c.Key("query"), &s.query).Bind(&s.field).AutoFocus().Placeholder("Find").Label("Find").
			Grow(1).MinWidth(0).Padding(0, 4).Background(ui.Transparent).Border(0, ui.Transparent).
			OnChange(func() { p.term.Search(s.query) }).
			OnTextKey(func(k ui.TextKey) bool {
				switch {
				case k.Key == ui.KeyEnter && k.Mods == 0:
					p.term.SearchNext()
				case k.Key == ui.KeyEnter && k.Mods == ui.Shift:
					p.term.SearchPrevious()
				case k.Key == ui.KeyEscape && k.Mods == 0:
					v.closeSearch(p)
				default:
					return false
				}
				return true
			})
		if s.query != "" {
			if searchButton(c, "clear", clearIcon, "Clear") {
				s.query = ""
				p.term.Search("")
				s.field.Focus(c)
			}
		}
		count := ""
		switch st := s.status; {
		case s.query == "":
		case st.Total == 0 && !st.Searching:
			count = "No results"
		case st.Selected > 0:
			count = fmt.Sprintf("%d of %d", st.Selected, st.Total)
		default:
			count = fmt.Sprintf("%d", st.Total)
		}
		ui.Text(c, count).FontSize(12).TextColor(t.TextMuted).SingleLine().Shrink(0)
		if searchButton(c, "previous", upIcon, "Previous Match") {
			p.term.SearchNext()
		}
		if searchButton(c, "next", downIcon, "Next Match") {
			p.term.SearchPrevious()
		}
		if searchButton(c, "close", closeIcon, "Close") {
			v.closeSearch(p)
		}
	})
}

// searchButton is an icon button of the search bar.
func searchButton(c *ui.Context, key string, icon *ui.SVG, label string) bool {
	t := c.Theme()
	b := ui.ButtonBase(c.Key(key)).Size(24, 24).Shrink(0).Radius(6).Center().TextColor(t.TextMuted).Label(label).Tooltip(label)
	if b.Pressed() {
		b.Background(t.Text.Alpha(0.14))
	} else if b.Hovered() {
		b.Background(t.Text.Alpha(0.08)).TextColor(t.Text)
	}
	b.Children(func() { ui.Icon(c, icon).Size(14, 14) })
	return b.Clicked()
}

func icon(shapes string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round">` + shapes + `</svg>`))
}

var (
	searchIcon = icon(`<circle cx="7" cy="7" r="4.5"/><path d="M10.5 10.5l3 3"/>`)
	clearIcon  = icon(`<circle cx="8" cy="8" r="6"/><path d="M6 6l4 4M10 6l-4 4"/>`)
	upIcon     = icon(`<path d="M4 10l4-4 4 4"/>`)
	downIcon   = icon(`<path d="M4 6l4 4 4-4"/>`)
	closeIcon  = icon(`<path d="M4 4l8 8M12 4l-8 8"/>`)
)
