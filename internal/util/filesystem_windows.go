//go:build windows

package util

import (
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/stubbedev/maestro/internal/php"
)

// dirSeparators are '/' and DIRECTORY_SEPARATOR.
const dirSeparators = `/\`

// unlinkPath is PHP's unlink() on Windows.
func unlinkPath(path string) error {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}

	return syscall.DeleteFile(p)
}

func rmdirPath(path string) error {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}

	return syscall.RemoveDirectory(p)
}

// isReadableAccess is PHP's is_readable on Windows: the file exists.
func isReadableAccess(path string) bool {
	return php.FileExists(path)
}

// isExecutable approximates PHP's is_executable on Windows, which asks
// GetBinaryType: an existing .exe or .com file.
func isExecutable(path string) bool {
	if !php.IsFile(path) {
		return false
	}

	ext := php.Strtolower(path[strings.LastIndexByte(path, '.')+1:])

	return ext == "exe" || ext == "com"
}

func chownLike(string, os.FileInfo) error { return nil }

func fileAtime(fi os.FileInfo) time.Time {
	if d, ok := fi.Sys().(*syscall.Win32FileAttributeData); ok {
		return time.Unix(0, d.LastAccessTime.Nanoseconds())
	}

	return fi.ModTime()
}

// Junction ports Filesystem::junction: creates an NTFS junction at junction
// pointing to target.
func (fs *Filesystem) Junction(target, junction string) error {
	if !php.IsDir(target) {
		return &IOError{Message: "Cannot junction to \"" + target + "\" as it is not a directory.", Path: target}
	}

	// Removing any previous junction to ensure clean execution.
	if !php.IsDir(junction) || IsJunction(junction) {
		_ = rmdirPath(junction)
	}

	cmd := Cmd("mklink", "/J", strings.ReplaceAll(junction, "/", `\`), Realpath(target))
	if code, err := fs.process().Execute(cmd, new(string), ""); err != nil || code != 0 {
		if err != nil {
			return err
		}

		return &IOError{Message: "Failed to create junction to \"" + target + "\" at \"" + junction + "\".", Path: target}
	}

	return nil
}

// IsJunction ports Filesystem::isJunction: a directory that is no symlink
// but whose own lstat mode is not a directory. Go reports junctions (mount
// point reparse points) as irregular files.
func IsJunction(junction string) bool {
	if !php.IsDir(junction) || isLink(junction) {
		return false
	}

	fi, err := os.Lstat(junction)

	return err == nil && !fi.IsDir()
}

// RemoveJunction ports Filesystem::removeJunction.
func RemoveJunction(junction string) (bool, error) {
	junction = strings.TrimRight(strings.ReplaceAll(junction, "/", `\`), `\`)
	if !IsJunction(junction) {
		return false, &IOError{Message: junction + " is not a junction and thus cannot be removed as one"}
	}

	if err := Rmdir(junction); err != nil {
		return false, err
	}

	return true, nil
}

// IsWritable is PHP's is_writable() on Windows: a file without the
// read-only attribute (directories are always writable for PHP).
func IsWritable(path string) bool {
	fi, err := os.Stat(path)

	return err == nil && (fi.IsDir() || fi.Mode().Perm()&0o200 != 0)
}
