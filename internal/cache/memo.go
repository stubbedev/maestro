// Ports nothing: the contents of cached files read before, kept for the
// process's later reads of the same files (deliberate deviation 3, speed).

package cache

import (
	"os"
	"sync"
	"time"

	"github.com/stubbedev/maestro/internal/util/fsstate"
)

// readMemo holds the contents of the cache files this process read, by
// path, with the identity of the file they were read from. A run reads
// most metadata files several times (the look-ahead, the loads, the
// security advisory and filter list lookups of the pool and of the audit),
// each time all of it; a read of a file whose identity has not changed
// since returns the contents read before instead.
var readMemo = struct {
	sync.Mutex
	files map[string]memoFile
}{files: map[string]memoFile{}}

type memoFile struct {
	stamp    fsstate.Stamp
	seen     time.Time
	contents string
}

// readFile is os.ReadFile(path) as a string. Where files have identities
// (fsstate.Known: device, inode, mode, size, modification and change
// times; not on Windows), the contents read last time are returned while
// the file's stays the same. Contents are only remembered when the file
// did not change while they were read.
func readFile(path string) (string, error) {
	if fsstate.Known() {
		if stamp, ok := fsstate.StatStamp(path); ok && stamp.IsRegular() {
			readMemo.Lock()
			memo, found := readMemo.files[path]
			readMemo.Unlock()
			if found && memo.stamp.Same(stamp) {
				return memo.contents, nil
			}
			if contents, stamp, seen, ok := fsstate.ReadStableString(path); ok {
				readMemo.Lock()
				readMemo.files[path] = memoFile{stamp: stamp, seen: seen, contents: contents}
				readMemo.Unlock()

				return contents, nil
			}
		}
	}
	data, err := os.ReadFile(path)

	return string(data), err
}

// Origin is the file a document was read from, as the read saw it: its
// identity, and a moment before the read looked at it. The zero Origin is
// none known.
type Origin struct {
	id   fsstate.ID
	seen time.Time
	ok   bool
}

// originOf is the origin of contents when readFile read them from path
// last, else the zero Origin.
func originOf(path, contents string) Origin {
	readMemo.Lock()
	memo, found := readMemo.files[path]
	readMemo.Unlock()
	// the strings are usually the same one, which compares at once
	if !found || memo.contents != contents {
		return Origin{}
	}
	id, ok := memo.stamp.ID()

	return Origin{id: id, seen: memo.seen, ok: ok}
}

// forget drops what readFile remembers of path, which this process
// replaces or removes.
func forget(path string) {
	readMemo.Lock()
	delete(readMemo.files, path)
	readMemo.Unlock()
}
