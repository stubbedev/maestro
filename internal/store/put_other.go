//go:build !linux

package store

import "io/fs"

// putObject stores data as the object for sum and perm unless an intact
// one is there already (writeObject). fresh, that this call created it,
// is always false here: the objects are imported as any other.
func (s *Store) putObject(data []byte, sum *[32]byte, perm fs.FileMode) (fresh bool, err error) {
	return false, s.writeObject(s.objectPath(sum, perm), data, sum, perm)
}
