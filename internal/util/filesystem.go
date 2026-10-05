// Ports src/Composer/Util/Filesystem.php.

package util

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
	"time"
)

// Filesystem ports Composer\Util\Filesystem. Its pure path helpers
// (NormalizePath, FindShortestPath, ...) are package functions; the methods
// are the operations that may shell out through the ProcessExecutor.
type Filesystem struct {
	executor *ProcessExecutor
}

// NewFilesystem returns a Filesystem running external commands through
// executor, or a fresh ProcessExecutor when nil.
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
		return true, fs.Unlink(file)
	}

	return false, nil
}

// IsDirEmpty ports Filesystem::isDirEmpty, counting dot files and VCS
// directories too.
func IsDirEmpty(dir string) (bool, error) {
	f, err := os.Open(dir)
	if err != nil {
		return false, finderDirError(dir)
	}
	defer f.Close()

	names, err := f.Readdirnames(1)
	if errors.Is(err, io.EOF) {
		return true, nil
	}

	if err != nil {
		return false, finderDirError(dir)
	}

	return len(names) == 0, nil
}

// finderDirError is the exception Symfony Finder::in() throws for a path
// that is not a directory.
func finderDirError(dir string) error {
	return fmt.Errorf("The %q directory does not exist.", dir) //nolint:revive,staticcheck // Symfony's message.
}

// EmptyDirectory ports Filesystem::emptyDirectory.
func (fs *Filesystem) EmptyDirectory(dir string, ensureDirectoryExists bool) error {
	if isLink(dir) && fileExists(dir) {
		if err := fs.Unlink(dir); err != nil {
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

	names, err := readDirNames(dir)
	if err != nil {
		return finderDirError(dir)
	}

	for _, name := range names {
		if _, err := fs.Remove(dir + "/" + name); err != nil {
			return err
		}
	}

	return nil
}

func readDirNames(dir string) ([]string, error) {
	f, err := os.Open(dir)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	return f.Readdirnames(-1)
}

// RemoveDirectory ports Filesystem::removeDirectory: recursively removes a
// directory with rm -rf (rmdir /S /Q on Windows), falling back on
// RemoveDirectoryPhp.
func (fs *Filesystem) RemoveDirectory(directory string) (bool, error) {
	if result, done, err := fs.removeEdgeCases(directory); done || err != nil {
		return result, err
	}

	code, err := fs.process().Execute(removeDirectoryCommand(directory), new(string), "")
	if err != nil {
		return false, err
	}

	if code == 0 && !isDir(directory) {
		return true, nil
	}

	return fs.RemoveDirectoryPhp(directory)
}

// RemoveDirectoryAsync ports Filesystem::removeDirectoryAsync. The executor
// must have async enabled.
func (fs *Filesystem) RemoveDirectoryAsync(directory string) (*Promise[bool], error) {
	if result, done, err := fs.removeEdgeCases(directory); done || err != nil {
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

		return fs.RemoveDirectoryPhp(directory)
	}), nil
}

func removeDirectoryCommand(directory string) Command {
	if IsWindows() {
		return Cmd("rmdir", "/S", "/Q", Realpath(directory))
	}

	return Cmd("rm", "-rf", directory)
}

// removeEdgeCases ports Filesystem::removeEdgeCases; done reports whether an
// edge case was hit, result is then whether removal succeeded.
func (fs *Filesystem) removeEdgeCases(directory string) (result, done bool, err error) {
	if IsSymlinkedDirectory(directory) {
		return true, true, fs.Unlink(resolveSymlinkedDirectorySymlink(directory))
	}

	if IsJunction(directory) {
		result, err := fs.RemoveJunction(directory)

		return result, true, err
	}

	if isLink(directory) {
		if err := unlinkPath(directory); err != nil {
			return false, true, errors.New(phpWarning("unlink", directory, err))
		}

		return true, true, nil
	}

	if !isDir(directory) {
		return true, true, nil
	}

	if isRootPath(directory) {
		return false, true, errors.New("Aborting an attempted deletion of " + directory + ", this was probably not intended, if it is a real use case please report it.")
	}

	return false, false, nil
}

// isRootPath matches {^(?:[a-z]:)?[/\\]+$}i.
func isRootPath(path string) bool {
	if len(path) >= 2 && isASCIIAlpha(path[0]) && path[1] == ':' {
		path = path[2:]
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
func (fs *Filesystem) RemoveDirectoryPhp(directory string) (bool, error) {
	if result, done, err := fs.removeEdgeCases(directory); done || err != nil {
		return result, err
	}

	names, err := readDirNames(directory)
	if err != nil {
		// Retry once, it sometimes fails without apparent reason, see
		// https://github.com/composer/composer/issues/4009
		time.Sleep(100 * time.Millisecond)

		if !isDir(directory) {
			return true, nil
		}

		if names, err = readDirNames(directory); err != nil {
			return false, fmt.Errorf("RecursiveDirectoryIterator::__construct(%s): Failed to open directory: %s", directory, strerror(err))
		}
	}

	if err := fs.removeChildren(directory, names); err != nil {
		return false, err
	}

	return true, fs.Rmdir(directory)
}

// removeChildren deletes the entries of dir child first, without following
// symlinks to directories, as a CHILD_FIRST RecursiveIteratorIterator does.
func (fs *Filesystem) removeChildren(dir string, names []string) error {
	for _, name := range names {
		path := dir + "/" + name

		fi, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("Could not delete %s: %s", path, strerror(err)) //nolint:revive,staticcheck // Composer's message.
		}

		if !fi.IsDir() {
			// A symlink to a directory is a directory to SplFileInfo::isDir.
			if fi.Mode()&os.ModeSymlink != 0 && isDir(path) {
				if err := fs.Rmdir(path); err != nil {
					return err
				}

				continue
			}

			if err := fs.Unlink(path); err != nil {
				return err
			}

			continue
		}

		children, err := readDirNames(path)
		if err != nil {
			return fmt.Errorf("RecursiveDirectoryIterator::__construct(%s): Failed to open directory: %s", path, strerror(err))
		}

		if err := fs.removeChildren(path, children); err != nil {
			return err
		}

		if err := fs.Rmdir(path); err != nil {
			return err
		}
	}

	return nil
}

// EnsureDirectoryExists ports Filesystem::ensureDirectoryExists.
func EnsureDirectoryExists(directory string) error {
	if isDir(directory) {
		return nil
	}

	if fileExists(directory) {
		return errors.New(directory + " exists and is not a directory.")
	}

	if isLink(directory) {
		if err := unlinkImplementation(directory); err != nil {
			return errors.New("Could not delete symbolic link " + directory + ": " + phpWarning("unlink", directory, err))
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

	failure := errors.New(directory + " does not exist and could not be created: mkdir(): " + strerror(err))

	// In pathological cases with paths like path/to/broken-symlink/../foo
	// is_dir fails to detect path/to/foo, but normalizing the ../ away first
	// works, see https://github.com/composer/composer/issues/11864
	if normalized := NormalizePath(directory); normalized != directory {
		if EnsureDirectoryExists(normalized) == nil {
			return nil
		}
	}

	return failure
}

// windowsLockHint is appended to deletion errors on Windows.
const windowsLockHint = "\nThis can be due to an antivirus or the Windows Search Indexer locking the file while they are analyzed"

// Unlink ports Filesystem::unlink, retrying once after 350ms on Windows.
func (fs *Filesystem) Unlink(path string) error {
	return retryDelete(path, "unlink", unlinkImplementation)
}

// Rmdir ports Filesystem::rmdir, retrying once after 350ms on Windows.
func (fs *Filesystem) Rmdir(path string) error {
	return retryDelete(path, "rmdir", rmdirPath)
}

func retryDelete(path, fn string, remove func(string) error) error {
	err := remove(path)
	if err == nil {
		return nil
	}

	// Retry after a bit on Windows since it tends to be touchy with mass
	// removals.
	if IsWindows() {
		time.Sleep(350 * time.Millisecond)

		if err = remove(path); err == nil {
			return nil
		}
	}

	message := "Could not delete " + path + ": " + phpWarning(fn, path, err)
	if IsWindows() {
		message += windowsLockHint
	}

	return errors.New(message)
}

// unlinkImplementation removes a symlink or file; directory symlinks on
// Windows need rmdir instead of unlink.
func unlinkImplementation(path string) error {
	if IsWindows() && isDir(path) && isLink(path) {
		return rmdirPath(path)
	}

	return unlinkPath(path)
}

// CopyThenRemove ports Filesystem::copyThenRemove, a non-atomic rename.
func (fs *Filesystem) CopyThenRemove(source, target string) error {
	if _, err := fs.Copy(source, target); err != nil {
		return err
	}

	if !isDir(source) {
		return fs.Unlink(source)
	}

	_, err := fs.RemoveDirectoryPhp(source)

	return err
}

// Copy ports Filesystem::copy: a file, or a directory tree (contents only,
// new files and directories get default permissions).
func (fs *Filesystem) Copy(source, target string) (bool, error) {
	// Refs https://github.com/composer/composer/issues/11864
	target = NormalizePath(target)

	if !isDir(source) {
		if err := phpCopy(source, target); err != nil {
			return false, err
		}

		return true, nil
	}

	if err := EnsureDirectoryExists(target); err != nil {
		return false, err
	}

	// PHP's copy() only returns false along with a warning, which Composer's
	// error handler turns into an exception, so the result is true or an
	// error.
	if err := copyTree(source, target, ""); err != nil {
		return false, err
	}

	return true, nil
}

// copyTree walks source self first, recursing only into real directories
// (not symlinks) as RecursiveDirectoryIterator does.
func copyTree(source, target, sub string) error {
	dir := source
	if sub != "" {
		dir += "/" + sub
	}

	f, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("RecursiveDirectoryIterator::__construct(%s): Failed to open directory: %s", dir, strerror(err))
	}

	entries, err := f.ReadDir(-1)
	f.Close()

	if err != nil {
		return fmt.Errorf("RecursiveDirectoryIterator::__construct(%s): Failed to open directory: %s", dir, strerror(err))
	}

	for _, entry := range entries {
		subPath := entry.Name()
		if sub != "" {
			subPath = sub + string(os.PathSeparator) + entry.Name()
		}

		path := dir + "/" + entry.Name()
		targetPath := target + string(os.PathSeparator) + subPath

		switch {
		case entry.IsDir():
			if err := EnsureDirectoryExists(targetPath); err != nil {
				return err
			}

			if err := copyTree(source, target, subPath); err != nil {
				return err
			}
		case entry.Type()&os.ModeSymlink != 0 && isDir(path):
			if err := EnsureDirectoryExists(targetPath); err != nil {
				return err
			}
		default:
			if err := phpCopy(path, targetPath); err != nil {
				return err
			}
		}
	}

	return nil
}

// phpCopy ports PHP's copy(): the contents of source (following symlinks)
// into target, created with default permissions or truncated.
func phpCopy(source, target string) error {
	src, err := os.Open(source)
	if err != nil {
		return errors.New(phpWarning("copy", source, fmt.Errorf("Failed to open stream: %s", strerror(err)))) //nolint:revive,staticcheck // PHP's message.
	}
	defer src.Close()

	srcInfo, err := src.Stat()
	if err != nil {
		return errors.New(phpWarning("copy", source, err))
	}

	if srcInfo.IsDir() {
		return errors.New("copy(): The first argument to copy() function cannot be a directory")
	}

	if dstInfo, err := os.Stat(target); err == nil {
		if dstInfo.IsDir() {
			return errors.New("copy(): The second argument to copy() function cannot be a directory")
		}

		if os.SameFile(srcInfo, dstInfo) {
			return nil
		}
	}

	dst, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o666)
	if err != nil {
		return errors.New(phpWarning("copy", target, fmt.Errorf("Failed to open stream: %s", strerror(err)))) //nolint:revive,staticcheck // PHP's message.
	}

	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()

		return errors.New(phpWarning("copy", source, err))
	}

	return dst.Close()
}

// Rename ports Filesystem::rename: rename(2), then mv (xcopy on Windows),
// then copy and remove.
func (fs *Filesystem) Rename(source, target string) error {
	if phpRename(source, target) == nil {
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

	return fs.CopyThenRemove(source, target)
}

// phpRename ports PHP's rename() for local files, which moves a file (not
// a directory) across devices by copying it with its mode and owner.
func phpRename(source, target string) error {
	err := os.Rename(source, target)
	if err == nil || !errors.Is(err, syscall.EXDEV) {
		return err
	}

	fi, statErr := os.Stat(source)
	if statErr != nil || fi.IsDir() {
		return err
	}

	if err := phpCopy(source, target); err != nil {
		return err
	}

	_ = os.Chmod(target, fi.Mode().Perm())
	chownLike(target, fi)

	return os.Remove(source)
}

// FindShortestPath ports Filesystem::findShortestPath: the shortest path
// from from to to, both absolute; with directories they are directories.
func FindShortestPath(from, to string, directories, preferRelative bool) (string, error) {
	return findShortestPath(from, to, directories, preferRelative, IsWindows())
}

func findShortestPath(from, to string, directories, preferRelative, windows bool) (string, error) {
	if !IsAbsolutePath(from) || !IsAbsolutePath(to) {
		return "", fmt.Errorf("$from (%s) and $to (%s) must be absolute paths.", from, to) //nolint:revive,staticcheck // Composer's message.
	}

	from = NormalizePath(from)
	to = NormalizePath(to)

	if directories {
		from = strings.TrimRight(from, "/") + "/dummy_file"
	}

	if phpDirname(from, windows) == phpDirname(to, windows) {
		return "./" + phpBasename(to, windows), nil
	}

	commonPath := findCommonPath(from, to, windows, false)

	// No commonality at all.
	if !strings.HasPrefix(from, commonPath) {
		return to, nil
	}

	commonPath = strings.TrimRight(commonPath, "/") + "/"
	sourcePathDepth := strings.Count(substrFrom(from, len(commonPath)), "/")
	commonPathCode := strings.Repeat("../", sourcePathDepth)

	// Allow top level /foo & /bar dirs to be addressed relatively as this is
	// common in Docker setups.
	if !preferRelative && commonPath == "/" && sourcePathDepth > 1 {
		return to, nil
	}

	result := commonPathCode + substrFrom(to, len(commonPath))
	if result == "" {
		return "./", nil
	}

	return result, nil
}

// FindShortestPathCode ports Filesystem::findShortestPathCode: PHP code that,
// run in from, evaluates to the path of to.
func FindShortestPathCode(from, to string, directories, staticCode, preferRelative bool) (string, error) {
	return findShortestPathCode(from, to, directories, staticCode, preferRelative, IsWindows())
}

func findShortestPathCode(from, to string, directories, staticCode, preferRelative, windows bool) (string, error) {
	if !IsAbsolutePath(from) || !IsAbsolutePath(to) {
		return "", fmt.Errorf("$from (%s) and $to (%s) must be absolute paths.", from, to) //nolint:revive,staticcheck // Composer's message.
	}

	from = NormalizePath(from)
	to = NormalizePath(to)

	if from == to {
		if directories {
			return "__DIR__", nil
		}

		return "__FILE__", nil
	}

	commonPath := findCommonPath(from, to, windows, true)

	// No commonality at all.
	if !strings.HasPrefix(from, commonPath) || commonPath == "." {
		return varExportString(to), nil
	}

	commonPath = strings.TrimRight(commonPath, "/") + "/"
	if strings.HasPrefix(to, from+"/") {
		return "__DIR__ . " + varExportString(substrFrom(to, len(from))), nil
	}

	sourcePathDepth := strings.Count(substrFrom(from, len(commonPath)), "/")
	if directories {
		sourcePathDepth++
	}

	// Allow top level /foo & /bar dirs to be addressed relatively as this is
	// common in Docker setups.
	if !preferRelative && commonPath == "/" && sourcePathDepth > 1 {
		return varExportString(to), nil
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

	return commonPathCode + "." + varExportString("/"+relTarget), nil
}

// findCommonPath walks up from to until it is a path prefix of from, the
// root or a drive root (or "." with stopAtDot). A path whose dirname is
// itself would loop forever in PHP; it stops there instead.
func findCommonPath(from, to string, windows, stopAtDot bool) string {
	commonPath := to
	for !strings.HasPrefix(from+"/", commonPath+"/") && commonPath != "/" && !isDriveRoot(commonPath) && (!stopAtDot || commonPath != ".") {
		parent := strings.ReplaceAll(phpDirname(commonPath, windows), `\`, "/")
		if parent == commonPath {
			break
		}

		commonPath = parent
	}

	return commonPath
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

// IsAbsolutePath ports Filesystem::isAbsolutePath.
func IsAbsolutePath(path string) bool {
	return strings.HasPrefix(path, "/") || (len(path) > 1 && path[1] == ':') || strings.HasPrefix(path, `\\`)
}

// Size ports Filesystem::size: the size of a file, or of all files below a
// directory.
func Size(path string) (int64, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return 0, errors.New(path + " does not exist.")
	}

	if fi.IsDir() {
		return directorySize(path)
	}

	return fi.Size(), nil
}

// directorySize sums the regular files (following symlinks) below dir,
// recursing into real directories only.
func directorySize(dir string) (int64, error) {
	f, err := os.Open(dir)
	if err != nil {
		return 0, fmt.Errorf("RecursiveDirectoryIterator::__construct(%s): Failed to open directory: %s", dir, strerror(err))
	}

	entries, err := f.ReadDir(-1)
	f.Close()

	if err != nil {
		return 0, fmt.Errorf("RecursiveDirectoryIterator::__construct(%s): Failed to open directory: %s", dir, strerror(err))
	}

	var size int64

	for _, entry := range entries {
		path := dir + "/" + entry.Name()

		switch {
		case entry.IsDir():
			n, err := directorySize(path)
			if err != nil {
				return 0, err
			}

			size += n
		case entry.Type().IsRegular():
			fi, err := entry.Info()
			if err == nil {
				size += fi.Size()
			}
		case entry.Type()&os.ModeSymlink != 0:
			if fi, err := os.Stat(path); err == nil && fi.Mode().IsRegular() {
				size += fi.Size()
			}
		}
	}

	return size, nil
}

// NormalizePath ports Filesystem::normalizePath: backslashes become slashes,
// redundant separators and up-level references collapse, the trailing slash
// goes, and a drive letter is uppercased.
func NormalizePath(path string) string {
	path = strings.ReplaceAll(path, `\`, "/")
	absolute := ""

	// Extract Windows UNC paths e.g. \\foo\bar
	if len(path) > 2 && strings.HasPrefix(path, "//") {
		absolute = "//"
		path = path[2:]
	}

	// Extract a prefix being a protocol://, protocol:, protocol://drive: or
	// simply drive:
	prefix := normalizePrefix(path)
	path = path[len(prefix):]

	if strings.HasPrefix(path, "/") {
		absolute = "/"
		path = path[1:]
	}

	parts := make([]string, 0, strings.Count(path, "/")+1)
	up := false

	for chunk := range strings.SplitSeq(path, "/") {
		switch {
		case chunk == ".." && (absolute != "" || up):
			if len(parts) > 0 {
				parts = parts[:len(parts)-1]
			}

			up = !(len(parts) == 0 || parts[len(parts)-1] == "..")
		case chunk != "." && chunk != "":
			parts = append(parts, chunk)
			up = chunk != ".."
		}
	}

	// Ensure c: is normalized to C:
	if n := len(prefix); n >= 2 && prefix[n-1] == ':' && isASCIIAlpha(prefix[n-2]) && (n == 2 || strings.HasSuffix(prefix[:n-2], "://")) {
		prefix = prefix[:n-2] + strings.ToUpper(prefix[n-2:])
	}

	return prefix + absolute + strings.Join(parts, "/")
}

// normalizePrefix matches {^( [0-9a-z]{2,}+: (?: // (?: [a-z]: )? )? | [a-z]: )}ix.
func normalizePrefix(path string) string {
	n := 0
	for n < len(path) && isASCIIAlnum(path[n]) {
		n++
	}

	if n >= 2 && n < len(path) && path[n] == ':' {
		end := n + 1
		if strings.HasPrefix(path[end:], "//") {
			end += 2
			if end+1 < len(path) && isASCIIAlpha(path[end]) && path[end+1] == ':' {
				end += 2
			}
		}

		return path[:end]
	}

	if len(path) >= 2 && isASCIIAlpha(path[0]) && path[1] == ':' {
		return path[:2]
	}

	return ""
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

	if hasPrefixFold(path, "file://") {
		if !windows || !strings.HasPrefix(path[7:], "//") {
			return true
		}
	}

	if strings.HasPrefix(path, "/") && (!windows || !strings.HasPrefix(path[1:], "/")) {
		return true
	}

	drive := 0
	if strings.HasPrefix(path, "/") {
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
	if windows && hasPrefixFold(path, "file:///") && len(path) > 8 && isASCIIAlpha(path[8]) {
		rest := path[9:]
		rest = strings.TrimPrefix(rest, ":")

		if strings.HasPrefix(rest, "/") {
			path = "file://" + path[8:9] + ":/" + rest[1:]
		}
	}

	if hasPrefixFold(path, "file://") {
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
		_, err = f.Read(make([]byte, 1))

		return err == nil || errors.Is(err, io.EOF)
	}

	return true
}

// RelativeSymlink ports Filesystem::relativeSymlink: a symlink at link to
// target, by the shortest relative path. PHP chdirs to the link's directory
// around symlink(); with an absolute link that changes nothing, so the
// process-wide working directory is left alone.
func (fs *Filesystem) RelativeSymlink(target, link string) (bool, error) {
	relativePath, err := FindShortestPath(link, target, false, false)
	if err != nil {
		return false, err
	}

	if dir := phpDirname(link, IsWindows()); !isDir(dir) {
		return false, fmt.Errorf("chdir(): %s (errno %d)", strerror(syscall.ENOENT), syscall.ENOENT)
	}

	return os.Symlink(relativePath, link) == nil, nil
}

// IsSymlinkedDirectory ports Filesystem::isSymlinkedDirectory.
func IsSymlinkedDirectory(directory string) bool {
	if !isDir(directory) {
		return false
	}

	return isLink(resolveSymlinkedDirectorySymlink(directory))
}

// resolveSymlinkedDirectorySymlink strips trailing slashes from a directory
// path so it names the symlink rather than its target.
func resolveSymlinkedDirectorySymlink(pathname string) string {
	if !isDir(pathname) {
		return pathname
	}

	if resolved := strings.TrimRight(pathname, "/"); resolved != "" {
		return resolved
	}

	return pathname
}

// IOError is Symfony's Filesystem IOException, carrying the path involved.
type IOError struct {
	Message string
	Path    string
}

func (e *IOError) Error() string { return e.Message }

// errJunctionUnsupported is the LogicException Filesystem::junction throws
// off Windows.
var errJunctionUnsupported = errors.New(`Function Composer\Util\Filesystem is not available on non-Windows platform`)

// FilePutContentsIfModified ports Filesystem::filePutContentsIfModified: it
// writes only when the content differs and returns the bytes written.
func FilePutContentsIfModified(path string, content []byte) (int, error) {
	current, err := os.ReadFile(path)
	if err == nil && bytes.Equal(current, content) {
		return 0, nil
	}

	if err := os.WriteFile(path, content, 0o666); err != nil {
		return 0, errors.New(phpWarning("file_put_contents", path, fmt.Errorf("Failed to open stream: %s", strerror(err)))) //nolint:revive,staticcheck // PHP's message.
	}

	return len(content), nil
}

// SafeCopy ports Filesystem::safeCopy: copies source over target unless
// both exist with equal contents, then gives target source's mtime and
// atime.
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
		return errors.New(phpWarning("fopen", source, fmt.Errorf("Failed to open stream: %s", strerror(err)))) //nolint:revive,staticcheck // PHP's message.
	}
	defer src.Close()

	dst, err := os.OpenFile(target, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o666)
	if err != nil {
		return errors.New(phpWarning("fopen", target, fmt.Errorf("Failed to open stream: %s", strerror(err)))) //nolint:revive,staticcheck // PHP's message.
	}

	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()

		return err
	}

	if err := dst.Close(); err != nil {
		return err
	}

	fi, err := src.Stat()
	if err != nil {
		return err
	}

	return os.Chtimes(target, fileAtime(fi), fi.ModTime())
}

// filesAreEqual compares two files by size, then content.
func filesAreEqual(a, b string) (bool, error) {
	fa, err := os.Open(a)
	if err != nil {
		return false, err
	}
	defer fa.Close()

	fb, err := os.Open(b)
	if err != nil {
		return false, err
	}
	defer fb.Close()

	ia, err := fa.Stat()
	if err != nil {
		return false, err
	}

	ib, err := fb.Stat()
	if err != nil {
		return false, err
	}

	if ia.Size() != ib.Size() {
		return false, nil
	}

	bufA := make([]byte, 8192)
	bufB := make([]byte, 8192)

	for {
		na, errA := io.ReadFull(fa, bufA)
		nb, errB := io.ReadFull(fb, bufB)

		if !bytes.Equal(bufA[:na], bufB[:nb]) {
			return false, nil
		}

		if errA != nil || errB != nil {
			return true, nil
		}
	}
}
