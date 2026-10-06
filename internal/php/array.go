// Ports PHP's ordered hash table (Zend/zend_hash.c) as used for arrays:
// insertion order, int/string key coercion and nNextFreeElement.

package php

import (
	"iter"
	"maps"
	"math"
	"sync/atomic"
)

// linearMax is the size up to which lookups scan the entries instead of
// using an index; most Composer arrays are this small.
const linearMax = 8

// noNextFree is ZEND_LONG_MIN, PHP's "no int key yet" marker.
const noNextFree = math.MinInt64

// Array is PHP's ordered map. The zero value is not usable; create arrays
// with NewArray, ListOf or ArrayOf. See the package documentation for
// reference vs value semantics.
type Array struct {
	entries []entry
	strIdx  map[string]int32 // position of each string key, when indexed
	intIdx  map[int64]int32  // position of each int key, when indexed
	live    int
	next    int64 // nNextFreeElement
	packed  bool  // entries[i] has the int key i for every i, no holes
	indexed bool
	// pins counts running All loops that still read entries in place;
	// a write detaches entries first so those loops keep a snapshot. gen
	// counts detaches, so a loop knows whether its pin is still counted.
	// pins is atomic because concurrent read-only loops are allowed.
	pins atomic.Int32
	gen  uint32
}

type entry struct {
	k Key
	v any
}

// NewArray returns an empty array.
func NewArray() *Array { return &Array{next: noNextFree, packed: true} }

// NewArrayCap returns an empty array with room for n elements.
func NewArrayCap(n int) *Array {
	a := NewArray()
	a.entries = make([]entry, 0, n)
	return a
}

// ListOf returns the list [values...].
func ListOf(values ...any) *Array {
	a := NewArrayCap(len(values))
	for _, v := range values {
		a.Append(v)
	}
	return a
}

// ArrayOf returns the array [k1 => v1, k2 => v2, ...] from alternating keys
// and values; keys are coerced with ToKey.
func ArrayOf(kv ...any) *Array {
	if len(kv)%2 != 0 {
		panic("php: ArrayOf needs key/value pairs")
	}
	a := NewArrayCap(len(kv) / 2)
	for i := 0; i+1 < len(kv); i += 2 {
		a.Set(kv[i], kv[i+1])
	}
	return a
}

// StringList returns the list of the given strings.
func StringList(values []string) *Array {
	a := NewArrayCap(len(values))
	for _, v := range values {
		a.Append(v)
	}
	return a
}

// Len returns count($a).
func (a *Array) Len() int { return a.live }

// find returns the position of k in entries, or -1.
func (a *Array) find(k Key) int {
	if a.packed {
		if k.kind == kindInt && k.i >= 0 && k.i < int64(len(a.entries)) {
			return int(k.i)
		}
		return -1
	}
	if a.indexed {
		var (
			p  int32
			ok bool
		)
		if k.kind == kindInt {
			p, ok = a.intIdx[k.i]
		} else {
			p, ok = a.strIdx[k.s]
		}
		if ok {
			return int(p)
		}
		return -1
	}
	for i := range a.entries {
		if e := &a.entries[i].k; e.kind == k.kind && e.i == k.i && e.s == k.s {
			return i
		}
	}
	return -1
}

// GetKey returns the value stored under k.
func (a *Array) GetKey(k Key) (any, bool) {
	if p := a.find(k); p >= 0 {
		return a.entries[p].v, true
	}
	return nil, false
}

// Get returns $a[$k] and whether the key exists (array_key_exists, which
// unlike isset is true for null values). k is coerced with ToKey.
func (a *Array) Get(k any) (any, bool) { return a.GetKey(ToKey(k)) }

// Has reports array_key_exists($k, $a).
func (a *Array) Has(k any) bool { return a.find(ToKey(k)) >= 0 }

// GetString returns $a[$k] when it exists and is a string.
func (a *Array) GetString(k any) (string, bool) {
	v, _ := a.Get(k)
	s, ok := v.(string)
	return s, ok
}

// GetArray returns $a[$k] when it exists and is an array.
func (a *Array) GetArray(k any) (*Array, bool) {
	v, _ := a.Get(k)
	arr, ok := v.(*Array)
	return arr, ok
}

// Set performs $a[$k] = $v. An existing key keeps its position. k is
// coerced with ToKey; v is normalized (int becomes int64).
func (a *Array) Set(k, v any) { a.SetKey(ToKey(k), v) }

// SetKey performs $a[$k] = $v for an already coerced key.
func (a *Array) SetKey(k Key, v any) {
	v = normalize(v)
	a.detach()
	if p := a.find(k); p >= 0 {
		a.entries[p].v = v
		return
	}
	a.insert(k, v)
}

// Append performs $a[] = $v. Like PHP it fails (panics with PHP's Error
// message) when the next index is already in use, which needs a key of
// math.MaxInt64.
func (a *Array) Append(v any) {
	v = normalize(v)
	a.detach()
	h := a.next
	if h == noNextFree {
		h = 0
	}
	k := Key{i: h}
	if a.find(k) >= 0 {
		panic("Cannot add element to the array as the next element is already occupied")
	}
	a.insert(k, v)
}

// insert adds a key known to be absent.
func (a *Array) insert(k Key, v any) {
	if k.kind == kindInt && k.i >= a.next {
		if k.i < math.MaxInt64 {
			a.next = k.i + 1
		} else {
			a.next = math.MaxInt64
		}
	}
	pos := len(a.entries)
	if a.packed && (k.kind != kindInt || k.i != int64(pos)) {
		a.packed = false
		if pos >= linearMax {
			a.buildIndex()
		}
	}
	a.entries = append(a.entries, entry{k, v})
	a.live++
	switch {
	case a.indexed:
		a.indexAdd(k, pos)
	case !a.packed && len(a.entries) > linearMax:
		a.buildIndex()
	}
}

func (a *Array) indexAdd(k Key, pos int) {
	if k.kind == kindInt {
		if a.intIdx == nil {
			a.intIdx = make(map[int64]int32)
		}
		a.intIdx[k.i] = int32(pos) //nolint:gosec // arrays never reach 2^31 entries
	} else {
		if a.strIdx == nil {
			a.strIdx = make(map[string]int32)
		}
		a.strIdx[k.s] = int32(pos) //nolint:gosec // see above
	}
}

// indexLike indexes a, a copy of src's live entries in their order:
// copying src's index, when it has one that holds the same positions
// (src has no holes), costs much less than building one.
func (a *Array) indexLike(src *Array) {
	if !src.indexed || src.packed || len(src.entries) != src.live {
		a.buildIndex()
		return
	}
	a.indexed = true
	a.strIdx, a.intIdx = maps.Clone(src.strIdx), maps.Clone(src.intIdx)
}

func (a *Array) buildIndex() {
	a.indexed = true
	a.strIdx, a.intIdx = nil, nil
	for i := range a.entries {
		if k := a.entries[i].k; k.kind != kindDead {
			a.indexAdd(k, i)
		}
	}
}

// Delete performs unset($a[$k]). The next free index is not lowered.
func (a *Array) Delete(k any) { a.DeleteKey(ToKey(k)) }

// DeleteKey performs unset($a[$k]) for an already coerced key.
func (a *Array) DeleteKey(k Key) {
	p := a.find(k)
	if p < 0 {
		return
	}
	a.detach()
	a.live--
	if a.indexed {
		if k.kind == kindInt {
			delete(a.intIdx, k.i)
		} else {
			delete(a.strIdx, k.s)
		}
	}
	if p == len(a.entries)-1 {
		a.entries[p] = entry{}
		a.entries = a.entries[:p]
		return
	}
	a.entries[p] = entry{k: Key{kind: kindDead}}
	if a.packed {
		a.packed = false
		if len(a.entries) > linearMax {
			a.buildIndex()
		}
	}
	if dead := len(a.entries) - a.live; dead > 16 && dead > a.live {
		a.compact()
	}
}

// compact removes deleted entries into a fresh slice.
func (a *Array) compact() {
	es := make([]entry, 0, a.live+a.live/4)
	for _, e := range a.entries {
		if e.k.kind != kindDead {
			es = append(es, e)
		}
	}
	a.entries = es
	if a.indexed {
		a.buildIndex()
	}
}

// detach gives running iterators their own copy of the entries before a
// write, which is what makes All behave like foreach by value.
func (a *Array) detach() {
	if a.pins.Load() == 0 {
		return
	}
	es := make([]entry, len(a.entries), cap(a.entries)+1)
	copy(es, a.entries)
	a.entries = es
	a.unpin()
}

// unpin is detach for writers that are about to replace a.entries with a
// new slice anyway: running iterators keep the old one.
func (a *Array) unpin() {
	if a.pins.Load() != 0 {
		a.pins.Store(0)
		a.gen++
	}
}

// All iterates over the keys and values in order, like foreach. The loop
// sees the array as it was when the loop started even if the array is
// modified meanwhile.
func (a *Array) All() iter.Seq2[Key, any] {
	return func(yield func(Key, any) bool) {
		es := a.entries
		if len(es) == 0 {
			return
		}
		a.pins.Add(1)
		gen := a.gen
		defer func() {
			if a.gen == gen {
				a.pins.Add(-1)
			}
		}()
		for i := range es {
			if e := &es[i]; e.k.kind != kindDead {
				if !yield(e.k, e.v) {
					return
				}
			}
		}
	}
}

// Keys returns array_keys($a) as Go keys.
func (a *Array) Keys() []Key {
	ks := make([]Key, 0, a.live)
	for i := range a.entries {
		if k := a.entries[i].k; k.kind != kindDead {
			ks = append(ks, k)
		}
	}
	return ks
}

// Values returns array_values($a) as a Go slice.
func (a *Array) Values() []any {
	vs := make([]any, 0, a.live)
	for i := range a.entries {
		if e := &a.entries[i]; e.k.kind != kindDead {
			vs = append(vs, e.v)
		}
	}
	return vs
}

// First returns the first key and value (reset/array_key_first).
func (a *Array) First() (Key, any, bool) {
	for i := range a.entries {
		if e := &a.entries[i]; e.k.kind != kindDead {
			return e.k, e.v, true
		}
	}
	return Key{}, nil, false
}

// Last returns the last key and value (end/array_key_last).
func (a *Array) Last() (Key, any, bool) {
	for i := len(a.entries) - 1; i >= 0; i-- {
		if e := &a.entries[i]; e.k.kind != kindDead {
			return e.k, e.v, true
		}
	}
	return Key{}, nil, false
}

// IsList reports array_is_list($a): the keys are 0, 1, 2, ... in order.
func (a *Array) IsList() bool {
	if a.packed {
		return true
	}
	n := int64(0)
	for i := range a.entries {
		k := a.entries[i].k
		if k.kind == kindDead {
			continue
		}
		if k.kind != kindInt || k.i != n {
			return false
		}
		n++
	}
	return true
}

// Clone returns a deep copy: nested arrays and objects are cloned too, as
// PHP's copy-on-write would separate them on modification. The next free
// index is kept.
func (a *Array) Clone() *Array {
	c := &Array{
		entries: make([]entry, 0, a.live),
		next:    a.next,
		packed:  true,
		live:    a.live,
	}
	for i := range a.entries {
		e := a.entries[i]
		if e.k.kind == kindDead {
			continue
		}
		e.v = cloneValue(e.v)
		if c.packed && (e.k.kind != kindInt || e.k.i != int64(len(c.entries))) {
			c.packed = false
		}
		c.entries = append(c.entries, e)
	}
	if !c.packed && len(c.entries) > linearMax {
		c.indexLike(a)
	}
	return c
}

// ShallowClone returns a copy of a that shares its nested arrays and
// objects: what setting each of a's keys and values in turn into a new
// array gives (the same order, and the next free index of the int keys
// copied), without its cost.
func (a *Array) ShallowClone() *Array {
	c := &Array{
		entries: make([]entry, 0, a.live),
		next:    noNextFree,
		packed:  true,
		live:    a.live,
	}
	for i := range a.entries {
		e := a.entries[i]
		if e.k.kind == kindDead {
			continue
		}
		if e.k.kind == kindInt && e.k.i >= c.next {
			if e.k.i < math.MaxInt64 {
				c.next = e.k.i + 1
			} else {
				c.next = math.MaxInt64
			}
		}
		if c.packed && (e.k.kind != kindInt || e.k.i != int64(len(c.entries))) {
			c.packed = false
		}
		c.entries = append(c.entries, e)
	}
	if !c.packed && len(c.entries) > linearMax {
		c.indexLike(a)
	}
	return c
}

// cloneValue deep-copies arrays and objects.
func cloneValue(v any) any {
	switch v := v.(type) {
	case *Array:
		return v.Clone()
	case *Object:
		return v.Clone()
	}
	return v
}

// Pop ports array_pop: it removes and returns the last element and lowers
// the next free index when the popped key was the last appended one.
func (a *Array) Pop() (any, bool) {
	k, v, ok := a.Last()
	if !ok {
		return nil, false
	}
	if k.kind == kindInt && a.next != noNextFree && k.i == a.next-1 {
		a.next--
	}
	a.DeleteKey(k)
	return v, true
}

// Shift ports array_shift: it removes and returns the first element and
// renumbers the int keys from 0, keeping string keys.
func (a *Array) Shift() (any, bool) {
	k, v, ok := a.First()
	if !ok {
		return nil, false
	}
	a.DeleteKey(k)
	a.renumber(nil)
	// PHP sets the next free index to the number of int keys, even 0.
	a.next = 0
	for i := range a.entries {
		if a.entries[i].k.kind == kindInt {
			a.next++
		}
	}
	return v, true
}

// Unshift ports array_unshift: it prepends the values and renumbers the
// int keys, keeping string keys.
func (a *Array) Unshift(values ...any) {
	vs := make([]any, len(values))
	for i, v := range values {
		vs[i] = normalize(v)
	}
	a.renumber(vs)
}

// renumber rebuilds a as prefix followed by its elements with int keys
// renumbered from 0, which is what array_shift and array_unshift do.
func (a *Array) renumber(prefix []any) {
	old := a.entries
	a.unpin()
	n := NewArrayCap(len(prefix) + a.live)
	for _, v := range prefix {
		n.insert(Key{i: int64(len(n.entries))}, v)
	}
	for i := range old {
		e := old[i]
		switch e.k.kind {
		case kindInt:
			n.insert(Key{i: n.nextIndex()}, e.v)
		case kindStr:
			n.insert(e.k, e.v)
		}
	}
	a.replaceWith(n)
}

// nextIndex is the key Append would use.
func (a *Array) nextIndex() int64 {
	if a.next == noNextFree {
		return 0
	}
	return a.next
}

// replaceWith moves n's contents into a.
func (a *Array) replaceWith(n *Array) {
	a.entries = n.entries
	a.strIdx, a.intIdx = n.strIdx, n.intIdx
	a.live = n.live
	a.next = n.next
	a.packed = n.packed
	a.indexed = n.indexed
}
