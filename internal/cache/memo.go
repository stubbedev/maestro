// Ports nothing: the contents of cached files read before, kept for the
// process's later reads of the same files (deliberate deviation 3, speed).

package cache

import (
	"io/fs"
	"os"
	"sync"
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
	id       fileIdentity
	contents string
}

// readFile is os.ReadFile(path) as a string. When path's identity is
// known (Stat: device, inode, size, modification and change times; not
// on Windows), the contents read last time are returned while it stays
// the same.
func readFile(path string) (string, error) {
	fi, err := os.Stat(path)
	if err != nil {
		data, err := os.ReadFile(path)

		return string(data), err
	}
	id, ok := identityOf(fi)
	if !ok {
		data, err := os.ReadFile(path)

		return string(data), err
	}

	readMemo.Lock()
	memo, found := readMemo.files[path]
	readMemo.Unlock()
	if found && memo.id == id {
		return memo.contents, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	contents := string(data)
	// the file may have been replaced after Stat: then its identity
	// differs from id, and the next read reads it again
	readMemo.Lock()
	readMemo.files[path] = memoFile{id: id, contents: contents}
	readMemo.Unlock()

	return contents, nil
}

// forget drops what readFile remembers of path, which this process
// replaces or removes.
func forget(path string) {
	readMemo.Lock()
	delete(readMemo.files, path)
	readMemo.Unlock()
}

// identityOf is fi's identity for readFile; false when there is none to
// tell a changed file by.
func identityOf(fi fs.FileInfo) (fileIdentity, bool) {
	if !fi.Mode().IsRegular() {
		return fileIdentity{}, false
	}

	return sysIdentity(fi)
}
