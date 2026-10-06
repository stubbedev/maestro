// Ports timelib's relative time handling as PHP 8.4 ships it (timelib
// 2022.17): the actions of the relative rules of parse_date.re
// (timelib_set_relative and its lookups), timelib_fill_holes, and
// timelib_update_ts of tm2unixtime.c (do_adjust_special_early,
// do_adjust_relative, do_adjust_special, timelib_do_normalize,
// do_adjust_timezone), which php_date_initialize runs on the parsed time.

package loader

import "time"

// The special relative types and first/last day of (timelib_private.h).
const (
	specialWeekday              = 1 // TIMELIB_SPECIAL_WEEKDAY
	specialDayOfWeekInMonth     = 2 // TIMELIB_SPECIAL_DAY_OF_WEEK_IN_MONTH
	specialLastDayOfWeekInMonth = 3 // TIMELIB_SPECIAL_LAST_DAY_OF_WEEK_IN_MONTH

	firstDayOfMonth = 1 // TIMELIB_SPECIAL_FIRST_DAY_OF_MONTH
	lastDayOfMonth  = 2 // TIMELIB_SPECIAL_LAST_DAY_OF_MONTH
)

// The units of timelib_relunit.
const (
	unitMicrosec = iota
	unitSecond
	unitMinute
	unitHour
	unitDay
	unitMonth
	unitYear
	unitWeekday
	unitSpecial
)

type relUnit struct {
	unit, multiplier int64
}

// relUnits is timelib_relunit_lookup (names compared case-insensitively;
// "µ" is the UTF-8 micro sign).
var relUnits = map[string]relUnit{
	"ms": {unitMicrosec, 1000}, "msec": {unitMicrosec, 1000}, "msecs": {unitMicrosec, 1000},
	"millisecond": {unitMicrosec, 1000}, "milliseconds": {unitMicrosec, 1000},
	"µs": {unitMicrosec, 1}, "usec": {unitMicrosec, 1}, "usecs": {unitMicrosec, 1}, "µsec": {unitMicrosec, 1},
	"µsecs": {unitMicrosec, 1}, "microsecond": {unitMicrosec, 1}, "microseconds": {unitMicrosec, 1},
	"sec": {unitSecond, 1}, "secs": {unitSecond, 1}, "second": {unitSecond, 1}, "seconds": {unitSecond, 1},
	"min": {unitMinute, 1}, "mins": {unitMinute, 1}, "minute": {unitMinute, 1}, "minutes": {unitMinute, 1},
	"hour": {unitHour, 1}, "hours": {unitHour, 1},
	"day": {unitDay, 1}, "days": {unitDay, 1}, "week": {unitDay, 7}, "weeks": {unitDay, 7},
	"fortnight": {unitDay, 14}, "fortnights": {unitDay, 14}, "forthnight": {unitDay, 14}, "forthnights": {unitDay, 14},
	"month": {unitMonth, 1}, "months": {unitMonth, 1}, "year": {unitYear, 1}, "years": {unitYear, 1},
	"mondays": {unitWeekday, 1}, "monday": {unitWeekday, 1}, "mon": {unitWeekday, 1},
	"tuesdays": {unitWeekday, 2}, "tuesday": {unitWeekday, 2}, "tue": {unitWeekday, 2},
	"wednesdays": {unitWeekday, 3}, "wednesday": {unitWeekday, 3}, "wed": {unitWeekday, 3},
	"thursdays": {unitWeekday, 4}, "thursday": {unitWeekday, 4}, "thu": {unitWeekday, 4},
	"fridays": {unitWeekday, 5}, "friday": {unitWeekday, 5}, "fri": {unitWeekday, 5},
	"saturdays": {unitWeekday, 6}, "saturday": {unitWeekday, 6}, "sat": {unitWeekday, 6},
	"sundays": {unitWeekday, 0}, "sunday": {unitWeekday, 0}, "sun": {unitWeekday, 0},
	"weekday": {unitSpecial, specialWeekday}, "weekdays": {unitSpecial, specialWeekday},
}

// relTexts is timelib_reltext_lookup: the value and the behavior.
var relTexts = map[string][2]int64{
	"first": {1, 0}, "next": {1, 0}, "second": {2, 0}, "third": {3, 0}, "fourth": {4, 0}, "fifth": {5, 0},
	"sixth": {6, 0}, "seventh": {7, 0}, "eight": {8, 0}, "eighth": {8, 0}, "ninth": {9, 0}, "tenth": {10, 0},
	"eleventh": {11, 0}, "twelfth": {12, 0}, "last": {-1, 0}, "previous": {-1, 0}, "this": {0, 1},
}

// asciiLower is timelib_strcasecmp's tolower (ASCII only).
func asciiLower(b []byte) string {
	out := make([]byte, len(b))
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}

		out[i] = c
	}

	return string(out)
}

// lookupRelunit is timelib_lookup_relunit: the word up to a separator.
func (t *dtToken) lookupRelunit() (relUnit, bool) {
	begin := t.p

	for {
		switch t.cur() {
		case 0, ' ', ',', '\t', ';', ':', '/', '.', '-', '(', ')':
			u, ok := relUnits[asciiLower(t.b[begin:t.p])]

			return u, ok
		}

		t.p++
	}
}

// getRelativeText is timelib_get_relative_text: behavior is left as it
// is for an unknown word.
func (t *dtToken) getRelativeText(behavior *int64) int64 {
	for c := t.cur(); c == ' ' || c == '\t' || c == '-' || c == '/'; c = t.cur() {
		t.p++
	}

	begin := t.p
	for isLetter(t.cur()) {
		t.p++
	}

	v, ok := relTexts[asciiLower(t.b[begin:t.p])]
	if !ok {
		return 0
	}

	*behavior = v[1]

	return v[0]
}

// addWithOverflow is timelib's add_with_overflow
// (__builtin_saddll_overflow: the wrapped sum is kept).
func (sc *dtScanner) addWithOverflow(e *int64, amount, multiplier int64) {
	add := amount * multiplier
	sum := *e + add

	if (add >= 0 && sum < *e) || (add < 0 && sum > *e) {
		sc.addError("Number out of range")
	}

	*e = sum
}

// setRelative is timelib_set_relative; keep is TIMELIB_TIME_PART_KEEP.
func (sc *dtScanner) setRelative(t *dtToken, amount, behavior int64, keep bool) {
	u, ok := t.lookupRelunit()
	if !ok {
		return
	}

	p := &sc.t

	switch u.unit {
	case unitMicrosec:
		sc.addWithOverflow(&p.rel.us, amount, u.multiplier)
	case unitSecond:
		sc.addWithOverflow(&p.rel.s, amount, u.multiplier)
	case unitMinute:
		sc.addWithOverflow(&p.rel.i, amount, u.multiplier)
	case unitHour:
		sc.addWithOverflow(&p.rel.h, amount, u.multiplier)
	case unitDay:
		sc.addWithOverflow(&p.rel.d, amount, u.multiplier)
	case unitMonth:
		sc.addWithOverflow(&p.rel.m, amount, u.multiplier)
	case unitYear:
		sc.addWithOverflow(&p.rel.y, amount, u.multiplier)
	case unitWeekday:
		p.haveRelative, p.rel.haveWeekdayRelative = true, true
		if !keep {
			sc.unhaveTime()
		}

		if amount > 0 {
			p.rel.d += (amount - 1) * 7
		} else {
			p.rel.d += amount * 7
		}

		p.rel.weekday, p.rel.weekdayBehavior = u.multiplier, behavior
	case unitSpecial:
		p.haveRelative, p.rel.haveSpecialRelative = true, true
		if !keep {
			sc.unhaveTime()
		}

		p.rel.specialType, p.rel.specialAmount = u.multiplier, amount
	}
}

// applyRelative runs the actions of the relative rules (parse_date.re).
func (sc *dtScanner) applyRelative(rule int, t *dtToken) {
	p := &sc.t

	// the token loops of relativetext, relativetextweek and relative run
	// once for every token their rules match; the guard only stops a
	// loop that would not advance
	loop := func(step func()) {
		for t.cur() != 0 {
			before := t.p
			step()

			if t.p == before {
				return
			}
		}
	}

	switch rule {
	case dtRuleFirstLastDayOf:
		p.haveRelative = true

		p.rel.firstLastDayOf = firstDayOfMonth
		if c := t.cur(); c == 'l' || c == 'L' {
			p.rel.firstLastDayOf = lastDayOfMonth
		}
	case dtRuleBackFrontOf:
		sc.unhaveTime()
		sc.haveTime()

		// timelib tests for a lowercase 'b' only: "Back of" is "front of"
		if t.cur() == 'b' {
			p.h, p.i = t.nr(2), 15
		} else {
			p.h, p.i = t.nr(2)-1, 45
		}

		if t.cur() != 0 {
			t.eatSpaces()
			p.h += t.meridian(p.h)
		}
	case dtRuleWeekdayOf:
		p.haveRelative, p.rel.haveSpecialRelative = true, true

		var behavior int64

		i := t.getRelativeText(&behavior)
		t.eatSpaces()

		if i > 0 {
			p.rel.specialType = specialDayOfWeekInMonth
			sc.setRelative(t, i, 1, false)
		} else {
			p.rel.specialType = specialLastDayOfWeekInMonth
			sc.setRelative(t, i, behavior, false)
		}
	case dtRuleAgo:
		// microseconds are not negated
		r := &p.rel
		r.y, r.m, r.d, r.h, r.i, r.s = -r.y, -r.m, -r.d, -r.h, -r.i, -r.s

		r.weekday = -r.weekday
		if r.weekday == 0 {
			r.weekday = -7
		}

		if r.haveSpecialRelative && r.specialType == specialWeekday {
			r.specialAmount = -r.specialAmount
		}
	case dtRuleDayText:
		p.haveRelative, p.rel.haveWeekdayRelative = true, true
		sc.unhaveTime()

		// "weekday(s)" is TIMELIB_SPECIAL with multiplier 1: Monday
		u, _ := t.lookupRelunit()

		p.rel.weekday = u.multiplier
		if p.rel.weekdayBehavior != 2 {
			p.rel.weekdayBehavior = 1
		}
	case dtRuleRelativeTextWeek:
		p.haveRelative = true

		var behavior int64

		loop(func() {
			i := t.getRelativeText(&behavior)
			t.eatSpaces()
			sc.setRelative(t, i, behavior, false)
			p.rel.weekdayBehavior = 2

			// "weekday + last/this/next week"
			if !p.rel.haveWeekdayRelative {
				p.rel.haveWeekdayRelative = true
				p.rel.weekday = 1
			}
		})
	case dtRuleRelativeText:
		p.haveRelative = true

		var behavior int64

		loop(func() {
			i := t.getRelativeText(&behavior)
			t.eatSpaces()
			sc.setRelative(t, i, behavior, false)
		})
	case dtRuleRelative:
		p.haveRelative = true

		loop(func() {
			i := sc.getSignedNr(t, 24)
			t.eatSpaces()
			sc.setRelative(t, i, 1, true)
		})
	}
}

// resolve ports what php_date_initialize does with the parsed time:
// timelib_fill_holes from now (in UTC, the constructor's zone), then
// timelib_update_ts (special and relative adjustments, normalisation,
// the zone) and the timestamp timelib_update_from_sse reads back.
func (p *dtParsed) resolve(now time.Time) time.Time {
	t := *p
	t.fillHoles(now.UTC())

	sse := t.updateTS()

	loc := time.UTC

	switch t.zoneType {
	case zoneOffset:
		loc = fixedZone(int(t.z))
	case zoneAbbr:
		loc = fixedZone(int(t.z + t.dst*3600))
	case zoneID:
		loc = t.loc
	}

	return time.Unix(sse, t.us*1000).In(loc)
}

// fillHoles is timelib_fill_holes with TIMELIB_NO_CLONE: the zone fields
// of now (UTC, an identifier) leave a parsed zone as it is.
func (p *dtParsed) fillHoles(now time.Time) {
	if p.haveDate != 0 && p.haveTime == 0 {
		p.h, p.i, p.s, p.us = 0, 0, 0, 0
	}

	if p.us == dtUnset {
		p.us = 0
		if p.y == dtUnset && p.m == dtUnset && p.d == dtUnset && p.h == dtUnset && p.i == dtUnset && p.s == dtUnset {
			p.us = int64(now.Nanosecond() / 1000)
		}
	}

	fill := func(v *int64, from int) {
		if *v == dtUnset {
			*v = int64(from)
		}
	}

	fill(&p.y, now.Year())
	fill(&p.m, int(now.Month()))
	fill(&p.d, now.Day())
	fill(&p.h, now.Hour())
	fill(&p.i, now.Minute())
	fill(&p.s, now.Second())
}

// updateTS is timelib_update_ts: the timestamp of the adjusted time.
func (p *dtParsed) updateTS() int64 {
	p.adjustSpecialEarly()
	p.adjustRelative()
	p.adjustSpecial()

	// timelib adds the days in two halves to stay in int64's range
	days := epochDaysFromTime(p.y, p.m, p.d)
	sse := p.h*3600 + p.i*60 + p.s
	sse += days * (86400 / 2)
	sse += days * (86400 / 2)

	// do_adjust_timezone; no zone is the constructor's UTC
	switch p.zoneType {
	case zoneOffset:
		sse -= p.z
	case zoneAbbr:
		sse -= p.z + p.dst*3600
	case zoneID:
		sse += p.idAdjustment(sse)
	}

	return sse
}

// adjustSpecialEarly is do_adjust_special_early.
func (p *dtParsed) adjustSpecialEarly() {
	if p.rel.haveSpecialRelative {
		switch p.rel.specialType {
		case specialDayOfWeekInMonth:
			p.d = 1
			p.m += p.rel.m
			p.rel.m = 0
		case specialLastDayOfWeekInMonth:
			p.d = 1
			p.m += p.rel.m + 1
			p.rel.m = 0
		}
	}

	p.firstLastDayOf()
	p.normalize()
}

// firstLastDayOf applies "first/last day of" (both adjustments do).
func (p *dtParsed) firstLastDayOf() {
	switch p.rel.firstLastDayOf {
	case firstDayOfMonth:
		p.d = 1
	case lastDayOfMonth:
		p.d = 0
		p.m++
	}
}

// adjustRelative is do_adjust_relative.
func (p *dtParsed) adjustRelative() {
	if p.rel.haveWeekdayRelative {
		p.adjustForWeekday()
	}

	p.normalize()

	if p.haveRelative {
		p.us += p.rel.us
		p.s += p.rel.s
		p.i += p.rel.i
		p.h += p.rel.h
		p.d += p.rel.d
		p.m += p.rel.m
		p.y += p.rel.y
	}

	p.firstLastDayOf()
	p.normalize()
}

// adjustForWeekday is do_adjust_for_weekday.
func (p *dtParsed) adjustForWeekday() {
	r := &p.rel
	current := dayOfWeek(p.y, p.m, p.d)

	if r.weekdayBehavior == 2 {
		// "this week" on a Sunday
		if current == 0 && r.weekday != 0 {
			r.weekday -= 7
		}

		// "sunday this week" on another day
		if r.weekday == 0 && current != 0 {
			r.weekday = 7
		}

		p.d += r.weekday - current

		return
	}

	difference := r.weekday - current
	if (r.d < 0 && difference < 0) || (r.d >= 0 && difference <= -r.weekdayBehavior) {
		difference += 7
	}

	if r.weekday >= 0 {
		p.d += difference
	} else {
		weekday := r.weekday
		if weekday < 0 {
			weekday = -weekday
		}

		p.d -= 7 - (weekday - current)
	}

	r.haveWeekdayRelative = false
}

// adjustSpecial is do_adjust_special.
func (p *dtParsed) adjustSpecial() {
	if p.rel.haveSpecialRelative && p.rel.specialType == specialWeekday {
		p.adjustSpecialWeekday()
	}

	p.normalize()
	p.rel.specialType, p.rel.specialAmount = 0, 0
}

// adjustSpecialWeekday is do_adjust_special_weekday ("+N weekdays").
func (p *dtParsed) adjustSpecialWeekday() {
	count := p.rel.specialAmount
	dow := dayOfWeek(p.y, p.m, p.d)

	// increments of 5 weekdays are a week
	p.d += (count / 5) * 7

	rem := count % 5

	if count > 0 {
		switch {
		case rem == 0:
			// back to Friday when stopping on the weekend
			switch dow {
			case 0:
				p.d -= 2
			case 6:
				p.d--
			}
		case dow == 6:
			p.d++
		case dow+rem > 5:
			p.d += 2
		}
	} else {
		switch {
		case rem == 0:
			// the mirror of the forward direction (also for 0: starting
			// on the weekend moves forward)
			switch dow {
			case 6:
				p.d += 2
			case 0:
				p.d++
			}
		case dow == 0:
			p.d--
		case dow+rem < 1:
			p.d -= 2
		}
	}

	p.d += rem
}

// rangeLimit is do_range_limit: a into [start, end), carrying into b.
func rangeLimit(start, end, adj int64, a, b *int64) {
	if *a < start {
		aPlus1 := *a + 1
		*b -= (start-aPlus1)/adj + 1
		*a += adj * ((start - aPlus1) / adj)
		*a += adj
	}

	if *a >= end {
		*b += *a / adj
		*a -= adj * (*a / adj)
	}
}

// daysInMonth and daysInMonthLeap are tm2unixtime.c's tables (0 is
// December).
var (
	daysInMonth     = [13]int64{31, 31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}
	daysInMonthLeap = [13]int64{31, 31, 29, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}
)

func isLeap(y int64) bool { return y%4 == 0 && (y%100 != 0 || y%400 == 0) }

const (
	daysPerEra        = 146097
	yearsPerEra       = 400
	hinnantEpochShift = 719468
)

// rangeLimitDays is do_range_limit_days.
func rangeLimitDays(y, m, d *int64) bool {
	if *d >= daysPerEra || *d <= -daysPerEra {
		*y += yearsPerEra * (*d / daysPerEra)
		*d -= daysPerEra * (*d / daysPerEra)
	}

	rangeLimit(1, 13, 12, m, y)

	current := &daysInMonth
	if isLeap(*y) {
		current = &daysInMonthLeap
	}

	changed := false

	for *d <= 0 && *m > 0 {
		prevMonth, prevYear := *m-1, *y
		if prevMonth < 1 {
			prevMonth += 12
			prevYear--
		}

		if isLeap(prevYear) {
			*d += daysInMonthLeap[prevMonth]
		} else {
			*d += daysInMonth[prevMonth]
		}

		*m--
		changed = true
	}

	for *d > 0 && *m <= 12 && *d > current[*m] {
		*d -= current[*m]
		*m++
		changed = true
	}

	return changed
}

// normalize is timelib_do_normalize (every field is set here).
func (p *dtParsed) normalize() {
	rangeLimit(0, 1000000, 1000000, &p.us, &p.s)
	rangeLimit(0, 60, 60, &p.s, &p.i)
	rangeLimit(0, 60, 60, &p.i, &p.h)
	rangeLimit(0, 24, 24, &p.h, &p.d)
	rangeLimit(1, 13, 12, &p.m, &p.y)

	// the shortcut against the Epoch
	if p.y == 1970 && p.m == 1 {
		p.y, p.m, p.d = dateFromEpochDays(p.d - 1)

		return
	}

	for rangeLimitDays(&p.y, &p.m, &p.d) {
	}

	rangeLimit(1, 13, 12, &p.m, &p.y)
}

// dateFromEpochDays is timelib_date_from_epoch_days.
func dateFromEpochDays(epochDays int64) (y, m, d int64) {
	days := epochDays + hinnantEpochShift

	era := days
	if days < 0 {
		era = days - daysPerEra + 1
	}

	era /= daysPerEra

	dayOfEra := days - era*daysPerEra
	yearOfEra := (dayOfEra - dayOfEra/1460 + dayOfEra/36524 - dayOfEra/146096) / 365
	y = yearOfEra + era*yearsPerEra
	dayOfYear := dayOfEra - (365*yearOfEra + yearOfEra/4 - yearOfEra/100)
	monthPortion := (5*dayOfYear + 2) / 153
	d = dayOfYear - (153*monthPortion+2)/5 + 1

	m = monthPortion - 9
	if monthPortion < 10 {
		m = monthPortion + 3
	}

	if m <= 2 {
		y++
	}

	return y, m, d
}

// epochDaysFromTime is timelib_epoch_days_from_time.
func epochDaysFromTime(y, m, d int64) int64 {
	if m <= 2 {
		y--
	}

	era := y
	if y < 0 {
		era = y - 399
	}

	era /= yearsPerEra

	yearOfEra := y - era*yearsPerEra

	shift := int64(9)
	if m > 2 {
		shift = -3
	}

	dayOfYear := (153*(m+shift)+2)/5 + d - 1
	dayOfEra := yearOfEra*365 + yearOfEra/4 - yearOfEra/100 + dayOfYear

	return era*daysPerEra + dayOfEra - hinnantEpochShift
}

// The month tables of dow.c (1 is January).
var (
	dowMonthCommon = [13]int64{-1, 0, 3, 3, 6, 1, 4, 6, 2, 5, 0, 3, 5}
	dowMonthLeap   = [13]int64{-1, 6, 2, 3, 6, 1, 4, 6, 2, 5, 0, 3, 5}
)

func positiveMod(x, y int64) int64 {
	r := x % y
	if r < 0 {
		r += y
	}

	return r
}

// dayOfWeek is timelib_day_of_week (0 is Sunday); m is 1 to 12 after
// timelib_do_normalize, unless its arithmetic overflowed (where C reads
// outside its table, this returns Sunday).
func dayOfWeek(y, m, d int64) int64 {
	if m < 0 || m > 12 {
		return 0
	}

	c1 := 6 - positiveMod(positiveMod(y, 400)/100, 4)*2
	y1 := positiveMod(y, 100)

	m1 := dowMonthCommon[m]
	if isLeap(y) {
		m1 = dowMonthLeap[m]
	}

	return positiveMod(c1+y1+m1+y1/4+d, 7)
}
