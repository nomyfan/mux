package layout

import "math"

// Pane returns the pane of a terminal in the tab, nil if none.
func (t *Tab) Pane(terminal string) *Node {
	if terminal == "" {
		return nil
	}
	for _, n := range t.Leaves() {
		if n.Terminal == terminal {
			return n
		}
	}
	return nil
}

// Leaves returns the tab's panes, left to right and top to bottom as the
// tree has them.
func (t *Tab) Leaves() []*Node {
	var out []*Node
	var walk func(n *Node)
	walk = func(n *Node) {
		switch {
		case n == nil:
		case n.Leaf():
			out = append(out, n)
		default:
			walk(n.A)
			walk(n.B)
		}
	}
	walk(t.Root)
	return out
}

// parentOf returns the split holding a node directly, nil for the root.
func (t *Tab) parentOf(child *Node) *Node {
	var find func(n *Node) *Node
	find = func(n *Node) *Node {
		if n == nil || n.Leaf() {
			return nil
		}
		if n.A == child || n.B == child {
			return n
		}
		if p := find(n.A); p != nil {
			return p
		}
		return find(n.B)
	}
	return find(t.Root)
}

// remove removes a terminal's pane, reporting whether the tab is left
// with none.
func (t *Tab) remove(terminal string) bool {
	n := t.Pane(terminal)
	if n == nil {
		return t.Root == nil
	}
	p := t.parentOf(n)
	if p == nil {
		t.Root = nil
		return true
	}
	other := p.A
	if other == n {
		other = p.B
	}
	*p = *other
	return false
}

// Direction is where a new pane goes beside another.
type Direction int

const (
	Right Direction = iota
	Down
	Left
	Up
)

// Split puts a pane showing terminal beside the pane of target, the two
// sharing its room evenly, and focuses it.
func (t *Tab) Split(target, terminal string, d Direction) bool {
	n := t.Pane(target)
	if n == nil {
		return false
	}
	old, added := &Node{Terminal: n.Terminal, Name: n.Name}, &Node{Terminal: terminal}
	*n = Node{Vertical: d == Down || d == Up, Ratio: 0.5, A: old, B: added}
	if d == Left || d == Up {
		n.A, n.B = added, old
	}
	t.Focus = terminal
	return true
}

// Swap swaps the places of two terminals' panes, names and all.
func (t *Tab) Swap(a, b string) bool {
	na, nb := t.Pane(a), t.Pane(b)
	if na == nil || nb == nil || na == nb {
		return false
	}
	*na, *nb = *nb, *na
	return true
}

// Move moves a terminal's pane beside the pane of target, the two sharing
// its room.
func (t *Tab) Move(terminal, target string, d Direction) bool {
	n := t.Pane(terminal)
	if n == nil || terminal == target || t.Pane(target) == nil {
		return false
	}
	name := n.Name
	t.remove(terminal)
	t.Split(target, terminal, d)
	t.Pane(terminal).Name = name
	return true
}

// Rect is a box, in any unit.
type Rect struct{ X, Y, W, H float32 }

// Contains reports whether x, y is in the box.
func (r Rect) Contains(x, y float32) bool { return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H }

// Box is where a pane shows.
type Box struct {
	Terminal string
	Rect     Rect
}

// Divider is the line between a split's two sides: its split, the room
// the split shares, and the line.
type Divider struct {
	Split *Node
	Area  Rect
	Line  Rect
}

// Boxes lays the tab's panes out in r, a gap wide line between the sides
// of each split: only the pane of zoomed, if any, while the tab is zoomed.
func (t *Tab) Boxes(r Rect, gap float32) ([]Box, []Divider) {
	if t.Zoomed {
		if n := t.Pane(t.Focus); n != nil {
			return []Box{{n.Terminal, r}}, nil
		}
	}
	var boxes []Box
	var dividers []Divider
	var lay func(n *Node, r Rect)
	lay = func(n *Node, r Rect) {
		if n == nil {
			return
		}
		if n.Leaf() {
			boxes = append(boxes, Box{n.Terminal, r})
			return
		}
		a, b, line := cut(r, n.Vertical, n.Ratio, gap)
		dividers = append(dividers, Divider{Split: n, Area: r, Line: line})
		lay(n.A, a)
		lay(n.B, b)
	}
	lay(t.Root, r)
	return boxes, dividers
}

// cut cuts r in two at ratio, across or down, a gap between.
func cut(r Rect, vertical bool, ratio float64, gap float32) (a, b, line Rect) {
	if vertical {
		h := float32(math.Round(float64(r.H-gap) * ratio))
		a = Rect{r.X, r.Y, r.W, h}
		line = Rect{r.X, r.Y + h, r.W, gap}
		b = Rect{r.X, r.Y + h + gap, r.W, r.H - h - gap}
		return
	}
	w := float32(math.Round(float64(r.W-gap) * ratio))
	a = Rect{r.X, r.Y, w, r.H}
	line = Rect{r.X + w, r.Y, gap, r.H}
	b = Rect{r.X + w + gap, r.Y, r.W - w - gap, r.H}
	return
}

// Neighbor returns the terminal of the pane beside a terminal's toward d
// among boxes: the nearest one overlapping it across, "" if none.
func Neighbor(boxes []Box, terminal string, d Direction) string {
	var from Rect
	found := false
	for _, b := range boxes {
		if b.Terminal == terminal {
			from, found = b.Rect, true
		}
	}
	if !found {
		return ""
	}
	best, bestDist := "", float32(math.MaxFloat32)
	for _, b := range boxes {
		r := b.Rect
		var dist, overlap float32
		switch d {
		case Right:
			dist, overlap = r.X-(from.X+from.W), span(from.Y, from.H, r.Y, r.H)
		case Left:
			dist, overlap = from.X-(r.X+r.W), span(from.Y, from.H, r.Y, r.H)
		case Down:
			dist, overlap = r.Y-(from.Y+from.H), span(from.X, from.W, r.X, r.W)
		case Up:
			dist, overlap = from.Y-(r.Y+r.H), span(from.X, from.W, r.X, r.W)
		}
		if b.Terminal == terminal || dist < 0 || overlap <= 0 {
			continue
		}
		// Among those as near, the one overlapping most.
		if score := dist - overlap/1e6; score < bestDist {
			best, bestDist = b.Terminal, score
		}
	}
	return best
}

// span is how much two stretches overlap.
func span(a, aLen, b, bLen float32) float32 {
	return min(a+aLen, b+bLen) - max(a, b)
}
