// Ports src/Composer/Package/Archiver/ZipArchiver.php, writing what PHP's
// ZipArchive (libzip 1.11) writes for it.

package archiver

import (
	"archive/zip"
	"bufio"
	"bytes"
	"compress/flate"
	"errors"
	"hash/crc32"
	"io/fs"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/util"
)

// libzip's header constants: "version made by" 6.3 on Unix, "version
// needed" 2.0, the general purpose flags for UTF-8 names and for maximum
// deflate compression, and the attributes of addEmptyDir()'s entries
// (drwxrwxrwx; ZipArchiver's setExternalAttributesName() call names the
// directory without its slash and so does not reach them).
const (
	libzipMadeBy      = 3<<8 | 63
	libzipNeeded      = 20
	libzipFlagUTF8    = 0x800
	libzipFlagMaxComp = 0x2
	libzipDirAttrs    = (0o40000 | 0o777) << 16
)

// ZipArchiver ports ZipArchiver.
type ZipArchiver struct {
	// now gives the directories' timestamps (libzip uses the current
	// time).
	now func() time.Time
}

// NewZipArchiver returns a ZipArchiver.
func NewZipArchiver() *ZipArchiver { return &ZipArchiver{now: time.Now} }

// zipEntry is an entry added to the ZipArchive.
type zipEntry struct {
	name   string
	isDir  bool
	source string
	mode   fs.FileMode // of the source, links followed
	mtime  time.Time
}

// Archive ports ZipArchiver::archive. Files are deflated at libzip's
// level 9, or stored when that does not make them smaller; file
// attributes are the files' modes and their times the modification times.
func (a *ZipArchiver) Archive(sources, target, _ string, excludes []string, ignoreFilters bool) (string, error) {
	if sourcesRealpath, ok := util.RealpathOK(sources); ok {
		sources = sourcesRealpath
	}

	sources = util.NormalizePath(sources)

	files, err := NewArchivableFilesFinder(sources, excludes, ignoreFilters)
	if err != nil {
		return "", err
	}

	now := a.now()
	entries := make([]zipEntry, 0, len(files.Files()))

	for _, file := range files.Files() {
		relativePath := file.RelativePathname
		if util.IsWindows() {
			relativePath = strings.ReplaceAll(relativePath, `\`, "/")
		}

		if file.IsDir {
			entries = append(entries, zipEntry{name: relativePath + "/", isDir: true, mtime: now})

			continue
		}

		fi, err := os.Stat(file.Pathname)
		if err != nil {
			// the file went away since the finder saw it: addFile() fails
			// quietly, fileperms() with a warning
			return "", &util.ErrorException{Message: "fileperms(): stat failed for " + file.Pathname, Site: phperr.At("ZipArchiver.php", 65)}
		}

		entries = append(entries, zipEntry{name: relativePath, source: file.Pathname, mode: fi.Mode(), mtime: fi.ModTime()})
	}

	if len(entries) == 0 {
		// libzip writes no file for an archive without entries: create
		// minimal valid ZIP file (Empty Central Directory + End of Central
		// Directory record)
		if !fileExists(target) {
			if err := os.WriteFile(target, emptyZip, 0o666); err != nil {
				return "", &util.ErrorException{Message: "file_put_contents(" + target + "): Failed to open stream: " + util.Strerror(err), Site: phperr.At("ZipArchiver.php", 87)}
			}
		}

		return target, nil
	}

	return target, writeLibzip(target, entries)
}

// writeLibzip is ZipArchive::close(): the archive is written to a
// temporary file beside the target and renamed onto it.
func writeLibzip(target string, entries []zipEntry) error {
	tmp, err := createTemp(target)
	if err != nil {
		return &util.ErrorException{Message: "ZipArchive::close(): Failure to create temporary file: " + util.Strerror(err), Site: phperr.At("ZipArchiver.php", 73)}
	}

	defer func() { _ = os.Remove(tmp.Name()) }()

	bw := bufio.NewWriter(tmp)
	zw := zip.NewWriter(bw)

	err = writeLibzipEntries(zw, entries)
	if err == nil {
		err = zw.Close()
	}

	if err == nil {
		err = bw.Flush()
	}

	if cerr := tmp.Close(); err == nil && cerr != nil {
		err = &util.ErrorException{Message: "ZipArchive::close(): Write error: " + util.Strerror(cerr), Site: phperr.At("ZipArchiver.php", 73)}
	}

	if err != nil {
		return err
	}

	if err := os.Rename(tmp.Name(), target); err != nil {
		return &util.ErrorException{Message: "ZipArchive::close(): Renaming temporary file failed: " + util.Strerror(err), Site: phperr.At("ZipArchiver.php", 73)}
	}

	return nil
}

func writeLibzipEntries(zw *zip.Writer, entries []zipEntry) error {
	for _, e := range entries {
		var data []byte

		h := &zip.FileHeader{
			Name:           e.name,
			CreatorVersion: libzipMadeBy,
			ReaderVersion:  libzipNeeded,
			Method:         zip.Store,
			ExternalAttrs:  libzipDirAttrs,
		}

		h.ModifiedDate, h.ModifiedTime = dosTime(e.mtime, true) //nolint:staticcheck // the DOS fields alone: Modified adds an extra field

		if !e.isDir {
			var err error
			if data, err = os.ReadFile(e.source); err != nil {
				return &util.ErrorException{Message: "ZipArchive::close(): Can't open file: " + util.Strerror(err), Site: phperr.At("ZipArchiver.php", 73)}
			}

			h.ExternalAttrs = unixMode(e.mode) << 16
			h.CRC32 = crc32.ChecksumIEEE(data)
			h.UncompressedSize64 = uint64(len(data))
		}

		raw := data

		if len(data) > 0 {
			var buf bytes.Buffer

			fw, _ := flate.NewWriter(&buf, flate.BestCompression)
			_, _ = fw.Write(data)
			_ = fw.Close()

			if buf.Len() < len(data) {
				raw = buf.Bytes()
				h.Method = zip.Deflate
				h.Flags |= libzipFlagMaxComp
			}
		}

		h.CompressedSize64 = uint64(len(raw))

		if !isASCII(e.name) && utf8.ValidString(e.name) {
			h.Flags |= libzipFlagUTF8
		}

		w, err := zw.CreateRaw(h)
		if err != nil {
			return err
		}

		if _, err := w.Write(raw); err != nil {
			return err
		}
	}

	return nil
}

// createTemp creates libzip's temporary file, target.XXXXXX, with the
// permissions a new file gets.
func createTemp(target string) (*os.File, error) {
	for {
		f, err := os.OpenFile(target+"."+randomHex(3), os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o666) //nolint:gosec // a new archive is 0666 & ~umask, as libzip makes it
		if !errors.Is(err, fs.ErrExist) {
			return f, err
		}
	}
}

// unixMode is fileperms(): the st_mode of a file.
func unixMode(m fs.FileMode) uint32 {
	mode := uint32(m.Perm())

	if m&fs.ModeSetuid != 0 {
		mode |= 0o4000
	}

	if m&fs.ModeSetgid != 0 {
		mode |= 0o2000
	}

	if m&fs.ModeSticky != 0 {
		mode |= 0o1000
	}

	switch {
	case m&fs.ModeDir != 0:
		mode |= 0o40000
	case m&fs.ModeNamedPipe != 0:
		mode |= 0o10000
	case m&fs.ModeSocket != 0:
		mode |= 0o140000
	case m&fs.ModeCharDevice != 0:
		mode |= 0o20000
	case m&fs.ModeDevice != 0:
		mode |= 0o60000
	default:
		mode |= 0o100000
	}

	return mode
}

func isASCII(s string) bool {
	for i := range len(s) {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}

	return true
}

// Supports ports ZipArchiver::supports; ZipArchive is always available.
func (a *ZipArchiver) Supports(format string, _ pkg.NullString) bool {
	return format == "zip"
}
