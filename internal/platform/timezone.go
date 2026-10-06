// Ports how PHP picks its default time zone (ext/date/php_date.c,
// guess_timezone): the zone date() and `new \DateTime()` use.

package platform

import (
	"os"
	"path/filepath"
	"strings"
	"time"
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
// rejects it.
func phpTimezone(name string) *time.Location {
	// "" is unset; Go's own names ("Local") and absolute paths are not
	// timelib identifiers
	if name == "" || name == "Local" || strings.HasPrefix(name, "/") || strings.Contains(name, "..") {
		return time.UTC
	}

	if loc, err := time.LoadLocation(name); err == nil {
		return loc
	}

	if canonical, ok := zoneNameFold(name); ok {
		if loc, err := time.LoadLocation(canonical); err == nil {
			return loc
		}
	}

	return time.UTC
}

// zoneSources are the zoneinfo directories Go's time package searches
// (time/zoneinfo_unix.go), $ZONEINFO first.
func zoneSources() []string {
	dirs := []string{"/usr/share/zoneinfo/", "/usr/share/lib/zoneinfo/", "/usr/lib/locale/TZ/", "/etc/zoneinfo/"}
	if z := os.Getenv("ZONEINFO"); z != "" {
		dirs = append([]string{z}, dirs...)
	}

	return dirs
}

// zoneNameFold finds the zone file whose name equals name ignoring case
// (timelib_timezone_id_is_valid compares case-insensitively), returning
// its name as the zoneinfo directory spells it.
func zoneNameFold(name string) (string, bool) {
	if strings.EqualFold(name, "UTC") {
		return "UTC", true
	}

	segments := strings.Split(name, "/")

	for _, dir := range zoneSources() {
		if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
			continue
		}

		resolved := make([]string, 0, len(segments))
		cur := dir

		for _, seg := range segments {
			entries, err := os.ReadDir(cur)
			if err != nil {
				break
			}

			match := ""

			for _, e := range entries {
				if strings.EqualFold(e.Name(), seg) {
					match = e.Name()

					break
				}
			}

			if match == "" {
				break
			}

			resolved = append(resolved, match)
			cur = filepath.Join(cur, match)
		}

		if len(resolved) == len(segments) {
			if fi, err := os.Stat(cur); err == nil && fi.Mode().IsRegular() {
				return strings.Join(resolved, "/"), true
			}
		}
	}

	return "", false
}
