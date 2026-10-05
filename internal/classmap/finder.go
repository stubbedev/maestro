// Ports the parts of symfony/finder 5.4 (Finder, Iterator\RecursiveDirectoryIterator,
// Iterator\ExcludeDirectoryFilterIterator, Iterator\FileTypeFilterIterator,
// Iterator\PathFilterIterator) that ClassMapGenerator::scanPaths() uses:
//
//	Finder::create()->files()->followLinks()->name($extensionsRegex)->in($path)->exclude($excludedDirs)
//
// with the Finder defaults of ignoring dot files and VCS directories. The
// name filter is implied by scanPaths()' own extension check and left to it.

package classmap

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// vcsPatterns are Finder::$vcsPatterns, excluded by default.
var vcsPatterns = [...]string{".svn", "_svn", "CVS", "_darcs", ".arch-params", ".monotone", ".bzr", ".git", ".hg"}

// finderIn is Finder::in($dir): the directories to search, a directory or
// the sorted directories a glob matches.
func finderIn(dir string) ([]string, error) {
	if isDir(dir) {
		return []string{finderNormalizeDir(dir)}, nil
	}
	dirs := phpGlobDirs(dir)
	if len(dirs) == 0 {
		return nil, newException(classDirectoryNotFound, `The "`+dir+`" directory does not exist.`)
	}
	for i, d := range dirs {
		dirs[i] = finderNormalizeDir(d)
	}

	return dirs, nil
}

// finderNormalizeDir is Finder::normalizeDir(): it removes trailing slashes,
// except from "/" and (s)ftp:// URLs.
func finderNormalizeDir(dir string) string {
	if dir == "/" {
		return dir
	}
	dir = strings.TrimRight(dir, "/")
	rest := strings.TrimPrefix(dir, "ssh2.")
	rest = strings.TrimPrefix(rest, "s")
	if strings.HasPrefix(rest, "ftp://") {
		dir += "/"
	}

	return dir
}

// finderExclusions is the configuration of ExcludeDirectoryFilterIterator:
// directory names excluded anywhere, and patterns containing a slash matched
// against relative paths.
type finderExclusions struct {
	names    map[string]bool
	patterns []string
}

func newFinderExclusions(excludedDirs []string) finderExclusions {
	var e finderExclusions
	add := func(dir string) {
		dir = strings.TrimRight(dir, "/")
		if strings.Contains(dir, "/") {
			e.patterns = append(e.patterns, dir)

			return
		}
		if e.names == nil {
			e.names = make(map[string]bool, len(vcsPatterns)+len(excludedDirs))
		}
		e.names[dir] = true
	}
	for _, d := range excludedDirs {
		add(d)
	}
	for _, d := range vcsPatterns {
		add(d)
	}

	return e
}

// matchesPattern matches '#(?:^|/)(?:pattern|...)(?:/|$)#' against path.
func (e *finderExclusions) matchesPattern(path string) bool {
	for _, p := range e.patterns {
		for i := 0; ; {
			k := strings.Index(path[i:], p)
			if k < 0 {
				break
			}
			k += i
			end := k + len(p)
			if (k == 0 || path[k-1] == '/') &&
				(end == len(path) || path[end] == '/' || end == len(path)-1 && path[end] == '\n') {
				return true
			}
			i = k + 1
		}
	}

	return false
}

// foundFile is a file the Finder yielded: its pathname, and whether the
// directory entry is known not to be a symlink.
type foundFile struct {
	path    string
	notLink bool
}

// finderFiles iterates the Finder over dirs and returns the files it
// yields, in order, and the exception that stopped the iteration, if any.
func finderFiles(dirs, excludedDirs []string) ([]foundFile, error) {
	w := finderWalk{excl: newFinderExclusions(excludedDirs)}
	for _, dir := range dirs {
		if dir == "" {
			return w.files, newException("ValueError", "RecursiveDirectoryIterator::__construct(): Argument #1 ($directory) cannot be empty")
		}
		w.base = dir
		if dir != "/" && !strings.HasSuffix(dir, "/") {
			w.base += "/"
		}
		if err := w.walk(dir, w.base, classUnexpectedValue); err != nil {
			return w.files, err
		}
	}

	return w.files, nil
}

type finderWalk struct {
	excl  finderExclusions
	base  string // root path with a trailing separator
	files []foundFile
}

// walk is the SELF_FIRST RecursiveIteratorIterator over the directory dir.
// prefix is dir's pathname followed by a separator, which starts with base.
// errClass is the class of the exception thrown when the directory cannot
// be opened (an AccessDeniedException below the root).
func (w *finderWalk) walk(dir, prefix, errClass string) error {
	f, err := os.Open(dir)
	if err != nil {
		return newException(errClass, "RecursiveDirectoryIterator::__construct("+dir+"): Failed to open directory: "+strerror(err))
	}
	// Directory order, as readdir() returns it. A read error ends the
	// iteration like the end of the directory.
	entries, _ := f.ReadDir(-1)
	_ = f.Close()

	for _, entry := range entries {
		name := entry.Name()
		pathname := prefix + name
		subPathname := pathname[len(w.base):]
		subPath := prefix[len(w.base):max(len(w.base), len(prefix)-1)]

		// SplFileInfo::isDir() follows symlinks.
		isLink := entry.Type()&os.ModeSymlink != 0
		isDir := entry.IsDir()
		if isLink {
			isDir = isDirectory(pathname)
		}

		// ExcludeDirectoryFilterIterator::accept()
		if isDir && w.excl.names[name] {
			continue
		}
		if len(w.excl.patterns) > 0 {
			rel := subPath
			if isDir {
				rel = subPathname
			}
			if w.excl.matchesPattern(rel) {
				continue
			}
		}

		if !isDir {
			// FileTypeFilterIterator (files only), then PathFilterIterator
			// with the dot files pattern.
			if !isDotPath(subPathname) {
				w.files = append(w.files, foundFile{path: pathname, notLink: !isLink})
			}

			continue
		}

		// hasChildren() is isDir() with FOLLOW_SYMLINKS.
		if err := w.walk(pathname, pathname+"/", classAccessDenied); err != nil {
			return err
		}
	}

	return nil
}

// isDotPath matches '#(^|/)\..+(/|$)#', Finder's IGNORE_DOT_FILES pattern:
// some path segment starts with a dot and has more after it. As in PCRE,
// . does not match a newline and $ also matches before a final newline.
func isDotPath(path string) bool {
	for p := range len(path) {
		if path[p] != '.' || p > 0 && path[p-1] != '/' {
			continue
		}
		// .+ runs to the next newline; it must take at least one byte and
		// then be followed by a slash or the end.
		r := p + 1
		for r < len(path) && path[r] != '\n' {
			r++
		}
		if r == p+1 {
			continue
		}
		if r == len(path) || r == len(path)-1 || strings.IndexByte(path[p+2:r], '/') >= 0 {
			return true
		}
	}

	return false
}

// isFile is PHP's is_file() (following symlinks).
func isFile(path string) bool {
	info, err := os.Stat(path)

	return err == nil && info.Mode().IsRegular()
}

// isDir is PHP's is_dir() (following symlinks).
func isDir(path string) bool {
	return isDirectory(path)
}

func isDirectory(path string) bool {
	info, err := os.Stat(path)

	return err == nil && info.IsDir()
}

// realpath is PHP's realpath(): the absolute path with symlinks resolved,
// false (ok == false) when it does not exist.
func realpath(path string) (string, bool) {
	if path == "" {
		path = "."
	}
	if !filepath.IsAbs(path) {
		cwd, err := getCwd()
		if err != nil {
			return "", false
		}
		path = cwd + "/" + path
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", false
	}

	return real, true
}

// realDirCache memoizes the realpath of directories during a scan, so that
// resolving a file costs at most one lstat() (none when the Finder saw it
// is not a symlink) and each directory one more.
type realDirCache struct {
	m sync.Map // directory path -> realpath, "" if it does not resolve
}

// realpath is realpath(path) for an absolute path. notLink tells that path
// is known not to be a symlink.
func (c *realDirCache) realpath(path string, notLink bool) (string, bool) {
	if !notLink {
		info, err := os.Lstat(path)
		if err != nil {
			return "", false
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return realpath(path)
		}
	}
	real := c.resolve(path, true)

	return real, real != ""
}

// resolve returns the realpath of path, "" if it does not resolve. Unless
// known, whether path itself is a symlink is checked with lstat().
func (c *realDirCache) resolve(path string, notLink bool) string {
	if path == "" || path == "/" {
		return "/"
	}
	if r, ok := c.m.Load(path); ok {
		real, _ := r.(string)

		return real
	}
	slash := strings.LastIndexByte(path, '/')
	name := path[slash+1:]
	if slash < 0 || name == "" || name == "." || name == ".." {
		// These also require the parent to be a directory.
		r, _ := realpath(path)

		return r
	}
	parent := c.resolve(path[:slash], false)
	var real string
	if parent != "" {
		if parent != "/" {
			parent += "/"
		}
		real = parent + name
		if !notLink {
			info, err := os.Lstat(real)
			switch {
			case err != nil:
				real = ""
			case info.Mode()&os.ModeSymlink != 0:
				real, _ = realpath(real)
			}
		}
	}
	if !notLink {
		c.m.Store(path, real)
	}

	return real
}
