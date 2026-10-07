package json

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
)

// installedJSON is an installed.json of n packages, large enough to be
// kept decoded.
func installedJSON(n int, version string) string {
	packages := make([]string, n)
	for i := range packages {
		packages[i] = `{"name": "vendor/package-` + strings.Repeat("x", i%7) + `", "version": "` + version + `", "type": "library", "autoload": {"psr-4": {"Vendor\\\\": "src/"}}}`
	}

	return "{\n    \"packages\": [\n        " + strings.Join(packages, ",\n        ") + "\n    ],\n    \"dev\": true\n}\n"
}

// A file read with KeepDecoded decodes to what its JSON decodes to, from
// its slot once kept, with the file's indentation and content as a read
// that decodes gives; a rewrite of the same size is decoded again.
func TestFile_KeepDecoded(t *testing.T) {
	slots := t.TempDir()
	UseDecodedFiles(slots)
	t.Cleanup(func() { UseDecodedFiles("") })

	path := filepath.Join(t.TempDir(), "installed.json")
	content := installedJSON(100, "1.0.0")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	want, err := ParseJSON(content, path)
	if err != nil {
		t.Fatal(err)
	}

	for i := range 2 {
		f, err := NewFile(path, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		f.KeepDecoded()
		got, err := f.Read()
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("read %d decoded to another value", i)
		}
		if c, ok := f.Content(); !ok || c != content || f.indent != "    " {
			t.Errorf("read %d: content %v, indent %q", i, ok && c == content, f.indent)
		}
	}
	if entries, _ := os.ReadDir(slots); len(entries) != 1 {
		t.Fatalf("%d slots, want 1", len(entries))
	}

	// same size, other content
	changed := installedJSON(100, "2.0.0")
	if len(changed) != len(content) {
		t.Fatal("the rewrite changes the size")
	}
	if err := os.WriteFile(path, []byte(changed), 0o644); err != nil {
		t.Fatal(err)
	}
	f, _ := NewFile(path, nil, nil)
	f.KeepDecoded()
	got, err := f.Read()
	if err != nil {
		t.Fatal(err)
	}
	if v := got.(*php.Array).Path("packages", 0, "version"); v != "2.0.0" {
		t.Errorf("the rewritten file read as version %v", v)
	}
}
