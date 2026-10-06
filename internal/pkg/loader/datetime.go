// Ports PHP's date parser (timelib 2022.17's timelib_strtotime in
// parse_date.re, behind new \DateTime($s, new \DateTimeZone('UTC')) and
// php_date_initialize) for the strings package metadata carries.
//
// The scanner is timelib's: at each position the longest match among its
// re2c rules wins (the earlier rule on a tie), so strings split into the
// same tokens and errors are reported at the same positions with the same
// messages. Every rule is matched; the actions of the rules that set
// absolute dates, times and zones, "now", "noon", "midnight", "today",
// "tomorrow", "yesterday", "@timestamp", ISO weeks and "ago" are ported.
// The relative rules that need timelib's weekday and special arithmetic
// (relative offsets such as "+1 day", "next month", "first day of",
// weekday names, "back of"/"front of") are not: a string using one yields
// a *DateTimeError with Unsupported set, unless an earlier token already
// failed (that error is PHP's, as only the first error is reported).

package loader

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DateTimeError is the exception new \DateTime() throws for a string it
// cannot parse (DateMalformedStringException), or, with Unsupported, a
// string using a relative format this port does not implement.
type DateTimeError struct {
	Time     string
	Position int
	// Character is the byte at Position of the trimmed string timelib
	// scans (0 for Time[Position]).
	Character byte
	Message   string
	// Unsupported marks a valid PHP relative format maestro cannot
	// evaluate (see parseDateTime).
	Unsupported bool
}

func (e *DateTimeError) Error() string {
	c := e.Character
	if c == 0 && e.Position < len(e.Time) {
		c = e.Time[e.Position]
	}

	if c == 0 {
		c = ' ' // php_date_initialize prints a NUL character as a space
	}

	return "Failed to parse time string (" + e.Time + ") at position " + strconv.Itoa(e.Position) + " (" + string([]byte{c}) + "): " + e.Message
}

// zoneAbbreviation is a timelib time zone abbreviation: the UTC offset in
// seconds (DST included) and whether it is a DST abbreviation.
type zoneAbbreviation struct {
	offset int64
	dst    int64
}

// parseDateTime ports new \DateTime($s, new \DateTimeZone('UTC')) at the
// current time.
func parseDateTime(s string) (time.Time, error) { return parseDateTimeAt(s, time.Now()) }

// parseDateTimeAt ports new \DateTime($s, new \DateTimeZone('UTC')) with
// now as the current time: the fields s leaves unset come from now in UTC
// (timelib_fill_holes), and the result keeps the zone s names (only its
// offset is meaningful: offset and abbreviation zones are named like
// PHP's offset zones, "+05:30"). An empty or blank string is now, a
// military zone letter ("a" to "z" but "j") is now's wall clock in that
// zone, and bare digits are read as timelib reads them ("1234" is 12:34
// today, "2460" the year 2460, "123456" 12:34:56, "20120101" a date).
func parseDateTimeAt(s string, now time.Time) (time.Time, error) {
	p, err := strToTime(s)
	if err != nil {
		return time.Time{}, err
	}

	return p.resolve(now), nil
}

// dtUnset is TIMELIB_UNSET; timelib also reads a parsed -9999999 as unset.
const dtUnset = -9999999

// The zone types of timelib_time.zone_type.
const (
	zoneNone = iota
	zoneOffset
	zoneAbbr
	zoneID
)

// dtParsed is the timelib_time timelib_strtotime fills.
type dtParsed struct {
	y, m, d, h, i, s, us int64
	haveTime, haveDate   int
	haveZone             int
	haveRelative         bool
	zoneType             int
	z, dst               int64
	loc                  *time.Location
	// the relative fields the ported rules set
	relD, relS, relUS int64
}

// dtScanner is timelib's Scanner: str is the trimmed string followed by
// NUL padding (YYMAXFILL), tok the start of the current token.
type dtScanner struct {
	str []byte
	n   int
	tok int
	t   dtParsed
	err *DateTimeError
}

// strToTime ports timelib_strtotime, stopping at the first error (the
// only one new \DateTime() reports).
func strToTime(input string) (*dtParsed, error) {
	// php_date_initialize reads "" as "now"; timelib trims C isspace()
	// from both ends, leaving at least one character.
	b := []byte(input)
	start, end := 0, len(b)-1

	if len(b) > 0 {
		for start < end && isCSpace(b[start]) {
			start++
		}

		for end > start && isCSpace(b[end]) {
			end--
		}
	}

	sc := &dtScanner{n: end + 1 - start}
	sc.str = make([]byte, sc.n+8)
	copy(sc.str, b[start:end+1])
	sc.t = dtParsed{y: dtUnset, m: dtUnset, d: dtUnset, h: dtUnset, i: dtUnset, s: dtUnset, us: dtUnset, z: dtUnset, dst: dtUnset}

	rules := dtRules()

	for cur := 0; cur < sc.n; {
		sc.tok = cur

		rule, length := dtRuleAny, 1
		rest := sc.str[cur:]

		for _, r := range rules {
			if loc := r.re.FindIndex(rest); loc != nil && loc[1] > 0 && (loc[1] > length || (loc[1] == length && r.id < rule)) {
				rule, length = r.id, loc[1]
			}
		}

		cur += length
		if sc.apply(rule, sc.str[sc.tok:cur]) {
			sc.err = &DateTimeError{Position: sc.tok, Character: sc.str[sc.tok], Message: "maestro does not support this relative time format", Unsupported: true}
		}

		if sc.err != nil {
			sc.err.Time = input

			return nil, sc.err
		}
	}

	return &sc.t, nil
}

func isCSpace(c byte) bool { return c == ' ' || (c >= '\t' && c <= '\r') }

func isDigit(c byte) bool  { return c >= '0' && c <= '9' }
func isLetter(c byte) bool { return c|0x20 >= 'a' && c|0x20 <= 'z' }

// addError is timelib's add_error: at the token's start.
func (sc *dtScanner) addError(msg string) {
	if sc.err == nil {
		sc.err = &DateTimeError{Position: sc.tok, Character: sc.str[sc.tok], Message: msg}
	}
}

// The rules of timelib's scanner, in its order (a tie in length goes to
// the earlier rule).
const (
	dtRuleYesterday = iota
	dtRuleNow
	dtRuleNoon
	dtRuleMidnightToday
	dtRuleTomorrow
	dtRuleTimestamp
	dtRuleTimestampMs
	dtRuleFirstLastDayOf
	dtRuleBackFrontOf
	dtRuleWeekdayOf
	dtRuleTime12
	dtRuleMssqlTime
	dtRuleTime24
	dtRuleGnuNoColon
	dtRuleISO8601NoColon
	dtRuleAmerican
	dtRuleISO8601Date4
	dtRuleISO8601Date2
	dtRuleISO8601DateX
	dtRuleGnuDateShorter
	dtRuleGnuDateShort
	dtRuleDateFull
	dtRulePointedDate4
	dtRulePointedDate2
	dtRuleDateNoDay
	dtRuleDateNoDayRev
	dtRuleDateTextual
	dtRuleDateNoYearRev
	dtRuleDateNoColon
	dtRuleXMLRPC
	dtRulePgYDotD
	dtRuleISOWeekDay
	dtRuleISOWeek
	dtRulePgTextShort
	dtRulePgTextReverse
	dtRuleCLF
	dtRuleYear4
	dtRuleAgo
	dtRuleDayText
	dtRuleRelativeTextWeek
	dtRuleRelativeText
	dtRuleMonthText
	dtRuleTimezone
	dtRuleDateShortWithTime12
	dtRuleDateShortWithTime24
	dtRuleRelative
	dtRuleDotComma
	dtRuleSpace
	dtRuleNul
	dtRuleAny
)

type dtRule struct {
	id int
	re *regexp.Regexp
}

// ci is a re2c case-insensitive string ('...'): ASCII letters only.
func ci(s string) string {
	var b strings.Builder

	b.WriteString("(?:")

	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			b.WriteString("[" + strings.ToUpper(string(r)) + strings.ToLower(string(r)) + "]")
		} else {
			b.WriteString(regexp.QuoteMeta(string(r)))
		}
	}

	b.WriteString(")")

	return b.String()
}

func alt(xs ...string) string { return "(?:" + strings.Join(xs, "|") + ")" }

func ciAlt(xs ...string) string {
	for i, x := range xs {
		xs[i] = ci(x)
	}

	return alt(xs...)
}

// dtRules compiles timelib's re2c definitions (parse_date.re) as
// leftmost-longest Go regexps anchored at the scan position.
var dtRules = sync.OnceValue(func() []dtRule {
	const (
		space        = `(?:[ \t]+|\x{a0}+|\x{202f}+)`
		frac         = `\.[0-9]+`
		hour24       = `(?:[01]?[0-9]|2[0-4])`
		hour24lz     = `(?:[01][0-9]|2[0-4])`
		hour12       = `(?:0?[1-9]|1[0-2])`
		minute       = `(?:[0-5]?[0-9])`
		minutelz     = `(?:[0-5][0-9])`
		second       = `(?:[0-5]?[0-9]|60)`
		secondlz     = `(?:[0-5][0-9]|60)`
		meridian     = `(?:[AaPp]\.?[Mm]\.?[\x00\t ])`
		tz           = `(?:\(?[A-Za-z]{1,6}\)?|[A-Z][a-z]+(?:[_/-][A-Za-z]+)+)`
		tzcorrection = `(?:(?:GMT)?[+-](?:` + hour24 + `(?::?` + minute + `)?|` + hour24lz + minutelz + secondlz + `|` + hour24lz + `:` + minutelz + `:` + secondlz + `))`
		daysuf       = `(?:st|nd|rd|th)`
		month        = `(?:0?[0-9]|1[0-2])`
		day          = `(?:(?:[0-2]?[0-9]|3[01])` + daysuf + `?)`
		year         = `(?:[0-9]{1,4})`
		year2        = `(?:[0-9]{2})`
		year4        = `(?:[0-9]{4})`
		year4sign    = `(?:[+-]?[0-9]{4})`
		yearx        = `(?:[+-][0-9]{5,19})`
		dayofyear    = `(?:00[1-9]|0[1-9][0-9]|[12][0-9][0-9]|3[0-5][0-9]|36[0-6])`
		weekofyear   = `(?:0[1-9]|[1-4][0-9]|5[0-3])`
		monthlz      = `(?:0[0-9]|1[0-2])`
		daylz        = `(?:0[0-9]|[12][0-9]|3[01])`
		monthroman   = `(?:I|II|III|IV|V|VI|VII|VIII|IX|X|XI|XII)`
	)

	var (
		dayfulls   = ciAlt("sundays", "mondays", "tuesdays", "wednesdays", "thursdays", "fridays", "saturdays")
		dayfull    = ciAlt("sunday", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday")
		dayabbr    = ciAlt("sun", "mon", "tue", "wed", "thu", "fri", "sat", "sun")
		dayspecial = ciAlt("weekday", "weekdays")
		daytext    = alt(dayfulls, dayfull, dayabbr, dayspecial)
		monthfull  = ciAlt("january", "february", "march", "april", "may", "june", "july", "august", "september", "october", "november", "december")
		monthabbr  = ciAlt("jan", "feb", "mar", "apr", "may", "jun", "jul", "aug", "sep", "sept", "oct", "nov", "dec")
		monthtext  = alt(monthfull, monthabbr, monthroman)

		timetiny12  = hour12 + space + `?` + meridian
		timeshort12 = hour12 + `[:.]` + minutelz + space + `?` + meridian
		timelong12  = hour12 + `[:.]` + minute + `[:.]` + secondlz + space + `?` + meridian
		timetiny24  = `[Tt]` + hour24
		timeshort24 = `[Tt]?` + hour24 + `[:.]` + minute
		timelong24  = `[Tt]?` + hour24 + `[:.]` + minute + `[:.]` + second
		iso8601long = `[Tt]?` + hour24 + `[:.]` + minute + `[:.]` + second + frac
		iso8601ntz  = `[Tt]?` + hour24 + `[:.]` + minute + `[:.]` + secondlz + space + `?` + alt(tzcorrection, tz)
		gnunocolon  = `[Tt]?` + hour24lz + minutelz
		iso8601nc   = `[Tt]?` + hour24lz + minutelz + secondlz

		americanshort    = month + `/` + day
		american         = month + `/` + day + `/` + year
		iso8601dateslash = year4 + `/` + monthlz + `/` + daylz + `/?`
		dateslash        = year4 + `/` + month + `/` + day
		iso8601date4     = year4sign + `-` + monthlz + `-` + daylz
		iso8601date2     = year2 + `-` + monthlz + `-` + daylz
		iso8601datex     = yearx + `-` + monthlz + `-` + daylz
		gnudateshorter   = year4 + `-` + month
		gnudateshort     = year + `-` + month + `-` + day
		pointeddate4     = day + `[.\t-]` + month + `[.-]` + year4
		pointeddate2     = day + `[.\t]` + month + `\.` + year2
		datefull         = day + `[ \t.-]*` + monthtext + `[ \t.-]*` + year
		datenoday        = monthtext + `[ .\t-]*` + year4
		datenodayrev     = year4 + `[ .\t-]*` + monthtext
		datetextual      = monthtext + `[ .\t-]*` + day + `[,.stndrh\t ]+` + year
		datenoyear       = monthtext + `[ .\t-]*` + day + `(?:[,.stndrh\t ]+|\x00)`
		datenoyearrev    = day + `[ .\t-]*` + monthtext
		datenocolon      = year4 + monthlz + daylz

		soap          = year4 + `-` + monthlz + `-` + daylz + `T` + hour24lz + `:` + minutelz + `:` + secondlz + frac + tzcorrection + `?`
		xmlrpc        = year4 + monthlz + daylz + `T` + hour24 + `:` + minutelz + `:` + secondlz
		xmlrpcnocolon = year4 + monthlz + daylz + `[Tt]` + hour24 + minutelz + secondlz
		wddx          = year4 + `-` + month + `-` + day + `T` + hour24 + `:` + minute + `:` + second
		pgydotd       = year4 + `[.-]?` + dayofyear
		pgtextshort   = monthabbr + `-` + daylz + `-` + year
		pgtextreverse = year + `-` + monthabbr + `-` + daylz
		mssqltime     = hour12 + `:` + minutelz + `:` + secondlz + `[:.][0-9]+` + meridian
		isoweekday    = year4 + `-?W` + weekofyear + `-?[0-7]`
		isoweek       = year4 + `-?W` + weekofyear
		exif          = year4 + `:` + monthlz + `:` + daylz + ` ` + hour24lz + `:` + minutelz + `:` + secondlz
		firstdayof    = ci("first day of")
		lastdayof     = ci("last day of")
		backof        = ci("back of ") + hour24 + `(?:` + space + `?` + meridian + `)?`
		frontof       = ci("front of ") + hour24 + `(?:` + space + `?` + meridian + `)?`
		clf           = day + `/` + monthabbr + `/` + year4 + `:` + hour24lz + `:` + minutelz + `:` + secondlz + space + tzcorrection
		timestamp     = `@-?[0-9]+`
		timestampms   = `@-?[0-9]+\.[0-9]{0,6}`

		reltextnumber    = ciAlt("first", "second", "third", "fourth", "fifth", "sixth", "seventh", "eight", "eighth", "ninth", "tenth", "eleventh", "twelfth")
		reltexttext      = ciAlt("next", "last", "previous", "this")
		reltextunit      = alt(ci("ms"), `\x{b5}[Ss]`, `(?:`+ciAlt("msec", "millisecond", `µsec`, "microsecond", "usec", "sec", "second", "min", "minute", "hour", "day", "fortnight", "forthnight", "month", "year")+`[Ss]?)`, ci("weeks"), daytext)
		relnumber        = `(?:[+-]*[ \t]*[0-9]{1,13})`
		relative         = relnumber + space + `?` + alt(reltextunit, ci("week"))
		relativetext     = alt(reltextnumber, reltexttext) + space + reltextunit
		relativetextweek = reltexttext + space + ci("week")
		weekdayof        = alt(reltextnumber, reltexttext) + space + alt(dayfulls, dayfull, dayabbr) + space + ci("of")
	)

	defs := []struct {
		id      int
		pattern string
	}{
		{dtRuleYesterday, ci("yesterday")},
		{dtRuleNow, ci("now")},
		{dtRuleNoon, ci("noon")},
		{dtRuleMidnightToday, ciAlt("midnight", "today")},
		{dtRuleTomorrow, ci("tomorrow")},
		{dtRuleTimestamp, timestamp},
		{dtRuleTimestampMs, timestampms},
		{dtRuleFirstLastDayOf, alt(firstdayof, lastdayof)},
		{dtRuleBackFrontOf, alt(backof, frontof)},
		{dtRuleWeekdayOf, weekdayof},
		{dtRuleTime12, alt(timetiny12, timeshort12, timelong12)},
		{dtRuleMssqlTime, mssqltime},
		{dtRuleTime24, alt(timetiny24, timeshort24, timelong24, iso8601long)},
		{dtRuleGnuNoColon, gnunocolon},
		{dtRuleISO8601NoColon, iso8601nc},
		{dtRuleAmerican, alt(americanshort, american)},
		{dtRuleISO8601Date4, alt(iso8601date4, iso8601dateslash, dateslash)},
		{dtRuleISO8601Date2, iso8601date2},
		{dtRuleISO8601DateX, iso8601datex},
		{dtRuleGnuDateShorter, gnudateshorter},
		{dtRuleGnuDateShort, gnudateshort},
		{dtRuleDateFull, datefull},
		{dtRulePointedDate4, pointeddate4},
		{dtRulePointedDate2, pointeddate2},
		{dtRuleDateNoDay, datenoday},
		{dtRuleDateNoDayRev, datenodayrev},
		{dtRuleDateTextual, alt(datetextual, datenoyear)},
		{dtRuleDateNoYearRev, datenoyearrev},
		{dtRuleDateNoColon, datenocolon},
		{dtRuleXMLRPC, alt(xmlrpc, xmlrpcnocolon, soap, wddx, exif)},
		{dtRulePgYDotD, pgydotd},
		{dtRuleISOWeekDay, isoweekday},
		{dtRuleISOWeek, isoweek},
		{dtRulePgTextShort, pgtextshort},
		{dtRulePgTextReverse, pgtextreverse},
		{dtRuleCLF, clf},
		{dtRuleYear4, year4},
		{dtRuleAgo, ci("ago")},
		{dtRuleDayText, daytext},
		{dtRuleRelativeTextWeek, relativetextweek},
		{dtRuleRelativeText, relativetext},
		{dtRuleMonthText, alt(monthfull, monthabbr)},
		{dtRuleTimezone, alt(tzcorrection, tz)},
		{dtRuleDateShortWithTime12, alt(datenoyear+timeshort12, datenoyear+timelong12)},
		{dtRuleDateShortWithTime24, alt(datenoyear+timeshort24, datenoyear+timelong24, datenoyear+iso8601ntz)},
		{dtRuleRelative, relative},
		{dtRuleDotComma, `[.,]`},
		{dtRuleSpace, space},
		{dtRuleNul, `[\x00\n]`},
	}

	rules := make([]dtRule, len(defs))
	for i, d := range defs {
		re := regexp.MustCompile(`^(?:` + d.pattern + `)`)
		re.Longest()
		rules[i] = dtRule{d.id, re}
	}

	return rules
})

// dtToken is the NUL-terminated copy of a token an action reads (ptr).
type dtToken struct {
	b []byte
	p int
}

func (t *dtToken) at(k int) byte {
	if t.p+k < len(t.b) {
		return t.b[t.p+k]
	}

	return 0
}

func (t *dtToken) cur() byte { return t.at(0) }

// getNr is timelib_get_nr_ex: skips to the first digit (unset at the end)
// and reads up to maxLen digits.
func (t *dtToken) getNr(maxLen int) (n int64, length int) {
	for c := t.cur(); !isDigit(c); c = t.cur() {
		if c == 0 {
			return dtUnset, 0
		}

		t.p++
	}

	begin := t.p
	for t.p-begin < maxLen && isDigit(t.cur()) {
		t.p++
	}

	n, _ = strconv.ParseInt(string(t.b[begin:t.p]), 10, 64)

	return n, t.p - begin
}

func (t *dtToken) nr(maxLen int) int64 {
	n, _ := t.getNr(maxLen)

	return n
}

// getFracNr is timelib_get_frac_nr: the microseconds of a ".ddd" fraction.
func (t *dtToken) getFracNr() int64 {
	for c := t.cur(); c != '.' && c != ':' && !isDigit(c); c = t.cur() {
		if c == 0 {
			return dtUnset
		}

		t.p++
	}

	begin := t.p
	for c := t.cur(); c == '.' || c == ':' || isDigit(c); c = t.cur() {
		t.p++
	}

	// strtod() of what follows the first character
	str := t.b[begin+1 : t.p]
	end := 0

	for end < len(str) && isDigit(str[end]) {
		end++
	}

	if end < len(str) && str[end] == '.' {
		end++
		for end < len(str) && isDigit(str[end]) {
			end++
		}
	}

	f, _ := strconv.ParseFloat(string(str[:end]), 64)
	if end == 0 {
		f = 0
	}

	return int64(f * math.Pow(10, float64(7-(t.p-begin))))
}

// getSignedNr is timelib_get_signed_nr.
func (sc *dtScanner) getSignedNr(t *dtToken, maxLen int) int64 {
	for c := t.cur(); !isDigit(c) && c != '+' && c != '-'; c = t.cur() {
		if c == 0 {
			sc.addError("Found unexpected data")

			return 0
		}

		t.p++
	}

	neg := false

	for c := t.cur(); c == '+' || c == '-'; c = t.cur() {
		if c == '-' {
			neg = !neg
		}

		t.p++
	}

	for c := t.cur(); !isDigit(c); c = t.cur() {
		if c == 0 {
			sc.addError("Found unexpected data")

			return 0
		}

		t.p++
	}

	begin := t.p
	for t.p-begin < maxLen && isDigit(t.cur()) {
		t.p++
	}

	digits := string(t.b[begin:t.p])
	if neg {
		digits = "-" + digits
	}

	n, err := strconv.ParseInt(digits, 10, 64)
	if err != nil {
		sc.addError("Number out of range")

		return 0
	}

	return n
}

// skipDaySuffix is timelib_skip_day_suffix.
func (t *dtToken) skipDaySuffix() {
	if isCSpace(t.cur()) {
		return
	}

	switch strings.ToLower(string([]byte{t.at(0), t.at(1)})) {
	case "nd", "rd", "st", "th":
		t.p += 2
	}
}

// monthNames is timelib_month_lookup.
var monthNames = map[string]int64{
	"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6, "jul": 7, "aug": 8, "sep": 9, "sept": 9, "oct": 10, "nov": 11, "dec": 12,
	"i": 1, "ii": 2, "iii": 3, "iv": 4, "v": 5, "vi": 6, "vii": 7, "viii": 8, "ix": 9, "x": 10, "xi": 11, "xii": 12,
	"january": 1, "february": 2, "march": 3, "april": 4, "june": 6, "july": 7, "august": 8,
	"september": 9, "october": 10, "november": 11, "december": 12,
}

// lookupMonth is timelib_lookup_month (0 for an unknown word).
func (t *dtToken) lookupMonth() int64 {
	begin := t.p
	for isLetter(t.cur()) {
		t.p++
	}

	return monthNames[strings.ToLower(string(t.b[begin:t.p]))]
}

// getMonth is timelib_get_month.
func (t *dtToken) getMonth() int64 {
	for c := t.cur(); c == ' ' || c == '\t' || c == '-' || c == '.' || c == '/'; c = t.cur() {
		t.p++
	}

	return t.lookupMonth()
}

// eatSpaces is timelib_eat_spaces.
func (t *dtToken) eatSpaces() {
	for {
		switch {
		case t.cur() == ' ' || t.cur() == '\t':
			t.p++
		case t.at(0) == 0xe2 && t.at(1) == 0x80 && t.at(2) == 0xaf:
			t.p += 3
		case t.at(0) == 0xc2 && t.at(1) == 0xa0:
			t.p += 2
		default:
			return
		}
	}
}

// meridian is timelib_meridian: the hour correction of am/pm.
func (t *dtToken) meridian(h int64) int64 {
	for c := t.cur(); c != 0 && !strings.ContainsRune("AaPp", rune(c)); c = t.cur() {
		t.p++
	}

	var ret int64

	if c := t.cur(); c == 'a' || c == 'A' {
		if h == 12 {
			ret = -12
		}
	} else if h != 12 {
		ret = 12
	}

	t.p++
	if t.cur() == '.' {
		t.p++
	}

	if c := t.cur(); c == 'M' || c == 'm' {
		t.p++
	}

	if t.cur() == '.' {
		t.p++
	}

	return ret
}

// parseTzCor is timelib_parse_tz_cor: the offset after a sign.
func (t *dtToken) parseTzCor() (int64, bool) {
	begin := t.p
	for c := t.cur(); isDigit(c) || c == ':'; c = t.cur() {
		t.p++
	}

	b := t.b[begin:t.p]

	strtol := func(from int) int64 {
		end := from
		for end < len(b) && isDigit(b[end]) {
			end++
		}

		n, _ := strconv.ParseInt(string(b[from:end]), 10, 64)

		return n
	}

	switch len(b) {
	case 1, 2: // H, HH
		return strtol(0) * 3600, true
	case 3, 4: // H:M, H:MM, HH:M, HHMM
		switch {
		case b[1] == ':':
			return strtol(0)*3600 + strtol(2)*60, true
		case b[2] == ':':
			return strtol(0)*3600 + strtol(3)*60, true
		default:
			n := strtol(0)

			return n/100*3600 + n%100*60, true
		}
	case 5: // HH:MM
		if b[2] == ':' {
			return strtol(0)*3600 + strtol(3)*60, true
		}
	case 6: // HHMMSS
		n := strtol(0)

		return n/10000*3600 + n/100%100*60 + n%100, true
	case 8: // HH:MM:SS
		if b[2] == ':' && b[5] == ':' {
			return strtol(0)*3600 + strtol(3)*60 + strtol(6), true
		}
	}

	return 0, false
}

// loadZone is php_date_parse_tzfile_wrapper: a time zone identifier.
func loadZone(name string) *time.Location {
	if name == "" || name == "Local" {
		return nil
	}

	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil
	}

	return loc
}

// parseZone is timelib_parse_zone; it reports whether the zone was found.
func (sc *dtScanner) parseZone(t *dtToken) bool {
	parens := 0

	for c := t.cur(); c == ' ' || c == '\t' || c == '('; c = t.cur() {
		if c == '(' {
			parens++
		}

		t.p++
	}

	if t.at(0) == 'G' && t.at(1) == 'M' && t.at(2) == 'T' && (t.at(3) == '+' || t.at(3) == '-') {
		t.p += 3
	}

	found := false
	p := &sc.t

	switch c := t.cur(); c {
	case '+', '-':
		t.p++
		p.zoneType, p.dst = zoneOffset, 0

		p.z, found = t.parseTzCor()
		if c == '-' {
			p.z = -p.z
		}
	default:
		begin := t.p
		for c := t.cur(); isLetter(c) || isDigit(c) || c == '/' || c == '_' || c == '-' || c == '+'; c = t.cur() {
			t.p++
		}

		word := string(t.b[begin:t.p])
		p.z = 0

		// abbr_search; MAX_ABBR_LEN is 6
		if len(word) < 6 {
			lower := strings.ToLower(word)

			abbr, ok := zoneAbbreviations[lower]
			if lower == "utc" || lower == "gmt" {
				abbr, ok = zoneAbbreviation{0, 0}, true
			}

			if ok {
				p.z, p.dst, p.zoneType, found = abbr.offset-abbr.dst*3600, abbr.dst, zoneAbbr, true
			}
		}

		if !found || word == "UTC" {
			if loc := loadZone(word); loc != nil {
				p.loc, p.zoneType, found = loc, zoneID, true
			}
		}
	}

	for parens > 0 && t.cur() == ')' {
		t.p++
		parens--
	}

	return found
}

// zone parses a zone in an action, failing as timelib does.
func (sc *dtScanner) zone(t *dtToken) {
	if !sc.parseZone(t) {
		sc.addError("The timezone could not be found in the database")
	}
}

// haveTime is TIMELIB_HAVE_TIME; false aborts the action.
func (sc *dtScanner) haveTime() bool {
	if sc.t.haveTime != 0 {
		sc.addError("Double time specification")

		return false
	}

	sc.t.haveTime = 1
	sc.t.h, sc.t.i, sc.t.s, sc.t.us = 0, 0, 0, 0

	return true
}

// unhaveTime is TIMELIB_UNHAVE_TIME.
func (sc *dtScanner) unhaveTime() {
	sc.t.haveTime = 0
	sc.t.h, sc.t.i, sc.t.s, sc.t.us = 0, 0, 0, 0
}

// haveDate is TIMELIB_HAVE_DATE; false aborts the action.
func (sc *dtScanner) haveDate() bool {
	if sc.t.haveDate != 0 {
		sc.addError("Double date specification")

		return false
	}

	sc.t.haveDate = 1

	return true
}

// unhaveDate is TIMELIB_UNHAVE_DATE.
func (sc *dtScanner) unhaveDate() {
	sc.t.haveDate = 0
	sc.t.y, sc.t.m, sc.t.d = 0, 0, 0
}

// haveTZ is TIMELIB_HAVE_TZ: a second zone is a warning (the token is
// ignored), a third an error.
func (sc *dtScanner) haveTZ() bool {
	if sc.t.haveZone > 0 {
		if sc.t.haveZone > 1 {
			sc.addError("Double timezone specification")
		}

		sc.t.haveZone++

		return false
	}

	sc.t.haveZone++

	return true
}

// processYear is TIMELIB_PROCESS_YEAR: two-digit years are 1970-2069.
func processYear(y int64, length int) int64 {
	if y == dtUnset || length >= 4 || y >= 100 {
		return y
	}

	if y < 70 {
		return y + 2000
	}

	return y + 1900
}

// apply runs the action of a rule on its token; true means the rule's
// action is not ported.
func (sc *dtScanner) apply(rule int, token []byte) bool {
	t := &dtToken{b: token}
	p := &sc.t

	switch rule {
	case dtRuleYesterday, dtRuleTomorrow:
		p.haveRelative = true
		sc.unhaveTime()

		p.relD = 1
		if rule == dtRuleYesterday {
			p.relD = -1
		}
	case dtRuleNow, dtRuleDotComma, dtRuleSpace, dtRuleNul:
	case dtRuleNoon:
		sc.unhaveTime()
		sc.haveTime()

		p.h = 12
	case dtRuleMidnightToday:
		sc.unhaveTime()
	case dtRuleTimestamp, dtRuleTimestampMs:
		p.haveRelative = true
		sc.unhaveDate()
		sc.unhaveTime()

		if !sc.haveTZ() {
			return false
		}

		neg := t.at(1) == '-'
		i := sc.getSignedNr(t, 24)

		if rule == dtRuleTimestampMs {
			before := t.p
			us := sc.getSignedNr(t, 6)

			us = int64(float64(us) * math.Pow(10, float64(7-(t.p-before))))
			if neg {
				us = -us
			}

			p.relUS = us
		}

		p.y, p.m, p.d, p.h, p.i, p.s, p.us = 1970, 1, 1, 0, 0, 0, 0
		p.relS += i
		p.zoneType, p.z, p.dst = zoneOffset, 0, 0
	case dtRuleTime12:
		if !sc.haveTime() {
			return false
		}

		p.h = t.nr(2)
		if c := t.cur(); c == ':' || c == '.' {
			p.i = t.nr(2)
			if c := t.cur(); c == ':' || c == '.' {
				p.s = t.nr(2)
			}
		}

		t.eatSpaces()
		p.h += t.meridian(p.h)
	case dtRuleMssqlTime:
		if !sc.haveTime() {
			return false
		}

		p.h = t.nr(2)

		p.i = t.nr(2)
		if c := t.cur(); c == ':' || c == '.' {
			p.s = t.nr(2)
			if c := t.cur(); c == ':' || c == '.' {
				p.us = t.getFracNr()
			}
		}

		t.eatSpaces()
		p.h += t.meridian(p.h)
	case dtRuleTime24:
		if !sc.haveTime() {
			return false
		}

		p.h = t.nr(2)
		if c := t.cur(); c == ':' || c == '.' {
			p.i = t.nr(2)
			if c := t.cur(); c == ':' || c == '.' {
				p.s = t.nr(2)
				if t.cur() == '.' {
					p.us = t.getFracNr()
				}
			}
		}

		if t.cur() != 0 {
			sc.zone(t)
		}
	case dtRuleGnuNoColon:
		switch p.haveTime {
		case 0:
			p.h = t.nr(2)
			p.i = t.nr(2)
			p.s = 0
		case 1:
			p.y = t.nr(4)
		default:
			sc.addError("Double time specification")

			return false
		}

		p.haveTime++
	case dtRuleISO8601NoColon:
		if !sc.haveTime() {
			return false
		}

		p.h = t.nr(2)
		p.i = t.nr(2)

		p.s = t.nr(2)
		if t.cur() != 0 {
			sc.zone(t)
		}
	case dtRuleAmerican:
		if !sc.haveDate() {
			return false
		}

		p.m = t.nr(2)

		p.d = t.nr(2)
		if t.cur() == '/' {
			p.y = processYear(t.getNr(4))
		}
	case dtRuleISO8601Date4:
		if !sc.haveDate() {
			return false
		}

		p.y = sc.getSignedNr(t, 4)
		p.m = t.nr(2)
		p.d = t.nr(2)
	case dtRuleISO8601Date2, dtRuleGnuDateShort:
		if !sc.haveDate() {
			return false
		}

		y, length := t.getNr(4)
		p.m = t.nr(2)
		p.d = t.nr(2)
		p.y = processYear(y, length)
	case dtRuleISO8601DateX:
		if !sc.haveDate() {
			return false
		}

		p.y = sc.getSignedNr(t, 19)
		p.m = t.nr(2)
		p.d = t.nr(2)
	case dtRuleGnuDateShorter:
		if !sc.haveDate() {
			return false
		}

		y, length := t.getNr(4)
		p.m = t.nr(2)
		p.d = 1
		p.y = processYear(y, length)
	case dtRuleDateFull:
		if !sc.haveDate() {
			return false
		}

		p.d = t.nr(2)
		t.skipDaySuffix()
		p.m = t.getMonth()
		p.y = processYear(t.getNr(4))
	case dtRulePointedDate4:
		if !sc.haveDate() {
			return false
		}

		p.d = t.nr(2)
		p.m = t.nr(2)
		p.y = t.nr(4)
	case dtRulePointedDate2:
		if !sc.haveDate() {
			return false
		}

		p.d = t.nr(2)
		p.m = t.nr(2)
		p.y = processYear(t.getNr(2))
	case dtRuleDateNoDay:
		if !sc.haveDate() {
			return false
		}

		p.m = t.getMonth()
		p.y = processYear(t.getNr(4))
		p.d = 1
	case dtRuleDateNoDayRev:
		if !sc.haveDate() {
			return false
		}

		y, length := t.getNr(4)
		p.m = t.getMonth()
		p.d = 1
		p.y = processYear(y, length)
	case dtRuleDateTextual, dtRulePgTextShort:
		if !sc.haveDate() {
			return false
		}

		p.m = t.getMonth()
		p.d = t.nr(2)
		p.y = processYear(t.getNr(4))
	case dtRuleDateNoYearRev:
		if !sc.haveDate() {
			return false
		}

		p.d = t.nr(2)
		t.skipDaySuffix()
		p.m = t.getMonth()
	case dtRuleDateNoColon:
		if !sc.haveDate() {
			return false
		}

		p.y = t.nr(4)
		p.m = t.nr(2)
		p.d = t.nr(2)
	case dtRuleXMLRPC:
		if !sc.haveTime() || !sc.haveDate() {
			return false
		}

		p.y = t.nr(4)
		p.m = t.nr(2)
		p.d = t.nr(2)
		p.h = t.nr(2)
		p.i = t.nr(2)

		p.s = t.nr(2)
		if t.cur() == '.' {
			p.us = t.getFracNr()
			if t.cur() != 0 {
				sc.zone(t)
			}
		}
	case dtRulePgYDotD:
		if !sc.haveDate() {
			return false
		}

		y, length := t.getNr(4)
		p.d = t.nr(3)
		p.m = 1
		p.y = processYear(y, length)
	case dtRuleISOWeekDay, dtRuleISOWeek:
		if !sc.haveDate() {
			return false
		}

		p.haveRelative = true

		p.y = t.nr(4)
		w := t.nr(2)

		d := int64(1)
		if rule == dtRuleISOWeekDay {
			d = t.nr(1)
		}

		p.m, p.d = 1, 1
		p.relD = dayNrFromWeekNr(p.y, w, d)
	case dtRulePgTextReverse:
		if !sc.haveDate() {
			return false
		}

		y, length := t.getNr(4)
		p.m = t.getMonth()
		p.d = t.nr(2)
		p.y = processYear(y, length)
	case dtRuleCLF:
		if !sc.haveTime() || !sc.haveDate() {
			return false
		}

		p.d = t.nr(2)
		p.m = t.getMonth()
		p.y = t.nr(4)
		p.h = t.nr(2)
		p.i = t.nr(2)
		p.s = t.nr(2)

		t.eatSpaces()
		sc.zone(t)
	case dtRuleYear4:
		p.y = t.nr(4)
	case dtRuleAgo:
		// of the relative fields ported, "ago" negates days and seconds
		// (not microseconds); weekday relatives are not ported.
		p.relD, p.relS = -p.relD, -p.relS
	case dtRuleMonthText:
		if !sc.haveDate() {
			return false
		}

		p.m = t.lookupMonth()
	case dtRuleTimezone:
		if !sc.haveTZ() {
			return false
		}

		t.eatSpaces()
		sc.zone(t)
	case dtRuleDateShortWithTime12, dtRuleDateShortWithTime24:
		if !sc.haveDate() {
			return false
		}

		p.m = t.getMonth()
		p.d = t.nr(2)

		if !sc.haveTime() {
			return false
		}

		p.h = t.nr(2)
		p.i = t.nr(2)

		sep := t.cur()
		if sep == ':' || (sep == '.' && rule == dtRuleDateShortWithTime12) {
			p.s = t.nr(2)
			if t.cur() == '.' {
				p.us = t.getFracNr()
			}
		}

		if rule == dtRuleDateShortWithTime12 {
			p.h += t.meridian(p.h)
		} else if t.cur() != 0 {
			sc.zone(t)
		}
	case dtRuleAny:
		sc.addError("Unexpected character")
	default:
		return true
	}

	return false
}

// dayNrFromWeekNr is timelib_daynr_from_weeknr: the day of the year (from
// January 1st) of day d of ISO week w.
func dayNrFromWeekNr(y, w, d int64) int64 {
	dow := int64(time.Date(int(y), time.January, 1, 0, 0, 0, 0, time.UTC).Weekday())

	day := -dow
	if dow > 4 {
		day = 7 - dow
	}

	return day + (w-1)*7 + d
}

// resolve ports what php_date_initialize does with the parsed time:
// timelib_fill_holes from now (in UTC, the constructor's zone), then
// timelib_update_ts (relative fields, then the zone).
func (p *dtParsed) resolve(now time.Time) time.Time {
	now = now.UTC()

	y, m, d, h, i, s, us := p.y, p.m, p.d, p.h, p.i, p.s, p.us

	if p.haveDate != 0 && p.haveTime == 0 {
		h, i, s, us = 0, 0, 0, 0
	}

	if us == dtUnset {
		us = 0
		if y == dtUnset && m == dtUnset && d == dtUnset && h == dtUnset && i == dtUnset && s == dtUnset {
			us = int64(now.Nanosecond() / 1000)
		}
	}

	fill := func(v *int64, from int) {
		if *v == dtUnset {
			*v = int64(from)
		}
	}

	fill(&y, now.Year())
	fill(&m, int(now.Month()))
	fill(&d, now.Day())
	fill(&h, now.Hour())
	fill(&i, now.Minute())
	fill(&s, now.Second())

	// the wall clock read as UTC (timelib's sse before do_adjust_timezone);
	// relS is only set by "@timestamp", whose zone is +00:00, so adding it
	// afterwards equals timelib's adding it to the wall clock
	sse := time.Date(int(y), time.Month(m), int(d+p.relD), int(h), int(i), int(s), 0, time.UTC).Unix()

	loc := time.UTC

	switch p.zoneType {
	case zoneOffset:
		loc = fixedZone(int(p.z))
		sse -= p.z
	case zoneAbbr:
		loc = fixedZone(int(p.z + p.dst*3600))
		sse -= p.z + p.dst*3600
	case zoneID:
		loc = p.loc
		sse += p.idAdjustment(sse)
	}

	return time.Unix(sse+p.relS, (us+p.relUS)*1000).In(loc)
}

// zoneOffsetInfo is timelib_get_time_zone_offset_info: the offset in
// effect at ts, when that period began (math.MinInt64 for the first) and
// whether it is DST.
func zoneOffsetInfo(ts int64, loc *time.Location) (offset, transition int64, isDST bool) {
	t := time.Unix(ts, 0).In(loc)
	_, off := t.Zone()

	transition = math.MinInt64
	if start, _ := t.ZoneBounds(); !start.IsZero() {
		transition = start.Unix()
	}

	return int64(off), transition, t.IsDST()
}

// idAdjustment ports do_adjust_timezone for a zone identifier: how
// timelib picks the offset of a wall clock time in a DST gap or overlap
// (time.Date resolves those differently).
func (p *dtParsed) idAdjustment(sse int64) int64 {
	current, _, currentDST := zoneOffsetInfo(sse, p.loc)
	after, afterTransition, _ := zoneOffsetInfo(sse-current, p.loc)

	actual, actualTransition := after, afterTransition

	// tz->dst: unset (non-zero) unless an abbreviation was read
	dst := p.dst != 0

	if current == after && p.haveZone != 0 {
		switch {
		case current >= 0 && dst && !currentDST:
			if earlier, earlierTransition, _ := zoneOffsetInfo(sse-current-7200, p.loc); earlier != after && sse-earlier < afterTransition {
				actual, actualTransition = earlier, earlierTransition
			}
		case current <= 0 && currentDST && !dst:
			if later, laterTransition, _ := zoneOffsetInfo(sse-current+7200, p.loc); later != after && sse-later >= laterTransition {
				actual, actualTransition = later, laterTransition
			}
		}
	}

	inTransition := actualTransition != math.MinInt64 &&
		sse-actual >= actualTransition+(current-actual) &&
		sse-actual < actualTransition

	if current != actual && !inTransition {
		return -actual
	}

	return -current
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
