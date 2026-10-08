// Ports nothing: the class map of a set of scans kept across runs
// (deliberate deviation 3, speed).

package classmap

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/stubbedev/maestro/internal/util/fsstate"
)

// RecordScan is one ScanPaths call of a recorded set of scans (with no
// excluded directories).
type RecordScan struct {
	Path      string
	Excluded  Matcher
	Type      AutoloadType
	Namespace string
}

// Record is the class map a set of scans built, kept in a file across
// runs with the identity (fsstate.ID) of every file and directory the scans
// depended on: each directory the Finder listed and its ancestors, and
// each file with a scanned extension. A later run doing the same scans
// takes the class map from it, with its ambiguous classes and PSR
// violations, instead of scanning, when all of them still have the
// identity recorded (as git trusts its index: an identity too close to
// the time the record was written is not trusted, see fsstate.Margin).
//
// A file the scan took for a store release's file by its stamp
// (AddReleases) is recorded without its change time, which changes
// whenever the store hard-links the inode into another project: its
// device, inode, size and stamp still have to match, the stamp being what
// the scan itself trusted for its content.
type Record struct {
	path    string
	key     [32]byte
	anchors []string
	// trust is how old a file must be for its identity to be recorded
	// and, recorded, trusted.
	trust fsstate.Margin
}

// recordMaxFiles bounds the records kept in a directory: past it, the
// least recently written are removed.
const recordMaxFiles = 128

// NewRecord returns the record of these scans, by a generator with parser
// p and the given extensions, kept in dir under a name derived from id
// (one record per id: the record of other scans with the same id replaces
// it). anchors are directories whose realpath the caller resolved itself
// (they are part of the record's key): the identities of the anchors and
// their ancestors are not recorded, as they change for reasons that do
// not matter, such as files written next to the project, unless the
// scans list them. ok is false when the scans cannot be recorded: an
// exclusion matcher whose pattern is unknown, a glob, a working directory
// that cannot be resolved, or a platform without file identities.
func NewRecord(dir, id string, anchors []string, p Parser, extensions []string, scans []RecordScan) (*Record, bool) {
	if dir == "" || !fsstate.Known() {
		return nil, false
	}
	cwd, err := getCwd()
	if err != nil {
		return nil, false
	}
	real, err := realCwd()
	if err != nil {
		return nil, false
	}
	h := fsstate.NewKeyHash()
	put := h.String
	pk := p.key()
	put(recordFormat.Header())
	put(id)
	put(cwd)
	put(real)
	put(fmt.Sprint(pk.shortOpenTag, pk.php))
	put(strings.Join(extensions, "\x00"))
	for _, a := range anchors {
		put(a)
	}
	for _, s := range scans {
		if strings.Contains(s.Path, "*") {
			return nil, false
		}
		pattern := ""
		if s.Excluded != nil {
			str, ok := s.Excluded.(fmt.Stringer)
			if !ok {
				return nil, false
			}
			pattern = "\x01" + str.String()
		}
		put(s.Path)
		put(pattern)
		put(s.Type.String())
		put(s.Namespace)
	}
	name := sha256.Sum256([]byte(id))
	r := &Record{path: filepath.Join(dir, hex.EncodeToString(name[:16])+".bin"), anchors: anchors}
	h.Sum(r.key[:0])

	return r, true
}

// recordFormat is the version of the records, and of the scans that
// build their class maps; its Header starts every record.
var recordFormat = fsstate.Format{Name: "classmap-record", Version: 3}

// SetMargin sets how old a file must be for its identity to be recorded
// and, recorded, trusted (zero: fsstate.DefaultMargin; tests trust
// identities however recent with a negative one).
func (rec *Record) SetMargin(m fsstate.Margin) { rec.trust = m }

// recording is what a generator's scans depended on.
type recording struct {
	roots, dirs, files []string
	broken             bool
	// ids are the identities of the files the scans parsed, as the scans
	// found them (path -> seenID): the record holds those, and needs no
	// stat of its own for them.
	ids sync.Map
}

// seenID is a file's identity as a scan found it, and a time before the
// scan looked: what the identity is trusted as of.
type seenID struct {
	at time.Time
	id fsstate.ID
}

// seen returns what notes the identity of the file at path for the record,
// nil when nothing is recorded. It is called before the scan looks at the
// file.
func (r *recording) seen(path string) func(fsstate.ID) {
	if r == nil {
		return nil
	}

	at := time.Now()

	return func(id fsstate.ID) { r.ids.Store(path, seenID{at: at, id: id}) }
}

// StartRecording makes the generator note what its scans depend on, for
// SaveRecord.
func (g *Generator) StartRecording() { g.rec = &recording{} }

// recordScan notes a ScanPaths call: its path, the files the Finder
// yielded and the directories it listed.
func (g *Generator) recordScan(path string, files []foundFile, listed []string) {
	r := g.rec
	if r == nil {
		return
	}
	r.roots = append(r.roots, path)
	r.dirs = append(r.dirs, listed...)
	for _, f := range files {
		if g.hasExtension(f.path) {
			r.files = append(r.files, f.path)
		}
	}
}

// SaveRecord keeps the class map the generator's scans built (since
// StartRecording, all of them successful) in the record. Failures only
// lose the record.
func (g *Generator) SaveRecord(rec *Record) {
	r := g.rec
	g.rec = nil
	if r == nil || r.broken || rec == nil {
		return
	}
	seen := make(map[string]bool, len(r.files)+len(r.dirs))
	var paths []string
	add := func(p string) {
		if !seen[p] {
			seen[p] = true
			paths = append(paths, p)
		}
	}
	// a root's ancestors up to an anchor (or its own ancestors), whose
	// renames or symlinks change the files' realpaths
	for _, root := range r.roots {
		add(root)
		for p := root; ; {
			parent := filepath.Dir(p)
			if parent == p || slices.ContainsFunc(rec.anchors, func(a string) bool {
				return a == parent || strings.HasPrefix(a, strings.TrimSuffix(parent, "/")+"/")
			}) {
				break
			}
			add(parent)
			p = parent
		}
	}
	for _, d := range r.dirs {
		add(d)
	}
	files := make(map[string]bool, len(r.files))
	for _, f := range r.files {
		add(f)
		files[f] = true
	}
	slices.Sort(paths)
	// a record holding an identity too recent to trust would never be
	// used (Load): it is not written (after an install, the next dump
	// writes one)
	keys := make([]fsstate.ID, len(paths))
	stamped := make([]bool, len(paths))
	now := time.Now()
	var failed atomic.Bool
	parallel(len(paths), func(i int) {
		// an identity the scan saw is trusted as of when the scan
		// looked, not now: a file written within the margin before that
		// could change again without changing its identity
		var k fsstate.ID
		at := now
		v, ok := r.ids.Load(paths[i])
		if ok && files[paths[i]] {
			seen, _ := v.(seenID)
			k, at = seen.id, seen.at
		} else {
			k, ok = fsstate.Stat(paths[i])
		}
		stamped[i] = ok && files[paths[i]] && g.cache.isStamped(paths[i], k)
		if !ok || !trusted(k, stamped[i], at, rec.trust) {
			failed.Store(true)
		}
		keys[i] = k
	})
	if failed.Load() {
		return
	}

	var b bytes.Buffer
	b.WriteString(recordFormat.Header())
	b.Write(rec.key[:])
	w := recordWriter{&b, [binary.MaxVarintLen64]byte{}}
	w.uint(uint64(len(paths)))
	prev := ""
	for i, p := range paths {
		shared := commonPrefix(prev, p)
		w.int(shared)
		w.string(p[shared:])
		prev = p
		k := keys[i]
		if stamped[i] {
			k.Ctime = stampedCtime
		}
		w.id(k)
	}
	index := make(map[string]int, len(paths))
	for i, p := range paths {
		index[p] = i
	}
	path := func(p string) {
		if i, ok := index[p]; ok {
			w.int(i + 1)

			return
		}
		w.uint(0)
		w.string(p)
	}
	m := &g.classMap
	w.uint(uint64(len(m.classes)))
	for i, class := range m.classes {
		w.string(class)
		path(m.paths[i])
	}
	w.uint(uint64(len(m.ambiguous)))
	for _, a := range m.ambiguous {
		w.string(a.Class)
		w.uint(uint64(len(a.Paths)))
		for _, p := range a.Paths {
			path(p)
		}
	}
	w.int(m.psrLive)
	for _, p := range m.psrViolations {
		if p.violations == nil {
			continue
		}
		w.string(p.path)
		w.uint(uint64(len(p.violations)))
		for _, v := range p.violations {
			w.string(v.Warning)
			w.string(v.ClassName)
		}
	}

	if fsstate.WriteAtomic(rec.path, b.Bytes()) != nil {
		return
	}
	pruneRecords(filepath.Dir(rec.path))
}

// stampedCtime is what a record holds as the change time of a stamped
// file (see Record), whose change time it does not record: no change time
// is it.
const stampedCtime = math.MinInt64

// isStamped reports whether the file at path with identity k is a release
// file by its stamp (lookupByIdentity), as the scan took it.
func (c *ParseCache) isStamped(path string, k fsstate.ID) bool {
	size, mtime, whole := releaseStamp(k)
	if c == nil || !whole {
		return false
	}
	c.releasesPending.Wait()
	if !c.stampedAny.Load() {
		return false
	}
	_, ok := c.releases.stamped(path, size, mtime)

	return ok
}

// trusted reports whether identity k, seen at t, is old enough to be
// trusted: its modification time and, unless the file is stamped (whose
// change time is not recorded), its change time.
func trusted(k fsstate.ID, stamped bool, t time.Time, margin fsstate.Margin) bool {
	if stamped {
		k.Ctime = k.Mtime
	}

	return k.Trusted(t, margin)
}

// pruneRecords removes the least recently written records past
// recordMaxFiles.
func pruneRecords(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) <= recordMaxFiles {
		return
	}
	type record struct {
		name string
		mod  time.Time
	}
	records := make([]record, 0, len(entries))
	for _, e := range entries {
		info, err := e.Info()
		switch {
		case err != nil || !info.Mode().IsRegular():
		case fsstate.IsTemp(e.Name()):
			// a record being written, or abandoned by a writer that died
			if time.Since(info.ModTime()) > fsstate.TempMaxAge {
				_ = os.Remove(filepath.Join(dir, e.Name()))
			}
		default:
			records = append(records, record{e.Name(), info.ModTime()})
		}
	}
	slices.SortFunc(records, func(a, b record) int { return b.mod.Compare(a.mod) })
	for _, r := range records[min(len(records), recordMaxFiles):] {
		_ = os.Remove(filepath.Join(dir, r.name))
	}
}

func commonPrefix(a, b string) int {
	n := min(len(a), len(b))
	for i := range n {
		if a[i] != b[i] {
			return i
		}
	}

	return n
}

type recordWriter struct {
	b   *bytes.Buffer
	buf [binary.MaxVarintLen64]byte
}

func (w *recordWriter) uint(v uint64) { w.b.Write(w.buf[:binary.PutUvarint(w.buf[:], v)]) }

func (w *recordWriter) id(id fsstate.ID) { w.b.Write(id.AppendBinary(w.buf[:0])) }

// int writes a count or an index.
func (w *recordWriter) int(n int) { w.uint(uint64(n)) } //nolint:gosec // never negative

func (w *recordWriter) string(s string) {
	w.uint(uint64(len(s)))
	w.b.WriteString(s)
}

// recordReader decodes a record's contents; a malformed record sets bad.
type recordReader struct {
	b   []byte
	off int
	bad bool
}

// ReadByte reads a byte, for fsstate.ReadBinary.
func (r *recordReader) ReadByte() (byte, error) {
	if r.bad || r.off >= len(r.b) {
		r.bad = true

		return 0, io.ErrUnexpectedEOF
	}
	c := r.b[r.off]
	r.off++

	return c, nil
}

func (r *recordReader) id() fsstate.ID {
	id, err := fsstate.ReadBinary(r)
	if err != nil {
		r.bad = true
	}

	return id
}

func (r *recordReader) uint() uint64 {
	if r.bad {
		return 0
	}
	v, n := binary.Uvarint(r.b[r.off:])
	if n <= 0 {
		r.bad = true

		return 0
	}
	r.off += n

	return v
}

// count is a length or an index, at most limit.
func (r *recordReader) count(limit int) int {
	v := r.uint()
	if v > uint64(limit) { //nolint:gosec // never negative
		r.bad = true

		return 0
	}

	return int(v) //nolint:gosec // at most limit
}

// bytes are the next n bytes, a length read first.
func (r *recordReader) bytes() []byte {
	n := r.count(len(r.b) - r.off)
	if r.bad {
		return nil
	}
	b := r.b[r.off : r.off+n]
	r.off += n

	return b
}

func (r *recordReader) string() string { return string(r.bytes()) }

// Load returns the recorded class map when every file and directory the
// recorded scans depended on still has the identity recorded, ok false
// otherwise (or when there is no record of these scans).
func (rec *Record) Load() (*ClassMap, bool) {
	if rec == nil {
		return nil, false
	}
	f, err := os.Open(rec.path)
	if err != nil {
		return nil, false
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()

		return nil, false
	}
	data := make([]byte, info.Size())
	_, err = io.ReadFull(f, data)
	_ = f.Close()
	if err != nil {
		return nil, false
	}
	header := recordFormat.Header()
	start := len(header) + len(rec.key)
	if len(data) < start || string(data[:len(header)]) != header || !bytes.Equal(data[len(header):start], rec.key[:]) {
		return nil, false
	}
	r := &recordReader{b: data[start:]}

	// the paths, built one after the other in one buffer
	n := r.count(len(r.b))
	ends := make([]int, n)
	keys := make([]fsstate.ID, n)
	arena := make([]byte, 0, len(data)*2)
	prevStart := 0
	for i := range n {
		shared := r.count(len(arena) - prevStart)
		suffix := r.bytes()
		if r.bad {
			return nil, false
		}
		start := len(arena)
		arena = append(arena, arena[prevStart:prevStart+shared]...)
		arena = append(arena, suffix...)
		ends[i], prevStart = len(arena), start
		keys[i] = r.id()
	}
	if r.bad {
		return nil, false
	}
	all := string(arena)
	paths := make([]string, n)
	for i, end := range ends {
		start := 0
		if i > 0 {
			start = ends[i-1]
		}
		paths[i] = all[start:end]
	}

	// the class map is decoded while the identities are checked
	var m *ClassMap
	decoded := make(chan struct{})
	go func() {
		defer close(decoded)
		m = decodeRecordedClassMap(r, paths)
	}()

	// every identity as recorded, and old enough to trust
	written := info.ModTime()
	var changed atomic.Bool
	parallel(n, func(i int) {
		if changed.Load() {
			return
		}
		k, ok := fsstate.Stat(paths[i])
		stamped := keys[i].Ctime == stampedCtime
		if stamped {
			k.Ctime = stampedCtime
		}
		if !ok || k != keys[i] || !trusted(k, stamped, written, rec.trust) {
			changed.Store(true)
		}
	})
	<-decoded
	if changed.Load() || m == nil {
		return nil, false
	}

	return m, true
}

// decodeRecordedClassMap decodes the class map part of a record, nil when
// malformed.
func decodeRecordedClassMap(r *recordReader, paths []string) *ClassMap {
	path := func() string {
		i := r.count(len(paths))
		if i == 0 {
			return r.string()
		}

		return paths[i-1]
	}
	m := &ClassMap{}
	classes := r.count(len(r.b))
	m.classes = make([]string, classes)
	m.paths = make([]string, classes)
	m.index = make(map[string]int, classes)
	for i := range classes {
		m.classes[i] = r.string()
		m.paths[i] = path()
		m.index[m.classes[i]] = i
	}
	for range r.count(len(r.b)) {
		class := r.string()
		for range r.count(len(r.b)) {
			m.AddAmbiguousClass(class, path())
		}
	}
	for range r.count(len(r.b)) {
		p := r.string()
		for range r.count(len(r.b)) {
			warning := r.string()
			m.AddPsrViolation(warning, r.string(), p)
		}
	}
	if r.bad || r.off != len(r.b) || len(m.index) != classes {
		return nil
	}

	return m
}
