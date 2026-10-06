package vcs

import (
	"bytes"
	"crypto/sha1" //nolint:gosec // git's index checksum.
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// errIndexFallback means refreshIndex cannot rewrite this index itself:
// `git update-index --refresh` has to.
var errIndexFallback = errors.New("index needs git to refresh")

// refreshIndex rewrites the stat data of every entry of the work tree's
// .git/index from lstat, as `git update-index --refresh` would after
// finding every file unchanged, and recomputes the checksum (kept zero
// under index.skipHash). Index versions 2 to 4 with SHA-1 object names
// are handled; anything else refresh would treat specially (SHA-256
// repositories, gitlinks, staged, assume-unchanged, skip-worktree and
// intent-to-add entries, extensions other than the cache tree, resolve
// undo and the end-of-index ones) fails with errIndexFallback.
func refreshIndex(workTree string) error {
	gitDir := filepath.Join(workTree, ".git")

	config, err := os.ReadFile(filepath.Join(gitDir, "config"))
	if err != nil {
		return err
	}

	if strings.Contains(strings.ToLower(string(config)), "objectformat") {
		return errIndexFallback
	}

	indexPath := filepath.Join(gitDir, "index")

	data, err := os.ReadFile(indexPath)
	if err != nil {
		return err
	}

	if err := restatIndex(data, workTree); err != nil {
		return err
	}

	info, err := os.Lstat(indexPath)
	if err != nil {
		return err
	}

	lock := indexPath + ".lock"
	if err := os.WriteFile(lock, data, info.Mode().Perm()); err != nil {
		_ = os.Remove(lock)
		return err
	}

	return os.Rename(lock, indexPath)
}

// index entry layout: ctime, mtime (seconds, nanoseconds), dev, ino, mode,
// uid, gid, size (32 bits each), SHA-1, flags (16 bits).
const (
	entryStat  = 40
	entryFixed = entryStat + sha1.Size + 2
	flagValid  = 0x8000
	flagExtend = 0x4000
	flagStage  = 0x3000
	flagName   = 0x0fff
)

// restatIndex rewrites the stat data in the index data in place.
func restatIndex(data []byte, workTree string) error {
	if len(data) < 12+sha1.Size || string(data[:4]) != "DIRC" {
		return errIndexFallback
	}

	version := binary.BigEndian.Uint32(data[4:8])
	if version < 2 || version > 4 {
		return errIndexFallback
	}

	count := binary.BigEndian.Uint32(data[8:12])
	end := len(data) - sha1.Size
	off := 12

	var name []byte

	for range count {
		if off+entryFixed > end {
			return errIndexFallback
		}

		e := data[off:]
		mode := binary.BigEndian.Uint32(e[24:28])
		flags := binary.BigEndian.Uint16(e[60:62])

		if flags&(flagValid|flagExtend|flagStage) != 0 || mode>>12 == 0o16 {
			// assume-unchanged, extended flags, staged, gitlink
			return errIndexFallback
		}

		pos := off + entryFixed

		if version == 4 {
			strip, n := indexVarint(data[pos:end])
			if n <= 0 || strip > uint64(len(name)) {
				return errIndexFallback
			}

			nul := bytes.IndexByte(data[pos+n:end], 0)
			if nul < 0 {
				return errIndexFallback
			}

			name = append(name[:uint64(len(name))-strip], data[pos+n:pos+n+nul]...)
			off = pos + n + nul + 1
		} else {
			nameLen := int(flags & flagName)
			if nameLen == flagName {
				nameLen = bytes.IndexByte(data[pos:end], 0)
			}

			if nameLen < 0 || pos+nameLen > end {
				return errIndexFallback
			}

			name = append(name[:0], data[pos:pos+nameLen]...)
			off += (entryFixed + nameLen + 8) &^ 7
		}

		if err := statEntry(filepath.Join(workTree, filepath.FromSlash(string(name))), e[:entryStat]); err != nil {
			return err
		}
	}

	// extensions: signature, size, data
	for off < end {
		if off+8 > end {
			return errIndexFallback
		}

		switch string(data[off : off+4]) {
		case "TREE", "REUC", "EOIE", "IEOT":
		default:
			return errIndexFallback
		}

		off += 8 + int(binary.BigEndian.Uint32(data[off+4:off+8]))
	}

	if off != end {
		return errIndexFallback
	}

	if trailer := data[end:]; !bytes.Equal(trailer, make([]byte, sha1.Size)) {
		sum := sha1.Sum(data[:end]) //nolint:gosec // git's index checksum.
		copy(trailer, sum[:])
	}

	return nil
}

// indexVarint decodes git's offset varint (index v4 name prefix lengths):
// each continuation adds one before shifting.
func indexVarint(b []byte) (uint64, int) {
	var v uint64

	for i, c := range b {
		if i > 0 {
			v++
		}

		v = v<<7 | uint64(c&0x7f)

		if c&0x80 == 0 {
			return v, i + 1
		}

		if i >= 9 {
			break
		}
	}

	return 0, 0
}
