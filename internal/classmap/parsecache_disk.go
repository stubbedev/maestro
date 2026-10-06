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
// project) instead of parsing them.
type diskCache struct {
	path, header string
	loaded       chan struct{}

	mu      sync.Mutex
	entries map[contentKey][]string
	used    map[contentKey]struct{}
	added   int
}

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
		d.entries = d.load()
	}()
}

// diskHeader is the first line of the file: format and binary.
func diskHeader() string {
	h := "maestro classmap cache 2"
	if exe, err := os.Executable(); err == nil {
		if info, err := os.Stat(exe); err == nil {
			h += " " + strconv.FormatInt(info.Size(), 10) + " " + strconv.FormatInt(info.ModTime().UnixNano(), 10)
		}
	}

	return h + "\n"
}

func (d *diskCache) load() map[contentKey][]string {
	entries := map[contentKey][]string{}
	f, err := os.Open(d.path)
	if err != nil {
		return entries
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 1<<16)
	header, err := r.ReadString('\n')
	if err != nil || header != d.header {
		return entries
	}
	for {
		var key contentKey
		if _, err := io.ReadFull(r, key.sum[:]); err != nil {
			if !errors.Is(err, io.EOF) {
				return map[contentKey][]string{}
			}

			return entries
		}
		var parser [3]byte // short_open_tag, PHP version (big-endian)
		if _, err := io.ReadFull(r, parser[:]); err != nil {
			return map[contentKey][]string{}
		}
		key.parser = parserKey{parser[0] == 1, phpVersion(binary.BigEndian.Uint16(parser[1:]))}
		n, err := binary.ReadUvarint(r)
		if err != nil || n > 1<<20 {
			return map[contentKey][]string{}
		}
		classes := make([]string, 0, n)
		for range n {
			l, err := binary.ReadUvarint(r)
			if err != nil || l > 1<<16 {
				return map[contentKey][]string{}
			}
			b := make([]byte, l)
			if _, err := io.ReadFull(r, b); err != nil {
				return map[contentKey][]string{}
			}
			classes = append(classes, string(b))
		}
		if n == 0 {
			classes = nil
		}
		entries[key] = classes
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
	if d.added == 0 {
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
	d.added = 0
}

// contentKeyOf is the contentKey of contents parsed by p.
func contentKeyOf(p Parser, contents []byte) contentKey {
	return contentKey{sum: sha256.Sum256(contents), parser: p.key()}
}
