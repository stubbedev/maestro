// Ports date_format() of ext/date/php_date.c (PHP 8.4): DateTime::format()
// and date().

package php

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

var (
	dayFullNames  = [...]string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}
	dayShortNames = [...]string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}
	monFullNames  = [...]string{"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"}
	monShortNames = [...]string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}
)

// englishSuffix is english_suffix(): the ordinal suffix of a day.
func englishSuffix(n int) string {
	if n >= 10 && n <= 19 {
		return "th"
	}
	switch n % 10 {
	case 1:
		return "st"
	case 2:
		return "nd"
	case 3:
		return "rd"
	}

	return "th"
}

// yearString is the year as "Y" prints it: at least four digits, "-"
// before years BCE.
func yearString(y int) string {
	if y < 0 {
		return fmt.Sprintf("-%04d", -y)
	}

	return fmt.Sprintf("%04d", y)
}

// offsetString is a UTC offset as "O" (sep "") or "P" (sep ":") print it.
func offsetString(offset int, sep string) string {
	sign := '+'
	if offset < 0 {
		sign = '-'
		offset = -offset
	}

	return fmt.Sprintf("%c%02d%s%02d", sign, offset/3600, sep, offset%3600/60)
}

// DateFormat is $dateTime->format($format) of a DateTime at t, in t's
// location (date($format, $ts) in the default time zone): every format
// character of PHP 8.4, a backslash escaping the next character, any
// other character as is.
func DateFormat(format string, t time.Time) string {
	var b strings.Builder

	zoneName, offset := t.Zone()
	day, month, year := t.Day(), int(t.Month()), t.Year()
	hour := t.Hour()
	isoYear, isoWeek := t.ISOWeek()

	for i := 0; i < len(format); i++ {
		c := format[i]
		switch c {
		// day
		case 'd':
			fmt.Fprintf(&b, "%02d", day)
		case 'D':
			b.WriteString(dayShortNames[t.Weekday()])
		case 'j':
			b.WriteString(strconv.Itoa(day))
		case 'l':
			b.WriteString(dayFullNames[t.Weekday()])
		case 'S':
			b.WriteString(englishSuffix(day))
		case 'w':
			b.WriteString(strconv.Itoa(int(t.Weekday())))
		case 'N':
			n := int(t.Weekday())
			if n == 0 {
				n = 7
			}
			b.WriteString(strconv.Itoa(n))
		case 'z':
			b.WriteString(strconv.Itoa(t.YearDay() - 1))

		// week
		case 'W':
			fmt.Fprintf(&b, "%02d", isoWeek)
		case 'o':
			b.WriteString(strconv.Itoa(isoYear))

		// month
		case 'F':
			b.WriteString(monFullNames[month-1])
		case 'm':
			fmt.Fprintf(&b, "%02d", month)
		case 'M':
			b.WriteString(monShortNames[month-1])
		case 'n':
			b.WriteString(strconv.Itoa(month))
		case 't':
			b.WriteString(strconv.Itoa(time.Date(year, time.Month(month)+1, 0, 0, 0, 0, 0, time.UTC).Day()))

		// year
		case 'L':
			if year%4 == 0 && (year%100 != 0 || year%400 == 0) {
				b.WriteByte('1')
			} else {
				b.WriteByte('0')
			}
		case 'y':
			fmt.Fprintf(&b, "%02d", (year%100+100)%100)
		case 'Y':
			b.WriteString(yearString(year))
		case 'x', 'X':
			if c == 'X' || year < 0 || year >= 10000 {
				sign := "+"
				if year < 0 {
					sign = "-"
				}
				abs := year
				if abs < 0 {
					abs = -abs
				}
				fmt.Fprintf(&b, "%s%04d", sign, abs)
			} else {
				fmt.Fprintf(&b, "%04d", year)
			}

		// time
		case 'a':
			if hour >= 12 {
				b.WriteString("pm")
			} else {
				b.WriteString("am")
			}
		case 'A':
			if hour >= 12 {
				b.WriteString("PM")
			} else {
				b.WriteString("AM")
			}
		case 'B':
			// Swatch Internet time: Biel Mean Time (UTC+1).
			beat := (int((t.Unix()%86400+86400)%86400) + 3600) * 10
			if beat < 0 {
				beat += 864000
			}
			fmt.Fprintf(&b, "%03d", (beat/864)%1000)
		case 'g':
			h := hour % 12
			if h == 0 {
				h = 12
			}
			b.WriteString(strconv.Itoa(h))
		case 'G':
			b.WriteString(strconv.Itoa(hour))
		case 'h':
			h := hour % 12
			if h == 0 {
				h = 12
			}
			fmt.Fprintf(&b, "%02d", h)
		case 'H':
			fmt.Fprintf(&b, "%02d", hour)
		case 'i':
			fmt.Fprintf(&b, "%02d", t.Minute())
		case 's':
			fmt.Fprintf(&b, "%02d", t.Second())
		case 'u':
			fmt.Fprintf(&b, "%06d", t.Nanosecond()/1000)
		case 'v':
			fmt.Fprintf(&b, "%03d", t.Nanosecond()/1000000)

		// timezone
		case 'I':
			if t.IsDST() {
				b.WriteByte('1')
			} else {
				b.WriteByte('0')
			}
		case 'p':
			if offset == 0 {
				b.WriteByte('Z')
			} else {
				b.WriteString(offsetString(offset, ":"))
			}
		case 'P':
			b.WriteString(offsetString(offset, ":"))
		case 'O':
			b.WriteString(offsetString(offset, ""))
		case 'T':
			b.WriteString(zoneName)
		case 'e':
			b.WriteString(t.Location().String())
		case 'Z':
			b.WriteString(strconv.Itoa(offset))

		// full date/time
		case 'c':
			fmt.Fprintf(&b, "%s-%02d-%02dT%02d:%02d:%02d%s", yearString(year), month, day, hour, t.Minute(), t.Second(), offsetString(offset, ":"))
		case 'r':
			fmt.Fprintf(&b, "%3s, %02d %3s %04d %02d:%02d:%02d %s", dayShortNames[t.Weekday()], day, monShortNames[month-1], year, hour, t.Minute(), t.Second(), offsetString(offset, ""))
		case 'U':
			b.WriteString(strconv.FormatInt(t.Unix(), 10))

		case '\\':
			// The escaped character; a trailing backslash reads the C
			// string's terminating NUL.
			i++
			if i < len(format) {
				b.WriteByte(format[i])
			} else {
				b.WriteByte(0)
			}

		default:
			b.WriteByte(c)
		}
	}

	return b.String()
}
