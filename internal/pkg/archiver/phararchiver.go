// Ports src/Composer/Package/Archiver/PharArchiver.php and
// ArchivableFilesFilter.php.

package archiver

import (
	"bytes"
	"io"
	"os"
	"strings"
	"time"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/util"
)

// pharFormats is PharArchiver::$formats: whether each format is a zip
// (Phar::ZIP) or a tar (Phar::TAR).
var pharFormats = map[string]bool{"zip": true, "tar": false, "tar.gz": false, "tar.bz2": false}

// pharCompressFormats is PharArchiver::$compressFormats.
var pharCompressFormats = map[string]bool{"tar.gz": true, "tar.bz2": true}

// PharArchiver ports PharArchiver, which writes archives with PharData;
// phardata.go reproduces what PharData writes.
type PharArchiver struct {
	// now gives the entries' timestamps (PharData uses the current time).
	now func() time.Time
}

// NewPharArchiver returns a PharArchiver.
func NewPharArchiver() *PharArchiver { return &PharArchiver{now: time.Now} }

// Archive ports PharArchiver::archive.
func (a *PharArchiver) Archive(sources, target, format string, excludes []string, ignoreFilters bool) (string, error) {
	sources, sourcesOK := php.Realpath(sources)

	// Phar would otherwise load the file which we don't want
	if fileExists(target) {
		if err := os.Remove(target); err != nil {
			return "", &util.ErrorException{Message: "unlink(" + target + "): " + util.Strerror(err)}
		}
	}

	// substr($target, 0, strrpos($target, $format) - 1), where a format
	// not found or at the start makes the length -1
	filename := target[:max(len(target)-1, 0)]
	if pos := strings.LastIndex(target, format); pos > 0 {
		filename = target[:pos-1]
	}

	// Check if compress format
	if pharCompressFormats[format] {
		// Current compress format supported base on tar
		target = filename + ".tar"
	}

	isZip, known := pharFormats[format]
	if !known {
		return "", &util.ErrorException{Message: `Undefined array key "` + format + `"`}
	}

	wrap := func(err error) error {
		if _, ok := err.(*util.UnexpectedValueError); ok { //nolint:errorlint // PHP catches this class, not subclasses of others
			return &util.RuntimeError{Message: "Could not create archive '" + target + "' from '" + sources + "': " + err.Error(), Prev: err}
		}

		return err
	}

	phar, err := newPharData(target, isZip, a.now())
	if err != nil {
		return "", wrap(err)
	}

	if !sourcesOK {
		// realpath() returned false, which ArchivableFilesFinder's string
		// parameter does not take
		return "", &php.EngineError{Class: php.ClassTypeError, Message: `Composer\Package\Archiver\ArchivableFilesFinder::__construct(): Argument #1 ($sources) must be of type string, false given`}
	}

	files, err := NewArchivableFilesFinder(sources, excludes, ignoreFilters)
	if err != nil {
		return "", wrap(err)
	}

	if err := a.build(phar, files.Files(), sources); err != nil {
		return "", wrap(err)
	}

	if !fileExists(target) {
		target = filename + "." + format

		return target, writeEmptyArchive(target, format)
	}

	if pharCompressFormats[format] {
		// Delete old tar
		if err := os.Remove(target); err != nil {
			return "", &util.ErrorException{Message: "unlink(" + target + "): " + util.Strerror(err)}
		}

		// Compress the new tar
		if err := phar.compress(format); err != nil {
			return "", wrap(err)
		}

		// Make the correct filename
		target = filename + "." + format
	}

	return target, nil
}

// build is `$phar->buildFromIterator(new ArchivableFilesFilter($files),
// $sources)` followed by ArchivableFilesFilter::addEmptyDir: the files,
// then the (empty) directories the filter held back. PharData writes the
// archive after the files and after each directory, so a directory that
// fails leaves the archive written so far.
func (a *PharArchiver) build(phar *pharData, files []File, sources string) error {
	var dirs []string

	for _, f := range files {
		if f.IsDir {
			dirs = append(dirs, f.Pathname)

			continue
		}

		if err := phar.addFile(sources, f.Pathname); err != nil {
			return err
		}
	}

	if err := phar.check(); err != nil {
		return err
	}

	written := len(phar.entries)

	for _, filepath := range dirs {
		localname := strings.ReplaceAll(filepath, sources+"/", "")

		err := phar.addEmptyDir(localname)
		if err == nil {
			err = phar.check()
		}

		if err != nil {
			phar.entries = phar.entries[:written]

			if ferr := phar.flush(); ferr != nil {
				return ferr
			}

			return err
		}

		written = len(phar.entries)
	}

	if len(dirs) > 0 {
		return phar.flush()
	}

	return phar.flush()
}

// emptyZip is the minimal valid zip file Composer writes for an archive
// without entries: an end of central directory record.
var emptyZip = []byte{0x50, 0x4b, 0x05, 0x06, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}

// writeEmptyArchive writes what PharArchiver writes when PharData wrote
// nothing: an empty tar (10240 zero bytes), compressed with
// gzcompress()/bzcompress() for tar.gz/tar.bz2, or an empty zip.
func writeEmptyArchive(target, format string) error {
	write := func(data []byte) error {
		if err := os.WriteFile(target, data, 0o666); err != nil {
			return &util.ErrorException{Message: "file_put_contents(" + target + "): Failed to open stream: " + util.Strerror(err)}
		}

		return nil
	}

	emptyTar := make([]byte, 10240)

	switch format {
	case "tar":
		return write(emptyTar)
	case "zip":
		return write(emptyZip)
	}

	var buf bytes.Buffer
	if err := compressTo(&buf, format, true, func(w io.Writer) error {
		_, err := w.Write(emptyTar)

		return err
	}); err != nil {
		return err
	}

	return write(buf.Bytes())
}

// Supports ports PharArchiver::supports.
func (a *PharArchiver) Supports(format string, _ pkg.NullString) bool {
	_, ok := pharFormats[format]

	return ok
}

// fileExists is file_exists().
func fileExists(path string) bool {
	_, err := os.Stat(path)

	return err == nil
}
