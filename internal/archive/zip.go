// Reproduces `unzip -qq <file> -d <dir>` (Info-ZIP UnZip 6.0 with the
// distributions' CVE patches, built for Unix with SYMLINKS, SET_DIR_ATTRIB,
// UNICODE_SUPPORT, USE_BZIP2 and ZIP64_SUPPORT), from process.c
// (find_ecrec, process_cdir_file_hdr, getUnicodeData), fileio.c (do_string),
// extract.c (extract_or_test_files, store_info,
// extract_or_test_entrylist, the overlap "cover"), unix/unix.c (mapattr,
// mapname, checkdir, close_outfile, set_direc_attribs) and unzpriv.h
// (Ext_ASCII_TO_Native).

package archive

import (
	"bufio"
	"cmp"
	"bytes"
	"compress/bzip2"
	"compress/flate"
	"encoding/binary"
	"hash/crc32"
	"io"
	"io/fs"
	"os"
	"slices"
	"strings"
	"unicode/utf8"
)

const (
	zipSigLocal   = 0x04034b50
	zipSigCentral = 0x02014b50
	zipSigEnd     = 0x06054b50
	zipSigLoc64   = 0x07064b50
	zipSigDesc    = 0x08074b50

	zipEndLen     = 22
	zipCentralLen = 46
	zipLocalLen   = 30

	// unzip looks for the end record in the last 66000 bytes.
	zipSearchLen = 66000
	// FILNAMSIZ (PATH_MAX): longer names are truncated with a warning.
	zipFilnamsiz = 4096
	// A symlink's target must fit in PATH_MAX including its NUL, or
	// symlink(2) fails and unzip silently leaves nothing behind.
	zipMaxLink = 4095
	// Bounds the central directory read into memory.
	zipMaxCentral = 256 << 20

	methodStored  = 0
	methodDeflate = 8
	methodBzip2   = 12

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

// zipEntry is a central directory record.
type zipEntry struct {
	nameRaw []byte
	extra   []byte
	name    string // after unzip's decoding (do_string + Unicode handling)
	offset  int64
	csize   int64
	usize   int64
	extAttr uint32
	crc     uint32
	flags   uint16
	method  uint16
	hostNum byte
	hostVer byte
}

type zipPlanner struct {
	f      *os.File
	b      *builder
	files  []zipFile
	cover  cover
	size   int64
	locale Locale
}

func (z *zipPlanner) fail(kind error, code int, entry, reason string, args ...any) *Error {
	e := errorf(Zip, kind, entry, reason, args...)
	e.ExitCode = code

	return e
}

func planZip(f *os.File, size int64, locale Locale, b *builder) (contentReader, error) {
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

	return &zipContent{files: z.files}, nil
}

// readCentral finds the end record (find_ecrec) and reads the central
// directory, refusing every layout unzip only extracts with a warning.
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
	disk := binary.LittleEndian.Uint16(end[4:])
	cdDisk := binary.LittleEndian.Uint16(end[6:])
	diskEntries := binary.LittleEndian.Uint16(end[8:])
	total := binary.LittleEndian.Uint16(end[10:])
	cdSize := int64(binary.LittleEndian.Uint32(end[12:]))
	cdOffset := int64(binary.LittleEndian.Uint32(end[16:]))
	commentLen := int64(binary.LittleEndian.Uint16(end[20:]))

	switch {
	case endOffset+zipEndLen+commentLen != z.size:
		return nil, z.fail(ErrIrreproducible, pkWarn, "", "the archive comment does not end the file")
	case disk != 0 || cdDisk != 0 || diskEntries != total:
		return nil, z.fail(ErrIrreproducible, pkWarn, "", "multi-part archives are not supported")
	case endOffset >= 20 && z.uint32At(endOffset-20) == zipSigLoc64,
		total == 0xffff, cdSize == 0xffffffff, cdOffset == 0xffffffff:
		return nil, z.fail(ErrIrreproducible, 0, "", "zip64 archives are not supported")
	case cdOffset+cdSize != endOffset:
		return nil, z.fail(ErrIrreproducible, pkWarn, "", "extra bytes at beginning or within zipfile")
	case total == 0:
		return nil, z.fail(ErrIrreproducible, pkWarn, "", "zipfile is empty")
	case int(total) > z.b.limits.MaxEntries:
		return nil, z.fail(ErrLimit, 0, "", "more than %d entries", z.b.limits.MaxEntries)
	case cdSize > zipMaxCentral:
		return nil, z.fail(ErrLimit, 0, "", "central directory larger than %d bytes", zipMaxCentral)
	}

	// The spans unzip seeds its overlap check with: the central directory
	// and the end record.
	z.cover.add(cdOffset, cdOffset+cdSize)

	if z.cover.add(endOffset, endOffset+zipEndLen+commentLen) != 0 {
		return nil, z.fail(ErrBomb, pkBomb, "", "invalid zip file with overlapped components (possible zip bomb)")
	}

	cd := make([]byte, cdSize)
	if _, err := z.f.ReadAt(cd, cdOffset); err != nil {
		return nil, err
	}

	entries := make([]zipEntry, total)
	pos := 0

	for i := range entries {
		used, err := z.parseCentral(cd[pos:], &entries[i])
		if err != nil {
			return nil, err
		}

		pos += used
	}

	if pos != len(cd) {
		return nil, z.fail(ErrIrreproducible, pkWarn, "", "central directory size does not match its entries")
	}

	return entries, nil
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
	csize := binary.LittleEndian.Uint32(cd[20:])
	usize := binary.LittleEndian.Uint32(cd[24:])
	diskStart := binary.LittleEndian.Uint16(cd[34:])
	e.extAttr = binary.LittleEndian.Uint32(cd[38:])
	offset := binary.LittleEndian.Uint32(cd[42:])
	e.csize, e.usize, e.offset = int64(csize), int64(usize), int64(offset)
	e.nameRaw = cd[zipCentralLen : zipCentralLen+nameLen]
	e.extra = cd[zipCentralLen+nameLen : zipCentralLen+nameLen+extraLen]

	name := string(e.nameRaw)

	switch {
	case csize == 0xffffffff || usize == 0xffffffff || offset == 0xffffffff || diskStart == 0xffff:
		return 0, z.fail(ErrIrreproducible, 0, name, "zip64 entries are not supported")
	case diskStart != 0:
		return 0, z.fail(ErrIrreproducible, pkErr, name, "entry starts on another disk")
	case neededHost == hostVMS:
		// unzip asks on stdin whether to extract VMS-format files.
		return 0, z.fail(ErrIrreproducible, izUnsup, name, "VMS file format")
	case neededVer > unzipVersion:
		return 0, z.fail(ErrIrreproducible, izUnsup, name, "need PK compat. v%d.%d (can do v4.6)", neededVer/10, neededVer%10)
	case e.method != methodStored && e.method != methodDeflate && e.method != methodBzip2:
		// Deflate64, shrink and implode are left out; unzip skips the rest.
		return 0, z.fail(ErrIrreproducible, izUnsup, name, "unsupported compression method %d", e.method)
	case e.flags&1 != 0:
		// unzip would prompt for a password on stdin.
		return 0, z.fail(ErrIrreproducible, izUnsup, name, "encrypted entry")
	}

	var err error
	if e.name, _, err = z.decodeName(e.nameRaw, e.extra, e, false); err != nil {
		return 0, err
	}

	return used, nil
}

// extract follows one entry through extract_or_test_entrylist(): the local
// header, the name checks, mapname() and the overlap check.
func (z *zipPlanner) extract(e *zipEntry) error {
	if z.cover.within(e.offset) {
		return z.fail(ErrBomb, pkBomb, e.name, "invalid zip file with overlapped components (possible zip bomb)")
	}

	var hdr [zipLocalLen]byte
	if _, err := z.f.ReadAt(hdr[:], e.offset); err != nil || binary.LittleEndian.Uint32(hdr[:]) != zipSigLocal {
		return z.fail(ErrIrreproducible, pkErr, e.name, "bad zipfile offset (local header sig)")
	}

	flags := binary.LittleEndian.Uint16(hdr[6:])
	method := binary.LittleEndian.Uint16(hdr[8:])
	crc := binary.LittleEndian.Uint32(hdr[14:])
	csize := int64(binary.LittleEndian.Uint32(hdr[18:]))
	usize := int64(binary.LittleEndian.Uint32(hdr[22:]))
	nameLen := int64(binary.LittleEndian.Uint16(hdr[26:]))
	extraLen := int64(binary.LittleEndian.Uint16(hdr[28:]))

	dataStart := e.offset + zipLocalLen + nameLen + extraLen

	local := make([]byte, nameLen+extraLen)
	if _, err := z.f.ReadAt(local, e.offset+zipLocalLen); err != nil {
		return z.fail(ErrCorrupt, pkErr, e.name, "truncated local header")
	}

	descriptor := e.flags&8 != 0

	switch {
	case flags&(1<<11) != e.flags&(1<<11):
		return z.fail(ErrIrreproducible, pkWarn, e.name, "local and central GPFlags bit 11 differ")
	case method != e.method:
		return z.fail(ErrIrreproducible, pkErr, e.name, "local and central compression methods differ")
	case flags&8 != e.flags&8:
		return z.fail(ErrIrreproducible, pkErr, e.name, "local and central data descriptor flags differ")
	case !descriptor && (crc != e.crc || csize != e.csize || usize != e.usize):
		// unzip would trust the local header; refuse rather than pick.
		return z.fail(ErrIrreproducible, pkErr, e.name, "local and central sizes or CRC differ")
	case e.method == methodStored && e.csize != e.usize:
		return z.fail(ErrIrreproducible, pkWarn, e.name, "compressed and uncompressed sizes differ for a stored entry")
	}

	localName, unipath, err := z.decodeName(local[:nameLen], local[nameLen:], e, true)
	if err != nil {
		return err
	}

	if localName != e.name {
		return z.fail(ErrIrreproducible, pkWarn, e.name, "mismatching local filename (%s)", localName)
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

	if crc == zipSigDesc && (e.crc != zipSigDesc ||
		(clen == zipSigDesc && (uint32(e.csize) != zipSigDesc || (ulen == zipSigDesc && uint32(e.usize) != zipSigDesc)))) {
		end += 4
		if end > z.size {
			return 0, z.fail(ErrCorrupt, pkErr, e.name, "truncated data descriptor")
		}
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

	dirs, last, isDir, err := z.mapname(name)
	if err != nil {
		return err
	}

	// checkdir(APPEND_DIR): each directory component is created with
	// mkdir(0777) unless it exists, and must be a directory.
	created := false
	path := ""

	for _, c := range dirs {
		if path != "" {
			path += "/"
		}

		path += c

		switch n := z.b.get(path); {
		case n == nil:
			if _, err := z.b.add(Entry{Path: path, Kind: Dir, Mode: 0o777, Umask: true}); err != nil {
				return err
			}

			created = true
		case n.Kind != Dir:
			return z.fail(ErrIrreproducible, pkErr, name, "checkdir error:  %s exists but is not directory", path)
		}
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

	if path != "" {
		path += "/"
	}

	path += last

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

		_, err = z.b.add(Entry{Path: path, Kind: Symlink, Link: target})

		return err
	}

	_, err = z.b.add(Entry{
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

// mapname splits an entry name into the directories to create and the final
// name as unix.c's mapname() does: "." components and empty ones vanish,
// control characters are dropped, a VMS version suffix is removed and a
// final "." or ".." becomes "_" or "__". A ".." directory component makes
// unzip warn, so it is refused.
func (z *zipPlanner) mapname(name string) (dirs []string, last string, isDir bool, err error) {
	var comp []byte

	lastSemi := -1

	for i := range len(name) {
		c := name[i]
		switch {
		case c == '/':
			switch s := string(comp); s {
			case "", ".":
			case "..":
				return nil, "", false, z.fail(ErrIrreproducible, pkWarn, name, "warning:  skipped \"../\" path component(s) in %s", name)
			default:
				dirs = append(dirs, s)
			}

			comp, lastSemi = comp[:0], -1
		case c == ';':
			lastSemi = len(comp)
			comp = append(comp, c)
		case c >= 0x20 && c <= 0x7e, c >= 0x80 && c <= 0xfe:
			comp = append(comp, c)
		case c == 0xff && z.locale == LocaleOther:
			// isprint(0xff) depends on the locale.
			return nil, "", false, z.fail(ErrIrreproducible, 0, name, "name contains byte 0xff")
		default:
			// isprint() fails: the character is dropped.
		}
	}

	if strings.HasSuffix(name, "/") {
		return dirs, "", true, nil
	}

	// Without -V, a trailing ";<digits>" is a VMS version number.
	if lastSemi >= 0 {
		j := lastSemi + 1
		for j < len(comp) && comp[j] >= '0' && comp[j] <= '9' {
			j++
		}

		if j == len(comp) {
			comp = comp[:lastSemi]
		}
	}

	switch last = string(comp); last {
	case ".":
		last = "_"
	case "..":
		last = "__"
	case "":
		return nil, "", false, z.fail(ErrIrreproducible, pkErr, name, "mapname:  conversion of %s failed", name)
	}

	return dirs, last, false, nil
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

	name = extASCIIToNative(raw, e, local)

	if len(extra) == 0 {
		return name, false, nil
	}

	var utf8Name []byte

	if e.flags&(1<<11) != 0 {
		utf8Name = raw
	} else if utf8Name, unipath, err = z.unicodePath(raw, extra, entry); err != nil {
		return "", false, err
	}

	if utf8Name == nil {
		return name, false, nil
	}

	switch z.locale {
	case LocaleUTF8:
		name = string(utf8Name)
	case LocaleC:
		escaped, ok := escapeToC(utf8Name)
		if !ok {
			return "", false, z.fail(ErrIrreproducible, pkErr, entry, "warning:  Unicode filename corrupt.")
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

// extASCIIToNative is Ext_ASCII_TO_Native: names from DOS, OS/2 and WinZip
// 5.0 hosts are in the OEM code page and become ISO 8859-1; every other name
// is kept as is.
func extASCIIToNative(raw []byte, e *zipEntry, local bool) string {
	hasUxAtt := e.extAttr&0xffff0000 != 0
	oem := (e.hostNum == hostFAT && !((local || hasUxAtt) && (e.hostVer == 25 || e.hostVer == 26 || e.hostVer == 40))) ||
		e.hostNum == hostHPFS || (e.hostNum == hostNTFS && e.hostVer == 50)

	if !oem || isASCII(raw) {
		return string(raw)
	}

	out := make([]byte, len(raw))
	for i, c := range raw {
		if c >= 0x80 {
			c = oem2iso850[c&0x7f]
		}

		out[i] = c
	}

	return string(out)
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
	files []zipFile
}

func (c *zipContent) readFiles(a *Archive, fn func(e *Entry, r io.Reader) error) error {
	order := make([]*Entry, 0, len(c.files))
	for i := range a.entries {
		if a.entries[i].Kind == File {
			order = append(order, &a.entries[i])
		}
	}

	slices.SortFunc(order, func(x, y *Entry) int {
		return cmp.Compare(c.files[x.src].dataStart, c.files[y.src].dataStart)
	})

	dec := &zipDecoders{}

	for _, e := range order {
		r, err := newZipReader(a.file, c.files[e.src], e.Path, dec)
		if err != nil {
			return err
		}

		if err := consume(e, r, fn); err != nil {
			return err
		}
	}

	return nil
}

// zipDecoders keeps decompressors for reuse across entries.
type zipDecoders struct {
	buf   *bufio.Reader
	flate io.ReadCloser
}

func newZipReader(f *os.File, file zipFile, entry string, dec *zipDecoders) (io.Reader, error) {
	if dec == nil {
		dec = &zipDecoders{}
	}

	src := io.NewSectionReader(f, file.dataStart, file.csize)

	var r io.Reader

	switch file.method {
	case methodStored:
		r = src
	case methodDeflate:
		if dec.buf == nil {
			dec.buf = bufio.NewReaderSize(src, 32<<10)
			dec.flate = flate.NewReader(dec.buf)
		} else {
			dec.buf.Reset(src)
			if err := dec.flate.(flate.Resetter).Reset(dec.buf, nil); err != nil {
				return nil, err
			}
		}

		r = dec.flate
	case methodBzip2:
		r = bzip2.NewReader(src)
	}

	return &checkReader{r: r, size: file.usize, want: file.crc, entry: entry}, nil
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

	if c.err != nil && c.err != io.EOF {
		return 0, c.err
	}

	return n, c.err
}
