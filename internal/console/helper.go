// Ports src/Helper/Helper.php, HelperInterface.php and HelperSet.php
// (symfony/console), with the parts of symfony/string's UnicodeString they
// rely on (width(false), length()).

package console

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/rivo/uniseg"
)

// Width returns the terminal column width of s (Helper::width).
func Width(s string) int {
	if !utf8.ValidString(s) {
		return len(s)
	}

	// UnicodeString::width(false): every control character (\p{Cc}, which
	// includes "\n") is removed before splitting on "\n", so the whole
	// string is measured as one line.
	width := 0
	for _, r := range s {
		width += runeWidth(r)
	}

	return width
}

// runeWidth ports AbstractUnicodeString::wcswidth for one code point that is
// not a control character (those are stripped by width(false)).
func runeWidth(r rune) int {
	switch {
	case r < 0x20, r >= 0x7F && r < 0xA0:
		// Removed by preg_replace('/[\p{Cc}\x7F]++/u', '', $s).
		return 0
	case r == 0x034F, r >= 0x200B && r <= 0x200F, r == 0x2028, r == 0x2029,
		r >= 0x202A && r <= 0x202E, r >= 0x2060 && r <= 0x2063:
		return 0
	}
	if inRuneTable(wcwidthZero[:], r) {
		return 0
	}
	if inRuneTable(wcwidthWide[:], r) {
		return 2
	}

	return 1
}

func inRuneTable(table [][2]rune, r rune) bool {
	if r < table[0][0] || r > table[len(table)-1][1] {
		return false
	}
	lo, hi := 0, len(table)-1
	for hi >= lo {
		mid := (lo + hi) / 2
		switch {
		case r > table[mid][1]:
			lo = mid + 1
		case r < table[mid][0]:
			hi = mid - 1
		default:
			return true
		}
	}

	return false
}

// Length returns the number of grapheme clusters in s (Helper::length).
func Length(s string) int {
	if !utf8.ValidString(s) {
		return len(s)
	}

	return uniseg.GraphemeClusterCount(s)
}

// Substr is Helper::substr: mb_substr on valid UTF-8, substr otherwise.
// A negative length counts from the end; noLength means "to the end".
func Substr(s string, from, length int, noLength bool) string {
	if !utf8.ValidString(s) {
		return byteSubstr(s, from, length, noLength)
	}

	n := utf8.RuneCountInString(s)
	if from < 0 {
		from = max(n+from, 0)
	}
	if from > n {
		return ""
	}
	end := n
	if !noLength {
		if length < 0 {
			end = n + length
		} else {
			end = min(from+length, n)
		}
	}
	if end <= from {
		return ""
	}

	// Convert rune offsets to byte offsets.
	bstart, bend := len(s), len(s)
	idx := 0
	for i := range s {
		if idx == from {
			bstart = i
		}
		if idx == end {
			bend = i

			break
		}
		idx++
	}

	return s[bstart:bend]
}

func byteSubstr(s string, from, length int, noLength bool) string {
	n := len(s)
	if from < 0 {
		from = max(n+from, 0)
	}
	if from > n {
		return ""
	}
	end := n
	if !noLength {
		if length < 0 {
			end = n + length
		} else {
			end = min(from+length, n)
		}
	}
	if end <= from {
		return ""
	}

	return s[from:end]
}

var timeFormats = []struct {
	limit float64
	text  string
	div   float64
}{
	{0, "< 1 sec", 0},
	{1, "1 sec", 0},
	{2, "secs", 1},
	{60, "1 min", 0},
	{120, "mins", 60},
	{3600, "1 hr", 0},
	{7200, "hrs", 3600},
	{86400, "1 day", 0},
	{172800, "days", 86400},
}

// FormatTime renders a duration in seconds (Helper::formatTime).
func FormatTime(secs float64) string {
	for i, f := range timeFormats {
		if secs >= f.limit {
			if (i+1 < len(timeFormats) && secs < timeFormats[i+1].limit) || i == len(timeFormats)-1 {
				if f.div == 0 {
					return f.text
				}

				return phpFloatString(math.Floor(secs/f.div)) + " " + f.text
			}
		}
	}

	return ""
}

// FormatMemory renders a byte count (Helper::formatMemory).
func FormatMemory(memory int) string {
	switch {
	case memory >= 1024*1024*1024:
		return fmt.Sprintf("%.1f GiB", float64(memory)/1024/1024/1024)
	case memory >= 1024*1024:
		return fmt.Sprintf("%.1f MiB", float64(memory)/1024/1024)
	case memory >= 1024:
		return strconv.Itoa(memory/1024) + " KiB"
	}

	return strconv.Itoa(memory) + " B"
}

// RemoveDecoration strips formatting tags, ANSI colour sequences and
// terminal hyperlinks from s (Helper::removeDecoration).
func RemoveDecoration(formatter Formatter, s string) string {
	decorated := formatter.IsDecorated()
	formatter.SetDecorated(false)
	s = formatter.Format(s)
	s = removeANSIColors(s)
	s = removeHyperlinks(s)
	formatter.SetDecorated(decorated)

	return s
}

// removeANSIColors is preg_replace("/\033\[[^m]*m/", '', $s).
func removeANSIColors(s string) string {
	if !strings.Contains(s, "\033[") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] == '\033' && i+1 < len(s) && s[i+1] == '[' {
			if j := strings.IndexByte(s[i+2:], 'm'); j >= 0 {
				i += 2 + j + 1

				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}

	return b.String()
}

// removeHyperlinks is preg_replace('/\\033]8;[^;]*;[^\\033]*\\033\\\\/', '', $s).
func removeHyperlinks(s string) string {
	if !strings.Contains(s, "\033]8;") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if strings.HasPrefix(s[i:], "\033]8;") {
			j := i + 4
			for j < len(s) && s[j] != ';' {
				j++
			}
			if j < len(s) {
				k := j + 1
				for k < len(s) && s[k] != '\033' {
					k++
				}
				if k+1 < len(s) && s[k+1] == '\\' {
					i = k + 2

					continue
				}
			}
		}
		b.WriteByte(s[i])
		i++
	}

	return b.String()
}

// Helper is HelperInterface.
type Helper interface {
	SetHelperSet(set *HelperSet)
	HelperSet() *HelperSet
	Name() string
}

// HelperBase implements the helper set plumbing of the abstract Helper class.
type HelperBase struct {
	helperSet *HelperSet
}

// SetHelperSet implements Helper.
func (h *HelperBase) SetHelperSet(set *HelperSet) { h.helperSet = set }

// HelperSet implements Helper.
func (h *HelperBase) HelperSet() *HelperSet { return h.helperSet }

// HelperSet is a named collection of helpers, iterated in insertion order.
type HelperSet struct {
	names   []string
	helpers map[string]Helper
}

// NewHelperSet registers helpers under their own names.
func NewHelperSet(helpers ...Helper) *HelperSet {
	s := &HelperSet{helpers: make(map[string]Helper, len(helpers))}
	for _, h := range helpers {
		s.Set(h, "")
	}

	return s
}

// Set registers helper under its name and, if non-empty, alias.
func (s *HelperSet) Set(helper Helper, alias string) {
	s.put(helper.Name(), helper)
	if alias != "" {
		s.put(alias, helper)
	}
	helper.SetHelperSet(s)
}

func (s *HelperSet) put(name string, h Helper) {
	if _, ok := s.helpers[name]; !ok {
		s.names = append(s.names, name)
	}
	s.helpers[name] = h
}

// Has reports whether a helper is registered under name.
func (s *HelperSet) Has(name string) bool {
	_, ok := s.helpers[name]

	return ok
}

// Get returns the named helper.
func (s *HelperSet) Get(name string) (Helper, error) {
	h, ok := s.helpers[name]
	if !ok {
		return nil, newError(KindInvalidArgument, "HelperSet.php", 79, `The helper "%s" is not defined.`, name)
	}

	return h, nil
}

// All returns the helpers in registration order (one entry per name).
func (s *HelperSet) All() []Helper {
	out := make([]Helper, len(s.names))
	for i, n := range s.names {
		out[i] = s.helpers[n]
	}

	return out
}
