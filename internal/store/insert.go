package store

import (
	"crypto/sha256"
	"errors"
	"hash"
	"io"
	"path/filepath"
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
	return s.insert(d, path, "", ImportOptions{}, dv)
}

// insert is Insert, also creating the package directory dst from the
// release when dst is not empty (Install): each file is imported as soon
// as its object is written, from the object still open, so that a new
// release's files are created once in the store and once in dst and
// nothing is looked up, opened or checked twice. dst is assembled in a
// temporary sibling and renamed onto it, as Materialize does.
func (s *Store) insert(d Dist, path, dst string, opts ImportOptions, dv Deriver) (*Release, error) {
	format, aopts, err := s.options(&d)
	if err != nil {
		return nil, err
	}

	indexPath, id := s.indexPath(&d, format, aopts)

	unlock, err := s.lock(false)
	if err != nil {
		return nil, err
	}

	defer unlock()

	// The slot bounds the archives held in memory as well as the
	// goroutines writing files.
	s.slots <- struct{}{}
	defer func() { <-s.slots }()

	a, err := archive.Open(path, format, aopts)
	if err != nil {
		return nil, err
	}

	defer func() { _ = a.Close() }()

	planned := a.Entries()
	entries := make([]Entry, len(planned))

	for i := range planned {
		entries[i].Entry = planned[i]
	}

	in := &inserter{s: s, entries: entries, derive: dv}

	var tmp string

	if dst != "" {
		tmp = tmpName(filepath.Dir(dst), "."+filepath.Base(dst)+".maestro-")
		if in.tree, err = s.plant(entries, tmp, opts.Unshared); err != nil {
			return nil, err
		}
	}

	r, err := in.run(a, indexPath, id)
	if err == nil && tmp != "" {
		err = renameDir(tmp, dst)
	}

	if err != nil {
		if tmp != "" {
			_ = removeTree(tmp)
		}

		return nil, err
	}

	return r, nil
}

// run reads the archive's files into the store (and the tree), then
// writes the release's index.
func (in *inserter) run(a *archive.Archive, indexPath string, id [32]byte) (*Release, error) {
	s := in.s

	readErr := a.ReadFiles(in.file)
	if readErr == nil {
		in.dispatch(false)
	}

	if err := in.wait(readErr); err != nil {
		return nil, err
	}

	if in.tree != nil {
		if err := in.tree.finish(); err != nil {
			return nil, err
		}
	}

	data := encodeIndex(in.entries)

	if err := s.intoShard(1, &id, indexPath, func() error { return s.writeAtomic(indexPath, data) }); err != nil {
		return nil, err
	}

	r := &Release{entries: in.entries, id: id}
	s.releases.Store(id, r)

	if in.derive != nil {
		in.derive.Inserted(s, r)
	}

	return r, nil
}

// inserter stores the files of one archive. The archive is read in the
// goroutine holding the insert's slot (decompression is sequential); the
// files it holds in memory are collected into batches, each stored by a
// helper when another slot is free and by that goroutine itself when
// none is.
type inserter struct {
	err     error
	s       *Store
	derive  Deriver
	tree    *tree // the package directory being built, if any
	hash    hash.Hash
	entries []Entry
	batch   []insertJob
	big     []byte
	size    int64 // bytes in batch
	one     [1]byte
	wg      sync.WaitGroup
	mu      sync.Mutex
	failed  atomic.Bool
}

type insertJob struct {
	buf *[]byte
	i   int
}

// A batch is handed on once it holds batchFiles files or batchBytes bytes:
// large enough that handing it over costs little beside the work, small
// enough that a package of a few hundred files still spreads over the
// free slots.
const (
	batchFiles = 32
	batchBytes = 512 << 10
)

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

	if e.Size > smallFile {
		return in.bigFile(e, r)
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

	in.batch = append(in.batch, insertJob{buf: bp, i: i})
	in.size += e.Size

	if len(in.batch) >= batchFiles || in.size >= batchBytes {
		in.dispatch(true)
	}

	return nil
}

// bigFile stores a file too large to hold in memory while reading it,
// then imports it into the tree from the store.
func (in *inserter) bigFile(e *Entry, r io.Reader) error {
	if in.hash == nil {
		in.hash, in.big = sha256.New(), make([]byte, 256<<10)
	}

	sum, err := in.s.putObjectStream(r, e.Size, objectPerm(e.Perm(in.s.umask)), in.hash, in.big)
	e.Hash = sum

	if err == nil && in.tree != nil {
		err = in.s.importFile(in.tree.dev, e, in.tree.path(e), in.tree.unshared)
	}

	return err
}

var errStopped = errors.New("store: insert stopped")

// dispatch hands the batch collected so far to a helper when a slot is
// free (and help is allowed), else stores it in this goroutine.
func (in *inserter) dispatch(help bool) {
	jobs := in.batch
	if len(jobs) == 0 {
		return
	}

	in.batch, in.size = nil, 0

	if help {
		select {
		case in.s.slots <- struct{}{}:
			in.wg.Go(func() {
				defer func() { <-in.s.slots }()

				in.store(jobs)
			})

			return
		default:
		}
	}

	in.store(jobs)
}

// store hashes and stores a batch of files, importing each into the tree.
func (in *inserter) store(jobs []insertJob) {
	s := in.s

	for _, j := range jobs {
		if !in.failed.Load() {
			e := &in.entries[j.i]
			data := (*j.buf)[:e.Size]
			e.Hash = sha256.Sum256(data)

			var err error
			if in.tree != nil {
				err = s.putImport(in.tree.dev, e, data, in.tree.path(e), in.tree.unshared)
			} else {
				err = s.putObject(data, &e.Hash, objectPerm(e.Perm(s.umask)))
			}

			if err != nil {
				in.fail(err)
			} else if in.derive != nil {
				in.derive.File(e.Path, data, &e.Hash)
			}
		}

		bufPool.Put(j.buf)
	}
}

// wait finishes the helpers and reports the first failure, the reader's
// own first.
func (in *inserter) wait(readErr error) error {
	in.wg.Wait()

	for _, j := range in.batch {
		bufPool.Put(j.buf)
	}

	in.batch = nil

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

// path is where entry e goes in the tree.
func (t *tree) path(e *Entry) string {
	return t.base + filepath.FromSlash(e.Path)
}
