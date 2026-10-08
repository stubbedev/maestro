package loader

import (
	"reflect"
	"testing"
	"time"
)

// dayNrFromWeekNr counts from the weekday of January 1st, which must be the
// proleptic Gregorian one time.Date gives, for every year of an era.
func TestDayNrFromWeekNr_JanuaryFirst(t *testing.T) {
	for y := -400; y <= 10000; y++ {
		jan1 := int64(time.Date(y, time.January, 1, 0, 0, 0, 0, time.UTC).Weekday())

		want := -jan1
		if jan1 > 4 {
			want = 7 - jan1
		}

		if got := dayNrFromWeekNr(int64(y), 1, 0); got != want {
			t.Fatalf("dayNrFromWeekNr(%d, 1, 0) = %d, want %d", y, got, want)
		}
	}
}

// quickStrToTime must read every string it accepts exactly as the
// scanner does.
func TestQuickStrToTime_MatchesScanner(t *testing.T) {
	var cases []string

	for _, date := range []string{"2012-01-01", "0000-00-00", "9999-12-31", "2012-12-31", "2012-13-01", "2012-01-32", "2012-00-00", "2012-1-01", "x012-01-01", "2012/01/01"} {
		cases = append(cases, date)

		for _, sep := range []string{" ", "T", "t", "  "} {
			for _, clock := range []string{"10:00:00", "00:00:00", "24:00:00", "25:00:00", "23:59:60", "23:60:00", "23:59:61", "1:00:00", "10:00", "10:00:00.5", "10:00:00.123456", "10:00:00.1234567", "10:00:00.", "10:00:00.05"} {
				for _, zone := range []string{"", "Z", "z", "+00:00", "-05:30", "+24:59", "+25:00", "+05:60", "+0530", "+05", " UTC", "+05:30:00", "x"} {
					cases = append(cases, date+sep+clock+zone)
				}
			}
		}
	}

	cases = append(cases, "@0", "@-1", "@1700000000", "@999999999999999999", "@9999999999999999999", "@-", "@", "@1.5", "@ 1", "@--1", "@+1", "@1x")

	quick := 0

	for _, s := range cases {
		got, ok := quickStrToTime(s)
		if !ok {
			continue
		}

		quick++

		want, err := strToTime(s)
		if err != nil {
			t.Errorf("%q: quick path accepts it, the scanner fails: %v", s, err)

			continue
		}

		if !reflect.DeepEqual(got, *want) {
			t.Errorf("%q:\nquick   %+v\nscanner %+v", s, got, *want)
		}
	}

	if quick < 100 {
		t.Errorf("only %d cases took the quick path", quick)
	}
}
