package store

import (
	"crypto/sha256"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/stubbedev/maestro/internal/archive"
)

// PruneResult is what Prune removed.
type PruneResult struct {
	Releases int
	Objects  int
	Bytes    int64
}

// Prune removes the releases not used (looked up or inserted) for maxAge,
// then every object no remaining release refers to, and leftover temporary
// files. It holds the store lock exclusively, so inserts and imports wait.
func (s *Store) Prune(maxAge time.Duration) (PruneResult, error) {
	var res PruneResult

	unlock, err := s.lock(true)
	if err != nil {
		return res, err
	}

	defer unlock()

	cutoff := time.Now().Add(-maxAge).Unix()
	keep := map[[32]byte]struct{}{}

	err = s.walkIndexes(func(path string, st fileStat) error {
		if st.mtime < cutoff {
			res.Releases++
			return remove(path)
		}

		entries, err := readIndex(path)
		if err != nil {
			res.Releases++
			return remove(path)
		}

		for i := range entries {
			if entries[i].Kind == archive.File {
				keep[entries[i].Hash] = struct{}{}
			}
		}

		return nil
	})
	if err != nil {
		return res, err
	}

	err = s.walkObjects(func(path string, sum *[32]byte, _ fs.FileMode, st fileStat) error {
		if _, ok := keep[*sum]; ok {
			return nil
		}

		res.Objects++
		res.Bytes += st.size

		return remove(path)
	})
	if err != nil {
		return res, err
	}

	// Nothing writes while the lock is held: whatever tmp/ holds is left
	// over from a crash.
	names, err := os.ReadDir(s.tmp)
	if err != nil {
		return res, err
	}

	for _, n := range names {
		if err := remove(filepath.Join(s.tmp, n.Name())); err != nil {
			return res, err
		}
	}

	return res, nil
}

// VerifyResult is what Verify found.
type VerifyResult struct {
	Objects int
	// Corrupt counts objects whose content no longer matched their name;
	// they were removed.
	Corrupt int
	// Restamped counts intact objects whose metadata had been changed
	// (a write that kept the content, a touch, a chmod); they were
	// replaced by fresh copies.
	Restamped int
	Releases  int
	// CorruptReleases counts unreadable indexes; they were removed.
	CorruptReleases int
	// Missing counts files that releases refer to without any object
	// holding their content.
	Missing int
}

// Verify re-hashes every object, removing the corrupt ones and replacing
// the ones whose metadata changed, and checks every index.
func (s *Store) Verify() (VerifyResult, error) {
	var res VerifyResult

	unlock, err := s.lock(false)
	if err != nil {
		return res, err
	}

	defer unlock()

	bp := bufPool.Get().(*[]byte) //nolint:errcheck // the pool only holds *[]byte.
	defer bufPool.Put(bp)

	buf := (*bp)[:cap(*bp)]
	have := map[[32]byte]struct{}{}

	err = s.walkObjects(func(path string, sum *[32]byte, perm fs.FileMode, st fileStat) error {
		res.Objects++

		got, n, err := hashFile(path, buf)
		if err != nil {
			return err
		}

		if got != *sum || n != st.size || !st.regular {
			res.Corrupt++
			return remove(path)
		}

		have[*sum] = struct{}{}

		if stampOf(st.size, perm, sum).check(st) == nil {
			return nil
		}

		res.Restamped++

		in, err := os.Open(path)
		if err != nil {
			return err
		}

		defer func() { _ = in.Close() }()

		_, err = s.putObjectStream(in, st.size, perm, sha256.New(), buf)

		return err
	})
	if err != nil {
		return res, err
	}

	err = s.walkIndexes(func(path string, _ fileStat) error {
		res.Releases++

		entries, err := readIndex(path)
		if err != nil {
			res.CorruptReleases++
			return remove(path)
		}

		for i := range entries {
			if entries[i].Kind != archive.File {
				continue
			}

			if _, ok := have[entries[i].Hash]; !ok {
				res.Missing++
			}
		}

		return nil
	})

	return res, err
}

// Stats is what the store holds.
type Stats struct {
	Releases int
	Objects  int
	// ObjectBytes is the size of all objects: the disk the store uses.
	ObjectBytes int64
	// ReleaseBytes is the size of every release's files added up: what
	// keeping each release whole would use.
	ReleaseBytes int64
	// LinkedBytes is the size of the files package directories share with
	// the store through hardlinks, each counted once per extra link.
	// Reflink clones share blocks too, which the store cannot see.
	LinkedBytes int64
}

// Saved is the number of bytes the store saves: content shared between
// releases plus content package directories share with it by hardlink.
func (st Stats) Saved() int64 {
	return max(st.ReleaseBytes-st.ObjectBytes, 0) + st.LinkedBytes
}

// Stats reports what the store holds.
func (s *Store) Stats() (Stats, error) {
	var res Stats

	err := s.walkObjects(func(_ string, _ *[32]byte, _ fs.FileMode, st fileStat) error {
		res.Objects++
		res.ObjectBytes += st.size

		if st.nlink > 1 {
			res.LinkedBytes += st.size * int64(st.nlink-1) //nolint:gosec // link counts are small.
		}

		return nil
	})
	if err != nil {
		return res, err
	}

	err = s.walkIndexes(func(path string, _ fileStat) error {
		entries, err := readIndex(path)
		if err != nil {
			return nil //nolint:nilerr // Verify deals with corrupt indexes.
		}

		res.Releases++

		for i := range entries {
			res.ReleaseBytes += entries[i].Size
		}

		return nil
	})

	return res, err
}

// walkObjects calls fn for every well-named object.
func (s *Store) walkObjects(fn func(path string, sum *[32]byte, perm fs.FileMode, st fileStat) error) error {
	return walkShards(s.files, func(shard, name, path string) error {
		sum, perm, ok := parseObjectName(shard, name)
		if !ok {
			return nil
		}

		st, err := lstat(path)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}

		if err != nil {
			return err
		}

		return fn(path, &sum, perm, st)
	})
}

// walkIndexes calls fn for every index.
func (s *Store) walkIndexes(fn func(path string, st fileStat) error) error {
	return walkShards(s.index, func(_, _, path string) error {
		st, err := lstat(path)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}

		if err != nil {
			return err
		}

		return fn(path, st)
	})
}

// walkShards calls fn for every file in dir's shard directories.
func walkShards(dir string, fn func(shard, name, path string) error) error {
	shards, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	for _, sh := range shards {
		if !sh.IsDir() || len(sh.Name()) != 2 {
			continue
		}

		shardDir := filepath.Join(dir, sh.Name())

		names, err := os.ReadDir(shardDir)
		if err != nil {
			return err
		}

		for _, n := range names {
			if err := fn(sh.Name(), n.Name(), filepath.Join(shardDir, n.Name())); err != nil {
				return err
			}
		}
	}

	return nil
}

func readIndex(path string) ([]Entry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	return decodeIndex(data)
}

func remove(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	return nil
}

// Unshare gives the regular file at path an inode of its own if it shares
// one (a hardlink to a store object), so that it can be changed in place
// without changing the store or other projects: the file is copied next
// to itself and the copy renamed over it. The copy keeps the mode.
func Unshare(path string) error {
	st, err := lstat(path)
	if err != nil || !st.regular || st.nlink <= 1 {
		return err
	}

	in, err := os.Open(path)
	if err != nil {
		return err
	}

	defer func() { _ = in.Close() }()

	tmp := tmpName(filepath.Dir(path), "."+filepath.Base(path)+".maestro-")

	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}

	_, err = io.Copy(out, in)
	if err == nil {
		err = out.Chmod(unixMode(st.mode))
	}

	if cerr := out.Close(); err == nil {
		err = cerr
	}

	// Windows replaces no file that is open without FILE_SHARE_DELETE.
	_ = in.Close()

	if err == nil {
		err = os.Rename(tmp, path)
	}

	if err != nil {
		_ = os.Remove(tmp)
	}

	return err
}

// Chmod is os.Chmod for package files, which may be hardlinks to store
// objects (Composer's chmod of package binaries): when the mode actually
// changes, the file is unshared first so the change stays in this project
// and the store object keeps its stamp. Like chmod, it follows symlinks.
func Chmod(path string, mode fs.FileMode) error {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}

	st, err := lstat(real)
	if err != nil {
		return err
	}

	if unixMode(st.mode) == statPerm(mode) {
		return nil
	}

	if err := Unshare(real); err != nil {
		return err
	}

	return os.Chmod(real, mode)
}

// unixMode turns raw permission and set-id/sticky bits into an
// fs.FileMode.
func unixMode(bits fs.FileMode) fs.FileMode {
	m := bits & fs.ModePerm

	if bits&0o4000 != 0 {
		m |= fs.ModeSetuid
	}

	if bits&0o2000 != 0 {
		m |= fs.ModeSetgid
	}

	if bits&0o1000 != 0 {
		m |= fs.ModeSticky
	}

	return m
}
