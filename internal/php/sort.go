// Ports Zend/zend_sort.c (zend_sort, zend_insert_sort) and the sort
// functions of ext/standard/array.c (sort, rsort, usort, uasort, uksort,
// asort, arsort, ksort, krsort) with their comparators and PHP 8's stable
// fallback on the original order.

package php

// SortFlag is the $flags argument of the sort functions.
type SortFlag int

// The sort flags of ext/standard/php_array.h. SORT_LOCALE_STRING compares
// like SORT_STRING, as in the C locale PHP runs with.
const (
	SortRegular      SortFlag = 0
	SortNumeric      SortFlag = 1
	SortString       SortFlag = 2
	SortLocaleString SortFlag = 5
	SortNatural      SortFlag = 6
	SortFlagCase     SortFlag = 8
)

// Sort ports sort($a, $flags): values ascending, keys renumbered.
func Sort(a *Array, flags SortFlag) { sortArray(a, dataCompare(flags, false), true) }

// Rsort ports rsort($a, $flags): values descending, keys renumbered.
func Rsort(a *Array, flags SortFlag) { sortArray(a, dataCompare(flags, true), true) }

// Asort ports asort($a, $flags): values ascending, keys kept.
func Asort(a *Array, flags SortFlag) { sortArray(a, dataCompare(flags, false), false) }

// Arsort ports arsort($a, $flags): values descending, keys kept.
func Arsort(a *Array, flags SortFlag) { sortArray(a, dataCompare(flags, true), false) }

// Ksort ports ksort($a, $flags): keys ascending.
func Ksort(a *Array, flags SortFlag) { sortArray(a, keyCompare(flags, false), false) }

// Krsort ports krsort($a, $flags): keys descending.
func Krsort(a *Array, flags SortFlag) { sortArray(a, keyCompare(flags, true), false) }

// Usort ports usort($a, $cmp): keys renumbered. Only the sign of cmp's
// result matters.
func Usort(a *Array, cmp func(x, y any) int) {
	sortArray(a, func(x, y *entry) int { return sign(cmp(x.v, y.v)) }, true)
}

// Uasort ports uasort($a, $cmp): keys kept.
func Uasort(a *Array, cmp func(x, y any) int) {
	sortArray(a, func(x, y *entry) int { return sign(cmp(x.v, y.v)) }, false)
}

// Uksort ports uksort($a, $cmp), comparing keys.
func Uksort(a *Array, cmp func(x, y Key) int) {
	sortArray(a, func(x, y *entry) int { return sign(cmp(x.k, y.k)) }, false)
}

// SortSlice sorts a Go slice with zend_sort and PHP 8's stable fallback,
// exactly as usort would order the same values; for porting usort calls
// on lists that are kept as Go slices.
func SortSlice[T any](s []T, cmp func(x, y T) int) {
	items := make([]sortItem[T], len(s))
	for i, v := range s {
		items[i] = sortItem[T]{v, i}
	}
	zendSort(items, func(x, y *sortItem[T]) int {
		if r := sign(cmp(x.v, y.v)); r != 0 {
			return r
		}
		return cmpInt(x.ord, y.ord)
	})
	for i := range items {
		s[i] = items[i].v
	}
}

type sortItem[T any] struct {
	v   T
	ord int
}

func sign(r int) int {
	switch {
	case r > 0:
		return 1
	case r < 0:
		return -1
	}
	return 0
}

func cmpInt(a, b int) int {
	switch {
	case a > b:
		return 1
	case a < b:
		return -1
	}
	return 0
}

// sortArray ports zend_hash_sort_internal.
func sortArray(a *Array, cmp func(x, y *entry) int, renumber bool) {
	if a.live == 0 || a.live == 1 && !renumber {
		return
	}
	items := make([]sortItem[entry], 0, a.live)
	for _, e := range a.entries {
		if e.k.kind != kindDead {
			items = append(items, sortItem[entry]{e, len(items)})
		}
	}
	zendSort(items, func(x, y *sortItem[entry]) int {
		if r := cmp(&x.v, &y.v); r != 0 {
			return r
		}
		return cmpInt(x.ord, y.ord)
	})

	a.unpin()
	es := make([]entry, len(items), len(items)+len(items)/4)
	packed := true
	for i := range items {
		es[i] = items[i].v
		if renumber {
			es[i].k = Key{i: int64(i)}
		} else if es[i].k.kind != kindInt || es[i].k.i != int64(i) {
			packed = false
		}
	}
	a.entries = es
	a.packed = packed
	a.indexed = false
	a.strIdx, a.intIdx = nil, nil
	if renumber {
		a.next = int64(len(es))
	} else if !packed && len(es) > linearMax {
		a.buildIndex()
	}
}

// dataCompare returns php_get_data_compare_func's comparator (without the
// stable fallback, which sortArray adds).
func dataCompare(flags SortFlag, reverse bool) func(x, y *entry) int {
	var cmp func(x, y any) int
	switch flags &^ SortFlagCase {
	case SortNumeric:
		cmp = func(x, y any) int { return threeway(ToFloat(x), ToFloat(y)) }
	case SortString, SortLocaleString:
		if flags&SortFlagCase != 0 && flags&^SortFlagCase == SortString {
			cmp = func(x, y any) int { return strcasecmpASCII(ToString(x), ToString(y)) }
		} else {
			cmp = func(x, y any) int { return binaryStrcmp(ToString(x), ToString(y)) }
		}
	case SortNatural:
		fold := flags&SortFlagCase != 0
		cmp = func(x, y any) int { return strnatcmpEx(ToString(x), ToString(y), fold) }
	default:
		cmp = Compare
	}
	if reverse {
		return func(x, y *entry) int { return -cmp(x.v, y.v) }
	}
	return func(x, y *entry) int { return cmp(x.v, y.v) }
}

// keyCompare returns php_get_key_compare_func's comparator.
func keyCompare(flags SortFlag, reverse bool) func(x, y *entry) int {
	var cmp func(x, y Key) int
	switch flags &^ SortFlagCase {
	case SortNumeric:
		cmp = func(x, y Key) int {
			if x.kind == kindInt && y.kind == kindInt {
				return cmpIntKeys(x.i, y.i)
			}
			return threeway(keyFloat(x), keyFloat(y))
		}
	case SortString, SortLocaleString:
		if flags&SortFlagCase != 0 && flags&^SortFlagCase == SortString {
			cmp = func(x, y Key) int { return strcasecmpASCII(x.String(), y.String()) }
		} else {
			cmp = func(x, y Key) int { return binaryStrcmp(x.String(), y.String()) }
		}
	case SortNatural:
		fold := flags&SortFlagCase != 0
		if reverse {
			// PHP swaps the operands here instead of negating.
			return func(x, y *entry) int { return strnatcmpEx(y.k.String(), x.k.String(), fold) }
		}
		return func(x, y *entry) int { return strnatcmpEx(x.k.String(), y.k.String(), fold) }
	default:
		cmp = func(x, y Key) int {
			switch {
			case x.kind == kindInt && y.kind == kindInt:
				return cmpIntKeys(x.i, y.i)
			case x.kind == kindStr && y.kind == kindStr:
				return smartStrcmp(x.s, y.s)
			}
			return Compare(x.Value(), y.Value())
		}
	}
	if reverse {
		return func(x, y *entry) int { return -cmp(x.k, y.k) }
	}
	return func(x, y *entry) int { return cmp(x.k, y.k) }
}

// cmpIntKeys never returns 0: keys are unique, so PHP skips the check.
func cmpIntKeys(a, b int64) int {
	if a > b {
		return 1
	}
	return -1
}

func keyFloat(k Key) float64 {
	if k.kind == kindInt {
		return float64(k.i)
	}
	return strtod(k.s)
}

// strcasecmpASCII is zend_binary_strcasecmp normalized to -1, 0, 1.
func strcasecmpASCII(a, b string) int {
	n := min(len(a), len(b))
	for i := range n {
		ca, cb := lowerASCII(a[i]), lowerASCII(b[i])
		if ca != cb {
			return cmpInt(int(ca), int(cb))
		}
	}
	return cmpInt(len(a), len(b))
}

// zendSort ports zend_sort: insertion sort up to 16 elements, otherwise a
// hybrid quick sort. The exact algorithm matters, because comparisons in
// PHP are not always transitive (mixed types), so a different algorithm
// could order the same input differently.
func zendSort[T any](s []T, cmp func(x, y *T) int) {
	for {
		n := len(s)
		if n <= 16 {
			insertSort(s, cmp)
			return
		}
		offset := n >> 1
		pivot := offset
		if n>>10 != 0 {
			delta := offset >> 1
			sort5(s, 0, delta, pivot, pivot+delta, n-1, cmp)
		} else {
			sort3(s, 0, pivot, n-1, cmp)
		}
		s[1], s[pivot] = s[pivot], s[1]
		pivot = 1
		i := pivot + 1
		j := n - 1
	partition:
		for {
			for cmp(&s[pivot], &s[i]) > 0 {
				i++
				if i == j {
					break partition
				}
			}
			j--
			if j == i {
				break partition
			}
			for cmp(&s[j], &s[pivot]) > 0 {
				j--
				if j == i {
					break partition
				}
			}
			s[i], s[j] = s[j], s[i]
			i++
			if i == j {
				break partition
			}
		}
		s[pivot], s[i-1] = s[i-1], s[pivot]
		if i-1 < n-i {
			zendSort(s[:i-1], cmp)
			s = s[i:]
		} else {
			zendSort(s[i:], cmp)
			s = s[:i-1]
		}
	}
}

func sort2[T any](s []T, a, b int, cmp func(x, y *T) int) {
	if cmp(&s[a], &s[b]) > 0 {
		s[a], s[b] = s[b], s[a]
	}
}

func sort3[T any](s []T, a, b, c int, cmp func(x, y *T) int) {
	if cmp(&s[a], &s[b]) <= 0 {
		if cmp(&s[b], &s[c]) <= 0 {
			return
		}
		s[b], s[c] = s[c], s[b]
		if cmp(&s[a], &s[b]) > 0 {
			s[a], s[b] = s[b], s[a]
		}
		return
	}
	if cmp(&s[c], &s[b]) <= 0 {
		s[a], s[c] = s[c], s[a]
		return
	}
	s[a], s[b] = s[b], s[a]
	if cmp(&s[b], &s[c]) > 0 {
		s[b], s[c] = s[c], s[b]
	}
}

func sort4[T any](s []T, a, b, c, d int, cmp func(x, y *T) int) {
	sort3(s, a, b, c, cmp)
	if cmp(&s[c], &s[d]) > 0 {
		s[c], s[d] = s[d], s[c]
		if cmp(&s[b], &s[c]) > 0 {
			s[b], s[c] = s[c], s[b]
			if cmp(&s[a], &s[b]) > 0 {
				s[a], s[b] = s[b], s[a]
			}
		}
	}
}

func sort5[T any](s []T, a, b, c, d, e int, cmp func(x, y *T) int) {
	sort4(s, a, b, c, d, cmp)
	if cmp(&s[d], &s[e]) > 0 {
		s[d], s[e] = s[e], s[d]
		if cmp(&s[c], &s[d]) > 0 {
			s[c], s[d] = s[d], s[c]
			if cmp(&s[b], &s[c]) > 0 {
				s[b], s[c] = s[c], s[b]
				if cmp(&s[a], &s[b]) > 0 {
					s[a], s[b] = s[b], s[a]
				}
			}
		}
	}
}

// insertSort ports zend_insert_sort.
func insertSort[T any](s []T, cmp func(x, y *T) int) {
	n := len(s)
	switch n {
	case 0, 1:
		return
	case 2:
		sort2(s, 0, 1, cmp)
		return
	case 3:
		sort3(s, 0, 1, 2, cmp)
		return
	case 4:
		sort4(s, 0, 1, 2, 3, cmp)
		return
	case 5:
		sort5(s, 0, 1, 2, 3, 4, cmp)
		return
	}
	shift := func(i, j int) {
		for k := i; k > j; k-- {
			s[k], s[k-1] = s[k-1], s[k]
		}
	}
	for i := 1; i < 6; i++ {
		j := i - 1
		if cmp(&s[j], &s[i]) <= 0 {
			continue
		}
		for j != 0 {
			j--
			if cmp(&s[j], &s[i]) <= 0 {
				j++
				break
			}
		}
		shift(i, j)
	}
	for i := 6; i < n; i++ {
		j := i - 1
		if cmp(&s[j], &s[i]) <= 0 {
			continue
		}
		for {
			j -= 2
			if cmp(&s[j], &s[i]) <= 0 {
				j++
				if cmp(&s[j], &s[i]) <= 0 {
					j++
				}
				break
			}
			if j == 0 {
				break
			}
			if j == 1 {
				j--
				if cmp(&s[i], &s[j]) > 0 {
					j++
				}
				break
			}
		}
		shift(i, j)
	}
}
