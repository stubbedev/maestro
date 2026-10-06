package php

import (
	"maps"
	"math"
	"math/rand/v2"
	"slices"
	"sync"
	"testing"
)

func TestStrKey(t *testing.T) {
	for s, want := range map[string]any{
		"0": int64(0), "123": int64(123), "-5": int64(-5), "9223372036854775807": int64(math.MaxInt64),
		"-9223372036854775808": int64(math.MinInt64), "9223372036854775808": "9223372036854775808",
		"-9223372036854775809": "-9223372036854775809", "05": "05", "-0": "-0", "1.5": "1.5", " 1": " 1", "1 ": "1 ",
		"": "", "-": "-", "+1": "+1", "1e3": "1e3", "00": "00", "0x1": "0x1", "١": "١",
	} {
		if got := StrKey(s).Value(); got != want {
			t.Errorf("StrKey(%q) = %#v, want %#v", s, got, want)
		}
	}
}

func TestToKey(t *testing.T) {
	for _, c := range []struct {
		in   any
		want any
	}{
		{nil, ""},
		{true, int64(1)},
		{false, int64(0)},
		{1.9, int64(1)},
		{-1.9, int64(-1)},
		{math.NaN(), int64(0)},
		{math.Inf(1), int64(0)},
		{1e19, int64(-8446744073709551616)},
		{7, int64(7)},
		{"7", int64(7)},
	} {
		if got := ToKey(c.in).Value(); got != c.want {
			t.Errorf("ToKey(%v) = %#v, want %#v", c.in, got, c.want)
		}
	}
	if msg := catch(func() { ToKey(NewArray()) }); msg != "php: illegal offset type array" {
		t.Errorf("array offset: %q", msg)
	}
}

// TestArrayForeachSnapshot checks that All iterates like foreach by value.
func TestArrayForeachSnapshot(t *testing.T) {
	a := ListOf("a", "b", "c")
	var seen []any
	for k, v := range a.All() {
		seen = append(seen, v)
		switch k.Int() {
		case 0:
			a.Set(1, "B")
			a.Append("d")
		case 1:
			a.Delete(2)
		}
	}
	if !slices.Equal(seen, []any{"a", "b", "c"}) {
		t.Errorf("loop saw %v", seen)
	}
	assertArray(t, a, ArrayOf(0, "a", 1, "B", 3, "d"))

	// Nested loops and breaking out keep the snapshot logic consistent.
	b := ListOf(1, 2, 3)
	for range b.All() {
		for range b.All() {
			break
		}
		b.Append(4)
		break
	}
	b.Set(0, 0)
	assertArray(t, b, ListOf(0, 2, 3, 4))
}

func TestArrayConcurrentReads(t *testing.T) {
	a := NewArray()
	for i := range 100 {
		a.Set("k"+string(rune('a'+i%26))+string(rune('0'+i/26)), i)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 100 {
				n := 0
				for range a.All() {
					n++
				}
				if n != 100 {
					t.Errorf("saw %d", n)
				}
				if _, ok := a.Get("ka0"); !ok {
					t.Error("lookup failed")
				}
			}
		})
	}
	wg.Wait()
}

// refArray is a straightforward model of a PHP array for the randomized
// test below.
type refArray struct {
	keys []Key
	vals []any
	next int64
}

func (r *refArray) find(k Key) int { return slices.Index(r.keys, k) }

func (r *refArray) set(k Key, v any) {
	if i := r.find(k); i >= 0 {
		r.vals[i] = v
		return
	}
	r.keys = append(r.keys, k)
	r.vals = append(r.vals, v)
	if k.IsInt() && k.Int() >= r.next {
		r.next = k.Int() + 1
	}
}

func (r *refArray) del(k Key) {
	if i := r.find(k); i >= 0 {
		r.keys = slices.Delete(r.keys, i, i+1)
		r.vals = slices.Delete(r.vals, i, i+1)
	}
}

func TestArrayModel(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for round := range 300 {
		a := NewArray()
		ref := &refArray{next: math.MinInt64}
		for op := range rng.IntN(200) {
			var k Key
			switch rng.IntN(3) {
			case 0:
				k = IntKey(int64(rng.IntN(40) - 5))
			case 1:
				k = StrKey(string(rune('a' + rng.IntN(26))))
			default:
				k = IntKey(int64(rng.IntN(20)))
			}
			switch rng.IntN(5) {
			case 0, 1:
				a.SetKey(k, op)
				ref.set(k, int64(op))
			case 2:
				a.DeleteKey(k)
				ref.del(k)
			case 3:
				a.Append(op)
				n := ref.next
				if n == math.MinInt64 {
					n = 0
				}
				ref.set(IntKey(n), int64(op))
			default:
				v, ok := a.GetKey(k)
				i := ref.find(k)
				if ok != (i >= 0) || ok && v != ref.vals[i] {
					t.Fatalf("round %d: get %v = %v %v", round, k, v, ok)
				}
			}
		}
		if !slices.Equal(a.Keys(), ref.keys) || !slices.Equal(a.Values(), ref.vals) || a.Len() != len(ref.keys) {
			t.Fatalf("round %d: keys %v, want %v", round, a.Keys(), ref.keys)
		}
		wantList := true
		for i, k := range ref.keys {
			wantList = wantList && k == IntKey(int64(i))
		}
		if a.IsList() != wantList {
			t.Fatalf("round %d: IsList = %v", round, a.IsList())
		}
		c := a.Clone()
		if !StrictEquals(a, c) {
			t.Fatalf("round %d: clone differs", round)
		}
	}
}

// TestArrayShallowClone checks ShallowClone against setting each key into
// a new array: the same entries, internal state and next free index, and
// the nested arrays shared.
func TestArrayShallowClone(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4))
	for round := range 300 {
		a := NewArray()
		for op := range rng.IntN(60) {
			var k Key
			if rng.IntN(2) == 0 {
				k = IntKey(int64(rng.IntN(30) - 5))
			} else {
				k = StrKey(string(rune('a' + rng.IntN(26))))
			}
			switch rng.IntN(4) {
			case 0:
				a.DeleteKey(k)
			case 1:
				a.Append(ListOf(op))
			default:
				a.SetKey(k, op)
			}
		}

		want := NewArrayCap(a.Len())
		for k, v := range a.All() {
			want.SetKey(k, v)
		}
		got := a.ShallowClone()
		if !slices.Equal(got.entries, want.entries) || got.live != want.live || got.next != want.next ||
			got.packed != want.packed || got.indexed != want.indexed {
			t.Fatalf("round %d: ShallowClone %+v, want %+v", round, got, want)
		}
		// the index may be copied from a's: it is the one building gives
		if !maps.Equal(got.strIdx, want.strIdx) || !maps.Equal(got.intIdx, want.intIdx) {
			t.Fatalf("round %d: index %v %v, want %v %v", round, got.strIdx, got.intIdx, want.strIdx, want.intIdx)
		}
		for k, v := range got.All() {
			if w, _ := a.GetKey(k); w != v {
				t.Fatalf("round %d: %v not shared", round, k)
			}
		}
		got.Append("x")
		want.Append("x")
		got.SetKey(StrKey("new"), 1)
		want.SetKey(StrKey("new"), 1)
		if !slices.Equal(got.Keys(), want.Keys()) {
			t.Fatalf("round %d: append after clone: %v, want %v", round, got.Keys(), want.Keys())
		}
		if a.Has("new") {
			t.Fatalf("round %d: a changed with its clone", round)
		}
	}
}

func TestArrayCloneIsDeep(t *testing.T) {
	inner := ListOf(1)
	o := NewObject()
	o.Set("x", ListOf(2))
	a := ArrayOf("in", inner, "obj", o)
	c := a.Clone()
	inner.Append(5)
	o.Set("y", 1)
	assertArray(t, c, ArrayOf("in", ListOf(1), "obj", c.mustObject("obj")))
	if co := c.mustObject("obj"); co == o || co.Len() != 1 {
		t.Errorf("object not cloned")
	}
}

func (a *Array) mustObject(k any) *Object {
	v, _ := a.Get(k)
	return v.(*Object)
}

func TestArrayAppendOccupied(t *testing.T) {
	a := NewArray()
	a.Set(int64(math.MaxInt64), 1)
	if msg := catch(func() { a.Append(2) }); msg != "Cannot add element to the array as the next element is already occupied" {
		t.Errorf("got %q", msg)
	}
}

func TestObject(t *testing.T) {
	o := NewObject()
	o.Set("1", "a")
	o.Set("b", 2)
	o.Set("1", "c")
	if o.Len() != 2 || !slices.Equal(o.Keys(), []string{"1", "b"}) || !o.Has("1") {
		t.Fatalf("object %v", o.Keys())
	}
	a := o.ToArray()
	if v, _ := a.Get(1); v != "c" {
		t.Errorf("(array) keeps the numeric key: %v", VarExport(a))
	}
	if b := ObjectFromArray(a); !slices.Equal(b.Keys(), []string{"1", "b"}) {
		t.Errorf("(object) %v", b.Keys())
	}
	o.Delete("1")
	if s, _ := JSONEncode(o, 0); s != `{"b":2}` {
		t.Errorf("json %s", s)
	}
	if s, _ := JSONEncode(NewObject(), JSONPrettyPrint); s != "{}" {
		t.Errorf("empty object %s", s)
	}
}

func TestSortSlice(t *testing.T) {
	// The same order usort gives, including stability for equal elements.
	s := []string{"b2", "a1", "b1", "a2", "c", "a3", "b3"}
	SortSlice(s, func(x, y string) int { return int(x[0]) - int(y[0]) })
	if want := []string{"a1", "a2", "a3", "b2", "b1", "b3", "c"}; !slices.Equal(s, want) {
		t.Errorf("got %v", s)
	}
}
