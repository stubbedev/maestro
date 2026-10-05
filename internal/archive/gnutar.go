// Reproduces XzDownloader's `tar -xJf <file> -C <dir>` (GNU tar run by a
// user other than root, so with --no-same-owner and --no-same-permissions)
// and GzipDownloader's `gzip -cd -- <file> > <dir>/<name>`, from
// src/Composer/Downloader/XzDownloader.php and GzipDownloader.php.

package archive

import (
	"archive/tar"
	"errors"
	"io"
	"io/fs"
	"os"
	"strings"
)

// gnuTar plans `tar -xJf`.
type gnuTar struct {
	b *builder
	// explicit marks the directories an entry of the archive created or
	// gave a mode, as opposed to ones tar made as missing parents.
	explicit map[string]bool
	files    []tarFile
}

func planGNUTar(f *os.File, size int64, b *builder) (contentReader, error) {
	s := &stream{f: f, size: size, comp: compressXz, format: Xz, limit: streamLimit(b.limits)}

	c, err := s.open()
	if err != nil {
		return nil, err
	}

	g := &gnuTar{b: b, explicit: map[string]bool{}}
	tr := tar.NewReader(c)

	for ordinal := 0; ; ordinal++ {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return nil, g.readError(err)
		}

		if err := g.entry(hdr, ordinal); err != nil {
			return nil, err
		}
	}

	// tar drains its decompressor, which fails on corrupt data anywhere in
	// the file, even past the end of the archive.
	if _, err := io.Copy(io.Discard, c); err != nil {
		return nil, err
	}

	return &gnuContent{s: s, files: g.files}, nil
}

func (g *gnuTar) fail(kind error, entry, reason string, args ...any) *Error {
	return errorf(Xz, kind, entry, reason, args...)
}

func (g *gnuTar) readError(err error) error {
	if ae, ok := errors.AsType[*Error](err); ok {
		return ae
	}

	return g.fail(ErrCorrupt, "", "invalid tar data: %v", err)
}

// entry places one member as GNU tar extracts it, refusing everything it
// would warn about, fail on, or that maestro does not reproduce (hard
// links, special files, sparse files, overwriting).
func (g *gnuTar) entry(hdr *tar.Header, ordinal int) error {
	name := hdr.Name

	switch hdr.Typeflag {
	case tar.TypeXGlobalHeader:
		// Global records apply to every later member; only those that
		// cannot change what is extracted (by a user other than root,
		// ignoring times) are accepted.
		for k := range hdr.PAXRecords {
			switch k {
			case "comment", "mtime", "atime", "ctime", "uid", "gid", "uname", "gname":
			default:
				return g.fail(ErrIrreproducible, name, "global pax record %q", k)
			}
		}

		return nil
	case tar.TypeReg, tar.TypeCont, tar.TypeDir, tar.TypeSymlink:
	default:
		return g.fail(ErrIrreproducible, name, "unsupported tar entry type %q", hdr.Typeflag)
	}

	if hdr.Typeflag != tar.TypeDir && strings.HasSuffix(name, "/") {
		return g.fail(ErrIrreproducible, name, "a non-directory entry named like a directory")
	}

	if strings.IndexByte(name, 0) >= 0 || strings.IndexByte(hdr.Linkname, 0) >= 0 {
		return g.fail(ErrIrreproducible, name, "name contains a NUL byte")
	}

	// tar strips leading slashes ("Removing leading `/' from member
	// names") and refuses names with a ".." component.
	parts := make([]string, 0, strings.Count(name, "/")+1)

	for c := range strings.SplitSeq(strings.TrimLeft(name, "/"), "/") {
		switch c {
		case "", ".":
		case "..":
			return g.fail(ErrIrreproducible, name, "Member name contains '..'")
		default:
			parts = append(parts, c)
		}
	}

	path := strings.Join(parts, "/")
	perm := fs.FileMode(hdr.Mode) & fs.ModePerm //nolint:gosec // masked to the permission bits.

	if path == "" {
		if hdr.Typeflag != tar.TypeDir {
			return g.fail(ErrIrreproducible, name, "a non-directory entry for the extraction directory")
		}

		return g.setDir(g.b.root(), "", perm, name)
	}

	if err := g.parents(path, name); err != nil {
		return err
	}

	if n := g.b.get(path); n != nil {
		if n.Kind == Dir && hdr.Typeflag == tar.TypeDir {
			return g.setDir(n, path, perm, name)
		}

		return g.fail(ErrIrreproducible, name, "the archive extracts more than one entry to %s", path)
	}

	var err error

	switch hdr.Typeflag {
	case tar.TypeDir:
		err = g.b.add(Entry{Path: path, Kind: Dir, Mode: perm, Umask: true})
		g.explicit[path] = true
	case tar.TypeSymlink:
		if hdr.Linkname == "" {
			return g.fail(ErrIrreproducible, name, "symlink with an empty target")
		}

		err = g.b.add(Entry{Path: path, Kind: Symlink, Link: hdr.Linkname})
	default:
		err = g.b.add(Entry{Path: path, Kind: File, Mode: perm, Umask: true, Size: hdr.Size, src: len(g.files)})
		g.files = append(g.files, tarFile{off: int64(ordinal), size: hdr.Size})
	}

	return err
}

// setDir gives a directory the mode of an entry naming it, which tar
// applies once everything is extracted. A second entry for the same
// directory is refused.
func (g *gnuTar) setDir(n *Entry, path string, perm fs.FileMode, name string) error {
	if g.explicit[path] {
		return g.fail(ErrIrreproducible, name, "the archive extracts more than one entry to %s", path)
	}

	g.explicit[path] = true
	n.Mode, n.Umask = perm, true

	return nil
}

// parents creates the missing directories above path as tar does: 0777
// under the umask. They must not pass through a file or a symlink.
func (g *gnuTar) parents(path, name string) error {
	dir := Parent(path)
	if dir == "" {
		return nil
	}

	at := ""

	for c := range strings.SplitSeq(dir, "/") {
		if at != "" {
			at += "/"
		}

		at += c

		switch n := g.b.get(at); {
		case n == nil:
			if err := g.b.add(Entry{Path: at, Kind: Dir, Mode: 0o777, Umask: true}); err != nil {
				return err
			}
		case n.Kind != Dir:
			return g.fail(ErrIrreproducible, name, "%s is not a directory", at)
		}
	}

	return nil
}

// gnuContent streams a planned GNU tar's regular files, located by their
// ordinal among the archive's members.
type gnuContent struct {
	s     *stream
	files []tarFile
}

func (g *gnuContent) readFiles(a *Archive, fn FileFunc) error {
	byOrdinal := make(map[int64]int, len(g.files))

	for i := range a.entries {
		if e := &a.entries[i]; e.Kind == File {
			byOrdinal[g.files[e.src].off] = i
		}
	}

	c, err := g.s.open()
	if err != nil {
		return err
	}

	tr := tar.NewReader(c)

	for ordinal := int64(0); len(byOrdinal) > 0; ordinal++ {
		if _, err := tr.Next(); err != nil {
			return (&gnuTar{}).readError(err)
		}

		i, ok := byOrdinal[ordinal]
		if !ok {
			continue
		}

		delete(byOrdinal, ordinal)

		e := &a.entries[i]
		r := &exactReader{r: tr, left: e.Size, entry: e.Path, format: Xz}

		if err := consume(i, r, fn); err != nil {
			return err
		}
	}

	return nil
}

// planGzip plans GzipDownloader: the decompressed file, named after the
// dist URL's file name without its extension, alone in the directory.
func planGzip(f *os.File, size int64, opts *Options, b *builder) (contentReader, error) {
	url := ""
	if opts != nil {
		url = opts.URL
	}

	name, ok := gzipTarget(url)
	if !ok {
		return nil, errorf(Gzip, ErrIrreproducible, "", "cannot derive a file name from the dist URL %q", url)
	}

	comp, err := detectCompression(f)
	if err != nil {
		return nil, err
	}

	if comp != compressGzip {
		// gzip -cd fails, and Composer falls back to gzread(), which copies
		// anything that is not gzip data verbatim.
		return nil, errorf(Gzip, ErrIrreproducible, "", "not in gzip format")
	}

	s := &stream{f: f, size: size, comp: compressGzip, format: Gzip, limit: b.limits.MaxFileSize}

	c, err := s.open()
	if err != nil {
		return nil, err
	}

	// The size is known only once the data is decompressed.
	n, err := io.Copy(io.Discard, c)
	if err != nil {
		return nil, err
	}

	if err := b.add(Entry{Path: name, Kind: File, Mode: 0o666, Umask: true, Size: n}); err != nil {
		return nil, err
	}

	return &tarContent{s: s, files: []tarFile{{off: 0, size: n}}}, nil
}

// gzipTarget is pathinfo(parse_url(strtr($url, '\\', '/'), PHP_URL_PATH),
// PATHINFO_FILENAME) for the URLs whose parsing is unambiguous: an
// absolute URL with an authority, or a bare absolute path. ok is false for
// anything else, and for names that cannot be a file.
func gzipTarget(url string) (string, bool) {
	url = strings.ReplaceAll(url, `\`, "/")

	for i := range len(url) {
		if c := url[i]; c <= ' ' || c >= 0x7f {
			return "", false
		}
	}

	path := url

	if scheme, rest, ok := strings.Cut(url, "://"); ok {
		if !isScheme(scheme) {
			return "", false
		}

		i := strings.IndexAny(rest, "/?#")
		if i <= 0 || rest[i] != '/' {
			return "", false
		}

		path = rest[i:]
	} else if !strings.HasPrefix(url, "/") || strings.HasPrefix(url, "//") {
		return "", false
	}

	if i := strings.IndexAny(path, "?#"); i >= 0 {
		path = path[:i]
	}

	// basename() drops trailing slashes, then everything up to the last one.
	path = strings.TrimRight(path, "/")
	path = path[strings.LastIndexByte(path, '/')+1:]

	if i := strings.LastIndexByte(path, '.'); i >= 0 {
		path = path[:i]
	}

	if path == "" || path == "." || path == ".." {
		return "", false
	}

	return path, true
}

func isScheme(s string) bool {
	if s == "" || !isAlpha(s[0]) {
		return false
	}

	for i := range len(s) {
		if c := s[i]; !isAlpha(c) && (c < '0' || c > '9') && c != '+' && c != '-' && c != '.' {
			return false
		}
	}

	return true
}

func isAlpha(c byte) bool {
	return (c|0x20) >= 'a' && (c|0x20) <= 'z'
}
