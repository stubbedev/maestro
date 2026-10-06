package store

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"hash"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/stubbedev/maestro/internal/archive"
)

// namedMagic opens the identity of every tree inserted from a directory,
// so that no named id can collide with a dist's.
const namedMagic = "maestro-store-dir-1"

// namedPath names the index of the tree the caller identifies by id: the
// SHA-256 of namedMagic, the process umask (the tree records the modes
// files were created with under it) and id.
func (s *Store) namedPath(id *[32]byte) (string, [32]byte) {
	h := sha256.New()

	for _, part := range []string{namedMagic, strconv.FormatUint(uint64(s.umask), 8), string(id[:])} {
		h.Write([]byte(strconv.Itoa(len(part))))
		h.Write([]byte{':'})
		h.Write([]byte(part))
	}

	var name [32]byte

	h.Sum(name[:0])

	return s.shardPath(s.index, &name, ""), name
}

// LookupNamed returns the tree InsertDir stored under id, or ErrNotFound.
// Like Lookup, it marks the tree as used, for Prune.
func (s *Store) LookupNamed(id [32]byte) (*Release, error) {
	path, name := s.namedPath(&id)

	return s.lookupIndex(path, name)
}

// InsertDir stores the directory tree at dir under id, which the caller
// derives from everything the tree's content depends on (a git checkout:
// the mirror, the reference, git and its configuration), replacing any
// tree stored under id before. Every entry keeps the permission bits it
// has on disk (Umask unset; the index name carries the umask instead),
// symlinks are recorded and never followed, and anything else than a
// file, directory or symlink fails the insert. Import the tree with
// Materialize, unshared when people may edit it.
func (s *Store) InsertDir(id [32]byte, dir string) (*Release, error) {
	indexPath, name := s.namedPath(&id)

	unlock, err := s.lock(false)
	if err != nil {
		return nil, err
	}

	defer unlock()

	root := filepath.Clean(dir)

	var (
		entries []Entry
		files   []int
	)

	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		var e Entry

		if path != root {
			e.Path = filepath.ToSlash(path[len(root)+1:])
		}

		e.Mode = info.Mode().Perm()

		switch mode := info.Mode(); {
		case mode.IsDir():
			e.Kind = archive.Dir
		case mode&fs.ModeSymlink != 0:
			e.Kind, e.Mode = archive.Symlink, 0

			if e.Link, err = os.Readlink(path); err != nil {
				return err
			}
		case mode.IsRegular():
			e.Kind, e.Size = archive.File, info.Size()
			files = append(files, len(entries))
		default:
			return fmt.Errorf("store: %s is not a file, directory or symlink", path)
		}

		if path == root && e.Kind != archive.Dir {
			return fmt.Errorf("store: %s is not a directory", path)
		}

		entries = append(entries, e)

		return nil
	})
	if err != nil {
		return nil, err
	}

	if err := s.putFiles(root, entries, files); err != nil {
		return nil, err
	}

	data := encodeIndex(entries)

	if err := s.intoShard(1, &name, indexPath, func() error { return s.writeAtomic(indexPath, data) }); err != nil {
		return nil, err
	}

	r := &Release{entries: entries, id: name}
	s.releases.Store(name, r)

	return r, nil
}

// putFiles stores the content of the files (indexes into entries) under
// root, filling in their hashes, with the store's workers.
func (s *Store) putFiles(root string, entries []Entry, files []int) error {
	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		first error
		next  = make(chan int)
	)

	for range min(s.workers, max(len(files), 1)) {
		wg.Go(func() {
			buf := make([]byte, 256<<10)
			h := sha256.New()

			for i := range next {
				mu.Lock()
				failed := first != nil
				mu.Unlock()

				if failed {
					continue
				}

				if err := s.putFile(root, &entries[i], h, buf); err != nil {
					mu.Lock()
					if first == nil {
						first = err
					}
					mu.Unlock()
				}
			}
		})
	}

	for _, i := range files {
		next <- i
	}

	close(next)
	wg.Wait()

	return first
}

// putFile stores one file of InsertDir's tree.
func (s *Store) putFile(root string, e *Entry, h hash.Hash, buf []byte) error {
	path := root + string(os.PathSeparator) + filepath.FromSlash(e.Path)
	perm := objectPerm(e.Mode)

	if e.Size <= smallFile {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		if int64(len(data)) != e.Size {
			return errors.New("store: " + path + " changed while it was inserted")
		}

		e.Hash = sha256.Sum256(data)

		return s.putObject(data, &e.Hash, perm)
	}

	f, err := os.Open(path)
	if err != nil {
		return err
	}

	defer func() { _ = f.Close() }()

	e.Hash, err = s.putObjectStream(f, e.Size, perm, h, buf)

	return err
}
