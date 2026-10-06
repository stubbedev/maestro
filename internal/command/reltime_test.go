package command

import (
	"testing"
	"time"

	"github.com/stubbedev/maestro/internal/pkg/loader"
	"github.com/stubbedev/maestro/internal/testutil"
)

// TestRelativeTime_Oracle compares getRelativeTime and the DateTime::diff
// fields it reads with PHP's (tools/oracle/command/reltime.php): release
// dates with offsets and without, "now" in several default time zones,
// around month ends, DST changes and the boundaries of the wording.
func TestRelativeTime_Oracle(t *testing.T) {
	var golden struct {
		Cases []struct {
			Release string `json:"release"`
			Now     int64  `json:"now"`
			Us      int64  `json:"us"`
			TZ      string `json:"tz"`
			Y, M    int
			Days    int    `json:"days"`
			Want    string `json:"want"`
		} `json:"cases"`
	}

	testutil.LoadJSONGolden(t, "testdata/reltime.json", &golden)

	if len(golden.Cases) < 1000 {
		t.Fatalf("only %d cases in the golden", len(golden.Cases))
	}

	for _, c := range golden.Cases {
		release, err := loader.ParseDateTime(c.Release)
		if err != nil {
			t.Fatal(err)
		}

		loc, err := time.LoadLocation(c.TZ)
		if err != nil {
			t.Fatal(err)
		}

		now := time.Unix(c.Now, c.Us*1000).In(loc)

		y, m, days := dateDiff(release, now)
		got := relativeTime(release, now)

		if y != c.Y || m != c.M || days != c.Days || got != c.Want {
			t.Errorf("%s at %s: got %q (y %d m %d days %d), PHP %q (y %d m %d days %d)",
				c.Release, now.Format(time.RFC3339Nano), got, y, m, days, c.Want, c.Y, c.M, c.Days)
		}
	}
}
