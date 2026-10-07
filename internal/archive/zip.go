// Reproduces `unzip -qq <file> -d <dir>` (Info-ZIP UnZip 6.0 with the
// distributions' CVE patches, built for Unix with SYMLINKS, SET_DIR_ATTRIB,
// UNICODE_SUPPORT, USE_BZIP2, USE_DEFLATE64 and ZIP64_SUPPORT), from
// process.c (find_ecrec, find_ecrec64, process_cdir_file_hdr,
// getZip64Data, getUnicodeData), fileio.c (do_string),
// extract.c (extract_or_test_files, store_info,
// extract_or_test_entrylist, the overlap "cover"), unix/unix.c (mapattr,
// mapname, checkdir, close_outfile, set_direc_attribs) and unzpriv.h
// (Ext_ASCII_TO_Native's test).

package archive

import (
	"bufio"
	"bytes"
	"compress/bzip2"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
	"io/fs"
	"runtime"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/klauspost/compress/flate"

	"github.com/stubbedev/maestro/internal/archive/deflate64"
)

const (
	zipSigLocal   = 0x04034b50
	zipSigCentral = 0x02014b50
	zipSigEnd     = 0x06054b50
	zipSigLoc64   = 0x07064b50
	zipSigEnd64   = 0x06064b50
	zipSigDesc    = 0x08074b50

	zipEndLen     = 22
	zipCentralLen = 46
	zipLocalLen   = 30
	zipLoc64Len   = 20
	zipEnd64Len   = 56

	// unzip looks for the end record in the last 66000 bytes.
	zipSearchLen = 66000
	// FILNAMSIZ (PATH_MAX): longer names are truncated with a warning.
	zipFilnamsiz = 4096
	// A symlink's target must fit in PATH_MAX including its NUL, or
	// symlink(2) fails and unzip silently leaves nothing behind.
	zipMaxLink = 4095
	// Bounds the central directory read into memory.
	zipMaxCentral = 256 << 20

	methodStored    = 0
	methodDeflate   = 8
	methodDeflate64 = 9
	methodBzip2     = 12

	// UNZIP_VERSION with ZIP64_SUPPORT and USE_BZIP2.
	unzipVersion = 46
)

// "Version made by" host numbers.
const (
	hostFAT    = 0
	hostAmiga  = 1
	hostVMS    = 2
	hostUnix   = 3
	hostAtari  = 5
	hostHPFS   = 6
	hostNTFS   = 11
	hostQDOS   = 12
	hostAcorn  = 13
	hostBeOS   = 16
	hostTandem = 17
	hostTheos  = 18
	hostAtheOS = 30
	numHosts   = 31
)

// Extra field block IDs.
const (
	efZip64   = 0x0001
	efPKVMS   = 0x000c
	efUnipath = 0x7075
	efASiUnix = 0x756e
)

// zipFile locates one file's data.
type zipFile struct {
	dataStart int64
	csize     int64
	usize     int64
	crc       uint32
	method    uint16
}

// zipEntry is a central directory record, its sizes and offset taken from
// the zip64 extra field where the record saturates them.
type zipEntry struct {
	nameRaw []byte
	extra   []byte
	name    string // after unzip's decoding (do_string + Unicode handling)
	unipath bool   // a Unicode Path extra field gave the name
	offset  int64
	csize   int64
	usize   int64
	extAttr uint32
	crc     uint32
	flags   uint16
	method  uint16
	hostNum byte
	hostVer byte
	// zip64: unzip may read a data descriptor's sizes as 64-bit ones
	// (G.zip64, see extract).
	zip64 bool
}

type zipPlanner struct {
	f      io.ReaderAt
	buf    []byte // local header reads
	b      *builder
	files  []zipFile
	cover  cover
	size   int64
	locale Locale
	// zip64Seen, unipathSeen: an extra field of the archive holds a zip64
	// field, a Unicode Path field.
	zip64Seen, unipathSeen bool
}

func (z *zipPlanner) fail(kind error, code int, entry, reason string, args ...any) *Error {
	e := errorf(Zip, kind, entry, reason, args...)
	e.ExitCode = code

	return e
}

func planZip(f io.ReaderAt, size int64, locale Locale, b *builder) (contentReader, error) {
	z := &zipPlanner{f: f, b: b, size: size, locale: locale}

	entries, err := z.readCentral()
	if err != nil {
		return nil, err
	}

	for i := range entries {
		if err := z.extract(&entries[i]); err != nil {
			return nil, err
		}
	}

	return &zipContent{files: z.files, src: f}, nil
}

// readCentral finds the end record (find_ecrec, find_ecrec64) and reads
// the central directory, refusing every layout unzip only extracts with a
// warning.
func (z *zipPlanner) readCentral() ([]zipEntry, error) {
	n := min(z.size, zipSearchLen)
	tail := make([]byte, n)

	if _, err := z.f.ReadAt(tail, z.size-n); err != nil {
		return nil, err
	}

	// The last signature at least 22 bytes from the end, as unzip finds it.
	at := -1

	for i := len(tail) - zipEndLen; i >= 0; i-- {
		if tail[i] == 'P' && binary.LittleEndian.Uint32(tail[i:]) == zipSigEnd {
			at = i
			break
		}
	}

	if at < 0 {
		return nil, z.fail(ErrCorrupt, 9, "", "End-of-central-directory signature not found.")
	}

	end := tail[at:]
	endOffset := z.size - n + int64(at)
	ec := zipEnd{
		disk:        uint64(binary.LittleEndian.Uint16(end[4:])),
		cdDisk:      uint64(binary.LittleEndian.Uint16(end[6:])),
		diskEntries: uint64(binary.LittleEndian.Uint16(end[8:])),
		total:       uint64(binary.LittleEndian.Uint16(end[10:])),
		cdSize:      uint64(binary.LittleEndian.Uint32(end[12:])),
		cdOffset:    uint64(binary.LittleEndian.Uint32(end[16:])),
	}
	commentLen := int64(binary.LittleEndian.Uint16(end[20:]))

	if endOffset+zipEndLen+commentLen != z.size {
		return nil, z.fail(ErrIrreproducible, pkWarn, "", "the archive comment does not end the file")
	}

	// cdEnd is where the central directory must end (real_ecrec_offset),
	// ecStart where the end records start (ecrec.ec_start).
	cdEnd, ecStart := endOffset, endOffset

	var ec64 [2]int64

	zip64 := endOffset >= zipLoc64Len && z.uint32At(endOffset-zipLoc64Len) == zipSigLoc64
	if zip64 {
		var err error
		if ec64, err = z.readEnd64(endOffset-zipLoc64Len, &ec); err != nil {
			return nil, err
		}

		cdEnd, ecStart = ec64[0], endOffset-zipLoc64Len
	}

	switch {
	case ec.disk != 0 || ec.cdDisk != 0 || ec.diskEntries != ec.total:
		return nil, z.fail(ErrIrreproducible, pkWarn, "", "multi-part archives are not supported")
	case !zip64 && (ec.total == 0xffff || ec.cdSize == 0xffffffff || ec.cdOffset == 0xffffffff):
		// unzip would take them as they are; no archive means that.
		return nil, z.fail(ErrIrreproducible, 0, "", "zip64 placeholders without a zip64 end record")
	case ec.cdOffset > uint64(cdEnd) || ec.cdSize > uint64(cdEnd) || int64(ec.cdOffset+ec.cdSize) != cdEnd: //nolint:gosec // both are at most cdEnd
		return nil, z.fail(ErrIrreproducible, pkWarn, "", "extra bytes at beginning or within zipfile")
	case ec.total == 0:
		return nil, z.fail(ErrIrreproducible, pkWarn, "", "zipfile is empty")
	case ec.total > uint64(z.b.limits.MaxEntries): //nolint:gosec // MaxEntries is positive
		return nil, z.fail(ErrLimit, 0, "", "more than %d entries", z.b.limits.MaxEntries)
	case ec.cdSize > zipMaxCentral:
		return nil, z.fail(ErrLimit, 0, "", "central directory larger than %d bytes", zipMaxCentral)
	}

	cdOffset, cdSize := int64(ec.cdOffset), int64(ec.cdSize) //nolint:gosec // bounded by cdEnd above

	// The spans unzip seeds its overlap check with: the central directory,
	// the zip64 end record and the end record (with the zip64 locator).
	z.cover.add(cdOffset, cdOffset+cdSize)

	if (zip64 && z.cover.add(ec64[0], ec64[1]) != 0) || z.cover.add(ecStart, z.size) != 0 {
		return nil, z.fail(ErrBomb, pkBomb, "", "invalid zip file with overlapped components (possible zip bomb)")
	}

	cd := make([]byte, cdSize)
	if _, err := z.f.ReadAt(cd, cdOffset); err != nil {
		return nil, err
	}

	entries := make([]zipEntry, 0, min(ec.total, uint64(cdSize/zipCentralLen)))
	pos := 0

	// unzip reads records up to the end of the central directory, then
	// checks their count: the 16-bit one of a plain end record counts
	// modulo 65536.
	for pos < len(cd) {
		if len(entries) == z.b.limits.MaxEntries {
			return nil, z.fail(ErrLimit, 0, "", "more than %d entries", z.b.limits.MaxEntries)
		}

		entries = append(entries, zipEntry{})

		used, err := z.parseCentral(cd[pos:], &entries[len(entries)-1])
		if err != nil {
			return nil, err
		}

		pos += used
	}

	count := uint64(len(entries))
	if !zip64 {
		count &= 0xffff
	}

	if count != ec.total {
		return nil, z.fail(ErrIrreproducible, pkWarn, "", "central directory size does not match its entries")
	}

	return entries, nil
}

// zipEnd is what the end records say of the central directory.
type zipEnd struct {
	disk, cdDisk, diskEntries, total, cdSize, cdOffset uint64
}

// readEnd64 is find_ecrec64(): the zip64 end record the locator at loc
// points at, whose values replace the end record's saturated ones. Every
// case where unzip would ignore the locator, look for the record elsewhere
// or fail is refused. It returns the zip64 end record's span.
func (z *zipPlanner) readEnd64(loc int64, ec *zipEnd) ([2]int64, error) {
	var none [2]int64

	l := make([]byte, zipLoc64Len)
	if _, err := z.f.ReadAt(l, loc); err != nil {
		return none, err
	}

	recDisk := uint64(binary.LittleEndian.Uint32(l[4:]))
	recOffset := binary.LittleEndian.Uint64(l[8:])
	totalDisks := uint64(binary.LittleEndian.Uint32(l[16:]))

	switch {
	case ec.disk != 0xffff && ec.disk+1 != totalDisks:
		// unzip takes the archive as a plain one, the locator as junk.
		return none, z.fail(ErrIrreproducible, pkWarn, "", "zip64 end locator unzip ignores (disk numbers differ)")
	case recOffset > uint64(loc): //nolint:gosec // loc is not negative
		return none, z.fail(ErrCorrupt, pkErr, "", "error searching for Zip64 EOCD Record")
	case recOffset+zipEnd64Len > uint64(loc): //nolint:gosec // loc is not negative
		return none, z.fail(ErrIrreproducible, pkErr, "", "zip64 end record runs into its locator")
	}

	start := int64(recOffset) //nolint:gosec // at most loc

	r := make([]byte, zipEnd64Len)
	if _, err := z.f.ReadAt(r, start); err != nil {
		return none, err
	}

	if binary.LittleEndian.Uint32(r) != zipSigEnd64 {
		// unzip guesses where the record is, with a warning.
		return none, z.fail(ErrIrreproducible, pkErr, "", "zip64 end record not where its locator says")
	}

	rec := zipEnd{
		disk:        uint64(binary.LittleEndian.Uint32(r[16:])),
		cdDisk:      uint64(binary.LittleEndian.Uint32(r[20:])),
		diskEntries: binary.LittleEndian.Uint64(r[24:]),
		total:       binary.LittleEndian.Uint64(r[32:]),
		cdSize:      binary.LittleEndian.Uint64(r[40:]),
		cdOffset:    binary.LittleEndian.Uint64(r[48:]),
	}
	recLen := binary.LittleEndian.Uint64(r[4:])

	// The record must agree with every end record field it does not
	// replace, else unzip takes the archive as a plain one.
	agrees := func(v, sat, v64 uint64) bool { return v == sat || v == v64 }
	if rec.disk != recDisk || !agrees(ec.cdDisk, 0xffff, rec.cdDisk) || !agrees(ec.diskEntries, 0xffff, rec.diskEntries) ||
		!agrees(ec.total, 0xffff, rec.total) || !agrees(ec.cdSize, 0xffffffff, rec.cdSize) || !agrees(ec.cdOffset, 0xffffffff, rec.cdOffset) {
		return none, z.fail(ErrIrreproducible, pkWarn, "", "zip64 end record unzip ignores (it disagrees with the end record)")
	}

	// Its declared length, which unzip's overlap check covers, must reach
	// the locator: nothing hides between them.
	if recLen > uint64(loc-start) || start+12+int64(recLen) != loc { //nolint:gosec // recLen is at most loc-start
		return none, z.fail(ErrIrreproducible, pkWarn, "", "zip64 end record length does not reach its locator")
	}

	if totalDisks != 1 || recDisk != 0 {
		return none, z.fail(ErrIrreproducible, pkWarn, "", "multi-part archives are not supported")
	}

	replace := func(v *uint64, sat, v64 uint64) {
		if *v == sat {
			*v = v64
		}
	}
	replace(&ec.disk, 0xffff, rec.disk)
	replace(&ec.cdDisk, 0xffff, rec.cdDisk)
	replace(&ec.diskEntries, 0xffff, rec.diskEntries)
	replace(&ec.total, 0xffff, rec.total)
	replace(&ec.cdSize, 0xffffffff, rec.cdSize)
	replace(&ec.cdOffset, 0xffffffff, rec.cdOffset)

	return [2]int64{start, loc}, nil
}

func (z *zipPlanner) uint32At(off int64) uint32 {
	var b [4]byte
	if _, err := z.f.ReadAt(b[:], off); err != nil {
		return 0
	}

	return binary.LittleEndian.Uint32(b[:])
}

// parseCentral reads one central record (process_cdir_file_hdr) and applies
// store_info()'s checks.
func (z *zipPlanner) parseCentral(cd []byte, e *zipEntry) (int, error) {
	if len(cd) < zipCentralLen || binary.LittleEndian.Uint32(cd) != zipSigCentral {
		return 0, z.fail(ErrIrreproducible, pkErr, "", "expected central file header signature not found")
	}

	nameLen := int(binary.LittleEndian.Uint16(cd[28:]))
	extraLen := int(binary.LittleEndian.Uint16(cd[30:]))
	commentLen := int(binary.LittleEndian.Uint16(cd[32:]))
	used := zipCentralLen + nameLen + extraLen + commentLen

	if used > len(cd) {
		return 0, z.fail(ErrIrreproducible, pkErr, "", "central directory entry runs past the central directory")
	}

	e.hostVer = cd[4]
	e.hostNum = min(cd[5], numHosts)
	neededVer, neededHost := cd[6], cd[7]
	e.flags = binary.LittleEndian.Uint16(cd[8:])
	e.method = binary.LittleEndian.Uint16(cd[10:])
	e.crc = binary.LittleEndian.Uint32(cd[16:])
	f := zip64Fields{
		usize:  uint64(binary.LittleEndian.Uint32(cd[24:])),
		csize:  uint64(binary.LittleEndian.Uint32(cd[20:])),
		offset: uint64(binary.LittleEndian.Uint32(cd[42:])),
		disk:   uint64(binary.LittleEndian.Uint16(cd[34:])),
	}
	e.extAttr = binary.LittleEndian.Uint32(cd[38:])
	e.nameRaw = cd[zipCentralLen : zipCentralLen+nameLen]
	e.extra = cd[zipCentralLen+nameLen : zipCentralLen+nameLen+extraLen]

	zip64, err := z.readZip64(e.extra, string(e.nameRaw), &f, true)
	if err != nil {
		return 0, err
	}

	if err := z.saturated(string(e.nameRaw), f, true); err != nil {
		return 0, err
	}

	z.zip64Seen = z.zip64Seen || zip64

	if e.csize, e.usize, e.offset, err = z.zip64Values(string(e.nameRaw), f); err != nil {
		return 0, err
	}

	switch name := e.nameRaw; {
	case f.disk != 0:
		return 0, z.fail(ErrIrreproducible, pkErr, string(name), "entry starts on another disk")
	case neededHost == hostVMS:
		// unzip asks on stdin whether to extract VMS-format files.
		return 0, z.fail(ErrIrreproducible, izUnsup, string(name), "VMS file format")
	case neededVer > unzipVersion:
		return 0, z.fail(ErrIrreproducible, izUnsup, string(name), "need PK compat. v%d.%d (can do v4.6)", neededVer/10, neededVer%10)
	case e.method == 1 || e.method == 6:
		// unzip unshrinks and explodes; maestro has no decoder for these
		// methods of 1990s PKZIP.
		return 0, z.fail(ErrIrreproducible, 0, string(name), "compression method %d (shrink or implode) is not supported by maestro", e.method)
	case e.method != methodStored && e.method != methodDeflate && e.method != methodDeflate64 && e.method != methodBzip2:
		// unzip skips the entry.
		return 0, z.fail(ErrIrreproducible, izUnsup, string(name), "unsupported compression method %d", e.method)
	case e.flags&1 != 0:
		// unzip would prompt for a password on stdin.
		return 0, z.fail(ErrIrreproducible, izUnsup, string(name), "encrypted entry")
	}

	if e.name, e.unipath, err = z.decodeName(e.nameRaw, e.extra, e, false); err != nil {
		return 0, err
	}

	return used, nil
}

// zip64Fields are the header fields the zip64 extra field may replace,
// those of a local header without offset and disk.
type zip64Fields struct {
	usize, csize, offset, disk uint64
}

// readZip64 is getZip64Data(): the zip64 extended information extra
// field's values replace, in its order, the saturated sizes, offset and
// disk (central) or sizes (local). found reports a zip64 field (see
// extract for what it does to a data descriptor).
//
// unzip also replaces a field when the other kind of header it last read
// (another entry's) saturated it; values that would stay saturated are
// refused (saturated, zip64Values), so that never happens here. A field
// too short for the values it must hold (unzip warns) and several zip64
// fields are refused.
func (z *zipPlanner) readZip64(extra []byte, entry string, f *zip64Fields, central bool) (found bool, err error) {
	for len(extra) >= 4 {
		id := binary.LittleEndian.Uint16(extra)
		size := int(binary.LittleEndian.Uint16(extra[2:]))

		if size > len(extra)-4 {
			break
		}

		z.unipathSeen = z.unipathSeen || id == efUnipath

		if id == efZip64 {
			if found {
				return false, z.fail(ErrIrreproducible, 0, entry, "several zip64 extra fields")
			}

			found = true
			block := extra[4 : 4+size]

			take := func(v *uint64, sat uint64, n int) error {
				if *v != sat {
					return nil
				}

				if len(block) < n {
					return z.fail(ErrIrreproducible, pkWarn, entry, "zip64 extra field too short for its values")
				}

				if n == 8 {
					*v = binary.LittleEndian.Uint64(block)
				} else {
					*v = uint64(binary.LittleEndian.Uint32(block))
				}

				block = block[n:]

				return nil
			}

			if err := take(&f.usize, 0xffffffff, 8); err != nil {
				return false, err
			}

			if err := take(&f.csize, 0xffffffff, 8); err != nil {
				return false, err
			}

			if central {
				if err := take(&f.offset, 0xffffffff, 8); err != nil {
					return false, err
				}

				if err := take(&f.disk, 0xffff, 4); err != nil {
					return false, err
				}
			}
		}

		extra = extra[4+size:]
	}

	return found, nil
}

// saturated refuses a zip64 placeholder no zip64 extra field replaced.
func (z *zipPlanner) saturated(entry string, f zip64Fields, central bool) error {
	if f.usize == 0xffffffff || f.csize == 0xffffffff || (central && (f.offset == 0xffffffff || f.disk == 0xffff)) {
		return z.fail(ErrIrreproducible, pkWarn, entry, "zip64 placeholder without its zip64 extra field value")
	}

	return nil
}

// zip64Values converts an entry's sizes and offset, refusing those past
// the end of the archive and sizes past the limits before they are used.
func (z *zipPlanner) zip64Values(entry string, f zip64Fields) (csize, usize, offset int64, err error) {
	switch {
	case f.csize > uint64(z.size) || f.offset >= uint64(z.size) || f.csize == 0xffffffff: //nolint:gosec // the size is not negative
		// (a 64-bit value of 0xffffffff would read as saturated to unzip
		// in the next header it reads)
		return 0, 0, 0, z.fail(ErrCorrupt, pkErr, entry, "entry size or offset past the end of the archive")
	case f.usize > uint64(z.b.limits.MaxFileSize): //nolint:gosec // MaxFileSize is positive
		return 0, 0, 0, z.fail(ErrLimit, 0, entry, "file larger than %d bytes", z.b.limits.MaxFileSize)
	}

	return int64(f.csize), int64(f.usize), int64(f.offset), nil //nolint:gosec // bounded above
}

// extract follows one entry through extract_or_test_entrylist(): the local
// header, the name checks, mapname() and the overlap check.
func (z *zipPlanner) extract(e *zipEntry) error {
	if z.cover.within(e.offset) {
		return z.fail(ErrBomb, pkBomb, e.name, "invalid zip file with overlapped components (possible zip bomb)")
	}

	// One read covers the local header, name and extra field when they are
	// as long as the central ones, as they almost always are.
	want := zipLocalLen + len(e.nameRaw) + len(e.extra)
	if cap(z.buf) < want {
		z.buf = make([]byte, want)
	}

	n, err := z.f.ReadAt(z.buf[:want], e.offset)
	if n < zipLocalLen || binary.LittleEndian.Uint32(z.buf) != zipSigLocal {
		return z.fail(ErrIrreproducible, pkErr, e.name, "bad zipfile offset (local header sig)")
	}

	hdr := z.buf[:zipLocalLen]
	flags := binary.LittleEndian.Uint16(hdr[6:])
	method := binary.LittleEndian.Uint16(hdr[8:])
	crc := binary.LittleEndian.Uint32(hdr[14:])
	lf := zip64Fields{usize: uint64(binary.LittleEndian.Uint32(hdr[22:])), csize: uint64(binary.LittleEndian.Uint32(hdr[18:]))}
	nameLen := int(binary.LittleEndian.Uint16(hdr[26:]))
	extraLen := int(binary.LittleEndian.Uint16(hdr[28:]))

	dataStart := e.offset + zipLocalLen + int64(nameLen) + int64(extraLen)

	if have := zipLocalLen + nameLen + extraLen; have > want || (have < want && err != nil) {
		if cap(z.buf) < have {
			z.buf = append(z.buf[:want], make([]byte, have-want)...)
		}

		if n, err = z.f.ReadAt(z.buf[:have], e.offset); n < have {
			return z.fail(ErrCorrupt, pkErr, e.name, "truncated local header: %v", err)
		}
	} else if n < have {
		return z.fail(ErrCorrupt, pkErr, e.name, "truncated local header: %v", err)
	}

	local := z.buf[zipLocalLen : zipLocalLen+nameLen+extraLen]

	descriptor := e.flags&8 != 0

	// With a data descriptor, unzip takes the central sizes in the end
	// (the local ones may stay placeholders), but getZip64Data runs on the
	// local extra field all the same.
	zip64, err := z.readZip64(local[nameLen:], e.name, &lf, false)
	if err != nil {
		return err
	}

	if !descriptor {
		if err := z.saturated(e.name, lf, false); err != nil {
			return err
		}
	}

	// Whether unzip reads a data descriptor's sizes as 64-bit ones
	// (G.zip64) depends on the build: the CVE-2019-13232 patch sets the
	// flag in getZip64Data() for a zip64 field, as the AppNote has it,
	// where its hunk applies there; on builds where it landed elsewhere
	// (nixpkgs'), a Unicode Path field sets it instead. do_string() skips
	// both for an empty extra field, leaving the flag of the last one read.
	// The descriptor is taken as 64-bit whenever one of those builds would,
	// so the wider span is checked for overlaps: what either build refuses
	// as a zip bomb is refused.
	e.zip64 = zip64 || z.unipathSeen || (extraLen == 0 && z.zip64Seen)
	z.zip64Seen = z.zip64Seen || zip64

	switch {
	case flags&(1<<11) != e.flags&(1<<11):
		return z.fail(ErrIrreproducible, pkWarn, e.name, "local and central GPFlags bit 11 differ")
	case method != e.method:
		return z.fail(ErrIrreproducible, pkErr, e.name, "local and central compression methods differ")
	case flags&8 != e.flags&8:
		return z.fail(ErrIrreproducible, pkErr, e.name, "local and central data descriptor flags differ")
	case !descriptor && (crc != e.crc || lf.csize != uint64(e.csize) || lf.usize != uint64(e.usize)): //nolint:gosec // e's sizes are not negative
		// unzip would trust the local header; refuse rather than pick.
		return z.fail(ErrIrreproducible, pkErr, e.name, "local and central sizes or CRC differ")
	case e.method == methodStored && e.csize != e.usize:
		return z.fail(ErrIrreproducible, pkWarn, e.name, "compressed and uncompressed sizes differ for a stored entry")
	}

	// The local name decodes as the central one did when its bytes, extra
	// field and the host test agree.
	unipath := e.unipath
	if !bytes.Equal(local[:nameLen], e.nameRaw) || !bytes.Equal(local[nameLen:], e.extra) || e.extAttr&0xffff0000 == 0 {
		var localName string

		if localName, unipath, err = z.decodeName(local[:nameLen], local[nameLen:], e, true); err != nil {
			return err
		}

		if localName != e.name {
			return z.fail(ErrIrreproducible, pkWarn, e.name, "mismatching local filename (%s)", localName)
		}
	}

	return z.place(e, zipFile{dataStart: dataStart, csize: e.csize, usize: e.usize, crc: e.crc, method: e.method}, unipath)
}

// end is where unzip stops reading the member: after its data and any data
// descriptor. Only extracted files are read this far.
func (z *zipPlanner) end(e *zipEntry, file zipFile, unipath bool) (int64, error) {
	end := file.dataStart + file.csize
	if end > z.size {
		return 0, z.fail(ErrCorrupt, pkErr, e.name, "entry runs past the end of the archive")
	}

	if e.flags&8 == 0 {
		return end, nil
	}

	if unipath {
		// getUnicodeData() leaves G.zip64 set, which makes unzip skip
		// eight more descriptor bytes.
		return 0, z.fail(ErrIrreproducible, 0, e.name, "a Unicode path field with a data descriptor")
	}

	return z.descriptorEnd(e, end)
}

// descriptorEnd is where unzip stops reading a data descriptor that starts
// at off, with its signature ambiguity resolved as extract.c does.
func (z *zipPlanner) descriptorEnd(e *zipEntry, off int64) (int64, error) {
	var buf [12]byte
	if _, err := z.f.ReadAt(buf[:], off); err != nil {
		return 0, z.fail(ErrCorrupt, pkErr, e.name, "truncated data descriptor")
	}

	crc := binary.LittleEndian.Uint32(buf[0:])
	clen := binary.LittleEndian.Uint32(buf[4:])
	ulen := binary.LittleEndian.Uint32(buf[8:])
	end := off + 12

	// With 64-bit sizes, ulen is the high half of the compressed size.
	signed := func(high uint64) bool {
		return crc == zipSigDesc && (e.crc != zipSigDesc ||
			(clen == zipSigDesc && (uint64(e.csize)&0xffffffff != zipSigDesc || (ulen == zipSigDesc && high != zipSigDesc)))) //nolint:gosec // csize is not negative
	}

	sig := signed(uint64(e.usize))                     //nolint:gosec // usize is not negative
	if e.zip64 && sig != signed(uint64(e.csize)>>32) { //nolint:gosec // csize is not negative
		return 0, z.fail(ErrIrreproducible, 0, e.name, "data descriptor signature ambiguity unzip builds resolve differently")
	}

	if sig {
		end += 4
	}

	if e.zip64 {
		// "skip eight more for ZIP64"
		end += 8
	}

	if end > z.size {
		return 0, z.fail(ErrCorrupt, pkErr, e.name, "truncated data descriptor")
	}

	return end, nil
}

// place runs mapname() and creates the entry in the tree, refusing every
// case where unzip would prompt, warn or fail.
func (z *zipPlanner) place(e *zipEntry, file zipFile, unipath bool) error {
	name := e.name
	if e.hostNum == hostFAT && strings.IndexByte(name, '/') < 0 && strings.IndexByte(name, '\\') >= 0 {
		return z.fail(ErrIrreproducible, pkWarn, name, "appears to use backslashes as path separators")
	}

	if strings.HasPrefix(name, "/") {
		return z.fail(ErrIrreproducible, pkWarn, name, "warning:  stripped absolute path spec from %s", name)
	}

	if isVolumeLabel(e) {
		// mapname(): "skipping: ... (can't do volume label)"; nothing is
		// extracted.
		return nil
	}

	attr, umasked, symlink := mapattr(e)

	path, isDir, err := z.mapname(name)
	if err != nil {
		return err
	}

	// checkdir(APPEND_DIR): each directory component is created with
	// mkdir(0777) unless it exists, and must be a directory.
	dirEnd := len(path)
	if !isDir {
		dirEnd = max(strings.LastIndexByte(path, '/'), 0)
	}

	created := false

	for start := 0; start < dirEnd; {
		end := dirEnd
		if i := strings.IndexByte(path[start:dirEnd], '/'); i >= 0 {
			end = start + i
		}

		switch n := z.b.get(path[:end]); {
		case n == nil:
			if err := z.b.add(Entry{Path: path[:end], Kind: Dir, Mode: 0o777, Umask: true}); err != nil {
				return err
			}

			created = true
		case n.Kind != Dir:
			return z.fail(ErrIrreproducible, pkErr, name, "checkdir error:  %s exists but is not directory", path[:end])
		}

		start = end + 1
	}

	if isDir {
		// A directory entry's attributes apply only when mapname() created
		// it; an existing directory (implied by an earlier entry) keeps its
		// mode. set_direc_attribs() then chmods it to the archive's mode,
		// set-id bits stripped by filtattr().
		if created {
			n := z.b.get(path)
			n.Mode, n.Umask = fs.FileMode(attr)&fs.ModePerm, umasked
		}

		return nil
	}

	if z.b.get(path) != nil {
		// unzip asks whether to replace it, reads EOF and skips it.
		return z.fail(ErrIrreproducible, pkWarn, name, "replace %s? [y]es, [n]o, [A]ll, [N]one, [r]ename:  NULL\n(EOF or read error, treating as \"[N]one\" ...)", path)
	}

	end, err := z.end(e, file, unipath)
	if err != nil {
		return err
	}

	if z.cover.add(e.offset, end) != 0 {
		return z.fail(ErrBomb, pkBomb, name, "invalid zip file with overlapped components (possible zip bomb)")
	}

	if symlink && file.usize > 0 {
		target, err := z.readLink(e, file)
		if err != nil {
			return err
		}

		err = z.b.add(Entry{Path: path, Kind: Symlink, Link: target})

		return err
	}

	err = z.b.add(Entry{
		Path:  path,
		Kind:  File,
		Mode:  fs.FileMode(attr) & fs.ModePerm,
		Umask: umasked,
		Size:  file.usize,
		src:   len(z.files),
	})
	z.files = append(z.files, file)

	return err
}

// readLink reads a symlink's target, the entry's content.
func (z *zipPlanner) readLink(e *zipEntry, file zipFile) (string, error) {
	if file.usize > zipMaxLink {
		return "", z.fail(ErrIrreproducible, 0, e.name, "symlink target longer than %d bytes", zipMaxLink)
	}

	r, err := newZipReader(z.f, file, e.name, nil)
	if err != nil {
		return "", err
	}

	target, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}

	if bytes.IndexByte(target, 0) >= 0 {
		return "", z.fail(ErrIrreproducible, 0, e.name, "symlink target contains a NUL byte")
	}

	return string(target), nil
}

func isVolumeLabel(e *zipEntry) bool {
	return e.extAttr&0x08 != 0 &&
		(e.hostNum == hostFAT || e.hostNum == hostHPFS || e.hostNum == hostNTFS || e.hostNum == hostAtari)
}

// mapname maps an entry name to the path unzip extracts it to, as
// unix.c's mapname() does: empty, "." and ".." directory components vanish
// (the warning about ".." is only shown, and only counts, without -qq),
// control characters are dropped, a VMS version suffix is removed and a
// final "." or ".." becomes "_" or "__". isDir reports a directory entry
// (a name ending in "/"); its path may be "", the extraction directory.
func (z *zipPlanner) mapname(name string) (path string, isDir bool, err error) {
	if cleanName(name) {
		return strings.TrimSuffix(name, "/"), strings.HasSuffix(name, "/"), nil
	}

	return z.mapnameSlow(name)
}

// mapnameSlow is mapname for names that need changing.
func (z *zipPlanner) mapnameSlow(name string) (path string, isDir bool, err error) {
	isDir = strings.HasSuffix(name, "/")
	out := make([]byte, 0, len(name))
	comp, lastSemi := len(out), -1

	for i := range len(name) {
		c := name[i]
		switch {
		case c == '/':
			switch string(out[comp:]) {
			case "", ".", "..":
				out = out[:comp]
			default:
				out = append(out, '/')
			}

			comp, lastSemi = len(out), -1
		case c == ';':
			lastSemi = len(out)
			out = append(out, c)
		case c >= 0x20 && c <= 0x7e, c >= 0x80 && c <= 0xfe:
			out = append(out, c)
		case c == 0xff && z.locale == LocaleOther:
			// isprint(0xff) depends on the locale.
			return "", false, z.fail(ErrIrreproducible, 0, name, "name contains byte 0xff")
		default:
			// isprint() fails: the character is dropped.
		}
	}

	if isDir {
		return strings.TrimSuffix(string(out), "/"), true, nil
	}

	// Without -V, a trailing ";<digits>" is a VMS version number.
	if lastSemi >= 0 {
		j := lastSemi + 1
		for j < len(out) && out[j] >= '0' && out[j] <= '9' {
			j++
		}

		if j == len(out) {
			out = out[:lastSemi]
		}
	}

	switch string(out[comp:]) {
	case ".":
		out = append(out[:comp], '_')
	case "..":
		out = append(out[:comp], "__"...)
	case "":
		return "", false, z.fail(ErrIrreproducible, pkErr, name, "mapname:  conversion of %s failed", name)
	}

	return string(out), false, nil
}

// cleanName reports whether mapname would leave name as it is (but for a
// directory's trailing slash): printable bytes only, no ';', no empty, "."
// or ".." component.
func cleanName(name string) bool {
	if name == "" || name[0] == '/' {
		return false
	}

	start := 0

	for i := 0; i <= len(name); i++ {
		if i < len(name) {
			c := name[i]
			if c != '/' {
				if c < 0x20 || c == 0x7f || c == 0xff || c == ';' {
					return false
				}

				continue
			}
		}

		switch name[start:i] {
		case ".", "..":
			return false
		case "":
			// Only a directory's trailing slash may end an empty component.
			if i != len(name) || i == 0 || name[i-1] != '/' || start != i {
				return false
			}
		}

		start = i + 1
	}

	return true
}

// decodeName turns a stored name into the one unzip works with: do_string()
// translates it from the creating host's code page, then, when the entry
// has an extra field, a UTF-8 name (general purpose bit 11 or a Unicode
// Path field) replaces it, converted to the locale. unipath reports a
// Unicode Path field was used.
func (z *zipPlanner) decodeName(raw, extra []byte, e *zipEntry, local bool) (name string, unipath bool, err error) {
	entry := string(raw)

	switch {
	case len(raw) == 0:
		return "", false, z.fail(ErrIrreproducible, pkErr, entry, "empty name")
	case bytes.IndexByte(raw, 0) >= 0:
		return "", false, z.fail(ErrIrreproducible, 0, entry, "name contains a NUL byte")
	case len(raw) >= zipFilnamsiz:
		return "", false, z.fail(ErrIrreproducible, pkWarn, entry, "warning:  filename too long--truncating.")
	}

	name, oem := entry, oemName(raw, e, local)

	// Distributions patch how unzip translates MS-DOS code pages (Debian
	// keeps the bytes, upstream maps them to ISO 8859-1), so a non-ASCII
	// name taken from one is refused unless a UTF-8 name replaces it.
	translated := func() (string, bool, error) {
		if oem {
			return "", false, z.fail(ErrIrreproducible, 0, entry, "non-ASCII name in an MS-DOS code page")
		}

		return name, false, nil
	}

	if len(extra) == 0 {
		return translated()
	}

	var utf8Name []byte

	if e.flags&(1<<11) != 0 {
		utf8Name = raw
	} else if utf8Name, unipath, err = z.unicodePath(raw, extra, entry); err != nil {
		return "", false, err
	}

	if utf8Name == nil {
		return translated()
	}

	switch z.locale {
	case LocaleUTF8:
		name = string(utf8Name)
	case LocaleC:
		escaped, ok := escapeToC(utf8Name)
		if !ok {
			return "", false, z.fail(ErrIrreproducible, pkErr, entry, "warning:  Unicode filename corrupt.")
		}

		if cLocaleLatin1 && hasLatin1(utf8Name) {
			// macOS's C locale converts U+0080 to U+00FF to single Latin-1
			// bytes, which APFS and HFS+ refuse as no UTF-8: unzip fails on
			// the entry and Composer falls back to ZipArchive.
			return "", false, z.fail(ErrIrreproducible, 0, entry, "Latin-1 name in macOS's C locale, which the filesystem refuses")
		}

		name = escaped
	default:
		if !isASCII(utf8Name) {
			return "", false, z.fail(ErrIrreproducible, 0, entry, "UTF-8 name in a locale that is neither UTF-8 nor C")
		}

		name = string(utf8Name)
	}

	if len(name) >= zipFilnamsiz {
		return "", false, z.fail(ErrIrreproducible, pkWarn, entry, "warning:  filename too long (P1) -- truncating.")
	}

	return name, unipath, nil
}

// unicodePath is getUnicodeData(): the UTF-8 name of an Info-ZIP Unicode
// Path field whose CRC matches the stored name, or nil.
func (z *zipPlanner) unicodePath(raw, extra []byte, entry string) ([]byte, bool, error) {
	var found []byte

	seen := false

	for len(extra) >= 4 {
		id := binary.LittleEndian.Uint16(extra)
		size := int(binary.LittleEndian.Uint16(extra[2:]))

		if size > len(extra)-4 {
			break
		}

		if id == efUnipath {
			if seen || size < 5 {
				// Several fields (or a short one) leave unzip's choice to its
				// error paths; refuse rather than mimic them.
				return nil, false, z.fail(ErrIrreproducible, 0, entry, "malformed Unicode Path extra field")
			}

			seen = true

			if extra[4] > 1 || binary.LittleEndian.Uint32(extra[5:]) != crc32.ChecksumIEEE(raw) {
				return nil, false, nil
			}

			found = extra[9 : 4+size]
			if len(found) == 0 {
				found = raw
			}

			if bytes.IndexByte(found, 0) >= 0 {
				return nil, false, z.fail(ErrIrreproducible, 0, entry, "Unicode Path contains a NUL byte")
			}
		}

		extra = extra[4+size:]
	}

	return found, found != nil, nil
}

// oemName is Ext_ASCII_TO_Native's test: names from DOS, OS/2 and WinZip
// 5.0 hosts are in an OEM code page and get translated; every other name is
// kept as is. It reports a non-ASCII name that would be translated, which
// the caller refuses, so the translation itself is never needed.
func oemName(raw []byte, e *zipEntry, local bool) bool {
	hasUxAtt := e.extAttr&0xffff0000 != 0
	winZip5 := (local || hasUxAtt) && (e.hostVer == 25 || e.hostVer == 26 || e.hostVer == 40)
	fromOEM := (e.hostNum == hostFAT && !winZip5) || e.hostNum == hostHPFS || (e.hostNum == hostNTFS && e.hostVer == 50)

	return fromOEM && !isASCII(raw)
}

// cLocaleLatin1 tells that wctomb() in the C locale converts U+0080 to
// U+00FF to single bytes, as macOS's libc does; glibc's converts nothing
// beyond ASCII, which escapeToC reproduces.
var cLocaleLatin1 = runtime.GOOS == "darwin"

// hasLatin1 reports whether the UTF-8 name s holds a character from U+0080
// to U+00FF.
func hasLatin1(s []byte) bool {
	for _, r := range string(s) {
		if r >= 0x80 && r <= 0xff {
			return true
		}
	}

	return false
}

// escapeToC is utf8_to_local_string() in the C locale: ASCII stays, every
// other character becomes "#Uxxxx" or "#Lxxxxxx". It fails on input that is
// not UTF-8.
func escapeToC(s []byte) (string, bool) {
	if !utf8.Valid(s) {
		return "", false
	}

	var b strings.Builder

	for _, r := range string(s) {
		switch {
		case r < 0x80:
			b.WriteRune(r)
		case r <= 0xffff:
			b.WriteString("#U")
			writeHex(&b, uint32(r), 4)
		default:
			b.WriteString("#L")
			writeHex(&b, uint32(r), 6)
		}
	}

	return b.String(), true
}

func writeHex(b *strings.Builder, v uint32, digits int) {
	const hex = "0123456789abcdef"
	for i := digits - 1; i >= 0; i-- {
		b.WriteByte(hex[v>>(4*i)&0xf])
	}
}

func isASCII(s []byte) bool {
	for _, c := range s {
		if c >= utf8.RuneSelf {
			return false
		}
	}

	return true
}

// mapattr is unix.c's mapattr(): the mode unzip gives an entry, whether the
// umask was applied to it, and whether it is a symlink.
func mapattr(e *zipEntry) (attr uint32, umasked, symlink bool) {
	tmp := e.extAttr

	switch e.hostNum {
	case hostAmiga:
		t := tmp >> 17 & 7
		return t<<6 | t<<3 | t, true, false
	case hostTheos, hostUnix, hostVMS, hostAcorn, hostAtari, hostAtheOS, hostBeOS, hostQDOS, hostTandem:
		if e.hostNum == hostTheos {
			tmp &= 0xf1ffffff
			if tmp&0xf0000000 != 0x40000000 {
				tmp &= 0x01ffffff
			} else {
				tmp &= 0x41ffffff
			}
		}

		attr = tmp >> 16
		if attr == 0 && len(e.extra) > 0 {
			var opaque bool
			if attr, opaque = unixExtraMode(e.extra); opaque {
				return dosAttr(e, tmp, tmp>>16)
			}
		}

		return attr, false, isSymlinkMode(attr) && symlinkHost(e.hostNum)
	case hostFAT:
		return dosAttr(e, tmp, tmp>>16)
	}

	return dosAttr(e, tmp, 0)
}

// unixExtraMode scans the extra field of a Unix entry without permission
// bits: ASi's Unix field carries a mode; a VMS (or short ASi) field means
// the bits are elsewhere and the MS-DOS attributes apply instead (opaque).
func unixExtraMode(ef []byte) (mode uint32, opaque bool) {
	for len(ef) >= 4 {
		id := binary.LittleEndian.Uint16(ef)
		size := int(binary.LittleEndian.Uint16(ef[2:]))

		if size > len(ef)-4 {
			break
		}

		switch id {
		case efASiUnix:
			if size >= 6 {
				return uint32(binary.LittleEndian.Uint16(ef[8:])), false
			}

			return 0, true
		case efPKVMS:
			return 0, true
		}

		ef = ef[4+size:]
	}

	return 0, false
}

// dosAttr is mapattr()'s MS-DOS branch: the read-only and directory bits
// expand to rwx bits under the umask, unless the Unix bits a FAT entry
// carries agree with them.
func dosAttr(e *zipEntry, tmp, attr uint32) (uint32, bool, bool) {
	if tmp&0x10 == 0 && strings.HasSuffix(e.name, "/") {
		tmp |= 0x10
	}

	t := (^tmp&1)<<1 | (tmp&0x10)>>4

	if attr&0o700 == 0o400|t<<6 {
		return attr, false, isSymlinkMode(attr) && e.hostNum == hostFAT
	}

	return 0o444 | t<<6 | t<<3 | t, true, false
}

func isSymlinkMode(attr uint32) bool {
	return attr&0o170000 == 0o120000
}

func symlinkHost(host byte) bool {
	return host == hostUnix || host == hostAtari || host == hostAtheOS || host == hostBeOS || host == hostVMS
}

// cover is unzip's record of the archive spans already accounted for; an
// entry overlapping one is a possible zip bomb.
type cover struct {
	spans [][2]int64 // sorted, disjoint, adjacent ones merged
}

// find is the index of the first span starting after v.
func (c *cover) find(v int64) int {
	i, _ := slices.BinarySearchFunc(c.spans, v, func(s [2]int64, v int64) int {
		if s[0] <= v {
			return -1
		}

		return 1
	})

	return i
}

func (c *cover) within(v int64) bool {
	i := c.find(v)
	return i > 0 && v < c.spans[i-1][1]
}

// add records [beg, end): 0 on success, 1 if it overlaps a span, -1 if
// empty.
func (c *cover) add(beg, end int64) int {
	if beg >= end {
		return -1
	}

	i := c.find(beg)
	if (i > 0 && beg < c.spans[i-1][1]) || (i < len(c.spans) && end > c.spans[i][0]) {
		return 1
	}

	prec := i > 0 && beg == c.spans[i-1][1]
	foll := i < len(c.spans) && end == c.spans[i][0]

	switch {
	case prec && foll:
		c.spans[i-1][1] = c.spans[i][1]
		c.spans = slices.Delete(c.spans, i, i+1)
	case prec:
		c.spans[i-1][1] = end
	case foll:
		c.spans[i][0] = beg
	default:
		c.spans = slices.Insert(c.spans, i, [2]int64{beg, end})
	}

	return 0
}

// zipContent streams the planned files' data.
type zipContent struct {
	src   io.ReaderAt
	files []zipFile
}

func (c *zipContent) readFiles(a *Archive, fn FileFunc) error {
	dec := &zipDecoders{}

	for _, i := range a.fileOrder(func(src int) int64 { return c.files[src].dataStart }) {
		e := &a.entries[i]

		r, err := newZipReader(c.src, c.files[e.src], e.Path, dec)
		if err != nil {
			return err
		}

		if err := consume(i, r, fn); err != nil {
			return err
		}
	}

	return nil
}

// zipDecoders holds the readers one entry after another reuses.
type zipDecoders struct {
	buf   *bufio.Reader
	flate io.Reader
	reset flate.Resetter
	// flate64 and reset64 are Deflate64's (zip method 9).
	flate64 io.Reader
	reset64 deflate64.Resetter
	sec     section
	chk     checkReader
}

// section reads [off, end) of the archive.
type section struct {
	f        io.ReaderAt
	off, end int64
}

func (s *section) Read(p []byte) (int, error) {
	if s.off >= s.end {
		return 0, io.EOF
	}

	if rest := s.end - s.off; int64(len(p)) > rest {
		p = p[:rest]
	}

	n, err := s.f.ReadAt(p, s.off)
	s.off += int64(n)

	if errors.Is(err, io.EOF) && s.off < s.end {
		err = io.ErrUnexpectedEOF
	} else if n > 0 && errors.Is(err, io.EOF) {
		err = nil
	}

	return n, err
}

// buffer points dec.buf, the decompressors' input, at dec.sec.
func (dec *zipDecoders) buffer() {
	if dec.buf == nil {
		dec.buf = bufio.NewReaderSize(&dec.sec, 32<<10)
	} else {
		dec.buf.Reset(&dec.sec)
	}
}

// newZipReader returns a reader of the entry's checked content, valid
// until the next call with the same dec (nil: a fresh one).
func newZipReader(f io.ReaderAt, file zipFile, entry string, dec *zipDecoders) (io.Reader, error) {
	if dec == nil {
		dec = &zipDecoders{}
	}

	dec.sec = section{f: f, off: file.dataStart, end: file.dataStart + file.csize}

	var r io.Reader

	switch file.method {
	case methodStored:
		r = &dec.sec
	case methodDeflate:
		dec.buffer()

		if dec.flate == nil {
			fr := flate.NewReader(dec.buf)
			dec.flate, dec.reset = fr, fr.(flate.Resetter) //nolint:errcheck // flate's reader always is a Resetter.
		} else if err := dec.reset.Reset(dec.buf, nil); err != nil {
			return nil, err
		}

		r = dec.flate
	case methodDeflate64:
		dec.buffer()

		if dec.flate64 == nil {
			fr := deflate64.NewReader(dec.buf)
			dec.flate64, dec.reset64 = fr, fr.(deflate64.Resetter) //nolint:errcheck // deflate64's reader always is a Resetter.
		} else if err := dec.reset64.Reset(dec.buf, nil); err != nil {
			return nil, err
		}

		r = dec.flate64
	case methodBzip2:
		r = bzip2.NewReader(&dec.sec)
	}

	dec.chk = checkReader{r: r, size: file.usize, want: file.crc, entry: entry}

	return &dec.chk, nil
}

// checkReader passes through exactly size bytes whose CRC-32 is want, and
// fails otherwise, as unzip's CRC check would.
type checkReader struct {
	r     io.Reader
	err   error
	entry string
	n     int64
	size  int64
	crc   uint32
	want  uint32
}

func (c *checkReader) Read(p []byte) (int, error) {
	if c.err != nil {
		return 0, c.err
	}

	// Allow one byte past the end, to notice content that is too long.
	if rest := c.size - c.n + 1; int64(len(p)) > rest {
		p = p[:rest]
	}

	n, err := c.r.Read(p)
	c.n += int64(n)
	c.crc = crc32.Update(c.crc, crc32.IEEETable, p[:n])

	switch {
	case c.n > c.size:
		c.err = errorf(Zip, ErrIrreproducible, c.entry, "content longer than its declared %d bytes", c.size)
	case err == io.EOF:
		switch {
		case c.n != c.size:
			c.err = errorf(Zip, ErrIrreproducible, c.entry, "content shorter than its declared %d bytes", c.size)
		case c.crc != c.want:
			c.err = errorf(Zip, ErrIrreproducible, c.entry, "bad CRC %08x  (should be %08x)", c.crc, c.want)
		default:
			c.err = io.EOF
		}
	case err != nil:
		c.err = errorf(Zip, ErrIrreproducible, c.entry, "invalid compressed data: %v", err)
	}

	if c.err != nil && !errors.Is(c.err, io.EOF) {
		return 0, c.err
	}

	return n, c.err
}
