package term

import (
	"cmp"
	"strings"

	"mux/src/herdr/endpoint"
)

// point is a cell of a pane's text: Row counts from the first row of its
// history, Col from the pane's left, as the server's requests on the text
// take them.
type point struct {
	Row int `json:"row"`
	Col int `json:"col"`
}

func (a point) before(b point) bool { return a.Row < b.Row || a.Row == b.Row && a.Col < b.Col }

// textRange is a pane's text from Start to End, both in it.
type textRange struct {
	Start point `json:"start"`
	End   point `json:"end"`
}

// top returns the row of a pane's text that its screen shows first.
func top(p endpoint.SurfacePane) int {
	if p.Scroll == nil {
		return 0
	}
	return int(p.Scroll.Max) - int(p.Scroll.Offset)
}

// pointAt returns the point of pane p that cell x, y of the surface shows.
func pointAt(p endpoint.SurfacePane, x, y int) point {
	return point{Row: top(p) + y - int(p.Inner.Y), Col: x - int(p.Inner.X)}
}

// selection is text the user selects in a pane, which stays with the text
// as it scrolls: from what the press began on to what the pointer is on, a
// cell, or a word or line after a double or triple click.
type selection struct {
	pane string
	unit int // 1 cells, 2 words, 3 lines
	// anchor and head are the first and last points of what the press
	// began on and of what the pointer is on.
	anchor, head [2]point
}

// span returns the first and last points the selection takes, and whether
// it takes any: a click that stays in its cell takes none.
func (s *selection) span() (from, to point, ok bool) {
	if s.unit == 1 && s.anchor == s.head {
		return point{}, point{}, false
	}
	if s.head[0].before(s.anchor[0]) {
		return s.head[0], s.anchor[1], true
	}
	return s.anchor[0], s.head[1], true
}

// wordBreaks end the words a double click selects, as Ghostty's
// selection-word-chars.
const wordBreaks = " \t'\"│`|:;,()[]{}<>$"

// extent returns the first and last points of the cell, word or line (by
// unit) at cell x, y of the frame, in pane p. A wide character's cells go
// together.
func extent(f *endpoint.Frame, p endpoint.SurfacePane, x, y, unit int) [2]point {
	left, right := int(p.Inner.X), int(p.Inner.X+p.Inner.Width)-1
	symbol := func(x int) string { return f.Cells[y*int(f.Width)+x].Symbol }
	// A wide character's second cell is blank.
	covered := func(x int) bool { return x > left && wide(symbol(x-1)) }
	breaks := func(x int) bool {
		s := symbol(x)
		return !covered(x) && (s == "" || strings.Contains(wordBreaks, s))
	}
	x0, x1 := x, x
	switch unit {
	case 1:
	case 2:
		if !breaks(x) {
			for x0 > left && !breaks(x0-1) {
				x0--
			}
			for x1 < right && !breaks(x1+1) {
				x1++
			}
		}
	default:
		x0, x1 = left, right
	}
	if covered(x0) {
		x0--
	}
	if x1 < right && wide(symbol(x1)) {
		x1++
	}
	return [2]point{pointAt(p, x0, y), pointAt(p, x1, y)}
}

// cols returns the columns x0 to x1 of row that the text from to to takes,
// in a pane width columns wide.
func cols(from, to point, width, row int) (x0, x1 int, ok bool) {
	if row < from.Row || row > to.Row {
		return 0, 0, false
	}
	x0, x1 = 0, width-1
	if row == from.Row {
		x0 = from.Col
	}
	if row == to.Row {
		x1 = to.Col
	}
	return x0, x1, x0 <= x1
}

// screenText returns the text from to to, without the blanks that end its
// rows, if the frame shows all of it in pane p.
func screenText(f *endpoint.Frame, p endpoint.SurfacePane, from, to point) (string, bool) {
	t, in := top(p), p.Inner
	if from.Row < t || to.Row >= t+int(in.Height) {
		return "", false
	}
	var lines []string
	for row := from.Row; row <= to.Row; row++ {
		x0, x1, _ := cols(from, to, int(in.Width), row)
		start := (row-t+int(in.Y))*int(f.Width) + int(in.X)
		var line strings.Builder
		covered := false
		for _, c := range f.Cells[start+x0 : start+x1+1] {
			if covered {
				covered = false
				continue
			}
			covered = wide(c.Symbol)
			line.WriteString(cmp.Or(c.Symbol, " "))
		}
		lines = append(lines, strings.TrimRight(line.String(), " "))
	}
	return strings.Join(lines, "\n"), true
}
