// Ports php-src 8.4 ext/standard/versioning.c: version_compare(),
// php_version_compare(), php_canonicalize_version() and
// compare_special_version_forms(), which composer/semver's Constraint,
// Comparator and Intervals rely on.

package semver

import (
	"github.com/stubbedev/maestro/internal/php"
)

// VersionCompare ports PHP's version_compare($version1, $version2) without
// an operator: it returns -1, 0 or 1.
//
// Like PHP, which works on C strings here, both arguments end at their first
// NUL byte. The comparison is allocation-free for versions up to 31 bytes.
func VersionCompare(version1, version2 string) int {
	return phpVersionCompare(cString(version1), cString(version2))
}

// VersionCompareOp ports PHP's version_compare($version1, $version2,
// $operator). Valid operators are <, lt, <=, le, >, gt, >=, ge, ==, =, eq,
// !=, <> and ne; any other operator returns the ValueError PHP throws.
func VersionCompareOp(version1, version2, operator string) (bool, error) {
	cmp := VersionCompare(version1, version2)
	switch operator {
	case "<", "lt":
		return cmp == -1, nil
	case "<=", "le":
		return cmp != 1, nil
	case ">", "gt":
		return cmp == 1, nil
	case ">=", "ge":
		return cmp != -1, nil
	case "==", "=", "eq":
		return cmp == 0, nil
	case "!=", "<>", "ne":
		return cmp != 0, nil
	}

	return false, &php.EngineError{Class: php.ClassValueError, Message: "version_compare(): Argument #3 ($operator) must be a valid comparison operator"}
}

// versionCompareOp is version_compare() with one of Constraint's operators.
func versionCompareOp(version1, version2 string, op Op) bool {
	return opResult(VersionCompare(version1, version2), op)
}

// opResult is what version_compare() with op returns for the result cmp
// of version_compare() without an operator.
func opResult(cmp int, op Op) bool {
	switch op {
	case OpLT:
		return cmp == -1
	case OpLE:
		return cmp != 1
	case OpGT:
		return cmp == 1
	case OpGE:
		return cmp != -1
	case OpEQ:
		return cmp == 0
	default: // OpNE
		return cmp != 0
	}
}

// preparedVersion is a version compared with many others (a compiled
// constraint's): its canonical form is computed once rather than on every
// comparison.
type preparedVersion struct {
	version string
	// canonical is the buffer php_version_compare compares for version;
	// nil when version is empty as a C string
	canonical []byte
}

func prepareVersion(version string) preparedVersion {
	p := preparedVersion{version: version}
	if v := cString(version); v != "" {
		p.canonical = canonicalVersion(nil, v)
	}

	return p
}

// opWith is versionCompareOp(v, p.version, op).
func (p *preparedVersion) opWith(v string, op Op) bool {
	v = cString(v)
	if p.canonical == nil || v == "" {
		return versionCompareOp(v, p.version, op)
	}
	var buf [canonicalBufSize]byte

	return opResult(compareCanonical(canonicalVersion(buf[:0], v), p.canonical), op)
}

// opBefore is versionCompareOp(p.version, v, op).
func (p *preparedVersion) opBefore(v string, op Op) bool {
	v = cString(v)
	if p.canonical == nil || v == "" {
		return versionCompareOp(p.version, v, op)
	}
	var buf [canonicalBufSize]byte

	return opResult(compareCanonical(p.canonical, canonicalVersion(buf[:0], v)), op)
}

// byteString is a version as php_version_compare sees it: the caller's
// strings, or the remainder of a canonical buffer when it recurses.
type byteString interface{ ~string | ~[]byte }

// cString truncates s at its first NUL byte, as strlen() would.
func cString[S byteString](s S) S {
	for i := range len(s) {
		if s[i] == 0 {
			return s[:i]
		}
	}

	return s
}

// numberForm is what php_version_compare substitutes for a numeric part
// compared against a special form ("#N#").
const numberForm = "#N#"

// canonicalBufSize is the stack buffer for canonical versions; a version of
// n bytes needs at most 2n bytes.
const canonicalBufSize = 64

// phpVersionCompare ports php_version_compare().
func phpVersionCompare[A, B byteString](orig1 A, orig2 B) int {
	if len(orig1) == 0 || len(orig2) == 0 {
		switch {
		case len(orig1) == 0 && len(orig2) == 0:
			return 0
		case len(orig1) != 0:
			return 1
		default:
			return -1
		}
	}

	var buf1, buf2 [canonicalBufSize]byte

	return compareCanonical(canonicalVersion(buf1[:0], orig1), canonicalVersion(buf2[:0], orig2))
}

// canonicalVersion returns the buffer php_version_compare compares: the
// version itself when it starts with '#', its canonical form otherwise.
func canonicalVersion[S byteString](dst []byte, version S) []byte {
	if version[0] == '#' {
		for i := range len(version) {
			dst = append(dst, version[i])
		}

		return dst
	}

	return canonicalize(dst, version)
}

// canonicalize ports php_canonicalize_version(): it turns "-", "_", "+" and
// any other non-alphanumeric byte into ".", inserts "." between runs of
// digits and non-digits, and drops a trailing ".". version is not empty.
func canonicalize[S byteString](dst []byte, version S) []byte {
	if plainVersion(version) {
		for i := range len(version) {
			dst = append(dst, version[i])
		}

		return dst
	}

	return canonicalizeBytes(dst, version)
}

// canonicalizeBytes is canonicalize, byte by byte as the C code goes.
func canonicalizeBytes[S byteString](dst []byte, version S) []byte {
	isdig := func(c byte) bool { return isDigit(c) }
	isndig := func(c byte) bool { return !isDigit(c) && c != '.' }

	lp := version[0]
	q := append(dst, lp)
	for i := 1; i < len(version); i++ {
		c := version[i]
		lq := q[len(q)-1]
		switch {
		case c == '-' || c == '_' || c == '+':
			if lq != '.' {
				q = append(q, '.')
			}
		case (isndig(lp) && isdig(c)) || (isdig(lp) && isndig(c)):
			if lq != '.' {
				q = append(q, '.')
			}
			q = append(q, c)
		case !isAlnum(c):
			if lq != '.' {
				q = append(q, '.')
			}
		default:
			q = append(q, c)
		}
		lp = c
	}
	if q[len(q)-1] == '.' {
		q = q[:len(q)-1]
	}

	return q
}

// plainVersion tells whether version is runs of digits separated by
// single dots ("1.2.3.0", the form of most normalized versions), which
// canonicalize returns as it is: a digit after a digit or a dot is
// copied, a dot after a digit is one, and there is no trailing dot.
func plainVersion[S byteString](version S) bool {
	if len(version) == 0 || !isDigit(version[0]) || version[len(version)-1] == '.' {
		return false
	}
	for i := 1; i < len(version); i++ {
		switch c := version[i]; {
		case isDigit(c):
		case c == '.' && version[i-1] != '.':
		default:
			return false
		}
	}

	return true
}

// compareCanonical is the token loop of php_version_compare(). The C code
// walks both buffers with strchr(), overwriting each "." with NUL; here a
// token is the slice up to the next ".", and its first byte is NUL (0) when
// it is empty.
func compareCanonical(v1, v2 []byte) int {
	p1, p2 := 0, 0
	// n1 and n2 mirror the C pointers of the same name: they start out
	// non-NULL and become NULL once a token has no "." after it.
	n1, n2 := true, true
	compare := 0
	for p1 < len(v1) && p2 < len(v2) && n1 && n2 {
		e1, e2 := indexDot(v1, p1), indexDot(v2, p2)
		n1, n2 = e1 >= 0, e2 >= 0
		if !n1 {
			e1 = len(v1)
		}
		if !n2 {
			e2 = len(v2)
		}
		t1, t2 := v1[p1:e1], v2[p2:e2]
		d1, d2 := isDigit(firstByte(t1)), isDigit(firstByte(t2))
		switch {
		case d1 && d2:
			compare = sign(strtol(t1) - strtol(t2))
		case !d1 && !d2:
			compare = compareSpecialVersionForms(t1, t2)
		case d1:
			compare = compareSpecialVersionForms(numberForm, t2)
		default:
			compare = compareSpecialVersionForms(t1, numberForm)
		}
		if compare != 0 {
			break
		}
		if n1 {
			p1 = e1 + 1
		}
		if n2 {
			p2 = e2 + 1
		}
	}
	if compare == 0 {
		switch {
		case n1:
			if p1 < len(v1) && isDigit(v1[p1]) {
				compare = 1
			} else {
				compare = phpVersionCompare(v1[p1:], numberForm)
			}
		case n2:
			if p2 < len(v2) && isDigit(v2[p2]) {
				compare = -1
			} else {
				compare = phpVersionCompare(numberForm, v2[p2:])
			}
		}
	}

	return compare
}

func indexDot(b []byte, from int) int {
	for i := from; i < len(b); i++ {
		if b[i] == '.' {
			return i
		}
	}

	return -1
}

func firstByte(b []byte) byte {
	if len(b) == 0 {
		return 0
	}

	return b[0]
}

// strtol ports strtol(token, NULL, 10) for a token starting with a digit:
// it reads the leading digits and saturates at LONG_MAX.
func strtol(token []byte) int64 {
	const maxInt64 = 1<<63 - 1
	var n int64
	for _, c := range token {
		if !isDigit(c) {
			break
		}
		d := int64(c - '0')
		if n > (maxInt64-d)/10 {
			return maxInt64
		}
		n = n*10 + d
	}

	return n
}

// specialForms is compare_special_version_forms()'s table, matched by
// prefix in this order.
var specialForms = [...]struct {
	name  string
	order int
}{
	{"dev", 0},
	{"alpha", 1},
	{"a", 1},
	{"beta", 2},
	{"b", 2},
	{"RC", 3},
	{"rc", 3},
	{"#", 4},
	{"pl", 5},
	{"p", 5},
}

// compareSpecialVersionForms ports compare_special_version_forms().
func compareSpecialVersionForms[A, B byteString](form1 A, form2 B) int {
	return sign(specialFormOrder(form1) - specialFormOrder(form2))
}

func specialFormOrder[S byteString](form S) int {
	for _, f := range specialForms {
		if len(form) >= len(f.name) && string(form[:len(f.name)]) == f.name {
			return f.order
		}
	}

	return -1
}

func sign[T int | int64](n T) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	default:
		return 0
	}
}
