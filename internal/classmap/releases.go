// Ports nothing: parse results kept per package release (deliberate
// deviation 3, speed).

package classmap

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"sync"

	"github.com/stubbedev/maestro/internal/util/fsstate"
)

// StampedFile is a file of a package tree whose content maestro knows
// without reading it: while the file holds that content, its size and
// modification time are these, to the nanosecond (the package store gives
// every file it imports a modification time derived from its content's
// hash; internal/store's Entry.ModTime).
type StampedFile struct {
	// Path is the file's path in the package tree, slash-separated.
	Path    string
	Size    int64
	ModTime int64 // Unix seconds; the nanoseconds are zero
	Sum     [32]byte
}

// Release is a package tree maestro installed from its package store: the
// files it knows by their stamps, and where the parse results of their
// contents are kept with it (store derived data).
type Release interface {
	// Name identifies the release among those of one cache.
	Name() string
	// Files are the release's files the scans may parse.
	Files() []StampedFile
	// ReadResults returns what WriteResults last kept, nil if nothing.
	ReadResults() ([]byte, error)
	// WriteResults keeps the release's parse results.
	WriteResults(data []byte) error
}

// stampKey is what a stamped file's stat shows: size and modification
// second.
type stampKey struct {
	size, mtime int64
}

type stampCandidate struct {
	rel  *releaseResults
	path string
	sum  [32]byte
}

// releaseResults is one release's parse results: those it kept, plus the
// ones this run found for its files (dirty until saved).
type releaseResults struct {
	src     Release
	results map[contentKey][]string
	dirty   bool
}

// releaseSet is what a ParseCache knows of releases: their stamped files
// and the parse results of their contents.
type releaseSet struct {
	mu       sync.RWMutex
	releases map[string]*releaseResults
	stamps   map[stampKey][]stampCandidate
}

// parseFormat is the version of the parse results kept across runs: in
// the cache file (UseFile) and with each release (AddReleases); its
// Header starts both.
var parseFormat = fsstate.Format{Name: "classmap-parse", Version: 1}

// AddReleases tells the cache about package trees installed from the
// package store: a scanned file whose stat shows the stamp of a release
// file with its path is taken to hold that file's content, so its parse
// result is looked up by the content's hash without reading the file
// (and parsed only when no run kept it yet). Their kept parse results are
// read now; Save keeps the new ones with each release. Releases already
// added (by name) are skipped.
func (c *ParseCache) AddReleases(releases []Release) {
	if c == nil {
		return
	}
	c.releasesPending.Wait()
	c.addReleases(releases)
}

// AddReleasesAsync is AddReleases of the releases find returns, run in the
// background: the lookups that could use them wait for it.
func (c *ParseCache) AddReleasesAsync(find func() []Release) {
	if c == nil {
		return
	}
	c.releasesPending.Wait()
	c.releasesPending.Go(func() { c.addReleases(find()) })
}

func (c *ParseCache) addReleases(releases []Release) {
	if len(releases) == 0 {
		return
	}
	set := &c.releases
	set.mu.Lock()
	fresh := make([]*releaseResults, 0, len(releases))
	for _, r := range releases {
		if _, ok := set.releases[r.Name()]; ok {
			continue
		}
		rr := &releaseResults{src: r}
		if set.releases == nil {
			set.releases = map[string]*releaseResults{}
		}
		set.releases[r.Name()] = rr
		fresh = append(fresh, rr)
	}
	set.mu.Unlock()

	header := parseFormat.Header()
	parallel(len(fresh), func(i int) {
		rr := fresh[i]
		data, err := rr.src.ReadResults()
		if err == nil {
			rr.results = decodeResults(data, header)
		}
		if rr.results == nil {
			rr.results = map[contentKey][]string{}
		}
	})

	set.mu.Lock()
	defer set.mu.Unlock()
	if set.stamps == nil {
		set.stamps = map[stampKey][]stampCandidate{}
	}
	for _, rr := range fresh {
		for _, f := range rr.src.Files() {
			k := stampKey{f.Size, f.ModTime}
			set.stamps[k] = append(set.stamps[k], stampCandidate{rel: rr, path: f.Path, sum: f.Sum})
		}
	}
	c.stampedAny.Store(len(set.stamps) > 0)
}

// stamped finds the release file a file at path with this stat is: the
// one candidate content among the release files of that size, stamp and
// path (two different contents leave it unknown).
func (s *releaseSet) stamped(path string, size, mtime int64) (*stampCandidate, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var found *stampCandidate
	for i, cand := range s.stamps[stampKey{size, mtime}] {
		if !hasPathSuffix(path, cand.path) {
			continue
		}
		if found != nil && found.sum != cand.sum {
			return nil, false
		}
		if found == nil {
			found = &s.stamps[stampKey{size, mtime}][i]
		}
	}

	return found, found != nil
}

// result returns the parse result of a release file's content: kept with
// its release, or with another release (then also kept with this one).
func (s *releaseSet) result(cand *stampCandidate, key contentKey) ([]string, bool) {
	s.mu.RLock()
	classes, ok := cand.rel.results[key]
	s.mu.RUnlock()
	if ok {
		return classes, true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, rr := range s.releases {
		if classes, ok := rr.results[key]; ok {
			cand.rel.results[key] = classes
			cand.rel.dirty = true

			return classes, true
		}
	}

	return nil, false
}

// put keeps the parse result of a release file's content with its release.
func (s *releaseSet) put(cand *stampCandidate, key contentKey, classes []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := cand.rel.results[key]; !ok {
		cand.rel.results[key] = classes
		cand.rel.dirty = true
	}
}

// save writes the results of the releases that got new ones.
func (s *releaseSet) save() {
	s.mu.Lock()
	var dirty []*releaseResults
	for _, rr := range s.releases {
		if rr.dirty {
			dirty = append(dirty, rr)
		}
	}
	s.mu.Unlock()
	header := parseFormat.Header()
	parallel(len(dirty), func(i int) {
		rr := dirty[i]
		s.mu.RLock()
		data := encodeResults(header, rr.results)
		s.mu.RUnlock()
		if rr.src.WriteResults(data) == nil {
			s.mu.Lock()
			rr.dirty = false
			s.mu.Unlock()
		}
	})
}

// hasPathSuffix reports whether path ends in the slash-separated relative
// path rel, at a separator.
func hasPathSuffix(path, rel string) bool {
	if len(path) <= len(rel) {
		return false
	}
	tail := path[len(path)-len(rel):]
	for i := range len(rel) {
		a, b := tail[i], rel[i]
		if a != b && (b != '/' || a != '\\') {
			return false
		}
	}
	sep := path[len(path)-len(rel)-1]

	return sep == '/' || sep == '\\'
}

// encodeResults writes parse results: the header, then per content its
// key (SHA-256, short_open_tag, PHP version) and classes, as the cache
// file holds them.
func encodeResults(header string, results map[contentKey][]string) []byte {
	var b bytes.Buffer
	w := bufio.NewWriter(&b)
	_, _ = w.WriteString(header)
	for key, classes := range results {
		writeResult(w, key, classes)
	}
	_ = w.Flush()

	return b.Bytes()
}

// decodeResults reads encodeResults' output; anything else (another
// format or binary, or a damaged file) is nil.
func decodeResults(data []byte, header string) map[contentKey][]string {
	if !bytes.HasPrefix(data, []byte(header)) {
		return nil
	}
	r := bytes.NewReader(data[len(header):])
	results := map[contentKey][]string{}
	for {
		key, classes, err := readResult(r)
		if errors.Is(err, io.EOF) {
			return results
		}
		if err != nil {
			return nil
		}
		results[key] = classes
	}
}

// writeResult writes one parse result.
func writeResult(w *bufio.Writer, key contentKey, classes []string) {
	var buf [binary.MaxVarintLen64]byte
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

// byteReader is what readResult reads from.
type byteReader interface {
	io.Reader
	io.ByteReader
}

// readResult reads one parse result; io.EOF means there are no more.
func readResult(r byteReader) (contentKey, []string, error) {
	var key contentKey
	if _, err := io.ReadFull(r, key.sum[:]); err != nil {
		if errors.Is(err, io.EOF) {
			return key, nil, io.EOF
		}

		return key, nil, io.ErrUnexpectedEOF
	}
	var parser [3]byte // short_open_tag, PHP version (big-endian)
	if _, err := io.ReadFull(r, parser[:]); err != nil {
		return key, nil, io.ErrUnexpectedEOF
	}
	key.parser = parserKey{parser[0] == 1, phpVersion(binary.BigEndian.Uint16(parser[1:]))}
	n, err := binary.ReadUvarint(r)
	if err != nil || n > 1<<20 {
		return key, nil, io.ErrUnexpectedEOF
	}
	if n == 0 {
		return key, nil, nil
	}
	classes := make([]string, 0, n)
	for range n {
		l, err := binary.ReadUvarint(r)
		if err != nil || l > 1<<16 {
			return key, nil, io.ErrUnexpectedEOF
		}
		b := make([]byte, l)
		if _, err := io.ReadFull(r, b); err != nil {
			return key, nil, io.ErrUnexpectedEOF
		}
		classes = append(classes, string(b))
	}

	return key, classes, nil
}
