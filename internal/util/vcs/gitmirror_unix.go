//go:build unix

package vcs

import (
	"os"
	"syscall"
)

// ownedByCurrentUser is git's ownership check of a repository directory
// (is_path_owned_by_current_uid), conservatively: root, which git
// compares with SUDO_UID, is not handled.
func ownedByCurrentUser(info os.FileInfo) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	euid := os.Geteuid()

	return ok && euid != 0 && int(st.Uid) == euid
}
