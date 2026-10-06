package php

import (
	"testing"
	"time"
)

func TestDateFormat(t *testing.T) {
	const format = `d D j l N S w z W F m M n t L o y Y x X a A B g G h H i s u v e I O P p T Z c r U \Y\\`

	// (new DateTime($when, new DateTimeZone($zone)))->format(format) on
	// PHP 8.4.25.
	for _, c := range []struct{ when, zone, want string }{
		{"2024-03-01 13:05:09.123456", "Europe/Copenhagen", `01 Fri 1 Friday 5 st 5 60 09 March 03 Mar 3 31 1 2024 24 2024 2024 +2024 pm PM 545 1 13 01 13 05 09 123456 123 Europe/Copenhagen 0 +0100 +01:00 +01:00 CET 3600 2024-03-01T13:05:09+01:00 Fri, 01 Mar 2024 13:05:09 +0100 1709294709 Y\`},
		{"2021-01-03 00:00:00.5", "America/New_York", `03 Sun 3 Sunday 7 rd 0 2 53 January 01 Jan 1 31 0 2020 21 2021 2021 +2021 am AM 250 12 0 12 00 00 00 500000 500 America/New_York 0 -0500 -05:00 -05:00 EST -18000 2021-01-03T00:00:00-05:00 Sun, 03 Jan 2021 00:00:00 -0500 1609650000 Y\`},
		{"2020-12-31 23:59:59", "UTC", `31 Thu 31 Thursday 4 st 4 365 53 December 12 Dec 12 31 1 2020 20 2020 2020 +2020 pm PM 041 11 23 11 23 59 59 000000 000 UTC 0 +0000 +00:00 Z UTC 0 2020-12-31T23:59:59+00:00 Thu, 31 Dec 2020 23:59:59 +0000 1609459199 Y\`},
		{"2024-07-22 06:07:08.000999", "Asia/Kolkata", `22 Mon 22 Monday 1 nd 1 203 30 July 07 Jul 7 31 1 2024 24 2024 2024 +2024 am AM 067 6 6 06 06 07 08 000999 000 Asia/Kolkata 0 +0530 +05:30 +05:30 IST 19800 2024-07-22T06:07:08+05:30 Mon, 22 Jul 2024 06:07:08 +0530 1721608628 Y\`},
		{"2009-12-31 12:00:00", "Australia/Lord_Howe", `31 Thu 31 Thursday 4 st 4 364 53 December 12 Dec 12 31 0 2009 09 2009 2009 +2009 pm PM 083 12 12 12 12 00 00 000000 000 Australia/Lord_Howe 1 +1100 +11:00 +11:00 +11 39600 2009-12-31T12:00:00+11:00 Thu, 31 Dec 2009 12:00:00 +1100 1262221200 Y\`},
		{"0099-02-11 11:00:00", "UTC", `11 Wed 11 Wednesday 3 th 3 41 07 February 02 Feb 2 28 0 99 99 0099 0099 +0099 am AM 500 11 11 11 11 00 00 000000 000 UTC 0 +0000 +00:00 Z UTC 0 0099-02-11T11:00:00+00:00 Wed, 11 Feb 0099 11:00:00 +0000 -59039413200 Y\`},
	} {
		loc, err := time.LoadLocation(c.zone)
		if err != nil {
			t.Skipf("no zone data for %s: %v", c.zone, err)
		}
		at, err := time.ParseInLocation("2006-01-02 15:04:05.999999", c.when, loc)
		if err != nil {
			t.Fatal(err)
		}
		if got := DateFormat(format, at); got != c.want {
			t.Errorf("%s %s:\n got %s\nwant %s", c.when, c.zone, got, c.want)
		}
	}

	// date("Y\\"): the trailing backslash reads the terminating NUL.
	if got := DateFormat(`Y\`, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)); got != "2026\x00" {
		t.Errorf("trailing backslash: %q", got)
	}
}

func TestDate_DefaultTimezone(t *testing.T) {
	t.Cleanup(func() { SetDefaultTimezone(nil) })

	// 2026-01-02 03:04:05 UTC
	const ts = 1767323045

	if got := Date("Y-m-d H:i:s", ts); got != "2026-01-02 03:04:05" {
		t.Fatalf("UTC: got %s", got)
	}

	SetDefaultTimezone(func() *time.Location { return time.FixedZone("x", -5*3600) })

	if got := Date("Y-m-d Hi", ts); got != "2026-01-01 2204" {
		t.Fatalf("-05:00: got %s", got)
	}

	SetDefaultTimezone(func() *time.Location { return nil })

	if got := DefaultTimezone(); got != time.UTC {
		t.Fatalf("nil zone: got %v", got)
	}
}
