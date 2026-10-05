//go:build windows

package cache

import (
	"os"

	"golang.org/x/sys/windows"

	"github.com/stubbedev/maestro/internal/php"
)

// isWritable is is_writable(): on Windows, a file without the read-only
// attribute (directories are always writable for PHP).
func isWritable(path string) bool {
	fi, err := os.Stat(path)

	return err == nil && (fi.IsDir() || fi.Mode().Perm()&0o200 != 0)
}

// diskFreeSpace is disk_free_space($dir) as PHP prints the float, or
// "unknown".
func diskFreeSpace(dir string) string {
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return "unknown"
	}

	var free uint64
	if err := windows.GetDiskFreeSpaceEx(p, &free, nil, nil); err != nil {
		return "unknown"
	}

	return php.FloatToString(float64(free))
}
