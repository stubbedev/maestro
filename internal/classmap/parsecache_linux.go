package classmap

import "golang.org/x/sys/unix"

func keyOf(st *unix.Stat_t) fileKey {
	return fileKey{
		dev: st.Dev, ino: st.Ino, size: st.Size,
		mtimeSec: widen(st.Mtim.Sec), mtimeNsec: widen(st.Mtim.Nsec),
		ctimeSec: widen(st.Ctim.Sec), ctimeNsec: widen(st.Ctim.Nsec),
	}
}

// widen converts the time fields, int32 on 32-bit platforms.
func widen[T ~int32 | ~int64](v T) int64 { return int64(v) }
