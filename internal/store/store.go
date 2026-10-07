package store

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/stubbedev/maestro/internal/archive"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util/fsstate"
)

// Method is how files get from the store into a package directory.
type Method int32

const (
	// Auto clones where the filesystem can, else hardlinks, else copies,
	// decided once per destination device (pnpm's auto).
	Auto Method = iota
	// Clone shares blocks copy-on-write (FICLONE, clonefile).
	Clone
	// Hardlink links the store's object.
	Hardlink
	// Copy writes the bytes out.
	Copy
)

func (m Method) String() string {
	switch m {
	case Clone:
		return "clone"
	case Hardlink:
		return "hardlink"
	case Copy:
		return "copy"
	case Auto:
	}

	return "auto"
}

// MethodEnv names the environment variable that selects the import method:
// ParseMethod(os.Getenv(MethodEnv)).
const MethodEnv = "MAESTRO_PACKAGE_IMPORT_METHOD"

// ParseMethod reads an import method name: auto (or empty), clone,
// hardlink or copy.
func ParseMethod(s string) (Method, error) {
	switch php.Strtolower(strings.TrimSpace(s)) {
	case "", "auto":
		return Auto, nil
	case "clone":
		return Clone, nil
	case "hardlink":
		return Hardlink, nil
	case "copy":
		return Copy, nil
	}

	return Auto, fmt.Errorf("invalid %s %q (expected auto, clone, hardlink or copy)", MethodEnv, s)
}

// ImportOptions adjust one import (Materialize, Install). The zero value
// imports with the store's method.
type ImportOptions struct {
	// Unshared imports every file as an inode of its own, by clone or
	// copy, never by hardlink, whatever the method: for packages that
	// rewrite their own files in place (Composer plugins), whose edits
	// would otherwise reach the store and every project linked to it.
	Unshared bool
}

// Options configure a Store. The zero value is ready to use.
type Options struct {
	// Archive sets the extraction locale and limits; its URL is ignored
	// (each Dist brings its own).
	Archive archive.Options
	// Method is the import method.
	Method Method
	// Workers bounds the goroutines one insert uses (default GOMAXPROCS),
	// and those all imports running at once use together (default
	// GOMAXPROCS, at most maxImportSlots).
	Workers int
}

// Store is the package store at one root. It is safe for concurrent use,
// and several processes may share a root.
type Store struct {
	devices sync.Map // device number -> *device
	// releases holds the releases this Store looked up or inserted, by
	// index name: an index never changes but by a new insert of the same
	// dist (the same tree) or by Prune and Verify, which forget them.
	releases sync.Map // [32]byte -> *Release
	archive  archive.Options
	root     string
	files    string
	index    string
	derived  string
	tmp      string
	// slots holds one token per goroutine creating package files, which
	// the imports running at once share.
	slots   chan struct{}
	workers int
	method  Method
	umask   fs.FileMode
	// shards records which files/, index/ and derived/ subdirectories
	// exist.
	shards [3][256]atomic.Bool
}

// maxImportSlots bounds the goroutines imports use by default: creating
// files is the filesystem's work, and beyond a few threads they only
// contend for its locks (btrfs gets slower, tmpfs no faster).
const maxImportSlots = 8

// ErrNotFound means a dist is not in the store.
var ErrNotFound = errors.New("not in the package store")

// MissingError means objects a release needs have gone from the store (or
// were found corrupt and dropped), so its archive must be inserted again.
type MissingError struct {
	Path string
}

func (e *MissingError) Error() string {
	return "package store object missing or corrupt: " + e.Path
}

// Open opens (creating it if needed) the store at root, which is
// cache.Store() for maestro's own store.
func Open(root string, opts *Options) (*Store, error) {
	if opts == nil {
		opts = &Options{}
	}

	if opts.Method < Auto || opts.Method > Copy {
		return nil, fmt.Errorf("store: invalid import method %d", opts.Method)
	}

	s := &Store{
		root:    root,
		files:   filepath.Join(root, "files"),
		index:   filepath.Join(root, "index"),
		derived: filepath.Join(root, "derived"),
		tmp:     filepath.Join(root, "tmp"),
		archive: opts.Archive,
		method:  opts.Method,
		workers: opts.Workers,
		umask:   processUmask(),
	}

	if s.workers <= 0 {
		s.workers = runtime.GOMAXPROCS(0)
	}

	slots := s.workers
	if opts.Workers <= 0 {
		slots = min(slots, maxImportSlots)
	}

	s.slots = make(chan struct{}, slots)

	s.archive.URL = ""

	for _, dir := range []string{s.files, s.index, s.tmp} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}

	return s, nil
}

// Root is the store's directory.
func (s *Store) Root() string {
	return s.root
}

// Dist identifies a dist archive: what Composer's lock file records of it.
type Dist struct {
	// Name is the package name.
	Name string
	// Type is the dist type ("zip", "tar", "xz", "gzip").
	Type string
	// URL is the dist URL.
	URL string
	// Reference is the dist reference.
	Reference string
	// Shasum is the dist's SHA-1, if the lock records one.
	Shasum string
}

func (s *Store) options(d *Dist) (archive.Format, *archive.Options, error) {
	format, ok := archive.ParseFormat(d.Type)
	if !ok {
		return 0, nil, fmt.Errorf("store: unsupported dist type %q", d.Type)
	}

	opts := s.archive
	opts.URL = d.URL

	return format, &opts, nil
}

// indexPath names the index of a dist: the SHA-256 of its identity and the
// extraction rules.
func (s *Store) indexPath(d *Dist, format archive.Format, opts *archive.Options) (string, [32]byte) {
	h := sha256.New()

	for _, part := range []string{"maestro-store-release-1", d.Name, d.Type, d.URL, d.Reference, d.Shasum, archive.Rules(format, opts)} {
		h.Write([]byte(strconv.Itoa(len(part))))
		h.Write([]byte{':'})
		h.Write([]byte(part))
	}

	var id [32]byte

	h.Sum(id[:0])

	return s.shardPath(s.index, &id, ""), id
}

// shardPath is dir/<2 hex>/<62 hex><suffix>.
func (s *Store) shardPath(dir string, sum *[32]byte, suffix string) string {
	var name [64]byte

	hex.Encode(name[:], sum[:])

	return dir + string(os.PathSeparator) + string(name[:2]) + string(os.PathSeparator) + string(name[2:]) + suffix
}

// ensureShard creates the shard directory holding path, once per process.
func (s *Store) ensureShard(kind int, sum *[32]byte, path string) error {
	if s.shards[kind][sum[0]].Load() {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	s.shards[kind][sum[0]].Store(true)

	return nil
}

// intoShard runs place, which renames a finished file to path, after
// making sure path's shard directory exists, and once more when the
// directory turns out to have been removed meanwhile (the store emptied
// by hand while this process runs).
func (s *Store) intoShard(kind int, sum *[32]byte, path string, place func() error) error {
	for retried := false; ; retried = true {
		err := s.ensureShard(kind, sum, path)
		if err == nil {
			err = place()
		}

		if retried || !errors.Is(err, fs.ErrNotExist) {
			return err
		}

		s.shards[kind][sum[0]].Store(false)
	}
}

// Lookup returns the release of a dist already in the store, or
// ErrNotFound. It marks the release as used, for Prune (once per Store:
// later lookups of the release are answered from memory).
func (s *Store) Lookup(d Dist) (*Release, error) {
	format, opts, err := s.options(&d)
	if err != nil {
		return nil, err
	}

	path, id := s.indexPath(&d, format, opts)

	return s.lookupIndex(path, id)
}

// lookupIndex loads the index named id at path (Lookup, LookupNamed).
func (s *Store) lookupIndex(path string, id [32]byte) (*Release, error) {
	if r, ok := s.releases.Load(id); ok {
		return r.(*Release), nil //nolint:errcheck // the map only holds *Release.
	}

	f, err := openShared(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotFound
	}

	if err != nil {
		return nil, err
	}

	defer func() { _ = f.Close() }()

	st, err := fstat(f)
	if err != nil {
		return nil, err
	}

	data := make([]byte, st.size)
	if _, err := f.ReadAt(data, 0); err != nil {
		return nil, err
	}

	entries, err := decodeIndex(data)
	if err != nil {
		// A corrupt index is dropped so the dist is inserted again.
		_ = os.Remove(path)

		return nil, ErrNotFound
	}

	if now := time.Now(); now.Unix()-st.mtime > int64(usedGranularity/time.Second) {
		_ = os.Chtimes(path, now, now)
	}

	r := &Release{entries: entries, id: id}
	s.releases.Store(id, r)

	return r, nil
}

// usedGranularity is how stale a release's last-used mark may get before
// Lookup refreshes it.
const usedGranularity = time.Hour

// Ensure returns the release of a dist, inserting it from the archive at
// path when it is not in the store yet.
func (s *Store) Ensure(d Dist, path string) (*Release, error) {
	r, err := s.Lookup(d)
	if !errors.Is(err, ErrNotFound) {
		return r, err
	}

	return s.Insert(d, path)
}

// Install materializes dist d at dst: from the store when it holds the
// dist, else (or when objects have gone missing) from the archive at path,
// which may be empty when the caller has none (ErrNotFound or a
// *MissingError then tells it to fetch the archive).
func (s *Store) Install(d Dist, path, dst string, opts ImportOptions) error {
	r, err := s.Lookup(d)
	if errors.Is(err, ErrNotFound) && path != "" {
		r, err = s.Insert(d, path)
	}

	if err != nil {
		return err
	}

	err = s.Materialize(r, dst, opts)

	var missing *MissingError
	if errors.As(err, &missing) && path != "" {
		if r, err = s.Insert(d, path); err == nil {
			err = s.Materialize(r, dst, opts)
		}
	}

	return err
}

// lock takes the store lock, shared or exclusive, and returns its release.
func (s *Store) lock(exclusive bool) (func(), error) {
	f, err := fsstate.OpenFile(filepath.Join(s.root, "lock"), os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}

	if err := lockFile(f, exclusive); err != nil {
		_ = f.Close()
		return nil, err
	}

	return func() { _ = f.Close() }, nil
}

// tmpSeq numbers temporary files within the process.
var tmpSeq atomic.Uint64

// tmpName is a fresh name in dir, unique among processes by pid.
func tmpName(dir, prefix string) string {
	return dir + string(os.PathSeparator) + prefix + fsstate.Pid() + "-" + strconv.FormatUint(tmpSeq.Add(1), 10)
}

// writeAtomic writes data to path through a temporary file in the store,
// replacing any previous file.
func (s *Store) writeAtomic(path string, data []byte) error {
	tmp := tmpName(s.tmp, "i")

	if err := fsstate.WriteFile(tmp, data, 0o644); err != nil {
		_ = os.Remove(tmp)
		return err
	}

	if err := replaceFile(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}

	return nil
}

// fileStat is what the store needs of a stat(2) result.
type fileStat struct {
	size    int64
	mtime   int64
	mtimeNs int64
	ctime   int64
	nlink   uint64
	dev     uint64
	mode    fs.FileMode // permission and set-id/sticky bits
	regular bool
	dir     bool
}
