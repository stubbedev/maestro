package classmap

import "golang.org/x/sys/unix"

func keyOf(st *unix.Stat_t) fileKey {
	return fileKey{
		dev: uint64(uint32(st.Dev)), ino: st.Ino, size: st.Size,
		mtimeSec: st.Mtim.Sec, mtimeNsec: st.Mtim.Nsec,
		ctimeSec: st.Ctim.Sec, ctimeNsec: st.Ctim.Nsec,
	}
}
