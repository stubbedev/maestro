package store

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/stubbedev/maestro/internal/util/fsstate"
)

// stampBase is 2000-01-01T00:00:00Z: object modification times lie in the
// eight and a half years after it.
const stampBase = 946684800

// stampTime is the modification time an object with content hash sum
// carries: derived from the hash, so it needs no record and differs
// between contents.
func stampTime(sum *[32]byte) int64 {
	return stampBase + int64(binary.BigEndian.Uint32(sum[:4])>>4)
}

// errStale means an object no longer matches its stamp.
var errStale = errors.New("store object modified")

// objectSuffix names the mode in an object's file name; 644 goes without.
func objectSuffix(perm fs.FileMode) string {
	if perm == 0o644 {
		return ""
	}

	return "-" + strconv.FormatUint(uint64(perm), 8)
}

// objectPerm is the mode of the object behind a file with permission bits
// perm: the same, except that the store can always read its objects. A
// file whose owner may not read it is copied, never linked.
func objectPerm(perm fs.FileMode) fs.FileMode {
	return perm | 0o400
}

// objectPath is where the object for content sum with (object) permission
// bits perm lives.
func (s *Store) objectPath(sum *[32]byte, perm fs.FileMode) string {
	return s.shardPath(s.files, sum, objectSuffix(perm))
}

// stamp is what an intact object's stat shows: a regular file of the
// indexed size with the object's mode and a modification time derived from
// its hash, to the second (nanoseconds zero). The link count is not part of
// it: objects are hard-linked into package directories, and every write
// through any of their names sets the modification time to the time of the
// write, which no stamp lies near. The change time cannot serve either:
// every new or removed link moves it.
type stamp struct {
	size  int64
	mtime int64
	perm  fs.FileMode
}

func stampOf(size int64, perm fs.FileMode, sum *[32]byte) stamp {
	return stamp{size: size, perm: perm, mtime: stampTime(sum)}
}

// check fails with errStale unless st is the object as the store wrote it.
func (w stamp) check(st fileStat) error {
	if !st.regular || st.size != w.size || st.mode != statPerm(w.perm) || st.mtime != w.mtime || st.mtimeNs != 0 {
		return errStale
	}

	return nil
}

// bufPool holds read buffers for inserting and hashing files.
var bufPool = sync.Pool{New: func() any {
	b := make([]byte, 0, 64<<10)
	return &b
}}

// createObjectTemp opens a new temporary file for an object with mode perm.
func (s *Store) createObjectTemp(perm fs.FileMode) (*os.File, error) {
	for {
		f, err := fsstate.OpenFile(tmpName(s.tmp, "o"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
		if !errors.Is(err, fs.ErrExist) {
			return f, err
		}
		// Left behind by a crashed process that had this pid.
	}
}

// publishObject stamps and closes a written temporary object and renames
// it into place: without replacing, unless replace is set (the object
// there is stale). Another process publishing the same object first is
// success.
func (s *Store) publishObject(f *os.File, sum *[32]byte, perm fs.FileMode, path string, replace bool) error {
	err := setMtime(f, stampTime(sum))
	if err == nil && chmodAfterCreate(perm, s.umask) {
		err = f.Chmod(perm)
	}

	if cerr := f.Close(); err == nil {
		err = cerr
	}

	if err == nil {
		err = s.intoShard(0, sum, path, func() error {
			if replace {
				return replaceFile(f.Name(), path)
			}

			return renameNoReplace(f.Name(), path)
		})

		if !replace && errors.Is(err, fs.ErrExist) {
			return os.Remove(f.Name())
		}
	}

	if err != nil {
		_ = os.Remove(f.Name())
	}

	return err
}

// putObject stores data as the object for sum and perm unless an intact
// one is there already: one stat when it is, one create, write and rename
// when it is not.
func (s *Store) putObject(data []byte, sum *[32]byte, perm fs.FileMode) error {
	path := s.objectPath(sum, perm)

	st, err := lstat(path)
	if err == nil && stampOf(int64(len(data)), perm, sum).check(st) == nil {
		return nil
	}

	stale := err == nil

	f, err := s.createObjectTemp(perm)
	if err != nil {
		return err
	}

	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())

		return err
	}

	return s.publishObject(f, sum, perm, path, stale)
}

// putObjectStream stores the content r yields (size bytes) for a file too
// large to buffer: it is written to a temporary file while hashed, then
// published. It returns the content's hash.
func (s *Store) putObjectStream(r io.Reader, size int64, perm fs.FileMode, h hash.Hash, buf []byte) ([32]byte, error) {
	var sum [32]byte

	f, err := s.createObjectTemp(perm)
	if err != nil {
		return sum, err
	}

	h.Reset()

	n, err := io.CopyBuffer(io.MultiWriter(f, h), r, buf)
	if err == nil && n != size {
		err = fmt.Errorf("store: read %d bytes of a %d byte file", n, size)
	}

	if err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())

		return sum, err
	}

	h.Sum(sum[:0])
	path := s.objectPath(&sum, perm)

	st, err := lstat(path)
	if err == nil && stampOf(size, perm, &sum).check(st) == nil {
		_ = f.Close()

		return sum, os.Remove(f.Name())
	}

	return sum, s.publishObject(f, &sum, perm, path, err == nil)
}

// heal makes the object for e (content hash, size) with perm intact again
// after it failed its stamp check or vanished: from its own content when
// that still hashes right, else from another mode's object of the same
// content. The content is hashed as it is copied into a new file, so
// nothing written to the old one meanwhile can get in; the new object
// replaces the old name, and package files hard-linked to the old inode
// keep it (with whatever was written through them). It fails with
// *MissingError when no intact content is left.
func (s *Store) heal(e *Entry, perm fs.FileMode) error {
	path := s.objectPath(&e.Hash, perm)

	if s.copyIfIntact(path, e, perm, path) {
		return nil
	}

	_ = os.Remove(path)

	// Every other mode of the same content: <62 hex>[-mode].
	dir, base := filepath.Split(path)
	stem, _, _ := strings.Cut(base, "-")

	names, _ := os.ReadDir(dir)
	for _, n := range names {
		if other := dir + n.Name(); n.Name() != base && strings.HasPrefix(n.Name(), stem) && s.copyIfIntact(other, e, perm, path) {
			return nil
		}
	}

	return &MissingError{Path: path}
}

// copyIfIntact copies src to the object dst (replacing it) if src holds
// e's content, and reports whether it did. Content that turns out to
// differ still lands in its own, correctly named object, which is harmless.
func (s *Store) copyIfIntact(src string, e *Entry, perm fs.FileMode, dst string) bool {
	in, err := openShared(src)
	if err != nil {
		return false
	}

	defer func() { _ = in.Close() }()

	if st, err := fstat(in); err != nil || !st.regular || st.size != e.Size {
		return false
	}

	bp := bufPool.Get().(*[]byte) //nolint:errcheck // the pool only holds *[]byte.
	defer bufPool.Put(bp)

	sum, err := s.putObjectStream(in, e.Size, perm, sha256.New(), (*bp)[:cap(*bp)])

	return err == nil && sum == e.Hash && dst == s.objectPath(&sum, perm)
}

// hashFile is the SHA-256 of the file at path.
func hashFile(path string, buf []byte) ([32]byte, int64, error) {
	var sum [32]byte

	f, err := openShared(path)
	if err != nil {
		return sum, 0, err
	}

	defer func() { _ = f.Close() }()

	h := sha256.New()

	n, err := io.CopyBuffer(h, f, buf)
	h.Sum(sum[:0])

	return sum, n, err
}

// parseObjectName reads an object's hash and mode from its shard and file
// name.
func parseObjectName(shard, name string) (sum [32]byte, perm fs.FileMode, ok bool) {
	stem, mode, hasMode := strings.Cut(name, "-")
	perm = 0o644

	if hasMode {
		v, err := strconv.ParseUint(mode, 8, 32)
		if err != nil || v > 0o777 || mode != strconv.FormatUint(v, 8) || v == 0o644 {
			return sum, 0, false
		}

		perm = fs.FileMode(v)
	}

	raw, err := hex.DecodeString(shard + stem)
	if err != nil || len(raw) != 32 || !bytes.Equal([]byte(hex.EncodeToString(raw)), []byte(shard+stem)) {
		return sum, 0, false
	}

	copy(sum[:], raw)

	return sum, perm, true
}
