package store

import (
	"crypto/sha256"
	"errors"
	"hash"
	"io"
	"sync"
	"sync/atomic"

	"github.com/stubbedev/maestro/internal/archive"
)

// smallFile is the largest file Insert reads into memory and hands to a
// worker; larger ones are streamed to disk while being hashed.
const smallFile = 1 << 20

// Deriver works data out from the files of a release while Insert stores
// them, and keeps it with the release (WriteDerived) once it is in the
// store.
type Deriver interface {
	// File sees the content of the file at path (slash-separated, in the
	// release's tree), with its hash, once it is stored. It is called for
	// the files Insert holds in memory (those of up to 1 MiB), from
	// several goroutines at once, and must not keep data.
	File(path string, data []byte, sum *[32]byte)
	// Inserted is called once release r is in store s, after File saw
	// all of its files, unless the insert failed.
	Inserted(s *Store, r *Release)
}

// Insert extracts the archive at path into the store as dist d's release,
// replacing any index the dist had. Files already in the store are not
// written again. Archives maestro cannot extract exactly as Composer would
// fail with the archive package's *archive.Error. dv, when not nil, sees
// the files as they are stored.
func (s *Store) Insert(d Dist, path string, dv Deriver) (*Release, error) {
	format, opts, err := s.options(&d)
	if err != nil {
		return nil, err
	}

	indexPath, id := s.indexPath(&d, format, opts)

	a, err := archive.Open(path, format, opts)
	if err != nil {
		return nil, err
	}

	defer func() { _ = a.Close() }()

	unlock, err := s.lock(false)
	if err != nil {
		return nil, err
	}

	defer unlock()

	planned := a.Entries()
	entries := make([]Entry, len(planned))

	for i := range planned {
		entries[i].Entry = planned[i]
	}

	in := &inserter{s: s, entries: entries, derive: dv}

	err = in.wait(a.ReadFiles(in.file))
	if err != nil {
		return nil, err
	}

	data := encodeIndex(entries)

	if err := s.intoShard(1, &id, indexPath, func() error { return s.writeAtomic(indexPath, data) }); err != nil {
		return nil, err
	}

	r := &Release{entries: entries, id: id}
	s.releases.Store(id, r)

	if dv != nil {
		dv.Inserted(s, r)
	}

	return r, nil
}

// inserter stores the files of one archive: the archive is read in one
// goroutine (decompression is sequential), small files are hashed and
// written by workers.
type inserter struct {
	err     error
	s       *Store
	derive  Deriver
	hash    hash.Hash
	jobs    chan insertJob
	entries []Entry
	big     []byte
	one     [1]byte
	wg      sync.WaitGroup
	mu      sync.Mutex
	failed  atomic.Bool
}

type insertJob struct {
	buf *[]byte
	i   int
}

func (in *inserter) fail(err error) {
	in.mu.Lock()
	if in.err == nil {
		in.err = err
	}
	in.mu.Unlock()
	in.failed.Store(true)
}

// file receives file i's content from the archive.
func (in *inserter) file(i int, r io.Reader) error {
	if in.failed.Load() {
		return errStopped
	}

	e := &in.entries[i]
	perm := objectPerm(e.Perm(in.s.umask))

	if e.Size > smallFile {
		if in.hash == nil {
			in.hash, in.big = sha256.New(), make([]byte, 256<<10)
		}

		sum, err := in.s.putObjectStream(r, e.Size, perm, in.hash, in.big)
		e.Hash = sum

		return err
	}

	bp := bufPool.Get().(*[]byte) //nolint:errcheck // the pool only holds *[]byte.
	if int64(cap(*bp)) < e.Size {
		*bp = make([]byte, e.Size)
	}

	buf := (*bp)[:e.Size]

	if _, err := io.ReadFull(r, buf); err != nil {
		bufPool.Put(bp)
		return err
	}

	// Read on to the end, so the archive checks the content (its CRC, say)
	// before anything is stored.
	if err := expectEOF(r, in.one[:]); err != nil {
		bufPool.Put(bp)
		return err
	}

	if in.jobs == nil {
		in.start()
	}

	in.jobs <- insertJob{buf: bp, i: i}

	return nil
}

var errStopped = errors.New("store: insert stopped")

func (in *inserter) start() {
	workers := max(in.s.workers-1, 1)
	in.jobs = make(chan insertJob, 2*workers)

	for range workers {
		in.wg.Go(func() {
			for j := range in.jobs {
				if !in.failed.Load() {
					e := &in.entries[j.i]
					data := (*j.buf)[:e.Size]
					e.Hash = sha256.Sum256(data)

					if err := in.s.putObject(data, &e.Hash, objectPerm(e.Perm(in.s.umask))); err != nil {
						in.fail(err)
					} else if in.derive != nil {
						in.derive.File(e.Path, data, &e.Hash)
					}
				}

				bufPool.Put(j.buf)
			}
		})
	}
}

// wait finishes the workers and reports the first failure, the reader's
// own first.
func (in *inserter) wait(readErr error) error {
	if in.jobs != nil {
		close(in.jobs)
		in.wg.Wait()
	}

	if readErr != nil && !errors.Is(readErr, errStopped) {
		return readErr
	}

	return in.err
}

// expectEOF fails unless r has nothing more to give; one is a scratch
// byte.
func expectEOF(r io.Reader, one []byte) error {
	for {
		n, err := r.Read(one)
		if n > 0 {
			return errors.New("store: file longer than planned")
		}

		if errors.Is(err, io.EOF) {
			return nil
		}

		if err != nil {
			return err
		}
	}
}
