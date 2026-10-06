// Ports nothing: cached metadata files kept decoded between runs, so that
// a run reads them back instead of decoding their JSON again (deliberate
// deviation 3, speed).

package composerrepo

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/stubbedev/maestro/internal/php"
)

// decodedCacheDir is where decoded metadata files are kept (nil or "" for
// nowhere); see UseDecodedCache. It is read from the speculation's
// goroutines, and a later factory call may set it again.
var decodedCacheDir atomic.Pointer[string]

// UseDecodedCache keeps, under root (cache.DecodedMetadata), the decoded
// form of the cached metadata files the repositories decode, for later
// runs; "" keeps none (the default). Call it before repositories load
// metadata. ClearDecodedCache and GcDecodedCache clean root up.
//
// The decoded form of a file (php.AppendBinary) is stored after a copy of
// the JSON it was decoded from, and only read back for that exact JSON, so
// what it gives is what decoding the JSON gives. (Comparing the copy costs
// far less than hashing the JSON.) Each cached file has one slot,
// overwritten when its JSON changes.
func UseDecodedCache(root string) {
	dir := root
	if root != "" {
		dir = filepath.Join(root, decodedVersion)
	}
	decodedCacheDir.Store(&dir)
}

// decodedVersion is the directory, under the root, of the slots of the
// current form; it changes with decodedMagic.
const decodedVersion = "v1"

// decodedMagic starts a decoded file; its version changes with the
// binary form.
const decodedMagic = "maestro-p2-v1\n"

// decodedMinSize is the size under which JSON is decoded at once: reading
// a small file back costs more than decoding it.
const decodedMinSize = 4096

// decodeCached is decodeArray(json) for json, the contents of the file
// cached under cacheKey, read back from the decoded cache when it holds
// them. Else store (non-nil) stores the decoded file there; call it before
// anything may change the array.
func (r *ComposerRepository) decodeCached(cacheKey, json string) (data *php.Array, store func()) {
	var dir string
	if p := decodedCacheDir.Load(); p != nil {
		dir = *p
	}
	if dir == "" || len(json) < decodedMinSize || r.cache == nil || r.cache.Root() == "" {
		return decodeArray(json), nil
	}

	slot := sha256.Sum256([]byte(r.cache.Root() + "\x00" + cacheKey))
	path := filepath.Join(dir, hex.EncodeToString(slot[:16])+".bin")
	// the file: magic, the JSON's length (uvarint), the JSON, its decoded
	// form
	header := binary.AppendUvarint([]byte(decodedMagic), uint64(len(json)))
	if data, err := os.ReadFile(path); err == nil {
		start := len(header) + len(json)
		if len(data) > start && bytes.Equal(data[:len(header)], header) && string(data[len(header):start]) == json {
			if v, err := php.DecodeBinary(data[start:]); err == nil {
				if a, ok := v.(*php.Array); ok {
					return a, nil
				}
			}
		}
	}

	a := decodeArray(json)
	if a == nil {
		return nil, nil
	}

	return a, func() {
		data := make([]byte, 0, len(header)+2*len(json))
		data = append(append(data, header...), json...)
		if data, ok := php.AppendBinary(data, a); ok {
			writeDecoded(dir, path, data)
		}
	}
}

// writeDecoded stores data at path, atomically: readers see the old file
// or the new one. A failure leaves the slot as it was.
func writeDecoded(dir, path string, data []byte) {
	if err := os.MkdirAll(dir, 0o777); err != nil {
		return
	}
	f, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return
	}
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		_ = os.Remove(f.Name())
	}
}

// ClearDecodedCache removes the decoded cache under root, every version
// of it: clear-cache does it when it clears the repository cache the
// slots were decoded from.
func ClearDecodedCache(root string) error {
	return os.RemoveAll(root)
}

// GcDecodedCache removes the files under root not written for ttl
// seconds: clear-cache --gc does it when it collects the repository
// cache, whose files Cache.Gc ages from their last write too. A slot is
// written when its file is first decoded after a change, so it ages with
// that file; slots of another version are aged the same way.
func GcDecodedCache(root string, ttl int) error {
	dir, err := os.OpenRoot(root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()

	expire := time.Now().Add(-time.Duration(ttl) * time.Second)

	return fs.WalkDir(dir.FS(), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if fi, err := d.Info(); err == nil && fi.ModTime().Before(expire) {
			if err := dir.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
		}

		return nil
	})
}
