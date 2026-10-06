// Ports ShowCommand::getRelativeTime (src/Composer/Command/ShowCommand.php)
// with the parts of PHP it relies on: the default time zone date() and
// `new \DateTimeImmutable()` use, and DateTime::diff (timelib's
// timelib_diff, as PHP 8.1+ computes it).

package command

import (
	"strconv"
	"strings"
	"time"

	"github.com/stubbedev/maestro/internal/platform"
)

// getRelativeTime ports ShowCommand::getRelativeTime.
func (c *ShowCommand) getRelativeTime(releaseDate time.Time) string {
	var snap *platform.Snapshot
	if view, _, err := c.processRuntime().ComposerView(); err == nil {
		snap = view
	}

	return relativeTime(releaseDate, time.Now().In(phpDefaultTimezone(snap)))
}

// relativeTime is getRelativeTime at the moment now, which is in PHP's
// default time zone (date('Ymd') and new \DateTimeImmutable()).
func relativeTime(releaseDate, now time.Time) string {
	if releaseDate.Format("20060102") == now.Format("20060102") {
		return "today"
	}

	y, m, days := dateDiff(releaseDate, now)
	if days < 7 {
		return "this week"
	}

	if days < 14 {
		return "last week"
	}

	if m < 1 && days < 31 {
		return strconv.Itoa(days/7) + " weeks ago"
	}

	if y < 1 {
		s := ""
		if m > 1 {
			s = "s"
		}

		return strconv.Itoa(m) + " month" + s + " ago"
	}

	s := ""
	if y > 1 {
		s = "s"
	}

	return strconv.Itoa(y) + " year" + s + " ago"
}

// phpDefaultTimezone is date_default_timezone_get() of the PHP Composer
// runs on (platform.Snapshot.DefaultTimezone); UTC without php.
func phpDefaultTimezone(snap *platform.Snapshot) *time.Location {
	return snap.DefaultTimezone()
}

// timelibTime is what timelib keeps of a DateTime for diff(): its wall
// clock fields, offset and zone.
type timelibTime struct {
	y, m, d, h, i, s, us int64
	sse                  int64 // seconds since the epoch
	z                    int64 // UTC offset in seconds, DST included
	id                   string
	isID                 bool // zone type 3 (an identifier), else an offset
}

// timelibOf reads a time.Time as the DateTime it stands for: time.UTC and
// loaded locations are zone identifiers (PHP's 'UTC' default, zone names),
// fixed zones (named "+hh:mm" by internal/pkg/loader) are offsets.
func timelibOf(t time.Time) timelibTime {
	_, z := t.Zone()
	name := t.Location().String()

	return timelibTime{
		y: int64(t.Year()), m: int64(t.Month()), d: int64(t.Day()),
		h: int64(t.Hour()), i: int64(t.Minute()), s: int64(t.Second()), us: int64(t.Nanosecond() / 1000),
		sse:  t.Unix(),
		z:    int64(z),
		id:   name,
		isID: t.Location() == time.UTC || name != "" && !strings.HasPrefix(name, "+") && !strings.HasPrefix(name, "-"),
	}
}

// dateDiff is the y, m and days fields of $a->diff($b) (timelib_diff).
func dateDiff(a, b time.Time) (y, m, days int) {
	one, two := timelibOf(a), timelibOf(b)

	// sort_old_to_new
	invert := false
	if one.sse > two.sse || (one.sse == two.sse && one.us > two.us) {
		one, two = two, one
		invert = true
	}

	rt := relTime{
		y: two.y - one.y, m: two.m - one.m, d: two.d - one.d,
		h: two.h - one.h, i: two.i - one.i, s: two.s - one.s, us: two.us - one.us,
	}

	if !one.isID || !two.isID || one.id != two.id {
		// Different zones: the offset difference is taken out of the
		// seconds (timelib also moves a DST hour into the hours, which
		// comes to the same once normalized).
		rt.s += one.z - two.z
	}

	base := two
	if invert {
		base = one
	}

	rt.normalize(base.y, base.m, invert)

	return int(rt.y), int(rt.m), int(diffDays(&one, &two))
}

// diffDays is timelib_diff_days (one is the earlier time).
func diffDays(one, two *timelibTime) int64 {
	same := one.isID == two.isID && (one.isID && one.id == two.id || !one.isID && one.z == two.z)
	if !same {
		d := (one.sse - two.sse) / 86400
		if d < 0 {
			d = -d
		}

		return d
	}

	days := epochDays(two.y, two.m, two.d) - epochDays(one.y, one.m, one.d)
	if days < 0 {
		days = -days
	}

	if days > 0 && clockOf(two) < clockOf(one) {
		days--
	}

	return days
}

func clockOf(t *timelibTime) int64 {
	return ((t.h*60+t.i)*60+t.s)*1000000 + t.us
}

// epochDays is the day number of a calendar date.
func epochDays(y, m, d int64) int64 {
	return time.Date(int(y), time.Month(m), int(d), 0, 0, 0, 0, time.UTC).Unix() / 86400
}

// relTime is timelib_rel_time's fields.
type relTime struct {
	y, m, d, h, i, s, us int64
}

// normalize is timelib_do_rel_normalize relative to the base date by, bm.
func (rt *relTime) normalize(by, bm int64, invert bool) {
	if rt.us < 0 {
		rt.us += 1000000
		rt.s--
	} else if rt.us >= 1000000 {
		rt.us -= 1000000
		rt.s++
	}

	rangeLimit(0, 60, 60, &rt.s, &rt.i)
	rangeLimit(0, 60, 60, &rt.i, &rt.h)
	rangeLimit(0, 24, 24, &rt.h, &rt.d)
	rangeLimit(0, 12, 12, &rt.m, &rt.y)

	rangeLimitDaysRelative(by, bm, &rt.m, &rt.d, invert)
	rangeLimit(0, 12, 12, &rt.m, &rt.y)
}

// rangeLimit is timelib's do_range_limit.
func rangeLimit(start, end, adj int64, a, b *int64) {
	if *a < start {
		*b -= (start-*a-1)/adj + 1
		*a += adj * ((start-*a-1)/adj + 1)
	}

	if *a >= end {
		*b += *a / adj
		*a -= adj * (*a / adj)
	}
}

// rangeLimitDaysRelative is timelib's do_range_limit_days_relative:
// negative days borrow whole months, counted from the month before the
// base date's (or from the base month on, inverted).
func rangeLimitDaysRelative(by, bm int64, m, d *int64, invert bool) {
	rangeLimit(1, 13, 12, &bm, &by)

	year, month := by, bm

	for *d < 0 {
		if !invert {
			month--
			if month < 1 {
				month += 12
				year--
			}

			*d += daysInMonth(year, month)
		} else {
			*d += daysInMonth(year, month)

			month++
			if month > 12 {
				month -= 12
				year++
			}
		}

		*m--
	}
}

func daysInMonth(y, m int64) int64 {
	return int64(time.Date(int(y), time.Month(m)+1, 0, 0, 0, 0, 0, time.UTC).Day())
}
