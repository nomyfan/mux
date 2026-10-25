package endpoint

// InputEvent is what a client sends a pane: a Key, Text, Paste or Mouse.
type InputEvent interface{ encode(e *encoder) }

// Modifiers are the keys held with a key or a click.
type Modifiers uint8

const (
	ModShift Modifiers = 1 << iota
	ModCtrl
	ModAlt
	ModSuper
)

// KeyCode is a key that types no text of its own, or KeyChar with the
// character of a chord, as Ctrl+C.
type KeyCode uint8

const (
	KeyBackspace KeyCode = iota
	KeyEnter
	KeyLeft
	KeyRight
	KeyUp
	KeyDown
	KeyHome
	KeyEnd
	KeyPageUp
	KeyPageDown
	KeyTab
	KeyBackTab
	KeyDelete
	KeyInsert
	KeyEsc
	KeyChar
	KeyF
)

// Key is a press of a key: Char with KeyChar, and the number of a function
// key in F with KeyF.
type Key struct {
	Code KeyCode
	Char rune
	F    uint8
	Mods Modifiers
}

func (k Key) encode(e *encoder) {
	e.variant(0)
	e.variant(uint32(k.Code))
	switch k.Code {
	case KeyChar:
		e.char(k.Char)
	case KeyF:
		e.u8(k.F)
	}
	e.u8(uint8(k.Mods))
	e.variant(0)  // kind: press
	e.uint(1)     // repeat_count
	e.u8(0)       // shifted_codepoint: none
	e.u8(0)       // generated_text: none
	e.bool(false) // tracks_release
	e.u8(0)       // physical_key_id: none
	e.u8(0)       // windows_record: none
}

// Text is text typed, as an input method commits it.
type Text string

func (t Text) encode(e *encoder) {
	e.variant(1)
	e.string(string(t))
}

// Paste is text pasted, which the server brackets for a program that asks.
type Paste string

func (p Paste) encode(e *encoder) {
	e.variant(3)
	e.string(string(p))
}

// MouseKind is what a mouse event does, in the order of the wire's variants.
type MouseKind uint8

const (
	MouseDown MouseKind = iota
	MouseUp
	MouseDrag
	MouseMove
	ScrollUp
	ScrollDown
	ScrollLeft
	ScrollRight
)

type MouseButton uint8

const (
	ButtonLeft MouseButton = iota
	ButtonRight
	ButtonMiddle
)

// Mouse is a mouse event at a cell of the surface. Button is for MouseDown,
// MouseUp and MouseDrag; Lines for the scrolls.
type Mouse struct {
	Kind        MouseKind
	Button      MouseButton
	Column, Row uint16
	Mods        Modifiers
	Lines       uint16
}

func (m Mouse) encode(e *encoder) {
	e.variant(2)
	e.variant(uint32(m.Kind))
	if m.Kind <= MouseDrag {
		e.variant(uint32(m.Button))
	}
	e.variant(0) // position: a cell
	e.uint(uint64(m.Column))
	e.uint(uint64(m.Row))
	e.u8(0) // geometry: none
	e.u8(uint8(m.Mods))
	e.uint(uint64(m.Lines))
}
