package downloader

import (
	"archive/tar"
	"errors"
	nethttp "net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/stubbedev/maestro/internal/archive/archivetest"
	"github.com/stubbedev/maestro/internal/cache"
	"github.com/stubbedev/maestro/internal/config"
	"github.com/stubbedev/maestro/internal/console"
	mio "github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/store"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/http"
)

// Ports tests/Composer/Test/Downloader/ArchiveDownloaderTest.php,
// ZipDownloaderTest.php and XzDownloaderTest.php (the unzip/ZipArchive
// mechanics they mock are replaced by the native extraction; their
// observable assertions are kept), plus integration tests of the
// store-backed downloads against an HTTP server.

func TestArchiveDownloader_GetFileName(t *testing.T) {
	p := dummyPackage()
	p.SetDistURL(pkg.Str("http://example.com/script.js"))

	d, err := NewZipDownloader(Deps{IO: nullIO(), Config: getConfig(t, "vendor-dir", "/vendor"), HTTPDownloader: &fakeHTTP{}})
	if err != nil {
		t.Fatal(err)
	}

	first := d.fileName(p)
	if !regexp.MustCompile(`/vendor/composer/tmp-[a-z0-9]+\.js`).MatchString(first) {
		t.Fatalf("unexpected file name %q", first)
	}

	if second := d.fileName(p); second != first {
		t.Fatalf("file name changed: %q, %q", first, second)
	}

	if other := d.fileName(dummyPackage()); other == first {
		t.Fatal("another package object got the same file name")
	}
}

func newArchiveForURLs(t *testing.T) *ArchiveDownloader {
	t.Helper()

	d, err := NewZipDownloader(Deps{IO: nullIO(), Config: getConfig(t), HTTPDownloader: &fakeHTTP{}})
	if err != nil {
		t.Fatal(err)
	}

	return d
}

func TestArchiveDownloader_ProcessUrl(t *testing.T) {
	for _, expected := range []string{
		"https://github.com/composer/composer/zipball/master",
		"https://github.com/composer/composer/archive/master.tar.gz",
		"https://api.github.com/repos/composer/composer/zipball/master",
	} {
		if got, err := newArchiveForURLs(t).processURL(dummyPackage(), expected); err != nil || got != expected {
			t.Errorf("processUrl(%q) = %q", expected, got)
		}
	}
}

func TestArchiveDownloader_ProcessUrlRewriteDist(t *testing.T) {
	for _, url := range []string{
		"https://api.github.com/repos/composer/composer/zipball/master",
		"https://api.github.com/repos/composer/composer/tarball/master",
		"https://github.com/composer/composer/zipball/master",
		"https://www.github.com/composer/composer/tarball/master",
		"https://github.com/composer/composer/archive/master.zip",
		"https://github.com/composer/composer/archive/master.tar.gz",
	} {
		typ := "zip"
		if strings.Contains(url, "tar") {
			typ = "tar"
		}

		expected := "https://api.github.com/repos/composer/composer/" + typ + "ball/ref"

		p := dummyPackage()
		p.SetDistReference(pkg.Str("ref"))

		if got, err := newArchiveForURLs(t).processURL(p, url); err != nil || got != expected {
			t.Errorf("processUrl(%q) = %q, want %q", url, got, expected)
		}
	}
}

func TestArchiveDownloader_ProcessUrlRewriteBitbucketDist(t *testing.T) {
	for _, c := range [][2]string{
		{"https://bitbucket.org/davereid/drush-virtualhost/get/77ca490c26ac818e024d1138aa8bd3677d1ef21f", "zip"},
		{"https://bitbucket.org/davereid/drush-virtualhost/get/master", "tar.gz"},
		{"https://bitbucket.org/davereid/drush-virtualhost/get/v1.0", "tar.bz2"},
	} {
		url := c[0] + "." + c[1]
		expected := "https://bitbucket.org/davereid/drush-virtualhost/get/ref." + c[1]

		p := dummyPackage()
		p.SetDistReference(pkg.Str("ref"))

		if got, err := newArchiveForURLs(t).processURL(p, url); err != nil || got != expected {
			t.Errorf("processUrl(%q) = %q, want %q", url, got, expected)
		}
	}
}

// httpConfig gives the test configuration the HTTP layer's Config.
type httpConfig struct{ configAdapter }

func (c httpConfig) ProhibitURLByConfig(url string, ioi mio.IO, repoOptions *php.Array) error {
	return c.c.ProhibitURLByConfig(url, ioi, repoOptions)
}

func (httpConfig) ConfigSource() http.ConfigSource          { return nil }
func (httpConfig) AuthConfigSource() http.ConfigSource      { return nil }
func (httpConfig) LocalAuthConfigSource() http.ConfigSource { return nil }

// distServer serves dist archives by path and counts the requests.
type distServer struct {
	*httptest.Server
	files map[string][]byte
	hits  map[string]int
	mu    sync.Mutex
}

func newDistServer(t *testing.T, files map[string][]byte) *distServer {
	t.Helper()

	s := &distServer{files: files, hits: map[string]int{}}
	s.Server = httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		s.mu.Lock()
		s.hits[r.URL.Path]++
		data, ok := s.files[r.URL.Path]
		s.mu.Unlock()

		if !ok {
			nethttp.NotFound(w, r)

			return
		}

		_, _ = w.Write(data)
	}))
	t.Cleanup(s.Close)

	return s
}

func (s *distServer) requests(path string) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.hits[path]
}

// project is one Composer project downloading through a set of archive
// downloaders that share a package store.
type project struct {
	t      *testing.T
	out    *mio.BufferIO
	loop   *http.Loop
	vendor string
	deps   Deps
}

type projectOptions struct {
	verbosity int
	noCache   bool
	readOnly  bool
	store     *store.Store
}

func newProject(t *testing.T, root string, opts projectOptions) *project {
	t.Helper()

	out := bufferIO(t, opts.verbosity)
	vendor := root + "/vendor"

	c := config.New(false, "")
	if err := c.Merge(php.ArrayOf("config", php.ArrayOf("vendor-dir", vendor, "secure-http", false)), "test"); err != nil {
		t.Fatal(err)
	}

	h, err := http.NewHttpDownloader(out, httpConfig{configAdapter{c}}, nil, true, http.NewStaticRuntime("8.4.0", "2.10.3"))
	if err != nil {
		t.Fatal(err)
	}

	process := asyncProcess()
	deps := Deps{IO: out, Config: configAdapter{c}, HTTPDownloader: h, Process: process, Filesystem: util.NewFilesystem(process), Store: opts.store, Metadata: NewMetadata()}

	if !opts.noCache {
		filesCache, err := cache.New(out, root+"/cache/files", "a-z0-9_./", nil, opts.readOnly)
		if err != nil {
			t.Fatal(err)
		}

		deps.Cache = filesCache
	}

	return &project{t: t, out: out, loop: http.NewLoop(h, process), vendor: vendor, deps: deps}
}

func newStore(t *testing.T) *store.Store {
	t.Helper()

	s, err := store.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}

	return s
}

// install downloads and installs p as InstallationManager does: download,
// wait, prepare, install, cleanup.
func (pr *project) install(d Downloader, p pkg.PackageInterface) (string, error) {
	path := pr.vendor + "/" + p.PrettyName()

	promise, err := d.Download(p, path, nil)
	if err != nil {
		return path, err
	}

	if err := pr.loop.Wait([]http.Waitable{promise}, nil); err != nil {
		return path, err
	}

	if err := await(d.Prepare("install", p, path, nil)); err != nil {
		return path, err
	}

	installErr := await(d.Install(p, path))

	if err := await(d.Cleanup("install", p, path, nil)); err != nil {
		return path, err
	}

	return path, installErr
}

func distPackage(url, typ string) *pkg.CompletePackage {
	p := getPackage("a/b", "1.0.0.0", "1.0.0")
	p.SetDistType(pkg.Str(typ))
	p.SetDistURL(pkg.Str(url))
	p.SetDistReference(pkg.Str("abc123"))

	return p
}

func githubZip() []byte {
	return archivetest.Zip("",
		archivetest.UnixDir("pkg-1a2b3c/", 0o755),
		archivetest.UnixFile("pkg-1a2b3c/composer.json", 0o644, `{"name":"a/b"}`),
		archivetest.UnixDir("pkg-1a2b3c/bin/", 0o755),
		archivetest.UnixFile("pkg-1a2b3c/bin/tool", 0o755, "#!/usr/bin/env php\n"),
	)
}

// downloadLines are the output lines, without the warning about the test
// server's plain http.
func downloadLines(out *mio.BufferIO) []string {
	return slices.DeleteFunc(outputLines(out), func(l string) bool {
		return strings.HasPrefix(l, "<warning>Warning: Accessing 127.0.0.1 over http")
	})
}

func checkTree(t *testing.T, path string) {
	t.Helper()

	data, err := os.ReadFile(path + "/composer.json")
	if err != nil || string(data) != `{"name":"a/b"}` {
		t.Fatalf("composer.json: %q, %v", data, err)
	}

	fi, err := os.Stat(path + "/bin/tool")
	if err != nil || fi.Mode().Perm() != 0o755 {
		t.Fatalf("bin/tool: %v, %v", fi, err)
	}
}

func checkNoLeftovers(t *testing.T, vendor string) {
	t.Helper()

	entries, _ := os.ReadDir(vendor + "/composer")
	if len(entries) != 0 {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}

		t.Fatalf("leftovers in vendor/composer: %q", names)
	}
}

func TestArchiveDownloader_StoreSecondInstallMakesNoRequest(t *testing.T) {
	srv := newDistServer(t, map[string][]byte{"/a.zip": githubZip()})
	shared := newStore(t)

	first := newProject(t, t.TempDir(), projectOptions{store: shared})

	d, err := NewZipDownloader(first.deps)
	if err != nil {
		t.Fatal(err)
	}

	path, err := first.install(d, distPackage(srv.URL+"/a.zip", "zip"))
	if err != nil {
		t.Fatal(err)
	}

	checkTree(t, path)
	checkNoLeftovers(t, first.vendor)

	want := []string{
		"  - Downloading a/b (1.0.0)",
		"  - Installing a/b (1.0.0): Extracting archive",
	}
	if got := downloadLines(first.out); !slices.Equal(got, want) {
		t.Fatalf("output %q, want %q", got, want)
	}

	if size, ok := first.deps.Metadata.Get("a/b"); !ok || size != int64(len(githubZip())) {
		t.Fatalf("download metadata %v", size)
	}

	// another project (or worktree) on the same machine
	second := newProject(t, t.TempDir(), projectOptions{store: shared, verbosity: console.VerbosityVeryVerbose})

	d, err = NewZipDownloader(second.deps)
	if err != nil {
		t.Fatal(err)
	}

	path, err = second.install(d, distPackage(srv.URL+"/a.zip", "zip"))
	if err != nil {
		t.Fatal(err)
	}

	checkTree(t, path)
	checkNoLeftovers(t, second.vendor)

	if n := srv.requests("/a.zip"); n != 1 {
		t.Fatalf("%d requests, want 1", n)
	}

	want = []string{
		"  - Loading a/b (1.0.0) from cache",
		"  - Installing a/b (1.0.0): Extracting archive",
	}
	if got := downloadLines(second.out); !slices.Equal(got, want) {
		t.Fatalf("output %q, want %q", got, want)
	}
}

func TestArchiveDownloader_StoreNotUsedWithoutFilesCache(t *testing.T) {
	srv := newDistServer(t, map[string][]byte{"/a.zip": githubZip()})
	shared := newStore(t)

	for range 2 {
		pr := newProject(t, t.TempDir(), projectOptions{store: shared, noCache: true})

		d, err := NewZipDownloader(pr.deps)
		if err != nil {
			t.Fatal(err)
		}

		path, err := pr.install(d, distPackage(srv.URL+"/a.zip", "zip"))
		if err != nil {
			t.Fatal(err)
		}

		checkTree(t, path)
		checkNoLeftovers(t, pr.vendor)
	}

	if n := srv.requests("/a.zip"); n != 2 {
		t.Fatalf("%d requests, want 2", n)
	}

	if st, err := shared.Stats(); err != nil || st.Releases != 0 {
		t.Fatalf("the shared store was written: %+v, %v", st, err)
	}
}

func TestArchiveDownloader_StoreReadOnlyCache(t *testing.T) {
	srv := newDistServer(t, map[string][]byte{"/a.zip": githubZip()})
	shared := newStore(t)

	pr := newProject(t, t.TempDir(), projectOptions{store: shared, readOnly: true})

	d, err := NewZipDownloader(pr.deps)
	if err != nil {
		t.Fatal(err)
	}

	path, err := pr.install(d, distPackage(srv.URL+"/a.zip", "zip"))
	if err != nil {
		t.Fatal(err)
	}

	checkTree(t, path)

	if st, err := shared.Stats(); err != nil || st.Releases != 0 {
		t.Fatalf("a read-only cache wrote the shared store: %+v, %v", st, err)
	}
}

func TestArchiveDownloader_ShasumMismatch(t *testing.T) {
	srv := newDistServer(t, map[string][]byte{"/a.zip": githubZip()})
	pr := newProject(t, t.TempDir(), projectOptions{store: newStore(t)})

	d, err := NewZipDownloader(pr.deps)
	if err != nil {
		t.Fatal(err)
	}

	p := distPackage(srv.URL+"/a.zip", "zip")
	p.SetDistSha1Checksum(pkg.Str("0000000000000000000000000000000000000000"))

	_, err = pr.install(d, p)
	if _, ok := errors.AsType[*util.UnexpectedValueError](err); !ok {
		t.Fatalf("expected UnexpectedValueException, got %v", err)
	}

	if want := "The checksum verification of the file failed (downloaded from " + srv.URL + "/a.zip)"; err.Error() != want {
		t.Fatalf("error %q, want %q", err, want)
	}

	// a checksum failure is not retried
	if n := srv.requests("/a.zip"); n != 1 {
		t.Fatalf("%d requests, want 1", n)
	}
}

func TestArchiveDownloader_ShasumMatch(t *testing.T) {
	data := githubZip()
	srv := newDistServer(t, map[string][]byte{"/a.zip": data})
	pr := newProject(t, t.TempDir(), projectOptions{store: newStore(t)})

	d, err := NewZipDownloader(pr.deps)
	if err != nil {
		t.Fatal(err)
	}

	tmp := filepath.Join(t.TempDir(), "a.zip")
	writeFile(t, tmp, data, 0o644)

	sum, err := sha1File(tmp)
	if err != nil {
		t.Fatal(err)
	}

	p := distPackage(srv.URL+"/a.zip", "zip")
	p.SetDistSha1Checksum(pkg.Str(sum))

	path, err := pr.install(d, p)
	if err != nil {
		t.Fatal(err)
	}

	checkTree(t, path)
}

func TestArchiveDownloader_RetriesAndReports404(t *testing.T) {
	srv := newDistServer(t, map[string][]byte{})
	pr := newProject(t, t.TempDir(), projectOptions{store: newStore(t)})

	d, err := NewZipDownloader(pr.deps)
	if err != nil {
		t.Fatal(err)
	}

	_, err = pr.install(d, distPackage(srv.URL+"/missing.zip", "zip"))
	if _, ok := errors.AsType[*util.TransportError](err); !ok {
		t.Fatalf("expected TransportException, got %v", err)
	}

	// a 404 is not retried
	if n := srv.requests("/missing.zip"); n != 1 {
		t.Fatalf("%d requests, want 1", n)
	}
}

func TestArchiveDownloader_MirrorFallback(t *testing.T) {
	srv := newDistServer(t, map[string][]byte{"/b.zip": githubZip()})
	pr := newProject(t, t.TempDir(), projectOptions{store: newStore(t)})

	d, err := NewZipDownloader(pr.deps)
	if err != nil {
		t.Fatal(err)
	}

	d.retryDelay, d.nextURLDelay = 0, 0

	p := distPackage(srv.URL+"/a.zip", "zip")
	p.SetDistMirrors(php.ListOf(php.ArrayOf("url", srv.URL+"/b.zip", "preferred", false)))

	path, err := pr.install(d, p)
	if err != nil {
		t.Fatal(err)
	}

	checkTree(t, path)

	if !strings.Contains(pr.out.Output(), "    Failed downloading a/b, trying the next URL (404: ") {
		t.Fatalf("output %q", pr.out.Output())
	}
}

// ZipDownloaderTest::testErrorMessages: a file that is not a zip.
func TestZipDownloader_ErrorMessages(t *testing.T) {
	srv := newDistServer(t, map[string][]byte{"/a.zip": []byte("<?php not a zip\n")})
	pr := newProject(t, t.TempDir(), projectOptions{store: newStore(t)})

	d, err := NewZipDownloader(pr.deps)
	if err != nil {
		t.Fatal(err)
	}

	p := distPackage(srv.URL+"/a.zip", "zip")
	tmpFile := d.fileName(p)

	path, err := pr.install(d, p)
	mustContain(t, err, "is not a zip archive")

	if want := "'" + tmpFile + "' is not a zip archive."; err.Error() != want {
		t.Fatalf("error %q, want %q", err, want)
	}

	out := pr.out.Output()
	for _, line := range []string{
		"    <warning>Failed to extract a/b: (9) " + unzipPath() + " -qq " + tmpFile + " -d ",
		"    The archive may contain identical file names with different capitalization (which fails on case insensitive filesystems)\n",
		"    Unzip with unzip command failed, falling back to ZipArchive class\n",
	} {
		if !strings.Contains(out, line) {
			t.Fatalf("output %q lacks %q", out, line)
		}
	}

	if isDir(path) {
		t.Fatal("the failed install left its directory")
	}

	checkNoLeftovers(t, pr.vendor)
}

// ZipDownloaderTest::testSystemUnzipOnlyFailed: unzip's own failure (a zip
// bomb, which Composer reports without falling back).
func TestZipDownloader_SystemUnzipOnlyFailed(t *testing.T) {
	var bomb []byte

	for _, c := range archivetest.ZipCorpus() {
		if c.Name == "overlap" {
			bomb = c.Data
		}
	}

	srv := newDistServer(t, map[string][]byte{"/a.zip": bomb})
	pr := newProject(t, t.TempDir(), projectOptions{store: newStore(t)})

	d, err := NewZipDownloader(pr.deps)
	if err != nil {
		t.Fatal(err)
	}

	_, err = pr.install(d, distPackage(srv.URL+"/a.zip", "zip"))
	mustContain(t, err, "Failed to extract a/b: (12) "+unzipPath())
	mustContain(t, err, "zip bomb")
}

// ZipDownloaderTest::testNonWindowsFallbackFailed: a corrupt archive.
func TestZipDownloader_CorruptArchiveFailed(t *testing.T) {
	var bad []byte

	for _, c := range archivetest.ZipCorpus() {
		if c.Name == "corrupt-data" {
			bad = c.Data
		}
	}

	srv := newDistServer(t, map[string][]byte{"/a.zip": bad})
	pr := newProject(t, t.TempDir(), projectOptions{store: newStore(t)})

	d, err := NewZipDownloader(pr.deps)
	if err != nil {
		t.Fatal(err)
	}

	_, err = pr.install(d, distPackage(srv.URL+"/a.zip", "zip"))
	mustContain(t, err, "Failed to extract a/b: ")
}

// XzDownloaderTest::testErrorMessages.
func TestXzDownloader_ErrorMessages(t *testing.T) {
	srv := newDistServer(t, map[string][]byte{"/a.tar.xz": []byte("<?php not an archive\n")})
	pr := newProject(t, t.TempDir(), projectOptions{store: newStore(t)})

	d, err := NewXzDownloader(pr.deps)
	if err != nil {
		t.Fatal(err)
	}

	_, err = pr.install(d, distPackage(srv.URL+"/a.tar.xz", "xz"))
	if _, ok := errors.AsType[*util.RuntimeError](err); !ok {
		t.Fatalf("expected RuntimeException, got %v", err)
	}

	if !regexp.MustCompile(`(?i)(File format not recognized|Unrecognized archive format)`).MatchString(err.Error()) {
		t.Fatalf("unexpected error %q", err)
	}

	mustContain(t, err, "Failed to execute tar -xJf ")
}

func tarball() []byte {
	return archivetest.Tar(tar.FormatPAX, false,
		archivetest.TDir("pkg/", 0o755),
		archivetest.TFile("pkg/composer.json", 0o644, `{"name":"a/b"}`),
		archivetest.TDir("pkg/bin/", 0o755),
		archivetest.TFile("pkg/bin/tool", 0o755, "#!/usr/bin/env php\n"),
	)
}

func TestArchiveDownloader_TarXzGzip(t *testing.T) {
	srv := newDistServer(t, map[string][]byte{
		"/a.tar.gz": archivetest.Gzip(tarball()),
		"/a.tar.xz": archivetest.Xz(tarball()),
		"/tool.gz":  archivetest.Gzip([]byte("#!/bin/sh\n")),
	})

	for _, c := range []struct {
		url  string
		typ  string
		ctor func(Deps) (*ArchiveDownloader, error)
	}{
		{"/a.tar.gz", "tar", NewTarDownloader},
		{"/a.tar.xz", "xz", NewXzDownloader},
	} {
		pr := newProject(t, t.TempDir(), projectOptions{store: newStore(t)})

		d, err := c.ctor(pr.deps)
		if err != nil {
			t.Fatal(err)
		}

		path, err := pr.install(d, distPackage(srv.URL+c.url, c.typ))
		if err != nil {
			t.Fatalf("%s: %v", c.typ, err)
		}

		checkTree(t, path)
		checkNoLeftovers(t, pr.vendor)
	}

	pr := newProject(t, t.TempDir(), projectOptions{store: newStore(t)})

	d, err := NewGzipDownloader(pr.deps)
	if err != nil {
		t.Fatal(err)
	}

	path, err := pr.install(d, distPackage(srv.URL+"/tool.gz", "gzip"))
	if err != nil {
		t.Fatal(err)
	}

	if data, err := os.ReadFile(path + "/tool"); err != nil || string(data) != "#!/bin/sh\n" {
		t.Fatalf("tool: %q, %v", data, err)
	}
}

func TestTarDownloader_PharDataExtensionCheck(t *testing.T) {
	srv := newDistServer(t, map[string][]byte{"/download.zip": tarball(), "/download.phar": tarball()})

	for _, c := range []struct{ url, want string }{
		{"/download.zip", `phar zip error: phar "`},
		{"/download.phar", `Cannot create phar '`},
	} {
		pr := newProject(t, t.TempDir(), projectOptions{store: newStore(t)})

		d, err := NewTarDownloader(pr.deps)
		if err != nil {
			t.Fatal(err)
		}

		_, err = pr.install(d, distPackage(srv.URL+c.url, "tar"))
		mustContain(t, err, c.want)
	}

	for _, c := range []struct {
		file string
		ok   bool
	}{
		{"/v/composer/tmp-abc.tar", true},
		{"/v/composer/tmp-abc.3", true},
		{"/v/composer/tmp-abc.Phar", true},
		{"/x.zip/composer/tmp-abc.tar", true},
		{"/v/composer/tmp-abc.xzip", false},
		{"/v/composer/tmp-abc.pharx", false},
		{"/a.phar.d/composer/tmp-abc.tar", false},
	} {
		if err := pharDataCheck(c.file); (err == nil) != c.ok {
			t.Errorf("pharDataCheck(%q) = %v", c.file, err)
		}
	}
}

func TestArchiveDownloader_InstallWithoutDownload(t *testing.T) {
	// a plugin placed the archive itself, then calls install()
	pr := newProject(t, t.TempDir(), projectOptions{store: newStore(t)})

	d, err := NewZipDownloader(pr.deps)
	if err != nil {
		t.Fatal(err)
	}

	p := distPackage("http://example.invalid/a.zip", "zip")

	file := d.fileName(p)
	if err := os.MkdirAll(filepath.Dir(file), 0o777); err != nil {
		t.Fatal(err)
	}

	writeFile(t, file, githubZip(), 0o644)

	path := pr.vendor + "/a/b"
	if err := await(d.Install(p, path)); err != nil {
		t.Fatal(err)
	}

	checkTree(t, path)

	if fileExists(file) {
		t.Fatal("the archive was left behind")
	}
}

func TestArchiveDownloader_MergesIntoExistingDirectory(t *testing.T) {
	// create-project in the current directory: path contains the vendor
	// dir and is not emptied, the package is merged into it
	srv := newDistServer(t, map[string][]byte{"/a.zip": githubZip()})
	root := t.TempDir()
	pr := newProject(t, root, projectOptions{store: newStore(t)})

	d, err := NewZipDownloader(pr.deps)
	if err != nil {
		t.Fatal(err)
	}

	writeFile(t, root+"/keep.txt", []byte("keep"), 0o644)

	p := distPackage(srv.URL+"/a.zip", "zip")

	promise, err := d.Download(p, root, nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := pr.loop.Wait([]http.Waitable{promise}, nil); err != nil {
		t.Fatal(err)
	}

	if err := await(d.Install(p, root)); err != nil {
		t.Fatal(err)
	}

	checkTree(t, root)

	if !fileExists(root + "/keep.txt") {
		t.Fatal("the existing file was removed")
	}
}

func TestArchiveDownloader_LocalChanges(t *testing.T) {
	srv := newDistServer(t, map[string][]byte{"/a.zip": githubZip()})
	pr := newProject(t, t.TempDir(), projectOptions{store: newStore(t)})

	d, err := NewZipDownloader(pr.deps)
	if err != nil {
		t.Fatal(err)
	}

	p := distPackage(srv.URL+"/a.zip", "zip")

	path, err := pr.install(d, p)
	if err != nil {
		t.Fatal(err)
	}

	if changes, err := d.LocalChanges(p, path); err != nil || changes.Valid {
		t.Fatalf("unchanged package reported %v, %v", changes, err)
	}

	writeFile(t, path+"/composer.json", []byte("{}"), 0o644)

	changes, err := d.LocalChanges(p, path)
	if err != nil || !changes.Valid || !strings.Contains(changes.S, "composer.json") {
		t.Fatalf("changes %v, %v", changes, err)
	}

	if isDir(path + "_compare") {
		t.Fatal("the comparison directory was left behind")
	}
}

func TestArchiveDownloader_UpdateAndRemove(t *testing.T) {
	srv := newDistServer(t, map[string][]byte{"/a.zip": githubZip()})
	pr := newProject(t, t.TempDir(), projectOptions{store: newStore(t)})

	d, err := NewZipDownloader(pr.deps)
	if err != nil {
		t.Fatal(err)
	}

	initial := distPackage(srv.URL+"/a.zip", "zip")

	path, err := pr.install(d, initial)
	if err != nil {
		t.Fatal(err)
	}

	target := getPackage("a/b", "1.1.0.0", "1.1.0")
	target.SetDistType(pkg.Str("zip"))
	target.SetDistURL(pkg.Str(srv.URL + "/a.zip"))
	target.SetDistReference(pkg.Str("def456"))

	promise, err := d.Download(target, path, initial)
	if err != nil {
		t.Fatal(err)
	}

	if err := pr.loop.Wait([]http.Waitable{promise}, nil); err != nil {
		t.Fatal(err)
	}

	if err := await(d.Update(initial, target, path)); err != nil {
		t.Fatal(err)
	}

	checkTree(t, path)

	if err := await(d.Remove(target, path)); err != nil {
		t.Fatal(err)
	}

	if fileExists(path) {
		t.Fatal("the package was not removed")
	}

	if !strings.Contains(pr.out.Output(), "  - Upgrading a/b (1.0.0 => 1.1.0): Extracting archive\n") {
		t.Fatalf("output %q", pr.out.Output())
	}

	if !strings.Contains(pr.out.Output(), "  - Removing a/b (1.1.0)\n") {
		t.Fatalf("output %q", pr.out.Output())
	}
}

func TestFileDownloader_FilesCache(t *testing.T) {
	srv := newDistServer(t, map[string][]byte{"/tool.phar": []byte("phar")})
	root := t.TempDir()

	for i := range 2 {
		pr := newProject(t, root, projectOptions{verbosity: console.VerbosityVeryVerbose})

		d, err := NewFileDownloader(pr.deps)
		if err != nil {
			t.Fatal(err)
		}

		p := distPackage(srv.URL+"/tool.phar", "file")

		path, err := pr.install(d, p)
		if err != nil {
			t.Fatal(err)
		}

		if data, err := os.ReadFile(path + "/tool.phar"); err != nil || string(data) != "phar" {
			t.Fatalf("tool.phar: %q, %v", data, err)
		}

		verb := "Downloading"
		if i == 1 {
			verb = "Loading"
		}

		if !strings.HasPrefix(pr.out.Output(), "  - "+verb+" a/b") {
			t.Fatalf("run %d output %q", i, pr.out.Output())
		}
	}

	if n := srv.requests("/tool.phar"); n != 1 {
		t.Fatalf("%d requests, want 1", n)
	}
}

// new \PharData() on a tar.bz2 (tar.gz) when the PHP running Composer
// lacks bz2 (zlib): the UnexpectedValueException ext/phar throws, before
// anything is extracted.
func TestTarDownloader_PharDataNeedsCompressionExtension(t *testing.T) {
	dir := t.TempDir()

	for _, c := range []struct {
		name, ext string
		data      []byte
		want      string
	}{
		{"tmp-a.bz2", "bz2", []byte("BZh91AY&SY"), `unable to decompress bzipped phar archive "` + dir + `/tmp-a.bz2" to temporary file, enable bz2 extension in php.ini`},
		{"tmp-b.gz", "zlib", []byte{0x1f, 0x8b, 8, 0}, `unable to decompress gzipped phar archive "` + dir + `/tmp-b.gz" to temporary file, enable zlib extension in php.ini`},
	} {
		file := filepath.Join(dir, c.name)
		if err := os.WriteFile(file, c.data, 0o644); err != nil {
			t.Fatal(err)
		}

		err := pharCompressionCheck(file, func(name string) bool { return name != c.ext })
		if err == nil || err.Error() != c.want {
			t.Errorf("%s: %v, want %s", c.name, err, c.want)
		}

		if _, ok := errors.AsType[*util.UnexpectedValueError](err); !ok {
			t.Errorf("%s: %T, want an UnexpectedValueException", c.name, err)
		}

		if err := pharCompressionCheck(file, func(string) bool { return true }); err != nil {
			t.Errorf("%s with the extension: %v", c.name, err)
		}
	}
}
