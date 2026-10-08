// Package capacity computes the capacity hints passed to make: the size a
// buffer built with append will probably reach, from lengths whose sum or
// product could overflow int. A hint never overflows; append grows the
// buffer past it when needed.
package capacity

import "math"

// Sum is the hint for a buffer holding terms (lengths, so non-negative):
// their sum or, when that overflows int, the largest term. It is at least
// every term, so make([]T, term, Sum(...)) cannot panic.
func Sum(terms ...int) int {
	total, largest, overflow := 0, 0, false

	for _, t := range terms {
		t = max(t, 0)
		largest = max(largest, t)

		if t > math.MaxInt-total {
			overflow = true

			continue
		}

		total += t
	}

	if overflow {
		return largest
	}

	return total
}

// Product is the hint for count items of size bytes each: count*size, or 0
// when either is negative or the product overflows int.
func Product(count, size int) int {
	if count <= 0 || size <= 0 || size > math.MaxInt/count {
		return 0
	}

	return count * size
}
