package store

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/stubbedev/maestro/internal/archive"
)

// errUnsupported means the filesystem cannot reflink.
var errUnsupported = errors.New("reflinks unsupported")

// device is the import method chosen for one destination device. Under
// Auto it only ever moves down: clone, hardlink, copy.
type device struct {
	method atomic.Int32
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

	root := &r.entries[0]
	if err := os.Mkdir(tmp, root.Perm(s.umask)|0o700); err != nil {
		return err
	}

	if err := s.build(r.entries, tmp, opts.Unshared); err != nil {
		_ = removeTree(tmp)
		return err
	}

	if err := renameDir(tmp, dst); err != nil {
		_ = removeTree(tmp)
		return err
	}

	if opts.Created != nil {
		base := dst + string(os.PathSeparator)
		for i := 1; i < len(r.entries); i++ {
			if e := &r.entries[i]; e.Kind == archive.File {
				opts.Created(base+filepath.FromSlash(e.Path), e.Hash)
			}
		}
	}

	return nil
}

// dirFix is a directory whose final mode is set once it is filled.
type dirFix struct {
	path string
	mode fs.FileMode
}

// build fills the fresh directory tmp with the release's tree, never
// hardlinking a file when unshared is set.
func (s *Store) build(entries []Entry, tmp string, unshared bool) error {
	st, err := lstat(tmp)
	if err != nil {
		return err
	}

	dev := s.device(st.dev)

	// A setgid bit inherited from the parent stays on every directory.
	sgid := fs.FileMode(0)
	if st.mode&0o2000 != 0 {
		sgid = fs.ModeSetgid
	}

	var fixes []dirFix

	fix := func(path string, perm, created fs.FileMode) {
		if perm != created {
			fixes = append(fixes, dirFix{path: path, mode: perm | sgid})
		}
	}

	fix(tmp, entries[0].Perm(s.umask), st.mode&fs.ModePerm)

	files := make([]int, 0, len(entries))
	base := tmp + string(os.PathSeparator)

	for i := 1; i < len(entries); i++ {
		e := &entries[i]
		path := base + filepath.FromSlash(e.Path)

		switch e.Kind {
		case archive.Dir:
			perm := e.Perm(s.umask)

			created, err := s.mkdir(path, perm|0o700)
			if err != nil {
				return err
			}

			fix(path, perm, created)
		case archive.Symlink:
			if err := os.Symlink(e.Link, path); err != nil {
				return err
			}
		case archive.File:
			files = append(files, i)
		}
	}

	if err := s.importFiles(dev, entries, files, base, unshared); err != nil {
		return err
	}

	// Deepest first: a parent's mode may forbid changing its children.
	for _, f := range slices.Backward(fixes) {
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

// importFiles imports the files, in parallel for larger packages.
func (s *Store) importFiles(dev *device, entries []Entry, files []int, base string, unshared bool) error {
	one := func(i int) error {
		e := &entries[i]
		return s.importFile(dev, e, base+filepath.FromSlash(e.Path), unshared)
	}

	workers := min(s.workers, (len(files)+63)/64)
	if workers <= 1 {
		for _, i := range files {
			if err := one(i); err != nil {
				return err
			}
		}

		return nil
	}

	var (
		next   atomic.Int64
		failed atomic.Bool
		once   sync.Once
		first  error
		wg     sync.WaitGroup
	)

	for range workers {
		wg.Go(func() {
			for !failed.Load() {
				k := next.Add(1) - 1
				if k >= int64(len(files)) {
					return
				}

				if err := one(files[k]); err != nil {
					once.Do(func() { first = err })
					failed.Store(true)
				}
			}
		})
	}

	wg.Wait()

	return first
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
// with the device's method, moving the device down to the next method when
// the filesystem refuses one (under Auto). linkable says a hardlink may be
// used: it carries the right mode and the package allows sharing; else a
// device that hardlinks copies.
func (s *Store) importObject(dev *device, obj, dst string, perm fs.FileMode, linkable bool, want stamp) error {
	for {
		switch m := dev.get(); {
		case m == Clone:
			err := cloneObject(obj, dst, perm, s.umask, want)
			if !errors.Is(err, errUnsupported) {
				return err
			}

			if s.method != Auto {
				return fmt.Errorf("store: cannot clone %s: the filesystem does not support reflinks", dst)
			}

			dev.demote(Clone, Hardlink)
		case m == Hardlink && linkable:
			err := linkObject(obj, dst, want)

			switch {
			case err == nil, stale(err):
				return err
			case linkLimit(err):
				// This object has as many links as the filesystem allows.
				return s.copyObject(obj, dst, perm, want)
			case !linkUnsupported(err):
				return err
			case s.method != Auto:
				return fmt.Errorf("store: cannot hardlink %s: %w", dst, err)
			}

			dev.demote(Hardlink, Copy)
		default:
			return s.copyObject(obj, dst, perm, want)
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
// where available). The object is checked against its stamp before and
// after the copy: an object can be written in place through a package
// file hard-linked to it, and any write moves its modification time.
func (s *Store) copyObject(obj, dst string, perm fs.FileMode, want stamp) error {
	in, err := openShared(obj)
	if err != nil {
		return err
	}

	defer func() { _ = in.Close() }()

	if err := checkOpen(in, want); err != nil {
		return err
	}

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}

	_, err = io.Copy(out, in)
	if err == nil && chmodAfterCreate(perm, s.umask) {
		err = out.Chmod(perm)
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
