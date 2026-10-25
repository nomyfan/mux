package term

import (
	"github.com/egoist/mygo/ui"

	"mux/internal/herdr/endpoint"
)

// specialKeys are the keys that type no text, which go to the pane as keys.
var specialKeys = map[ui.Key]endpoint.KeyCode{
	ui.KeyEnter:     endpoint.KeyEnter,
	ui.KeyEscape:    endpoint.KeyEsc,
	ui.KeyBackspace: endpoint.KeyBackspace,
	ui.KeyTab:       endpoint.KeyTab,
	ui.KeyDelete:    endpoint.KeyDelete,
	ui.KeyInsert:    endpoint.KeyInsert,
	ui.KeyHome:      endpoint.KeyHome,
	ui.KeyEnd:       endpoint.KeyEnd,
	ui.KeyPageUp:    endpoint.KeyPageUp,
	ui.KeyPageDown:  endpoint.KeyPageDown,
	ui.KeyLeft:      endpoint.KeyLeft,
	ui.KeyRight:     endpoint.KeyRight,
	ui.KeyUp:        endpoint.KeyUp,
	ui.KeyDown:      endpoint.KeyDown,
}

var functionKeys = map[ui.Key]uint8{
	ui.KeyF1: 1, ui.KeyF2: 2, ui.KeyF3: 3, ui.KeyF4: 4, ui.KeyF5: 5, ui.KeyF6: 6,
	ui.KeyF7: 7, ui.KeyF8: 8, ui.KeyF9: 9, ui.KeyF10: 10, ui.KeyF11: 11, ui.KeyF12: 12,
}

// keyRunes are the characters of the keys that type text, unshifted on a
// US layout, for the chords that send them as keys, as Ctrl+C.
var keyRunes = map[ui.Key]rune{
	ui.KeyA: 'a', ui.KeyB: 'b', ui.KeyC: 'c', ui.KeyD: 'd', ui.KeyE: 'e', ui.KeyF: 'f', ui.KeyG: 'g',
	ui.KeyH: 'h', ui.KeyI: 'i', ui.KeyJ: 'j', ui.KeyK: 'k', ui.KeyL: 'l', ui.KeyM: 'm', ui.KeyN: 'n',
	ui.KeyO: 'o', ui.KeyP: 'p', ui.KeyQ: 'q', ui.KeyR: 'r', ui.KeyS: 's', ui.KeyT: 't', ui.KeyU: 'u',
	ui.KeyV: 'v', ui.KeyW: 'w', ui.KeyX: 'x', ui.KeyY: 'y', ui.KeyZ: 'z',
	ui.Key0: '0', ui.Key1: '1', ui.Key2: '2', ui.Key3: '3', ui.Key4: '4',
	ui.Key5: '5', ui.Key6: '6', ui.Key7: '7', ui.Key8: '8', ui.Key9: '9',
	ui.KeySpace: ' ', ui.KeyMinus: '-', ui.KeyEqual: '=', ui.KeyComma: ',', ui.KeyPeriod: '.',
	ui.KeySlash: '/', ui.KeySemicolon: ';', ui.KeyQuote: '\'', ui.KeyBracketLeft: '[',
	ui.KeyBracketRight: ']', ui.KeyBackslash: '\\', ui.KeyBackquote: '`',
}

func modifiers(m ui.Modifiers) endpoint.Modifiers {
	var out endpoint.Modifiers
	if m&ui.Shift != 0 {
		out |= endpoint.ModShift
	}
	if m&ui.Ctrl != 0 {
		out |= endpoint.ModCtrl
	}
	if m&ui.Alt != 0 {
		out |= endpoint.ModAlt
	}
	return out
}

// keyEvent returns the key a press sends the pane, if it sends one rather
// than the text it types, which comes after it as an InputText. Command
// chords are the app's shortcuts; Option types characters on macOS, so
// only Control makes a chord of a key that types text.
func keyEvent(k ui.Key, m ui.Modifiers) (endpoint.Key, bool) {
	if m&ui.Super != 0 {
		return endpoint.Key{}, false
	}
	// Option+Left/Right move by word, sent as Esc b / Esc f as Ghostty does,
	// since shells bind those and not the arrows' Alt sequences.
	if m == ui.Alt {
		switch k {
		case ui.KeyLeft:
			return endpoint.Key{Code: endpoint.KeyChar, Char: 'b', Mods: endpoint.ModAlt}, true
		case ui.KeyRight:
			return endpoint.Key{Code: endpoint.KeyChar, Char: 'f', Mods: endpoint.ModAlt}, true
		}
	}
	mods := modifiers(m)
	if code, ok := specialKeys[k]; ok {
		if code == endpoint.KeyTab && m&ui.Shift != 0 {
			code = endpoint.KeyBackTab
		}
		return endpoint.Key{Code: code, Mods: mods}, true
	}
	if f, ok := functionKeys[k]; ok {
		return endpoint.Key{Code: endpoint.KeyF, F: f, Mods: mods}, true
	}
	if r, ok := keyRunes[k]; ok && m&ui.Ctrl != 0 {
		return endpoint.Key{Code: endpoint.KeyChar, Char: r, Mods: mods}, true
	}
	return endpoint.Key{}, false
}
