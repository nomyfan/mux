package endpoint

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"testing"
)

// The expected bytes are herdr-gpui's compatibility fixtures, which pin
// herdr's wire layout (crates/herdr-protocol/tests).
func TestEncodeFixtures(t *testing.T) {
	e := &encoder{}
	e.variant(clientShellResize)
	e.uint(8)
	e.uint(16)
	e.uint(80)
	e.uint(24)
	e.bool(false)
	if want := []byte{12, 8, 16, 80, 24, 0}; !bytes.Equal(e.buf, want) {
		t.Errorf("resize %v, want %v", e.buf, want)
	}

	e = &encoder{}
	e.variant(clientShellRequest)
	e.string("b")
	e.string("{}")
	if want := []byte{15, 1, 'b', 2, '{', '}'}; !bytes.Equal(e.buf, want) {
		t.Errorf("request %v, want %v", e.buf, want)
	}

	e = &encoder{}
	Key{Code: KeyChar, Char: 'é', Mods: ModCtrl | ModAlt}.encode(e)
	// Key, Char('é') in UTF-8, modifiers, press, repeat 1, four Nones and false.
	if want := []byte{0, 15, 0xc3, 0xa9, 6, 0, 1, 0, 0, 0, 0, 0}; !bytes.Equal(e.buf, want) {
		t.Errorf("key %v, want %v", e.buf, want)
	}
}

// SetHostTheme's updates, against herdr-gpui's host theme fixtures.
func TestHostTheme(t *testing.T) {
	server, client := net.Pipe()
	c := &Conn{conn: client}
	theme := HostTheme{Foreground: RGB{0x2e, 0x34, 0x40}, Background: RGB{1, 2, 3}, Dark: true}
	theme.Palette[4] = RGB{0x2e, 0x34, 0x40}
	go func() { c.SetHostTheme(theme); client.Close() }()
	var got [][]byte
	r := bufio.NewReader(server)
	for {
		var prefix [4]byte
		if _, err := io.ReadFull(r, prefix[:]); err != nil {
			break
		}
		payload := make([]byte, binary.LittleEndian.Uint32(prefix[:]))
		io.ReadFull(r, payload)
		got = append(got, payload)
	}
	if len(got) != 4 {
		t.Fatalf("%d updates, want 4", len(got))
	}
	for i, want := range [][]byte{{17, 0, 0, 0x2e, 0x34, 0x40}, {17, 0, 1, 1, 2, 3}, nil, {17, 2, 0}} {
		if want != nil && !bytes.Equal(got[i], want) {
			t.Errorf("update %d: %v, want %v", i, got[i], want)
		}
	}
	// The palette: 256 entries, index then color, the fifth the one set.
	if p := got[2]; !bytes.Equal(p[:5], []byte{17, 1, 251, 0, 1}) || !bytes.Equal(p[5+4*4:5+5*4], []byte{4, 0x2e, 0x34, 0x40}) {
		t.Errorf("palette update starts %v", p[:5+5*4])
	}
}

func TestVarint(t *testing.T) {
	for _, v := range []uint64{0, 250, 251, 65535, 65536, 1 << 32, 1<<64 - 1} {
		e := &encoder{}
		e.uint(v)
		d := &decoder{buf: e.buf}
		if got := d.u64(); got != v || d.err != nil || len(d.buf) != 0 {
			t.Errorf("%d round-tripped as %d, %v", v, got, d.err)
		}
	}
	// A 256-entry palette's length, as herdr-gpui's fixture has it.
	e := &encoder{}
	e.uint(256)
	if want := []byte{251, 0, 1}; !bytes.Equal(e.buf, want) {
		t.Errorf("256 as %v, want %v", e.buf, want)
	}
	// A u16 field holding more fails rather than wrapping.
	d := &decoder{buf: []byte{252, 0, 0, 1, 0}}
	if d.u16(); d.err == nil {
		t.Error("a u32 read as u16")
	}
}

func TestApplyPatch(t *testing.T) {
	cell := func(s string) Cell { return Cell{Symbol: s, FG: 16, BG: 16, Hyperlink: -1} }
	s := &Surface{BootID: "b", SurfaceRevision: 1, Frame: Frame{Width: 3, Height: 2,
		Cells: []Cell{cell("a"), cell("b"), cell("c"), cell("d"), cell("e"), cell("f")}}}
	p := &SurfacePatch{BootID: "b", BaseRevision: 1, Revision: 2, Rows: []PatchRow{{X: 1, Y: 1, Cells: []Cell{cell("x"), cell("y")}}}}
	if err := s.Apply(p); err != nil {
		t.Fatal(err)
	}
	if got := s.Frame.Row(1); got != "dxy" || s.SurfaceRevision != 2 {
		t.Errorf("row %q at revision %d", got, s.SurfaceRevision)
	}
	if err := s.Apply(p); err != ErrStalePatch {
		t.Errorf("a patch applied twice: %v", err)
	}
	p = &SurfacePatch{BootID: "b", BaseRevision: 2, Revision: 3, Rows: []PatchRow{{X: 2, Y: 0, Cells: []Cell{cell("x"), cell("y")}}}}
	if err := s.Apply(p); err == nil || s.SurfaceRevision != 2 {
		t.Errorf("a row past the edge applied: %v", err)
	}
}
