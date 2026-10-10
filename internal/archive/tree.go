// Ports ArchiveDownloader::install()'s choice of the package directory, from
// src/Composer/Downloader/ArchiveDownloader.php.

package archive

import (
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
// directory. Paths are relative to that directory, whose own node, all[0],
// is the root.
type builder struct {
	nodes  map[string]int
	all    []Entry
	total  int64
	limits Limits
	format Format
}

func newBuilder(format Format, limits Limits) *builder {
	// ArchiveDownloader makes the temporary directory with
	// ensureDirectoryExists(): mkdir 0777, recursive, under the umask.
	return &builder{
		format: format,
		limits: limits,
		nodes:  map[string]int{"": 0},
		all:    []Entry{{Kind: Dir, Mode: 0o777, Umask: true}},
	}
}

// get returns the node at path, or nil. The pointer is valid until the
// next add.
func (b *builder) get(path string) *Entry {
	if i, ok := b.nodes[path]; ok {
		return &b.all[i]
	}

	return nil
}

// root is the extraction directory's node, valid until the next add.
func (b *builder) root() *Entry {
	return &b.all[0]
}

// add records a new entry at e.Path, whose parent must already be a
// directory in the tree and which must not exist yet.
func (b *builder) add(e Entry) error {
	if err := b.checkPath(e.Path); err != nil {
		return err
	}

	if len(b.all) > b.limits.MaxEntries {
		return errorf(b.format, ErrLimit, "", "more than %d entries", b.limits.MaxEntries)
	}

	if e.Kind == File {
		if err := b.account(e.Path, e.Size); err != nil {
			return err
		}
	}

	b.nodes[e.Path] = len(b.all)
	b.all = append(b.all, e)

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
	switch {
	case len(p) > maxPath:
		return errorf(b.format, ErrIrreproducible, p, "name longer than %d bytes", maxPath)
	case !ValidPath(p):
		return errorf(b.format, ErrIrreproducible, p, "name is empty, contains a NUL byte or has an empty, \".\" or \"..\" component")
	}

	for c := range strings.SplitSeq(p, "/") {
		if len(c) > maxComponent {
			return errorf(b.format, ErrIrreproducible, p, "name component longer than %d bytes", maxComponent)
		}
	}

	return nil
}

// ValidPath reports whether p is a path an entry may have: non-empty,
// relative, slash-separated, without NUL bytes and without empty, "." or
// ".." components.
func ValidPath(p string) bool {
	if p == "" || strings.IndexByte(p, 0) >= 0 {
		return false
	}

	for c := range strings.SplitSeq(p, "/") {
		if c == "" || c == "." || c == ".." {
			return false
		}
	}

	return true
}

// finish applies ArchiveDownloader's rule and returns the package tree:
// when the extraction holds exactly one entry besides .DS_Store and it is a
// directory, that directory is the package; otherwise the temporary
// directory is. Entries come out root first, each after its parent.
func (b *builder) finish() ([]Entry, error) {
	root, prefix := 0, ""
	only, count := 0, 0

	for i := 1; i < len(b.all); i++ {
		if p := b.all[i].Path; strings.IndexByte(p, '/') < 0 && p != ".DS_Store" {
			only = i
			count++
		}
	}

	if count == 1 {
		switch n := &b.all[only]; n.Kind {
		case Dir:
			root, prefix = only, n.Path+"/"
		case Symlink:
			// is_dir() would follow the link, and rename() would then move
			// the link itself into place: refuse rather than guess.
			return nil, errorf(b.format, ErrIrreproducible, n.Path, "the archive's only top-level entry is a symlink")
		case File:
		}
	}

	entries := make([]Entry, 1, len(b.all))
	entries[0] = b.all[root]
	entries[0].Path = ""

	for i := 1; i < len(b.all); i++ {
		if i == root || !strings.HasPrefix(b.all[i].Path, prefix) {
			continue
		}

		e := b.all[i]
		e.Path = e.Path[len(prefix):]
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

// Parent is the directory holding the entry path p ("" for a top-level
// name).
func Parent(p string) string {
	if before, _, ok := strings.CutLast(p, "/"); ok {
		return before
	}

	return ""
}
