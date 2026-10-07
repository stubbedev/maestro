// Ports src/Composer/Util/Filesystem.php.

package util

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util/fspath"
)

// Filesystem ports Composer\Util\Filesystem. Operations that never shell
// out are package functions; the methods are those that may run external
// commands through the ProcessExecutor, directly or through a callee.
type Filesystem struct {
	executor *ProcessExecutor
}

// NewFilesystem returns a Filesystem running external commands through
// executor, or through a ProcessExecutor of its own when nil.
func NewFilesystem(executor *ProcessExecutor) *Filesystem {
	return &Filesystem{executor: executor}
}

func (fs *Filesystem) process() *ProcessExecutor {
	if fs.executor == nil {
		fs.executor = NewProcessExecutor(nil)
	}

	return fs.executor
}

// Remove ports Filesystem::remove.
func (fs *Filesystem) Remove(file string) (bool, error) {
	if isDir(file) {
		return fs.RemoveDirectory(file)
	}

	if fileExists(file) {
		return true, Unlink(file)
	}

	return false, nil
}

// finderIn ports Finder::create()->in($dir) as Composer uses it (depth 0,
// ignoreVCS(false), ignoreDotFiles(false)): it opens the directory, its
// trailing slashes trimmed as Finder's normalizeDir does.
func finderIn(dir string) (string, *os.File, error) {
	if dir != "/" {
		dir = strings.TrimRight(dir, dirSeparators)
	}

	if !isDir(dir) {
		// DirectoryNotFoundException.
		return "", nil, &InvalidArgumentError{Class: ClassDirectoryNotFound, Message: `The "` + dir + `" directory does not exist.`}
	}

	f, err := os.Open(dir)
	if err != nil {
		// AccessDeniedException, wrapping RecursiveDirectoryIterator's.
		return "", nil, dirIteratorError(dir, err)
	}

	return dir, f, nil
}

// IsDirEmpty ports Filesystem::isDirEmpty, counting dot files and VCS
// directories too.
func IsDirEmpty(dir string) (bool, error) {
	dir, f, err := finderIn(dir)
	if err != nil {
		return false, err
	}
	defer f.Close()

	if _, err := f.Readdirnames(1); err != nil {
		if errors.Is(err, io.EOF) {
			return true, nil
		}

		return false, dirIteratorError(dir, err)
	}

	return false, nil
}

// EmptyDirectory ports Filesystem::emptyDirectory.
func (fs *Filesystem) EmptyDirectory(dir string, ensureDirectoryExists bool) error {
	if isLink(dir) && fileExists(dir) {
		if err := Unlink(dir); err != nil {
			return err
		}
	}

	if ensureDirectoryExists {
		if err := EnsureDirectoryExists(dir); err != nil {
			return err
		}
	}

	if !isDir(dir) {
		return nil
	}

	dir, f, err := finderIn(dir)
	if err != nil {
		return err
	}

	names, err := f.Readdirnames(-1)
	_ = f.Close()

	if err != nil {
		return dirIteratorError(dir, err)
	}

	for _, name := range names {
		if _, err := fs.Remove(dir + "/" + name); err != nil {
			return err
		}
	}

	return nil
}

// readDir lists dir in directory order (os.ReadDir would sort), the way
// RecursiveDirectoryIterator with SKIP_DOTS does.
func readDir(dir string) ([]fs.DirEntry, error) {
	f, err := os.Open(dir)
	if err != nil {
		return nil, dirIteratorError(dir, err)
	}
	defer f.Close()

	entries, err := f.ReadDir(-1)
	if err != nil {
		return nil, dirIteratorError(dir, err)
	}

	return entries, nil
}

// entryIsDir is SplFileInfo::isDir for a directory entry: whether it is a
// directory, following symlinks. hasChildren reports whether
// RecursiveDirectoryIterator descends into it, which it does not through
// symlinks.
func entryIsDir(path string, entry fs.DirEntry) (isDir, hasChildren bool) {
	switch typ := entry.Type(); {
	case typ.IsDir():
		return true, true
	case typ&fs.ModeSymlink != 0:
		fi, err := os.Stat(path)

		return err == nil && fi.IsDir(), false
	default:
		return false, false
	}
}

// RemoveDirectory ports Filesystem::removeDirectory: recursively removes a
// directory with rm -rf (rmdir /S /Q on Windows), falling back on
// RemoveDirectoryPhp.
func (fs *Filesystem) RemoveDirectory(directory string) (bool, error) {
	if result, done, err := removeEdgeCases(directory); done {
		return result, err
	}

	code, err := fs.process().Execute(removeDirectoryCommand(directory), new(string), "")
	if err != nil {
		return false, err
	}

	if code == 0 && !isDir(directory) {
		return true, nil
	}

	return RemoveDirectoryPhp(directory)
}

// RemoveDirectoryAsync ports Filesystem::removeDirectoryAsync. The executor
// must have async enabled.
func (fs *Filesystem) RemoveDirectoryAsync(directory string) (*Promise[bool], error) {
	if result, done, err := removeEdgeCases(directory); done {
		if err != nil {
			return nil, err
		}

		return Resolved(result), nil
	}

	job, err := fs.process().ExecuteAsync(removeDirectoryCommand(directory), "")
	if err != nil {
		return nil, err
	}

	return Then(job, func(p *Process) (bool, error) {
		if p.IsSuccessful() && !isDir(directory) {
			return true, nil
		}

		return RemoveDirectoryPhp(directory)
	}), nil
}

func removeDirectoryCommand(directory string) Command {
	if IsWindows() {
		return Cmd("rmdir", "/S", "/Q", Realpath(directory))
	}

	return Cmd("rm", "-rf", directory)
}

// RemoveEdgeCases is removeEdgeCases for the plugin shim, whose
// Filesystem::removeDirectoryAsync() runs the removal in PHP.
func RemoveEdgeCases(directory string) (result, done bool, err error) {
	return removeEdgeCases(directory)
}

// removeEdgeCases ports Filesystem::removeEdgeCases; done reports whether an
// edge case was hit (always with an error), result is then whether removal
// succeeded.
func removeEdgeCases(directory string) (result, done bool, err error) {
	dirOK := isDir(directory)
	link := isLink(directory)

	// isSymlinkedDirectory, then unlinkSymlinkedDirectory.
	if dirOK {
		resolved := resolveSymlinkedDirectorySymlink(directory, true)
		if resolved == directory && link || resolved != directory && isLink(resolved) {
			if err := Unlink(resolved); err != nil {
				return false, true, err
			}

			return true, true, nil
		}
	}

	if IsJunction(directory) {
		result, err := RemoveJunction(directory)

		return result, true, err
	}

	if link {
		if err := unlinkPath(directory); err != nil {
			return false, true, warning("unlink", directory, err)
		}

		return true, true, nil
	}

	if !dirOK {
		return true, true, nil
	}

	if isRootPath(directory) {
		return false, true, &RuntimeError{Message: "Aborting an attempted deletion of " + directory + ", this was probably not intended, if it is a real use case please report it."}
	}

	return false, false, nil
}

// isRootPath matches {^(?:[a-z]:)?[/\\]+$}i.
func isRootPath(path string) bool {
	if len(path) >= 2 && isASCIIAlpha(path[0]) && path[1] == ':' && isOnlySlashes(path[2:]) {
		return true
	}

	return isOnlySlashes(path)
}

// isOnlySlashes matches ^[/\\]+$, where $ also matches before a final
// newline.
func isOnlySlashes(path string) bool {
	path = strings.TrimSuffix(path, "\n")
	if path == "" {
		return false
	}

	for i := range len(path) {
		if path[i] != '/' && path[i] != '\\' {
			return false
		}
	}

	return true
}

// RemoveDirectoryPhp ports Filesystem::removeDirectoryPhp: a child-first
// walk deleting files, then their directories.
func RemoveDirectoryPhp(directory string) (bool, error) {
	if result, done, err := removeEdgeCases(directory); done {
		return result, err
	}

	entries, err := readDir(directory)
	if err != nil {
		// Retry once, it sometimes fails without apparent reason, see
		// https://github.com/composer/composer/issues/4009
		time.Sleep(100 * time.Millisecond)

		if !isDir(directory) {
			return true, nil
		}

		if entries, err = readDir(directory); err != nil {
			return false, err
		}
	}

	if err := removeChildren(directory, entries); err != nil {
		return false, err
	}

	if err := Rmdir(directory); err != nil {
		return false, err
	}

	return true, nil
}

// removeChildren deletes the entries of dir child first, as a CHILD_FIRST
// RecursiveIteratorIterator does: directories (symlinks to directories
// included) are rmdir'ed, everything else is unlinked.
func removeChildren(dir string, entries []fs.DirEntry) error {
	for _, entry := range entries {
		path := dir + "/" + entry.Name()

		isDir, hasChildren := entryIsDir(path, entry)
		if hasChildren {
			children, err := readDir(path)
			if err != nil {
				return err
			}

			if err := removeChildren(path, children); err != nil {
				return err
			}
		}

		remove := Unlink
		if isDir {
			remove = Rmdir
		}

		if err := remove(path); err != nil {
			return err
		}
	}

	return nil
}

// EnsureDirectoryExists ports Filesystem::ensureDirectoryExists.
func EnsureDirectoryExists(directory string) error {
	if fi, err := os.Stat(directory); err == nil {
		if fi.IsDir() {
			return nil
		}

		return &RuntimeError{Message: directory + " exists and is not a directory."}
	}

	if isLink(directory) {
		if fn, err := unlinkImplementation(directory); err != nil {
			return &RuntimeError{Message: "Could not delete symbolic link " + directory + ": " + phpWarning(fn, directory, err)}
		}
	}

	err := os.MkdirAll(directory, 0o777)
	if err == nil {
		return nil
	}

	// Maybe another process created it since we checked above?
	if isDir(directory) {
		return nil
	}

	failure := &RuntimeError{Message: directory + " does not exist and could not be created: mkdir(): " + strerror(err)}

	// In pathological cases with paths like path/to/broken-symlink/../foo
	// is_dir fails to detect path/to/foo, but normalizing the ../ away first
	// works, see https://github.com/composer/composer/issues/11864
	if normalized := fspath.NormalizePath(directory); normalized != directory {
		if EnsureDirectoryExists(normalized) == nil {
			return nil
		}
	}

	return failure
}

// windowsLockHint is appended to deletion errors on Windows.
const windowsLockHint = "\nThis can be due to an antivirus or the Windows Search Indexer locking the file while they are analyzed"

// Unlink ports Filesystem::unlink, retrying once after 350ms on Windows.
func Unlink(path string) error {
	return retryDelete(path, unlinkImplementation)
}

// Rmdir ports Filesystem::rmdir, retrying once after 350ms on Windows.
func Rmdir(path string) error {
	return retryDelete(path, func(path string) (string, error) { return "rmdir", rmdirPath(path) })
}

// retryDelete runs remove, which returns the name of the PHP function it
// called, again after 350ms on Windows if it failed.
func retryDelete(path string, remove func(string) (string, error)) error {
	fn, err := remove(path)
	if err == nil {
		return nil
	}

	// Retry after a bit on Windows since it tends to be touchy with mass
	// removals.
	if IsWindows() {
		time.Sleep(350 * time.Millisecond)

		if fn, err = remove(path); err == nil {
			return nil
		}
	}

	message := "Could not delete " + path + ": " + phpWarning(fn, path, err)
	if IsWindows() {
		message += windowsLockHint
	}

	return &RuntimeError{Message: message}
}

// unlinkImplementation removes a symlink or file; directory symlinks on
// Windows need rmdir instead of unlink. It returns the PHP function used.
func unlinkImplementation(path string) (string, error) {
	if IsWindows() && isDir(path) && isLink(path) {
		return "rmdir", rmdirPath(path)
	}

	return "unlink", unlinkPath(path)
}

// CopyThenRemove ports Filesystem::copyThenRemove, a non-atomic rename.
func CopyThenRemove(source, target string) error {
	if _, err := Copy(source, target); err != nil {
		return err
	}

	if !isDir(source) {
		return Unlink(source)
	}

	_, err := RemoveDirectoryPhp(source)

	return err
}

// Copy ports Filesystem::copy: a file, or the contents of a directory tree.
// New files and directories get default permissions. PHP's copy() fails
// without a warning, so this returns false without an error, when source
// and target are the same file.
func Copy(source, target string) (bool, error) {
	// Refs https://github.com/composer/composer/issues/11864
	target = fspath.NormalizePath(target)

	if !isDir(source) {
		return phpCopy(source, target)
	}

	entries, err := readDir(source)
	if err != nil {
		return false, err
	}

	if err := EnsureDirectoryExists(target); err != nil {
		return false, err
	}

	result := true
	err = copyTree(source, target+string(os.PathSeparator), entries, &result)

	return result && err == nil, err
}

// copyTree walks dir self first. Directories (symlinks to directories
// included) are created below target, descending only into real ones;
// files are copied until one copy() returns false, as `$result && copy()`
// does.
func copyTree(dir, target string, entries []fs.DirEntry, result *bool) error {
	for _, entry := range entries {
		path := dir + "/" + entry.Name()
		targetPath := target + entry.Name()

		isDir, hasChildren := entryIsDir(path, entry)
		if !isDir {
			if *result {
				ok, err := phpCopy(path, targetPath)
				if err != nil {
					return err
				}

				*result = ok
			}

			continue
		}

		if err := EnsureDirectoryExists(targetPath); err != nil {
			return err
		}

		if hasChildren {
			children, err := readDir(path)
			if err != nil {
				return err
			}

			if err := copyTree(path, targetPath+string(os.PathSeparator), children, result); err != nil {
				return err
			}
		}
	}

	return nil
}

// phpCopy ports PHP's copy() (php_copy_file_ctx) for local files: the
// contents of source, following symlinks, into target, created with default
// permissions or truncated.
func phpCopy(source, target string) (bool, error) {
	if srcInfo, err := os.Stat(source); err == nil {
		if srcInfo.IsDir() {
			return false, &ErrorException{Message: "copy(): The first argument to copy() function cannot be a directory"}
		}

		if dstInfo, err := os.Stat(target); err == nil {
			if dstInfo.IsDir() {
				return false, &ErrorException{Message: "copy(): The second argument to copy() function cannot be a directory"}
			}

			if os.SameFile(srcInfo, dstInfo) {
				return false, nil
			}
		}
	}

	src, err := os.Open(source)
	if err != nil {
		return false, streamWarning("copy", source, err)
	}
	defer src.Close()

	dst, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o666) //nolint:gosec // PHP's "wb" mode; the umask applies.
	if err != nil {
		return false, streamWarning("copy", target, err)
	}

	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()

		return false, warning("copy", source, err)
	}

	if err := dst.Close(); err != nil {
		return false, warning("copy", target, err)
	}

	return true, nil
}

// Rename ports Filesystem::rename: rename(2), then mv (xcopy on Windows),
// then copy and remove.
func (fs *Filesystem) Rename(source, target string) error {
	if phpRename(source, target) {
		return nil
	}

	if IsWindows() {
		// Try to copy & delete - this is a workaround for random "Access
		// denied" errors.
		code, err := fs.process().Execute(Cmd("xcopy", source, target, "/E", "/I", "/Q", "/Y"), new(string), "")
		if err != nil {
			return err
		}

		if code == 0 {
			_, err := fs.Remove(source)

			return err
		}
	} else {
		// rename(2) does not move between partitions.
		code, err := fs.process().Execute(Cmd("mv", source, target), new(string), "")
		if err != nil {
			return err
		}

		if code == 0 {
			return nil
		}
	}

	return CopyThenRemove(source, target)
}

// phpRename ports PHP's rename() for local files, which moves a file (not a
// directory) across devices by copying it with its owner and mode.
func phpRename(source, target string) bool {
	err := os.Rename(source, target)
	if err == nil {
		return true
	}

	if !errors.Is(err, syscall.EXDEV) {
		return false
	}

	if ok, err := phpCopy(source, target); !ok || err != nil {
		return false
	}

	fi, err := os.Stat(source)
	if err != nil || chownLike(target, fi) != nil {
		return false
	}

	if err := os.Chmod(target, fi.Mode()); err != nil && !errors.Is(err, syscall.EPERM) {
		return false
	}

	_ = os.Remove(source)

	return true
}

// FindShortestPath ports Filesystem::findShortestPath: the shortest path
// from from to to, both absolute; with directories they are directories.
func FindShortestPath(from, to string, directories, preferRelative bool) (string, error) {
	return findShortestPath(from, to, directories, preferRelative, IsWindows())
}

func findShortestPath(from, to string, directories, preferRelative, windows bool) (string, error) {
	if !fspath.IsAbsolutePath(from) || !fspath.IsAbsolutePath(to) {
		return "", absolutePathsError(from, to)
	}

	from = fspath.NormalizePath(from)
	to = fspath.NormalizePath(to)

	if directories {
		from = strings.TrimRight(from, "/") + "/dummy_file"
	}

	if php.DirnameOn(from, windows) == php.DirnameOn(to, windows) {
		return "./" + php.BasenameOn(to, "", windows), nil
	}

	commonPath := findCommonPath(from, to, windows, false)

	// No commonality at all.
	if !strings.HasPrefix(from, commonPath) {
		return to, nil
	}

	commonPath = strings.TrimRight(commonPath, "/") + "/"
	sourcePathDepth := strings.Count(substrFrom(from, len(commonPath)), "/")

	// Allow top level /foo & /bar dirs to be addressed relatively as this is
	// common in Docker setups.
	if !preferRelative && commonPath == "/" && sourcePathDepth > 1 {
		return to, nil
	}

	result := strings.Repeat("../", sourcePathDepth) + substrFrom(to, len(commonPath))
	if result == "" {
		return "./", nil
	}

	return result, nil
}

func absolutePathsError(from, to string) error {
	return &InvalidArgumentError{Message: "$from (" + from + ") and $to (" + to + ") must be absolute paths."}
}

// FindShortestPathCode ports Filesystem::findShortestPathCode: PHP code
// that, run in from, evaluates to the path of to.
func FindShortestPathCode(from, to string, directories, staticCode, preferRelative bool) (string, error) {
	return findShortestPathCode(from, to, directories, staticCode, preferRelative, IsWindows())
}

func findShortestPathCode(from, to string, directories, staticCode, preferRelative, windows bool) (string, error) {
	if !fspath.IsAbsolutePath(from) || !fspath.IsAbsolutePath(to) {
		return "", absolutePathsError(from, to)
	}

	from = fspath.NormalizePath(from)
	to = fspath.NormalizePath(to)

	if from == to {
		if directories {
			return "__DIR__", nil
		}

		return "__FILE__", nil
	}

	commonPath := findCommonPath(from, to, windows, true)

	// No commonality at all.
	if !strings.HasPrefix(from, commonPath) || commonPath == "." {
		return php.VarExport(to), nil
	}

	commonPath = strings.TrimRight(commonPath, "/") + "/"

	// str_starts_with($to, $from.'/'), from and to differing.
	if isPathPrefix(to, from) {
		return "__DIR__ . " + php.VarExport(to[len(from):]), nil
	}

	sourcePathDepth := strings.Count(substrFrom(from, len(commonPath)), "/")
	if directories {
		sourcePathDepth++
	}

	// Allow top level /foo & /bar dirs to be addressed relatively as this is
	// common in Docker setups.
	if !preferRelative && commonPath == "/" && sourcePathDepth > 1 {
		return php.VarExport(to), nil
	}

	var commonPathCode string
	if staticCode {
		commonPathCode = "__DIR__ . '" + strings.Repeat("/..", sourcePathDepth) + "'"
	} else {
		commonPathCode = strings.Repeat("dirname(", sourcePathDepth) + "__DIR__" + strings.Repeat(")", sourcePathDepth)
	}

	relTarget := substrFrom(to, len(commonPath))
	if relTarget == "" {
		return commonPathCode, nil
	}

	return commonPathCode + "." + php.VarExport("/"+relTarget), nil
}

// findCommonPath walks up from to until it is a path prefix of from, the
// root or a drive root (or "." with stopAtDot). PHP loops forever on a path
// whose dirname is itself and none of those ("." without stopAtDot); this
// stops there instead.
func findCommonPath(from, to string, windows, stopAtDot bool) string {
	commonPath := to
	for !isPathPrefix(from, commonPath) && commonPath != "/" && !isDriveRoot(commonPath) && (!stopAtDot || commonPath != ".") {
		parent := php.DirnameOn(commonPath, windows)
		if windows {
			parent = strings.ReplaceAll(parent, `\`, "/")
		}

		if parent == commonPath {
			break
		}

		commonPath = parent
	}

	return commonPath
}

// isPathPrefix is strpos($path.'/', $prefix.'/') === 0: prefix is path or
// one of its parent directories.
func isPathPrefix(path, prefix string) bool {
	return strings.HasPrefix(path, prefix) && (len(path) == len(prefix) || path[len(prefix)] == '/')
}

// isDriveRoot matches {^[A-Z]:/?$}i, where $ also matches before a final
// newline.
func isDriveRoot(path string) bool {
	if len(path) < 2 || !isASCIIAlpha(path[0]) || path[1] != ':' {
		return false
	}

	rest := path[2:]

	return rest == "" || rest == "/" || rest == "\n" || rest == "/\n"
}

// substrFrom is PHP's (string) substr($s, $start) for 0 <= start.
func substrFrom(s string, start int) string {
	if start >= len(s) {
		return ""
	}

	return s[start:]
}

// Size ports Filesystem::size: the size of a file, or of all files below a
// directory.
func Size(path string) (int64, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return 0, &RuntimeError{Message: path + " does not exist."}
	}

	if fi.IsDir() {
		return directorySize(path)
	}

	return fi.Size(), nil
}

// directorySize sums the regular files (following symlinks) below dir,
// descending into real directories only.
func directorySize(dir string) (int64, error) {
	entries, err := readDir(dir)
	if err != nil {
		return 0, err
	}

	var size int64

	for _, entry := range entries {
		switch typ := entry.Type(); {
		case typ.IsDir():
			n, err := directorySize(dir + "/" + entry.Name())
			if err != nil {
				return 0, err
			}

			size += n
		case typ.IsRegular():
			if fi, err := entry.Info(); err == nil {
				size += fi.Size()
			}
		case typ&fs.ModeSymlink != 0:
			if fi, err := os.Stat(dir + "/" + entry.Name()); err == nil && fi.Mode().IsRegular() {
				size += fi.Size()
			}
		}
	}

	return size, nil
}

// TrimTrailingSlash ports Filesystem::trimTrailingSlash, which leaves a path
// of only slashes alone.
func TrimTrailingSlash(path string) string {
	if !isOnlySlashes(path) {
		path = strings.TrimRight(path, `/\`)
	}

	return path
}

// IsLocalPath ports Filesystem::isLocalPath.
func IsLocalPath(path string) bool {
	return isLocalPath(path, IsWindows())
}

// isLocalPath matches, case-insensitively, on Windows
// {^(file://(?!//)|/(?!/)|/?[a-z]:[\\/]|\.\.[\\/]|[a-z0-9_.-]+[\\/])}i and
// elsewhere {^(file://|/|/?[a-z]:[\\/]|\.\.[\\/]|[a-z0-9_.-]+[\\/])}i. On
// Windows \\foo is a network path; on Linux file:////foo resolves to /foo.
func isLocalPath(path string, windows bool) bool {
	isSep := func(i int) bool { return i < len(path) && (path[i] == '/' || path[i] == '\\') }

	if php.Strncasecmp(path, "file://", 7) == 0 && (!windows || !strings.HasPrefix(path[7:], "//")) {
		return true
	}

	drive := 0
	if strings.HasPrefix(path, "/") {
		if !windows || !strings.HasPrefix(path[1:], "/") {
			return true
		}

		drive = 1
	}

	if drive+1 < len(path) && isASCIIAlpha(path[drive]) && path[drive+1] == ':' && isSep(drive+2) {
		return true
	}

	// \.\.[\\/] is covered by the name run, which includes dots.
	n := 0
	for n < len(path) && (isASCIIAlnum(path[n]) || path[n] == '_' || path[n] == '.' || path[n] == '-') {
		n++
	}

	return n > 0 && isSep(n)
}

// GetPlatformPath ports Filesystem::getPlatformPath: strips a file://
// scheme, keeping Windows drive letters as file://C:/ paths.
func GetPlatformPath(path string) string {
	return getPlatformPath(path, IsWindows())
}

func getPlatformPath(path string, windows bool) string {
	// {^(?:file:///([a-z]):?/)}i => file://$1:/
	if windows && php.Strncasecmp(path, "file:///", 8) == 0 && len(path) > 8 && isASCIIAlpha(path[8]) {
		if rest := strings.TrimPrefix(path[9:], ":"); strings.HasPrefix(rest, "/") {
			path = "file://" + path[8:9] + ":/" + rest[1:]
		}
	}

	if php.Strncasecmp(path, "file://", 7) == 0 {
		return path[7:]
	}

	return path
}

// IsReadable ports Filesystem::isReadable, which also tries reading as
// is_readable can not be trusted on network mounts and \\wsl$ paths.
func IsReadable(path string) bool {
	if isReadableAccess(path) {
		return true
	}

	fi, err := os.Stat(path)
	if err != nil || (!fi.Mode().IsRegular() && !fi.IsDir()) {
		return false
	}

	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	if fi.Mode().IsRegular() {
		var b [1]byte

		_, err = f.Read(b[:])

		return err == nil || errors.Is(err, io.EOF)
	}

	return true
}

// RelativeSymlink ports Filesystem::relativeSymlink: a symlink at link to
// target, by the shortest relative path. PHP chdirs to the link's directory
// around symlink(); link being absolute, that changes nothing, so the
// process-wide working directory is left alone.
func RelativeSymlink(target, link string) (bool, error) {
	relativePath, err := FindShortestPath(link, target, false, false)
	if err != nil {
		return false, err
	}

	dir := php.DirnameOn(link, IsWindows())
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		if err == nil {
			err = php.ENOTDIR
		}
		errno, _ := php.Errno(err)

		return false, &ErrorException{Message: "chdir(): " + php.Strerror(err) + " (errno " + strconv.Itoa(errno) + ")"}
	}

	return os.Symlink(relativePath, link) == nil, nil
}

// IsSymlinkedDirectory ports Filesystem::isSymlinkedDirectory.
func IsSymlinkedDirectory(directory string) bool {
	if !isDir(directory) {
		return false
	}

	return isLink(resolveSymlinkedDirectorySymlink(directory, true))
}

// resolveSymlinkedDirectorySymlink strips trailing slashes from a directory
// path so it names the symlink rather than its target; dirOK is is_dir.
func resolveSymlinkedDirectorySymlink(pathname string, dirOK bool) string {
	if !dirOK {
		return pathname
	}

	if resolved := strings.TrimRight(pathname, "/"); resolved != "" {
		return resolved
	}

	return pathname
}

// FilePutContentsIfModified ports Filesystem::filePutContentsIfModified: it
// writes only when the content differs and returns the bytes written.
func FilePutContentsIfModified(path string, content []byte) (int, error) {
	if current, err := os.ReadFile(path); err == nil && bytes.Equal(current, content) {
		return 0, nil
	}

	if err := os.WriteFile(path, content, 0o666); err != nil {
		return 0, streamWarning("file_put_contents", path, err)
	}

	return len(content), nil
}

// SafeCopy ports Filesystem::safeCopy: copies source over target unless
// both exist with equal contents, then gives target source's mtime and
// atime in whole seconds, as touch() does.
func SafeCopy(source, target string) error {
	if fileExists(target) && fileExists(source) {
		equal, err := filesAreEqual(source, target)
		if err != nil {
			return err
		}

		if equal {
			return nil
		}
	}

	src, err := os.Open(source)
	if err != nil {
		return streamWarning("fopen", source, err)
	}
	defer src.Close()

	dst, err := os.OpenFile(target, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o666) //nolint:gosec // PHP's "w+" mode; the umask applies.
	if err != nil {
		return streamWarning("fopen", target, err)
	}

	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()

		return warning("stream_copy_to_stream", source, err)
	}

	if err := dst.Close(); err != nil {
		return warning("fclose", target, err)
	}

	fi, err := src.Stat()
	if err != nil {
		return warning("filemtime", source, err)
	}

	mtime := time.Unix(fi.ModTime().Unix(), 0)
	atime := time.Unix(fileAtime(fi).Unix(), 0)

	if err := os.Chtimes(target, atime, mtime); err != nil {
		return warning("touch", target, err)
	}

	return nil
}

// filesAreEqual compares two files by size, then content in 8KiB chunks.
func filesAreEqual(a, b string) (bool, error) {
	fa, err := os.Open(a)
	if err != nil {
		return false, streamWarning("fopen", a, err)
	}
	defer fa.Close()

	fb, err := os.Open(b)
	if err != nil {
		return false, streamWarning("fopen", b, err)
	}
	defer fb.Close()

	ia, err := fa.Stat()
	if err != nil {
		return false, warning("filesize", a, err)
	}

	ib, err := fb.Stat()
	if err != nil {
		return false, warning("filesize", b, err)
	}

	if ia.Size() != ib.Size() {
		return false, nil
	}

	var bufA, bufB [8192]byte

	for {
		na, errA := io.ReadFull(fa, bufA[:])
		nb, errB := io.ReadFull(fb, bufB[:])

		if !bytes.Equal(bufA[:na], bufB[:nb]) {
			return false, nil
		}

		if errA != nil || errB != nil {
			return true, nil
		}
	}
}
