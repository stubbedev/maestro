package cache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
)

// setUp is CacheTest::setUp: four 1000 byte files in a fresh cache.
func setUp(t *testing.T) (*Cache, string, []string) {
	t.Helper()

	root := t.TempDir()
	zeros := strings.Repeat("0", 1000)

	files := make([]string, 4)
	for i := range files {
		files[i] = filepath.Join(root, "cached.file"+string(rune('0'+i))+".zip")
		if err := os.WriteFile(files[i], []byte(zeros), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	c, err := New(io.NewNullIO(), root, "", nil, false)
	if err != nil {
		t.Fatal(err)
	}

	return c, root, files
}

func assertFileExists(t *testing.T, path string, exists bool) {
	t.Helper()

	_, err := os.Stat(path)
	if (err == nil) != exists {
		t.Fatalf("%s exists: %v, want %v", path, err == nil, exists)
	}
}

func TestCache_RemoveOutdatedFiles(t *testing.T) {
	c, _, files := setUp(t)

	old := time.Now().Add(-time.Hour)
	for _, f := range files[1:] {
		if err := os.Chtimes(f, old, old); err != nil {
			t.Fatal(err)
		}
	}

	if ok, err := c.Gc(600, 1024*1024*1024); !ok || err != nil {
		t.Fatalf("got %v %v", ok, err)
	}

	for _, f := range files[1:] {
		assertFileExists(t, f, false)
	}

	assertFileExists(t, files[0], true)
}

func TestCache_RemoveFilesWhenCacheIsTooLarge(t *testing.T) {
	c, _, files := setUp(t)

	// accessed in order 0, 1, 2, 3; none outdated
	now := time.Now()
	for i, f := range files {
		at := now.Add(time.Duration(i-10) * time.Minute)
		if err := os.Chtimes(f, at, now); err != nil {
			t.Fatal(err)
		}
	}

	if ok, err := c.Gc(600, 1500); !ok || err != nil {
		t.Fatalf("got %v %v", ok, err)
	}

	for _, f := range files[:3] {
		assertFileExists(t, f, false)
	}

	assertFileExists(t, files[3], true)
}

func TestCache_ClearCache(t *testing.T) {
	c, root, files := setUp(t)

	c2, err := New(io.NewNullIO(), root, "a-z0-9.", util.NewFilesystem(nil), false)
	if err != nil {
		t.Fatal(err)
	}

	if ok, err := c2.Clear(); !ok || err != nil {
		t.Fatalf("got %v %v", ok, err)
	}

	assertFileExists(t, files[0], false)
	assertFileExists(t, root, true)
	_ = c
}

func TestCache_ReadWrite(t *testing.T) {
	b, err := io.NewBufferIO("", console.VerbosityDebug, nil)
	if err != nil {
		t.Fatal(err)
	}

	root := filepath.Join(t.TempDir(), "cache", "repo")

	c, err := New(b, root+"/", "", nil, false)
	if err != nil {
		t.Fatal(err)
	}

	if c.Root() != root+"/" {
		t.Fatal(c.Root())
	}

	if ok, err := c.Write("provider-vendor/Package$name.json", "data"); !ok || err != nil {
		t.Fatalf("write: %v %v", ok, err)
	}

	assertFileExists(t, filepath.Join(root, "provider-vendor-Package-name.json"), true)

	if data, ok, err := c.Read("provider-vendor/Package$name.json"); !ok || err != nil || data != "data" {
		t.Fatalf("read: %q %v %v", data, ok, err)
	}

	if _, ok, _ := c.Read("missing"); ok {
		t.Fatal("expected a miss")
	}

	if sum, ok, _ := c.Sha256("provider-vendor/Package$name.json"); !ok || sum != "3a6eb0790f39ac87c94f3856b2dd2c5d110e6811602261a9a923d3bb23adc8b7" {
		t.Fatalf("sha256 %s", sum)
	}

	if sum, ok, _ := c.Sha1("provider-vendor/Package$name.json"); !ok || sum != "a17c9aaa61e80a1bf71d0d850af4e5baa9800bbd" {
		t.Fatalf("sha1 %s", sum)
	}

	if age, ok := c.Age("provider-vendor/Package$name.json"); !ok || age > 5 {
		t.Fatalf("age %d", age)
	}

	target := filepath.Join(t.TempDir(), "copy")
	if ok, err := c.CopyTo("provider-vendor/Package$name.json", target); !ok || err != nil {
		t.Fatalf("copyTo %v %v", ok, err)
	}

	if ok, err := c.CopyFrom("sub/dir/file", target); !ok || err != nil {
		t.Fatalf("copyFrom %v %v", ok, err)
	}

	assertFileExists(t, filepath.Join(root, "sub-dir-file"), true)

	if ok, err := c.Remove("sub/dir/file"); !ok || err != nil {
		t.Fatalf("remove %v %v", ok, err)
	}

	want := "Writing " + root + "/provider-vendor-Package-name.json into cache\n" +
		"Reading " + root + "/provider-vendor-Package-name.json from cache\n"
	if !strings.HasPrefix(php.NormalizeEOL(b.Output()), want) {
		t.Fatalf("output %q", b.Output())
	}

	c.SetReadOnly(true)

	if ok, _ := c.Write("x", "y"); ok || !c.IsReadOnly() {
		t.Fatal("expected read-only writes to be skipped")
	}
}

func TestCache_IsUsable(t *testing.T) {
	for path, usable := range map[string]bool{
		"/dev/null":        false,
		"/dev/null/":       false,
		"nul":              false,
		`C:\foo\NUL`:       false,
		"$null":            false,
		"/home/me/.cache":  true,
		"/home/me/nullify": true,
	} {
		if IsUsable(path) != usable {
			t.Errorf("%s: expected %v", path, usable)
		}
	}

	c, err := New(io.NewNullIO(), "/dev/null", "", nil, false)
	if err != nil || c.IsEnabled() {
		t.Fatal("expected the null device cache disabled")
	}
}

func TestCache_NotWritable(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root can write anywhere")
	}
	if runtime.GOOS == "windows" {
		t.Skip("a directory's mode does not stop writes into it on Windows")
	}

	b, _ := io.NewBufferIO("", 0, nil)
	dir := t.TempDir()

	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	c, _ := New(b, filepath.Join(dir, "sub"), "", nil, false)
	if c.IsEnabled() {
		t.Fatal("expected the cache disabled")
	}

	if !strings.Contains(b.Output(), "Cannot create cache directory "+filepath.Join(dir, "sub")+"/, or directory is not writable. Proceeding without cache.") {
		t.Fatalf("output %q", b.Output())
	}
}

func TestCache_GcVcsCache(t *testing.T) {
	root := t.TempDir()
	old := time.Now().Add(-2 * time.Hour)

	for _, d := range []string{"old-repo", "new-repo", ".hidden"} {
		if err := os.MkdirAll(filepath.Join(root, d, "objects"), 0o700); err != nil {
			t.Fatal(err)
		}
	}

	for _, d := range []string{"old-repo", ".hidden"} {
		if err := os.Chtimes(filepath.Join(root, d), old, old); err != nil {
			t.Fatal(err)
		}
	}

	c, _ := New(io.NewNullIO(), root, "", nil, false)

	if ok, err := c.GcVcsCache(3600); !ok || err != nil {
		t.Fatalf("got %v %v", ok, err)
	}

	assertFileExists(t, filepath.Join(root, "old-repo"), false)
	assertFileExists(t, filepath.Join(root, "new-repo"), true)
	assertFileExists(t, filepath.Join(root, ".hidden"), true)
}

func TestOracle_CacheKeys(t *testing.T) {
	data, err := os.ReadFile("testdata/oracle/cache.json")
	if err != nil {
		t.Fatal(err)
	}

	var golden struct {
		CacheKeys []struct {
			Allowlist string `json:"allowlist"`
			File      string `json:"file"`
			Key       string `json:"key"`
		} `json:"cachekeys"`
	}

	if err := json.Unmarshal(data, &golden); err != nil {
		t.Fatal(err)
	}

	for _, c := range golden.CacheKeys {
		cache, err := New(io.NewNullIO(), t.TempDir(), c.Allowlist, nil, false)
		if err != nil {
			t.Fatal(err)
		}

		if got := cache.key(c.File); got != c.Key {
			t.Errorf("%s / %q: got %q, want %q", c.Allowlist, c.File, got, c.Key)
		}
	}
}
