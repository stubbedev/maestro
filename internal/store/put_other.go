//go:build !linux

package store

import "io/fs"

// putObject stores data as the object for sum and perm unless an intact
// one is there already (writeObject).
func (s *Store) putObject(data []byte, sum *[32]byte, perm fs.FileMode) error {
	return s.writeObject(s.objectPath(sum, perm), data, sum, perm)
}

// putImport stores e's content data (putObject) and creates the package
// file dst from its object (importFile).
func (s *Store) putImport(dev *device, e *Entry, data []byte, dst string, unshared bool) error {
	if err := s.putObject(data, &e.Hash, objectPerm(e.Perm(s.umask))); err != nil {
		return err
	}

	return s.importFile(dev, e, dst, unshared)
}
