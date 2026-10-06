// Ports nothing: see versioncache.go.

package vcs

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
)

// gitBinaryKey names the kept version of the git binary found: the path
// found, the file it resolves to and that file's identity. "" when its
// version is not kept: nothing found, a file not named git (a multi-call
// binary, such as /usr/bin/snap behind /snap/bin/git, picks what it runs
// by its name), a script, or a file changed after limit.
func gitBinaryKey(found string, limit time.Time) string {
	if found == "" {
		return ""
	}
	resolved, err := filepath.EvalSymlinks(found)
	if err != nil || filepath.Base(resolved) != "git" {
		return ""
	}
	f, err := os.Open(resolved)
	if err != nil {
		return ""
	}
	defer f.Close()
	head := make([]byte, 2)
	if _, err := io.ReadFull(f, head); err != nil || string(head) == "#!" {
		return ""
	}
	var st unix.Stat_t
	if unix.Fstat(int(f.Fd()), &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFREG ||
		!time.Unix(st.Mtim.Unix()).Before(limit) || !time.Unix(st.Ctim.Unix()).Before(limit) {
		return ""
	}

	h := sha256.New()
	for _, s := range [...]string{versionCacheHeader, found, resolved} {
		h.Write(binary.AppendUvarint(nil, uint64(len(s))))
		h.Write([]byte(s))
	}
	for _, v := range [...]uint64{
		st.Dev, st.Ino, uint64(st.Mode), uint64(st.Size), //nolint:gosec // never negative
		uint64(st.Mtim.Nano()), uint64(st.Ctim.Nano()), //nolint:gosec // an identity, not a quantity
	} {
		h.Write(binary.AppendUvarint(nil, v))
	}

	return hex.EncodeToString(h.Sum(nil)[:16])
}
