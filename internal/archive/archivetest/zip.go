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
	Name    string
	Data    string
	Extra   []byte
	Attr    uint32
	Flags   uint16
	Host    byte
	HostVer byte
	Deflate bool
	// Deflate64 compresses the data with Deflate64 (method 9) instead.
	Deflate64  bool
	Descriptor bool
	// Zip64 writes the sizes as zip64 placeholders (0xffffffff) with
	// their values in a zip64 extra field, in the local and the central
	// header, and the local header's offset too in the central one; a data
	// descriptor then has 64-bit sizes. With a descriptor, the local
	// field holds zeros, as streaming writers leave it.
	Zip64 bool
	// Descriptor32 writes the data descriptor of a Zip64 entry with 32-bit
	// sizes all the same.
	Descriptor32 bool
	// Link points this central record at the local header of an earlier
	// entry (index+1), to craft overlapping members.
	Link int
}

// ZipOptions shape the end of a crafted zip.
type ZipOptions struct {
	// End64 writes a zip64 end record and its locator before the end
	// record.
	End64 bool
	// Saturate writes the end record's counts, size and offset as zip64
	// placeholders, leaving their values to the zip64 end record.
	Saturate bool
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
	return ZipWith(ZipOptions{}, comment, entries...)
}

// zip64Extra is a zip64 extended information extra field of values.
func zip64Extra(values ...uint64) []byte {
	b := binary.LittleEndian.AppendUint16(nil, 0x0001)
	b = binary.LittleEndian.AppendUint16(b, uint16(8*len(values))) //nolint:gosec // a few values

	for _, v := range values {
		b = binary.LittleEndian.AppendUint64(b, v)
	}

	return b
}

// ZipWith is Zip with the end shaped by opts.
func ZipWith(opts ZipOptions, comment string, entries ...ZipEntry) []byte {
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

		if e.Deflate64 {
			data, method = Deflate64(data), 9
		}

		needed := uint16(20)
		if e.Zip64 {
			needed = 45
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
			extra := e.Extra

			if e.Zip64 {
				lcsize, lusize = 0xffffffff, 0xffffffff
				extra = append(zip64Extra(uint64(len(e.Data)), uint64(len(data))), extra...)

				if e.Descriptor {
					extra = append(zip64Extra(0, 0), e.Extra...)
				}
			}

			if e.Descriptor {
				lcrc = 0
				if !e.Zip64 {
					lcsize, lusize = 0, 0
				}
			}

			le(&out, uint32(0x04034b50), needed, flags, method, uint16(0), uint16(0x21), lcrc, lcsize, lusize,
				uint16(len(e.Name)), uint16(len(extra))) //nolint:gosec // as above.
			out.WriteString(e.Name)
			out.Write(extra)
			out.Write(data)

			if e.Descriptor {
				if e.Zip64 && !e.Descriptor32 {
					le(&out, uint32(0x08074b50), crc, uint64(len(data)), uint64(len(e.Data)))
				} else {
					le(&out, uint32(0x08074b50), crc, uint32(len(data)), uint32(len(e.Data))) //nolint:gosec // as above.
				}
			}
		}

		csize, usize, offset, extra := uint32(len(data)), uint32(len(e.Data)), offsets[i], e.Extra //nolint:gosec // as above.
		if e.Zip64 {
			extra = append(zip64Extra(uint64(usize), uint64(csize), uint64(offset)), extra...)
			csize, usize, offset = 0xffffffff, 0xffffffff, 0xffffffff
		}

		le(&cd, uint32(0x02014b50), uint16(e.Host)<<8|uint16(e.HostVer), needed, flags, method, uint16(0), uint16(0x21),
			crc, csize, usize, uint16(len(e.Name)), uint16(len(extra)), uint16(0), //nolint:gosec // as above.
			uint16(0), uint16(0), e.Attr, offset)
		cd.WriteString(e.Name)
		cd.Write(extra)
	}

	cdOffset := out.Len()
	out.Write(cd.Bytes())

	if opts.End64 {
		end64 := out.Len()
		le(&out, uint32(0x06064b50), uint64(44), uint16(3<<8|45), uint16(45), uint32(0), uint32(0),
			uint64(len(entries)), uint64(len(entries)), uint64(cd.Len()), uint64(cdOffset)) //nolint:gosec // as above.
		le(&out, uint32(0x07064b50), uint32(0), uint64(end64), uint32(1)) //nolint:gosec // as above.
	}

	count, cdSize, cdOff := uint16(len(entries)), uint32(cd.Len()), uint32(cdOffset) //nolint:gosec // as above.
	if opts.Saturate {
		count, cdSize, cdOff = 0xffff, 0xffffffff, 0xffffffff
	}

	le(&out, uint32(0x06054b50), uint16(0), uint16(0), count, count, cdSize, cdOff, uint16(len(comment))) //nolint:gosec // as above.
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
