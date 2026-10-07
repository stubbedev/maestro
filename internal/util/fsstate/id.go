// Package fsstate is how maestro's own caches (deviation 3, speed) tell
// whether a file changed since they looked at it, read a file as it was
// at one moment, and replace their files atomically. It ports nothing;
// the content-addressed store's stamps (internal/store) are a different
// thing.
package fsstate

import (
	"encoding/binary"
	"time"
)

// ID is a file's identity as the file system describes it: device and
// inode (a file replaced by a rename gets another), mode, size, and
// modification and change times in nanoseconds (writing to the file or
// changing its mode or links changes the change time). A change to any
// field means the file may have changed; an unchanged ID means it did
// not, unless it changed within a timestamp tick of when the ID was
// taken (see Margin). IDs are known on Unix systems only (Known).
type ID struct {
	Dev   uint64 `json:"dev,omitempty"`
	Ino   uint64 `json:"ino,omitempty"`
	Mode  uint32 `json:"mode,omitempty"`
	Size  int64  `json:"size,omitempty"`
	Mtime int64  `json:"mtime,omitempty"`
	Ctime int64  `json:"ctime,omitempty"`
}

// The file type bits of ID.Mode (st_mode), the same on every Unix.
const (
	modeType    = 0o170000
	modeRegular = 0o100000
)

// IsRegular reports whether the file is a regular file.
func (id ID) IsRegular() bool { return id.Mode&modeType == modeRegular }

// AppendBinary appends id's fields to b as uvarints (the times and size
// as their two's complement), for a cache to write or hash it;
// ReadBinary reads them back.
func (id ID) AppendBinary(b []byte) []byte {
	for _, v := range [...]uint64{
		id.Dev, id.Ino, uint64(id.Mode),
		uint64(id.Size), uint64(id.Mtime), uint64(id.Ctime), //nolint:gosec // read back as int64s
	} {
		b = binary.AppendUvarint(b, v)
	}

	return b
}

// ReadBinary reads an ID AppendBinary wrote from r.
func ReadBinary(r interface{ ReadByte() (byte, error) }) (ID, error) {
	var v [6]uint64
	for i := range v {
		var err error
		if v[i], err = binary.ReadUvarint(r); err != nil {
			return ID{}, err
		}
	}

	return ID{
		Dev: v[0], Ino: v[1], Mode: uint32(v[2]), //nolint:gosec // written from a uint32
		Size: int64(v[3]), Mtime: int64(v[4]), Ctime: int64(v[5]), //nolint:gosec // written from int64s
	}, nil
}

// DefaultMargin is how much older than the moment a file was looked at
// its modification and change times must be for what was seen to be
// trusted later: the file system stamps times from a coarse clock, so a
// file changed within a tick of the look may not show it (git's "racily
// clean" index entries). More than the coarsest granularity in use
// (FAT's two seconds).
const DefaultMargin = 3 * time.Second

// Margin is the margin an owner trusts identities with; the zero value is
// DefaultMargin. Tests set another on the owner (a negative one trusts
// everything).
type Margin time.Duration

// Before returns the time a file's times must be older than for what was
// seen of it at t to be trusted.
func (m Margin) Before(t time.Time) time.Time {
	if m == 0 {
		return t.Add(-DefaultMargin)
	}

	return t.Add(-time.Duration(m))
}

// Trusted reports whether id, seen at t, can be trusted later: neither of
// its times is within margin of t.
func (id ID) Trusted(t time.Time, margin Margin) bool {
	limit := margin.Before(t).UnixNano()

	return id.Mtime < limit && id.Ctime < limit
}
