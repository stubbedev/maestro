//go:build !(linux || darwin)

package classmap

import "os"

// statKey: no file identities here, so nothing is cached.
func statKey(string) (fileKey, bool) { return fileKey{}, false }

func fstatKey(*os.File) (fileKey, bool) { return fileKey{}, false }

// statStamp is the size and modification second of the regular file at
// path (symlinks followed) when its modification time has no fraction of
// a second: what a release file's stamp is compared with.
func statStamp(path string) (size, mtime int64, ok bool) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.ModTime().Nanosecond() != 0 {
		return 0, 0, false
	}

	return info.Size(), info.ModTime().Unix(), true
}
