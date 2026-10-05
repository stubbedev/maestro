package archivetest

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"hash/crc32"
	"io/fs"
)

// Zip hosts ("version made by" high byte).
const (
	HostFAT  = 0
	HostUnix = 3
	HostNTFS = 11
)

// ZipEntry is one member of a crafted zip, written exactly as described:
// no field is filled in or corrected.
type ZipEntry struct {
	Name       string
	Data       string
	Extra      []byte
	Attr       uint32
	Flags      uint16
	Host       byte
	HostVer    byte
	Deflate    bool
	Descriptor bool
	// Link points this central record at the local header of an earlier
	// entry (index+1), to craft overlapping members.
	Link int
}

// UnixFile is a regular file made on Unix with the given mode (permission
// and set-id bits).
func UnixFile(name string, mode fs.FileMode, data string) ZipEntry {
	return ZipEntry{Name: name, Data: data, Host: HostUnix, HostVer: 30, Attr: (0o100000 | unixBits(mode)) << 16, Deflate: true}
}

// UnixDir is a directory entry made on Unix.
func UnixDir(name string, mode fs.FileMode) ZipEntry {
	return ZipEntry{Name: name, Host: HostUnix, HostVer: 30, Attr: (0o040000|unixBits(mode))<<16 | 0x10}
}

// UnixLink is a symlink made on Unix.
func UnixLink(name, target string) ZipEntry {
	return ZipEntry{Name: name, Data: target, Host: HostUnix, HostVer: 30, Attr: 0o120777 << 16}
}

// FatFile is a file made on MS-DOS/Windows with the given attribute byte
// (0x01 read-only, 0x10 directory, 0x20 archive).
func FatFile(name string, attr uint32, data string) ZipEntry {
	return ZipEntry{Name: name, Data: data, Host: HostFAT, HostVer: 20, Attr: attr, Deflate: true}
}

func unixBits(mode fs.FileMode) uint32 {
	bits := uint32(mode.Perm())
	if mode&fs.ModeSetuid != 0 {
		bits |= 0o4000
	}

	if mode&fs.ModeSetgid != 0 {
		bits |= 0o2000
	}

	if mode&fs.ModeSticky != 0 {
		bits |= 0o1000
	}

	return bits
}

// Zip writes a zip archive of the entries, with comment as the archive
// comment.
func Zip(comment string, entries ...ZipEntry) []byte {
	var out, cd bytes.Buffer

	offsets := make([]uint32, len(entries))

	le := func(b *bytes.Buffer, vs ...any) {
		for _, v := range vs {
			_ = binary.Write(b, binary.LittleEndian, v)
		}
	}

	for i, e := range entries {
		data := []byte(e.Data)
		crc := crc32.ChecksumIEEE(data)
		method := uint16(0)

		if e.Deflate {
			var z bytes.Buffer

			w, _ := flate.NewWriter(&z, flate.BestCompression)
			_, _ = w.Write(data)
			_ = w.Close()
			data, method = z.Bytes(), 8
		}

		flags := e.Flags
		if e.Descriptor {
			flags |= 8
		}

		if e.Link > 0 {
			offsets[i] = offsets[e.Link-1]
		} else {
			offsets[i] = uint32(out.Len()) //nolint:gosec // test archives are small.

			lcrc, lcsize, lusize := crc, uint32(len(data)), uint32(len(e.Data)) //nolint:gosec // as above.
			if e.Descriptor {
				lcrc, lcsize, lusize = 0, 0, 0
			}

			le(&out, uint32(0x04034b50), uint16(20), flags, method, uint16(0), uint16(0x21), lcrc, lcsize, lusize,
				uint16(len(e.Name)), uint16(len(e.Extra))) //nolint:gosec // as above.
			out.WriteString(e.Name)
			out.Write(e.Extra)
			out.Write(data)

			if e.Descriptor {
				le(&out, uint32(0x08074b50), crc, uint32(len(data)), uint32(len(e.Data))) //nolint:gosec // as above.
			}
		}

		le(&cd, uint32(0x02014b50), uint16(e.Host)<<8|uint16(e.HostVer), uint16(20), flags, method, uint16(0), uint16(0x21),
			crc, uint32(len(data)), uint32(len(e.Data)), uint16(len(e.Name)), uint16(len(e.Extra)), uint16(0), //nolint:gosec // as above.
			uint16(0), uint16(0), e.Attr, offsets[i])
		cd.WriteString(e.Name)
		cd.Write(e.Extra)
	}

	cdOffset := out.Len()
	out.Write(cd.Bytes())
	le(&out, uint32(0x06054b50), uint16(0), uint16(0), uint16(len(entries)), uint16(len(entries)), //nolint:gosec // as above.
		uint32(cd.Len()), uint32(cdOffset), uint16(len(comment))) //nolint:gosec // as above.
	out.WriteString(comment)

	return out.Bytes()
}

// UnicodePath is an Info-ZIP Unicode Path extra field giving name as the
// UTF-8 name of an entry whose stored name is stored.
func UnicodePath(stored, name string) []byte {
	var b bytes.Buffer

	_ = binary.Write(&b, binary.LittleEndian, uint16(0x7075))
	_ = binary.Write(&b, binary.LittleEndian, uint16(5+len(name))) //nolint:gosec // short names.
	b.WriteByte(1)
	_ = binary.Write(&b, binary.LittleEndian, crc32.ChecksumIEEE([]byte(stored)))
	b.WriteString(name)

	return b.Bytes()
}
