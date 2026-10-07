package platform

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/switches"
	"github.com/stubbedev/maestro/internal/util/fsstate"
)

// fakeProbeOutput is what probe.php prints for a php of version, which
// mapped these files.
func fakeProbeOutput(t *testing.T, version string, mapped ...string) []byte {
	t.Helper()

	return fakeProbeOutputWith(t, nil, version, mapped...)
}

// fakeProbeOutputWith is fakeProbeOutput with the fields of extra too.
func fakeProbeOutputWith(t *testing.T, extra map[string]any, version string, mapped ...string) []byte {
	t.Helper()

	result := map[string]any{
		"format":       probeFormat,
		"constants":    map[string]any{"PHP_VERSION": version, "PHP_VERSION_ID": 80425},
		"mapped_files": mapped,
	}
	maps.Copy(result, extra)

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}

	return append([]byte(probeMarker), data...)
}

// trustedStart is a probe start time the files a test just wrote are
// old enough for.
func trustedStart() time.Time {
	return time.Now().Add(fsstate.DefaultMargin + time.Second)
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

	storeProbeCache(key, binary, s, output, trustedStart())

	cached := loadProbeCache(key, binary)
	if cached == nil || cached.Version != "8.4.25" || cached.Binary != binary {
		t.Fatalf("cached snapshot %+v", cached)
	}

	// a variable php does not read is not part of the key
	t.Setenv("PROBE_CACHE_TEST_VARIABLE", "1")

	if probeCacheKey(binary) != key || loadProbeCache(key, binary) == nil {
		t.Error("an unrelated variable changed the entry")
	}

	// one php reads is
	t.Setenv("PHPRC", dir)

	if probeCacheKey(binary) == key {
		t.Error("PHPRC is not part of the key")
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

	storeProbeCache(key, wrapper, s, output, trustedStart())

	if loadProbeCache(key, wrapper) == nil {
		t.Fatal("the wrapper's entry is not used in its directory")
	}

	// a wrapper may choose its php by the working directory
	t.Chdir(t.TempDir())

	if loadProbeCache(key, wrapper) != nil {
		t.Error("the wrapper's entry is used in another directory")
	}
}

// storeFake stores the fake probe result of a php at binary (mapping
// mapped, with the fields of extra) and returns its key.
func storeFake(t *testing.T, binary string, extra map[string]any, mapped ...string) string {
	t.Helper()

	key := probeCacheKey(binary)
	if key == "" {
		t.Fatal("no cache key")
	}

	output := fakeProbeOutputWith(t, extra, "8.4.25", mapped...)

	s, err := ParseSnapshot(binary, output)
	if err != nil {
		t.Fatal(err)
	}

	storeProbeCache(key, binary, s, output, trustedStart())

	if loadProbeCache(key, binary) == nil {
		t.Fatal("the probe was not cached")
	}

	return key
}

// TestProbeCache_Env checks which environment variables an entry depends
// on: those php reads (isProbeEnv), those its ini files refer to and those
// its extensions read, and no others.
func TestProbeCache_Env(t *testing.T) {
	t.Setenv("MAESTRO_CACHE_DIR", t.TempDir())
	t.Setenv("PROBE_INI_VARIABLE", "a")
	t.Setenv("BLACKFIRE_SERVER_ID", "a")
	t.Setenv("MY_EXT_TOKEN", "a")
	t.Setenv("WINDOWID", "1")
	t.Setenv("COMPOSER_DEV_MODE", "1")
	t.Setenv("HYPERFINE_RANDOMIZED_ENVIRONMENT_OFFSET", "1")

	dir := t.TempDir()
	binary := filepath.Join(dir, "php")
	ini := filepath.Join(dir, "php.ini")
	writeFile(t, binary, "\x7fELF php")
	writeFile(t, ini, "memory_limit = ${PROBE_INI_VARIABLE}\nerror_log = \"${ PROBE_INI_DEFAULTED:-${HOME}}/log\"\n")

	key := storeFake(t, binary, map[string]any{
		"ini_files":  []any{ini, false},
		"extensions": [][]any{{"Core", "8.4.25", ""}, {"blackfire", "1.0", ""}, {"my_ext", "1.0", ""}},
	}, binary)

	for name, value := range map[string]string{
		"WINDOWID":          "2",
		"COMPOSER_DEV_MODE": "0",
		"HYPERFINE_RANDOMIZED_ENVIRONMENT_OFFSET": "22",
		"PROBE_UNRELATED":                         "1",
	} {
		t.Setenv(name, value)

		if probeCacheKey(binary) != key || loadProbeCache(key, binary) == nil {
			t.Errorf("a change of %s missed the cache", name)
		}
	}

	for _, name := range []string{"PHPRC", "PHP_INI_SCAN_DIR", "TZ", "LD_LIBRARY_PATH", "XDEBUG_MODE", "PATH", "LC_ALL"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(name, "changed")

			if probeCacheKey(binary) == key {
				t.Errorf("a change of %s kept the key", name)
			}
		})
	}

	for _, name := range []string{"PROBE_INI_VARIABLE", "PROBE_INI_DEFAULTED", "BLACKFIRE_SERVER_ID", "BLACKFIRE_NEW", "MY_EXT_TOKEN", "MYEXT_SETTING"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(name, "changed")

			if probeCacheKey(binary) != key {
				t.Fatalf("a change of %s changed the key", name)
			}

			if loadProbeCache(key, binary) != nil {
				t.Errorf("a change of %s hit the cache", name)
			}
		})
	}

	if loadProbeCache(key, binary) == nil {
		t.Error("the entry is not used once the environment is back")
	}
}

// TestProbeCache_WrapperEnv checks that a wrapper's entry depends on the
// variables the wrapper names, but for those a shell sets anew for each
// command (volatileEnv): "_" is in every binary wrapper's symbols, and
// is the program that ran maestro (make, time, an IDE, ...).
func TestProbeCache_WrapperEnv(t *testing.T) {
	t.Setenv("MAESTRO_CACHE_DIR", t.TempDir())
	t.Setenv("_", "/usr/bin/make")
	t.Setenv("SHLVL", "1")

	dir := t.TempDir()
	wrapper := filepath.Join(dir, "php")
	real := filepath.Join(dir, ".php-wrapped")
	writeFile(t, wrapper, "\x7fELF\x00setenv\x00PROBE_WRAPPER_VARIABLE\x00_\x00PWD\x00OLDPWD\x00SHLVL\x00MAESTRO_X\x00")
	writeFile(t, real, "\x7fELF php")

	t.Chdir(dir)

	key := storeFake(t, wrapper, nil, real)

	t.Setenv("PROBE_UNRELATED", "1")
	t.Setenv("_", "/usr/bin/time")
	t.Setenv("SHLVL", "2")
	t.Setenv("OLDPWD", "/elsewhere")
	t.Setenv("MAESTRO_X", "1")

	if loadProbeCache(key, wrapper) == nil {
		t.Error("an unrelated or volatile variable missed a wrapper's entry")
	}

	t.Setenv("PROBE_WRAPPER_VARIABLE", "1")

	if loadProbeCache(key, wrapper) != nil {
		t.Error("a change of a variable the wrapper names hit the cache")
	}
}

// TestProbeCache_Racy checks that a result is not kept when a file it
// depends on changed too close to the probe.
func TestProbeCache_Racy(t *testing.T) {
	t.Setenv("MAESTRO_CACHE_DIR", t.TempDir())

	dir := t.TempDir()
	binary := filepath.Join(dir, "php")
	lib := filepath.Join(dir, "libphp.so")
	writeFile(t, binary, "\x7fELF php")
	writeFile(t, lib, "library")

	key := probeCacheKey(binary)
	output := fakeProbeOutput(t, "8.4.25", binary, lib)

	s, err := ParseSnapshot(binary, output)
	if err != nil {
		t.Fatal(err)
	}

	// the library is replaced as the probe starts
	storeProbeCache(key, binary, s, output, time.Now())

	if loadProbeCache(key, binary) != nil {
		t.Error("a result was kept although its library changed during the probe")
	}

	// php.ini disappears during the probe: its directory changed
	writeFile(t, filepath.Join(dir, "php.ini"), "")

	old := time.Now().Add(-time.Hour)
	for _, p := range []string{binary, lib} {
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}

	start := time.Now().Add(fsstate.DefaultMargin - time.Second)

	if err := os.Remove(filepath.Join(dir, "php.ini")); err != nil {
		t.Fatal(err)
	}

	storeProbeCache(key, binary, s, output, start)

	if loadProbeCache(key, binary) != nil {
		t.Error("a result was kept although an ini file was removed during the probe")
	}
}

// TestProbeCache_Prune checks that storing an entry bounds the cache.
func TestProbeCache_Prune(t *testing.T) {
	t.Setenv("MAESTRO_CACHE_DIR", t.TempDir())

	dir := probeCacheDir()
	if err := os.MkdirAll(filepath.Join(dir, "ab"), 0o755); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	files := map[string]time.Duration{
		"ab/legacy":              time.Minute,
		"entry1":                 5 * time.Hour,
		"entry2":                 4 * time.Hour,
		"entry3":                 3 * time.Hour,
		"entry4":                 2 * time.Hour,
		"expired":                25 * time.Hour,
		fsstate.TempPrefix + "1": time.Hour,
		fsstate.TempPrefix + "2": time.Second,
	}

	for name, age := range files {
		path := filepath.Join(dir, name)
		writeFile(t, path, "")

		if err := os.Chtimes(path, now.Add(-age), now.Add(-age)); err != nil {
			t.Fatal(err)
		}
	}

	names := func() []string {
		t.Helper()
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}

		return names
	}

	// a store prunes what is too old (the bound is far)
	binary := filepath.Join(t.TempDir(), "php")
	writeFile(t, binary, "\x7fELF php")
	key := storeFake(t, binary, nil, binary)
	want := []string{fsstate.TempPrefix + "2", key, "entry1", "entry2", "entry3", "entry4"}
	slices.Sort(want)
	if got := names(); !slices.Equal(got, want) {
		t.Errorf("left %q, want %q", got, want)
	}

	// and the least recently used beyond the bound
	pruneProbeCache(dir, 3)
	want = []string{fsstate.TempPrefix + "2", key, "entry3", "entry4"}
	slices.Sort(want)
	if got := names(); !slices.Equal(got, want) {
		t.Errorf("left %q, want %q", got, want)
	}

	// using an entry makes it recent
	path := probeCachePath(key)
	if err := os.Chtimes(path, now.Add(-2*time.Hour), now.Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}

	if loadProbeCache(key, binary) == nil {
		t.Fatal("the entry is not used")
	}

	if info, err := os.Stat(path); err != nil || time.Since(info.ModTime()) > time.Minute {
		t.Error("using an entry did not make it recent")
	}
}

// TestProbeCache_RealPHP probes the php on the PATH through the cache:
// the second detection is the cached result of the first.
func TestProbeCache_RealPHP(t *testing.T) {
	if !switches.On(switches.PHPTests) {
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

	storeProbeCache(key, binary, s, output, trustedStart())

	if loadProbeCache(key, binary) != nil {
		t.Error("a result whose mapped files are unknown was cached")
	}
}

// TestProbeCache_BinaryIdentical checks that the cache's binary form of
// the real probe's result reads back as the snapshot its JSON gives.
func TestProbeCache_BinaryIdentical(t *testing.T) {
	if !switches.On(switches.PHPTests) {
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
