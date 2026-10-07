// Ports nothing: parse results worked out while a release enters the
// package store (deliberate deviation 3, speed).

package classmap

import "sync"

// ReleaseParse parses the files of one release as the package store
// inserts it, from the contents it holds in memory and by the hash it
// computed of them, so that the class map scans of the dump that follows
// find their results kept with the release (Release.ReadResults) instead
// of reading, hashing and parsing the installed files. The results are
// those a scan's parse of the same contents gives, by the same parser;
// which files are parsed only changes how many a scan finds parsed. It is
// safe for concurrent use.
type ReleaseParse struct {
	p      Parser
	accept func(path string) bool

	mu      sync.Mutex
	results map[contentKey][]string
}

// NewReleaseParse returns a ReleaseParse by parser p of the release files
// accept takes (by their slash-separated path in the release).
func NewReleaseParse(p Parser, accept func(path string) bool) *ReleaseParse {
	return &ReleaseParse{p: p, accept: accept, results: map[contentKey][]string{}}
}

// File parses data, the content of the release file at path, whose
// SHA-256 is sum, when accept takes path. A content that does not parse
// is left to the scans, whose errors name the installed file.
func (r *ReleaseParse) File(path string, data []byte, sum *[32]byte) {
	if !r.accept(path) {
		return
	}
	b := getBuffers()
	defer bufferPool.Put(b)
	classes, err := r.p.classesIn(b, b.load(data), path)
	if err != nil {
		return
	}
	r.mu.Lock()
	r.results[contentKey{sum: *sum, parser: r.p.key()}] = classes
	r.mu.Unlock()
}

// Merge returns the release's results kept (as Release.ReadResults returns
// them; nil or anything unreadable for none) with those File found added,
// for Release.WriteResults; ok is false when File found nothing new.
func (r *ReleaseParse) Merge(kept []byte) (data []byte, ok bool) {
	header := parseFormat.Header()
	results := decodeResults(kept, header)
	if results == nil {
		results = map[contentKey][]string{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	added := false
	for key, classes := range r.results {
		if _, ok := results[key]; !ok {
			results[key] = classes
			added = true
		}
	}
	if !added {
		return nil, false
	}

	return encodeResults(header, results), true
}

// load puts data in b.src, followed by the zero padding the scanner
// expects, as readFile does with a file's contents, and returns its
// length.
func (b *parseBuffers) load(data []byte) int {
	n := len(data)
	if cap(b.src) < n+stripPadding {
		b.src = make([]byte, 0, n+stripPadding+512)
	}
	b.src = b.src[:n+stripPadding]
	copy(b.src, data)
	clear(b.src[n:])

	return n
}
