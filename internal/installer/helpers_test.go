package installer

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/stubbedev/maestro/internal/config"
	"github.com/stubbedev/maestro/internal/downloader"
	mio "github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/util"
)

func newBufferIO(t *testing.T) *mio.BufferIO {
	t.Helper()

	io, err := mio.NewBufferIO("", 0, nil)
	if err != nil {
		t.Fatal(err)
	}

	return io
}

func newPackage(name, version string) *pkg.Package {
	return pkg.NewPackage(name, version+".0", version)
}

// testComposer is a PartialComposer, or a Composer when dm is set.
type testComposer struct {
	cfg *config.Config
}

func (c *testComposer) Config() *config.Config { return c.cfg }

type fullTestComposer struct {
	testComposer
	dm *downloader.DownloadManager
	pm PluginManager
}

func (c *fullTestComposer) DownloadManager() *downloader.DownloadManager { return c.dm }

func (c *fullTestComposer) InstallerPluginManager() PluginManager { return c.pm }

func newConfig(t *testing.T, vendorDir, binDir string) *config.Config {
	t.Helper()

	cfg := config.New(false, "")

	c := php.NewArray()
	c.Set("vendor-dir", vendorDir)
	c.Set("bin-dir", binDir)

	merge := php.NewArray()
	merge.Set("config", c)

	if err := cfg.Merge(merge, config.SourceUnknown); err != nil {
		t.Fatal(err)
	}

	return cfg
}

// recorder records calls as strings, safely across goroutines.
type recorder struct {
	mu    sync.Mutex
	calls []string
}

func (r *recorder) add(format string, args ...any) {
	r.mu.Lock()
	r.calls = append(r.calls, fmt.Sprintf(format, args...))
	r.mu.Unlock()
}

func (r *recorder) list() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]string(nil), r.calls...)
}

func (r *recorder) count(prefix string) int {
	n := 0

	for _, c := range r.list() {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}

	return n
}

// mockRepo is an InstalledRepositoryInterface mock: an
// InstalledArrayRepository whose hasPackage answers can be scripted
// (onConsecutiveCalls) and whose mutations are recorded.
type mockRepo struct {
	*repository.InstalledArrayRepository
	rec        *recorder
	hasAnswers []bool
	hasCalls   int
	// fake makes add/remove only record, as a PHPUnit mock does.
	fake bool
}

func newMockRepo(t *testing.T, hasAnswers ...bool) *mockRepo {
	t.Helper()

	r, err := repository.NewInstalledArrayRepository(nil)
	if err != nil {
		t.Fatal(err)
	}

	return &mockRepo{InstalledArrayRepository: r, rec: &recorder{}, hasAnswers: hasAnswers, fake: true}
}

func (r *mockRepo) HasPackage(p pkg.PackageInterface) (bool, error) {
	r.hasCalls++
	r.rec.add("hasPackage %s", p.String())

	if !r.fake {
		return r.InstalledArrayRepository.HasPackage(p)
	}

	if len(r.hasAnswers) == 0 {
		return false, nil
	}

	answer := r.hasAnswers[0]
	r.hasAnswers = r.hasAnswers[1:]

	return answer, nil
}

func (r *mockRepo) AddPackage(p pkg.PackageInterface) error {
	r.rec.add("addPackage %s", p.String())

	if !r.fake {
		return r.InstalledArrayRepository.AddPackage(p)
	}

	return nil
}

func (r *mockRepo) RemovePackage(p pkg.PackageInterface) error {
	r.rec.add("removePackage %s", p.String())

	if !r.fake {
		return r.InstalledArrayRepository.RemovePackage(p)
	}

	return nil
}

func (r *mockRepo) Write(devMode bool, _ repository.InstallationManager) error {
	r.rec.add("write %v", devMode)

	return nil
}

// mockDM is a DownloadManager mock recording its calls; each returns a
// resolved promise unless result says otherwise.
type mockDM struct {
	rec    *recorder
	result func(method string, p pkg.PackageInterface) (*downloader.Promise, error)
}

func newMockDM() *mockDM { return &mockDM{rec: &recorder{}} }

func (d *mockDM) ret(method string, p pkg.PackageInterface) (*downloader.Promise, error) {
	if d.result != nil {
		return d.result(method, p)
	}

	return util.Resolved(""), nil
}

func (d *mockDM) Download(p pkg.PackageInterface, targetDir string, _ pkg.PackageInterface) (*downloader.Promise, error) {
	d.rec.add("download %s %s", p.String(), targetDir)

	return d.ret("download", p)
}

func (d *mockDM) Prepare(typ string, p pkg.PackageInterface, targetDir string, _ pkg.PackageInterface) (*downloader.Promise, error) {
	d.rec.add("prepare %s %s %s", typ, p.String(), targetDir)

	return d.ret("prepare", p)
}

func (d *mockDM) Install(p pkg.PackageInterface, targetDir string) (*downloader.Promise, error) {
	d.rec.add("install %s %s", p.String(), targetDir)

	return d.ret("install", p)
}

func (d *mockDM) Update(initial, target pkg.PackageInterface, targetDir string) (*downloader.Promise, error) {
	d.rec.add("update %s %s %s", initial.String(), target.String(), targetDir)

	return d.ret("update", target)
}

func (d *mockDM) Remove(p pkg.PackageInterface, targetDir string) (*downloader.Promise, error) {
	d.rec.add("remove %s %s", p.String(), targetDir)

	return d.ret("remove", p)
}

func (d *mockDM) Cleanup(typ string, p pkg.PackageInterface, targetDir string, _ pkg.PackageInterface) (*downloader.Promise, error) {
	d.rec.add("cleanup %s %s %s", typ, p.String(), targetDir)

	return d.ret("cleanup", p)
}

// mockInstaller is an InstallerInterface mock recording its calls.
type mockInstaller struct {
	rec      *recorder
	supports func(string) bool
	// result is the promise the operation methods return (nil: resolved).
	result func(method string, p pkg.PackageInterface) (*Promise, error)
}

func newMockInstaller(rec *recorder, supports func(string) bool) *mockInstaller {
	if rec == nil {
		rec = &recorder{}
	}

	return &mockInstaller{rec: rec, supports: supports}
}

func (i *mockInstaller) ret(method string, p pkg.PackageInterface) (*Promise, error) {
	if i.result != nil {
		return i.result(method, p)
	}

	return Resolved(), nil
}

func (i *mockInstaller) Supports(packageType string) (bool, error) {
	i.rec.add("supports %s", packageType)

	return i.supports(packageType), nil
}

func (i *mockInstaller) IsInstalled(_ repository.InstalledRepositoryInterface, p pkg.PackageInterface) (bool, error) {
	i.rec.add("isInstalled %s", p.String())

	return true, nil
}

func (i *mockInstaller) Download(p, _ pkg.PackageInterface) (*Promise, error) {
	i.rec.add("download %s", p.String())

	return i.ret("download", p)
}

func (i *mockInstaller) Prepare(typ string, p, _ pkg.PackageInterface) (*Promise, error) {
	i.rec.add("prepare %s %s", typ, p.String())

	return i.ret("prepare", p)
}

func (i *mockInstaller) Install(_ repository.InstalledRepositoryInterface, p pkg.PackageInterface) (*Promise, error) {
	i.rec.add("install %s", p.String())

	return i.ret("install", p)
}

func (i *mockInstaller) Update(_ repository.InstalledRepositoryInterface, initial, target pkg.PackageInterface) (*Promise, error) {
	i.rec.add("update %s %s", initial.String(), target.String())

	return i.ret("update", target)
}

func (i *mockInstaller) Uninstall(_ repository.InstalledRepositoryInterface, p pkg.PackageInterface) (*Promise, error) {
	i.rec.add("uninstall %s", p.String())

	return i.ret("uninstall", p)
}

func (i *mockInstaller) Cleanup(typ string, p, _ pkg.PackageInterface) (*Promise, error) {
	i.rec.add("cleanup %s %s", typ, p.String())

	return i.ret("cleanup", p)
}

func (i *mockInstaller) InstallPath(p pkg.PackageInterface) (string, bool, error) {
	return "/install/" + p.Name(), true, nil
}

// mockBinaries is a BinaryInstaller mock.
type mockBinaries struct{ rec *recorder }

func (b *mockBinaries) InstallBinaries(p pkg.PackageInterface, installPath string, warnOnOverwrite bool) error {
	b.rec.add("installBinaries %s %s %v", p.String(), installPath, warnOnOverwrite)

	return nil
}

func (b *mockBinaries) RemoveBinaries(p pkg.PackageInterface) error {
	b.rec.add("removeBinaries %s", p.String())

	return nil
}

func equalCalls(t *testing.T, got, want []string) {
	t.Helper()

	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("calls:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
