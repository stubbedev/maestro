package vcs

import (
	"encoding/binary"

	"golang.org/x/sys/unix"
)

// statEntry writes path's lstat data into an index entry's stat fields
// as git's fill_stat_data does: every field truncated to 32 bits.
func statEntry(path string, e []byte) error {
	var st unix.Stat_t
	if err := unix.Lstat(path, &st); err != nil {
		return errIndexFallback
	}

	for i, v := range []uint64{
		uint64(st.Ctim.Sec), uint64(st.Ctim.Nsec), //nolint:gosec // truncated as git does.
		uint64(st.Mtim.Sec), uint64(st.Mtim.Nsec), //nolint:gosec // as above.
		st.Dev, st.Ino,
	} {
		binary.BigEndian.PutUint32(e[4*i:], uint32(v)) //nolint:gosec // as above.
	}

	binary.BigEndian.PutUint32(e[28:], st.Uid)
	binary.BigEndian.PutUint32(e[32:], st.Gid)
	binary.BigEndian.PutUint32(e[36:], uint32(st.Size)) //nolint:gosec // as above.

	return nil
}
