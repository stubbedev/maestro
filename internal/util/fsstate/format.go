package fsstate

import "strconv"

// Format is the version of what one of maestro's own caches keeps: of how
// an entry is written and read, and of the code computing what it holds.
// An entry is used only by a maestro of the same Version, whichever build
// wrote it, so a new binary keeps the caches of the one before it unless
// that code changed. internal/cache's TestOwnFormats fails when the code a
// Format covers changes and its Version does not.
type Format struct {
	// Name names the cache in Header.
	Name string
	// Version is bumped with every change to the code the Format covers.
	Version int
}

// String names the format and its version: "maestro-<name>-v<version>".
func (f Format) String() string {
	return "maestro-" + f.Name + "-v" + strconv.Itoa(f.Version)
}

// Header is the first line of what the cache writes: String, then "\n".
func (f Format) Header() string { return f.String() + "\n" }
