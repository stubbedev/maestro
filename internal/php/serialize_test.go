package php

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"
)

// The oracle goldens carry PHP values as serialize() strings; these test
// helpers port serialize() and the subset of unserialize() they need.

func phpSerialize(v any) string {
	var b strings.Builder
	serializeTo(&b, v)
	return b.String()
}

func serializeTo(b *strings.Builder, v any) {
	switch v := v.(type) {
	case nil:
		b.WriteString("N;")
	case bool:
		if v {
			b.WriteString("b:1;")
		} else {
			b.WriteString("b:0;")
		}
	case int64:
		fmt.Fprintf(b, "i:%d;", v)
	case int:
		fmt.Fprintf(b, "i:%d;", v)
	case float64:
		b.WriteString("d:")
		b.Write(appendDouble(nil, v, -1, false))
		b.WriteByte(';')
	case string:
		fmt.Fprintf(b, "s:%d:\"%s\";", len(v), v)
	case *Array:
		fmt.Fprintf(b, "a:%d:{", v.Len())
		for k, x := range v.All() {
			serializeTo(b, k.Value())
			serializeTo(b, x)
		}
		b.WriteByte('}')
	case *Object:
		fmt.Fprintf(b, "O:8:\"stdClass\":%d:{", v.Len())
		for k, x := range v.All() {
			serializeTo(b, k)
			serializeTo(b, x)
		}
		b.WriteByte('}')
	default:
		panic(fmt.Sprintf("serialize: %T", v))
	}
}

type unserializer struct {
	s    string
	pos  int
	vals []any // values in var_hash order, for r: references
}

func phpUnserialize(t testing.TB, s string) any {
	t.Helper()
	u := &unserializer{s: s}
	v, err := u.value()
	if err == nil && u.pos != len(s) {
		err = fmt.Errorf("trailing data at %d", u.pos)
	}
	if err != nil {
		t.Fatalf("unserialize %q: %v", s, err)
	}
	return v
}

func (u *unserializer) until(c byte) (string, error) {
	i := strings.IndexByte(u.s[u.pos:], c)
	if i < 0 {
		return "", fmt.Errorf("missing %q at %d", c, u.pos)
	}
	r := u.s[u.pos : u.pos+i]
	u.pos += i + 1
	return r, nil
}

func (u *unserializer) str() (string, error) {
	n, err := u.until(':')
	if err != nil {
		return "", err
	}
	l, err := strconv.Atoi(n)
	if err != nil {
		return "", err
	}
	u.pos++ // "
	r := u.s[u.pos : u.pos+l]
	u.pos += l + 1 // "
	return r, nil
}

func (u *unserializer) value() (any, error) {
	return u.parse(true)
}

func (u *unserializer) parse(record bool) (any, error) {
	if u.pos+2 > len(u.s) {
		return nil, fmt.Errorf("truncated at %d", u.pos)
	}
	typ := u.s[u.pos]
	u.pos += 2
	var v any
	switch typ {
	case 'N':
	case 'b':
		x, err := u.until(';')
		if err != nil {
			return nil, err
		}
		v = x == "1"
	case 'i':
		x, err := u.until(';')
		if err != nil {
			return nil, err
		}
		n, err := strconv.ParseInt(x, 10, 64)
		if err != nil {
			return nil, err
		}
		v = n
	case 'd':
		x, err := u.until(';')
		if err != nil {
			return nil, err
		}
		switch x {
		case "INF":
			v = math.Inf(1)
		case "-INF":
			v = math.Inf(-1)
		case "NAN":
			v = math.NaN()
		default:
			f, err := strconv.ParseFloat(x, 64)
			if err != nil {
				return nil, err
			}
			v = f
		}
	case 's':
		x, err := u.str()
		if err != nil {
			return nil, err
		}
		u.pos++ // ;
		v = x
	case 'r', 'R':
		x, err := u.until(';')
		if err != nil {
			return nil, err
		}
		n, _ := strconv.Atoi(x)
		v = u.vals[n-1]
		if record {
			u.vals = append(u.vals, v)
		}
		return v, nil
	case 'a':
		n, err := u.until(':')
		if err != nil {
			return nil, err
		}
		count, _ := strconv.Atoi(n)
		u.pos++ // {
		a := NewArrayCap(count)
		if record {
			u.vals = append(u.vals, a)
		}
		for range count {
			k, err := u.parse(false)
			if err != nil {
				return nil, err
			}
			x, err := u.value()
			if err != nil {
				return nil, err
			}
			a.Set(k, x)
		}
		u.pos++ // }
		return a, nil
	case 'O':
		if _, err := u.str(); err != nil {
			return nil, err
		}
		u.pos++ // :
		n, err := u.until(':')
		if err != nil {
			return nil, err
		}
		count, _ := strconv.Atoi(n)
		u.pos++ // {
		o := NewObject()
		if record {
			u.vals = append(u.vals, o)
		}
		for range count {
			k, err := u.parse(false)
			if err != nil {
				return nil, err
			}
			x, err := u.value()
			if err != nil {
				return nil, err
			}
			o.Set(ToString(k), x)
		}
		u.pos++ // }
		return o, nil
	default:
		return nil, fmt.Errorf("unknown type %q at %d", typ, u.pos-2)
	}
	if record {
		u.vals = append(u.vals, v)
	}
	return v, nil
}

func TestSerializeRoundTrip(t *testing.T) {
	for _, s := range []string{
		`N;`, `b:1;`, `i:-5;`, `d:0.1;`, `d:1.0E+25;`, `d:-0;`, `s:3:"a"b";`, `a:2:{i:0;s:1:"x";s:1:"k";a:0:{}}`,
		`O:8:"stdClass":2:{s:1:"0";i:1;s:0:"";N;}`,
	} {
		if got := phpSerialize(phpUnserialize(t, s)); got != s {
			t.Errorf("%s: got %s", s, got)
		}
	}
}
