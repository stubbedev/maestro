//go:build unix && !darwin

package fsstate

import "golang.org/x/sys/unix"

func devOf(st *unix.Stat_t) uint64 { return uint64(st.Dev) } //nolint:unconvert // uint32 on some systems
