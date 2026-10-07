package php

import (
	"os"
	"path/filepath"
	"testing"
)

// A nil array is PHP's absent one: every read is empty and isset() false.
func TestArray_NilReads(t *testing.T) {
	var a *Array
	if a.Len() != 0 || a.Isset("k") || a.At("k") != nil || a.ArrayAt("k") != nil || a.Path("a", "b") != nil || a.Has("k") {
		t.Error("a nil array is not empty")
	}
	if _, ok := a.Get("k"); ok {
		t.Error("Get on a nil array")
	}
	for range a.All() {
		t.Error("All yields from a nil array")
	}
	if c := a.Clone(); c == nil || c.Len() != 0 {
		t.Error("Clone of a nil array is not a new empty one")
	}
}

func TestArray_Accessors(t *testing.T) {
	a := ArrayOf("null", nil, "list", ListOf("x"), "deep", ArrayOf("k", "v"))
	if a.Isset("null") || !a.Has("null") || !a.Isset("list") {
		t.Error("isset() and array_key_exists() differ only for null")
	}
	if a.Path("deep", "k") != "v" || a.Path("deep", "missing") != nil || a.Path("list", 0, "x") != nil {
		t.Error("Path")
	}
	if a.ArrayAt("null") != nil || a.ArrayAt("deep") == nil {
		t.Error("ArrayAt")
	}
	created := a.ArrayAtOrCreate("new")
	created.Set("k", "v")
	if a.Path("new", "k") != "v" || a.ArrayAtOrCreate("deep").At("k") != "v" {
		t.Error("ArrayAtOrCreate")
	}
}

func TestTruthy(t *testing.T) {
	for s, want := range map[string]bool{"": false, "0": false, "00": true, "0.0": true, " ": true, "false": true} {
		if Truthy(s) != want {
			t.Errorf("Truthy(%q) = %v", s, !want)
		}
	}
}

// strtolower() and strtoupper() are ASCII-only (PHP 8.2+): the Kelvin sign
// and the long s are left as they are.
func TestCaseMappingIsASCII(t *testing.T) {
	const kelvin, longS = "\xe2\x84\xaa", "\xc5\xbf" // U+212A, U+017F
	if got := Strtolower("Acme/" + kelvin); got != "acme/"+kelvin {
		t.Errorf("Strtolower = %q", got)
	}
	if got := Strtoupper("post-in" + longS + "tall"); got != "POST-IN"+longS+"TALL" {
		t.Errorf("Strtoupper = %q", got)
	}
}

func TestHashes(t *testing.T) {
	if Md5("abc") != "900150983cd24fb0d6963f7d28e17f72" || Sha1("abc") != "a9993e364706816aba3e25717850c26c9cd0d89d" {
		t.Error("md5/sha1")
	}
	path := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(path, []byte("abc"), 0o666); err != nil {
		t.Fatal(err)
	}
	if got, err := Sha1File(path); err != nil || got != Sha1("abc") {
		t.Errorf("Sha1File = %q, %v", got, err)
	}
	if h := RandomHex(8); len(h) != 16 {
		t.Errorf("RandomHex(8) = %q", h)
	}
}
