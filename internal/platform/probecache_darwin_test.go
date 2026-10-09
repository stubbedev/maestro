//go:build darwin

package platform

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// darwinOutput is the probe result of a php at binary that loaded
// extensionDir's extensions.
func darwinOutput(t *testing.T, binary, extensionDir string, extra map[string]any) []byte {
	t.Helper()

	constants := map[string]any{"PHP_VERSION": "8.4.25", "PHP_VERSION_ID": 80425, "PHP_BINARY": binary}
	if extensionDir != "" {
		constants["PHP_EXTENSION_DIR"] = extensionDir
	}

	fields := map[string]any{"constants": constants}
	for k, v := range extra {
		fields[k] = v
	}

	return fakeProbeOutputWith(t, fields, "8.4.25")
}

// TestProbeCache_Darwin checks that a probe's result is kept while the
// files its load commands walked are unchanged, and dropped when any of
// them changes: php's own binary, a dylib it loads, or one of that
// dylib's own.
func TestProbeCache_Darwin(t *testing.T) {
	t.Setenv("MAESTRO_CACHE_DIR", t.TempDir())

	dir := t.TempDir()
	dep := filepath.Join(dir, "libdep.dylib")
	writeMachO(t, dep, []machoLoad{{testCmdDylib, "@loader_path/libnested.dylib"}}, nil)
	writeMachO(t, filepath.Join(dir, "libnested.dylib"), nil, nil)

	binary := filepath.Join(dir, "php")
	writeMachO(t, binary, []machoLoad{{testCmdDylib, "@loader_path/libdep.dylib"}}, nil)

	key := probeCacheKey(binary)
	if key == "" {
		t.Fatal("no cache key")
	}

	output := darwinOutput(t, binary, "", nil)

	s, err := ParseSnapshot(binary, output)
	if err != nil {
		t.Fatal(err)
	}

	storeProbeCache(key, binary, s, output, trustedStart())

	if loadProbeCache(key, binary) == nil {
		t.Fatal("the probe was not cached")
	}

	// every change keeps what the files load, so each subtest starts from
	// an entry watching them all, whatever order they run in
	for name, change := range map[string]func(t *testing.T){
		"the binary": func(t *testing.T) {
			writeMachO(t, binary, []machoLoad{{testCmdDylib, "@loader_path/libdep.dylib"}}, []string{"@loader_path"})
		},
		"a dylib": func(t *testing.T) {
			writeMachO(t, dep, []machoLoad{{testCmdDylib, "@loader_path/libnested.dylib"}}, []string{"@loader_path"})
		},
		"a nested dylib": func(t *testing.T) {
			writeMachO(t, filepath.Join(dir, "libnested.dylib"), []machoLoad{{lcLoadWeakDylib, "@loader_path/gone.dylib"}}, []string{"@loader_path"})
		},
	} {
		t.Run(name, func(t *testing.T) {
			time.Sleep(10 * time.Millisecond)
			change(t)

			if loadProbeCache(key, binary) != nil {
				t.Error("the cache survived the change")
			}

			// and what replaced it is cached in turn
			storeProbeCache(key, binary, s, output, trustedStart())

			if loadProbeCache(key, binary) == nil {
				t.Error("the probe after the change was not cached")
			}
		})
	}
}

// TestProbeCache_DarwinWrapper checks that a php another executable
// started is keyed on what that executable may read and the working
// directory, and on the php that answered.
func TestProbeCache_DarwinWrapper(t *testing.T) {
	t.Setenv("MAESTRO_CACHE_DIR", t.TempDir())

	dir := t.TempDir()
	php := filepath.Join(dir, "php-real")
	writeMachO(t, php, []machoLoad{{testCmdDylib, "@loader_path/libdep.dylib"}}, nil)
	writeMachO(t, filepath.Join(dir, "libdep.dylib"), nil, nil)

	// the wrapper names a variable in its bytes, as a real one reads one
	wrapper := filepath.Join(dir, "php")
	writeMachO(t, wrapper, nil, []string{"WRAPSEL_VERSION"})

	key := probeCacheKey(wrapper)
	output := darwinOutput(t, php, "", nil)

	s, err := ParseSnapshot(wrapper, output)
	if err != nil {
		t.Fatal(err)
	}

	storeProbeCache(key, wrapper, s, output, trustedStart())

	if loadProbeCache(key, wrapper) == nil {
		t.Fatal("the php a wrapper started was not cached")
	}

	// the variable the wrapper reads chooses the php again
	t.Setenv("WRAPSEL_VERSION", "8.3")

	if loadProbeCache(key, wrapper) != nil {
		t.Error("the cache survived the variable the wrapper reads")
	}

	t.Setenv("WRAPSEL_VERSION", "")

	// a wrapper may choose its php by the working directory
	other := t.TempDir()
	restore, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(other); err != nil {
		t.Fatal(err)
	}

	if loadProbeCache(key, wrapper) != nil {
		t.Error("the cache survived the working directory")
	}

	if err := os.Chdir(restore); err != nil {
		t.Fatal(err)
	}

	// what the php that answered loads is the entry's
	time.Sleep(10 * time.Millisecond)
	writeMachO(t, filepath.Join(dir, "libdep.dylib"), nil, nil)

	if loadProbeCache(key, wrapper) != nil {
		t.Error("the cache survived the php's dylib changing")
	}
}

// TestProbeCache_DarwinExtensions checks that the extension files php
// loaded - the ini's, and the loaded names in the extension directory -
// and what they load are part of the entry.
func TestProbeCache_DarwinExtensions(t *testing.T) {
	t.Setenv("MAESTRO_CACHE_DIR", t.TempDir())

	dir := t.TempDir()
	extDir := filepath.Join(dir, "ext")
	if err := os.Mkdir(extDir, 0o755); err != nil {
		t.Fatal(err)
	}

	curl := filepath.Join(extDir, "curl.so")
	writeMachO(t, curl, []machoLoad{{testCmdDylib, "@loader_path/libssl.dylib"}}, nil)
	writeMachO(t, filepath.Join(extDir, "libssl.dylib"), nil, nil)

	ini := filepath.Join(dir, "php.ini")
	writeFile(t, ini, "extension = curl.so\nzend_extension = \"opcache.so\"\n")

	binary := filepath.Join(dir, "php")
	writeMachO(t, binary, nil, nil)

	key := probeCacheKey(binary)
	output := darwinOutput(t, binary, extDir, map[string]any{
		"ini_files":       []any{ini, false},
		"extensions":      [][]any{{"curl", "8.4", ""}},
		"zend_extensions": []string{"opcache"},
	})

	s, err := ParseSnapshot(binary, output)
	if err != nil {
		t.Fatal(err)
	}

	storeProbeCache(key, binary, s, output, trustedStart())

	if loadProbeCache(key, binary) == nil {
		t.Fatal("the probe was not cached")
	}

	// opcache.so is not there: it was not loaded from it, and its coming
	// is the directory's change, not the entry's
	time.Sleep(10 * time.Millisecond)
	writeMachO(t, filepath.Join(extDir, "libssl.dylib"), nil, nil)

	if loadProbeCache(key, binary) != nil {
		t.Error("the cache survived an extension's library changing")
	}
}

// TestProbeCache_DarwinNoMachO checks that a php that cannot be read as
// a Mach-O file, whose loads cannot be told, is not cached.
func TestProbeCache_DarwinNoMachO(t *testing.T) {
	t.Setenv("MAESTRO_CACHE_DIR", t.TempDir())

	dir := t.TempDir()
	binary := filepath.Join(dir, "php")
	writeFile(t, binary, "a php of no known format")

	key := probeCacheKey(binary)
	output := darwinOutput(t, binary, "", nil)

	s, err := ParseSnapshot(binary, output)
	if err != nil {
		t.Fatal(err)
	}

	storeProbeCache(key, binary, s, output, trustedStart())

	if loadProbeCache(key, binary) != nil {
		t.Error("a php whose loads cannot be told was cached")
	}
}
