package archive

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strconv"
)

// Format is a Composer dist type whose archives maestro extracts.
type Format uint8

const (
	// Zip is ZipDownloader's format, extracted as `unzip -qq` does.
	Zip Format = iota + 1
	// Tar is TarDownloader's format (tar, tar.gz, tar.bz2), extracted as
	// PharData::extractTo does.
	Tar
	// Xz is XzDownloader's format, extracted as `tar -xJf` does.
	Xz
	// Gzip is GzipDownloader's format: one gzip-compressed file.
	Gzip
)

// ParseFormat maps a dist type ("zip", "tar", "xz", "gzip") to its Format.
func ParseFormat(distType string) (Format, bool) {
	switch distType {
	case "zip":
		return Zip, true
	case "tar":
		return Tar, true
	case "xz":
		return Xz, true
	case "gzip":
		return Gzip, true
	}

	return 0, false
}

func (f Format) String() string {
	switch f {
	case Zip:
		return "zip"
	case Tar:
		return "tar"
	case Xz:
		return "xz"
	case Gzip:
		return "gzip"
	}

	return "format(" + strconv.Itoa(int(f)) + ")"
}

// rulesVersion is bumped whenever a change to this package can change the
// tree some archive extracts to, so trees cached under the old rules are not
// reused.
const rulesVersion = "1"

// Rules identifies everything that decides the tree an archive of format f
// extracts to, besides the archive and the umask: this package's rules and,
// for zip, the locale class unzip decodes names in. Caches of extracted trees
// key on it.
func Rules(f Format, opts *Options) string {
	r := f.String() + "/" + rulesVersion
	if f == Zip {
		r += "/" + opts.locale().String()
	}

	return r
}

// Kind is the type of an extracted entry.
type Kind uint8

const (
	// Dir is a directory.
	Dir Kind = iota + 1
	// File is a regular file.
	File
	// Symlink is a symbolic link.
	Symlink
)

func (k Kind) String() string {
	switch k {
	case Dir:
		return "dir"
	case File:
		return "file"
	case Symlink:
		return "symlink"
	}

	return "kind(" + strconv.Itoa(int(k)) + ")"
}

// Entry is one node of the extracted package tree.
type Entry struct {
	// Path is relative to the package directory and slash-separated; the
	// package directory itself is "". It holds the bytes the extractor would
	// have used as the name, which need not be valid UTF-8.
	Path string
	// Link is the target of a Symlink, verbatim.
	Link string
	// Size is the length of a File.
	Size int64
	// Kind is what the entry is.
	Kind Kind
	// Mode holds the permission bits (0o777 at most) the extractor gives
	// the entry, before the umask when Umask is set. Symlinks have none.
	Mode fs.FileMode
	// Umask reports that the extractor creates the entry with Mode masked
	// by the process umask (Composer's own directories, unzip's implied
	// directories and MS-DOS-made entries, PharData's directories, ...).
	Umask bool
	// src locates the file's content for the format's reader.
	src int
}

// Perm is the entry's final permission bits under the given umask.
func (e *Entry) Perm(umask fs.FileMode) fs.FileMode {
	if e.Umask {
		return e.Mode &^ umask
	}

	return e.Mode
}

// Locale is the class of LC_CTYPE unzip runs under, which decides how it
// decodes UTF-8 entry names.
type Locale uint8

const (
	// LocaleEnv means: take the locale from the environment (LC_ALL,
	// LC_CTYPE, LANG), as the unzip Composer starts would.
	LocaleEnv Locale = iota
	// LocaleUTF8 is any UTF-8 locale: UTF-8 names are kept as they are.
	LocaleUTF8
	// LocaleC is the C/POSIX locale: non-ASCII characters of UTF-8 names
	// become #Uxxxx escapes.
	LocaleC
	// LocaleOther is any other locale; names that would need converting to
	// it are refused.
	LocaleOther
)

func (l Locale) String() string {
	switch l {
	case LocaleUTF8:
		return "utf8"
	case LocaleC:
		return "c"
	case LocaleOther:
		return "other"
	}

	return "env"
}

// Options tune an extraction. The zero value (or nil) is ready to use.
type Options struct {
	// URL is the dist URL; GzipDownloader names the extracted file after it.
	URL string
	// Limits caps what an archive may expand to; zero fields take defaults.
	Limits Limits
	// Locale is the locale class unzip decodes names in.
	Locale Locale
}

func (o *Options) locale() Locale {
	if o == nil || o.Locale == LocaleEnv {
		return localeFromEnv()
	}

	return o.Locale
}

func (o *Options) limits() Limits {
	var l Limits
	if o != nil {
		l = o.Limits
	}

	return l.withDefaults()
}

// Limits bound what an archive may expand to, so a hostile archive cannot
// fill the disk or memory. Exceeding one fails with ErrLimit.
type Limits struct {
	// MaxEntries caps the number of entries (default 1,000,000).
	MaxEntries int
	// MaxFileSize caps one file's size (default 2 GiB).
	MaxFileSize int64
	// MaxTotalSize caps the sum of all file sizes (default 8 GiB).
	MaxTotalSize int64
	// MaxRatio caps the expanded size over the archive's size once the
	// expansion exceeds 64 MiB (default 1000).
	MaxRatio int64
}

const ratioFloor = 64 << 20

func (l Limits) withDefaults() Limits {
	if l.MaxEntries <= 0 {
		l.MaxEntries = 1_000_000
	}

	if l.MaxFileSize <= 0 {
		l.MaxFileSize = 2 << 30
	}

	if l.MaxTotalSize <= 0 {
		l.MaxTotalSize = 8 << 30
	}

	if l.MaxRatio <= 0 {
		l.MaxRatio = 1000
	}

	return l
}

var (
	// ErrIrreproducible means Composer's extractor would not extract the
	// archive cleanly (so Composer would fall back to another extractor or
	// fail), or the archive relies on behaviour maestro does not reproduce.
	ErrIrreproducible = errors.New("extraction cannot be reproduced exactly")
	// ErrBomb means unzip rejects the archive as a possible zip bomb, which
	// Composer reports without falling back.
	ErrBomb = errors.New("possible zip bomb")
	// ErrLimit means the archive exceeds maestro's extraction limits.
	ErrLimit = errors.New("archive exceeds the extraction limits")
	// ErrCorrupt means the archive cannot be read.
	ErrCorrupt = errors.New("corrupt archive")
)

// Error describes why an archive cannot be extracted. Err is one of the
// package's sentinel errors; errors.Is matches it.
type Error struct {
	Err error
	// Entry is the archive member concerned, if any.
	Entry string
	// Reason says what is wrong, in the extractor's words where it has
	// them (unzip's and PharData's messages are copied).
	Reason string
	// Format is the archive's format.
	Format Format
	// ExitCode is the status `unzip -qq` would exit with, for zip archives
	// unzip does not extract cleanly; 0 otherwise.
	ExitCode int
}

func (e *Error) Error() string {
	msg := e.Format.String() + ": "
	if e.Entry != "" {
		msg += strconv.Quote(e.Entry) + ": "
	}

	return msg + e.Reason
}

func (e *Error) Unwrap() error {
	return e.Err
}

// Unzip's exit codes (unzip.h's PK_* and IZ_* values).
const (
	pkWarn  = 1
	pkErr   = 2
	pkBomb  = 12
	izUnsup = 81
)

// Archive is an opened archive whose extraction has been planned.
type Archive struct {
	file    *os.File
	content contentReader
	entries []Entry
	limits  Limits
	format  Format
}

// contentReader streams the content of the files of a planned extraction.
type contentReader interface {
	readFiles(a *Archive, fn func(e *Entry, r io.Reader) error) error
}

// Open plans the extraction of the archive at path as Composer's extractor
// for format would perform it. It reads the archive's metadata (and, for
// compressed tars, decompresses it once) but writes nothing.
func Open(path string, format Format, opts *Options) (*Archive, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}

	a := &Archive{file: f, format: format, limits: opts.limits()}
	b := newBuilder(format, a.limits)

	switch format {
	case Zip:
		a.content, err = planZip(f, info.Size(), opts.locale(), b)
	case Tar:
		a.content, err = planPharTar(f, info.Size(), b)
	case Xz:
		a.content, err = planGNUTar(f, info.Size(), b)
	case Gzip:
		a.content, err = planGzip(f, info.Size(), opts, b)
	default:
		err = fmt.Errorf("archive: unknown format %v", format)
	}

	if err == nil {
		err = b.checkRatio(info.Size())
	}

	if err == nil {
		a.entries, err = b.finish()
	}

	if err != nil {
		_ = f.Close()
		return nil, err
	}

	return a, nil
}

// Entries is the planned package tree: the package directory ("") first,
// then every entry after its parent directory, in path order.
func (a *Archive) Entries() []Entry {
	return a.entries
}

// Format is the archive's format.
func (a *Archive) Format() Format {
	return a.format
}

// ReadFiles calls fn for every File entry, in the order the archive stores
// their content, with a reader of exactly that content. The reader fails if
// the archive's data does not match what was planned (wrong length, bad
// CRC, corrupt compression); fn must then fail too, and ReadFiles returns
// the reader's error. A reader fn leaves unread is drained, so the checks
// still run. fn must not retain r.
func (a *Archive) ReadFiles(fn func(e *Entry, r io.Reader) error) error {
	return a.content.readFiles(a, fn)
}

// Close releases the archive file.
func (a *Archive) Close() error {
	return a.file.Close()
}

// errorf builds an *Error.
func errorf(format Format, kind error, entry, reason string, args ...any) *Error {
	if len(args) > 0 {
		reason = fmt.Sprintf(reason, args...)
	}

	return &Error{Format: format, Err: kind, Entry: entry, Reason: reason}
}

// consume hands r to fn, then drains what fn left so the reader's checks run
// to the end, and reports the first failure.
func consume(e *Entry, r io.Reader, fn func(e *Entry, r io.Reader) error) error {
	if err := fn(e, r); err != nil {
		return err
	}

	_, err := io.Copy(io.Discard, r)

	return err
}
