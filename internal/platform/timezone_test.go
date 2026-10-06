package platform

import (
	"testing"
	"time"
)

// TestPhpTimezone checks date.timezone values against what PHP 8.4 makes
// of them (php -d date.timezone=... -r 'echo date("Y-m-d H:i:s", 0);').
func TestPhpTimezone(t *testing.T) {
	if _, err := time.LoadLocation("Europe/Copenhagen"); err != nil {
		t.Skip("no time zone database:", err)
	}

	for _, tc := range []struct{ ini, want string }{
		{"", "1970-01-01 00:00:00"},
		{"UTC", "1970-01-01 00:00:00"},
		{"utc", "1970-01-01 00:00:00"},
		{"Europe/Copenhagen", "1970-01-01 01:00:00"},
		{"europe/copenhagen", "1970-01-01 01:00:00"},
		{"CET", "1970-01-01 01:00:00"},
		{"EST5EDT", "1969-12-31 19:00:00"},
		{"Local", "1970-01-01 00:00:00"},          // "Invalid date.timezone value 'Local', using 'UTC' instead"
		{"Bogus/Zone", "1970-01-01 00:00:00"},     // likewise
		{"/etc/localtime", "1970-01-01 00:00:00"}, // likewise
	} {
		if got := time.Unix(0, 0).In(phpTimezone(tc.ini)).Format(time.DateTime); got != tc.want {
			t.Errorf("date.timezone=%q: got %s, want %s", tc.ini, got, tc.want)
		}
	}

	var nilSnap *Snapshot
	if nilSnap.DefaultTimezone() != time.UTC {
		t.Error("no php must be UTC")
	}
}
