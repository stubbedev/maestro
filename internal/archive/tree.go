// Ports ArchiveDownloader::install()'s choice of the package directory, from
// src/Composer/Downloader/ArchiveDownloader.php.

package archive

import (
	"io/fs"
	"slices"
	"strings"
)

const (
	// maxComponent is NAME_MAX: a longer name fails to be created.
	maxComponent = 255
	// maxPath bounds a relative path. Extractors fail somewhere below
	// PATH_MAX (4096) depending on where Composer's temporary directory is;
	// any path that could get there is refused instead.
	maxPath = 3072
)

// builder accumulates the tree an extractor leaves in Composer's temporary
// directory. Paths are relative to that directory, whose own node is root.
type builder struct {
	nodes  map[string]*Entry
	root   *Entry
	all    []*Entry
	total  int64
	limits Limits
	format Format
}

func newBuilder(format Format, limits Limits) *builder {
	// ArchiveDownloader makes the temporary directory with
	// ensureDirectoryExists(): mkdir 0777, recursive, under the umask.
	root := &Entry{Kind: Dir, Mode: 0o777, Umask: true}

	return &builder{
		format: format,
		limits: limits,
		nodes:  map[string]*Entry{"": root},
		root:   root,
	}
}

func (b *builder) get(path string) *Entry {
	return b.nodes[path]
}

// add records a new entry at e.Path, whose parent must already be a
// directory in the tree and which must not exist yet.
func (b *builder) add(e Entry) (*Entry, error) {
	if err := b.checkPath(e.Path); err != nil {
		return nil, err
	}

	if len(b.all) >= b.limits.MaxEntries {
		return nil, errorf(b.format, ErrLimit, "", "more than %d entries", b.limits.MaxEntries)
	}

	if e.Kind == File {
		if err := b.account(e.Path, e.Size); err != nil {
			return nil, err
		}
	}

	n := &e
	b.nodes[e.Path] = n
	b.all = append(b.all, n)

	return n, nil
}

// replace gives the file at n new content, as an extractor overwriting it
// would.
func (b *builder) replace(n *Entry, size int64, src int, mode fs.FileMode) error {
	if err := b.account(n.Path, size); err != nil {
		return err
	}

	n.Size, n.src, n.Mode = size, src, mode

	return nil
}

func (b *builder) account(path string, size int64) error {
	if size < 0 || size > b.limits.MaxFileSize {
		return errorf(b.format, ErrLimit, path, "file of %d bytes exceeds the %d byte limit", size, b.limits.MaxFileSize)
	}

	b.total += size
	if b.total > b.limits.MaxTotalSize {
		return errorf(b.format, ErrLimit, "", "expands to more than %d bytes", b.limits.MaxTotalSize)
	}

	return nil
}

func (b *builder) checkRatio(archiveSize int64) error {
	if b.total > ratioFloor && b.total/max(archiveSize, 1) > b.limits.MaxRatio {
		return errorf(b.format, ErrLimit, "", "expands %d bytes to %d (ratio above %d)", archiveSize, b.total, b.limits.MaxRatio)
	}

	return nil
}

// checkPath enforces what every path in the tree satisfies: relative,
// slash-separated, no empty, "." or ".." component, no NUL, and short enough
// for any extractor to create.
func (b *builder) checkPath(p string) error {
	if p == "" {
		return errorf(b.format, ErrIrreproducible, p, "empty name")
	}

	if len(p) > maxPath {
		return errorf(b.format, ErrIrreproducible, p, "name longer than %d bytes", maxPath)
	}

	if strings.IndexByte(p, 0) >= 0 {
		return errorf(b.format, ErrIrreproducible, p, "name contains a NUL byte")
	}

	for c := range strings.SplitSeq(p, "/") {
		switch {
		case c == "" || c == "." || c == "..":
			return errorf(b.format, ErrIrreproducible, p, "name has an empty, \".\" or \"..\" component")
		case len(c) > maxComponent:
			return errorf(b.format, ErrIrreproducible, p, "name component longer than %d bytes", maxComponent)
		}
	}

	return nil
}

// finish applies ArchiveDownloader's rule and returns the package tree:
// when the extraction holds exactly one entry besides .DS_Store and it is a
// directory, that directory is the package; otherwise the temporary
// directory is. Entries come out root first, each after its parent.
func (b *builder) finish() ([]Entry, error) {
	root, prefix := b.root, ""

	var only *Entry

	count := 0

	for _, n := range b.all {
		if strings.IndexByte(n.Path, '/') < 0 && n.Path != ".DS_Store" {
			only = n
			count++
		}
	}

	if count == 1 {
		switch only.Kind {
		case Dir:
			root, prefix = only, only.Path+"/"
		case Symlink:
			// is_dir() would follow the link, and rename() would then move
			// the link itself into place: refuse rather than guess.
			return nil, errorf(b.format, ErrIrreproducible, only.Path, "the archive's only top-level entry is a symlink")
		case File:
		}
	}

	entries := make([]Entry, 1, len(b.all)+1)
	entries[0] = *root
	entries[0].Path = ""

	for _, n := range b.all {
		if n == root || !strings.HasPrefix(n.Path, prefix) {
			continue
		}

		e := *n
		e.Path = n.Path[len(prefix):]
		entries = append(entries, e)
	}

	slices.SortFunc(entries[1:], func(x, y Entry) int { return ComparePaths(x.Path, y.Path) })

	return entries, nil
}

// ComparePaths orders slash-separated paths so that every directory comes
// right before its contents: bytewise, with '/' sorting before every other
// byte.
func ComparePaths(a, b string) int {
	n := min(len(a), len(b))
	for i := range n {
		ca, cb := a[i], b[i]
		if ca == cb {
			continue
		}

		if ca == '/' {
			return -1
		}

		if cb == '/' {
			return 1
		}

		if ca < cb {
			return -1
		}

		return 1
	}

	return len(a) - len(b)
}

// parent is the directory holding p ("" for a top-level name).
func parent(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[:i]
	}

	return ""
}
