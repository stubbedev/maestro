// Ports src/Composer/Cache.php: Composer's cache directories (repository
// metadata, dist archives, VCS mirrors). The archive downloaders keep
// every dist archive in the files cache as Composer does (and extract it
// into internal/store); Open is their copyTo when the store has the
// package already.

package cache

import (
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // Composer's sha1() of a cached file
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"hash"
	"io"
	"io/fs"
	"math/big"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	mio "github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/util"
)

// DefaultAllowlist is Cache's default $allowlist: the characters a cache
// key keeps, the others becoming "-".
const DefaultAllowlist = "a-z0-9._"

// cacheCollected is Cache::$cacheCollected: garbage collection runs at
// most once per process.
var cacheCollected atomic.Bool

// Cache ports Composer\Cache.
type Cache struct {
	io         mio.IO
	root       string
	enabled    int8 // 0 unknown, 1 enabled, -1 disabled
	sanitize   *php.Regexp
	filesystem *util.Filesystem
	readOnly   bool
	// now is the clock (tests move it).
	now func() time.Time
}

// New is new Cache($io, $cacheDir, $allowlist, $filesystem, $readOnly).
// An empty allowlist is the default; a nil filesystem a new one.
func New(ioi mio.IO, cacheDir, allowlist string, filesystem *util.Filesystem, readOnly bool) (*Cache, error) {
	if allowlist == "" {
		allowlist = DefaultAllowlist
	}

	re, err := php.Compile("{[^" + allowlist + "]}i")
	if err != nil {
		return nil, err
	}

	if filesystem == nil {
		filesystem = util.NewFilesystem(nil)
	}

	c := &Cache{
		io:         ioi,
		root:       strings.TrimRight(cacheDir, `/\`) + "/",
		sanitize:   re,
		filesystem: filesystem,
		readOnly:   readOnly,
		now:        time.Now,
	}

	if !IsUsable(cacheDir) {
		c.enabled = -1
	}

	return c, nil
}

// SetReadOnly is setReadOnly().
func (c *Cache) SetReadOnly(readOnly bool) { c.readOnly = readOnly }

// IsReadOnly is isReadOnly().
func (c *Cache) IsReadOnly() bool { return c.readOnly }

var unusableRegex = php.MustCompile(`{(^|[\\/])(\$null|nul|NUL|/dev/null)([\\/]|$)}`)

// IsUsable is Cache::isUsable($path): false for the null devices. The
// pattern does constant work per start position, so Preg::isMatch cannot
// throw.
func IsUsable(path string) bool {
	m, _ := unusableRegex.IsMatch(path)

	return !m
}

// IsEnabled is isEnabled(): the cache directory exists (it is created)
// and is writable, or the cache is read-only.
func (c *Cache) IsEnabled() bool {
	if c.enabled == 0 {
		c.enabled = 1

		if !c.readOnly && ((!isDir(c.root) && os.MkdirAll(c.root, 0o777) != nil) || !util.IsWritable(c.root)) {
			c.io.WriteError("<warning>Cannot create cache directory "+c.root+", or directory is not writable. Proceeding without cache. See also cache-read-only config if your filesystem is read-only.</warning>", true, mio.Normal)
			c.enabled = -1
		}
	}

	return c.enabled == 1
}

// Root is getRoot().
func (c *Cache) Root() string { return c.root }

// key sanitises a cache file name with the allowlist. The pattern is a
// single character class, so Preg::replace cannot throw.
func (c *Cache) key(file string) string {
	out, _, err := c.sanitize.Replace(file, "-", -1)
	if err != nil {
		return file
	}

	return out
}

// Read is read($file): the cached contents; false when missing.
func (c *Cache) Read(file string) (string, bool, error) {
	if !c.IsEnabled() {
		return "", false, nil
	}

	file = c.key(file)
	if !fileExists(c.root + file) {
		return "", false, nil
	}

	c.io.WriteError("Reading "+c.root+file+" from cache", true, mio.Debug)

	data, err := os.ReadFile(c.root + file)
	if err != nil {
		return "", false, &util.ErrorException{Message: "file_get_contents(" + c.root + file + "): Failed to open stream: " + util.Strerror(err), Site: phperr.At("Cache.php", 128)}
	}

	return string(data), true, nil
}

// ReadAll is Read of each file in turn, with the files read in parallel
// (deliberate deviation 3): the "Reading … from cache" lines come out in
// order once all reads are done, and an error is that of the first file
// that failed, after the lines of the files before it, as the loop of
// Reads would leave them. then, when not nil, is called with each file
// read, on the goroutine that read it.
func (c *Cache) ReadAll(files []string, then func(i int, contents string)) ([]string, []bool, error) {
	contents := make([]string, len(files))
	found := make([]bool, len(files))
	if !c.IsEnabled() || len(files) == 0 {
		return contents, found, nil
	}

	paths := make([]string, len(files))
	errs := make([]error, len(files))
	work := make(chan int)
	var wg sync.WaitGroup
	for range min(runtime.GOMAXPROCS(0), len(files)) {
		wg.Go(func() {
			for i := range work {
				paths[i] = c.root + c.key(files[i])
				if !fileExists(paths[i]) {
					continue
				}
				found[i] = true
				data, err := os.ReadFile(paths[i])
				if err != nil {
					errs[i] = err

					continue
				}
				contents[i] = string(data)
				if then != nil {
					then(i, contents[i])
				}
			}
		})
	}
	for i := range files {
		work <- i
	}
	close(work)
	wg.Wait()

	for i := range files {
		if !found[i] {
			continue
		}
		c.io.WriteError("Reading "+paths[i]+" from cache", true, mio.Debug)
		if errs[i] != nil {
			return nil, nil, &util.ErrorException{Message: "file_get_contents(" + paths[i] + "): Failed to open stream: " + util.Strerror(errs[i]), Site: phperr.At("Cache.php", 128)}
		}
	}

	return contents, found, nil
}

// Peek is Read without its output, for speculative reads that Composer
// does not make (deliberate deviation 3): a failure reads as a miss.
func (c *Cache) Peek(file string) (string, bool) {
	if !c.IsEnabled() {
		return "", false
	}

	data, err := os.ReadFile(c.root + c.key(file))
	if err != nil {
		return "", false
	}

	return string(data), true
}

// Peeker returns Peek as it is now, for use on other goroutines: the
// function reads the files without the cache's state, which only the
// goroutine using the cache may touch.
func (c *Cache) Peeker() func(file string) (string, bool) {
	if !c.IsEnabled() {
		return func(string) (string, bool) { return "", false }
	}
	root, sanitize := c.root, c.sanitize

	return func(file string) (string, bool) {
		key, _, err := sanitize.Replace(file, "-", -1)
		if err != nil {
			key = file
		}
		data, err := os.ReadFile(root + key)
		if err != nil {
			return "", false
		}

		return string(data), true
	}
}

// errPartialWrite carries file_put_contents' partial write warning.
type errPartialWrite struct{ written, total int }

func (e *errPartialWrite) Error() string {
	return "file_put_contents(): Only " + strconv.Itoa(e.written) + " of " + strconv.Itoa(e.total) + " bytes written, possibly out of free disk space"
}

// Write is write($file, $contents): it stores contents atomically (through
// a temporary file) and reports whether it did.
func (c *Cache) Write(file, contents string) (bool, error) {
	wasEnabled := c.enabled == 1

	if !c.IsEnabled() || c.readOnly {
		return false, nil
	}

	file = c.key(file)
	c.io.WriteError("Writing "+c.root+file+" into cache", true, mio.Debug)

	random := make([]byte, 5)
	_, _ = rand.Read(random)
	tempFileName := c.root + file + hex.EncodeToString(random) + ".tmp"

	err := writeFile(tempFileName, contents)
	if err == nil {
		if rerr := os.Rename(tempFileName, c.root+file); rerr != nil {
			err = &util.ErrorException{Message: "rename(" + tempFileName + "," + c.root + file + "): " + util.Strerror(rerr), Site: phperr.At("Cache.php", 149)}
		}
	}

	if err == nil {
		return true, nil
	}

	// If the write failed despite isEnabled checks passing earlier, rerun
	// the isEnabled checks to see if they are still current and recreate
	// the cache dir if needed. Refs composer/composer#11076
	if wasEnabled {
		c.enabled = 0

		return c.Write(file, contents)
	}

	c.io.WriteError("<warning>Failed to write into cache: "+err.Error()+"</warning>", true, mio.Debug)

	if partial, ok := errors.AsType[*errPartialWrite](err); ok {
		// Remove partial file.
		_ = os.Remove(tempFileName)

		c.io.WriteError("<warning>Writing "+tempFileName+" into cache failed after "+strconv.Itoa(partial.written)+" of "+strconv.Itoa(partial.total)+" bytes written, only "+diskFreeSpace(filepath.Dir(tempFileName))+" bytes of free space available</warning>", true, mio.Normal)

		return false, nil
	}

	return false, err
}

// writeFile is file_put_contents() with Composer's error handler: failures
// are ErrorExceptions, partial writes *errPartialWrite.
func writeFile(path, contents string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o666) //nolint:gosec // file_put_contents mode, umask applies
	if err != nil {
		return &util.ErrorException{Message: "file_put_contents(" + path + "): Failed to open stream: " + util.Strerror(err), Site: phperr.At("Cache.php", 149)}
	}

	n, werr := io.WriteString(f, contents)
	cerr := f.Close()

	if werr != nil || cerr != nil {
		return &errPartialWrite{written: n, total: len(contents)}
	}

	return nil
}

// CopyFrom is copyFrom($file, $source): it stores a copy of source.
func (c *Cache) CopyFrom(file, source string) (bool, error) {
	if !c.IsEnabled() || c.readOnly {
		return false, nil
	}

	file = c.key(file)

	if err := util.EnsureDirectoryExists(filepath.Dir(c.root + file)); err != nil {
		return false, err
	}

	if !fileExists(source) {
		c.io.WriteError("<error>"+source+" does not exist, can not write into cache</error>", true, mio.Normal)
	} else if c.io.IsDebug() {
		c.io.WriteError("Writing "+c.root+file+" into cache from "+source, true, mio.Normal)
	}

	return util.Copy(source, c.root+file)
}

// CopyTo is copyTo($file, $target): it copies the cached file to target,
// marking it as recently used.
func (c *Cache) CopyTo(file, target string) (bool, error) {
	if !c.IsEnabled() {
		return false, nil
	}

	file = c.key(file)
	if !fileExists(c.root + file) {
		return false, nil
	}

	now := c.now()

	if fi, err := os.Stat(c.root + file); err == nil {
		if os.Chtimes(c.root+file, now, fi.ModTime()) != nil {
			// fallback in case the above failed due to incorrect ownership
			// see composer/composer#4070
			_ = os.Chtimes(c.root+file, now, now)
		}
	}

	c.io.WriteError("Reading "+c.root+file+" from cache", true, mio.Debug)

	return util.Copy(c.root+file, target)
}

// Open is copyTo($file, $target) without the copy: the cached file is
// marked as recently used and its debug line printed as copyTo does, and
// it is returned open (nil when missing), so that its content stays
// readable even if a garbage collection removes it meanwhile.
func (c *Cache) Open(file string) (*os.File, error) {
	if !c.IsEnabled() {
		return nil, nil
	}

	file = c.key(file)

	f, err := os.Open(c.root + file)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	now := c.now()

	if fi, err := f.Stat(); err != nil || !fi.Mode().IsRegular() {
		_ = f.Close()

		return nil, err
	} else if os.Chtimes(c.root+file, now, fi.ModTime()) != nil {
		// fallback in case the above failed due to incorrect ownership
		// see composer/composer#4070
		_ = os.Chtimes(c.root+file, now, now)
	}

	c.io.WriteError("Reading "+c.root+file+" from cache", true, mio.Debug)

	return f, nil
}

// GcIsNecessary is gcIsNecessary(): true once in a while (1 in 51), at
// most once per process.
func (c *Cache) GcIsNecessary() bool {
	if cacheCollected.Swap(true) {
		return false
	}

	if v, ok := util.GetEnv("COMPOSER_TEST_SUITE"); ok && php.ToBool(v) {
		return false
	}

	if util.IsInputCompletionProcess() {
		return false
	}

	n, err := rand.Int(rand.Reader, big.NewInt(51))

	return err == nil && n.Sign() == 0
}

// Remove is remove($file).
func (c *Cache) Remove(file string) (bool, error) {
	if !c.IsEnabled() || c.readOnly {
		return false, nil
	}

	file = c.key(file)
	if !fileExists(c.root + file) {
		return false, nil
	}

	if err := util.Unlink(c.root + file); err != nil {
		return false, err
	}

	return true, nil
}

// Clear is clear(): it empties the cache directory.
func (c *Cache) Clear() (bool, error) {
	if !c.IsEnabled() || c.readOnly {
		return false, nil
	}

	if err := c.filesystem.EmptyDirectory(c.root, true); err != nil {
		return false, err
	}

	return true, nil
}

// Holds reports whether file is in the cache, silently: no output, no
// access time update, and the cache directory is neither created nor
// checked (maestro's, for work started ahead of the request that reads
// the file).
func (c *Cache) Holds(file string) bool {
	fi, err := os.Stat(c.root + c.key(file))

	return err == nil && fi.Mode().IsRegular()
}

// Age is getAge($file): seconds since the file was written; false when
// missing.
func (c *Cache) Age(file string) (int64, bool) {
	if !c.IsEnabled() {
		return 0, false
	}

	fi, err := os.Stat(c.root + c.key(file))
	if err != nil {
		return 0, false
	}

	age := c.now().Unix() - fi.ModTime().Unix()
	if age < 0 {
		age = -age
	}

	return age, true
}

// Gc is gc($ttl, $maxSize): it removes the files not written for ttl
// seconds, then the least recently used ones until the cache holds at most
// maxSize bytes.
func (c *Cache) Gc(ttl int, maxSize int64) (bool, error) {
	if !c.IsEnabled() || c.readOnly {
		return false, nil
	}

	expire := c.now().Unix() - int64(ttl)

	files, err := c.files()
	if err != nil {
		return false, err
	}

	kept := files[:0]

	for _, f := range files {
		if f.info.ModTime().Unix() < expire {
			if err := util.Unlink(f.path); err != nil {
				return false, err
			}

			continue
		}

		kept = append(kept, f)
	}

	totalSize, err := util.Size(c.root)
	if err != nil {
		return false, err
	}

	if totalSize > maxSize {
		slices.SortStableFunc(kept, func(a, b cachedFile) int {
			return util.FileAtime(a.info).Compare(util.FileAtime(b.info))
		})

		for _, f := range kept {
			if totalSize <= maxSize {
				break
			}

			size, err := util.Size(f.path)
			if err != nil {
				return false, err
			}

			totalSize -= size

			if err := util.Unlink(f.path); err != nil {
				return false, err
			}
		}
	}

	cacheCollected.Store(true)

	return true, nil
}

// GcVcsCache is gcVcsCache($ttl): it removes the VCS mirrors (top-level
// directories) not updated for ttl seconds.
func (c *Cache) GcVcsCache(ttl int) (bool, error) {
	if !c.IsEnabled() {
		return false, nil
	}

	expire := c.now().Unix() - int64(ttl)

	entries, err := os.ReadDir(c.root)
	if err != nil {
		return false, err
	}

	for _, e := range entries {
		if finderIgnored(e.Name()) {
			continue
		}

		info, err := os.Stat(filepath.Join(c.root, e.Name()))
		if err != nil || !info.IsDir() || info.ModTime().Unix() >= expire {
			continue
		}

		if _, err := c.filesystem.RemoveDirectory(c.root + e.Name()); err != nil {
			return false, err
		}
	}

	cacheCollected.Store(true)

	return true, nil
}

// Sha1 is sha1($file): the file's SHA-1 in hex; false when missing.
func (c *Cache) Sha1(file string) (string, bool, error) {
	return c.hash(file, sha1.New()) //nolint:gosec // Composer's sha1() of a cached file
}

// Sha256 is sha256($file): the file's SHA-256 in hex; false when missing.
func (c *Cache) Sha256(file string) (string, bool, error) {
	return c.hash(file, sha256.New())
}

func (c *Cache) hash(file string, h hash.Hash) (string, bool, error) {
	if !c.IsEnabled() {
		return "", false, nil
	}

	file = c.key(file)
	if !fileExists(c.root + file) {
		return "", false, nil
	}

	f, err := os.Open(c.root + file)
	if err != nil {
		return "", false, err
	}
	defer f.Close()

	if _, err := io.Copy(h, f); err != nil {
		return "", false, err
	}

	return hex.EncodeToString(h.Sum(nil)), true, nil
}

type cachedFile struct {
	path string
	info fs.FileInfo
}

// files is getFinder(): the files under the cache root, as Symfony's
// Finder lists them by default (dot files and VCS directories skipped,
// symlinked directories not followed).
func (c *Cache) files() ([]cachedFile, error) {
	var out []cachedFile

	root := strings.TrimSuffix(c.root, "/")

	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == root {
				return err
			}

			return nil
		}

		if p == root {
			return nil
		}

		if finderIgnored(d.Name()) {
			if d.IsDir() {
				return filepath.SkipDir
			}

			return nil
		}

		if d.IsDir() {
			return nil
		}

		info, err := os.Stat(p)
		if err != nil || !info.Mode().IsRegular() {
			return nil //nolint:nilerr // broken links are skipped like Finder does
		}

		out = append(out, cachedFile{path: p, info: info})

		return nil
	})

	return out, err
}

// finderIgnored reports the names Finder skips by default
// (IGNORE_DOT_FILES and IGNORE_VCS_FILES).
func finderIgnored(name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}

	switch name {
	case "CVS", "_darcs", "_svn":
		return true
	}

	return false
}

func fileExists(path string) bool {
	_, err := os.Stat(path)

	return err == nil
}

func isDir(path string) bool {
	fi, err := os.Stat(path)

	return err == nil && fi.IsDir()
}
