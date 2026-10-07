package cache

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util/fsstate"
)

// A slot is read back for JSON read from a file of the identity it was
// stored for, when that identity could be trusted, and for nothing else;
// otherwise it holds a copy of the JSON and is read back for that JSON,
// and turned into a slot told by the origin once the origin can be
// trusted.
func TestDecoded_ByOrigin(t *testing.T) {
	if !fsstate.Known() {
		t.Skip("no file identities here")
	}
	root := t.TempDir()
	c, err := New(io.NewNullIO(), root, "", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	d := NewDecoded("test\n", 0, 0)
	slots := t.TempDir()
	d.Use(slots)
	decodes := 0
	decode := func(json string) (any, error) {
		decodes++

		return php.JSONDecode(json, true)
	}
	write := func(json string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, "f.json"), []byte(json), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// read reads the file through the cache and decodes it, storing what
	// Decode asks to; decoded tells whether the JSON was decoded
	read := func(want string, origin bool) (decoded bool) {
		t.Helper()
		json, ok := c.Peek("f.json")
		if !ok || json != want {
			t.Fatalf("Peek = %q, %v; want %q", json, ok, want)
		}
		var o Origin
		if origin {
			o = c.OriginOf("f.json", json)
			if !o.ok {
				t.Fatal("no origin for what Peek read")
			}
		}
		before := decodes
		v, store, err := d.Decode("f", o, json, decode)
		if want, _ := php.JSONDecode(json, true); err != nil || !reflect.DeepEqual(v, want) {
			t.Fatalf("Decode = %v, %v; want %v", v, err, want)
		}
		if store != nil {
			store()
		}

		return decodes > before
	}
	slotForm := func() byte {
		t.Helper()
		entries, _ := os.ReadDir(slots)
		if len(entries) != 1 {
			t.Fatalf("%d slots", len(entries))
		}
		data, err := os.ReadFile(filepath.Join(slots, entries[0].Name()))
		if err != nil {
			t.Fatal(err)
		}

		return data[len("test\n")]
	}

	// a file just written: its identity cannot be trusted yet
	write(`{"a": 1}`)
	if !read(`{"a": 1}`, true) {
		t.Fatal("first read: slot read back")
	}
	if slotForm() != slotByCopy {
		t.Fatal("a recent file's slot does not hold the JSON")
	}
	if read(`{"a": 1}`, true) {
		t.Fatal("copy: slot not read back")
	}
	if read(`{"a": 1}`, false) {
		t.Fatal("copy, no origin: slot not read back")
	}

	// trusted (as if the file were older than a timestamp tick): the
	// copy is read back once more, and the slot told by its origin
	d.margin = fsstate.Margin(-time.Hour)
	if read(`{"a": 1}`, true) {
		t.Fatal("copy: slot not read back")
	}
	if slotForm() != slotByOrigin {
		t.Fatal("a trusted file's slot holds the JSON")
	}
	if read(`{"a": 1}`, true) {
		t.Fatal("origin: slot not read back")
	}

	// the same JSON of no origin, and other JSON of the same length in
	// the file, are not told by the slot
	if !read(`{"a": 1}`, false) {
		t.Fatal("JSON of no origin: slot read back")
	}
	if read(`{"a": 1}`, true) || slotForm() != slotByOrigin {
		t.Fatal("origin: the copy a read of no origin stored is not read back, or not turned")
	}
	write(`{"a": 2}`)
	if !read(`{"a": 2}`, true) {
		t.Fatal("rewritten file: slot read back")
	}
	if read(`{"a": 2}`, true) {
		t.Fatal("rewritten file: slot not read back")
	}
}
