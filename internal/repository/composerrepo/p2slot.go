// Ports nothing: the form metadata files are kept in between runs, which
// decodes a package's versions one by one and only when they are read,
// with an index of what the speculation and the loads read of them
// (deliberate deviation 3, speed).

package composerrepo

import (
	"encoding/binary"
	"errors"
	"hash/crc32"
	"sync"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/loader"
)

// p2Slot is a metadata file read back from its slot, with the JSON it
// was decoded from. Its top is the file with the version list of each
// slotted package replaced by null; those lists are decoded from their
// entries' spans of the JSON when read.
type p2Slot struct {
	json    string
	topData []byte
	top     *php.Array
	names   map[string]*p2SlotName
}

// p2SlotName is a slotted package's version list: where its entries are
// in the JSON and, for a minified list that expandEach expands, the
// expanded version at every keyframeEvery-th entry (a keyframe) and the
// index of the versions. A keyframe tells, for each key of the expanded
// version in order, the entry its value comes from (how many entries
// before the keyframe's) and the key's place in that entry.
type p2SlotName struct {
	entries   []php.JSONSpan
	keyframes [][]byte
	index     *p2IndexData

	indexOnce sync.Once
	idx       *p2Index

	// expansion holds the entries the expansions decoded, each once
	expansionOnce sync.Once
	expansion     []expansionEntry
}

// expansionEntry is an entry of a slotted list as the expansions read it,
// decoded once: they share its values, as the versions expandEach
// expands share them.
type expansionEntry struct {
	once   sync.Once
	entry  *php.Array
	keys   []php.Key
	values []any
}

// keyframeEvery is how many entries apart the expanded versions a slot
// keeps are: expanding a version applies at most that many entries to
// its keyframe.
const keyframeEvery = 16

// p2Index is what versionsOf and prebuild read of a minified version
// list, kept with its slot: the versions scanned (scanVersions), and the
// skeleton config of each version that loads as a skeleton
// (loader.SkeletonChecker).
type p2Index struct {
	scanned []scannedVersion
	exact   bool
	// links are the link arrays of the versions, by ref
	links []*php.Array
	// skeletons holds the versions' skeleton records (appendSkeleton),
	// each at its offset (-1: none), as bytes and as a string to cut
	// strings from
	skeletons    []byte
	skeletonsStr string
	offsets      []int
}

// p2IndexData is an index in its slot form: the link arrays, the scanned
// versions and the skeleton records, each after its length (0 for none).
type p2IndexData struct {
	links, scanned, skeletons []byte
}

// link is the link array of ref; nil for none.
func (idx *p2Index) link(ref uint64) *php.Array {
	if ref < uint64(len(idx.links)) {
		return idx.links[ref]
	}

	return nil
}

// The tags of the values of a skeleton record.
const (
	skNull   byte = iota
	skFalse       // false
	skTrue        // true
	skString      // its length, its bytes
	skArray       // an array, by its ref
	skValue       // any other value: its length, its binary form
)

// appendSkeleton appends the skeleton record of a version, that of its
// SkeletonConfig: the number of its keys, then each key (its index in
// loader.SkeletonKeys) and its value as loader.SkeletonFields yields it.
// Arrays (the links, the extra) are kept by ref: the versions of
// a list share them, as the expansion has them share.
func appendSkeleton(dst []byte, version *php.Array, arrayRef func(*php.Array) uint64) ([]byte, bool) {
	// the count, at most len(loader.SkeletonKeys), fits in its byte
	count := len(dst)
	dst = append(dst, 0)
	for key, v := range loader.SkeletonFields(version) {
		dst[count]++
		dst = append(dst, byte(key)) //nolint:gosec // an index in loader.SkeletonKeys
		switch v := v.(type) {
		case nil:
			dst = append(dst, skNull)
		case bool:
			if v {
				dst = append(dst, skTrue)
			} else {
				dst = append(dst, skFalse)
			}
		case string:
			dst = appendString(append(dst, skString), v)
		case *php.Array:
			dst = binary.AppendUvarint(append(dst, skArray), arrayRef(v))
		default:
			part, ok := php.AppendBinary(nil, v)
			if !ok {
				return dst, false
			}
			dst = appendBytes(append(dst, skValue), part)
		}
	}

	return dst, true
}

// skeleton is the SkeletonConfig of the version at index i; nil when the
// version does not load as a skeleton.
func (idx *p2Index) skeleton(i int) *php.Array {
	if idx.offsets[i] < 0 {
		return nil
	}
	r := p2Reader{b: idx.skeletons, s: idx.skeletonsStr, pos: idx.offsets[i]}
	n := r.uvarint()
	config := php.NewArrayCap(int(n)) //nolint:gosec // bounded by the record
	for range n {
		key, _ := r.byte()
		tag, ok := r.byte()
		if !ok || int(key) >= len(loader.SkeletonKeys) {
			return nil
		}
		var v any
		switch tag {
		case skNull:
		case skFalse:
			v = false
		case skTrue:
			v = true
		case skString:
			v = r.str()
		case skArray:
			a := idx.link(r.uvarint())
			if a == nil {
				return nil
			}
			v = a
		case skValue:
			var err error
			if v, err = php.DecodeBinary([]byte(r.str())); err != nil {
				return nil
			}
		default:
			return nil
		}
		loader.SetSkeletonField(config, int(key), v)
	}
	if r.bad {
		return nil
	}

	return config
}

// versions is name's version list, decoded now.
func (s *p2Slot) versions(name string) any {
	n, ok := s.names[name]
	if !ok {
		return packageVersions(s.top, name)
	}
	list := php.NewArrayCap(len(n.entries))
	for i := range n.entries {
		list.Append(s.entry(n, i))
	}

	return list
}

// entry decodes the entry at index i of a slotted list from its span of
// the JSON, which decodeP2 checked.
func (s *p2Slot) entry(n *p2SlotName, i int) any {
	span := n.entries[i]
	v, err := php.JSONDecode(s.json[span.Start:span.End], true)
	if err != nil {
		panic(err)
	}

	return v
}

// mustDecode decodes a part of a slot, which decodeP2 checked.
func mustDecode(data []byte) any {
	v, err := php.DecodeBinary(data)
	if err != nil {
		panic(err)
	}

	return v
}

// hasVersions is isset($data['packages'][$name]).
func (s *p2Slot) hasVersions(name string) bool {
	if _, ok := s.names[name]; ok {
		return true
	}

	return packageVersions(s.top, name) != nil
}

// array is the whole file, decoded now.
func (s *p2Slot) array() *php.Array {
	file, _ := mustDecode(s.topData).(*php.Array)
	packages, _ := file.At("packages").(*php.Array)
	for name := range s.names {
		packages.Set(name, s.versions(name))
	}

	return file
}

// slim is slimFile of the file.
func (s *p2Slot) slim() *php.Array {
	return slimFileOf(s.top, func(name php.Key) bool {
		_, ok := s.names[name.String()]

		return ok && name.IsString()
	})
}

// index is the index of name's versions; nil when there is none.
func (s *p2Slot) index(name string) *p2Index {
	n, ok := s.names[name]
	if !ok || n.index == nil {
		return nil
	}
	n.indexOnce.Do(func() { n.idx = n.index.decode(len(n.entries)) })

	return n.idx
}

// expanded is the version at index i of name's list as expandEach expands
// it, an array of its own (its values may be shared with other expanded
// versions, as expandEach's copies share them); ok is false when the slot
// keeps no expanded versions of the list.
func (s *p2Slot) expanded(name string, i int) (*php.Array, bool) {
	n, ok := s.names[name]
	if !ok || n.keyframes == nil || i < 0 || i >= len(n.entries) {
		return nil, false
	}
	from := i - i%keyframeEvery
	working := s.keyframe(n, from)
	for j := from + 1; j <= i; j++ {
		working, _ = expandNext(working, s.expansionEntry(n, j).entry)
	}

	return working, true
}

// expansionEntry is the entry at index j of a slotted list, decoded once.
func (s *p2Slot) expansionEntry(n *p2SlotName, j int) *expansionEntry {
	n.expansionOnce.Do(func() { n.expansion = make([]expansionEntry, len(n.entries)) })
	e := &n.expansion[j]
	e.once.Do(func() {
		e.entry, _ = s.entry(n, j).(*php.Array)
		e.keys, e.values = e.entry.Keys(), e.entry.Values()
	})

	return e
}

// keyframe is the expanded version at index from, a keyframe's.
func (s *p2Slot) keyframe(n *p2SlotName, from int) *php.Array {
	r := p2Reader{b: n.keyframes[from/keyframeEvery]}
	// each key takes two bytes or more
	count, _ := r.count()
	working := php.NewArrayCap(count)
	for range count {
		back, place := r.uvarint(), r.uvarint()
		if back > uint64(from) { //nolint:gosec // from is an index
			panic(errP2Slot)
		}
		e := s.expansionEntry(n, from-int(back)) //nolint:gosec // bounded by from
		if r.bad || place >= uint64(len(e.keys)) {
			// not a keyframe appendIndex wrote for this JSON
			panic(errP2Slot)
		}
		working.SetKey(e.keys[place], e.values[place])
	}

	return working
}

// keyOrigin is where the value of a key of an expanded version comes
// from: the index of the entry, and the key's place in it.
type keyOrigin struct{ entry, place int }

// loadedVersion returns the version at index i of name's list as
// createPackages loads it: expanded, with notifyURL (notificationURL) as
// its notification-url when it has none.
func (s *p2Slot) loadedVersion(name string, i int, notifyURL any) func() *php.Array {
	return func() *php.Array {
		v, _ := s.expanded(name, i)
		if n, _ := v.Get("notification-url"); n == nil {
			v.Set("notification-url", notifyURL)
		}

		return v
	}
}

// The p2 codec's form of a file: its checksum, then its top, then for each
// slotted package, its name, its entries' spans of the JSON (each the
// distance from the end of the one before, or from the start of the
// JSON, then its length), its keyframes and its index: a flag, then the
// links, the scanned versions and the skeleton configs. Each part is a
// uvarint count or length followed by its bytes.

// p2Codec keeps metadata files as p2Slots.
var p2Codec = struct {
	parser *pkg.VersionParser
	loader *loader.ArrayLoader
}{pkg.NewVersionParser(), loader.NewArrayLoader(nil, true)}

var errP2Slot = errors.New("composerrepo: malformed p2 slot")

// appendP2 appends the slot form of v, a *p2File decoded at once by
// decodeFile, after its checksum: the parts are decoded only when they
// are read, so that a damaged slot cannot be told from a good one then.
func appendP2(dst []byte, v any) ([]byte, bool) {
	start := len(dst)
	dst, ok := appendP2Parts(append(dst, make([]byte, crcSize)...), v)
	if !ok {
		return dst, false
	}
	binary.LittleEndian.PutUint32(dst[start:], crc32.Checksum(dst[start+crcSize:], crcTable))

	return dst, true
}

// crcSize is the size of a slot's checksum (CRC-32C).
const crcSize = 4

var crcTable = crc32.MakeTable(crc32.Castagnoli)

// appendP2Parts appends the parts of the slot form of v.
func appendP2Parts(dst []byte, v any) ([]byte, bool) {
	f, ok := v.(*p2File)
	if !ok || f.data == nil {
		return dst, false
	}

	top := f.data
	type slotted struct {
		name string
		list *php.Array
	}
	var names []slotted
	if packages, ok := f.data.At("packages").(*php.Array); ok {
		var placeholders *php.Array
		for k, list := range packages.All() {
			// a list rebuilt from its entries is the list itself
			l, ok := list.(*php.Array)
			if !ok || !k.IsString() || !l.IsAppended() || len(f.spans[l]) != l.Len() {
				continue
			}
			if placeholders == nil {
				placeholders = packages.ShallowClone()
			}
			placeholders.SetKey(k, nil)
			names = append(names, slotted{k.String(), l})
		}
		if placeholders != nil {
			top = f.data.ShallowClone()
			top.Set("packages", placeholders)
		}
	}

	if dst, ok = appendPart(dst, top); !ok {
		return dst, false
	}
	dst = binary.AppendUvarint(dst, uint64(len(names)))
	minified := f.data.At("minified") == "composer/2.0"
	for _, n := range names {
		dst = appendString(dst, n.name)
		items := n.list.Values()
		dst = binary.AppendUvarint(dst, uint64(len(items)))
		end := 0
		for _, span := range f.spans[n.list] {
			dst = binary.AppendUvarint(dst, uint64(span.Start-end))      //nolint:gosec // spans follow each other
			dst = binary.AppendUvarint(dst, uint64(span.End-span.Start)) //nolint:gosec // a span ends after it starts
			end = span.End
		}
		if dst, ok = appendIndex(dst, items, minified, f.drafts[n.name]); !ok {
			return dst, false
		}
	}

	return dst, true
}

// appendPart appends v's binary form, after its length.
func appendPart(dst []byte, v any) ([]byte, bool) {
	part, ok := php.AppendBinary(nil, v)
	if !ok {
		return dst, false
	}

	return appendBytes(dst, part), true
}

func appendBytes(dst, b []byte) []byte {
	return append(binary.AppendUvarint(dst, uint64(len(b))), b...)
}

// appendString is appendBytes for a string.
func appendString(dst []byte, s string) []byte {
	return append(binary.AppendUvarint(dst, uint64(len(s))), s...)
}

// appendIndex appends the keyframes and the index a slot keeps of a
// version list: none unless the list is minified and expandEach expands
// it. It takes what draft (nil: none) holds instead of finding it out.
func appendIndex(dst []byte, items []any, minified bool, draft *indexDraft) ([]byte, bool) {
	if !minified {
		return append(dst, 0, 0), true
	}
	if _, ok := expandable(items); !ok {
		return append(dst, 0, 0), true
	}
	if b := draft.builtFor(items); b != nil {
		return b.appendTo(dst)
	}

	b := newIndexBuilder(items, draft.scannedFor(items))
	_, _ = expandEach(items, func(v *php.Array, _ func() *php.Array) error {
		if !b.add(v, fitUnknown) {
			return errStopExpanding
		}

		return nil
	})

	return b.appendTo(dst)
}

// indexBuilder builds the keyframes and the index of a minified version
// list that expandEach expands, from its versions added one by one as
// expandEach expands them.
type indexBuilder struct {
	items []any
	// scanned are the versions scanned already; nil: none
	scanned []scannedVersion
	added   int
	ok      bool

	keyframes                   [][]byte
	scannedPart, skeletons, rec []byte
	// links are the arrays the skeleton records hold, by ref
	links *php.Array
	refs  map[*php.Array]uint64
	// origins are, for each key of the expanded version, the entry its
	// value comes from and the key's place in it; expanded is the
	// length of the expanded version before the one added
	origins  map[string]keyOrigin
	expanded int
	checker  *loader.SkeletonChecker
}

// The fits of a version an indexBuilder adds: whether it loads as a
// skeleton, when a load tells.
const (
	fitUnknown byte = iota
	fitYes
	fitNo
)

// newIndexBuilder is an indexBuilder for the versions of items, scanned
// already (scanVersions) unless scanned is nil.
func newIndexBuilder(items []any, scanned []scannedVersion) *indexBuilder {
	return &indexBuilder{
		items: items, scanned: scanned, ok: true,
		links: php.NewArray(), refs: map[*php.Array]uint64{}, origins: map[string]keyOrigin{},
	}
}

// arrayRef is the ref of a, an array a skeleton record holds.
func (b *indexBuilder) arrayRef(a *php.Array) uint64 {
	ref, ok := b.refs[a]
	if !ok {
		ref = uint64(len(b.refs))
		b.refs[a] = ref
		b.links.Append(a)
	}

	return ref
}

// add adds the next version, v as expandEach expands it, which fits
// tells loads as a skeleton (fitUnknown: it is checked); false when the
// index cannot hold it.
func (b *indexBuilder) add(v *php.Array, fits byte) bool {
	i := b.added
	b.added++

	// as expandNext changes the expanded version
	entry, _ := b.items[i].(*php.Array)
	restarted := b.expanded == 0
	if restarted {
		clear(b.origins)
	}
	place := 0
	for k, value := range entry.All() {
		if value == "__unset" && !restarted {
			delete(b.origins, k.String())
		} else {
			b.origins[k.String()] = keyOrigin{i, place}
		}
		place++
	}
	b.expanded = v.Len()
	if i%keyframeEvery == 0 {
		keyframe := binary.AppendUvarint(nil, uint64(v.Len())) //nolint:gosec // a length
		for k := range v.All() {
			o := b.origins[k.String()]
			keyframe = binary.AppendUvarint(binary.AppendUvarint(keyframe, uint64(i-o.entry)), uint64(o.place)) //nolint:gosec // places and earlier entries
		}
		b.keyframes = append(b.keyframes, keyframe)
	}

	var sv scannedVersion
	if b.scanned != nil {
		sv = b.scanned[i]
	} else {
		sv = scanVersion(v, p2Codec.parser, p2Codec.loader)
	}
	b.scannedPart = appendScanned(b.scannedPart, sv, b.arrayRef)
	b.rec = b.rec[:0]
	if !sv.skip {
		if fits == fitUnknown {
			if b.checker == nil {
				b.checker = p2Codec.loader.SkeletonChecker()
			}
			fits = fitNo
			if fitsSkeleton(b.checker, v) {
				fits = fitYes
			}
		}
		if fits == fitYes {
			if b.rec, b.ok = appendSkeleton(b.rec, v, b.arrayRef); !b.ok {
				return false
			}
		}
	}
	b.skeletons = appendBytes(b.skeletons, b.rec)

	return true
}

// appendTo appends the keyframes and the index, once every version was
// added.
func (b *indexBuilder) appendTo(dst []byte) ([]byte, bool) {
	if !b.ok || b.added != len(b.items) {
		return dst, false
	}
	dst = binary.AppendUvarint(dst, uint64(len(b.keyframes)))
	for _, k := range b.keyframes {
		dst = appendBytes(dst, k)
	}
	dst = append(dst, 1)
	dst, ok := appendPart(dst, b.links)
	if !ok {
		return dst, false
	}
	dst = appendBytes(dst, b.scannedPart)

	return appendBytes(dst, b.skeletons), true
}

// The flags of a scanned version in its slot form.
const (
	scannedSkip byte = 1 << iota
	scannedExact
	scannedRequire
)

// appendScanned appends a scanned version: its flags, then, unless it is
// skipped, its normalized version and alias (each after its length) and
// its require's link ref, if any.
func appendScanned(dst []byte, sv scannedVersion, arrayRef func(*php.Array) uint64) []byte {
	if sv.skip {
		return append(dst, scannedSkip)
	}
	var flags byte
	if sv.exact {
		flags |= scannedExact
	}
	if sv.require != nil {
		flags |= scannedRequire
	}
	dst = append(dst, flags)
	dst = appendString(dst, sv.normalized)
	dst = appendString(dst, sv.alias)
	if sv.require != nil {
		dst = binary.AppendUvarint(dst, arrayRef(sv.require))
	}

	return dst
}

// fitsSkeleton is checker.Fits for an expanded version, loaded with any
// notification-url when it has none (the loads give it the repository's).
func fitsSkeleton(checker *loader.SkeletonChecker, v *php.Array) (fits bool) {
	withNotificationURL(v, "https://notify.invalid", func() { fits = checker.Fits(v) })

	return fits
}

// withNotificationURL runs fn with url as the notification-url of v, a
// version, when it has none (null or no key), as createPackages loads
// it; v is then left as it was.
func withNotificationURL(v *php.Array, url any, fn func()) {
	n, present := v.Get("notification-url")
	if n != nil {
		fn()

		return
	}
	v.Set("notification-url", url)
	fn()
	if present {
		v.Set("notification-url", n)
	} else {
		// the key added last: removing it leaves the array as it was
		v.Delete("notification-url")
	}
}

// decode reads back an index of a list of count versions; nil when it
// does not hold them.
func (d *p2IndexData) decode(count int) *p2Index {
	v, err := php.DecodeBinary(d.links)
	links, _ := v.(*php.Array)
	if err != nil || links == nil {
		return nil
	}
	idx := &p2Index{exact: true, scanned: make([]scannedVersion, 0, count), skeletons: d.skeletons, offsets: make([]int, 0, count)}

	// the records' offsets; their strings are cut from one copy
	r := p2Reader{b: d.skeletons}
	for range count {
		n, ok := r.count()
		if !ok {
			return nil
		}
		if n == 0 {
			idx.offsets = append(idx.offsets, -1)
		} else {
			idx.offsets = append(idx.offsets, r.pos)
		}
		r.pos += n
	}
	if r.pos != len(r.b) {
		return nil
	}
	idx.skeletonsStr = string(d.skeletons)

	for _, l := range links.Values() {
		a, _ := l.(*php.Array)
		idx.links = append(idx.links, a)
	}

	// the strings are cut from one copy of the scanned versions
	r = p2Reader{b: d.scanned, s: string(d.scanned)}
	for range count {
		flags, ok := r.byte()
		if !ok {
			return nil
		}
		if flags&scannedSkip != 0 {
			idx.exact = false
			idx.scanned = append(idx.scanned, scannedVersion{skip: true})

			continue
		}
		sv := scannedVersion{normalized: r.str(), alias: r.str(), exact: flags&scannedExact != 0}
		if flags&scannedRequire != 0 {
			ref := r.uvarint()
			sv.require = idx.link(ref)
		}
		idx.exact = idx.exact && sv.exact
		idx.scanned = append(idx.scanned, sv)
	}
	if r.bad || r.pos != len(r.b) {
		return nil
	}

	return idx
}

// decodeP2 reads a file back from its slot form, for json, the JSON it
// was decoded from.
func decodeP2(data []byte, json string) (any, error) {
	if len(data) < crcSize || crc32.Checksum(data[crcSize:], crcTable) != binary.LittleEndian.Uint32(data) {
		return nil, errP2Slot
	}
	data = data[crcSize:]
	r := p2Reader{b: data}
	s := &p2Slot{json: json, topData: r.part()}
	if count, ok := r.count(); ok {
		s.names = make(map[string]*p2SlotName, count)
		for range count {
			name := string(r.part())
			n := &p2SlotName{}
			entries, _ := r.count()
			n.entries = make([]php.JSONSpan, 0, entries)
			end := 0
			for range entries {
				gap, size := r.uvarint(), r.uvarint()
				if r.bad || gap > uint64(len(json)-end) || size == 0 || size > uint64(len(json)-end)-gap {
					return nil, errP2Slot
				}
				start := end + int(gap) //nolint:gosec // bounded by len(json)
				end = start + int(size) //nolint:gosec // bounded by len(json)
				n.entries = append(n.entries, php.JSONSpan{Start: start, End: end})
			}
			if keyframes, _ := r.count(); keyframes > 0 {
				n.keyframes = make([][]byte, 0, keyframes)
				for range keyframes {
					n.keyframes = append(n.keyframes, r.part())
				}
			}
			if hasIndex, _ := r.byte(); hasIndex == 1 {
				n.index = &p2IndexData{links: r.part(), scanned: r.part(), skeletons: r.part()}
			}
			s.names[name] = n
		}
	}
	if r.bad || r.pos != len(data) {
		return nil, errP2Slot
	}

	top, err := php.DecodeBinary(s.topData)
	if err != nil {
		return nil, err
	}
	if s.top, _ = top.(*php.Array); s.top == nil {
		return nil, errP2Slot
	}
	packages, _ := s.top.At("packages").(*php.Array)
	for name, n := range s.names {
		if v, ok := packages.Get(name); !ok || v != nil || len(n.entries) == 0 ||
			(n.keyframes != nil && len(n.keyframes) != (len(n.entries)+keyframeEvery-1)/keyframeEvery) {
			return nil, errP2Slot
		}
	}

	return &p2File{slot: s}, nil
}

// p2Reader reads the parts of a slot; s, when set, is b as a string, which
// str cuts strings from.
type p2Reader struct {
	b   []byte
	s   string
	pos int
	bad bool
}

func (r *p2Reader) byte() (byte, bool) {
	if r.pos >= len(r.b) {
		r.bad = true

		return 0, false
	}
	r.pos++

	return r.b[r.pos-1], true
}

func (r *p2Reader) uvarint() uint64 {
	x, n := binary.Uvarint(r.b[r.pos:])
	if n <= 0 {
		r.bad = true

		return 0
	}
	r.pos += n

	return x
}

// count reads a count or length of things that follow, which the bytes
// left must hold.
func (r *p2Reader) count() (int, bool) {
	x, n := binary.Uvarint(r.b[r.pos:])
	if n <= 0 || x > uint64(len(r.b[r.pos+n:])) {
		r.bad = true

		return 0, false
	}
	r.pos += n

	return int(x), true //nolint:gosec // bounded by len(r.b)
}

func (r *p2Reader) part() []byte {
	n, ok := r.count()
	if !ok {
		return nil
	}
	p := r.b[r.pos : r.pos+n : r.pos+n]
	r.pos += n

	return p
}

func (r *p2Reader) str() string {
	n, ok := r.count()
	if !ok {
		return ""
	}
	s := r.s[r.pos : r.pos+n]
	r.pos += n

	return s
}
