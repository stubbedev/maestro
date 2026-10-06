package vcs

import (
	"bytes"
	"crypto/sha1" //nolint:gosec // git's index checksum.
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// errIndexFallback means refreshIndex cannot rewrite this index itself:
// `git update-index --refresh` has to.
var errIndexFallback = errors.New("index needs git to refresh")

// refreshIndex rewrites the stat data of every entry of the work tree's
// .git/index from lstat, as `git update-index --refresh` would after
// finding every file unchanged, and recomputes the checksum (kept zero
// under index.skipHash). Index versions 2 to 4 with SHA-1 object names
// are handled, and the empty untracked cache a clone writes under
// core.untrackedCache or feature.manyFiles; anything else refresh would
// treat specially (SHA-256 repositories, gitlinks, staged,
// assume-unchanged, skip-worktree and intent-to-add entries, a populated
// untracked cache, extensions other than the cache tree, resolve undo,
// the untracked cache and the end-of-index ones) fails with
// errIndexFallback.
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

	data, err = restatIndex(data, workTree)
	if err != nil {
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

// restatIndex rewrites the stat data in the index data in place and
// returns the index to write.
func restatIndex(data []byte, workTree string) ([]byte, error) {
	if len(data) < 12+sha1.Size || string(data[:4]) != "DIRC" {
		return nil, errIndexFallback
	}

	version := binary.BigEndian.Uint32(data[4:8])
	if version < 2 || version > 4 {
		return nil, errIndexFallback
	}

	count := binary.BigEndian.Uint32(data[8:12])
	end := len(data) - sha1.Size
	off := 12

	var name []byte

	for range count {
		if off+entryFixed > end {
			return nil, errIndexFallback
		}

		e := data[off:]
		mode := binary.BigEndian.Uint32(e[24:28])
		flags := binary.BigEndian.Uint16(e[60:62])

		if flags&(flagValid|flagExtend|flagStage) != 0 || mode>>12 == 0o16 {
			// assume-unchanged, extended flags, staged, gitlink
			return nil, errIndexFallback
		}

		pos := off + entryFixed

		if version == 4 {
			strip, n := indexVarint(data[pos:end])
			if n <= 0 || strip > uint64(len(name)) {
				return nil, errIndexFallback
			}

			nul := bytes.IndexByte(data[pos+n:end], 0)
			if nul < 0 {
				return nil, errIndexFallback
			}

			name = append(name[:uint64(len(name))-strip], data[pos+n:pos+n+nul]...)
			off = pos + n + nul + 1
		} else {
			nameLen := int(flags & flagName)
			if nameLen == flagName {
				nameLen = bytes.IndexByte(data[pos:end], 0)
			}

			if nameLen < 0 || pos+nameLen > end {
				return nil, errIndexFallback
			}

			name = append(name[:0], data[pos:pos+nameLen]...)
			off += (entryFixed + nameLen + 8) &^ 7
		}

		if err := statEntry(filepath.Join(workTree, filepath.FromSlash(string(name))), e[:entryStat]); err != nil {
			return nil, err
		}
	}

	// extensions: signature, size, data
	out := data[:off:off]
	untracked, eoie := false, false

	for off < end {
		if off+8 > end {
			return nil, errIndexFallback
		}

		sig := string(data[off : off+4])
		next := off + 8 + int(binary.BigEndian.Uint32(data[off+4:off+8]))

		if next < off+8 || next > end {
			return nil, errIndexFallback
		}

		switch sig {
		case "TREE", "REUC", "IEOT":
			out = append(out, data[off:next]...)
		case "EOIE":
			eoie = true
			out = append(out, data[off:next]...)
		case "UNTR":
			ext, err := refreshUntracked(data[off+8:next], workTree)
			if err != nil {
				return nil, err
			}

			untracked = true
			out = binary.BigEndian.AppendUint32(append(out, sig...), uint32(len(ext))) //nolint:gosec // a few hundred bytes.
			out = append(out, ext...)
		default:
			return nil, errIndexFallback
		}

		off = next
	}

	if off != end || untracked && eoie {
		// the end-of-index entry hashes the extension sizes
		return nil, errIndexFallback
	}

	trailer := data[end:]
	out = append(out, trailer...)

	if !bytes.Equal(trailer, make([]byte, sha1.Size)) {
		sum := sha1.Sum(out[:len(out)-sha1.Size]) //nolint:gosec // git's index checksum.
		copy(out[len(out)-sha1.Size:], sum[:])
	}

	return out, nil
}

// untrackedEmpty is an untracked cache extension after its ident: the
// stat data of info/exclude and core.excludesFile (zero: never read),
// dir flags (4 bytes at untrackedFlags), their object names, the
// per-directory exclude file and no directories.
var untrackedEmpty = append(make([]byte, 2*36+4+2*sha1.Size), ".gitignore\x00\x00"...)

const untrackedFlags = 2 * 36

// refreshUntracked returns the untracked cache extension data git writes
// back on refreshing an index holding ext, the empty cache a clone writes
// (core.untrackedCache=true, which feature.manyFiles implies, being the
// only way one appears there): git's add_untracked_cache keeps it while
// its ident names this work tree and system and otherwise starts a new
// empty one with the ident it would use. The flags come from
// status.showUntrackedFiles, which the store key covers, so the stored
// ones are those git would compute. A populated cache or an ident list
// fails with errIndexFallback.
func refreshUntracked(ext []byte, workTree string) ([]byte, error) {
	n, l := indexVarint(ext)
	if l <= 0 || n == 0 || n > uint64(len(ext)-l) { //nolint:gosec // l <= len(ext)
		return nil, errIndexFallback
	}

	ident, rest := ext[l:l+int(n)], ext[l+int(n):] //nolint:gosec // n <= len(ext)
	if bytes.IndexByte(ident, 0) != len(ident)-1 || len(rest) != len(untrackedEmpty) {
		return nil, errIndexFallback
	}

	empty := slices.Clone(untrackedEmpty)
	copy(empty[untrackedFlags:], rest[untrackedFlags:untrackedFlags+4])

	if !bytes.Equal(rest, empty) {
		return nil, errIndexFallback
	}

	want, err := untrackedIdent(workTree)
	if err != nil {
		return nil, err
	}

	want += "\x00"

	out := appendIndexVarint(nil, uint64(len(want)))

	return append(append(out, want...), empty...), nil
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

// appendIndexVarint appends v in git's offset varint encoding.
func appendIndexVarint(b []byte, v uint64) []byte {
	var buf [10]byte

	i := len(buf) - 1
	buf[i] = byte(v & 0x7f)

	for v >>= 7; v != 0; v >>= 7 {
		v--
		i--
		buf[i] = 0x80 | byte(v&0x7f)
	}

	return append(b, buf[i:]...)
}
