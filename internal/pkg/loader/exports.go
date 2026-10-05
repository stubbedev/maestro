// Exported faces of loader internals for the packages above it.

package loader

import "time"

// ParseDateTime ports new \DateTime($s, new \DateTimeZone('UTC')) for the
// date formats package metadata uses (see datetime.go); the error is a
// *DateTimeError. internal/repository uses it for security advisories'
// reportedAt.
func ParseDateTime(s string) (time.Time, error) { return parseDateTime(s) }
