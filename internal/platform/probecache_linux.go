// Ports nothing: see probecache.go, whose entries on Linux are keyed on
// what /proc/self/maps tells about the files php mapped.

package platform

import (
	"strings"
	"time"
)

// maxProbeCacheAge is the usual age: what php mapped (a day of upgrades
// short of one) is the whole truth on Linux.
const maxProbeCacheAge = 24 * time.Hour

// probeMappedFiles is what the probing process mapped (its executable,
// libraries and extensions), as /proc/self/maps told probe.php.
func probeMappedFiles(s *Snapshot, _ string, _ []string) ([]string, bool) {
	// the maps mark a file replaced or removed while php ran
	// ("(deleted)"): what php used is gone, and a signature would
	// describe whatever took its place
	for _, f := range s.mappedFiles {
		if strings.HasSuffix(f, " (deleted)") {
			return nil, false
		}
	}

	return s.mappedFiles, s.hasMappedFiles
}

// probeWrapper reports whether the probe's first mapped file is another
// file than resolved: a wrapper (an executable that starts another php)
// replaced itself with the php it chose, so the entry's files are php's
// own, while what chose them, the wrapper, is not; cacheable is always
// true: the entry takes what the wrapper may read (wrapperEnvNames) and
// the working directory into its key.
func probeWrapper(s *Snapshot, resolved string) (wrapper, cacheable bool) {
	if len(s.mappedFiles) == 0 {
		return false, false
	}

	return s.mappedFiles[0] != resolved, true
}

// probeIniDirs: php looks for its ini files nowhere else than here.
func probeIniDirs(dirs []string, _ *Snapshot) []string { return dirs }
