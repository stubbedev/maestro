// Ports the parts of PHP's phar extension (ext/phar, PHP 8.4) behind the
// PharData calls PharArchiver makes: the constructor's file name checks,
// buildFromIterator() with a base directory, addEmptyDir(), the tar and
// zip writers, and compress().

package archiver

import (
	"archive/zip"
	"bufio"
	"compress/gzip"
	"compress/zlib"
	"encoding/binary"
	"hash/crc32"
	"io"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/dsnet/compress/bzip2"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
)

// pharPermMask is PHAR_ENT_PERM_MASK.
const pharPermMask = 0o777

// pharDirPerms is PHAR_ENT_PERM_DEF_DIR, the mode of addEmptyDir()'s
// entries.
const pharDirPerms = 0o777

// pharEntry is an entry of a phar manifest.
type pharEntry struct {
	name   string // without a trailing slash for directories
	isDir  bool
	perms  uint16
	source string // the file whose contents the entry holds
}

// pharData is a new PharData archive: entries are written, in manifest
// order, when flush is called.
type pharData struct {
	fname   string
	isZip   bool
	now     time.Time
	entries []pharEntry
	index   map[string]int
}

// newPharData is `new PharData($fname, ..., $format)` for a file that does
// not exist yet; zip is the format being Phar::ZIP. Like PharData it fails
// with an UnexpectedValueException when the name has no usable extension
// or its directory does not exist, and makes a zip whatever the format
// when the extension's first "z" starts "zip".
func newPharData(fname string, zip bool, now time.Time) (*pharData, error) {
	ext, ok := pharDetectExt(fname)
	if !ok {
		return nil, &util.UnexpectedValueError{Message: "Cannot create phar '" + fname + "', file extension (or combination) not recognised or the directory does not exist"}
	}

	// phar_create_or_parse_filename: an extension whose first "z" starts
	// "zip" makes a zip, then the constructor converts a brand new tar to
	// the requested zip format
	if i := strings.IndexByte(ext, 'z'); i >= 0 && strings.HasPrefix(ext[i:], "zip") {
		zip = true
	}

	return &pharData{fname: fname, isZip: zip, now: now, index: map[string]int{}}, nil
}

// pharDetectExt ports phar_detect_phar_fname_ext() for a data phar being
// created: the extension that makes fname an archive name. Phar aliases,
// URLs and already open phars do not occur here.
func pharDetectExt(filename string) (string, bool) {
	if len(filename) <= 1 {
		return "", false
	}

	pos := strings.IndexByte(filename[1:], '.')
	if pos < 0 {
		return "", false
	}

	pos++

	nextDot := func(from int) int {
		if i := strings.IndexByte(filename[from+1:], '.'); i >= 0 {
			return from + 1 + i
		}

		return -1
	}

	for {
		for filename[pos-1] == '/' {
			if pos = nextDot(pos); pos < 0 {
				return "", false
			}
		}

		slash := strings.IndexByte(filename[pos:], '/')
		if slash < 0 {
			ext := filename[pos:]

			return ext, pharCheckStr(filename, pos, len(ext))
		}

		if pharCheckStr(filename, pos, slash) {
			return filename[pos : pos+slash], true
		}

		if pos = nextDot(pos); pos < 0 {
			return "", false
		}
	}
}

// pharCheckStr ports phar_check_str() for data phars: an extension needs a
// character after its dot that is not a dot or slash, and must not hold a
// ".phar" extension.
func pharCheckStr(fname string, extStart, extLen int) bool {
	if extLen >= 50 {
		return false
	}

	ext := fname[extStart : extStart+extLen]
	if i := strings.Index(ext, ".phar"); i >= 0 && (i+5 == len(ext) || ext[i+5] == '.') {
		return false
	}

	if len(ext) < 2 || ext[1] == '.' || ext[1] == '/' {
		return false
	}

	return pharAnalyzePath(fname[:extStart+extLen])
}

// pharAnalyzePath ports phar_analyze_path() when creating: the name with
// this extension must not exist, and its directory must.
func pharAnalyzePath(filename string) bool {
	if _, err := os.Stat(filename); err == nil {
		return false
	}

	slash := strings.LastIndexByte(filename, '/')
	if slash < 0 {
		// relative to the working directory
		return true
	}

	return php.IsDir(filename[:slash])
}

// pharPathCheck ports phar_path_check(): the entry name phar keeps (a
// "?" ends it, as it would a URL's path) and what makes it invalid, or ""
// for a valid one.
func pharPathCheck(name string) (string, string) {
	for i := 0; i < len(name); i++ {
		c := name[i]

		switch {
		case c == '?':
			return name[:i], ""
		case c == '/':
			rest := name[i+1:]

			switch {
			case strings.HasPrefix(rest, "/"):
				return name, "double slash"
			case rest == ".." || strings.HasPrefix(rest, "../"):
				return name, "upper directory reference"
			case rest == "." || strings.HasPrefix(rest, "./"):
				return name, "current directory reference"
			}
		case c == '\\':
			return name, "back-slash"
		case c == '*':
			return name, "star"
		case c >= 1 && c <= 0o31:
			// the scanner's range is [\001-\031], octal
			return name, "illegal character"
		case c >= utf8.RuneSelf:
			r, size := utf8.DecodeRuneInString(name[i:])
			if r == utf8.RuneError && size <= 1 {
				return name, "illegal character"
			}

			i += size - 1
		}
	}

	return name, ""
}

// isMagicPhar reports whether a name is in phar's magic .phar directory.
func isMagicPhar(name string) bool {
	return name == ".phar" || strings.HasPrefix(name, ".phar/")
}

// addFile is what buildFromIterator() does with an SplFileInfo: the entry
// is named after the file's real path relative to base; files in the
// magic .phar directory are skipped.
func (p *pharData) addFile(base, pathname string) error {
	fname, ok := php.Realpath(pathname)
	if !ok {
		fname = pathname
	}

	// fname must continue base with a slash: IS_SLASH, which takes a
	// backslash too on Windows, where the key is unixified.
	if !strings.HasPrefix(fname, base) || len(fname) <= len(base) ||
		fname[len(base)] != '/' && (fname[len(base)] != '\\' || !util.IsWindows()) {
		return &util.UnexpectedValueError{Message: `Iterator Composer\Package\Archiver\ArchivableFilesFilter returned a path "` + fname + `" that is not in the base directory "` + base + `"`}
	}

	key := fname[len(base)+1:]
	if util.IsWindows() {
		key = strings.ReplaceAll(key, `\`, "/")
	}

	fi, err := os.Stat(fname)
	if err == nil {
		var fp *os.File
		if fp, err = os.Open(fname); err == nil {
			_ = fp.Close()
		}
	}

	if err != nil {
		return &util.UnexpectedValueError{Message: `Iterator Composer\Package\Archiver\ArchivableFilesFilter returned a file that could not be opened "` + fname + `"`}
	}

	// silently skip any files that would be added to the magic .phar
	// directory
	if isMagicPhar(key) {
		return nil
	}

	name, problem := pharPathCheck(key)
	if problem != "" {
		return &BadMethodCallError{Message: "Entry " + key + ` cannot be created: phar error: invalid path "` + key + `" contains ` + problem}
	}

	p.set(pharEntry{name: name, perms: uint16(statPerms(fname, fi.Mode(), util.IsWindows()) & pharPermMask), source: fname})

	return nil
}

// addEmptyDir is PharData::addEmptyDir().
func (p *pharData) addEmptyDir(name string) error {
	if isMagicPhar(name) {
		return &BadMethodCallError{Message: `Cannot create a directory in magic ".phar" directory`}
	}

	entryName, problem := pharPathCheck(name)
	if problem != "" {
		return &BadMethodCallError{Message: "Directory " + name + ` does not exist and cannot be created: phar error: invalid path "` + name + `" contains ` + problem}
	}

	p.set(pharEntry{name: entryName, isDir: true, perms: pharDirPerms})

	return nil
}

// set adds an entry, replacing one of the same name in place.
func (p *pharData) set(e pharEntry) {
	if i, ok := p.index[e.name]; ok {
		p.entries[i] = e

		return
	}

	p.index[e.name] = len(p.entries)
	p.entries = append(p.entries, e)
}

// check is the part of flush that can fail before anything is written: tar
// headers only hold names of up to 256 bytes, split at a slash.
func (p *pharData) check() error {
	if p.isZip {
		return nil
	}

	for _, e := range p.entries {
		if _, _, ok := tarSplitName(e.name); !ok {
			return &PharError{Message: `tar-based phar "` + p.fname + `" cannot be created, filename "` + e.name + `" is too long for tar file format`}
		}
	}

	return nil
}

// flush writes the archive, as phar_flush() does; an empty archive is not
// written.
func (p *pharData) flush() error {
	if len(p.entries) == 0 {
		return nil
	}

	return p.writeFile(p.fname, func(w io.Writer) error { return p.write(w) })
}

// compress is PharData::compress($compression) for a tar: the archive is
// written compressed beside the original, named after it with its last
// extension replaced.
func (p *pharData) compress(compression string) error {
	newname := p.fname
	if i := strings.LastIndexByte(p.fname, '.'); i > strings.LastIndexByte(p.fname, '/') {
		newname = p.fname[:i]
	}

	return p.writeFile(newname+"."+compression, func(w io.Writer) error {
		return compressTo(w, compression, false, func(cw io.Writer) error { return p.write(cw) })
	})
}

// compressTo writes what write produces through the compression of a
// tar.gz or tar.bz2 archive: gzip with zlib's defaults (the zlib.deflate
// stream filter), or bzip2 with 900k blocks (the bzip2.compress filter).
// standalone selects gzcompress()/bzcompress() instead: a zlib stream, or
// bzip2 with 400k blocks.
func compressTo(w io.Writer, compression string, standalone bool, write func(io.Writer) error) error {
	var cw io.WriteCloser

	switch {
	case compression == "tar.gz" && standalone:
		cw = zlib.NewWriter(w)
	case compression == "tar.gz":
		gz := gzip.NewWriter(w)
		gz.OS = 3 // zlib's OS_CODE on Unix

		cw = gz
	default:
		level := 9
		if standalone {
			level = 4
		}

		bz, err := bzip2.NewWriter(w, &bzip2.WriterConfig{Level: level})
		if err != nil {
			return err
		}

		cw = bz
	}

	if err := write(cw); err != nil {
		return err
	}

	return cw.Close()
}

// writeFile creates path with what write produces.
func (p *pharData) writeFile(path string, write func(io.Writer) error) error {
	f, err := os.Create(path)
	if err != nil {
		return &PharError{Message: `unable to open new phar "` + path + `" for writing`}
	}

	bw := bufio.NewWriter(f)

	err = write(bw)
	if err == nil {
		err = bw.Flush()
	}

	if cerr := f.Close(); err == nil {
		err = cerr
	}

	return err
}

// write writes the archive in its format.
func (p *pharData) write(w io.Writer) error {
	if p.isZip {
		return p.writeZip(w)
	}

	return p.writeTar(w)
}

// writeTar ports phar_tar_flush() for a data phar: ustar headers with
// uid/gid, user/group names and device numbers left empty, then two zero
// blocks.
func (p *pharData) writeTar(w io.Writer) error {
	mtime := p.now.Unix()

	for _, e := range p.entries {
		var data []byte

		if !e.isDir {
			var err error
			if data, err = os.ReadFile(e.source); err != nil {
				return err
			}
		}

		header, err := p.tarHeader(e, int64(len(data)), mtime)
		if err != nil {
			return err
		}

		if _, err := w.Write(header); err != nil {
			return err
		}

		if _, err := w.Write(data); err != nil {
			return err
		}

		if pad := len(data) % 512; pad != 0 {
			if _, err := w.Write(make([]byte, 512-pad)); err != nil {
				return err
			}
		}
	}

	_, err := w.Write(make([]byte, 1024))

	return err
}

// tarHeader builds the 512-byte header of an entry.
func (p *pharData) tarHeader(e pharEntry, size, mtime int64) ([]byte, error) {
	h := make([]byte, 512)

	name, prefix, ok := tarSplitName(e.name)
	if !ok {
		return nil, &PharError{Message: `tar-based phar "` + p.fname + `" cannot be created, filename "` + e.name + `" is too long for tar file format`}
	}

	copy(h[0:100], name)
	copy(h[345:500], prefix)
	tarOctal(h[100:107], int64(e.perms))

	if !tarOctal(h[124:135], size) {
		return nil, &PharError{Message: `tar-based phar "` + p.fname + `" cannot be created, file "` + e.name + `" is too large for tar file format`}
	}

	tarOctal(h[136:147], mtime)
	copy(h[148:156], "        ")

	h[156] = '0'
	if e.isDir {
		h[156] = '5'
	}

	copy(h[257:263], "ustar\x00")
	copy(h[263:265], "00")

	var sum int64
	for _, b := range h {
		sum += int64(b)
	}

	tarOctal(h[148:155], sum)

	return h, nil
}

// tarSplitName splits a name over the ustar name and prefix fields, as
// phar_tar_writeheaders_int() does.
func tarSplitName(name string) (n, prefix string, ok bool) {
	if len(name) <= 100 {
		return name, "", true
	}

	if len(name) > 256 {
		return "", "", false
	}

	boundary := strings.IndexByte(name[len(name)-101:], '/')
	if boundary < 0 {
		return "", "", false
	}

	boundary += len(name) - 101
	if boundary > 155 {
		return "", "", false
	}

	return name[boundary+1:], name[:boundary], true
}

// tarOctal is phar_tar_octal(): val as len(buf) octal digits, or all
// sevens when it does not fit.
func tarOctal(buf []byte, val int64) bool {
	for i := len(buf) - 1; i >= 0; i-- {
		buf[i] = byte('0' + val&7)
		val >>= 3
	}

	if val == 0 {
		return true
	}

	for i := range buf {
		buf[i] = '7'
	}

	return false
}

// pharZipPermsTag is PHAR_ZIP_PERMS, the "nu" (ASi Unix) extra field phar
// stores entry permissions in.
const pharZipPermsTag = 0x756e

// writeZip ports phar_zip_flush() for a data phar: stored entries with the
// UTF-8 flag, version 0, no external attributes, and the permissions in an
// ASi Unix extra field whose CRC covers the mode only.
func (p *pharData) writeZip(w io.Writer) error {
	date, tm := dosTime(p.now, false)
	zw := zip.NewWriter(w)

	for _, e := range p.entries {
		var data []byte

		name := e.name
		if e.isDir {
			name += "/"
		} else {
			var err error
			if data, err = os.ReadFile(e.source); err != nil {
				return err
			}
		}

		extra := make([]byte, 18)
		binary.LittleEndian.PutUint16(extra[0:], pharZipPermsTag)
		binary.LittleEndian.PutUint16(extra[2:], 14)
		binary.LittleEndian.PutUint16(extra[8:], e.perms)
		binary.LittleEndian.PutUint32(extra[4:], crc32.ChecksumIEEE(extra[8:10]))

		fw, err := zw.CreateRaw(&zip.FileHeader{
			Name:               name,
			Flags:              0x800,
			Method:             zip.Store,
			ModifiedTime:       tm,   //nolint:staticcheck // the DOS fields alone: Modified adds an extra field
			ModifiedDate:       date, //nolint:staticcheck // as above
			CRC32:              crc32.ChecksumIEEE(data),
			CompressedSize64:   uint64(len(data)),
			UncompressedSize64: uint64(len(data)),
			Extra:              extra,
		})
		if err != nil {
			return err
		}

		if _, err := fw.Write(data); err != nil {
			return err
		}
	}

	return zw.Close()
}

// dosTime converts t to an MS-DOS date and time in local time. clampYear
// is libzip's handling of years before 1980; phar only sees the current
// time.
func dosTime(t time.Time, clampYear bool) (date, tm uint16) {
	t = t.Local()

	year := t.Year()
	if clampYear && year < 1980 {
		year = 1980
	}

	date = uint16((year-1980)<<9 + int(t.Month())<<5 + t.Day()) //nolint:gosec // wraps as the C code does
	tm = uint16(t.Hour()<<11 + t.Minute()<<5 + t.Second()>>1)   //nolint:gosec // fits 16 bits
	return date, tm
}
