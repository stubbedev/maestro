// The value codec (docs/PLUGINS.md §6.4). Values cross as JSON, written
// and read with internal/php's json_encode/json_decode ports (the shim
// uses PHP's own). A tag is a JSON object whose first key starts with NUL:
//
//	{"\u0000b":"<base64>"}               a string that is not UTF-8
//	{"\u0000f":"INF"}                    INF, -INF, NAN
//	{"\u0000e":[[k,v],…]}                an array whose first key starts with NUL
//	                                     (or with a key that is not UTF-8)
//	{"\u0000s":{…}}                      a stdClass
//	{"\u0000o":h,"c":…,"base":…,"d":…}   an object, by handle; class, base and
//	                                     snapshot only the first time the
//	                                     receiver sees the handle
//
// Other packages add value tags (constraints, links) with ValueEncoder and
// Conn.RegisterTag.

package rpc

import (
	"encoding/base64"
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/stubbedev/maestro/internal/php"
)

// The tag keys.
const (
	tagObject = "\x00o"
	tagBinary = "\x00b"
	tagFloat  = "\x00f"
	tagPairs  = "\x00e"
	tagStd    = "\x00s"
)

// jsonFlags are the shim's json_encode flags; jsonDepth is the depth both
// sides allow (far beyond what Composer data nests to, with the tags'
// levels).
const (
	jsonFlags = php.JSONUnescapedSlashes | php.JSONUnescapedUnicode | php.JSONPreserveZeroFraction
	jsonDepth = 65536
)

// ValueEncoder is a Go value that crosses as a value tag of its own (a
// constraint, a link).
type ValueEncoder interface {
	// EncodeRPC returns the tag: a *php.Array whose first key is the tag
	// key, with its fields encoded through e.
	EncodeRPC(e *Encoder) (*php.Array, error)
}

// TagDecoder decodes a value tag registered with Conn.RegisterTag.
type TagDecoder func(tag *php.Array, d *Decoder) (any, error)

// Tag builds a tag: key (starting with NUL) holding value, then the other
// key/value pairs.
func Tag(key string, value any, kv ...any) *php.Array {
	a := php.NewArrayCap(1 + len(kv)/2)
	a.Set(key, value)
	for i := 0; i+1 < len(kv); i += 2 {
		a.Set(kv[i], kv[i+1])
	}

	return a
}

// Encoder encodes the values of one message. The objects it sends for the
// first time count as sent only once the message is.
type Encoder struct {
	c          *Conn
	sent       map[Handle]bool
	newMirrors []*mirrorState
}

func (c *Conn) newEncoder() *Encoder { return &Encoder{c: c, sent: map[Handle]bool{}} }

// commit records what the message sent.
func (e *Encoder) commit() {
	maps.Copy(e.c.h.sent, e.sent)
	for _, ms := range e.newMirrors {
		e.c.h.mirrors = append(e.c.h.mirrors, ms)
		e.c.h.mirrorIdx[ms.h] = ms
	}
}

func (e *Encoder) known(h Handle) bool { return e.c.h.sent[h] || e.sent[h] }

// Value returns v in the form json_encode writes as §6.4 describes. It
// accepts nil, booleans, integers, floats, strings, []byte (a binary
// string), *php.Array, *php.Object (a stdClass), []any, []string,
// map[string]any (in key order), Handle, *PHPObject, Object (Go-owned
// objects) and ValueEncoder.
func (e *Encoder) Value(v any) (any, error) {
	out, _, err := e.value(v)

	return out, err
}

// value also reports whether the result differs from v.
func (e *Encoder) value(v any) (any, bool, error) {
	switch v := v.(type) {
	case nil, bool, int64, int:
		return v, false, nil
	case string:
		return encodeString(v)
	case float64:
		return encodeFloat(v)
	case float32:
		out, _, err := encodeFloat(float64(v))

		return out, true, err
	case int8:
		return int64(v), true, nil
	case int16:
		return int64(v), true, nil
	case int32:
		return int64(v), true, nil
	case uint8:
		return int64(v), true, nil
	case uint16:
		return int64(v), true, nil
	case uint32:
		return int64(v), true, nil
	case uint:
		return encodeUint(uint64(v))
	case uint64:
		return encodeUint(v)
	case []byte:
		out, _, err := encodeString(string(v))

		return out, true, err
	case *php.Array:
		return e.array(v)
	case *php.Object:
		props, _, err := e.array(v.ToArray())

		return Tag(tagStd, props), true, err
	case []any:
		a := php.NewArrayCap(len(v))
		for _, item := range v {
			out, _, err := e.value(item)
			if err != nil {
				return nil, false, err
			}
			a.Append(out)
		}

		return a, true, nil
	case []string:
		a := php.NewArrayCap(len(v))
		for _, item := range v {
			out, _, err := encodeString(item)
			if err != nil {
				return nil, false, err
			}
			a.Append(out)
		}

		return a, true, nil
	case map[string]any:
		a := php.NewArrayCap(len(v))
		for _, k := range slices.Sorted(maps.Keys(v)) {
			a.Set(k, v[k])
		}
		out, _, err := e.array(a)

		return out, true, err
	case Handle:
		if _, ok := e.c.h.lookup(v); !ok {
			return nil, false, unknownHandle(v)
		}

		return Tag(tagObject, int64(v)), true, nil
	case *PHPObject:
		return Tag(tagObject, int64(v.H)), true, nil
	case ValueEncoder:
		out, err := v.EncodeRPC(e)

		return out, true, err
	case Object:
		out, err := e.object(v)

		return out, true, err
	}

	return nil, false, fmt.Errorf("rpc: a %T cannot cross to PHP", v)
}

func encodeString(s string) (any, bool, error) {
	if utf8.ValidString(s) {
		return s, false, nil
	}

	return Tag(tagBinary, base64.StdEncoding.EncodeToString([]byte(s))), true, nil
}

func encodeFloat(f float64) (any, bool, error) {
	switch {
	case math.IsNaN(f):
		return Tag(tagFloat, "NAN"), true, nil
	case math.IsInf(f, 1):
		return Tag(tagFloat, "INF"), true, nil
	case math.IsInf(f, -1):
		return Tag(tagFloat, "-INF"), true, nil
	}

	return f, false, nil
}

func encodeUint(u uint64) (any, bool, error) {
	if u > math.MaxInt64 {
		return nil, false, fmt.Errorf("rpc: %d does not fit a PHP int", u)
	}

	return int64(u), true, nil
}

// array encodes a PHP array, copying it only when something in it
// changes. An array whose first key starts with NUL, or with a string key
// that is not UTF-8, becomes explicit pairs.
func (e *Encoder) array(a *php.Array) (any, bool, error) {
	var (
		out     *php.Array
		pairs   bool
		changed bool
		i       int
	)

	keys := a.Keys()
	for _, k := range keys {
		if k.IsString() && !pairs {
			s := k.String()
			if (i == 0 && strings.HasPrefix(s, "\x00")) || !utf8.ValidString(s) {
				pairs = true
			}
		}

		item, _ := a.GetKey(k)
		v, ch, err := e.value(item)
		if err != nil {
			return nil, false, err
		}
		if ch && out == nil {
			out = php.NewArrayCap(len(keys))
			for _, prev := range keys[:i] {
				pv, _ := a.GetKey(prev)
				out.SetKey(prev, pv)
			}
		}
		if out != nil {
			out.SetKey(k, v)
		}
		changed = changed || ch
		i++
	}
	if out == nil {
		out = a
	}

	if !pairs {
		return out, changed, nil
	}

	list := php.NewArrayCap(out.Len())
	for k, v := range out.All() {
		var key any = k.Int()
		if k.IsString() {
			s, _, _ := encodeString(k.String())
			key = s
		}
		list.Append(php.ListOf(key, v))
	}

	return Tag(tagPairs, list), true, nil
}

// object encodes a Go-owned object: its handle, plus its class, mirror
// base and snapshot the first time PHP sees it.
func (e *Encoder) object(o Object) (any, error) {
	h, err := e.c.h.handleOf(o)
	if err != nil {
		return nil, err
	}
	if e.known(h) {
		return Tag(tagObject, int64(h)), nil
	}
	e.sent[h] = true

	t := Tag(tagObject, int64(h), "c", o.PHPClass())
	if m, ok := o.(Mirror); ok {
		rev := m.Rev()
		snapshot, err := m.MirrorSnapshot()
		if err != nil {
			return nil, err
		}
		d, _, err := e.array(snapshot)
		if err != nil {
			return nil, err
		}
		t.Set("base", m.MirrorBase())
		t.Set("d", d)
		e.newMirrors = append(e.newMirrors, &mirrorState{h: h, m: m, rev: rev})
	}

	return t, nil
}

// Decoder resolves the tags of a decoded message.
type Decoder struct {
	c *Conn
}

// Value resolves the tags in v (a json_decode()d value), in place.
func (d *Decoder) Value(v any) (any, error) {
	a, ok := v.(*php.Array)
	if !ok {
		return v, nil
	}

	if k, _, ok := a.First(); ok && k.IsString() && strings.HasPrefix(k.String(), "\x00") {
		return d.tag(k.String(), a)
	}

	for _, k := range a.Keys() {
		item, _ := a.GetKey(k)
		if sub, ok := item.(*php.Array); ok {
			v, err := d.Value(sub)
			if err != nil {
				return nil, err
			}
			a.SetKey(k, v)
		}
	}

	return a, nil
}

func (d *Decoder) tag(key string, t *php.Array) (any, error) {
	v, _ := t.Get(key)

	switch key {
	case tagObject:
		return d.object(t)
	case tagBinary:
		s, _ := v.(string)
		b, err := base64.StdEncoding.Strict().DecodeString(s)
		if err != nil {
			return nil, &ProtocolError{Message: "invalid binary string"}
		}

		return string(b), nil
	case tagFloat:
		switch v {
		case "INF":
			return math.Inf(1), nil
		case "-INF":
			return math.Inf(-1), nil
		case "NAN":
			return math.NaN(), nil
		}

		return nil, &ProtocolError{Message: "invalid float tag"}
	case tagPairs:
		list, ok := v.(*php.Array)
		if !ok {
			return nil, &ProtocolError{Message: "invalid pairs tag"}
		}
		out := php.NewArrayCap(list.Len())
		for _, item := range list.Values() {
			pair, ok := item.(*php.Array)
			if !ok || pair.Len() != 2 {
				return nil, &ProtocolError{Message: "invalid pair"}
			}
			k, _ := pair.Get(0)
			val, _ := pair.Get(1)
			k, err := d.Value(k)
			if err != nil {
				return nil, err
			}
			val, err = d.Value(val)
			if err != nil {
				return nil, err
			}
			out.SetKey(php.ToKey(k), val)
		}

		return out, nil
	case tagStd:
		props, err := d.Value(v)
		if err != nil {
			return nil, err
		}
		a, ok := props.(*php.Array)
		if !ok {
			return nil, &ProtocolError{Message: "invalid stdClass tag"}
		}

		return php.ObjectFromArray(a), nil
	}

	if dec, ok := d.c.tags[key]; ok {
		return dec(t, d)
	}

	return nil, &ProtocolError{Message: "unknown value tag " + fmt.Sprintf("%q", key)}
}

// object resolves an object tag. A PHP handle seen for the first time
// carries the class: a PHP-born mirror whose base has a factory becomes a
// Go object (registered back to PHP in the next sync block); anything
// else stays a *PHPObject.
func (d *Decoder) object(t *php.Array) (any, error) {
	hv, _ := t.Get(tagObject)
	hi, ok := hv.(int64)
	if !ok {
		return nil, &ProtocolError{Message: "invalid object tag"}
	}
	h := Handle(hi)

	if o, ok := d.c.h.lookup(h); ok {
		return o, nil
	}
	class, ok := t.GetString("c")
	if h >= 0 || !ok {
		return nil, unknownHandle(h)
	}
	base, _ := t.GetString("base")

	var snapshot any
	if raw, ok := t.Get("d"); ok {
		var err error
		if snapshot, err = d.Value(raw); err != nil {
			return nil, err
		}
	}

	if factory, ok := d.c.factories[base]; ok && base != "" {
		fields, _ := snapshot.(*php.Array)
		if fields == nil {
			fields = php.NewArray()
		}
		m, err := factory(class, fields)
		if err != nil {
			return nil, err
		}
		gh, err := d.c.h.handleOf(m)
		if err != nil {
			return nil, err
		}
		d.c.h.adopted[h] = m
		d.c.pendingReg = append(d.c.pendingReg, registration{tmp: h, h: gh, m: m, rev: m.Rev()})

		return m, nil
	}

	o := &PHPObject{H: h, Class: class, Base: base, Snapshot: snapshot}
	d.c.h.phpObjects[h] = o

	return o, nil
}
