// Ports nothing: the contents of cached files read before, kept for the
// process's later reads of the same files (deliberate deviation 3, speed).

package cache

import (
	"os"
	"sync"

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
			if s, ok := fsstate.ReadStable(path); ok {
				contents := string(s.Content())
				readMemo.Lock()
				readMemo.files[path] = memoFile{stamp: s.Stamp(), contents: contents}
				readMemo.Unlock()

				return contents, nil
			}
		}
	}
	data, err := os.ReadFile(path)

	return string(data), err
}

// forget drops what readFile remembers of path, which this process
// replaces or removes.
func forget(path string) {
	readMemo.Lock()
	delete(readMemo.files, path)
	readMemo.Unlock()
}
