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
// is only read back for the exact JSON it was decoded from, so what it
// gives is what decoding the JSON gives. It tells that JSON in one of two
// ways:
//
//   - by the identity of the file the JSON was read from (Origin), when
//     that identity could be trusted when the JSON was read
//     (fsstate.ID.Trusted: the file's times were older than a timestamp
//     tick). Any later change to the file changes its identity: a write
//     sets the change time to the time of the write, which is past the
//     one the file had then, and a rename brings another inode. So JSON
//     read later from a file of the same identity is the same JSON.
//   - otherwise by a copy of the JSON, compared in full.
//
// A slot is overwritten when its source's JSON changes.
type Decoded struct {
	// magic starts a slot; it changes with the slot's form.
	magic string
	// minSize is the size under which JSON is decoded at once: reading a
	// small slot back costs more than decoding it.
	minSize int
	// maxSlots bounds the slots kept, the least recently written going
	// first; 0 bounds nothing.
	maxSlots int
	// margin is what origins are trusted with (zero: the default).
	margin fsstate.Margin

	dir atomic.Pointer[string]
}

// How a slot tells the JSON it was decoded from, after the magic.
const (
	// slotByOrigin: the origin's ID (fsstate.ID.AppendBinary), then the
	// JSON's length (uvarint)
	slotByOrigin byte = 'o'
	// slotByCopy: the JSON's length (uvarint), then the JSON
	slotByCopy byte = 'c'
)

// NewDecoded returns a Decoded keeping nothing until Use.
func NewDecoded(magic string, minSize, maxSlots int) *Decoded {
	return &Decoded{magic: magic, minSize: minSize, maxSlots: maxSlots}
}

// Use keeps the slots in dir; "" keeps none. It may be called again
// while documents are decoded.
func (d *Decoded) Use(dir string) { d.dir.Store(&dir) }

// Decode returns what decode gives for json, the document of source read
// from origin (the zero Origin when not known), read back from source's
// slot when it holds json. Otherwise, when decode gave a value, store
// (non-nil) keeps it in the slot: call it before anything may change the
// value. store also rewrites a slot read back by its copy of the JSON
// that its origin can tell from now on.
func (d *Decoded) Decode(source string, origin Origin, json string, decode func(string) (any, error)) (v any, store func(), err error) {
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
	byOrigin := origin.ok && origin.id.Trusted(origin.seen, d.margin)
	if data, err := os.ReadFile(path); err == nil {
		if form, decoded, ok := d.holds(data, origin, json); ok {
			if v, err := php.DecodeBinary(decoded); err == nil {
				if form == slotByCopy && byOrigin {
					// told by its origin from now on, without the copy
					return v, func() { d.write(dir, path, d.slotHeader(origin, json, true), decoded) }, nil
				}

				return v, nil, nil
			}
		}
	}

	if v, err = decode(json); err != nil {
		return v, nil, err
	}

	return v, func() {
		if data, ok := php.AppendBinary(d.slotHeader(origin, json, byOrigin), v); ok {
			d.write(dir, path, data, nil)
		}
	}, nil
}

// slotHeader is the start of a slot for json read from origin: the magic
// and what tells the JSON, by origin or by a copy.
func (d *Decoded) slotHeader(origin Origin, json string, byOrigin bool) []byte {
	data := []byte(d.magic)
	if byOrigin {
		data = origin.id.AppendBinary(append(data, slotByOrigin))

		return binary.AppendUvarint(data, uint64(len(json)))
	}
	data = binary.AppendUvarint(append(data, slotByCopy), uint64(len(json)))

	return append(data, json...)
}

// write replaces the slot at path with header followed by decoded, then
// bounds the slots of dir.
func (d *Decoded) write(dir, path string, header, decoded []byte) {
	if fsstate.WriteAtomic(path, append(header, decoded...)) == nil {
		d.bound(dir)
	}
}

// holds returns how data, a slot, tells the JSON it was decoded from, and
// its decoded form, when it was decoded from json, read from origin; ok is
// false otherwise.
func (d *Decoded) holds(data []byte, origin Origin, json string) (form byte, decoded []byte, ok bool) {
	rest, ok := bytes.CutPrefix(data, []byte(d.magic))
	if !ok || len(rest) == 0 {
		return 0, nil, false
	}
	form, r := rest[0], bytes.NewReader(rest[1:])
	if form == slotByOrigin {
		id, err := fsstate.ReadBinary(r)
		if err != nil || !origin.ok || id != origin.id {
			return 0, nil, false
		}
	} else if form != slotByCopy {
		return 0, nil, false
	}
	if n, err := binary.ReadUvarint(r); err != nil || n != uint64(len(json)) {
		return 0, nil, false
	}
	rest = rest[len(rest)-r.Len():]
	if form == slotByCopy {
		if len(rest) < len(json) || string(rest[:len(json)]) != json {
			return 0, nil, false
		}
		rest = rest[len(json):]
	}

	return form, rest, true
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
