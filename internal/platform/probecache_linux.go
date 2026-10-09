// Ports nothing: see probecache.go, whose entries on Linux are keyed on
// what /proc/self/maps tells about the files php mapped.

package platform

import (
	"bytes"

	"golang.org/x/sys/unix"
)

// unameString is php_uname()'s fields, which the snapshot records.
func unameString() string {
	var u unix.Utsname
	if unix.Uname(&u) != nil {
		return ""
	}

	field := func(b []byte) string { return string(bytes.TrimRight(b, "\x00")) }

	return field(u.Sysname[:]) + "\x00" + field(u.Nodename[:]) + "\x00" + field(u.Release[:]) + "\x00" + field(u.Version[:]) + "\x00" + field(u.Machine[:])
}

// probeWrapper reports whether the probe's first mapped file is another
// file than resolved: a wrapper (an executable that starts another php)
// replaced itself with the php it chose, so the entry's files are php's
// own, while what chose them, the wrapper, is not; cacheable is always
// true: the entry takes what the wrapper may read (wrapperEnvNames) and
// the working directory into its key.
func probeWrapper(s *Snapshot, resolved string) (wrapper, cacheable bool) {
	return s.mappedFiles[0] != resolved, true
}

// probeIniDirs: php looks for its ini files nowhere else than here.
func probeIniDirs(dirs []string, _ *Snapshot) []string { return dirs }
