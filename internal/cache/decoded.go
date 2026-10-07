// Ports nothing: JSON documents kept decoded between runs, so that a run
// reads them back instead of decoding their JSON again (deliberate
// deviation 3, speed).

package cache

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"time"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util/fsstate"
)

// Decoded keeps, in a directory, the decoded form (php.AppendBinary) of
// JSON documents for later runs, one slot per source of a document. A slot
// holds a copy of the JSON it was decoded from and is only read back for
// that exact JSON, so what it gives is what decoding the JSON gives.
// (Comparing the copy costs far less than hashing the JSON.) A slot is
// overwritten when its source's JSON changes.
type Decoded struct {
	// magic starts a slot; it changes with the binary form.
	magic string
	// minSize is the size under which JSON is decoded at once: reading a
	// small slot back costs more than decoding it.
	minSize int
	// maxSlots bounds the slots kept, the least recently written going
	// first; 0 bounds nothing.
	maxSlots int

	dir atomic.Pointer[string]
}

// NewDecoded returns a Decoded keeping nothing until Use.
func NewDecoded(magic string, minSize, maxSlots int) *Decoded {
	return &Decoded{magic: magic, minSize: minSize, maxSlots: maxSlots}
}

// Use keeps the slots in dir; "" keeps none. It may be called again
// while documents are decoded.
func (d *Decoded) Use(dir string) { d.dir.Store(&dir) }

// Decode returns what decode gives for json, the document of source, read
// back from source's slot when it holds json. Otherwise, when decode gave
// a value, store (non-nil) keeps it in the slot: call it before anything
// may change the value.
func (d *Decoded) Decode(source, json string, decode func(string) (any, error)) (v any, store func(), err error) {
	var dir string
	if p := d.dir.Load(); p != nil {
		dir = *p
	}
	if dir == "" || len(json) < d.minSize {
		v, err = decode(json)

		return v, nil, err
	}

	slot := sha256.Sum256([]byte(source))
	path := filepath.Join(dir, hex.EncodeToString(slot[:16])+".bin")
	// the slot: magic, the JSON's length (uvarint), the JSON, its decoded
	// form
	header := binary.AppendUvarint([]byte(d.magic), uint64(len(json)))
	if data, err := os.ReadFile(path); err == nil {
		start := len(header) + len(json)
		if len(data) > start && bytes.Equal(data[:len(header)], header) && string(data[len(header):start]) == json {
			if v, err := php.DecodeBinary(data[start:]); err == nil {
				return v, nil, nil
			}
		}
	}

	if v, err = decode(json); err != nil {
		return v, nil, err
	}

	return v, func() {
		data := make([]byte, 0, len(header)+2*len(json))
		data = append(append(data, header...), json...)
		if data, ok := php.AppendBinary(data, v); ok && fsstate.WriteAtomic(path, data) == nil {
			d.bound(dir)
		}
	}, nil
}

// bound removes the least recently written slots beyond maxSlots.
func (d *Decoded) bound(dir string) {
	if d.maxSlots <= 0 {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) <= d.maxSlots {
		return
	}
	type slot struct {
		name  string
		mtime time.Time
	}
	slots := make([]slot, 0, len(entries))
	for _, e := range entries {
		if info, err := e.Info(); err == nil && info.Mode().IsRegular() && !fsstate.IsTemp(e.Name()) {
			slots = append(slots, slot{e.Name(), info.ModTime()})
		}
	}
	slices.SortFunc(slots, func(a, b slot) int { return a.mtime.Compare(b.mtime) })
	for _, s := range slots[:max(len(slots)-d.maxSlots, 0)] {
		_ = os.Remove(filepath.Join(dir, s.name))
	}
}

// GcDecoded removes the slots under root not written for ttl seconds. A
// slot is written when its document is first decoded after a change, so
// it ages with that document; slots of another version are aged the same
// way.
func GcDecoded(root string, ttl int) error {
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
