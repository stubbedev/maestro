package composerrepo

import (
	"bytes"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/loader"
)

// FuzzP2Slot checks that a metadata file read back from its slot form is
// what decoding its JSON gives (checkP2File), whatever the file holds.
func FuzzP2Slot(f *testing.F) {
	for _, seed := range []string{
		`{"minified":"composer/2.0","packages":{"a/a":[{"name":"a/a","version":"1.0.0","version_normalized":"1.0.0.0","require":{"b/b":"^1"}},{"version":"1.1.0","version_normalized":"1.1.0.0"},{"version":"2.0.0","version_normalized":"2.0.0.0","require":"__unset"}]}}`,
		`{"minified":"composer/2.0","packages":{"a/a":[{"name":"a/a","version":"dev-main","version_normalized":"dev-main","default-branch":true,"extra":{"branch-alias":{"dev-main":"2.x-dev"}}},{"version":"1.0.0","version_normalized":"1.0.0.0","time":"yesterday"}]},"security-advisories":[]}`,
		`{"packages":{"a/a":[{"name":"a/a","version":"1.0"}],"b/b":{"x":1},"0":[{"name":"c/c"}]},"last-modified":"x"}`,
		`{"minified":"composer/2.0","packages":{"a/a":[{"name":"a/a","version":"1.0","dist":"x"},{"0":1},{"version":"2"}]}}`,
		`{"minified":"composer/2.0","packages":{"a/a":[]}}`,
		`{"packages":[]}`,
		// the expansion restarts after an entry unsets every key; keyframes past the first
		`{"minified":"composer/2.0","packages":{"a/a":[{"name":"a/a","version":"1"},{"name":"__unset","version":"__unset"},{"version":"2","x":"__unset"},{"x":1},{"version":"3"},{"y":[1]},{"x":"__unset"},{"version":"4"},{"version":"5"},{"version":"6"},{"version":"7"},{"version":"8"},{"version":"9"},{"version":"10"},{"version":"11"},{"version":"12"},{"y":"__unset","version":"13"},{"version":"14","x":2},{"name":"a/b"}]}}`,
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, json string) {
		data := decodeArray(json)
		if data == nil {
			return
		}
		slot, ok := appendP2(nil, decodeFile(json))
		if !ok {
			return
		}
		v, err := decodeP2(slot, json)
		if err != nil {
			t.Fatalf("the slot does not read back: %v", err)
		}
		file := v.(*p2File)
		if !reflect.DeepEqual(file.array(), decodeArray(json)) {
			t.Fatal("the file read back differs")
		}
		checkP2File(t, "fuzz", file, decodeArray(json))

		// a damaged slot does not read back
		for _, damaged := range [][]byte{slot[:len(slot)-1], append(append([]byte{}, slot...), 0), flipped(slot)} {
			if _, err := decodeP2(damaged, json); err == nil {
				t.Fatal("a damaged slot reads back")
			}
		}
	})
}

// flipped is data with a bit of its last byte flipped.
func flipped(data []byte) []byte {
	out := append([]byte{}, data...)
	out[len(out)-1] ^= 1

	return out
}

// drafted is the file decoded from json with the drafts the speculation
// leaves: each minified list scanned (versionsOf) and, unless loaded is
// nil, the versions whose index loaded tells loaded with a repository
// loader (prebuild), with the list's index built on the way when index
// is set.
func drafted(json string, loaded func(i int) bool, index bool) *p2File {
	f := decodeFile(json)
	if f == nil || f.data.At("minified") != "composer/2.0" {
		return f
	}
	parser, l := pkg.NewVersionParser(), loader.NewArrayLoader(nil, false)
	packages, _ := f.data.At("packages").(*php.Array)
	for name, list := range packages.All() {
		items := asArray(list).Values()
		d := f.draft(name.String())
		var ok bool
		if d.scanned, _, ok = scanVersions(items, true, parser, l); !ok {
			d.scanned = nil
		}
		if loaded == nil {
			continue
		}
		var b *indexBuilder
		if index {
			b = d.builder(items)
		}
		batch := l.Batch()
		i := -1
		_, _ = expandEach(items, func(v *php.Array, _ func() *php.Array) error {
			i++
			fits := fitUnknown
			if normalized := v.At("version_normalized"); loaded(i) && normalized != nil && normalized != pkg.DefaultBranchAlias {
				var err error
				withNotificationURL(v, "https://example.org/notify", func() { _, err = batch.Load(v) })
				fits = fitNo
				if err != nil {
					batch = l.Batch()
				} else if loader.LoadedFits(v) {
					fits = fitYes
				}
			}
			if b != nil {
				b.add(v, fits)
			}

			return nil
		})
	}

	return f
}

// A slot built from what the speculation found out about its file (its
// drafts: the scans, and the index built while loading some versions) is
// the slot built without them.
func TestP2SlotFromDrafts(t *testing.T) {
	for path, json := range p2Files(t) {
		want, ok := appendP2(nil, decodeFile(json))
		if !ok {
			t.Fatalf("%s: no slot", path)
		}
		for _, every := range []int{0, 1, 3} {
			loaded := func(i int) bool { return every > 0 && i%every == 0 }
			got, ok := appendP2(nil, drafted(json, loaded, every > 0))
			if !ok || !bytes.Equal(got, want) {
				t.Fatalf("%s: the slot built from drafts of every %d versions differs", path, every)
			}
		}
	}
}

// What a run that decodes a file does for its slot, beyond what it does
// anyway (the speculation's scan and loads of most of its versions):
// building the index on the way and storing the slot, allocates less
// than the store before skeletons did (the file in binary form,
// php.AppendBinary), and the slots take less room.
func TestP2SlotStoreBudget(t *testing.T) {
	paths, _ := filepath.Glob(p2Fixtures + "/*.json.gz")
	var slotAlloc, binaryAlloc, slotSize, binarySize uint64
	loaded := func(i int) bool { return i%5 != 0 }
	allocated := func(fn func()) uint64 {
		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		fn()
		runtime.ReadMemStats(&after)

		return after.TotalAlloc - before.TotalAlloc
	}
	for _, path := range paths {
		json := readGzip(t, path)
		data := decodeArray(json)
		slotAlloc += allocated(func() {
			slot, _ := appendP2(nil, drafted(json, loaded, true))
			slotSize += uint64(len(slot))
		})
		slotAlloc -= allocated(func() { drafted(json, loaded, false) })
		binaryAlloc += allocated(func() {
			b, _ := php.AppendBinary(nil, data)
			binarySize += uint64(len(b))
		})
	}
	t.Logf("slots: %d bytes allocated, %d bytes; binary: %d bytes allocated, %d bytes", slotAlloc, slotSize, binaryAlloc, binarySize)
	if slotAlloc > binaryAlloc {
		t.Errorf("storing the slots allocated %d bytes, the binary form %d", slotAlloc, binaryAlloc)
	}
	if slotSize > binarySize/2 {
		t.Errorf("the slots take %d bytes, the binary form %d", slotSize, binarySize)
	}
}
