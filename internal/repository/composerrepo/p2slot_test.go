package composerrepo

import (
	"reflect"
	"testing"
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
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, json string) {
		data := decodeArray(json)
		if data == nil {
			return
		}
		slot, ok := appendP2(nil, eagerFile(data))
		if !ok {
			return
		}
		v, err := decodeP2(slot)
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
			if _, err := decodeP2(damaged); err == nil {
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
