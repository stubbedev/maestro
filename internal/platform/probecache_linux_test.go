package platform

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
)

// fakeProbeOutput is what probe.php prints for a php of version, which
// mapped these files.
func fakeProbeOutput(t *testing.T, version string, mapped ...string) []byte {
	t.Helper()

	data, err := json.Marshal(map[string]any{
		"format":       probeFormat,
		"constants":    map[string]any{"PHP_VERSION": version, "PHP_VERSION_ID": 80425},
		"mapped_files": mapped,
	})
	if err != nil {
		t.Fatal(err)
	}

	return append([]byte(probeMarker), data...)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestProbeCache(t *testing.T) {
	t.Setenv("MAESTRO_CACHE_DIR", t.TempDir())

	dir := t.TempDir()
	binary := filepath.Join(dir, "php")
	lib := filepath.Join(dir, "libphp.so")
	writeFile(t, binary, "\x7fELF php")
	writeFile(t, lib, "library")

	key := probeCacheKey(binary)
	if key == "" {
		t.Fatal("no cache key for a php binary")
	}

	if s := loadProbeCache(key, binary); s != nil {
		t.Fatal("a snapshot before any was stored")
	}

	output := fakeProbeOutput(t, "8.4.25", binary, lib)

	s, err := ParseSnapshot(binary, output)
	if err != nil {
		t.Fatal(err)
	}

	storeProbeCache(key, binary, s, output)

	cached := loadProbeCache(key, binary)
	if cached == nil || cached.Version != "8.4.25" || cached.Binary != binary {
		t.Fatalf("cached snapshot %+v", cached)
	}

	// another environment is another entry
	t.Setenv("PROBE_CACHE_TEST_VARIABLE", "1")

	if probeCacheKey(binary) == key {
		t.Error("the environment is not part of the key")
	}

	// a library that changed invalidates the entry
	writeFile(t, lib, "library, upgraded")

	if loadProbeCache(key, binary) != nil {
		t.Error("an entry whose library changed was used")
	}

	// scripts (version managers' shims) are never cached
	shim := filepath.Join(dir, "php-shim")
	writeFile(t, shim, "#!/bin/sh\nexec php \"$@\"\n")

	if probeCacheKey(shim) != "" {
		t.Error("a script has a cache key")
	}
}

func TestProbeCache_Wrapper(t *testing.T) {
	t.Setenv("MAESTRO_CACHE_DIR", t.TempDir())

	dir := t.TempDir()
	wrapper := filepath.Join(dir, "php")
	real := filepath.Join(dir, ".php-wrapped")
	writeFile(t, wrapper, "\x7fELF wrapper")
	writeFile(t, real, "\x7fELF php")

	t.Chdir(t.TempDir())

	key := probeCacheKey(wrapper)
	output := fakeProbeOutput(t, "8.3.0", real)

	s, err := ParseSnapshot(wrapper, output)
	if err != nil {
		t.Fatal(err)
	}

	storeProbeCache(key, wrapper, s, output)

	if loadProbeCache(key, wrapper) == nil {
		t.Fatal("the wrapper's entry is not used in its directory")
	}

	// a wrapper may choose its php by the working directory
	t.Chdir(t.TempDir())

	if loadProbeCache(key, wrapper) != nil {
		t.Error("the wrapper's entry is used in another directory")
	}
}

// TestProbeCache_RealPHP probes the php on the PATH through the cache:
// the second detection is the cached result of the first.
func TestProbeCache_RealPHP(t *testing.T) {
	if os.Getenv("MAESTRO_PHP_TESTS") != "1" {
		t.Skip("set MAESTRO_PHP_TESTS=1 to probe php")
	}

	binary, ok := FindPHP()
	if !ok {
		t.Fatal("no php in PATH")
	}

	t.Setenv("MAESTRO_CACHE_DIR", t.TempDir())

	probed, err := probeCached(t.Context(), binary)
	if err != nil {
		t.Fatal(err)
	}

	if !probed.hasMappedFiles {
		t.Skip("php could not tell its mapped files")
	}

	key := probeCacheKey(binary)
	if key == "" {
		t.Skip("php is not cached here (a script)")
	}

	cached := loadProbeCache(key, binary)
	if cached == nil {
		t.Fatal("the probe was not cached")
	}

	if cached.Version != probed.Version || len(cached.Extensions) != len(probed.Extensions) || cached.PHPBinary() != probed.PHPBinary() {
		t.Errorf("cached %s (%d extensions), probed %s (%d)", cached.Version, len(cached.Extensions), probed.Version, len(probed.Extensions))
	}
}

func TestProbeCache_NoMappedFiles(t *testing.T) {
	t.Setenv("MAESTRO_CACHE_DIR", t.TempDir())

	binary := filepath.Join(t.TempDir(), "php")
	writeFile(t, binary, "\x7fELF php")

	key := probeCacheKey(binary)
	output := fakeProbeOutput(t, "8.4.25")

	s, err := ParseSnapshot(binary, output)
	if err != nil {
		t.Fatal(err)
	}

	storeProbeCache(key, binary, s, output)

	if loadProbeCache(key, binary) != nil {
		t.Error("a result whose mapped files are unknown was cached")
	}
}

// TestProbeCache_BinaryIdentical checks that the cache's binary form of
// the real probe's result reads back as the snapshot its JSON gives.
func TestProbeCache_BinaryIdentical(t *testing.T) {
	if os.Getenv("MAESTRO_PHP_TESTS") != "1" {
		t.Skip("set MAESTRO_PHP_TESTS=1 to probe php")
	}

	binary, ok := FindPHP()
	if !ok {
		t.Fatal("no php in PATH")
	}

	probed, output, err := probe(t.Context(), binary)
	if err != nil {
		t.Fatal(err)
	}

	result := probeResult(output)

	bin, ok := php.AppendBinary(nil, result)
	if !ok {
		t.Fatal("the probe's result has no binary form")
	}

	decoded, err := php.DecodeBinary(bin)
	if err != nil {
		t.Fatal(err)
	}

	if !php.StrictEquals(decoded, result) {
		t.Fatal("the binary form does not read back as the JSON")
	}

	cached, reason := snapshotOf(binary, decoded)
	if reason != "" {
		t.Fatal(reason)
	}

	if !sameField(reflect.ValueOf(cached), reflect.ValueOf(probed)) {
		t.Error("the snapshot of the binary form differs from the probed one")
	}
}

var phpArrayType = reflect.TypeFor[*php.Array]()

// sameField is reflect.DeepEqual, comparing php arrays with sameValue (their
// lookup indexes are built lazily).
func sameField(a, b reflect.Value) bool {
	if a.IsValid() != b.IsValid() {
		return false
	}

	if !a.IsValid() {
		return true
	}

	if a.Type() != b.Type() {
		return false
	}

	if a.Type() == phpArrayType {
		// (unexported fields cannot be read with Interface)
		return sameValue((*php.Array)(a.UnsafePointer()), (*php.Array)(b.UnsafePointer()))
	}

	switch a.Kind() {
	case reflect.Pointer, reflect.Interface:
		if a.IsNil() || b.IsNil() {
			return a.IsNil() == b.IsNil()
		}

		return sameField(a.Elem(), b.Elem())
	case reflect.Struct:
		for i := range a.NumField() {
			if !sameField(a.Field(i), b.Field(i)) {
				return false
			}
		}

		return true
	case reflect.Slice, reflect.Array:
		if a.Len() != b.Len() {
			return false
		}

		for i := range a.Len() {
			if !sameField(a.Index(i), b.Index(i)) {
				return false
			}
		}

		return true
	case reflect.Map:
		if a.Len() != b.Len() {
			return false
		}

		for _, k := range a.MapKeys() {
			if !sameField(a.MapIndex(k), b.MapIndex(k)) {
				return false
			}
		}

		return true
	case reflect.Float32, reflect.Float64:
		x, y := a.Float(), b.Float()

		return x == y || x != x && y != y
	default:
		return reflect.DeepEqual(valueOf(a), valueOf(b))
	}
}

// valueOf is v's value, unexported fields included.
func valueOf(v reflect.Value) any {
	switch v.Kind() {
	case reflect.Bool:
		return v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return v.Uint()
	case reflect.String:
		return v.String()
	}

	panic("unexpected kind " + v.Kind().String())
}
