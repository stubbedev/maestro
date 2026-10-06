package archiver

import (
	"os"
	"testing"
	"time"
	_ "time/tzdata" // the zone below, whatever the machine has installed
)

// oracleZone is the time zone tools/oracle/archiver/archiver.php recorded
// its goldens in. libzip (ZipArchiver) and PharData's zip writer store DOS
// times in local time, as maestro does, so the zip goldens hold
// Europe/Copenhagen wall-clock times and the tests run in that zone.
const oracleZone = "Europe/Copenhagen"

func TestMain(m *testing.M) {
	loc, err := time.LoadLocation(oracleZone)
	if err != nil {
		panic(err)
	}

	time.Local = loc

	os.Exit(m.Run())
}
