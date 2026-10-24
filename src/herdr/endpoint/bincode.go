package endpoint

import (
	"encoding/binary"
	"errors"
	"math"
	"unicode/utf8"
)

// The wire is bincode 2's standard configuration: little endian, integers
// wider than a byte in its variable-length form, and every value in the
// order its type declares it, enums as a variant index before their fields.

const (
	varint16 = 251 // the next two bytes hold the value
	varint32 = 252 // the next four
	varint64 = 253 // the next eight
)

var errMalformed = errors.New("endpoint: malformed message")

type encoder struct{ buf []byte }

func (e *encoder) u8(v uint8) { e.buf = append(e.buf, v) }

func (e *encoder) uint(v uint64) {
	switch {
	case v < varint16:
		e.buf = append(e.buf, byte(v))
	case v <= math.MaxUint16:
		e.buf = binary.LittleEndian.AppendUint16(append(e.buf, varint16), uint16(v))
	case v <= math.MaxUint32:
		e.buf = binary.LittleEndian.AppendUint32(append(e.buf, varint32), uint32(v))
	default:
		e.buf = binary.LittleEndian.AppendUint64(append(e.buf, varint64), v)
	}
}

func (e *encoder) bool(v bool) {
	if v {
		e.u8(1)
	} else {
		e.u8(0)
	}
}

func (e *encoder) string(s string) {
	e.uint(uint64(len(s)))
	e.buf = append(e.buf, s...)
}

// char is a Rust char: its UTF-8 bytes, with no length.
func (e *encoder) char(r rune) { e.buf = utf8.AppendRune(e.buf, r) }

// variant starts an enum value with the index of its variant.
func (e *encoder) variant(i uint32) { e.uint(uint64(i)) }

// decoder reads values in order; the first malformed one sets err, after
// which every read returns zero.
type decoder struct {
	buf []byte
	err error
}

func (d *decoder) fail() {
	if d.err == nil {
		d.err = errMalformed
	}
	d.buf = nil
}

func (d *decoder) take(n int) []byte {
	if d.err != nil || n > len(d.buf) {
		d.fail()
		return nil
	}
	b := d.buf[:n]
	d.buf = d.buf[n:]
	return b
}

func (d *decoder) u8() uint8 {
	if b := d.take(1); b != nil {
		return b[0]
	}
	return 0
}

func (d *decoder) uint(max uint64) uint64 {
	var v uint64
	switch first := d.u8(); first {
	case varint16:
		if b := d.take(2); b != nil {
			v = uint64(binary.LittleEndian.Uint16(b))
		}
	case varint32:
		if b := d.take(4); b != nil {
			v = uint64(binary.LittleEndian.Uint32(b))
		}
	case varint64:
		if b := d.take(8); b != nil {
			v = binary.LittleEndian.Uint64(b)
		}
	default:
		if first > varint64 {
			d.fail() // u128, which no field holds
		}
		v = uint64(first)
	}
	if v > max {
		d.fail()
		return 0
	}
	return v
}

func (d *decoder) u16() uint16 { return uint16(d.uint(math.MaxUint16)) }
func (d *decoder) u32() uint32 { return uint32(d.uint(math.MaxUint32)) }
func (d *decoder) u64() uint64 { return d.uint(math.MaxUint64) }

// i32 is zigzag encoded before its variable-length form.
func (d *decoder) i32() int32 {
	v := uint32(d.uint(math.MaxUint32))
	return int32(v>>1) ^ -int32(v&1)
}

func (d *decoder) bool() bool {
	switch d.u8() {
	case 0:
		return false
	case 1:
		return true
	}
	d.fail()
	return false
}

// len reads the length of a sequence, which cannot exceed what is left of
// the message: every element takes at least a byte.
func (d *decoder) len() int { return int(d.uint(uint64(len(d.buf)))) }

func (d *decoder) bytes() []byte { return d.take(d.len()) }

func (d *decoder) string() string {
	b := d.bytes()
	if !utf8.Valid(b) {
		d.fail()
		return ""
	}
	return string(b)
}

func (d *decoder) variant() uint32 { return d.u32() }

// some reads the tag of an Option.
func (d *decoder) some() bool { return d.bool() }

func (d *decoder) optString() *string {
	if !d.some() {
		return nil
	}
	s := d.string()
	return &s
}

func (d *decoder) optU32() *uint32 {
	if !d.some() {
		return nil
	}
	v := d.u32()
	return &v
}
