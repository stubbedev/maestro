// Ports nothing: the form metadata files are kept in between runs, which
// decodes a package's versions one by one and only when they are read,
// with an index of what the speculation and the loads read of them
// (deliberate deviation 3, speed).

package composerrepo

import (
	"encoding/binary"
	"errors"
	"hash/crc32"
	"slices"
	"sync"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/loader"
)

// p2Slot is a metadata file read back from its slot. Its top is the file
// with the version list of each slotted package replaced by null; those
// lists are decoded from their entries when read.
type p2Slot struct {
	topData []byte
	top     *php.Array
	names   map[string]*p2SlotName
}

// p2SlotName is a slotted package's version list: its entries, each in
// its binary form and, for a minified list that expandEach expands, the
// expanded version at every keyframeEvery-th entry and the index of the
// versions.
type p2SlotName struct {
	entries   [][]byte
	keyframes [][]byte
	index     *p2IndexData

	indexOnce sync.Once
	idx       *p2Index
}

// keyframeEvery is how many entries apart the expanded versions a slot
// keeps are: expanding a version decodes at most that many entries.
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

// The keys of a skeleton record: those of loader.SkeletonConfig.
var skeletonKeys = [...]string{
	"name", "version", "version_normalized", "type", "default-branch", "abandoned",
	"require", "conflict", "provide", "replace", "require-dev", "extra",
}

// The tags of the values of a skeleton record.
const (
	skNull   byte = iota
	skFalse       // false
	skTrue        // true
	skString      // its length, its bytes
	skLink        // a link array, by its ref
	skValue       // any other value: its length, its binary form
)

// appendSkeleton appends the skeleton record of config, a SkeletonConfig:
// the number of its keys, then each key's index in skeletonKeys and its
// value; false for a key it does not know.
func appendSkeleton(dst []byte, config *php.Array, linkRef func(*php.Array) uint64) ([]byte, bool) {
	dst = binary.AppendUvarint(dst, uint64(len(config.Keys())))
	for k, v := range config.All() {
		key := slices.Index(skeletonKeys[:], k.String())
		if key < 0 || !k.IsString() {
			return dst, false
		}
		dst = append(dst, byte(key)) //nolint:gosec // an index in skeletonKeys
		_, isLink := pkg.SupportedLinkType(k.String())
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
			dst = appendBytes(append(dst, skString), []byte(v))
		default:
			if a, ok := v.(*php.Array); ok && isLink {
				dst = binary.AppendUvarint(append(dst, skLink), linkRef(a))

				continue
			}
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
		if !ok || int(key) >= len(skeletonKeys) {
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
		case skLink:
			ref := r.uvarint()
			v = idx.link(ref)
		case skValue:
			var err error
			if v, err = php.DecodeBinary([]byte(r.str())); err != nil {
				return nil
			}
		default:
			return nil
		}
		config.Set(skeletonKeys[key], v)
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
	for _, e := range n.entries {
		list.Append(mustDecode(e))
	}

	return list
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
// it, an array of its own; ok is false when the slot keeps no expanded
// versions of the list.
func (s *p2Slot) expanded(name string, i int) (*php.Array, bool) {
	n, ok := s.names[name]
	if !ok || n.keyframes == nil || i < 0 || i >= len(n.entries) {
		return nil, false
	}
	from := i - i%keyframeEvery
	working, _ := mustDecode(n.keyframes[from/keyframeEvery]).(*php.Array)
	for j := from + 1; j <= i; j++ {
		entry, _ := mustDecode(n.entries[j]).(*php.Array)
		working, _ = expandNext(working, entry)
	}

	return working, true
}

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

// The p2 codec's form of a file: its checksum, then its top, then for each slotted package,
// its name, its entries, its keyframes and its index: a flag, then the
// links, the scanned versions and the skeleton configs. Each part is a
// uvarint count or length followed by its bytes.

// p2Codec keeps metadata files as p2Slots.
var p2Codec = struct {
	parser *pkg.VersionParser
	loader *loader.ArrayLoader
}{pkg.NewVersionParser(), loader.NewArrayLoader(nil, true)}

var errP2Slot = errors.New("composerrepo: malformed p2 slot")

// appendP2 appends the slot form of v, a *p2File decoded at once,
// after its checksum: the parts are decoded only when they are read, so
// that a damaged slot cannot be told from a good one then.
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
			if !ok || !k.IsString() || !l.IsAppended() {
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
		dst = appendBytes(dst, []byte(n.name))
		items := n.list.Values()
		dst = binary.AppendUvarint(dst, uint64(len(items)))
		for _, item := range items {
			if dst, ok = appendPart(dst, item); !ok {
				return dst, false
			}
		}
		if dst, ok = appendIndex(dst, items, minified); !ok {
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

// appendIndex appends the keyframes and the index a slot keeps of a
// version list: none unless the list is minified and expandEach expands
// it.
func appendIndex(dst []byte, items []any, minified bool) ([]byte, bool) {
	if !minified {
		return append(dst, 0, 0), true
	}
	if _, ok := expandable(items); !ok {
		return append(dst, 0, 0), true
	}

	refs := map[*php.Array]uint64{}
	links := php.NewArray()
	linkRef := func(a *php.Array) uint64 {
		ref, ok := refs[a]
		if !ok {
			ref = uint64(len(refs))
			refs[a] = ref
			links.Append(a)
		}

		return ref
	}

	var (
		keyframes [][]byte
		scanned   []byte
		skeletons []byte
		ok        = true
		checker   = p2Codec.loader.SkeletonChecker()
	)
	i := -1
	_, _ = expandEach(items, func(v *php.Array, _ func() *php.Array) error {
		i++
		if i%keyframeEvery == 0 {
			var keyframe []byte
			keyframe, ok = php.AppendBinary(nil, v)
			if !ok {
				return errStopExpanding
			}
			keyframes = append(keyframes, keyframe)
		}
		sv := scanVersion(v, p2Codec.parser, p2Codec.loader)
		scanned = appendScanned(scanned, sv, linkRef)
		var record []byte
		if !sv.skip && fitsSkeleton(checker, v) {
			if record, ok = appendSkeleton(nil, loader.SkeletonConfig(v), linkRef); !ok {
				return errStopExpanding
			}
		}
		skeletons = appendBytes(skeletons, record)

		return nil
	})
	if !ok {
		return dst, false
	}

	dst = binary.AppendUvarint(dst, uint64(len(keyframes)))
	for _, k := range keyframes {
		dst = appendBytes(dst, k)
	}
	dst = append(dst, 1)
	if dst, ok = appendPart(dst, links); !ok {
		return dst, false
	}
	dst = appendBytes(dst, scanned)
	dst = appendBytes(dst, skeletons)

	return dst, true
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
func appendScanned(dst []byte, sv scannedVersion, linkRef func(*php.Array) uint64) []byte {
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
	dst = appendBytes(dst, []byte(sv.normalized))
	dst = appendBytes(dst, []byte(sv.alias))
	if sv.require != nil {
		dst = binary.AppendUvarint(dst, linkRef(sv.require))
	}

	return dst
}

// fitsSkeleton is checker.Fits for an expanded version, loaded with any
// notification-url when it has none (the loads give it the repository's).
func fitsSkeleton(checker *loader.SkeletonChecker, v *php.Array) bool {
	n, present := v.Get("notification-url")
	if n != nil {
		return checker.Fits(v)
	}
	v.Set("notification-url", "https://notify.invalid")
	fits := checker.Fits(v)
	if present {
		v.Set("notification-url", n)
	} else {
		// the key added last: removing it leaves the array as it was
		v.Delete("notification-url")
	}

	return fits
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

// decodeP2 reads a file back from its slot form.
func decodeP2(data []byte) (any, error) {
	if len(data) < crcSize || crc32.Checksum(data[crcSize:], crcTable) != binary.LittleEndian.Uint32(data) {
		return nil, errP2Slot
	}
	data = data[crcSize:]
	r := p2Reader{b: data}
	s := &p2Slot{topData: r.part()}
	if count, ok := r.count(); ok {
		s.names = make(map[string]*p2SlotName, count)
		for range count {
			name := string(r.part())
			n := &p2SlotName{}
			entries, _ := r.count()
			n.entries = make([][]byte, 0, entries)
			for range entries {
				n.entries = append(n.entries, r.part())
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
