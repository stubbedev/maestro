package platform

// Windows has no zoneinfo directory, and Go reads $GOROOT's zoneinfo.zip
// only where a Go installation is: the embedded database gives
// DefaultTimezone the zones PHP's bundled one has, as on Unix.
import _ "time/tzdata"
