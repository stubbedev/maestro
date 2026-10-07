// Ports nothing: where a tool lies in PATH, looked up once per run
// (deliberate deviation 3, speed).

package util

import (
	"os"
	"path/filepath"
	"sync"
)

// PathHit is a file named like the tool in a directory of PATH.
type PathHit struct {
	File       string
	Dir        bool // a directory, which the shell skips
	Regular    bool
	Executable bool
}

// PathSearch is what the absolute directories of a PATH hold under a
// name, in PATH order, up to its first relative directory (which the
// shell resolves against the command's directory, so that nothing after
// it can be decided).
type PathSearch struct {
	Hits     []PathHit
	Relative bool // PATH has a relative directory
}

// Executable is the first hit the shell would run, "" without one.
func (s PathSearch) Executable() string {
	for _, h := range s.Hits {
		if h.Regular && h.Executable {
			return h.File
		}
	}

	return ""
}

type pathSearchKey struct{ path, name string }

// pathSearches memoises SearchPath: the version control tools are looked
// for up to a few times per command each, in every directory of PATH,
// and a run does not install them.
var pathSearches = struct {
	sync.Mutex
	m map[pathSearchKey]PathSearch
}{m: map[pathSearchKey]PathSearch{}}

// SearchPath looks for name in each directory of path (a PATH value), a
// stat per directory, once per run for each path and name.
func SearchPath(path, name string) PathSearch {
	key := pathSearchKey{path, name}

	pathSearches.Lock()
	s, ok := pathSearches.m[key]
	pathSearches.Unlock()

	if ok {
		return s
	}

	for _, dir := range filepath.SplitList(path) {
		if !filepath.IsAbs(dir) {
			s.Relative = true

			break
		}

		file := dir + string(os.PathSeparator) + name
		if fi, err := os.Stat(file); err == nil {
			s.Hits = append(s.Hits, PathHit{File: file, Dir: fi.IsDir(), Regular: fi.Mode().IsRegular(), Executable: isExecutable(file)})
		}
	}

	pathSearches.Lock()
	pathSearches.m[key] = s
	pathSearches.Unlock()

	return s
}
