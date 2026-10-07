// Ports the array functions of ext/standard/array.c that Composer uses.
// None of them modify their arguments except ArraySplice; the results
// share nested values with the inputs (PHP would copy them lazily), so
// Clone a nested array before modifying it in place.

package php

import (
	"math"
	"slices"
)

// ArrayMerge ports array_merge(...$arrays): string keys are overwritten by
// later arrays (keeping their first position), int keys are renumbered.
func ArrayMerge(arrays ...*Array) *Array {
	n := 0
	for _, a := range arrays {
		n += a.live
	}
	r := NewArrayCap(n)
	for _, a := range arrays {
		r.merge(a)
	}
	return r
}

func (a *Array) merge(src *Array) {
	for k, v := range src.All() {
		if k.kind == kindStr {
			a.SetKey(k, v)
		} else {
			a.Append(v)
		}
	}
}

// ArrayMergeRecursive ports array_merge_recursive(...$arrays).
func ArrayMergeRecursive(arrays ...*Array) *Array {
	r := NewArray()
	for _, a := range arrays {
		r.mergeRecursive(a)
	}
	return r
}

// mergeRecursive ports php_array_merge_recursive. Nested arrays of a that
// get merged into are cloned first, so the inputs are left untouched.
func (a *Array) mergeRecursive(src *Array) {
	for k, sv := range src.All() {
		if k.kind != kindStr {
			a.Append(sv)
			continue
		}
		dv, ok := a.GetKey(k)
		if !ok {
			a.SetKey(k, sv)
			continue
		}
		var dest *Array
		switch d := dv.(type) {
		case *Array:
			dest = d.Clone()
		case nil:
			dest = ListOf(nil)
		case *Object:
			dest = d.ToArray()
		default:
			dest = ListOf(d)
		}
		switch s := sv.(type) {
		case *Array:
			dest.mergeRecursive(s)
		case *Object:
			dest.mergeRecursive(s.ToArray())
		default:
			dest.Append(sv)
		}
		a.SetKey(k, dest)
	}
}

// ArrayReplace ports array_replace($array, ...$replacements).
func ArrayReplace(array *Array, replacements ...*Array) *Array {
	r := array.shallowCopy()
	for _, rep := range replacements {
		for k, v := range rep.All() {
			r.SetKey(k, v)
		}
	}
	return r
}

// ArrayReplaceRecursive ports array_replace_recursive($array,
// ...$replacements).
func ArrayReplaceRecursive(array *Array, replacements ...*Array) *Array {
	r := array.shallowCopy()
	for _, rep := range replacements {
		r.replaceRecursive(rep)
	}
	return r
}

func (a *Array) replaceRecursive(src *Array) {
	for k, sv := range src.All() {
		sa, ok := sv.(*Array)
		if ok {
			if dv, found := a.GetKey(k); found {
				if da, isArr := dv.(*Array); isArr {
					d := da.shallowCopy()
					d.replaceRecursive(sa)
					a.SetKey(k, d)
					continue
				}
			}
		}
		a.SetKey(k, sv)
	}
}

// shallowCopy copies the table but shares the values, including the next
// free index.
func (a *Array) shallowCopy() *Array {
	r := NewArrayCap(a.live)
	for k, v := range a.All() {
		r.insert(k, v)
	}
	r.next = a.next
	return r
}

// ArrayUnique ports array_unique($array) with the default SORT_STRING:
// the first of the values with equal string forms is kept, with its key.
func ArrayUnique(array *Array) *Array {
	if array.live <= 1 {
		return array.shallowCopy()
	}
	r := NewArrayCap(array.live)
	seen := make(map[string]struct{}, array.live)
	for k, v := range array.All() {
		s := ToString(v)
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		r.insert(k, v)
	}
	return r
}

// ArrayKeys ports array_keys($array) as a list of int and string values.
func ArrayKeys(array *Array) *Array {
	r := NewArrayCap(array.live)
	for k := range array.All() {
		r.Append(k.Value())
	}
	return r
}

// ArrayValues ports array_values($array).
func ArrayValues(array *Array) *Array {
	r := NewArrayCap(array.live)
	for _, v := range array.All() {
		r.Append(v)
	}
	return r
}

// ArrayFlip ports array_flip($array). Values other than ints and strings
// are skipped (PHP warns "Can only flip string and integer values, entry
// skipped").
func ArrayFlip(array *Array) *Array {
	r := NewArrayCap(array.live)
	for k, v := range array.All() {
		switch v := v.(type) {
		case int64:
			r.SetKey(IntKey(v), k.Value())
		case string:
			r.SetKey(StrKey(v), k.Value())
		}
	}
	return r
}

// ArraySlice ports array_slice($array, $offset, $length, $preserveKeys).
// Pass math.MaxInt as length for PHP's null (to the end).
func ArraySlice(array *Array, offset, length int, preserveKeys bool) *Array {
	n := array.live
	if offset > n {
		return NewArray()
	}
	if offset < 0 {
		offset = max(n+offset, 0)
	}
	switch {
	case length == math.MaxInt:
		length = n - offset
	case length < 0:
		length = n - offset + length
	case offset+length > n:
		length = n - offset
	}
	r := NewArray()
	if length <= 0 {
		return r
	}
	pos := 0
	for k, v := range array.All() {
		if pos >= offset+length {
			break
		}
		if pos >= offset {
			if k.kind == kindStr || preserveKeys {
				r.SetKey(k, v)
			} else {
				r.Append(v)
			}
		}
		pos++
	}
	return r
}

// ArraySplice ports array_splice(&$array, $offset, $length, $replacement):
// it removes length elements from offset (math.MaxInt for PHP's null),
// inserts the replacement values, renumbers int keys and returns the
// removed elements.
func ArraySplice(array *Array, offset, length int, replacement ...any) *Array {
	n := array.live
	if offset < 0 {
		offset = max(n+offset, 0)
	} else if offset > n {
		offset = n
	}
	switch {
	case length == math.MaxInt:
		length = n - offset
	case length < 0:
		length = max(n-offset+length, 0)
	case offset+length > n:
		length = n - offset
	}
	removed := NewArray()
	out := NewArrayCap(n - length + len(replacement))
	pos := 0
	add := func(k Key, v any) {
		if k.kind == kindStr {
			out.SetKey(k, v)
		} else {
			out.Append(v)
		}
	}
	for k, v := range array.All() {
		if pos == offset {
			for _, rv := range replacement {
				out.Append(rv)
			}
		}
		if pos >= offset && pos < offset+length {
			if k.kind == kindStr {
				removed.SetKey(k, v)
			} else {
				removed.Append(v)
			}
		} else {
			add(k, v)
		}
		pos++
	}
	if offset >= pos {
		for _, rv := range replacement {
			out.Append(rv)
		}
	}
	array.unpin()
	array.replaceWith(out)
	return removed
}

// ArrayChunk ports array_chunk($array, $length, $preserveKeys).
func ArrayChunk(array *Array, length int, preserveKeys bool) *Array {
	if length < 1 {
		panic("array_chunk(): Argument #2 ($length) must be greater than 0")
	}
	r := NewArray()
	var chunk *Array
	for k, v := range array.All() {
		if chunk == nil {
			chunk = NewArrayCap(min(length, array.live))
		}
		if preserveKeys {
			chunk.SetKey(k, v)
		} else {
			chunk.Append(v)
		}
		if chunk.live == length {
			r.Append(chunk)
			chunk = nil
		}
	}
	if chunk != nil {
		r.Append(chunk)
	}
	return r
}

// ArraySearch ports array_search($needle, $haystack, $strict): the key of
// the first matching value.
func ArraySearch(needle any, haystack *Array, strict bool) (Key, bool) {
	for k, v := range haystack.All() {
		if valueMatches(v, needle, strict) {
			return k, true
		}
	}
	return Key{}, false
}

// InArray ports in_array($needle, $haystack, $strict).
func InArray(needle any, haystack *Array, strict bool) bool {
	_, ok := ArraySearch(needle, haystack, strict)
	return ok
}

func valueMatches(v, needle any, strict bool) bool {
	if strict {
		return StrictEquals(v, needle)
	}
	return LooseEquals(v, needle)
}

// ArrayReverse ports array_reverse($array, $preserveKeys). String keys are
// always kept.
func ArrayReverse(array *Array, preserveKeys bool) *Array {
	r := NewArrayCap(array.live)
	for _, e := range slices.Backward(array.entries) {
		switch {
		case e.k.kind == kindDead:
		case e.k.kind == kindStr || preserveKeys:
			r.SetKey(e.k, e.v)
		default:
			r.Append(e.v)
		}
	}
	return r
}

// ArrayDiff ports array_diff($array, ...$arrays): the entries of array
// whose string form occurs in none of the others, keys kept.
func ArrayDiff(array *Array, others ...*Array) *Array {
	set := stringSet(others)
	if array.live == 0 {
		return NewArray()
	}
	if len(set) == 0 {
		return array.shallowCopy()
	}
	if array.live == 1 {
		// PHP returns the input array itself when nothing is removed.
		if _, v, _ := array.First(); hasString(set, v) {
			return NewArray()
		}
		return array.shallowCopy()
	}
	r := NewArray()
	for k, v := range array.All() {
		if !hasString(set, v) {
			r.insert(k, v)
		}
	}
	return r
}

func hasString(set map[string]struct{}, v any) bool {
	_, found := set[ToString(v)]
	return found
}

// ArrayIntersect ports array_intersect($array, ...$arrays): the entries of
// array whose string form occurs in every other array, keys kept.
func ArrayIntersect(array *Array, others ...*Array) *Array {
	r := array.shallowCopy()
	for _, o := range others {
		set := stringSet([]*Array{o})
		for k, v := range r.All() {
			if !hasString(set, v) {
				r.DeleteKey(k)
			}
		}
	}
	return r
}

func stringSet(arrays []*Array) map[string]struct{} {
	set := make(map[string]struct{})
	for _, a := range arrays {
		for _, v := range a.All() {
			set[ToString(v)] = struct{}{}
		}
	}
	return set
}

// ArrayDiffKey ports array_diff_key($array, ...$arrays).
func ArrayDiffKey(array *Array, others ...*Array) *Array {
	r := NewArray()
outer:
	for k, v := range array.All() {
		for _, o := range others {
			if o.find(k) >= 0 {
				continue outer
			}
		}
		r.insert(k, v)
	}
	return r
}

// ArrayDiffAssoc ports array_diff_assoc($array, ...$arrays): the entries
// of array whose key is in none of the others with the same string value.
func ArrayDiffAssoc(array *Array, others ...*Array) *Array {
	r := NewArray()
outer:
	for k, v := range array.All() {
		for _, o := range others {
			if ov, ok := o.GetKey(k); ok && ToString(ov) == ToString(v) {
				continue outer
			}
		}
		r.insert(k, v)
	}
	return r
}

// ArrayPad ports array_pad($array, $length, $value): padded on the right
// for a positive length, on the left for a negative one; int keys are
// renumbered when padding happens.
func ArrayPad(array *Array, length int, value any) *Array {
	n := array.live
	pad := length
	if pad < 0 {
		pad = -pad
	}
	if pad <= n {
		return array.shallowCopy()
	}
	value = normalize(value)
	r := NewArrayCap(pad)
	if length < 0 {
		for range pad - n {
			r.Append(value)
		}
	}
	for k, v := range array.All() {
		if k.kind == kindStr {
			r.SetKey(k, v)
		} else {
			r.Append(v)
		}
	}
	if length > 0 {
		for range pad - n {
			r.Append(value)
		}
	}
	return r
}

// ArrayIntersectKey ports array_intersect_key($array, ...$arrays).
func ArrayIntersectKey(array *Array, others ...*Array) *Array {
	r := NewArray()
outer:
	for k, v := range array.All() {
		for _, o := range others {
			if o.find(k) < 0 {
				continue outer
			}
		}
		r.insert(k, v)
	}
	return r
}

// valueKey converts a value to a key as array_combine and array_fill_keys
// do: ints stay ints, everything else goes through its string form.
func valueKey(v any) Key {
	switch v := v.(type) {
	case int64:
		return IntKey(v)
	case int:
		return IntKey(int64(v))
	}
	return StrKey(ToString(v))
}

// ArrayCombine ports array_combine($keys, $values). It panics with PHP's
// ValueError message when the counts differ.
func ArrayCombine(keys, values *Array) *Array {
	if keys.live != values.live {
		panic("array_combine(): Argument #1 ($keys) and argument #2 ($values) must have the same number of elements")
	}
	r := NewArrayCap(keys.live)
	vs := values.Values()
	i := 0
	for _, k := range keys.All() {
		r.SetKey(valueKey(k), vs[i])
		i++
	}
	return r
}

// ArrayFillKeys ports array_fill_keys($keys, $value).
func ArrayFillKeys(keys *Array, value any) *Array {
	r := NewArrayCap(keys.live)
	for _, k := range keys.All() {
		r.SetKey(valueKey(k), value)
	}
	return r
}

// ArrayFilter ports array_filter($array, $callback, ARRAY_FILTER_USE_BOTH);
// a nil callback keeps the truthy values. Keys are kept.
func ArrayFilter(array *Array, keep func(k Key, v any) bool) *Array {
	r := NewArray()
	for k, v := range array.All() {
		if keep == nil && ToBool(v) || keep != nil && keep(k, v) {
			r.insert(k, v)
		}
	}
	return r
}

// ArrayMap ports array_map($callback, $array) for a single array: keys
// are kept.
func ArrayMap(array *Array, fn func(v any) any) *Array {
	r := NewArrayCap(array.live)
	for k, v := range array.All() {
		r.insert(k, normalize(fn(v)))
	}
	return r
}
