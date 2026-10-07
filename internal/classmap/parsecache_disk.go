// Ports nothing: parse results kept across runs (deliberate deviation 3,
// speed).

package classmap

import (
	"bufio"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/stubbedev/maestro/internal/util/fsstate"
)

// contentKey names a parse result by the SHA-256 of the file's contents
// and the parser settings that change it.
type contentKey struct {
	sum    [32]byte
	parser parserKey
}

// diskCacheMaxEntries bounds the file: past it, only the results used by
// the run that writes it are kept.
const diskCacheMaxEntries = 1 << 19

// diskCache is the persistent part of a ParseCache: the classes found in
// file contents seen by earlier runs of this maestro binary, by content
// hash, so that a run reads and hashes files it has seen before (in any
// project) instead of parsing them; and the content hash of the files
// those runs read, by file identity (fsstate.ID), so that a file unchanged
// since is not even read.
type diskCache struct {
	path, header string
	// once loads the file, on first use; touched is set from then on, so
	// that saving a cache never used reads nothing.
	once    sync.Once
	touched atomic.Bool

	mu      sync.Mutex
	entries map[contentKey][]string
	used    map[contentKey]struct{}
	added   int

	// stats maps a file's identity to the SHA-256 of its contents, as an
	// earlier run read them (git's index, in effect). written is when the
	// file was saved: an identity whose times are not safely before it
	// may belong to a file changed again within the same clock tick
	// (git's "racily clean" entries), so it is not trusted.
	stats        map[fsstate.ID][32]byte
	written      time.Time
	statsChanged bool
	// racy is set when an identity was too recent to trust: saving the
	// file again (with a later modification time) lets the next run trust
	// it, as git rewrites an index holding racily clean entries.
	racy bool
}

// UseFile makes the cache keep its results in the file at path across
// runs. The file is read once per process, when a scan (or Warm) first
// needs it, and shared by every ParseCache of the process using it: a
// command that never scans never reads it. Results are only shared
// between runs of the same maestro binary (its size and modification time
// are part of the file's header), so a parser change never reads results
// of another. Save writes the file.
func (c *ParseCache) UseFile(path string) {
	if c == nil || c.disk != nil {
		return
	}
	c.disk = sharedDiskCache(path)
}

// diskCaches holds the diskCache of each file path used in this process.
var diskCaches = struct {
	sync.Mutex
	byPath map[string]*diskCache
}{byPath: map[string]*diskCache{}}

// sharedDiskCache is the process's diskCache of the file at path.
func sharedDiskCache(path string) *diskCache {
	diskCaches.Lock()
	defer diskCaches.Unlock()
	d, ok := diskCaches.byPath[path]
	if !ok {
		d = &diskCache{path: path, header: diskHeader(), used: map[contentKey]struct{}{}}
		diskCaches.byPath[path] = d
	}

	return d
}

// ready loads the file unless it was loaded already, and waits for it.
func (d *diskCache) ready() {
	d.touched.Store(true)
	d.once.Do(func() { d.entries, d.stats, d.written = d.load() })
}

// diskHeader is the first line of the file: format and binary.
func diskHeader() string {
	return "maestro classmap cache 4" + fsstate.BinaryID() + "\n"
}

// load reads the file: the header, the identity index (a count, then per
// entry the ID (fsstate.ID.AppendBinary) and the content's SHA-256), then
// the parse results up to the end. Anything malformed loads as empty.
func (d *diskCache) load() (map[contentKey][]string, map[fsstate.ID][32]byte, time.Time) {
	entries, stats := map[contentKey][]string{}, map[fsstate.ID][32]byte{}
	empty := func() (map[contentKey][]string, map[fsstate.ID][32]byte, time.Time) {
		return map[contentKey][]string{}, map[fsstate.ID][32]byte{}, time.Time{}
	}
	f, err := os.Open(d.path)
	if err != nil {
		return empty()
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return empty()
	}
	r := bufio.NewReaderSize(f, 1<<16)
	header, err := r.ReadString('\n')
	if err != nil || header != d.header {
		return empty()
	}
	count, err := binary.ReadUvarint(r)
	if err != nil || count > diskCacheMaxEntries {
		return empty()
	}
	for range count {
		key, err := fsstate.ReadBinary(r)
		if err != nil {
			return empty()
		}
		var sum [32]byte
		if _, err := io.ReadFull(r, sum[:]); err != nil {
			return empty()
		}
		stats[key] = sum
	}
	for {
		key, classes, err := readResult(r)
		if errors.Is(err, io.EOF) {
			return entries, stats, info.ModTime()
		}
		if err != nil {
			return empty()
		}
		entries[key] = classes
	}
}

// statSum returns the content hash recorded for a file identity, if it
// can be trusted (see diskCache.stats).
func (d *diskCache) statSum(key fsstate.ID, trust fsstate.Margin) ([32]byte, bool) {
	d.ready()
	d.mu.Lock()
	defer d.mu.Unlock()
	sum, ok := d.stats[key]
	if !ok || d.written.IsZero() {
		return sum, false
	}
	if !key.Trusted(d.written, trust) {
		d.racy = true

		return sum, false
	}

	return sum, true
}

// putStat records the content hash of a file this run read.
func (d *diskCache) putStat(key fsstate.ID, sum [32]byte) {
	d.ready()
	d.mu.Lock()
	defer d.mu.Unlock()
	if old, ok := d.stats[key]; !ok || old != sum {
		d.stats[key] = sum
		d.statsChanged = true
	}
}

func (d *diskCache) get(key contentKey) ([]string, bool) {
	d.ready()
	d.mu.Lock()
	defer d.mu.Unlock()
	classes, ok := d.entries[key]
	if ok {
		d.used[key] = struct{}{}
	}

	return classes, ok
}

func (d *diskCache) put(key contentKey, classes []string) {
	d.ready()
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.entries[key]; !ok {
		d.entries[key] = classes
		d.added++
	}
	d.used[key] = struct{}{}
}

// Save keeps the results of this run: those of release files with their
// releases (AddReleases), the others (and, while the file stays small
// enough, the earlier ones) in the cache file, if the run found new ones.
// The file is replaced atomically; failures only lose the cache.
func (c *ParseCache) Save() {
	if c == nil {
		return
	}
	c.releasesPending.Wait()
	c.releases.save()
	if c.disk == nil {
		return
	}
	d := c.disk
	if !d.touched.Load() {
		return
	}
	d.ready()
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.added == 0 && !d.statsChanged && !d.racy {
		return
	}
	keep := func(key contentKey) bool { return true }
	if len(d.entries) > diskCacheMaxEntries {
		keep = func(key contentKey) bool { _, ok := d.used[key]; return ok }
	}
	err := fsstate.WriteAtomicFunc(d.path, func(f io.Writer) error {
		w := bufio.NewWriterSize(f, 1<<16)
		_, _ = w.WriteString(d.header)
		var buf [binary.MaxVarintLen64]byte
		// the identity index, dropped wholesale when it outgrows the
		// bound (this run's files are read again next time, and
		// recorded then)
		stats := d.stats
		if len(stats) > diskCacheMaxEntries {
			stats = nil
		}
		_, _ = w.Write(buf[:binary.PutUvarint(buf[:], uint64(len(stats)))])
		var id []byte
		for key, sum := range stats {
			id = key.AppendBinary(id[:0])
			_, _ = w.Write(id)
			_, _ = w.Write(sum[:])
		}
		for key, classes := range d.entries {
			if keep(key) {
				writeResult(w, key, classes)
			}
		}

		return w.Flush()
	})
	if err != nil {
		return
	}
	d.added, d.statsChanged, d.racy = 0, false, false
	// what is in memory is now what loading the file would give: its
	// identities are trusted as of its new modification time
	if info, err := os.Stat(d.path); err == nil {
		d.written = info.ModTime()
	}
}

// contentKeyOf is the contentKey of contents parsed by p.
func contentKeyOf(p Parser, contents []byte) contentKey {
	return contentKey{sum: sha256.Sum256(contents), parser: p.key()}
}
