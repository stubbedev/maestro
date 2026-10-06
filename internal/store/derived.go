package store

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"
)

// Derived data is what other parts of maestro work out from a release's
// files and keep beside it, so that later runs need not read the files
// again (the autoload dump's parse results). The store treats it as
// opaque: it is named by the release and a name, written atomically, and
// removed by Prune with the release.
//
//	derived/<2 hex>/<62 hex>.<name>   the release's index name, then the name

// derivedPath is where data name of release r is kept.
func (s *Store) derivedPath(r *Release, name string) (string, error) {
	if name == "" || strings.Trim(name, "abcdefghijklmnopqrstuvwxyz0123456789-") != "" {
		return "", fmt.Errorf("store: invalid derived data name %q", name)
	}

	return s.shardPath(s.derived, &r.id, "."+name), nil
}

// ReadDerived returns the data kept as name for release r (WriteDerived),
// or nil when there is none.
func (s *Store) ReadDerived(r *Release, name string) ([]byte, error) {
	path, err := s.derivedPath(r, name)
	if err != nil {
		return nil, err
	}

	f, err := openShared(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	defer func() { _ = f.Close() }()

	st, err := fstat(f)
	if err != nil {
		return nil, err
	}

	data := make([]byte, st.size)
	if _, err := f.ReadAt(data, 0); err != nil {
		return nil, err
	}

	return data, nil
}

// WriteDerived keeps data as name for release r, replacing what was kept.
func (s *Store) WriteDerived(r *Release, name string, data []byte) error {
	path, err := s.derivedPath(r, name)
	if err != nil {
		return err
	}

	unlock, err := s.lock(false)
	if err != nil {
		return err
	}

	defer unlock()

	return s.intoShard(2, &r.id, path, func() error { return s.writeAtomic(path, data) })
}

// pruneDerived removes the derived data of releases not in keep (index
// names, <2 hex>/<62 hex>).
func (s *Store) pruneDerived(keep map[string]struct{}) error {
	err := walkShards(s.derived, func(shard, name, path string) error {
		stem, _, _ := strings.Cut(name, ".")
		if _, ok := keep[shard+"/"+stem]; ok {
			return nil
		}

		return remove(path)
	})
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}

	return err
}
