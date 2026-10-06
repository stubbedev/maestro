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
	"path/filepath"
	"strconv"
	"sync"
	"time"
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
// those runs read, by file identity (fileKey), so that a file unchanged
// since is not even read.
type diskCache struct {
	path, header string
	loaded       chan struct{}

	mu      sync.Mutex
	entries map[contentKey][]string
	used    map[contentKey]struct{}
	added   int

	// stats maps a file's identity to the SHA-256 of its contents, as an
	// earlier run read them (git's index, in effect). written is when the
	// file was saved: an identity whose times are not safely before it
	// may belong to a file changed again within the same clock tick
	// (git's "racily clean" entries), so it is not trusted.
	stats        map[fileKey][32]byte
	written      time.Time
	statsChanged bool
}

// statTrustMargin is how much older than the cache file a file's
// modification and change times must be for its recorded identity to be
// trusted: more than the coarsest timestamp granularity in use (FAT's
// two seconds). A variable for tests.
var statTrustMargin = 3 * time.Second

// UseFile makes the cache keep its results in the file at path across
// runs, starting to load it in the background. Results are only shared
// between runs of the same maestro binary (its size and modification time
// are part of the file's header), so a parser change never reads results
// of another. Save writes the file.
func (c *ParseCache) UseFile(path string) {
	if c == nil || c.disk != nil {
		return
	}
	d := &diskCache{path: path, header: diskHeader(), loaded: make(chan struct{}), used: map[contentKey]struct{}{}}
	c.disk = d
	go func() {
		defer close(d.loaded)
		d.entries, d.stats, d.written = d.load()
	}()
}

// diskHeader is the first line of the file: format and binary.
func diskHeader() string {
	h := "maestro classmap cache 3"
	if exe, err := os.Executable(); err == nil {
		if info, err := os.Stat(exe); err == nil {
			h += " " + strconv.FormatInt(info.Size(), 10) + " " + strconv.FormatInt(info.ModTime().UnixNano(), 10)
		}
	}

	return h + "\n"
}

// load reads the file: the header, the identity index (a count, then per
// entry the fileKey's fields as varints and the content's SHA-256), then
// the parse results up to the end. Anything malformed loads as empty.
func (d *diskCache) load() (map[contentKey][]string, map[fileKey][32]byte, time.Time) {
	entries, stats := map[contentKey][]string{}, map[fileKey][32]byte{}
	empty := func() (map[contentKey][]string, map[fileKey][32]byte, time.Time) {
		return map[contentKey][]string{}, map[fileKey][32]byte{}, time.Time{}
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
		var (
			key    fileKey
			fields [7]uint64
		)
		for i := range fields {
			if fields[i], err = binary.ReadUvarint(r); err != nil {
				return empty()
			}
		}
		key.dev, key.ino = fields[0], fields[1]
		key.size = int64(fields[2])      //nolint:gosec // written from an int64
		key.mtimeSec = int64(fields[3])  //nolint:gosec // written from an int64
		key.mtimeNsec = int64(fields[4]) //nolint:gosec // written from an int64
		key.ctimeSec = int64(fields[5])  //nolint:gosec // written from an int64
		key.ctimeNsec = int64(fields[6]) //nolint:gosec // written from an int64
		var sum [32]byte
		if _, err := io.ReadFull(r, sum[:]); err != nil {
			return empty()
		}
		stats[key] = sum
	}
	for {
		var key contentKey
		if _, err := io.ReadFull(r, key.sum[:]); err != nil {
			if !errors.Is(err, io.EOF) {
				return empty()
			}

			return entries, stats, info.ModTime()
		}
		var parser [3]byte // short_open_tag, PHP version (big-endian)
		if _, err := io.ReadFull(r, parser[:]); err != nil {
			return empty()
		}
		key.parser = parserKey{parser[0] == 1, phpVersion(binary.BigEndian.Uint16(parser[1:]))}
		n, err := binary.ReadUvarint(r)
		if err != nil || n > 1<<20 {
			return empty()
		}
		classes := make([]string, 0, n)
		for range n {
			l, err := binary.ReadUvarint(r)
			if err != nil || l > 1<<16 {
				return empty()
			}
			b := make([]byte, l)
			if _, err := io.ReadFull(r, b); err != nil {
				return empty()
			}
			classes = append(classes, string(b))
		}
		if n == 0 {
			classes = nil
		}
		entries[key] = classes
	}
}

// statSum returns the content hash recorded for a file identity, if it
// can be trusted (see diskCache.stats).
func (d *diskCache) statSum(key fileKey) ([32]byte, bool) {
	<-d.loaded
	d.mu.Lock()
	defer d.mu.Unlock()
	sum, ok := d.stats[key]
	if !ok || d.written.IsZero() {
		return sum, false
	}
	limit := d.written.Add(-statTrustMargin)
	if !time.Unix(key.mtimeSec, key.mtimeNsec).Before(limit) || !time.Unix(key.ctimeSec, key.ctimeNsec).Before(limit) {
		return sum, false
	}

	return sum, true
}

// putStat records the content hash of a file this run read.
func (d *diskCache) putStat(key fileKey, sum [32]byte) {
	<-d.loaded
	d.mu.Lock()
	defer d.mu.Unlock()
	if old, ok := d.stats[key]; !ok || old != sum {
		d.stats[key] = sum
		d.statsChanged = true
	}
}

func (d *diskCache) get(key contentKey) ([]string, bool) {
	<-d.loaded
	d.mu.Lock()
	defer d.mu.Unlock()
	classes, ok := d.entries[key]
	if ok {
		d.used[key] = struct{}{}
	}

	return classes, ok
}

func (d *diskCache) put(key contentKey, classes []string) {
	<-d.loaded
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.entries[key]; !ok {
		d.entries[key] = classes
		d.added++
	}
	d.used[key] = struct{}{}
}

// Save writes the results of this run (and, while the file stays small
// enough, the earlier ones) to the cache file, if the run found new ones.
// The file is replaced atomically; failures only lose the cache.
func (c *ParseCache) Save() {
	if c == nil || c.disk == nil {
		return
	}
	d := c.disk
	<-d.loaded
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.added == 0 && !d.statsChanged {
		return
	}
	keep := func(key contentKey) bool { return true }
	if len(d.entries) > diskCacheMaxEntries {
		keep = func(key contentKey) bool { _, ok := d.used[key]; return ok }
	}
	if err := os.MkdirAll(filepath.Dir(d.path), 0o755); err != nil {
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(d.path), ".classmap-*")
	if err != nil {
		return
	}
	w := bufio.NewWriterSize(tmp, 1<<16)
	_, _ = w.WriteString(d.header)
	var buf [binary.MaxVarintLen64]byte
	// the identity index, dropped wholesale when it outgrows the bound
	// (this run's files are read again next time, and recorded then)
	stats := d.stats
	if len(stats) > diskCacheMaxEntries {
		stats = nil
	}
	_, _ = w.Write(buf[:binary.PutUvarint(buf[:], uint64(len(stats)))])
	for key, sum := range stats {
		for _, v := range [...]uint64{
			key.dev, key.ino, uint64(key.size), //nolint:gosec // read back as an int64
			uint64(key.mtimeSec), uint64(key.mtimeNsec), //nolint:gosec // read back as an int64
			uint64(key.ctimeSec), uint64(key.ctimeNsec), //nolint:gosec // read back as an int64
		} {
			_, _ = w.Write(buf[:binary.PutUvarint(buf[:], v)])
		}
		_, _ = w.Write(sum[:])
	}
	for key, classes := range d.entries {
		if !keep(key) {
			continue
		}
		_, _ = w.Write(key.sum[:])
		parser := [3]byte{}
		if key.parser.shortOpenTag {
			parser[0] = 1
		}
		binary.BigEndian.PutUint16(parser[1:], uint16(key.parser.php))
		_, _ = w.Write(parser[:])
		_, _ = w.Write(buf[:binary.PutUvarint(buf[:], uint64(len(classes)))])
		for _, class := range classes {
			_, _ = w.Write(buf[:binary.PutUvarint(buf[:], uint64(len(class)))])
			_, _ = w.WriteString(class)
		}
	}
	if w.Flush() != nil || tmp.Close() != nil || os.Rename(tmp.Name(), d.path) != nil {
		_ = os.Remove(tmp.Name())

		return
	}
	d.added, d.statsChanged = 0, false
}

// contentKeyOf is the contentKey of contents parsed by p.
func contentKeyOf(p Parser, contents []byte) contentKey {
	return contentKey{sum: sha256.Sum256(contents), parser: p.key()}
}
