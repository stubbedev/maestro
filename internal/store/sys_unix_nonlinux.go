//go:build unix && !linux

package store

import (
	"os"

	"golang.org/x/sys/unix"
)

// setMtime gives an open file the modification time t (whole seconds).
func setMtime(f *os.File, t int64) error {
	tv := []unix.Timeval{unix.NsecToTimeval(t * 1e9), unix.NsecToTimeval(t * 1e9)}

	return unix.Futimes(int(f.Fd()), tv)
}
