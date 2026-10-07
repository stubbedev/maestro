package fsstate

import "golang.org/x/sys/unix"

// devOf is st_dev, an int32 here: as an unsigned 32-bit number, not sign
// extended.
func devOf(st *unix.Stat_t) uint64 { return uint64(uint32(st.Dev)) }
