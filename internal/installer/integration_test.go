//go:build unix

package installer

import (
	nethttp "net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stubbedev/maestro/internal/archive/archivetest"
	"github.com/stubbedev/maestro/internal/config"
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/downloader"
	mio "github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/resolver/operation"
	"github.com/stubbedev/maestro/internal/store"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/http"
)

// zipProject is a project installing zip dists served by a test server
// through the real download manager, archive downloader and package store.
type zipProject struct {
	srv     *httptest.Server
	vendor  string
	out     interface{ Output() string }
	manager *Manager
}

// newZipProject serves v/<name>'s zip at /<name>.zip (with bin/<name>);
// handle, when not nil, runs first for every request.
func newZipProject(t *testing.T, handle func()) *zipProject {
	t.Helper()

	srv := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if handle != nil {
			handle()
		}

		name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/"), ".zip")
		_, _ = w.Write(archivetest.Zip("",
			archivetest.UnixDir("pkg/", 0o755),
			archivetest.UnixFile("pkg/composer.json", 0o644, `{"name":"v/`+name+`"}`),
			archivetest.UnixDir("pkg/bin/", 0o755),
			archivetest.UnixFile("pkg/bin/"+name, 0o644, "#!/usr/bin/env php\n<?php echo 1;\n"),
		))
	}))
	t.Cleanup(srv.Close)

	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	vendor := root + "/vendor"
	out := newBufferIO(t)

	cfg := config.New(false, root)
	if err := cfg.Merge(php.ArrayOf("config", php.ArrayOf("vendor-dir", vendor, "bin-dir", vendor+"/bin", "secure-http", false)), "test"); err != nil {
		t.Fatal(err)
	}

	h, err := http.NewHttpDownloader(out, cfg.ForHTTP(), nil, true, http.NewStaticRuntime("8.4.0", "2.10.3"))
	if err != nil {
		t.Fatal(err)
	}

	process := util.NewProcessExecutor(nil)
	loop := http.NewLoop(h, process)

	st, err := store.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}

	metadata := downloader.NewMetadata()
	deps := downloader.Deps{IO: out, Config: cfg.ForIO(), HTTPDownloader: h, Process: process, Filesystem: util.NewFilesystem(process), Store: st, Metadata: metadata}

	zip, err := downloader.NewZipDownloader(deps)
	if err != nil {
		t.Fatal(err)
	}

	dm := downloader.NewDownloadManager(out, false, util.NewFilesystem(process))
	dm.SetDownloader("zip", zip)

	composer := &fullTestComposer{dm: dm}
	composer.cfg = cfg

	library, err := NewLibraryInstaller(out, composer, pkg.NullString{}, util.NewFilesystem(process), nil)
	if err != nil {
		t.Fatal(err)
	}

	manager := NewManager(loop, out, nil)
	manager.SetDownloadMetadata(metadata)
	manager.AddInstaller(library)

	return &zipProject{srv: srv, vendor: vendor, out: out, manager: manager}
}

// installOp is the install operation of v/<name> 1.0.0 from the project's
// server, with the given binaries.
func (z *zipProject) installOp(name string, bins ...any) operation.Operation {
	p := pkg.NewPackage("v/"+name, "1.0.0.0", "1.0.0")
	p.SetType("library")
	p.SetDistType(pkg.Str("zip"))
	p.SetDistURL(pkg.Str(z.srv.URL + "/" + name + ".zip"))
	p.SetDistReference(pkg.Str("ref" + name))
	p.SetBinaries(php.ListOf(bins...))

	return operation.NewInstallOperation(p)
}

// TestInstallationManager_IntegrationZip installs many zip dists through
// the real download manager, archive downloader and package store: the
// downloads run concurrently, the output lines come in operation order
// and every package is placed with its bin proxy.
func TestInstallationManager_IntegrationZip(t *testing.T) {
	const n = 24

	var (
		inFlight, maxInFlight atomic.Int64
		release               sync.Once
	)

	block := make(chan struct{})

	z := newZipProject(t, func() {
		cur := inFlight.Add(1)
		for {
			m := maxInFlight.Load()
			if cur <= m || maxInFlight.CompareAndSwap(m, cur) {
				break
			}
		}

		if cur >= 4 {
			// enough concurrency observed: release everyone (once: several
			// handlers get here at the same time, and a second close
			// panicked the handler, which the client saw as curl error 52,
			// "Empty reply from server")
			release.Do(func() { close(block) })
		}

		<-block
		inFlight.Add(-1)
	})
	vendor, out, manager := z.vendor, z.out, z.manager

	var ops []operation.Operation

	for i := range n {
		name := "p" + string(rune('a'+i))
		ops = append(ops, z.installOp(name, "bin/"+name))
	}

	repo, err := repository.NewInstalledArrayRepository(nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := manager.Execute(repo, ops, true, true, false); err != nil {
		t.Fatal(err)
	}

	if maxInFlight.Load() < 4 {
		t.Errorf("downloads did not run concurrently (max %d in flight)", maxInFlight.Load())
	}

	var installing []string

	for line := range strings.SplitSeq(out.Output(), "\n") {
		if strings.HasPrefix(line, "  - Installing ") {
			installing = append(installing, line)
		}
	}

	var want []string
	for i := range n {
		want = append(want, "  - Installing v/p"+string(rune('a'+i))+" (1.0.0): Extracting archive")
	}

	if !slices.Equal(installing, want) {
		t.Errorf("install lines:\n%s\nwant\n%s\nfull output:\n%s", strings.Join(installing, "\n"), strings.Join(want, "\n"), out.Output())
	}

	packages, _ := repo.Packages()
	if len(packages) != n {
		t.Errorf("%d packages in the repository, want %d", len(packages), n)
	}

	for i := range n {
		name := "p" + string(rune('a'+i))

		data, err := os.ReadFile(vendor + "/v/" + name + "/composer.json")
		if err != nil || string(data) != `{"name":"v/`+name+`"}` {
			t.Errorf("%s: composer.json %q, %v", name, data, err)
		}

		if st, err := os.Stat(vendor + "/v/" + name + "/bin/" + name); err != nil || st.Mode().Perm() != 0o755 {
			t.Errorf("%s: bin not executable: %v", name, err)
		}

		proxy, err := os.ReadFile(vendor + "/bin/" + name)
		if err != nil || !strings.Contains(string(proxy), "return include __DIR__ . '/..'.'/v/"+name+"/bin/"+name+"';") {
			t.Errorf("%s: proxy %q, %v", name, proxy, err)
		}
	}

	if entries, _ := os.ReadDir(vendor + "/composer"); len(entries) != 0 {
		t.Errorf("leftovers in vendor/composer: %v", entries)
	}
}

// TestInstallationManager_ArchiveInstallContinuations: an archive install
// settles on a later loop tick (Composer extracts with an async unzip, then
// removes the temporary directory asynchronously), so what runs after it
// (here the bin warnings of LibraryInstaller) comes after every
// "Installing" line of the batch, in operation order.
func TestInstallationManager_ArchiveInstallContinuations(t *testing.T) {
	z := newZipProject(t, nil)

	ops := []operation.Operation{
		z.installOp("pa", "bin/missing"),
		z.installOp("pb", "bin/pb"),
		z.installOp("pc", "bin/missing"),
	}

	repo, err := repository.NewInstalledArrayRepository(nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := z.manager.Execute(repo, ops, true, true, false); err != nil {
		t.Fatal(err)
	}

	var got []string

	for line := range strings.SplitSeq(z.out.Output(), "\n") {
		if strings.HasPrefix(line, "  - Installing ") || strings.HasPrefix(line, "    <warning>Skipped") {
			got = append(got, line)
		}
	}

	want := []string{
		"  - Installing v/pa (1.0.0): Extracting archive",
		"  - Installing v/pb (1.0.0): Extracting archive",
		"  - Installing v/pc (1.0.0): Extracting archive",
		"    <warning>Skipped installation of bin bin/missing for package v/pa: file not found in package</warning>",
		"    <warning>Skipped installation of bin bin/missing for package v/pc: file not found in package</warning>",
	}

	if !slices.Equal(got, want) {
		t.Errorf("output:\n%s\nwant\n%s\nfull output:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"), z.out.Output())
	}
}

// TestInstallationManager_NotifyInstalls checks the notification requests:
// one batch POST per repository URL (with download sizes for packagist),
// one form POST per package for the deprecated %package% URLs.
func TestInstallationManager_NotifyInstalls(t *testing.T) {
	type request struct{ path, contentType, body string }

	requests := make(chan request, 10)

	srv := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		requests <- request{r.URL.Path, r.Header.Get("Content-Type"), string(body)}
	}))
	t.Cleanup(srv.Close)

	out := newBufferIO(t)

	cfg := config.New(false, "")
	if err := cfg.Merge(php.ArrayOf("config", php.ArrayOf("secure-http", false)), "test"); err != nil {
		t.Fatal(err)
	}

	h, err := http.NewHttpDownloader(out, cfg.ForHTTP(), nil, true, http.NewStaticRuntime("8.4.0", "2.10.3"))
	if err != nil {
		t.Fatal(err)
	}

	metadata := downloader.NewMetadata()
	manager := NewManager(http.NewLoop(h, nil), out, nil)
	manager.SetDownloadMetadata(metadata)
	manager.AddInstaller(NewNoopInstaller())

	newNotified := func(name, url string) *pkg.Package {
		p := pkg.NewPackage(name, "1.2.0.0", "v1.2.0")
		p.SetNotificationURL(url)

		return p
	}

	repo := newMockRepo(t)

	for _, p := range []*pkg.Package{
		newNotified("a/a", srv.URL+"/packagist.org/downloads/"),
		newNotified("b/b", srv.URL+"/packagist.org/downloads/"),
		newNotified("c/c", srv.URL+"/legacy/%package%"),
		pkg.NewPackage("d/d", "1.0.0.0", "1.0.0"),
	} {
		if _, err := manager.Install(repo, operation.NewInstallOperation(p)); err != nil {
			t.Fatal(err)
		}
	}

	// the size the downloader recorded for a/a (b/b has none)
	metadata.Reset()

	manager.NotifyInstalls(out)
	close(requests)

	var got []request
	for r := range requests {
		got = append(got, r)
	}

	slices.SortFunc(got, func(a, b request) int { return strings.Compare(a.path, b.path) })

	want := []request{
		{"/legacy/c/c", "application/x-www-form-urlencoded", "version=v1.2.0&version_normalized=1.2.0.0"},
		{"/packagist.org/downloads/", "application/json", `{"downloads":[{"name":"a\/a","version":"1.2.0.0","downloaded":false},{"name":"b\/b","version":"1.2.0.0","downloaded":false}]}`},
	}

	if !slices.Equal(got, want) {
		t.Errorf("requests:\n%v\nwant\n%v", got, want)
	}
}

// TestInstallationManager_ExecuteFailureLeavesProgressLine: Loop::wait
// throws the rejection, so waitOnPromises never clears the progress bar or
// writes the line break ending it (the exception's rendering does).
func TestInstallationManager_ExecuteFailureLeavesProgressLine(t *testing.T) {
	rec := &recorder{}
	installer := newMockInstaller(rec, func(string) bool { return true })
	installer.result = func(method string, p pkg.PackageInterface) (*Promise, error) {
		if method == "install" && p.Name() == "b/b" {
			return Rejected(&util.RuntimeError{Message: "boom"}), nil
		}

		return Resolved(), nil
	}

	in, err := console.NewArrayInput(nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	out := console.NewBufferedOutput(console.VerbosityNormal, false, nil)
	cio := mio.NewConsoleIO(in, out, console.NewHelperSet())

	h, err := http.NewHttpDownloader(cio, config.New(false, "").ForHTTP(), nil, true, http.NewStaticRuntime("8.4.0", "2.10.3"))
	if err != nil {
		t.Fatal(err)
	}

	manager := NewManager(http.NewLoop(h, nil), cio, nil)
	manager.SetOutputProgress(true)
	manager.AddInstaller(installer)

	repo := newMockRepo(t)
	repo.rec = rec

	err = manager.Execute(repo, []operation.Operation{
		operation.NewInstallOperation(installedPackage("a/a", "1.0.0", "library")),
		operation.NewInstallOperation(installedPackage("b/b", "1.0.0", "library")),
	}, true, true, false)
	if err == nil || err.Error() != "boom" {
		t.Fatalf("err = %v", err)
	}

	if got := out.Fetch(); strings.HasSuffix(got, "\n") || !strings.Contains(got, "    Install of b/b failed\n") {
		t.Errorf("output %q: want the progress bar's last line without a line break", got)
	}
}
