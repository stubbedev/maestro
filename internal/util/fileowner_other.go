//go:build !unix

package util

import "os"

// fileOwner is the owner and group PHP's stat() reports where the platform
// has neither: 0 for every file, so a file's ownership is always kept.
func fileOwner(os.FileInfo) (uid, gid int, ok bool) { return 0, 0, true }
