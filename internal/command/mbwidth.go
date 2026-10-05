// Ports mb_strwidth and mb_strimwidth (PHP 8.4 mbstring, UTF-8) for
// ShowCommand's description trimming.

package command

import (
	"sort"
	"strings"
	"unicode/utf8"
)

// mbCharWidth is the column width mbstring gives one code point.
func mbCharWidth(r rune) int {
	i := sort.Search(len(mbWideRanges), func(i int) bool { return mbWideRanges[i][1] >= r })
	if i < len(mbWideRanges) && mbWideRanges[i][0] <= r {
		return 2
	}

	return 1
}

// mbChars splits s as mbstring's UTF-8 decoder does: an invalid byte is
// one character of width 1.
func mbChars(s string) []string {
	var chars []string
	for len(s) > 0 {
		_, n := utf8.DecodeRuneInString(s)
		chars = append(chars, s[:n])
		s = s[n:]
	}

	return chars
}

func mbWidthOf(char string) int {
	r, n := utf8.DecodeRuneInString(char)
	if r == utf8.RuneError && n <= 1 {
		return 1
	}

	return mbCharWidth(r)
}

// mbStrwidth ports mb_strwidth($s, 'UTF-8').
func mbStrwidth(s string) int {
	w := 0
	for _, c := range mbChars(s) {
		w += mbWidthOf(c)
	}

	return w
}

// mbStrimwidth ports mb_strimwidth($s, 0, $width, $trimMarker, 'UTF-8')
// for width >= 0.
func mbStrimwidth(s string, width int, trimMarker string) string {
	if mbStrwidth(s) <= width {
		return s
	}
	avail := width - mbStrwidth(trimMarker)
	var out strings.Builder
	if avail > 0 {
		w := 0
		for _, c := range mbChars(s) {
			cw := mbWidthOf(c)
			if w+cw > avail {
				break
			}
			w += cw
			out.WriteString(c)
		}
	}

	return out.String() + trimMarker
}
