package fsstate

import (
	"os"
	"path/filepath"
	"slices"
	"time"
)

// Prune bounds dir, a directory of one cache's entries, each a file: it
// removes the entries last modified more than maxAge ago (0 for no age
// limit), then the least recently modified beyond maxEntries, the files
// WriteAtomic's writers abandoned (older than TempMaxAge) and any
// directory, which an earlier layout left. Other processes may read, write
// and prune at the same time: a removed entry costs its recomputation,
// never a wrong result, and a temporary file is removed only long after
// its writer would have renamed it. Without an age limit, a directory
// within maxEntries is left as it is without looking further.
func Prune(dir string, maxEntries int, maxAge time.Duration) {
	entries, err := os.ReadDir(dir)
	if err != nil || (maxAge == 0 && len(entries) <= maxEntries) {
		return
	}

	type entry struct {
		path  string
		mtime time.Time
	}

	kept := make([]entry, 0, len(entries))
	now := time.Now()

	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		if e.IsDir() {
			_ = os.RemoveAll(path)

			continue
		}

		info, err := e.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}

		age := now.Sub(info.ModTime())

		switch {
		case IsTemp(e.Name()):
			if age > TempMaxAge {
				_ = os.Remove(path)
			}
		case maxAge != 0 && age > maxAge:
			_ = os.Remove(path)
		default:
			kept = append(kept, entry{path, info.ModTime()})
		}
	}

	if len(kept) <= maxEntries {
		return
	}

	slices.SortFunc(kept, func(a, b entry) int { return b.mtime.Compare(a.mtime) })

	for _, e := range kept[maxEntries:] {
		_ = os.Remove(e.path)
	}
}
