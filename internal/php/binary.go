// Ports nothing: a compact binary form of decoded JSON values, which reads
// back much faster than json_decode() builds them (deliberate deviation 3,
// speed). maestro keeps decoded repository metadata in it between runs.

package php

import (
	"encoding/binary"
	"errors"
	"math"
)

// The binary form is a tree of tagged values. A string's first use holds
// its bytes, later uses its index among the strings seen so far, so the
// many repeated strings of a metadata file are stored and allocated once.
const (
	binNull byte = iota
	binFalse
	binTrue
	binInt    // zigzag varint
	binFloat  // 8 bytes, IEEE 754 little endian
	binStrNew // varint length, bytes
	binStrRef // varint index
	binArray  // varint count, then count × (key, value)

	// keys
	binKeyInt    // zigzag varint
	binKeyStrNew // varint length, bytes
	binKeyStrRef // varint index
)

// binMaxDepth bounds the nesting DecodeBinary accepts, as json_decode()'s
// default depth bounds what it builds.
const binMaxDepth = JSONDefaultDepth + 1

var errBinary = errors.New("php: malformed binary value")

// AppendBinary appends the binary form of v to dst. ok is false when v
// holds a value other than null, bool, int, float, string or array (an
// object); dst is then to be dropped.
func AppendBinary(dst []byte, v any) (out []byte, ok bool) {
	e := binEncoder{buf: dst, strs: map[string]uint64{}}
	if !e.value(v, 0) {
		return dst, false
	}

	return e.buf, true
}

type binEncoder struct {
	buf  []byte
	strs map[string]uint64
}

func (e *binEncoder) str(s string, newTag, refTag byte) {
	if i, ok := e.strs[s]; ok {
		e.buf = append(e.buf, refTag)
		e.buf = binary.AppendUvarint(e.buf, i)

		return
	}
	e.strs[s] = uint64(len(e.strs))
	e.buf = append(e.buf, newTag)
	e.buf = binary.AppendUvarint(e.buf, uint64(len(s)))
	e.buf = append(e.buf, s...)
}

func (e *binEncoder) value(v any, depth int) bool {
	switch v := v.(type) {
	case nil:
		e.buf = append(e.buf, binNull)
	case bool:
		if v {
			e.buf = append(e.buf, binTrue)
		} else {
			e.buf = append(e.buf, binFalse)
		}
	case int64:
		e.buf = append(e.buf, binInt)
		e.buf = binary.AppendVarint(e.buf, v)
	case float64:
		e.buf = append(e.buf, binFloat)
		e.buf = binary.LittleEndian.AppendUint64(e.buf, math.Float64bits(v))
	case string:
		e.str(v, binStrNew, binStrRef)
	case *Array:
		if depth >= binMaxDepth || v.next != binNextFree(v) {
			return false
		}
		e.buf = append(e.buf, binArray)
		e.buf = binary.AppendUvarint(e.buf, uint64(v.live)) //nolint:gosec // live is never negative
		for i := range v.entries {
			ent := &v.entries[i]
			switch ent.k.kind {
			case kindDead:
				continue
			case kindInt:
				e.buf = append(e.buf, binKeyInt)
				e.buf = binary.AppendVarint(e.buf, ent.k.i)
			default:
				e.str(ent.k.s, binKeyStrNew, binKeyStrRef)
			}
			if !e.value(ent.v, depth+1) {
				return false
			}
		}
	default:
		return false
	}

	return true
}

// binNextFree is the next free index an array holding a's keys has when
// they were only ever added, as DecodeBinary builds it: one past the
// largest int key. An array that lost its largest int key keeps a larger
// one, which the binary form does not carry.
func binNextFree(a *Array) int64 {
	next := int64(noNextFree)
	for i := range a.entries {
		if k := a.entries[i].k; k.kind == kindInt && k.i >= next {
			next = k.i + 1
			if k.i == math.MaxInt64 {
				next = math.MaxInt64
			}
		}
	}

	return next
}

// DecodeBinary returns the value AppendBinary encoded in data. Arrays come
// back with the keys, order and values they were encoded with, as building
// them by adding those keys in order makes them (json_decode() builds its
// arrays so). Strings share the memory of one copy of data.
func DecodeBinary(data []byte) (any, error) {
	d := binDecoder{b: data, s: string(data)}
	v, ok := d.value(0)
	if !ok || d.pos != len(d.s) {
		return nil, errBinary
	}

	return v, nil
}

type binDecoder struct {
	// b is the data, s a copy the strings are cut from
	b   []byte
	s   string
	pos int
	// strs are the strings seen so far; boxes their values as any, made
	// once each when used as a value
	strs  []string
	boxes []any
}

func (d *binDecoder) uvarint() (uint64, bool) {
	x, n := binary.Uvarint(d.b[d.pos:])
	if n <= 0 {
		return 0, false
	}
	d.pos += n

	return x, true
}

func (d *binDecoder) varint() (int64, bool) {
	x, n := binary.Varint(d.b[d.pos:])
	if n <= 0 {
		return 0, false
	}
	d.pos += n

	return x, true
}

// count reads the number of things that follow, each taking per bytes at
// least: it cannot be more than the data left holds.
func (d *binDecoder) count(per int) (int, bool) {
	x, ok := d.uvarint()
	if !ok || x > uint64((len(d.s)-d.pos)/per) { //nolint:gosec // never negative
		return 0, false
	}

	return int(x), true //nolint:gosec // bounded by len(d.s)
}

// newStr reads a string's bytes and adds it to the strings seen.
func (d *binDecoder) newStr() (int, bool) {
	n, ok := d.count(1)
	if !ok {
		return 0, false
	}
	d.strs = append(d.strs, d.s[d.pos:d.pos+n])
	d.boxes = append(d.boxes, nil)
	d.pos += n

	return len(d.strs) - 1, true
}

func (d *binDecoder) strRef() (int, bool) {
	x, ok := d.uvarint()
	if !ok || x >= uint64(len(d.strs)) {
		return 0, false
	}

	return int(x), true //nolint:gosec // bounded by len(d.strs)
}

func (d *binDecoder) box(i int) any {
	if d.boxes[i] == nil {
		d.boxes[i] = d.strs[i]
	}

	return d.boxes[i]
}

func (d *binDecoder) value(depth int) (any, bool) {
	if d.pos >= len(d.s) {
		return nil, false
	}
	tag := d.s[d.pos]
	d.pos++
	switch tag {
	case binNull:
		return nil, true
	case binFalse:
		return false, true
	case binTrue:
		return true, true
	case binInt:
		i, ok := d.varint()

		return i, ok
	case binFloat:
		if len(d.s)-d.pos < 8 {
			return nil, false
		}
		var b [8]byte
		copy(b[:], d.s[d.pos:d.pos+8])
		d.pos += 8

		return math.Float64frombits(binary.LittleEndian.Uint64(b[:])), true
	case binStrNew:
		i, ok := d.newStr()
		if !ok {
			return nil, false
		}

		return d.box(i), true
	case binStrRef:
		i, ok := d.strRef()
		if !ok {
			return nil, false
		}

		return d.box(i), true
	case binArray:
		return d.array(depth)
	}

	return nil, false
}

func (d *binDecoder) array(depth int) (any, bool) {
	// each entry takes two bytes at least
	n, ok := d.count(2)
	if !ok || depth >= binMaxDepth {
		return nil, false
	}
	a := NewArray()
	if n == 0 {
		return a, true
	}
	a.entries = make([]entry, n)
	for j := range a.entries {
		if d.pos >= len(d.s) {
			return nil, false
		}
		tag := d.s[d.pos]
		d.pos++
		ent := &a.entries[j]
		switch tag {
		case binKeyInt:
			i, ok := d.varint()
			if !ok {
				return nil, false
			}
			ent.k = Key{i: i}
		case binKeyStrNew, binKeyStrRef:
			var i int
			if tag == binKeyStrNew {
				i, ok = d.newStr()
			} else {
				i, ok = d.strRef()
			}
			if !ok {
				return nil, false
			}
			ent.k = Key{s: d.strs[i], kind: kindStr}
		default:
			return nil, false
		}
		if ent.v, ok = d.value(depth + 1); !ok {
			return nil, false
		}
	}
	if !a.built() {
		return nil, false
	}

	return a, true
}

// built sets the state of an array whose entries were filled in, to what
// inserting them in order gives, and tells whether its keys are distinct.
func (a *Array) built() bool {
	a.live = len(a.entries)
	strs := 0
	for i := range a.entries {
		k := a.entries[i].k
		if k.kind == kindInt {
			if k.i >= a.next {
				a.next = k.i + 1
				if k.i == math.MaxInt64 {
					a.next = math.MaxInt64
				}
			}
			if a.packed && k.i != int64(i) {
				a.packed = false
			}
		} else {
			a.packed = false
			strs++
		}
	}
	if a.packed {
		return true
	}
	if len(a.entries) <= linearMax {
		for i := 1; i < len(a.entries); i++ {
			for j := range i {
				if a.entries[i].k == a.entries[j].k {
					return false
				}
			}
		}

		return true
	}

	// insert indexes an array once it is not packed and longer than
	// linearMax: here at once, the maps sized for the keys
	a.indexed = true
	if strs > 0 {
		a.strIdx = make(map[string]int32, strs)
	}
	if ints := len(a.entries) - strs; ints > 0 {
		a.intIdx = make(map[int64]int32, ints)
	}
	for i := range a.entries {
		k := a.entries[i].k
		if k.kind == kindInt {
			if _, dup := a.intIdx[k.i]; dup {
				return false
			}
			a.intIdx[k.i] = int32(i)
		} else {
			if _, dup := a.strIdx[k.s]; dup {
				return false
			}
			a.strIdx[k.s] = int32(i)
		}
	}

	return true
}
