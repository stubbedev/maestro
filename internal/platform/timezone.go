// Ports how PHP picks its default time zone (ext/date/php_date.c,
// guess_timezone): the zone date() and `new \DateTime()` use.

package platform

import (
	"slices"
	"time"

	"github.com/stubbedev/maestro/internal/php"
)

// DefaultTimezone is date_default_timezone_get() in the php the snapshot
// describes (Composer's Application pins it for the whole run): the
// date.timezone ini setting when it names a valid zone, else UTC (PHP
// warns "Invalid date.timezone value" and uses UTC). PHP 8 never consults
// the TZ environment variable or the system's zone. Zone names are matched
// without regard to case, as timelib does ("europe/copenhagen" works). A
// nil snapshot (no php) is UTC.
func (s *Snapshot) DefaultTimezone() *time.Location {
	if s == nil {
		return time.UTC
	}

	name, _ := s.IniGet("date.timezone")

	return phpTimezone(name)
}

// phpTimezone resolves a date.timezone value as PHP does, UTC when PHP
// rejects it: the value must name a zone of PHP's bundled database
// (timezoneIdentifiers), ignoring case, whatever the system's zoneinfo
// holds; Go's own database (the system's, or the embedded copy on
// Windows, tzdata_windows.go) then provides the zone's rules.
func phpTimezone(name string) *time.Location {
	canonical, ok := zoneNameFold(name)
	if !ok {
		return time.UTC
	}

	if loc, err := time.LoadLocation(canonical); err == nil {
		return loc
	}

	return time.UTC
}

// zoneNameFold finds the identifier of PHP's database that equals name
// ignoring case (timelib_timezone_id_is_valid compares
// case-insensitively), as the database spells it.
func zoneNameFold(name string) (string, bool) {
	i, found := slices.BinarySearch(timezoneIdentifiers, name)
	if found {
		return timezoneIdentifiers[i], true
	}

	for _, id := range timezoneIdentifiers {
		if php.Strcasecmp(id, name) == 0 {
			return id, true
		}
	}

	return "", false
}
