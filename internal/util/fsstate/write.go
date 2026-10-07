// Ports nothing: how maestro's own caches (deviation 3, speed) write their
// files. Composer's shared caches (repository metadata, dist files, VCS
// mirrors) keep Composer's layout and do not go through here.

package fsstate

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// TempPrefix starts the name of a file WriteAtomic is writing.
const TempPrefix = ".tmp-"

// TempMaxAge is how old a file WriteAtomic is writing is when the process
// writing it is taken to have died: a cache pruning its directory removes
// it then, and leaves a younger one to its writer.
const TempMaxAge = 10 * time.Minute

// IsTemp reports whether name, a file's base name, is that of a file
// WriteAtomic is (or was, before its writer died) writing.
func IsTemp(name string) bool { return strings.HasPrefix(name, TempPrefix) }

// WriteAtomic replaces the file at path with data (see WriteAtomicFunc).
func WriteAtomic(path string, data []byte) error {
	return WriteAtomicFunc(path, func(w io.Writer) error {
		_, err := w.Write(data)

		return err
	})
}

// WriteAtomicFunc replaces the file at path with what write writes,
// atomically: a reader sees the old file or the new one, never part of
// either. It writes a temporary file (TempPrefix) in path's directory,
// creating the directory as needed (0777 less the umask), and renames it
// over path. The file is created 0666 less the umask, as os.WriteFile and
// Composer's Cache::write leave theirs, so a cache shared between users
// stays readable. On failure path is left as it was and the temporary
// file is removed.
func WriteAtomicFunc(path string, write func(w io.Writer) error) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o777); err != nil {
		return err
	}
	f, err := createTemp(dir)
	if err != nil {
		return err
	}
	err = write(f)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		_ = os.Remove(f.Name())
	}

	return err
}

// tempSeq makes the names createTemp tries unique within the process; the
// process id makes them unique across processes sharing a directory.
var tempSeq atomic.Uint64

// createTemp creates a new file named TempPrefix plus a unique suffix in
// dir, 0666 less the umask (os.CreateTemp's are 0600).
func createTemp(dir string) (*os.File, error) {
	for {
		name := filepath.Join(dir, TempPrefix+pid+"-"+strconv.FormatUint(tempSeq.Add(1), 36))
		f, err := OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o666)
		if !errors.Is(err, fs.ErrExist) {
			return f, err
		}
	}
}
