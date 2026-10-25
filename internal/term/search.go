package term

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"

	"mux/internal/herdr/endpoint"
)

// search is the text the user searches a pane's text for, history and
// all, as the server finds it. Each pane has its own.
type search struct {
	open  bool
	query string
	field ui.Handle
	bar   ui.Rect // where the bar showed, in the view
	// matches are the matches the server sent, current the one the user
	// is at in them, -1 for none, which is index of total, all told.
	matches []textRange
	current int
	index   int
	total   int
	asked   int // counts searches: a response to an earlier one is stale
	// revision is the pane's content revision the matches were found or
	// last checked in; syncing marks a search finding them anew.
	revision uint64
	syncing  bool
}

// searchResult is the server's response to pane.copy_search.
type searchResult struct {
	ContentRevision uint64      `json:"content_revision"`
	Matches         []textRange `json:"matches"`
	Current         *int        `json:"current"`
	CurrentGlobal   *int        `json:"current_global"`
	Total           int         `json:"total"`
}

// searchOf returns a pane's search, which it makes the first time.
func (v *View) searchOf(paneID string) *search {
	s := v.searches[paneID]
	if s == nil {
		s = &search{current: -1}
		v.searches[paneID] = s
	}
	return s
}

// staleTries is how many times a search goes again as the pane's text
// changes under it, which the server refuses (stale_content).
const staleTries = 3

// find searches a pane for its search's query, from the match the user is
// at, if any, else from the bottom of the screen, up its text ("backward")
// or down ("forward"), showing the match it comes to.
func (v *View) find(paneID, direction string) { v.findTry(paneID, direction, staleTries) }

func (v *View) findTry(paneID, direction string, tries int) {
	s := v.searchOf(paneID)
	s.asked++
	p, ok := v.pane(paneID)
	if !ok || s.query == "" {
		s.matches, s.current, s.total = nil, -1, 0
		return
	}
	params := map[string]any{
		"pane_id": p.ID, "query": s.query, "direction": direction, "content_revision": p.ContentRevision,
		"cursor": point{Row: top(p) + int(p.Inner.Height)},
	}
	if s.current >= 0 {
		params["cursor"], params["previous"] = s.matches[s.current].Start, s.matches[s.current]
	}
	asked := s.asked
	v.hooks.Request("pane.copy_search", params, func(result json.RawMessage, err error) {
		if asked != s.asked {
			return
		}
		if err != nil && strings.Contains(err.Error(), "stale_content") && tries > 1 {
			v.findTry(paneID, direction, tries-1)
			return
		}
		s.syncing = false
		var r searchResult
		if err != nil || json.Unmarshal(result, &r) != nil {
			return
		}
		s.matches, s.total, s.current, s.index, s.revision = r.Matches, r.Total, -1, 0, r.ContentRevision
		if r.Current != nil && *r.Current < len(r.Matches) {
			s.current, s.index = *r.Current, *r.Current
			if r.CurrentGlobal != nil {
				s.index = *r.CurrentGlobal
			}
			v.reveal(paneID, s.matches[s.current])
		}
	})
}

// step goes to the match before ("backward") or after ("forward") the one
// the user is at, among those the server sent, round from one end to the
// other when it sent them all: the server finds each in the pane's whole
// history, which takes long in a long one. It asks the server for those
// beyond the ones it sent.
func (v *View) step(paneID, direction string) {
	s := v.searchOf(paneID)
	if s.current < 0 {
		v.find(paneID, direction)
		return
	}
	if p, ok := v.pane(paneID); !ok {
		return
	} else if fresh, _ := v.verify(p, s); !fresh {
		v.resync(paneID)
		return
	}
	n, d := len(s.matches), 1
	if direction == "backward" {
		d = -1
	}
	next := s.current + d
	switch {
	case next >= 0 && next < n:
		s.current, s.index = next, s.index+d
	case n == s.total:
		s.current = (next + n) % n
		s.index = s.current
	default:
		v.find(paneID, direction)
		return
	}
	s.asked++ // a search still coming is stale
	v.reveal(paneID, s.matches[s.current])
}

// verify reports whether a search's matches are still where the server
// found them in the pane's text, and whether it is sure: they all move up
// its rows at once as the pane drops the oldest of its history. Since the
// text changed, it reads the first match the screen shows for the query;
// with none to read, it is not sure, and says they moved.
func (v *View) verify(p endpoint.SurfacePane, s *search) (fresh, sure bool) {
	if p.ContentRevision == s.revision {
		return true, true
	}
	t := top(p)
	for _, m := range s.matches {
		if m.Start.Row < t || m.End.Row >= t+int(p.Inner.Height) {
			continue
		}
		text, _ := screenText(&v.surface.Frame, p, m.Start, m.End)
		if !strings.EqualFold(text, strings.TrimRight(s.query, " ")) {
			return false, true
		}
		s.revision = p.ContentRevision
		return true, true
	}
	return false, false
}

// resync searches a pane anew from the bottom of its screen, as its
// matches moved, hiding them meanwhile.
func (v *View) resync(paneID string) {
	s := v.searchOf(paneID)
	if s.syncing {
		return
	}
	s.syncing, s.matches, s.current = true, nil, -1
	v.find(paneID, "backward")
}

// newQuery searches a pane for its query anew, from the bottom of the
// screen.
func (v *View) newQuery(paneID string) {
	v.searchOf(paneID).current = -1
	v.find(paneID, "backward")
}

// scrolling is a pane's scroll the view asked for: the offset it asked
// for last, and the one it will ask for once the server is done.
type scrolling struct {
	asked int
	next  int // -1 for none
}

// reveal scrolls a pane to show a match in the middle of its screen, if
// the screen does not show it, or will not once it scrolls as the view
// asked: a scroll waits for the one before, and only the last of those
// waiting is asked for.
func (v *View) reveal(paneID string, m textRange) {
	p, ok := v.pane(paneID)
	if !ok || p.Scroll == nil {
		return
	}
	offset := int(p.Scroll.Offset)
	sc := v.scrolls[paneID]
	if sc != nil {
		offset = sc.asked
		if sc.next >= 0 {
			offset = sc.next
		}
	}
	limit, rows := int(p.Scroll.Max), int(p.Inner.Height)
	if t := limit - offset; m.Start.Row >= t && m.Start.Row < t+rows {
		return
	}
	offset = min(max(limit-m.Start.Row+rows/2, 0), limit)
	if sc != nil {
		sc.next = offset
		return
	}
	v.scrollTo(paneID, offset)
}

// scrollTo asks the server to scroll a pane, then for the scroll asked for
// since, if any.
func (v *View) scrollTo(paneID string, offset int) {
	sc := &scrolling{asked: offset, next: -1}
	v.scrolls[paneID] = sc
	v.hooks.Request("pane.scroll", map[string]any{"pane_id": paneID, "offset_from_bottom": offset}, func(json.RawMessage, error) {
		delete(v.scrolls, paneID)
		if sc.next >= 0 && sc.next != sc.asked {
			v.scrollTo(paneID, sc.next)
		}
	})
}

// pane returns the pane of the surface with the id.
func (v *View) pane(id string) (endpoint.SurfacePane, bool) {
	if v.surface != nil {
		for _, p := range v.surface.Panes {
			if p.ID == id {
				return p, true
			}
		}
	}
	return endpoint.SurfacePane{}, false
}

// focusedPane returns the pane that has the focus in the surface.
func (v *View) focusedPane() (endpoint.SurfacePane, bool) {
	if v.surface != nil {
		for _, p := range v.surface.Panes {
			if p.Focused {
				return p, true
			}
		}
	}
	return endpoint.SurfacePane{}, false
}

// overSearch reports whether x, y in the view is on a search's bar, whose
// controls take the pointer there.
func (v *View) overSearch(x, y float32) bool {
	for _, b := range v.boxes() {
		if s := v.searches[b.pane.ID]; s != nil && s.open && s.bar.Contains(x, y) {
			return true
		}
	}
	return false
}

// searchBar shows a pane's search at the top right of its card: its field,
// how many matches it found, and buttons going from one to the next. Enter
// goes up the text to the match before, Shift+Enter down, and Escape
// closes it.
func (v *View) searchBar(c *ui.Context, b paneBox, s *search) {
	t, id := c.Theme(), b.pane.ID
	w := min(320, b.card.W-16)
	s.bar = ui.Rect{X: b.card.X + b.card.W - w - 8, Y: b.header.Y + b.header.H + 4, W: w, H: 32}
	bar := ui.Row(c.Key(id+" search")).Absolute().Left(s.bar.X).Top(s.bar.Y).Size(s.bar.W, s.bar.H).
		AlignItems(ui.Center).Gap(2).Padding(0, 4, 0, 8).Background(t.Surface).Radius(8).Border(1, t.Border).
		Shadow(0, 4, 12, 0, ui.RGB(0, 0, 0).Alpha(0.12))
	bar.Children(func() {
		ui.Icon(c, searchIcon).Size(14, 14).TextColor(t.TextMuted).Shrink(0)
		ui.TextInput(c.Key("query"), &s.query).Bind(&s.field).AutoFocus().Placeholder("Find").Label("Find").
			Grow(1).MinWidth(0).Padding(0, 4).Background(ui.Transparent).Border(0, ui.Transparent).
			OnChange(func() { v.newQuery(id) }).
			OnTextKey(func(k ui.TextKey) bool {
				switch {
				case k.Key == ui.KeyEnter && k.Mods == 0:
					v.step(id, "backward")
				case k.Key == ui.KeyEnter && k.Mods == ui.Shift:
					v.step(id, "forward")
				case k.Key == ui.KeyEscape && k.Mods == 0:
					v.closeSearch(id)
				default:
					return false
				}
				return true
			})
		if s.query != "" {
			if searchButton(c, "clear", clearIcon, "Clear") {
				s.query = ""
				v.newQuery(id)
				s.field.Focus(c)
			}
		}
		count := "No results"
		switch {
		case s.query == "":
			count = ""
		case s.current >= 0:
			count = fmt.Sprintf("%d of %d", s.index+1, s.total)
		}
		ui.Text(c, count).FontSize(12).TextColor(t.TextMuted).SingleLine().Shrink(0)
		if searchButton(c, "previous", upIcon, "Previous Match") {
			v.step(id, "backward")
		}
		if searchButton(c, "next", downIcon, "Next Match") {
			v.step(id, "forward")
		}
		if searchButton(c, "close", closeIcon, "Close") {
			v.closeSearch(id)
		}
	})
}

// openSearch shows the focused pane's search, and takes the keyboard to
// its field.
func (v *View) openSearch(c *ui.Context) {
	p, ok := v.focusedPane()
	if !ok {
		return
	}
	s := v.searchOf(p.ID)
	if !s.open {
		s.open = true
		if s.query != "" {
			v.newQuery(p.ID)
		}
	}
	s.field.Focus(c)
}

// closeSearch hides a pane's search and its matches, giving the keyboard
// back to the terminal.
func (v *View) closeSearch(paneID string) {
	s := v.searchOf(paneID)
	s.open, s.matches, s.current = false, nil, -1
	s.asked++
	v.refocus = true
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
