package store

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/stubbedev/maestro/internal/archive"
	"github.com/stubbedev/maestro/internal/util/fsstate"
)

// errUnsupported means the filesystem cannot reflink.
var errUnsupported = errors.New("reflinks unsupported")

// device is the import method chosen for one destination device. Under
// Auto it only ever moves down: clone, hardlink, copy.
type device struct {
	method atomic.Int32
	// noNewLinks: hardlinks from the store to this device fail
	// (importFresh).
	noNewLinks atomic.Bool
}

func (d *device) get() Method {
	return Method(d.method.Load())
}

func (d *device) demote(from, to Method) {
	d.method.CompareAndSwap(int32(from), int32(to))
}

func (s *Store) device(dev uint64) *device {
	if d, ok := s.devices.Load(dev); ok {
		return d.(*device) //nolint:errcheck // the map only holds *device.
	}

	d := &device{}

	start := s.method
	if start == Auto {
		start = Clone
	}

	d.method.Store(int32(start))

	actual, _ := s.devices.LoadOrStore(dev, d)

	return actual.(*device) //nolint:errcheck // as above.
}

// Materialize creates the package directory dst from a release: it is
// assembled in a temporary sibling and renamed onto dst, which must not
// exist or be an empty directory. Objects that went missing or were
// modified are healed from intact copies; when none is left it fails with
// *MissingError and the release must be inserted again.
func (s *Store) Materialize(r *Release, dst string, opts ImportOptions) error {
	unlock, err := s.lock(false)
	if err != nil {
		return err
	}

	defer unlock()

	tmp := tmpName(filepath.Dir(dst), "."+filepath.Base(dst)+".maestro-")

	s.imports <- struct{}{}
	err = s.build(r.entries, tmp, opts.Unshared)
	<-s.imports

	if err == nil {
		err = renameDir(tmp, dst)
	}

	if err != nil {
		_ = removeTree(tmp)
	}

	return err
}

// dirFix is a directory whose final mode is set once it is filled.
type dirFix struct {
	path string
	mode fs.FileMode
}

// tree is a package directory being built: its directories and symlinks
// are there, its files are being imported.
type tree struct {
	dev      *device
	base     string // the directory and a separator
	fixes    []dirFix
	files    []int // the File entries
	unshared bool  // never hardlink a file
}

// build creates tmp and fills it with the release's tree, never
// hardlinking a file when unshared is set.
func (s *Store) build(entries []Entry, tmp string, unshared bool) error {
	t, err := s.plant(entries, tmp, unshared)
	if err == nil {
		err = s.importFiles(t.dev, entries, t.files, t.base, unshared, nil)
	}

	if err == nil {
		err = t.finish()
	}

	return err
}

// plant creates tmp with the directories and symlinks of the tree, and
// lists its files.
func (s *Store) plant(entries []Entry, tmp string, unshared bool) (*tree, error) {
	if err := os.Mkdir(tmp, entries[0].Perm(s.umask)|0o700); err != nil {
		return nil, err
	}

	st, err := lstat(tmp)
	if err != nil {
		return nil, err
	}

	t := &tree{
		dev:      s.device(st.dev),
		base:     tmp + string(os.PathSeparator),
		files:    make([]int, 0, len(entries)),
		unshared: unshared,
	}

	// A setgid bit inherited from the parent stays on every directory.
	sgid := fs.FileMode(0)
	if st.mode&0o2000 != 0 {
		sgid = fs.ModeSetgid
	}

	fix := func(path string, perm, created fs.FileMode) {
		if perm != created {
			t.fixes = append(t.fixes, dirFix{path: path, mode: perm | sgid})
		}
	}

	fix(tmp, entries[0].Perm(s.umask), st.mode&fs.ModePerm)

	for i := 1; i < len(entries); i++ {
		e := &entries[i]
		path := t.base + filepath.FromSlash(e.Path)

		switch e.Kind {
		case archive.Dir:
			perm := e.Perm(s.umask)

			created, err := s.mkdir(path, perm|0o700)
			if err != nil {
				return nil, err
			}

			fix(path, perm, created)
		case archive.Symlink:
			if err := os.Symlink(e.Link, path); err != nil {
				return nil, err
			}
		case archive.File:
			t.files = append(t.files, i)
		}
	}

	return t, nil
}

// finish gives the tree's directories their final modes, deepest first: a
// parent's mode may forbid changing its children.
func (t *tree) finish() error {
	for _, f := range slices.Backward(t.fixes) {
		if err := os.Chmod(f.path, f.mode); err != nil {
			return err
		}
	}

	return nil
}

// mkdir creates a directory the store can fill, returning the permission
// bits it got.
func (s *Store) mkdir(path string, mode fs.FileMode) (fs.FileMode, error) {
	if err := os.Mkdir(path, mode); err != nil {
		return 0, err
	}

	if s.umask&0o700 == 0 {
		return mode &^ s.umask, nil
	}

	// A umask that takes the owner's own access away.
	return mode, os.Chmod(path, mode)
}

// importFiles imports the files, in parallel for larger packages. fresh,
// when not nil, marks the files whose objects this process has just
// created (importFresh). The files of one directory are created by one
// goroutine, in the release's order: readdir() lists a directory's
// entries in the order they were created on some filesystems (btrfs), as
// unzip creates them, and Composer and the dump follow readdir order. The
// caller holds a slot and works through the directories; it takes on
// helpers from the import slots while some are free and enough files are
// left (rechecking as it goes, since the imports running beside it
// finish), so that concurrent imports together keep to their slots.
func (s *Store) importFiles(dev *device, entries []Entry, files []int, base string, unshared bool, fresh []bool) error {
	dirs, ends := byDirectory(entries, files)

	var (
		next    atomic.Int64
		failed  atomic.Bool
		once    sync.Once
		first   error
		wg      sync.WaitGroup
		helpers int
		work    func(owner bool)
	)

	work = func(owner bool) {
		for !failed.Load() {
			k := next.Add(1) - 1
			if k >= int64(len(dirs)) {
				return
			}

			if owner {
				// another helper per importBatch files still to do
				left := int64(len(files) - ends[k])
				for helpers < cap(s.imports)-1 && left/importBatch > int64(helpers) {
					select {
					case s.imports <- struct{}{}:
						helpers++

						wg.Go(func() {
							defer func() { <-s.imports }()

							work(false)
						})

						continue
					default:
					}

					break
				}
			}

			for _, i := range dirs[k] {
				e := &entries[i]
				dst := base + filepath.FromSlash(e.Path)

				var err error
				if fresh != nil && fresh[i] {
					err = s.importFresh(dev, e, dst, unshared)
				} else {
					err = s.importFile(dev, e, dst, unshared)
				}

				if err != nil {
					once.Do(func() { first = err })
					failed.Store(true)

					break
				}
			}
		}
	}

	work(true)
	wg.Wait()

	return first
}

// byDirectory groups the files by their directory, each group in the
// files' order and the groups in the order of their first file; ends[k]
// counts the files in groups 0 to k.
func byDirectory(entries []Entry, files []int) (dirs [][]int, ends []int) {
	at := map[string]int{}

	for _, i := range files {
		dir := path.Dir(entries[i].Path)

		k, ok := at[dir]
		if !ok {
			k = len(dirs)
			at[dir] = k
			dirs = append(dirs, nil)
		}

		dirs[k] = append(dirs[k], i)
	}

	ends = make([]int, len(dirs))
	n := 0

	for k := range dirs {
		n += len(dirs[k])
		ends[k] = n
	}

	return dirs, ends
}

// importBatch is how many files are worth another goroutine.
const importBatch = 64

// importFresh creates dst from e's object, which this process has just
// created: no package file links to it yet, so nothing but the store can
// have written it, and under Auto it is hard-linked without checking its
// stamp again wherever the hardlink has the file's mode and the package
// is not unshared (whatever the filesystem: that project and the store
// then share the inode, while later imports clone where they can). Else,
// and when the object was replaced meanwhile (another insert of the same
// content met it unfinished and wrote it again, which leaves the inode
// created here without a name to link), it is imported as importFile does.
func (s *Store) importFresh(dev *device, e *Entry, dst string, unshared bool) error {
	perm := e.Perm(s.umask)
	objPerm := objectPerm(perm)

	if objPerm == perm && !unshared && s.method == Auto && !dev.noNewLinks.Load() {
		err := os.Link(s.objectPath(&e.Hash, objPerm), dst)

		switch {
		case err == nil:
			return nil
		case linkUnsupported(err):
			dev.noNewLinks.Store(true)
		case !errors.Is(err, fs.ErrNotExist):
			return err
		}
	}

	return s.importFile(dev, e, dst, unshared)
}

// importFile creates dst from e's object, healing the object once if it
// is missing or no longer matches its stamp. An object that still fails
// after healing (rewritten again meanwhile) is reported missing, so that
// the caller inserts the release again.
func (s *Store) importFile(dev *device, e *Entry, dst string, unshared bool) error {
	perm := e.Perm(s.umask)
	objPerm := objectPerm(perm)
	obj := s.objectPath(&e.Hash, objPerm)
	want := stampOf(e.Size, objPerm, &e.Hash)
	linkable := objPerm == perm && !unshared

	err := s.importObject(dev, obj, dst, perm, linkable, want)
	if !stale(err) {
		return err
	}

	if herr := s.heal(e, objPerm); herr != nil {
		return herr
	}

	if err := s.importObject(dev, obj, dst, perm, linkable, want); !stale(err) {
		return err
	}

	return &MissingError{Path: obj}
}

// stale reports an object that failed its stamp check or is gone.
func stale(err error) bool {
	return errors.Is(err, errStale) || errors.Is(err, fs.ErrNotExist)
}

// importObject creates dst with permission bits perm from the object obj
// with the device's method (importWith), checking the object against its
// stamp.
func (s *Store) importObject(dev *device, obj, dst string, perm fs.FileMode, linkable bool, want stamp) error {
	return s.importWith(dev, storedObject{s: s, path: obj, want: want}, dst, perm, linkable)
}

// objectSource is an object a package file is created from, by each
// method.
type objectSource interface {
	clone(dst string, perm fs.FileMode) error
	link(dst string) error
	copy(dst string, perm fs.FileMode) error
}

// storedObject is an object in the store, found by its name: every import
// from it checks it against its stamp.
type storedObject struct {
	s    *Store
	path string
	want stamp
}

func (o storedObject) clone(dst string, perm fs.FileMode) error {
	return cloneObject(o.path, dst, perm, o.s.umask, o.want)
}

func (o storedObject) link(dst string) error {
	return linkObject(o.path, dst, o.want)
}

func (o storedObject) copy(dst string, perm fs.FileMode) error {
	return o.s.copyObject(o.path, dst, perm, o.want)
}

// importWith creates dst with permission bits perm from src with the
// device's method, moving the device down to the next method when the
// filesystem refuses one (under Auto). linkable says a hardlink may be
// used: it carries the right mode and the package allows sharing; else a
// device that hardlinks copies.
func (s *Store) importWith(dev *device, src objectSource, dst string, perm fs.FileMode, linkable bool) error {
	for {
		switch m := dev.get(); {
		case m == Clone:
			err := src.clone(dst, perm)
			if !errors.Is(err, errUnsupported) {
				return err
			}

			if s.method != Auto {
				return fmt.Errorf("store: cannot clone %s: the filesystem does not support reflinks", dst)
			}

			dev.demote(Clone, Hardlink)
		case m == Hardlink && linkable:
			err := src.link(dst)

			switch {
			case err == nil, stale(err):
				return err
			case linkLimit(err):
				// This object has as many links as the filesystem allows.
				return src.copy(dst, perm)
			case !linkUnsupported(err):
				return err
			case s.method != Auto:
				return fmt.Errorf("store: cannot hardlink %s: %w", dst, err)
			}

			dev.demote(Hardlink, Copy)
		default:
			return src.copy(dst, perm)
		}
	}
}

// linkObject creates dst as a hardlink to the object obj and checks the
// inode it got against the object's stamp (one lstat, after the link, so
// that no content written before the link completed passes).
func linkObject(obj, dst string, want stamp) error {
	if err := os.Link(obj, dst); err != nil {
		return err
	}

	st, err := lstat(dst)
	if err == nil {
		err = want.check(st)
	}

	if err != nil {
		_ = os.Remove(dst)
	}

	return err
}

// copyObject creates dst as a copy of the object obj (copy_file_range
// where available) carrying the object's stamp as its modification time.
// The object is checked against its stamp after the copy: an object can
// be written in place through a package file hard-linked to it, and any
// write, before or during the copy, moves its modification time.
func (s *Store) copyObject(obj, dst string, perm fs.FileMode, want stamp) error {
	in, err := openShared(obj)
	if err != nil {
		return err
	}

	defer func() { _ = in.Close() }()

	out, err := fsstate.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}

	_, err = io.Copy(out, in)
	if err == nil && chmodAfterCreate(perm, s.umask) {
		err = out.Chmod(perm)
	}

	if err == nil {
		err = setMtime(out, want.mtime)
	}

	if cerr := out.Close(); err == nil {
		err = cerr
	}

	if err == nil {
		err = checkOpen(in, want)
	}

	if err != nil {
		_ = os.Remove(dst)
	}

	return err
}

// checkOpen checks an open object against its stamp.
func checkOpen(f *os.File, want stamp) error {
	st, err := fstat(f)
	if err != nil {
		return err
	}

	return want.check(st)
}

// removeTree removes path even where it holds directories without write or
// search permission.
func removeTree(path string) error {
	err := os.RemoveAll(path)
	if err == nil {
		return nil
	}

	_ = filepath.WalkDir(path, func(p string, d fs.DirEntry, _ error) error {
		if d != nil && d.IsDir() {
			_ = os.Chmod(p, 0o700) //nolint:gosec // the tree is being deleted.
		}

		return nil
	})

	return os.RemoveAll(path)
}
