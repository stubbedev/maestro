//go:build windows

package cache

import (
	"golang.org/x/sys/windows"

	"github.com/stubbedev/maestro/internal/php"
)

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
