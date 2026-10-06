package php

import (
	"compress/gzip"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// p2Fixtures are real Packagist metadata files.
const p2Fixtures = "../pkg/loader/testdata/oracle/p2"

func readP2Fixtures(t testing.TB) map[string]string {
	t.Helper()
	paths, err := filepath.Glob(p2Fixtures + "/*.json.gz")
	if err != nil || len(paths) == 0 {
		t.Fatalf("no p2 fixtures: %v", err)
	}
	files := map[string]string{}
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			t.Fatal(err)
		}
		r, err := gzip.NewReader(f)
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(r)
		_ = f.Close()
		if err != nil {
			t.Fatal(err)
		}
		files[filepath.Base(p)] = string(b)
	}

	return files
}

// binaryRoundTrip checks that the binary form of what json_decode() gives
// for json reads back as exactly that, internal state included.
func binaryRoundTrip(t *testing.T, name, json string) {
	t.Helper()
	want, err := JSONDecode(json, true)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	b, ok := AppendBinary(nil, want)
	if !ok {
		t.Fatalf("%s: not encoded", name)
	}
	got, err := DecodeBinary(b)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s: decoded %s, want %s", name, mustJSON(t, got), mustJSON(t, want))
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	s, err := JSONEncode(v, 0)
	if err != nil {
		return err.Error()
	}

	return s
}

func TestBinaryRoundTrip(t *testing.T) {
	for _, json := range []string{
		`null`, `true`, `false`, `0`, `-1`, `9223372036854775807`, `-9223372036854775808`,
		`99999999999999999999`, `1.5`, `-0.0`, `1e300`, `""`, `"a\u0000b"`, `"é"`,
		`[]`, `{}`, `[[]]`, `[{}]`, `[1,"a",null,true,1.0]`,
		`{"a":1,"b":"a","a":2}`, `{"1":1,"0":2}`, `{"0":"x","1":"y"}`, `{"-5":1,"9223372036854775807":2}`,
		`{"05":1,"1.5":2,"-0":3," 1":4,"9223372036854775808":5}`,
		`{"a":{"b":{"c":["a","b","a"]}},"b":["b"]}`,
		`{"1":1,"2":2,"3":3,"4":4,"5":5,"6":6,"7":7,"8":8,"9":9,"10":10}`,
		`{"a":1,"b":2,"c":3,"d":4,"e":5,"f":6,"g":7,"h":8,"i":9,"j":10,"a":11}`,
		`[0,1,2,3,4,5,6,7,8,9,10,11,12]`,
	} {
		binaryRoundTrip(t, json, json)
	}
	for name, json := range readP2Fixtures(t) {
		binaryRoundTrip(t, name, json)
	}
}

func TestBinaryRefusesWhatItCannotCarry(t *testing.T) {
	if _, ok := AppendBinary(nil, NewObject()); ok {
		t.Error("object encoded")
	}
	if _, ok := AppendBinary(nil, ListOf(1, NewObject())); ok {
		t.Error("nested object encoded")
	}
	// an array that lost its largest int key keeps its next free index
	a := ListOf(1, 2, 3)
	a.Delete(int64(2))
	if _, ok := AppendBinary(nil, a); ok {
		t.Error("array with a larger next free index encoded")
	}
	b := ArrayOf("a", 1, "b", 2)
	b.Delete("a")
	got, err := DecodeBinary(mustBinary(t, b))
	if err != nil || !StrictEquals(got, ArrayOf("b", 2)) {
		t.Errorf("array with a deleted key: %v, %v", got, err)
	}
	if got, err := DecodeBinary(mustBinary(t, math.Inf(1))); err != nil || got != math.Inf(1) {
		t.Errorf("inf: %v, %v", got, err)
	}
}

func mustBinary(t *testing.T, v any) []byte {
	t.Helper()
	b, ok := AppendBinary(nil, v)
	if !ok {
		t.Fatalf("%v not encoded", v)
	}

	return b
}

func TestDecodeBinaryRejectsMalformed(t *testing.T) {
	good := mustBinary(t, ArrayOf("a", ListOf("x", 1.5, int64(-3)), "b", "x"))
	for i := range good {
		if _, err := DecodeBinary(good[:i]); err == nil {
			t.Errorf("truncated to %d: no error", i)
		}
	}
	for _, b := range [][]byte{
		{},
		{99},
		append(append([]byte{}, good...), 0),
		{binStrRef, 0}, // unknown string
		{binArray, 2, binKeyInt, 0, binNull, binKeyInt, 0, binNull},          // key twice
		{binArray, 1, binNull, binNull},                                      // not a key
		{binArray, 0xff, 0xff, 0xff, 0xff, 0x0f},                             // count beyond the data
		{binInt, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x7f}, // varint overflow
	} {
		if _, err := DecodeBinary(b); err == nil {
			t.Errorf("%v: no error", b)
		}
	}
	// any damage is an error or some value, never a panic
	for i := range good {
		for _, b := range []byte{0, 1, 0x7f, 0x80, 0xff, good[i] ^ 1} {
			damaged := append([]byte{}, good...)
			damaged[i] = b
			_, _ = DecodeBinary(damaged)
		}
	}
	deep := make([]byte, 0, 2*binMaxDepth+2)
	for range binMaxDepth + 1 {
		deep = append(deep, binArray, 1, binKeyInt, 0)
	}
	if _, err := DecodeBinary(append(deep, binNull)); err == nil {
		t.Error("too deep: no error")
	}
}

func BenchmarkP2Decode(b *testing.B) {
	files := readP2Fixtures(b)
	json := files["laravel_framework.json.gz"]
	v, _ := JSONDecode(json, true)
	bin, _ := AppendBinary(nil, v)
	b.Run("json", func(b *testing.B) {
		b.SetBytes(int64(len(json)))
		for b.Loop() {
			_, _ = JSONDecode(json, true)
		}
	})
	b.Run("binary", func(b *testing.B) {
		b.SetBytes(int64(len(json)))
		for b.Loop() {
			_, _ = DecodeBinary(bin)
		}
	})
	b.Run("encode", func(b *testing.B) {
		b.SetBytes(int64(len(json)))
		for b.Loop() {
			_, _ = AppendBinary(nil, v)
		}
	})
}
