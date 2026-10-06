package loader_test

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/stubbedev/maestro/internal/pkg/loader"
)

// hexOrString decodes the oracle's string, or {"hex": ...} for bytes that
// are not UTF-8.
func hexOrString(t *testing.T, raw json.RawMessage) string {
	t.Helper()

	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}

	var h struct{ Hex string }
	if err := json.Unmarshal(raw, &h); err != nil {
		t.Fatal(err)
	}

	b, err := hex.DecodeString(h.Hex)
	if err != nil {
		t.Fatal(err)
	}

	return string(b)
}

// unsupportedRelative matches the words of the timelib rules whose
// actions parseDateTime does not port (relative offsets, weekdays,
// "first/last day of", "back/front of").
var unsupportedRelative = regexp.MustCompile(`(?i)sec|min|hour|day|week|fortnight|forthnight|month|year|ms|µs|sun|mon|tue|wed|thu|fri|sat|first|last|next|previous|this|back of|front of`)

func TestOracle_DateTime(t *testing.T) {
	var cases [][2]json.RawMessage
	if err := json.Unmarshal(readFile(t, "oracle/datetime.json"), &cases); err != nil {
		t.Fatal(err)
	}

	unsupported := 0

	for _, c := range cases {
		s := hexOrString(t, c[0])

		var want struct {
			E   json.RawMessage
			R   []json.RawMessage
			Now []json.RawMessage
		}
		if err := json.Unmarshal(c[1], &want); err != nil {
			t.Fatal(err)
		}

		now := time.Now()

		var usBefore, usAfter int64

		if want.Now != nil {
			var nowStr string
			_ = json.Unmarshal(want.Now[0], &nowStr)
			_ = json.Unmarshal(want.Now[1], &usBefore)
			_ = json.Unmarshal(want.Now[2], &usAfter)

			n, err := time.Parse(time.DateTime, nowStr)
			if err != nil {
				t.Fatal(err)
			}

			now = n.Add(time.Duration(usBefore) * time.Microsecond)
		}

		got, err := loader.ParseDateTimeAt(s, now)

		var de *loader.DateTimeError
		if errors.As(err, &de) && de.Unsupported {
			if !unsupportedRelative.MatchString(s) {
				t.Errorf("DateTime(%q): unsupported, but it uses no relative format: %v", s, err)
			}

			unsupported++

			continue
		}

		if want.E != nil {
			message := hexOrString(t, want.E)
			if err == nil {
				t.Errorf("DateTime(%q) = %s, want %q", s, got.Format(time.RFC3339Nano), message)
			} else if err.Error() != message {
				t.Errorf("DateTime(%q):\n got %q\nwant %q", s, err, message)
			}

			continue
		}

		if err != nil {
			t.Errorf("DateTime(%q): %v, want %s", s, err, c[1])

			continue
		}

		var (
			formatted string
			unix, us  int64
		)

		_ = json.Unmarshal(want.R[0], &formatted)
		_ = json.Unmarshal(want.R[1], &unix)
		_ = json.Unmarshal(want.R[2], &us)

		gotUS := int64(got.Nanosecond() / 1000)
		// microseconds read from the clock lie between the reads around
		// PHP's call; ParseDateTimeAt got the first one
		usOK := gotUS == us || (gotUS == usBefore && us >= usBefore && us <= usAfter)

		if f := got.Format("2006-01-02T15:04:05-07:00"); f != formatted || got.Unix() != unix || !usOK {
			t.Errorf("DateTime(%q) = %s %d %d, want %s %d %d", s, f, got.Unix(), gotUS, formatted, unix, us)
		}
	}

	t.Logf("%d of %d cases use an unsupported relative format", unsupported, len(cases))
}

// TestParseDateTime_RelativeForms pins the forms new \DateTime() reads
// relative to now that package metadata hits (outputs from PHP 8.4.25).
func TestParseDateTime_RelativeForms(t *testing.T) {
	now := time.Date(2026, 10, 6, 9, 26, 17, 137756000, time.UTC)

	for _, c := range []struct{ in, want string }{
		{"", "2026-10-06T09:26:17.137756+00:00"},
		{" \t ", "2026-10-06T09:26:17.137756+00:00"},
		{"a", "2026-10-06T09:26:17.137756+01:00"},
		{"Z", "2026-10-06T09:26:17.137756+00:00"},
		{"m", "2026-10-06T09:26:17.137756+12:00"},
		{"n", "2026-10-06T09:26:17.137756-01:00"},
		{"y", "2026-10-06T09:26:17.137756-12:00"},
		{"1234", "2026-10-06T12:34:00.000000+00:00"},
		{"2400", "2026-10-07T00:00:00.000000+00:00"},
		{"2460", "2460-10-06T09:26:17.000000+00:00"},
		{"123456", "2026-10-06T12:34:56.000000+00:00"},
		{"1234567890", "7890-10-06T12:34:56.000000+00:00"},
		{"00000000", "-0001-11-30T00:00:00.000000+00:00"},
		{"x2012", "2026-10-06T20:12:00.000000-11:00"},
		{"2012-01-01T", "2012-01-01T00:00:00.000000-07:00"},
		{"tomorrow", "2026-10-07T00:00:00.000000+00:00"},
	} {
		got, err := loader.ParseDateTimeAt(c.in, now)
		if err != nil {
			t.Errorf("DateTime(%q): %v", c.in, err)
		} else if f := got.Format("2006-01-02T15:04:05.000000-07:00"); f != c.want {
			t.Errorf("DateTime(%q) = %s, want %s", c.in, f, c.want)
		}
	}

	for _, c := range []struct{ in, want string }{
		{"j", "Failed to parse time string (j) at position 0 (j): The timezone could not be found in the database"},
		{"12", "Failed to parse time string (12) at position 0 (1): Unexpected character"},
		{"12345", "Failed to parse time string (12345) at position 4 (5): Unexpected character"},
		{"20121301", "Failed to parse time string (20121301) at position 7 (1): Unexpected character"},
		{" 12", "Failed to parse time string ( 12) at position 0 (1): Unexpected character"},
	} {
		if _, err := loader.ParseDateTimeAt(c.in, now); err == nil || err.Error() != c.want {
			t.Errorf("DateTime(%q): %v, want %s", c.in, err, c.want)
		}
	}
}

// BenchmarkParseDateTime parses release dates as package metadata carries
// them.
func BenchmarkParseDateTime(b *testing.B) {
	for _, s := range []string{"2023-06-01T12:34:56+00:00", "2012-01-01 10:00:00", "@1700000000"} {
		b.Run(s, func(b *testing.B) {
			for b.Loop() {
				if _, err := loader.ParseDateTime(s); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
