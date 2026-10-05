// Ports ext/standard/strnatcmp.c (strnatcmp_ex).

package php

// Strnatcmp ports strnatcmp(): natural order comparison ("img12" after
// "img2"), returning -1, 0 or 1.
func Strnatcmp(a, b string) int { return strnatcmpEx(a, b, false) }

// Strnatcasecmp ports strnatcasecmp(): Strnatcmp ignoring ASCII case.
func Strnatcasecmp(a, b string) int { return strnatcmpEx(a, b, true) }

func strnatcmpEx(a, b string, foldCase bool) int {
	if len(a) == 0 || len(b) == 0 {
		return cmpInt64(int64(len(a)), int64(len(b)))
	}
	// at mimics reading the C string's NUL terminator past the end.
	at := func(s string, i int) byte {
		if i < len(s) {
			return s[i]
		}
		return 0
	}
	ap, bp := 0, 0
	ca, cb := a[0], b[0]

	// Skip over leading zeros.
	for ca == '0' && ap+1 < len(a) && isDigit(a[ap+1]) {
		ap++
		ca = a[ap]
	}
	for cb == '0' && bp+1 < len(b) && isDigit(b[bp+1]) {
		bp++
		cb = b[bp]
	}

	for {
		// Skip consecutive whitespace.
		for isCSpace(ca) {
			ap++
			ca = at(a, ap)
		}
		for isCSpace(cb) {
			bp++
			cb = at(b, bp)
		}

		// Process a run of digits.
		if isDigit(ca) && isDigit(cb) {
			var result int
			if ca == '0' || cb == '0' {
				result = natCompareLeft(a, &ap, b, &bp)
			} else {
				result = natCompareRight(a, &ap, b, &bp)
			}
			switch {
			case result != 0:
				return result
			case ap == len(a) && bp == len(b):
				return 0
			case ap == len(a):
				return -1
			case bp == len(b):
				return 1
			}
			ca, cb = a[ap], b[bp]
		}

		if foldCase {
			ca, cb = upperASCII(ca), upperASCII(cb)
		}
		if ca < cb {
			return -1
		} else if ca > cb {
			return 1
		}

		ap++
		bp++
		switch {
		case ap >= len(a) && bp >= len(b):
			return 0
		case ap >= len(a):
			return -1
		case bp >= len(b):
			return 1
		}
		ca, cb = a[ap], b[bp]
	}
}

// natCompareRight compares two right-aligned numbers: the longest run of
// digits wins, else the first differing digit.
func natCompareRight(a string, ap *int, b string, bp *int) int {
	bias := 0
	for ; ; *ap, *bp = *ap+1, *bp+1 {
		aDone := *ap == len(a) || !isDigit(a[*ap])
		bDone := *bp == len(b) || !isDigit(b[*bp])
		switch {
		case aDone && bDone:
			return bias
		case aDone:
			return -1
		case bDone:
			return 1
		case a[*ap] < b[*bp]:
			if bias == 0 {
				bias = -1
			}
		case a[*ap] > b[*bp]:
			if bias == 0 {
				bias = 1
			}
		}
	}
}

// natCompareLeft compares two left-aligned numbers (with a leading zero):
// the first differing digit wins.
func natCompareLeft(a string, ap *int, b string, bp *int) int {
	for ; ; *ap, *bp = *ap+1, *bp+1 {
		aDone := *ap == len(a) || !isDigit(a[*ap])
		bDone := *bp == len(b) || !isDigit(b[*bp])
		switch {
		case aDone && bDone:
			return 0
		case aDone:
			return -1
		case bDone:
			return 1
		case a[*ap] < b[*bp]:
			return -1
		case a[*ap] > b[*bp]:
			return 1
		}
	}
}

// isCSpace is isspace() in the C locale.
func isCSpace(c byte) bool {
	return c == ' ' || (c >= '\t' && c <= '\r')
}

func upperASCII(c byte) byte {
	if c >= 'a' && c <= 'z' {
		return c - ('a' - 'A')
	}
	return c
}

func lowerASCII(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + ('a' - 'A')
	}
	return c
}
