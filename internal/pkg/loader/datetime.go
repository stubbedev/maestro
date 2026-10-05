// Ports the subset of PHP's date parser (timelib, behind new \DateTime($s,
// new \DateTimeZone('UTC'))) that package release dates use.

package loader

import (
	"strconv"
	"strings"
	"time"
)

// DateTimeError is the exception new \DateTime() throws for a string it
// cannot parse (DateMalformedStringException).
type DateTimeError struct {
	Time     string
	Position int
	Message  string
}

func (e *DateTimeError) Error() string {
	c := ""
	if e.Position < len(e.Time) {
		c = e.Time[e.Position : e.Position+1]
	}

	return "Failed to parse time string (" + e.Time + ") at position " + strconv.Itoa(e.Position) + " (" + c + "): " + e.Message
}

// zoneAbbreviations maps the timezone abbreviations timelib knows that
// package metadata is likely to carry to their UTC offsets in seconds.
var zoneAbbreviations = map[string]int{
	"utc": 0, "gmt": 0, "ut": 0, "z": 0, "wet": 0, "west": 3600, "bst": 3600,
	"cet": 3600, "cest": 7200, "met": 3600, "mest": 7200, "eet": 7200, "eest": 10800, "msk": 10800,
	"est": -18000, "edt": -14400, "cst": -21600, "cdt": -18000, "mst": -25200, "mdt": -21600,
	"pst": -28800, "pdt": -25200, "akst": -32400, "akdt": -28800, "hst": -36000,
	"jst": 32400, "kst": 32400, "aest": 36000, "aedt": 39600, "nzst": 43200, "nzdt": 46800,
}

// parseDateTime ports new \DateTime($s, new \DateTimeZone('UTC')) for
// "@timestamp" and ISO 8601 style dates: YYYY-MM-DD, YYYY/MM/DD or
// YYYYMMDD, optionally followed by a time (after "T" or spaces: HH:MM,
// HH:MM:SS, with a fraction, or a bare HH after "T") and a zone ("Z",
// +HH, +HHMM, +HH:MM, an abbreviation such as UTC or CEST, or an
// identifier such as Europe/Paris). The result keeps the zone the string
// names, as PHP's DateTime does. Relative formats ("tomorrow", "+1 day")
// and the other formats timelib accepts are reported as unparsable, and
// error positions and messages are those of timelib only for the common
// cases.
func parseDateTime(s string) (time.Time, error) {
	p := dateParser{s: s}

	t, ok := p.parse()
	if !ok {
		msg := "Unexpected character"
		if p.pos < len(s) && isLetter(s[p.pos]) {
			msg = "The timezone could not be found in the database"
		}

		return time.Time{}, &DateTimeError{Time: s, Position: p.pos, Message: msg}
	}

	return t, nil
}

type dateParser struct {
	s   string
	pos int
}

func isDigit(c byte) bool  { return c >= '0' && c <= '9' }
func isLetter(c byte) bool { return c|0x20 >= 'a' && c|0x20 <= 'z' }

func (p *dateParser) peek() byte {
	if p.pos < len(p.s) {
		return p.s[p.pos]
	}

	return 0
}

func (p *dateParser) skipSpace() {
	for p.pos < len(p.s) && (p.s[p.pos] == ' ' || p.s[p.pos] == '\t') {
		p.pos++
	}
}

// digits reads between minN and maxN digits.
func (p *dateParser) digits(minN, maxN int) (int, bool) {
	start := p.pos
	for p.pos < len(p.s) && p.pos-start < maxN && isDigit(p.s[p.pos]) {
		p.pos++
	}

	if p.pos-start < minN {
		return 0, false
	}

	n, _ := strconv.Atoi(p.s[start:p.pos])

	return n, true
}

func (p *dateParser) parse() (time.Time, bool) {
	p.skipSpace()

	if p.peek() == '@' {
		return p.timestamp()
	}

	year, month, day, ok := p.date()
	if !ok {
		return time.Time{}, false
	}

	var hour, minute, sec, nsec int

	loc := time.UTC

	switch p.peek() {
	case 'T', 't':
		p.pos++

		if hour, minute, sec, nsec, ok = p.clock(true); !ok {
			return time.Time{}, false
		}
	case ' ', '\t':
		p.skipSpace()

		if isDigit(p.peek()) {
			if hour, minute, sec, nsec, ok = p.clock(false); !ok {
				return time.Time{}, false
			}
		}
	}

	if p.pos < len(p.s) {
		p.skipSpace()

		if p.pos < len(p.s) {
			if loc, ok = p.zone(); !ok {
				return time.Time{}, false
			}

			p.skipSpace()
		}
	}

	if p.pos != len(p.s) {
		return time.Time{}, false
	}

	return time.Date(year, time.Month(month), day, hour, minute, sec, nsec, loc), true
}

// timestamp parses "@" [-] digits [. digits].
func (p *dateParser) timestamp() (time.Time, bool) {
	p.pos++

	neg := p.peek() == '-'
	if neg || p.peek() == '+' {
		p.pos++
	}

	start := p.pos

	sec, ok := p.digits(1, 19)
	if !ok || (p.pos-start == 19 && p.s[start:p.pos] > "9223372036854775807") {
		return time.Time{}, false
	}

	var nsec int

	if p.peek() == '.' {
		p.pos++

		if nsec, ok = p.fraction(); !ok {
			return time.Time{}, false
		}
	}

	p.skipSpace()

	if p.pos != len(p.s) {
		return time.Time{}, false
	}

	if neg {
		sec, nsec = -sec, -nsec
	}

	return time.Unix(int64(sec), int64(nsec)).In(utcOffsetZone), true
}

// date parses YYYY-M-D, YYYY/M/D or YYYYMMDD. Months 0 to 12 and days 0
// to 31 are accepted (time.Date normalizes them as PHP does); a larger
// two-digit value fails at its second digit, where timelib stops.
func (p *dateParser) date() (year, month, day int, ok bool) {
	if year, ok = p.digits(4, 4); !ok {
		return 0, 0, 0, false
	}

	if isDigit(p.peek()) {
		if month, ok = p.field(2, 12); !ok {
			return 0, 0, 0, false
		}

		if day, ok = p.field(2, 31); !ok {
			return 0, 0, 0, false
		}

		return year, month, day, true
	}

	sep := p.peek()
	if sep != '-' && sep != '/' {
		return 0, 0, 0, false
	}

	p.pos++

	if month, ok = p.field(1, 12); !ok {
		return 0, 0, 0, false
	}

	if p.peek() != sep {
		return 0, 0, 0, false
	}

	p.pos++

	if day, ok = p.field(1, 31); !ok {
		return 0, 0, 0, false
	}

	return year, month, day, true
}

// field reads a number of minN to 2 digits no larger than maxValue;
// a two-digit number above it fails at its second digit.
func (p *dateParser) field(minN, maxValue int) (int, bool) {
	start := p.pos

	n, ok := p.digits(minN, 2)
	if !ok {
		return 0, false
	}

	if n > maxValue {
		p.pos = start + 1

		return 0, false
	}

	return n, true
}

// clock parses HH:MM[:SS[.frac]], or a bare HH when hourOnly is allowed.
func (p *dateParser) clock(hourOnly bool) (hour, minute, sec, nsec int, ok bool) {
	if hour, ok = p.field(1, 24); !ok {
		return 0, 0, 0, 0, false
	}

	if p.peek() != ':' {
		return hour, 0, 0, 0, hourOnly
	}

	p.pos++

	if minute, ok = p.field(2, 59); !ok {
		return 0, 0, 0, 0, false
	}

	if p.peek() == ':' {
		p.pos++

		if sec, ok = p.field(2, 60); !ok {
			return 0, 0, 0, 0, false
		}

		if c := p.peek(); c == '.' || c == ',' {
			p.pos++

			if nsec, ok = p.fraction(); !ok {
				return 0, 0, 0, 0, false
			}
		}
	}

	return hour, minute, sec, nsec, true
}

// fraction reads a decimal fraction, kept to the microsecond like PHP.
func (p *dateParser) fraction() (int, bool) {
	start := p.pos
	for p.pos < len(p.s) && isDigit(p.s[p.pos]) {
		p.pos++
	}

	if p.pos == start {
		return 0, false
	}

	frac := p.s[start:p.pos]
	if len(frac) > 6 {
		frac = frac[:6]
	}

	n, _ := strconv.Atoi(frac + strings.Repeat("0", 6-len(frac)))

	return n * 1000, true
}

// zone parses Z, an offset, an abbreviation or a zone identifier.
func (p *dateParser) zone() (*time.Location, bool) {
	switch c := p.peek(); {
	case c == '+' || c == '-':
		p.pos++

		start := p.pos

		hours, ok := p.digits(1, 4)
		if !ok {
			return nil, false
		}

		var minutes int

		switch n := p.pos - start; {
		case n == 4:
			hours, minutes = hours/100, hours%100
		case n == 3:
			hours, minutes = hours/100, hours%100
		case p.peek() == ':':
			p.pos++

			if minutes, ok = p.digits(2, 2); !ok {
				return nil, false
			}
		}

		offset := hours*3600 + minutes*60
		if c == '-' {
			offset = -offset
		}

		return fixedZone(offset), true
	case isLetter(c):
		start := p.pos
		for p.pos < len(p.s) && (isLetter(p.s[p.pos]) || p.s[p.pos] == '/' || p.s[p.pos] == '_') {
			p.pos++
		}

		name := p.s[start:p.pos]

		if offset, ok := zoneAbbreviations[strings.ToLower(name)]; ok {
			return fixedZone(offset), true
		}

		if strings.Contains(name, "/") {
			if loc, err := time.LoadLocation(name); err == nil {
				return loc, true
			}
		}

		p.pos = start

		return nil, false
	}

	return nil, false
}

// utcOffsetZone is the zone of "+00:00", the offset almost every release
// date carries; time.Location values are immutable.
var utcOffsetZone = time.FixedZone("+00:00", 0)

// fixedZone returns the zone of an offset, named as PHP names offset
// zones (only the offset is ever formatted).
func fixedZone(offset int) *time.Location {
	if offset == 0 {
		return utcOffsetZone
	}

	return time.FixedZone(formatOffset(offset), offset)
}

// formatOffset formats an offset as PHP names offset zones: +05:30.
func formatOffset(offset int) string {
	sign := "+"
	if offset < 0 {
		sign, offset = "-", -offset
	}

	pad := func(n int) string {
		if n < 10 {
			return "0" + strconv.Itoa(n)
		}

		return strconv.Itoa(n)
	}

	return sign + pad(offset/3600) + ":" + pad(offset%3600/60)
}
