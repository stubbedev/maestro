// Ports src/ClassMapGenerator.php.

// Package classmap is a port of composer/class-map-generator 1.7.3: it scans
// PHP files for the classes, interfaces, traits and enums they declare and
// builds the class map Composer's autoloader dumps.
//
// Files are found the way the PHP code finds them through Symfony Finder
// (directory order, dot files and VCS directories skipped, symlinks
// followed) and parsed in parallel, while the class map, its ambiguous
// classes and PSR violations are built in the exact order PHP visits the
// files, so which file wins for a duplicate class is the same.
//
// What is found in a file depends on the PHP running Composer: its
// short_open_tag, and its version, whose scanner php_strip_whitespace()
// uses (PHP 7.2 to 8.5 are reproduced; see phpversion.go) and which decides
// whether enums are looked for (PHP >= 8.1). Parser carries both.
package classmap

import (
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/stubbedev/maestro/internal/php"
)

// AutoloadType is the autoload standard whose mapping rules a scan applies.
type AutoloadType uint8

// The autoload types of scanPaths().
const (
	Classmap AutoloadType = iota // 'classmap': every class found is kept
	PSR0                         // 'psr-0'
	PSR4                         // 'psr-4'
)

func (t AutoloadType) String() string {
	switch t {
	case PSR0:
		return "psr-0"
	case PSR4:
		return "psr-4"
	}

	return "classmap"
}

// DefaultExtensions are the file extensions scanned by default.
var DefaultExtensions = []string{"php", "inc"}

// Generator is ClassMapGenerator.
type Generator struct {
	// Parser decides how files are tokenized (PHP's short_open_tag and
	// version).
	Parser Parser

	extensions   []string
	scannedFiles *FileList
	classMap     ClassMap
	// pre holds what Prefetch found and parsed ahead of the scans (nil:
	// nothing).
	pre *prefetched
	// cache keeps parse results across scans and generators (nil: none).
	cache *ParseCache
}

// NewGenerator returns a generator scanning files with the given extensions
// (DefaultExtensions when nil).
func NewGenerator(extensions []string) *Generator {
	if extensions == nil {
		extensions = DefaultExtensions
	}

	return &Generator{Parser: DefaultParser, extensions: extensions}
}

// SetParseCache makes the generator take parse results from the cache and
// keep its own there.
func (g *Generator) SetParseCache(cache *ParseCache) *Generator {
	g.cache = cache

	return g
}

// AvoidDuplicateScans makes sure that, when ScanPaths is called repeatedly
// with paths that may overlap, the same file is never scanned twice. A nil
// list starts a new one.
func (g *Generator) AvoidDuplicateScans(scannedFiles *FileList) *Generator {
	if scannedFiles == nil {
		scannedFiles = &FileList{}
	}
	g.scannedFiles = scannedFiles

	return g
}

// ClassMap returns the class map built so far.
func (g *Generator) ClassMap() *ClassMap { return &g.classMap }

// CreateMap scans path (a file, a directory or a glob) for classes and
// returns the resulting class map (ClassMapGenerator::createMap()).
func CreateMap(path string) (*ClassMap, error) {
	g := NewGenerator(nil)
	if err := g.ScanPaths(path, nil, Classmap, "", nil); err != nil {
		return nil, err
	}

	return g.ClassMap(), nil
}

// ScanPaths iterates over all files in path, a file, a directory or a glob
// of directories, searching for classes (scanPaths() with a string path).
//
// excluded matches the paths (realpath and as found) of files to leave out
// of the class map, nil for none. autoloadType PSR0 or PSR4 keeps only the
// classes the namespace autoloaders could load from their file, with
// namespace as the prefix of the rule ("" to not filter by prefix), and
// records the others as PSR violations. excludedDirs are directories to
// skip, relative to path.
//
// On error, the classes of the files visited before the failing one have
// been added, as in PHP.
func (g *Generator) ScanPaths(path string, excluded Matcher, autoloadType AutoloadType, namespace string, excludedDirs []string) error {
	var dirs []string
	switch {
	case isFile(path):
	case isDir(path) || strings.Contains(path, "*"):
		var err error
		if dirs, err = finderIn(path); err != nil {
			return err
		}
	default:
		return newException(classRuntime, `Could not scan for classes inside "`+path+`" which does not appear to be a file nor a folder`)
	}

	cwd, err := g.realCwd()
	if err != nil {
		return err
	}

	// The Finder is iterated after getcwd(); its errors surface after the
	// files it yielded before them have been processed.
	files, walkErr := []foundFile{{path: path}}, error(nil)
	if dirs != nil {
		if w, ok := g.pre.walk(path, excludedDirs); ok {
			files, walkErr = w.files, w.err
		} else {
			files, walkErr = finderFiles(dirs, excludedDirs)
		}
	}

	return g.scan(files, walkErr, cwd, excluded, autoloadType, namespace, path)
}

// ScanFiles scans the given files for classes, keeping them all
// (scanPaths() with an array of SplFileInfo, whose pathnames these are).
func (g *Generator) ScanFiles(files []string, excluded Matcher) error {
	cwd, err := realCwd()
	if err != nil {
		return err
	}
	found := make([]foundFile, len(files))
	for i, f := range files {
		found[i].path = f
	}

	return g.scan(found, nil, cwd, excluded, Classmap, "", "")
}

// scanItem is one file of a scan.
type scanItem struct {
	filePath   string
	notLink    bool // filePath is known not to be a symlink
	realPath   string
	realErr    error // realpath() failure
	excludeErr error // the exclusion regex failed
	excluded   bool
	classes    []string
	parseErr   error // findClasses() failure
}

// scan is the foreach of scanPaths() over the pathnames of files, followed
// by walkErr (the exception the file iterator threw after them, if any).
// The files are resolved and parsed in parallel; the class map is then
// built sequentially in file order.
func (g *Generator) scan(files []foundFile, walkErr error, cwd string, excluded Matcher, typ AutoloadType, namespace, basePath string) error {
	items := g.scanItems(files, cwd)

	g.prepare(items, excluded)

	for i := range items {
		it := &items[i]
		if it.realErr != nil {
			return it.realErr
		}
		// if a list of scanned files is given, avoid scanning twice the same
		// file to save cycles and avoid generating warnings in case a
		// PSR-0/4 declaration follows another more specific one, or a
		// classmap declaration, which covered this file already
		if g.scannedFiles != nil && g.scannedFiles.Contains(it.realPath) {
			continue
		}
		if it.excludeErr != nil {
			return it.excludeErr
		}
		if it.excluded {
			continue
		}
		if it.parseErr != nil {
			return it.parseErr
		}

		classes := it.classes
		if typ != Classmap {
			var err error
			classes, err = g.filterByNamespace(classes, it.filePath, namespace, typ, basePath)
			if err != nil {
				return err
			}
			// if no valid class was found in the file then we do not mark it
			// as scanned as it might still be matched by another rule later
			if len(classes) > 0 && g.scannedFiles != nil {
				g.scannedFiles.Add(it.realPath)
			}
		} else if g.scannedFiles != nil {
			// classmap autoload rules always collect all classes so for these
			// we definitely do not want to scan again
			g.scannedFiles.Add(it.realPath)
		}

		for _, class := range classes {
			if !g.classMap.HasClass(class) {
				g.classMap.AddClass(class, it.filePath)
			} else if it.filePath != g.classMap.path(class) {
				g.classMap.AddAmbiguousClass(class, it.filePath)
			}
		}
	}

	return walkErr
}

// prepare resolves the realpath of every item, applies the exclusion regex
// and parses the files the sequential pass will need, in parallel. Files
// already in the scanned list are not parsed: the list only grows, so the
// sequential pass skips them too.
// scanItems are the items of the files that have a scanned extension, with
// their paths as scanPaths() builds them.
func (g *Generator) scanItems(files []foundFile, cwd string) []scanItem {
	items := make([]scanItem, 0, len(files))
	for _, f := range files {
		if !g.hasExtension(f.path) {
			continue
		}
		it := scanItem{}
		if !isAbsolutePath(f.path) && !isStreamWrapperPath(f.path) {
			joined := cwd + "/" + f.path
			it.filePath = normalizePath(joined)
			// The Finder's knowledge applies if the path still names the
			// directory entry it read.
			it.notLink = f.notLink && cwd != "" && it.filePath == joined
		} else {
			it.filePath = collapseSeparators(f.path)
			it.notLink = f.notLink && it.filePath == f.path
		}
		items = append(items, it)
	}

	return items
}

func (g *Generator) prepare(items []scanItem, excluded Matcher) {
	dirs := &realDirCache{}
	if g.pre != nil {
		dirs = &g.pre.dirs
	}
	work := func(b *parseBuffers, it *scanItem) {
		if isStreamWrapperPath(it.filePath) {
			it.realPath = it.filePath
		} else if real, ok := dirs.realpath(it.filePath, it.notLink); ok {
			it.realPath = real
		} else {
			// fallback just in case but this really should not happen
			it.realErr = newException(classRuntime, "realpath of "+it.filePath+" failed to resolve, got false")

			return
		}
		if g.scannedFiles != nil && g.scannedFiles.Contains(it.realPath) {
			return
		}
		// check the realpath of the file against the excluded paths as the
		// path might be a symlink and the excluded path is realpath'd so
		// symlinks are resolved; then the non-realpath of the file for
		// directory symlinks in the project dir
		if excluded != nil {
			for _, p := range [2]string{it.realPath, it.filePath} {
				matched, err := excluded.IsMatch(strings.ReplaceAll(p, `\`, "/"))
				if err != nil {
					it.excludeErr = err

					return
				}
				if matched {
					it.excluded = true

					return
				}
			}
		}
		if r, ok := g.pre.parsed(it.filePath); ok {
			it.classes, it.parseErr = r.classes, r.err

			return
		}
		it.classes, it.parseErr = g.Parser.cachedFindClasses(b, it.filePath, g.cache)
	}

	workers := min(runtime.GOMAXPROCS(0), len(items))
	if workers <= 1 {
		b := getBuffers()
		for i := range items {
			work(b, &items[i])
		}
		bufferPool.Put(b)

		return
	}
	var next atomic.Int64
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			b := getBuffers()
			defer bufferPool.Put(b)
			for {
				i := int(next.Add(1) - 1)
				if i >= len(items) {
					return
				}
				work(b, &items[i])
			}
		})
	}
	wg.Wait()
}

// bufferPool keeps parse buffers across scans: Composer scans each autoload
// directory separately.
var bufferPool = sync.Pool{New: func() any { return new(parseBuffers) }}

func getBuffers() *parseBuffers {
	if b, ok := bufferPool.Get().(*parseBuffers); ok {
		return b
	}

	return new(parseBuffers)
}

// hasExtension is in_array(pathinfo($filePath, PATHINFO_EXTENSION),
// $this->extensions, true). It also covers the Finder's name filter
// '/\.(?:ext|...)$/', which every file with a listed extension passes.
func (g *Generator) hasExtension(path string) bool {
	ext := ""
	base := php.Basename(path, "")
	if dot := strings.LastIndexByte(base, '.'); dot >= 0 {
		ext = base[dot+1:]
	}
	return slices.Contains(g.extensions, ext)
}

// filterByNamespace removes the classes which could not have been loaded by
// the namespace autoloaders. baseNamespace is the prefix of the autoload
// mapping, basePath its root directory.
func (g *Generator) filterByNamespace(classes []string, filePath, baseNamespace string, typ AutoloadType, basePath string) ([]string, error) {
	var validClasses, rejectedClasses []string

	realSubPath := php.Substr(filePath, len(basePath)+1)
	if dot := strings.LastIndexByte(realSubPath, '.'); dot >= 0 {
		realSubPath = realSubPath[:dot]
	}

	for _, class := range classes {
		// transform class name to file path and validate
		var subPath string
		if typ == PSR0 {
			if baseNamespace != "" && !strings.HasPrefix(class, baseNamespace) {
				rejectedClasses = append(rejectedClasses, class)

				continue
			}
			// DIRECTORY_SEPARATOR, which the Finder's paths use too.
			if nsLen := strings.LastIndexByte(class, '\\'); nsLen >= 0 {
				subPath = strings.ReplaceAll(class[:nsLen+1], `\`, finderSep) + strings.ReplaceAll(class[nsLen+1:], "_", finderSep)
			} else {
				subPath = strings.ReplaceAll(class, "_", finderSep)
			}
		} else {
			subNamespace := class
			if baseNamespace != "" {
				subNamespace = php.Substr(class, len(baseNamespace))
			}
			subPath = strings.ReplaceAll(subNamespace, `\`, finderSep)
		}
		if subPath == realSubPath {
			validClasses = append(validClasses, class)
		} else {
			rejectedClasses = append(rejectedClasses, class)
		}
	}

	// warn only if no valid classes, else silently skip invalid
	if len(validClasses) > 0 {
		return validClasses, nil
	}
	cwd, err := g.violationCwd()
	if err != nil {
		return nil, err
	}
	shortPath := replaceCwd(normalizePath(filePath), cwd)
	shortBasePath := replaceCwd(normalizePath(basePath), cwd)
	for _, class := range rejectedClasses {
		g.classMap.AddPsrViolation("Class "+class+" located in "+shortPath+" does not comply with "+typ.String()+
			" autoloading standard (rule: "+baseNamespace+" => "+shortBasePath+"). Skipping.", class, filePath)
	}

	return nil, nil
}

// realCwd is realCwd(), computed once after Prefetch (the working
// directory does not change during the scans).
func (g *Generator) realCwd() (string, error) {
	if g.pre != nil && g.pre.realCwdOK {
		return g.pre.realCwd, nil
	}

	return realCwd()
}

// violationCwd is the normalized realpath of the working directory that
// PSR violation messages are relative to (computed once after Prefetch:
// the working directory does not change during the scans).
func (g *Generator) violationCwd() (string, error) {
	if g.pre != nil && g.pre.cwd != "" {
		return g.pre.cwd, nil
	}
	cwd, err := getCwd()
	if err != nil {
		return "", err
	}
	if real, ok := realpath(cwd); ok {
		cwd = real
	}
	cwd = normalizePath(cwd)
	if g.pre != nil {
		g.pre.cwd = cwd
	}

	return cwd, nil
}

// replaceCwd is Preg::replace('{^'.preg_quote($cwd).'}', '.', $path, 1).
func replaceCwd(path, cwd string) string {
	if strings.HasPrefix(path, cwd) {
		return "." + path[len(cwd):]
	}

	return path
}

// isAbsolutePath checks if the given path is absolute (see
// Composer\Util\Filesystem::isAbsolutePath).
func isAbsolutePath(path string) bool {
	return strings.HasPrefix(path, "/") || len(path) > 1 && path[1] == ':' || strings.HasPrefix(path, `\\`)
}

// streamWrappers are stream_get_wrappers() of a PHP CLI with the usual
// extensions.
var streamWrappers = [...]string{"https", "ftps", "php", "file", "glob", "data", "http", "ftp", "compress.zlib", "zip", "phar"}

// isStreamWrapperPath matches '{^(?:wrappers)://}'.
func isStreamWrapperPath(path string) bool {
	scheme, _, ok := strings.Cut(path, "://")
	if !ok {
		return false
	}
	for _, w := range streamWrappers {
		if scheme == w {
			return true
		}
	}

	return false
}

// collapseSeparators is Preg::replace('{(?<!:)[\\\\/]{2,}}', '/', $path):
// runs of two or more slashes or backslashes not preceded by a colon become
// one slash.
func collapseSeparators(path string) string {
	isSep := func(i int) bool { return i < len(path) && (path[i] == '/' || path[i] == '\\') }
	var b []byte
	for i := 0; i < len(path); i++ {
		if isSep(i) && isSep(i+1) && (i == 0 || path[i-1] != ':') {
			if b == nil {
				b = append(make([]byte, 0, len(path)), path[:i]...)
			}
			for isSep(i + 1) {
				i++
			}
			b = append(b, '/')

			continue
		}
		if b != nil {
			b = append(b, path[i])
		}
	}
	if b == nil {
		return path
	}

	return string(b)
}

// normalizePath replaces backslashes with slashes, removes the ending slash
// and collapses redundant separators and up-level references (see
// Composer\Util\Filesystem::normalizePath).
func normalizePath(path string) string {
	if isNormalAbsolute(path) {
		return path
	}
	path = strings.ReplaceAll(path, `\`, "/")
	prefix, absolute := "", ""

	// extract windows UNC paths e.g. \\foo\bar
	if strings.HasPrefix(path, "//") && len(path) > 2 {
		absolute = "//"
		path = path[2:]
	}

	// extract a prefix being a protocol://, protocol:, protocol://drive: or
	// simply drive:
	if n := pathPrefixLen(path); n > 0 {
		prefix = path[:n]
		path = path[n:]
	}

	if strings.HasPrefix(path, "/") {
		absolute = "/"
		path = path[1:]
	}

	var parts []string
	up := false
	for chunk := range strings.SplitSeq(path, "/") {
		if chunk == ".." && (absolute != "" || up) {
			if len(parts) > 0 {
				parts = parts[:len(parts)-1]
			}
			up = len(parts) > 0 && parts[len(parts)-1] != ".."
		} else if chunk != "." && chunk != "" {
			parts = append(parts, chunk)
			up = chunk != ".."
		}
	}

	// ensure c: is normalized to C:, matching '{(?:^|://)[a-z]:$}i'
	if n := len(prefix); n >= 2 && prefix[n-1] == ':' && isASCIILetter(prefix[n-2]) &&
		(n == 2 || strings.HasSuffix(prefix[:n-2], "://")) {
		prefix = prefix[:n-2] + strings.ToUpper(prefix[n-2:n-1]) + ":"
	}

	return prefix + absolute + strings.Join(parts, "/")
}

// isNormalAbsolute reports whether normalizePath() returns path unchanged
// because it is an absolute Unix path without empty, "." or ".." segments,
// backslashes or a trailing slash.
func isNormalAbsolute(path string) bool {
	if path == "" || path[0] != '/' || strings.IndexByte(path, '\\') >= 0 {
		return false
	}
	if len(path) == 1 {
		return true
	}
	segStart := 1
	for i := 1; i <= len(path); i++ {
		if i < len(path) && path[i] != '/' {
			continue
		}
		switch path[segStart:i] {
		case "", ".", "..":
			return false
		}
		segStart = i + 1
	}

	return true
}

// pathPrefixLen matches '{^( [0-9a-z]{2,}+: (?: // (?: [a-z]: )? )? | [a-z]: )}ix'
// and returns the length of the match, or 0.
func pathPrefixLen(path string) int {
	n := 0
	for n < len(path) && (isASCIILetter(path[n]) || path[n] >= '0' && path[n] <= '9') {
		n++
	}
	if n >= 2 && n < len(path) && path[n] == ':' {
		n++
		if strings.HasPrefix(path[n:], "//") {
			n += 2
			if n+1 < len(path) && isASCIILetter(path[n]) && path[n+1] == ':' {
				n += 2
			}
		}

		return n
	}
	if len(path) >= 2 && isASCIILetter(path[0]) && path[1] == ':' {
		return 2
	}

	return 0
}

func isASCIILetter(c byte) bool { return c|0x20 >= 'a' && c|0x20 <= 'z' }

// getCwd is Composer\Util\Platform::getCwd(): getcwd(), which reports the
// kernel's view of the working directory.
func getCwd() (string, error) {
	cwd, err := syscall.Getwd()
	if err != nil {
		return "", newException(classRuntime, "Could not determine the current working directory")
	}

	return cwd, nil
}

// realCwd is realpath(self::getCwd()), "" (PHP's false) when that fails.
func realCwd() (string, error) {
	cwd, err := getCwd()
	if err != nil {
		return "", err
	}
	real, _ := realpath(cwd)

	return real, nil
}
