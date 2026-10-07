package fsstate

import (
	"io/fs"
	"os"
	"strconv"
)

// pid is the process id, as temporary file names carry it: os.Getpid is a
// system call each time.
var pid = strconv.Itoa(os.Getpid())

// Pid is the process id in decimal, for temporary file names unique among
// the processes sharing a directory.
func Pid() string { return pid }

// Open opens the file at path for reading, as os.Open does, without the
// cost os.Open has for a regular file (OpenFile).
func Open(path string) (*os.File, error) { return OpenFile(path, os.O_RDONLY, 0) }

// WriteFile is os.WriteFile through OpenFile.
func WriteFile(path string, data []byte, perm fs.FileMode) error {
	f, err := OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}

	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}

	return err
}
