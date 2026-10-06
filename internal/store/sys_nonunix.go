//go:build !unix

package store

import (
	"io/fs"
	"os"
	"time"
)

func setMtime(f *os.File, t int64) error {
	return os.Chtimes(f.Name(), time.Unix(t, 0), time.Unix(t, 0))
}

// lockFile is a no-op without flock: pruning then relies on its grace
// period alone.
func lockFile(*os.File, bool) error {
	return nil
}

func processUmask() fs.FileMode {
	return 0
}
