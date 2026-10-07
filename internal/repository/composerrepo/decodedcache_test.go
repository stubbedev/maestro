package composerrepo

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stubbedev/maestro/internal/cache"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/switches"
)

// p2Files are the p2 fixtures and, to check the decoded cache on more real
// files, Packagist files found in Composer's cache directory and in the
// directories MAESTRO_P2_DIRS lists (separated by the path list
// separator): file name to JSON.
func p2Files(t *testing.T) map[string]string {
	t.Helper()
	files := map[string]string{}
	paths, _ := filepath.Glob(p2Fixtures + "/*.json.gz")
	for _, p := range paths {
		files[p] = readGzip(t, p)
	}
	if len(files) == 0 {
		t.Fatal("no p2 fixtures")
	}

	var dirs []string
	if dir := os.Getenv("COMPOSER_CACHE_DIR"); dir != "" {
		dirs = append(dirs, filepath.Join(dir, "repo", "https---repo.packagist.org"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".cache", "composer", "repo", "https---repo.packagist.org"))
	}
	dirs = append(dirs, filepath.SplitList(os.Getenv(switches.P2Dirs))...)
	for _, dir := range dirs {
		paths, _ := filepath.Glob(filepath.Join(dir, "provider-*.json"))
		for _, p := range paths {
			if b, err := os.ReadFile(p); err == nil {
				files[p] = string(b)
			}
		}
	}
	t.Logf("%d files", len(files))

	return files
}

// withDecodedCache uses a decoded cache under a temporary root and
// returns the directory of its slots.
func withDecodedCache(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	UseDecodedCache(root)
	t.Cleanup(func() { UseDecodedCache("") })

	return filepath.Join(root, decodedVersion)
}

// The decoded cache gives what decoding the JSON gives, arrays' internal
// state included, when it stores a file and when it reads it back.
func TestDecodedCacheEqualsDecoding(t *testing.T) {
	dir := withDecodedCache(t)
	r := newRepo(t, php.ArrayOf("url", "https://example.org"), createConfig(t), mock(t))

	stored := map[string]bool{} // the slots written
	for path, json := range p2Files(t) {
		key := "provider-" + filepath.Base(path)
		want := decodeArray(json)
		if want == nil {
			t.Fatalf("%s: not an array", path)
		}
		for _, pass := range []string{"stored", "read back"} {
			got, store := r.decodeCached(key, json)
			if !reflect.DeepEqual(got.array(), want) {
				t.Fatalf("%s (%s): differs from decoding the JSON", path, pass)
			}
			checkP2File(t, path+" ("+pass+")", got, want)
			if store != nil {
				store()
			}
		}
		if len(json) >= decodedMinSize {
			stored[key] = true
		}
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != len(stored) {
		t.Errorf("%d decoded files for %d files", len(entries), len(stored))
	}
}

// A slot holding another JSON, or a damaged file, is not read back.
func TestDecodedCacheOnlyReadsItsJSON(t *testing.T) {
	dir := withDecodedCache(t)
	r := newRepo(t, php.ArrayOf("url", "https://example.org"), createConfig(t), mock(t))

	json := readGzip(t, p2Fixtures+"/monolog_monolog.json.gz")
	changed := strings.Replace(json, `"monolog/monolog"`, `"monolog/other"`, 1)
	if changed == json || len(json) < decodedMinSize {
		t.Fatal("fixture unsuitable")
	}
	if _, store := r.decodeCached("provider-monolog~monolog.json", json); store != nil {
		store()
	}
	if _, store := r.decodeCached("provider-monolog~monolog.json", json); store != nil {
		t.Fatal("stored file not read back")
	}
	if got, _ := r.decodeCached("provider-monolog~monolog.json", changed); !reflect.DeepEqual(got.array(), decodeArray(changed)) {
		t.Fatal("changed JSON: the old file was read back")
	}

	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("%d decoded files", len(entries))
	}
	path := filepath.Join(dir, entries[0].Name())
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, damaged := range [][]byte{data[:len(data)-1], append(append([]byte{}, data...), 0)} {
		if err := os.WriteFile(path, damaged, 0o600); err != nil {
			t.Fatal(err)
		}
		if got, _ := r.decodeCached("provider-monolog~monolog.json", json); !reflect.DeepEqual(got.array(), decodeArray(json)) {
			t.Fatal("damaged file: differs from decoding the JSON")
		}
	}
}

// clear-cache empties the decoded cache; --gc removes the files not
// written for the TTL, of any version.
func TestDecodedCacheClearAndGc(t *testing.T) {
	dir := withDecodedCache(t)
	root := filepath.Dir(dir)
	r := newRepo(t, php.ArrayOf("url", "https://example.org"), createConfig(t), mock(t))

	json := readGzip(t, p2Fixtures+"/monolog_monolog.json.gz")
	if _, store := r.decodeCached("provider-monolog~monolog.json", json); store != nil {
		store()
	}
	old := filepath.Join(root, "v0", "old.bin")
	if err := os.MkdirAll(filepath.Dir(old), 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(old, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}

	if err := cache.GcDecoded(root, 3600); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Errorf("expired file kept: %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("%d slots after gc, want 1", len(entries))
	}
	if _, store := r.decodeCached("provider-monolog~monolog.json", json); store != nil {
		t.Error("slot not read back after gc")
	}

	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Errorf("root kept: %v", err)
	}
	if err := cache.GcDecoded(root, 3600); err != nil {
		t.Errorf("gc without a cache: %v", err)
	}
	if _, store := r.decodeCached("provider-monolog~monolog.json", json); store == nil {
		t.Error("slot read back after clear")
	}
}

// checkP2File checks that what file gives of a metadata file is what
// reading data, the file decoded at once, gives: its top-level values,
// version lists, slim form and, for a file read back from its slot, its
// index (the versions scanned, and skeletons that complete to the
// packages the loader builds) and the expanded versions it keeps.
func checkP2File(t *testing.T, what string, file *p2File, data *php.Array) {
	t.Helper()
	for k := range data.All() {
		if k.String() != "packages" && !reflect.DeepEqual(file.at(k.String()), data.At(k.String())) {
			t.Fatalf("%s: %s differs", what, k.String())
		}
	}
	packages, _ := data.At("packages").(*php.Array)
	for name, versions := range packages.All() {
		if !reflect.DeepEqual(file.versions(name.String()), versions) || file.hasVersions(name.String()) != (versions != nil) {
			t.Fatalf("%s: the versions of %s differ", what, name.String())
		}
		idx := file.index(name.String())
		list, _ := versions.(*php.Array)
		if idx == nil {
			if _, expandable := expandable(list.Values()); file.slot != nil && name.IsString() && data.At("minified") == "composer/2.0" && list.IsAppended() && expandable {
				t.Fatalf("%s: %s: no index", what, name.String())
			}

			continue
		}
		scanned, exact, _ := scanVersions(list.Values(), true, p2Codec.parser, p2Codec.loader)
		if exact != idx.exact || len(scanned) != len(idx.scanned) {
			t.Fatalf("%s: %s: index of %d versions, exact %v; want %d, %v", what, name.String(), len(idx.scanned), idx.exact, len(scanned), exact)
		}
		i := -1
		_, _ = expandEach(list.Values(), func(v *php.Array, keep func() *php.Array) error {
			i++
			sv, want := idx.scanned[i], scanned[i]
			if sv.skip != want.skip || sv.normalized != want.normalized || sv.alias != want.alias || sv.exact != want.exact || !reflect.DeepEqual(sv.require, want.require) {
				t.Fatalf("%s: %s: version %d scanned as %+v, want %+v", what, name.String(), i, sv, want)
			}
			if expanded, ok := file.slot.expanded(name.String(), i); !ok || !php.StrictEquals(expanded, v) {
				t.Fatalf("%s: %s: version %d expands to another version", what, name.String(), i)
			}
			if skeleton := idx.skeleton(i); skeleton != nil {
				full := keep()
				eager, err := p2Codec.loader.Batch().Load(full)
				if err != nil {
					t.Fatalf("%s: %s: version %d: %v", what, name.String(), i, err)
				}
				lazy, err := p2Codec.loader.Batch().LoadSkeleton(skeleton, func() *php.Array { return full })
				if err != nil || !pkg.SkeletonMatches(lazy, eager) {
					t.Fatalf("%s: %s: version %d: the skeleton is not the package, %v", what, name.String(), i, err)
				}
			}

			return nil
		})
	}
	if !reflect.DeepEqual(file.slim(), slimFile(data)) {
		t.Fatalf("%s: the slim form differs", what)
	}
}
