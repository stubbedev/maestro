//go:build unix

package cache

import (
	"golang.org/x/sys/unix"

	"github.com/stubbedev/maestro/internal/php"
)

// isWritable is is_writable(): access(2) with W_OK.
func isWritable(path string) bool {
	return unix.Access(path, unix.W_OK) == nil
}

// diskFreeSpace is disk_free_space($dir) as PHP prints the float, or
// "unknown".
func diskFreeSpace(dir string) string {
	var st unix.Statfs_t
	if err := unix.Statfs(dir, &st); err != nil {
		return "unknown"
	}

	return php.FloatToString(float64(st.Bavail) * float64(st.Bsize))
}
