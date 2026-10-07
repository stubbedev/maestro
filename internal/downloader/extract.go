// Ports the extract() methods of src/Composer/Downloader/{ZipDownloader,
// TarDownloader,XzDownloader,GzipDownloader,RarDownloader,PharDownloader}.php:
// for zip, tar, xz and gzip, how their failures read when the native
// extraction (internal/archive) refuses an archive; for rar and phar, the
// extraction itself.

package downloader

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/stubbedev/maestro/internal/archive"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/util"
)

// unzipPath is the unzip ZipDownloader would run, as its messages name it
// (ExecutableFinder::find('unzip')).
var unzipPath = sync.OnceValue(func() string {
	if path, ok := util.NewExecutableFinder().Find("unzip"); ok {
		return path
	}

	return "unzip"
})

// xzMagic starts every xz stream.
var xzMagic = []byte{0xfd, '7', 'z', 'X', 'Z', 0}

// extractionError turns a failed extraction into the exception Composer's
// extractor throws for that archive, and the warnings it prints first.
// Where Composer's extractor would have succeeded differently (unzip
// falling back to ZipArchive, gzip to zlib) maestro fails instead, saying
// why.
func (d *FileDownloader) extractionError(p pkg.PackageInterface, file, path string, err error) ([]string, error) {
	var ae *archive.Error
	if !errors.As(err, &ae) {
		return nil, err
	}

	generic := &util.RuntimeError{Message: "Failed to extract " + p.Name() + ": " + ae.Error() + " (maestro cannot extract this archive as Composer would)"}

	if errors.Is(ae, archive.ErrLimit) {
		return nil, generic
	}

	switch ae.Format {
	case archive.Zip:
		return zipError(p, file, path, ae, generic)
	case archive.Tar:
		switch {
		case strings.HasPrefix(ae.Reason, "is a corrupted tar file"):
			return nil, &util.UnexpectedValueError{Message: `phar error: "` + file + `" ` + ae.Reason}
		case strings.HasPrefix(ae.Reason, "Cannot extract"):
			return nil, &util.UnexpectedValueError{Message: `Extraction from phar "` + file + `" failed: ` + ae.Reason}
		}
	case archive.Xz:
		if errors.Is(ae, archive.ErrCorrupt) {
			stderr := ae.Reason + "\n"
			if !hasPrefix(file, xzMagic) {
				stderr = "xz: (stdin): File format not recognized\ntar: Child returned status 1\ntar: Error is not recoverable: exiting now\n"
			}

			return nil, &util.RuntimeError{Message: "Failed to execute tar -xJf " + file + " -C " + path + "\n\n" + stderr}
		}
	case archive.Gzip:
	}

	return nil, generic
}

// zipErNoZip is ZipArchive::ER_NOZIP, the code of ZipDownloader's
// "is not a zip archive" exception.
const zipErNoZip = 19

// zipError is ZipDownloader's failure for an archive unzip rejects: unzip's
// error, then ZipArchive's.
func zipError(p pkg.PackageInterface, file, path string, ae *archive.Error, generic error) ([]string, error) {
	if ae.ExitCode == 0 {
		return nil, generic
	}

	processError := "Failed to extract " + p.Name() + ": (" + strconv.Itoa(ae.ExitCode) + ") " + unzipPath() + " -qq " + file + " -d " + path + "\n\n" + ae.Reason

	if errors.Is(ae, archive.ErrBomb) {
		return nil, &util.RuntimeError{Message: processError}
	}

	if errors.Is(ae, archive.ErrIrreproducible) {
		// ZipArchive would extract it, differently
		return nil, generic
	}

	warnings := []string{
		"    <warning>" + processError + "</warning>",
		"    The archive may contain identical file names with different capitalization (which fails on case insensitive filesystems)",
		"    Unzip with unzip command failed, falling back to ZipArchive class",
	}

	if fi, err := os.Stat(file); err == nil && fi.Size() == 0 {
		return warnings, &util.UnexpectedValueError{Message: "'" + file + "' is a corrupted zip archive (0 bytes), try again.", Code: -1}
	}

	if ae.ExitCode == 9 {
		// unzip found no zip structure: ZipArchive::open fails with ER_NOZIP
		return warnings, &util.UnexpectedValueError{Message: "'" + file + "' is not a zip archive.", Code: zipErNoZip}
	}

	return warnings, &util.RuntimeError{Message: `There was an error extracting the ZIP file for "` + p.Name() + `", it is either corrupted or using an invalid format.`}
}

// pharDataCheck reproduces how new \PharData($file) and extractTo() treat
// the temporary file's name, which internal/archive does not look at: an
// extension containing "zip" makes PharData open a tar as a zip, "phar" is
// refused, and ".phar" elsewhere in the path breaks the phar:// URL
// extractTo() iterates. Checked against PHP 8.4's ext/phar.
func pharDataCheck(file string) error {
	ext := ""
	if i := strings.LastIndexByte(file, '.'); i >= 0 && !strings.Contains(file[i:], "/") {
		ext = file[i+1:]
	}

	switch {
	case ext == "" || ext == "phar":
		return &util.UnexpectedValueError{Message: "Cannot create phar '" + file + "', file extension (or combination) not recognised or the directory does not exist"}
	case strings.Contains(ext, "zip"):
		return &util.UnexpectedValueError{Message: `phar zip error: phar "` + file + `" already exists as a regular phar and must be deleted from disk prior to creating as a zip-based phar`}
	case strings.Contains(file, ".phar"):
		return &util.UnexpectedValueError{Message: "RecursiveDirectoryIterator::__construct(phar://" + file + "/): Failed to open directory: '" + file +
			"' is not a phar archive. Use PharData::__construct() for a standard zip or tar archive\nphar url \"phar://" + file + "/\" is unknown"}
	}

	return nil
}

// pharCompressionCheck reproduces new \PharData($file) on a compressed tar
// when the PHP running Composer lacks the extension that decompresses it
// (ext/phar's phar_open_from_fp recognises gzip and bzip2 by their magic
// bytes and needs zlib or bz2 for them).
func pharCompressionCheck(file string, extensionLoaded func(name string) bool) error {
	f, err := os.Open(file)
	if err != nil {
		return nil
	}

	defer func() { _ = f.Close() }()

	return pharCompressionCheckAt(file, f, extensionLoaded)
}

// pharCompressionCheckAt is pharCompressionCheck reading the archive from
// src (which file names in the message): a store-backed cache hit checks
// the open cached archive, which Composer would have copied to file.
func pharCompressionCheckAt(file string, src io.ReaderAt, extensionLoaded func(name string) bool) error {
	head := make([]byte, 3)
	n, _ := src.ReadAt(head, 0)
	head = head[:n]

	switch {
	case bytes.HasPrefix(head, []byte{0x1f, 0x8b}) && !extensionLoaded("zlib"):
		return &util.UnexpectedValueError{Message: `unable to decompress gzipped phar archive "` + file + `" to temporary file, enable zlib extension in php.ini`}
	case bytes.HasPrefix(head, []byte("BZh")) && !extensionLoaded("bz2"):
		return &util.UnexpectedValueError{Message: `unable to decompress bzipped phar archive "` + file + `" to temporary file, enable bz2 extension in php.ini`}
	}

	return nil
}

// extractRar is RarDownloader::extract. maestro's PHP has no RarArchive
// class, so only unrar is tried.
func (a *ArchiveDownloader) extractRar(_ pkg.PackageInterface, file, path string) error {
	processError := ""

	// Try to use unrar on *nix
	if !util.IsWindows() {
		args := []string{"sh", "-c", `unrar x -- "$0" "$1" >/dev/null && chmod -R u+w "$1"`, file, path}

		var ignoredOutput string

		code, err := a.process.Execute(util.Cmd(args...), &ignoredOutput, "")
		if err != nil {
			return err
		}

		if code == 0 {
			return nil
		}

		processError = "Failed to execute " + strings.Join(args, " ") + "\n\n" + a.process.GetErrorOutput()
	}

	// php.ini path is added to the error message to help users find the
	// correct file
	iniMessage := util.IniGetMessage(a.iniFiles)

	if !util.IsWindows() {
		return &util.RuntimeError{Message: "Could not decompress the archive, enable the PHP rar extension.\n" + iniMessage}
	}

	return &util.RuntimeError{Message: "Could not decompress the archive, enable the PHP rar extension or install unrar.\n" + iniMessage + "\n" + processError}
}

// pharExtractScript is PharDownloader::extract, run by the user's php.
const pharExtractScript = `try { $archive = new \Phar($argv[1]); $archive->extractTo($argv[2], null, true); } catch (\Throwable $e) { fwrite(STDERR, $e->getMessage()); exit(1); }`

// extractPhar is PharDownloader::extract: \Phar::extractTo, which only PHP
// implements, run with the user's php.
func (a *ArchiveDownloader) extractPhar(_ pkg.PackageInterface, file, path string) error {
	php, ok := util.NewPhpExecutableFinder().Find()
	if !ok {
		return &util.RuntimeError{Message: "Could not find the php binary to extract the phar archive " + file}
	}

	var ignoredOutput string

	code, err := a.process.Execute(util.Cmd(php, "-r", pharExtractScript, file, path), &ignoredOutput, "")
	if err != nil {
		return err
	}

	if code != 0 {
		// Can throw an UnexpectedValueException
		return &util.UnexpectedValueError{Message: a.process.GetErrorOutput()}
	}

	return nil
}

// hasPrefix reports whether the file starts with prefix.
func hasPrefix(file string, prefix []byte) bool {
	f, err := os.Open(file)
	if err != nil {
		return false
	}

	defer func() { _ = f.Close() }()

	buf := make([]byte, len(prefix))
	n, _ := f.Read(buf)

	return bytes.Equal(buf[:n], prefix)
}
