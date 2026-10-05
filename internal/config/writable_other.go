//go:build !unix

package config

import "os"

// isWritable is PHP's is_writable on Windows: the file exists and is not
// read-only.
func isWritable(path string) bool {
	fi, err := os.Stat(path)

	return err == nil && fi.Mode().Perm()&0o200 != 0
}
