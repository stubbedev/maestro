//go:build unix

package config

import "golang.org/x/sys/unix"

// isWritable is PHP's is_writable: access(2) with W_OK.
func isWritable(path string) bool {
	return unix.Access(path, unix.W_OK) == nil
}
