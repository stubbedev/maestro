package fsstate

import (
	"crypto/sha256"
	"encoding/binary"
	"hash"
	"io"
	"os"
)

// KeyHash hashes the parts of a cache entry's key with SHA-256: each
// string prefixed with its length (a uvarint), so that no two lists of
// parts hash alike, and each ID as AppendBinary writes it.
type KeyHash struct {
	h   hash.Hash
	buf []byte
}

// NewKeyHash returns an empty KeyHash.
func NewKeyHash() *KeyHash { return &KeyHash{h: sha256.New()} }

// String adds s.
func (k *KeyHash) String(s string) {
	k.buf = binary.AppendUvarint(k.buf[:0], uint64(len(s)))
	k.h.Write(k.buf)
	_, _ = io.WriteString(k.h, s)
}

// ID adds id.
func (k *KeyHash) ID(id ID) {
	k.buf = id.AppendBinary(k.buf[:0])
	k.h.Write(k.buf)
}

// Sum appends the hash to b.
func (k *KeyHash) Sum(b []byte) []byte { return k.h.Sum(b) }

// IsScript reports whether the open file starts with "#!": a script,
// which runs whatever its interpreter line names (a version manager's
// shim, say), rather than a binary that stays what it is.
func IsScript(f *os.File) (bool, error) {
	var head [2]byte
	if _, err := f.ReadAt(head[:], 0); err != nil {
		return false, err
	}

	return string(head[:]) == "#!", nil
}
