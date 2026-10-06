//go:build !unix

package vcs

import "os"

// ownedByCurrentUser: git's ownership check is not reproduced here, so
// git is always asked.
func ownedByCurrentUser(os.FileInfo) bool { return false }
