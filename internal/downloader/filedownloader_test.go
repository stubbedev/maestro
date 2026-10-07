package downloader

import (
	"crypto/sha1"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stubbedev/maestro/internal/eventdispatcher"
	mio "github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/http"
)

// Ports tests/Composer/Test/Downloader/FileDownloaderTest.php.

func newTestFileDownloader(t *testing.T, ioi mio.IO, config Config, events EventDispatcher, cache Cache, h HTTPDownloader) *FileDownloader {
	t.Helper()

	if ioi == nil {
		ioi = nullIO()
	}

	if config == nil {
		config = getConfig(t)
	}

	if h == nil {
		h = &fakeHTTP{}
	}

	deps := Deps{IO: ioi, Config: config, HTTPDownloader: h, EventDispatcher: events, Process: asyncProcess()}
	if cache != nil {
		deps.Cache = cache
	}

	d, err := NewFileDownloader(deps)
	if err != nil {
		t.Fatal(err)
	}

	d.retryDelay, d.nextURLDelay = 0, 0

	return d
}

func TestFileDownloader_DownloadForPackageWithoutDistReference(t *testing.T) {
	d := newTestFileDownloader(t, nil, nil, nil, nil, nil)

	_, err := d.Download(dummyPackage(), "/path", nil)
	if !phperr.InstanceOf(err, "InvalidArgumentException") {
		t.Fatalf("expected InvalidArgumentException, got %v", err)
	}
}

func TestFileDownloader_DownloadToExistingFile(t *testing.T) {
	p := dummyPackage()
	p.SetDistURL(pkg.Str("url"))

	path := filepath.Join(t.TempDir(), "file")
	writeFile(t, path, nil, 0o644)

	d := newTestFileDownloader(t, nil, nil, nil, nil, nil)

	_, err := d.Download(p, path, nil)
	if !phperr.InstanceOf(err, "RuntimeException") {
		t.Fatalf("expected RuntimeException, got %v", err)
	}

	mustContain(t, err, "exists and is not a directory")
}

func TestFileDownloader_InstallDoesNotChmodBinOutsideOfPackage(t *testing.T) {
	rootDir := t.TempDir()
	vendorDir := rootDir + "/vendor"
	path := vendorDir + "/attacker/pkg"

	p := dummyPackage()
	p.SetDistURL(pkg.Str("http://example.com/script.js"))
	// a ".." bin which never went through
	// ValidatingArrayLoader::validatePackage(), as is the case for composer
	// reinstall which builds its operations straight from installed.json
	p.SetBinaries(php.ListOf("../../../victim.sh"))

	victim := rootDir + "/victim.sh"
	writeFile(t, victim, []byte("#!/bin/sh\necho pwned\n"), 0o600)

	before, err := os.Stat(victim)
	if err != nil {
		t.Fatal(err)
	}

	d := newTestFileDownloader(t, nil, getConfig(t, "vendor-dir", vendorDir), nil, nil, nil)

	// seed the downloaded file where install() expects to find it
	tmpFile := d.ownFileName(p)
	if err := os.MkdirAll(filepath.Dir(tmpFile), 0o777); err != nil {
		t.Fatal(err)
	}

	writeFile(t, tmpFile, []byte("downloaded"), 0o644)

	if err := await(d.install(call{d.io, false}, p, path)); err != nil {
		t.Fatal(err)
	}

	if !php.FileExists(path + "/script.js") {
		t.Fatal("script.js was not installed")
	}

	after, err := os.Stat(victim)
	if err != nil {
		t.Fatal(err)
	}

	if before.Mode() != after.Mode() {
		t.Fatal("A bin escaping the package dir must not be chmod'd")
	}
}

func TestFileDownloader_InstallChmodsBinaries(t *testing.T) {
	if util.IsWindows() {
		t.Skip("Windows has no execute bit for chmod() to set")
	}

	vendorDir := t.TempDir() + "/vendor"
	path := vendorDir + "/a/b"

	p := dummyPackage()
	p.SetDistURL(pkg.Str("http://example.com/tool"))
	p.SetBinaries(php.ListOf("tool"))

	d := newTestFileDownloader(t, nil, getConfig(t, "vendor-dir", vendorDir), nil, nil, nil)

	tmpFile := d.ownFileName(p)
	if err := os.MkdirAll(filepath.Dir(tmpFile), 0o777); err != nil {
		t.Fatal(err)
	}

	writeFile(t, tmpFile, []byte("#!/bin/sh\n"), 0o644)

	if err := await(d.Install(p, path)); err != nil {
		t.Fatal(err)
	}

	if !util.IsExecutable(path + "/tool") {
		t.Fatal("the binary was not made executable")
	}
}

func TestFileDownloader_GetFileName(t *testing.T) {
	p := dummyPackage()
	p.SetDistURL(pkg.Str("http://example.com/script.js"))

	d := newTestFileDownloader(t, nil, getConfig(t, "vendor-dir", "/vendor"), nil, nil, nil)

	if got := d.ownFileName(p); !regexp.MustCompile(`/vendor/composer/tmp-[a-z0-9]+\.js`).MatchString(got) {
		t.Fatalf("unexpected file name %q", got)
	}
}

func TestFileDownloader_DownloadButFileIsUnsaved(t *testing.T) {
	p := dummyPackage()
	p.SetDistURL(pkg.Str("http://example.com/script.js"))

	path := t.TempDir()
	d := newTestFileDownloader(t, nil, getConfig(t, "vendor-dir", path+"/vendor"), nil, nil, nil)

	err := await(d.Download(p, path, nil))
	if !phperr.InstanceOf(err, "UnexpectedValueException") {
		t.Fatalf("expected UnexpectedValueException, got %v", err)
	}

	mustContain(t, err, "could not be saved to")
}

func cacheKeyOf(name, key string) string {
	sum := sha1.Sum([]byte(key))

	return name + "/" + hex.EncodeToString(sum[:]) + "."
}

func TestFileDownloader_DownloadWithCustomProcessedUrl(t *testing.T) {
	path := t.TempDir()

	p := dummyPackage()
	p.SetDistURL(pkg.Str("url"))

	config := getConfig(t, "vendor-dir", path+"/vendor", "bin-dir", path+"/vendor/bin")

	expectedURL := "foobar"
	expectedCacheKey := cacheKeyOf("dummy/pkg", expectedURL)

	dispatcher := &fakeDispatcher{listener: func(e eventdispatcher.Event) error {
		if pre, ok := e.(*eventdispatcher.PreFileDownloadEvent); ok {
			pre.SetProcessedURL(expectedURL)
		}

		return nil
	}}

	cache := &fakeCache{t: t}
	cache.copyTo = func(key, _ string) bool {
		if key != expectedCacheKey {
			t.Errorf("Failed assertion on $cacheKey argument of Cache::copyTo method: %q", key)
		}

		return false
	}
	cache.copyFrom = func(key, _ string) bool {
		if key != expectedCacheKey {
			t.Errorf("Failed assertion on $cacheKey argument of Cache::copyFrom method: %q", key)
		}

		return false
	}

	h := &fakeHTTP{copy: func(url, _ string) (*http.Response, error) {
		if url != expectedURL {
			t.Errorf("Failed assertion on $url argument of HttpDownloader::addCopy method: %q", url)
		}

		return http.NewResponse("http://example.org/", 200, nil, "file~"), nil
	}}

	d := newTestFileDownloader(t, nil, config, dispatcher, cache, h)

	err := await(d.Download(p, path, nil))
	if !phperr.InstanceOf(err, "UnexpectedValueException") {
		t.Fatalf("expected UnexpectedValueException, got %v", err)
	}

	mustContain(t, err, "could not be saved to")
}

func TestFileDownloader_DownloadWithCustomCacheKey(t *testing.T) {
	path := t.TempDir()

	p := dummyPackage()
	p.SetDistURL(pkg.Str("url"))

	config := getConfig(t, "vendor-dir", path+"/vendor", "bin-dir", path+"/vendor/bin")

	expectedURL := "url"
	customCacheKey := "xyzzy"
	expectedCacheKey := cacheKeyOf("dummy/pkg", customCacheKey)

	dispatcher := &fakeDispatcher{listener: func(e eventdispatcher.Event) error {
		if pre, ok := e.(*eventdispatcher.PreFileDownloadEvent); ok {
			pre.SetCustomCacheKey(pkg.Str(customCacheKey))
		}

		return nil
	}}

	cache := &fakeCache{t: t}
	cache.copyTo = func(key, _ string) bool {
		if key != expectedCacheKey {
			t.Errorf("Failed assertion on $cacheKey argument of Cache::copyTo method: %q", key)
		}

		return false
	}
	cache.copyFrom = func(key, _ string) bool {
		if key != expectedCacheKey {
			t.Errorf("Failed assertion on $cacheKey argument of Cache::copyFrom method: %q", key)
		}

		return false
	}

	h := &fakeHTTP{copy: func(url, _ string) (*http.Response, error) {
		if url != expectedURL {
			t.Errorf("Failed assertion on $url argument of HttpDownloader::addCopy method: %q", url)
		}

		return http.NewResponse("http://example.org/", 200, nil, "file~"), nil
	}}

	d := newTestFileDownloader(t, nil, config, dispatcher, cache, h)

	err := await(d.Download(p, path, nil))
	if !phperr.InstanceOf(err, "UnexpectedValueException") {
		t.Fatalf("expected UnexpectedValueException, got %v", err)
	}

	mustContain(t, err, "could not be saved to")
}

func TestFileDownloader_CacheGarbageCollectionIsCalled(t *testing.T) {
	expectedTTL := 99999999
	config := getConfig(t, "cache-files-ttl", "99999999", "cache-files-maxsize", "500M")

	calls := 0
	cache := &fakeCache{t: t, gcNeeded: true, gc: func(ttl int, _ int64) {
		calls++

		if ttl != expectedTTL {
			t.Errorf("gc called with ttl %d", ttl)
		}
	}}

	newTestFileDownloader(t, nil, config, nil, cache, nil)

	if calls != 1 {
		t.Fatalf("gc called %d times", calls)
	}
}

func TestFileDownloader_DownloadFileWithInvalidChecksum(t *testing.T) {
	p := dummyPackage()
	p.SetDistURL(pkg.Str("http://example.com/script.js"))
	p.SetDistSha1Checksum(pkg.Str("invalid"))

	path := t.TempDir()
	d := newTestFileDownloader(t, nil, getConfig(t, "vendor-dir", path+"/vendor"), nil, nil, nil)

	// make sure the file expected to be downloaded is on disk already
	dlFile := d.ownFileName(p)
	if err := os.MkdirAll(filepath.Dir(dlFile), 0o777); err != nil {
		t.Fatal(err)
	}

	writeFile(t, dlFile, nil, 0o644)

	err := await(d.Download(p, path, nil))
	if !phperr.InstanceOf(err, "UnexpectedValueException") {
		t.Fatalf("expected UnexpectedValueException, got %v", err)
	}

	mustContain(t, err, "checksum verification")
}

func TestFileDownloader_DowngradeShowsAppropriateMessage(t *testing.T) {
	oldPackage := getPackage("dummy/pkg", "1.2.0.0", "1.2.0")
	newPackage := getPackage("dummy/pkg", "1.0.0.0", "1.0.0")
	newPackage.SetDistURL(pkg.Str("http://example.com/script.js"))

	out := bufferIO(t, 0)

	// Composer mocks the Filesystem; here the package path lies beside
	// vendor/ so removing it keeps the downloaded file.
	root := t.TempDir()
	path := root + "/pkg"
	d := newTestFileDownloader(t, out, getConfig(t, "vendor-dir", root+"/vendor"), nil, nil, nil)

	// make sure the file expected to be downloaded is on disk already
	dlFile := d.ownFileName(newPackage)
	if err := os.MkdirAll(filepath.Dir(dlFile), 0o777); err != nil {
		t.Fatal(err)
	}

	writeFile(t, dlFile, nil, 0o644)

	if err := await(d.Download(newPackage, path, oldPackage)); err != nil {
		t.Fatal(err)
	}

	if err := await(d.Update(oldPackage, newPackage, path)); err != nil {
		t.Fatal(err)
	}

	lines := outputLines(out)
	if len(lines) != 2 || !regexp.MustCompile(`Downloading .*`).MatchString(lines[0]) || !regexp.MustCompile(`Downgrading .*`).MatchString(lines[1]) {
		t.Fatalf("unexpected output %q", lines)
	}
}
