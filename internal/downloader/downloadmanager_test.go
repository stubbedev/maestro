package downloader

import (
	"errors"
	"slices"
	"testing"

	"github.com/stubbedev/maestro/internal/console"
	mio "github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/util"
)

// Ports tests/Composer/Test/Downloader/DownloadManagerTest.php. PHPUnit's
// partial mocks of getDownloader/getDownloaderForPackage become downloaders
// registered for the package's types.

// fakeDownloader is a DownloaderInterface mock recording its calls.
type fakeDownloader struct {
	source   string
	download func() (*Promise, error)
	prepare  func() (*Promise, error)
	calls    []string
	paths    []string
}

func (f *fakeDownloader) InstallationSource() string { return f.source }

func (f *fakeDownloader) record(call, path string) {
	f.calls = append(f.calls, call)
	f.paths = append(f.paths, path)
}

func (f *fakeDownloader) Download(_ pkg.PackageInterface, path string, _ pkg.PackageInterface) (*Promise, error) {
	f.record("download", path)

	if f.download != nil {
		return f.download()
	}

	return resolved(""), nil
}

func (f *fakeDownloader) Prepare(typ string, _ pkg.PackageInterface, path string, _ pkg.PackageInterface) (*Promise, error) {
	f.record("prepare:"+typ, path)

	if f.prepare != nil {
		return f.prepare()
	}

	return resolved(""), nil
}

func (f *fakeDownloader) Install(_ pkg.PackageInterface, path string) (*Promise, error) {
	f.record("install", path)

	return resolved(""), nil
}

func (f *fakeDownloader) Update(_, _ pkg.PackageInterface, path string) (*Promise, error) {
	f.record("update", path)

	return resolved(""), nil
}

func (f *fakeDownloader) Remove(_ pkg.PackageInterface, path string) (*Promise, error) {
	f.record("remove", path)

	return resolved(""), nil
}

func (f *fakeDownloader) Cleanup(typ string, _ pkg.PackageInterface, path string, _ pkg.PackageInterface) (*Promise, error) {
	f.record("cleanup:"+typ, path)

	return resolved(""), nil
}

func (f *fakeDownloader) expectCalls(t *testing.T, calls ...string) {
	t.Helper()

	if !slices.Equal(f.calls, calls) {
		t.Fatalf("calls %q, want %q", f.calls, calls)
	}
}

// dmPackage is a package mock: a dev package when dev, with the given
// source and dist types ("" for null).
func dmPackage(name string, dev bool, sourceType, distType string) *pkg.Package {
	version := "1.0.0.0"
	if dev {
		version = "dev-master"
	}

	p := pkg.NewPackage(name, version, version)
	p.SetSourceType(pkg.NonEmpty(sourceType))
	p.SetDistType(pkg.NonEmpty(distType))

	return p
}

func newManager(t *testing.T) (*DownloadManager, *mio.BufferIO) {
	t.Helper()

	out := bufferIO(t, console.VerbosityDebug)

	return NewDownloadManager(out, false, nil), out
}

func TestDownloadManager_SetGetDownloader(t *testing.T) {
	d := &fakeDownloader{source: "dist"}
	m, _ := newManager(t)

	m.SetDownloader("test", d)

	if got, err := m.Downloader("test"); err != nil || got != d {
		t.Fatalf("got %v, %v", got, err)
	}

	_, err := m.Downloader("unregistered")
	if _, ok := errors.AsType[*util.InvalidArgumentError](err); !ok {
		t.Fatalf("expected InvalidArgumentException, got %v", err)
	}

	if err.Error() != "Unknown downloader type: unregistered. Available types: test." {
		t.Fatalf("unexpected message %q", err)
	}
}

func TestDownloadManager_GetDownloaderForIncorrectlyInstalledPackage(t *testing.T) {
	m, _ := newManager(t)

	_, err := m.DownloaderForPackage(dmPackage("a/b", false, "", ""))
	if _, ok := errors.AsType[*util.InvalidArgumentError](err); !ok {
		t.Fatalf("expected InvalidArgumentException, got %v", err)
	}
}

func TestDownloadManager_GetDownloaderForCorrectlyInstalledDistPackage(t *testing.T) {
	p := dmPackage("a/b", false, "", "pear")
	p.SetInstallationSource(pkg.Str("dist"))

	d := &fakeDownloader{source: "dist"}
	m, _ := newManager(t)
	m.SetDownloader("pear", d)

	if got, err := m.DownloaderForPackage(p); err != nil || got != d {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestDownloadManager_GetDownloaderForIncorrectlyInstalledDistPackage(t *testing.T) {
	p := dmPackage("a/b", false, "", "git")
	p.SetInstallationSource(pkg.Str("dist"))

	m, _ := newManager(t)
	m.SetDownloader("git", &fakeDownloader{source: "source"})

	_, err := m.DownloaderForPackage(p)
	if _, ok := errors.AsType[*util.LogicError](err); !ok {
		t.Fatalf("expected LogicException, got %v", err)
	}
}

func TestDownloadManager_GetDownloaderForCorrectlyInstalledSourcePackage(t *testing.T) {
	p := dmPackage("a/b", false, "git", "")
	p.SetInstallationSource(pkg.Str("source"))

	d := &fakeDownloader{source: "source"}
	m, _ := newManager(t)
	m.SetDownloader("git", d)

	if got, err := m.DownloaderForPackage(p); err != nil || got != d {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestDownloadManager_GetDownloaderForIncorrectlyInstalledSourcePackage(t *testing.T) {
	p := dmPackage("a/b", false, "pear", "")
	p.SetInstallationSource(pkg.Str("source"))

	m, _ := newManager(t)
	m.SetDownloader("pear", &fakeDownloader{source: "dist"})

	_, err := m.DownloaderForPackage(p)
	if _, ok := errors.AsType[*util.LogicError](err); !ok {
		t.Fatalf("expected LogicException, got %v", err)
	}
}

func TestDownloadManager_GetDownloaderForMetapackage(t *testing.T) {
	p := dmPackage("a/b", false, "", "")
	p.SetType("metapackage")

	m, _ := newManager(t)

	if d, err := m.DownloaderForPackage(p); d != nil || err != nil {
		t.Fatalf("got %v, %v", d, err)
	}
}

// downloadCase runs DownloadManager::download for a package with the
// given types, the git and pear downloaders registered, and returns the
// installation sources the package went through.
type downloadCase struct {
	sourceType, distType string
	dev                  bool
	preferSource         bool
	preferences          *php.Array
	name                 string
}

func runDownload(t *testing.T, c downloadCase, git, pear *fakeDownloader) (*pkg.Package, error) {
	t.Helper()

	name := c.name
	if name == "" {
		name = "a/b"
	}

	p := dmPackage(name, c.dev, c.sourceType, c.distType)

	m, _ := newManager(t)
	m.SetPreferSource(c.preferSource)

	if c.preferences != nil {
		if _, err := m.SetPreferences(c.preferences); err != nil {
			t.Fatal(err)
		}
	}

	m.SetDownloader("git", git)
	m.SetDownloader("pear", pear)

	err := await(m.Download(p, "target_dir", nil))

	return p, err
}

func expectDownloaded(t *testing.T, c downloadCase, wantSource string) {
	t.Helper()

	git := &fakeDownloader{source: "source"}
	pear := &fakeDownloader{source: "dist"}

	p, err := runDownload(t, c, git, pear)
	if err != nil {
		t.Fatal(err)
	}

	if got := p.InstallationSource(); got != pkg.Str(wantSource) {
		t.Fatalf("installation source %v, want %s", got, wantSource)
	}

	used, unused := pear, git
	if wantSource == "source" {
		used, unused = git, pear
	}

	used.expectCalls(t, "download")
	unused.expectCalls(t)

	if used.paths[0] != "target_dir" {
		t.Fatalf("downloaded to %q", used.paths[0])
	}
}

func TestDownloadManager_FullPackageDownload(t *testing.T) {
	expectDownloaded(t, downloadCase{sourceType: "git", distType: "pear"}, "dist")
}

func TestDownloadManager_FullPackageDownloadFailover(t *testing.T) {
	git := &fakeDownloader{source: "source"}
	pear := &fakeDownloader{source: "dist", download: func() (*Promise, error) {
		return nil, &util.RuntimeError{Message: "Foo"}
	}}

	p := dmPackage("a/b", false, "git", "pear")
	m, _ := newManager(t)
	m.SetDownloader("git", git)
	m.SetDownloader("pear", pear)

	// Source fallback now defaults to false; opt in to exercise failover.
	m.SetSourceFallback(true)

	if err := await(m.Download(p, "target_dir", nil)); err != nil {
		t.Fatal(err)
	}

	pear.expectCalls(t, "download")
	git.expectCalls(t, "download")

	if p.InstallationSource() != pkg.Str("source") {
		t.Fatalf("installation source %v", p.InstallationSource())
	}
}

func TestDownloadManager_BadPackageDownload(t *testing.T) {
	_, err := runDownload(t, downloadCase{}, &fakeDownloader{source: "source"}, &fakeDownloader{source: "dist"})
	if _, ok := errors.AsType[*util.InvalidArgumentError](err); !ok {
		t.Fatalf("expected InvalidArgumentException, got %v", err)
	}
}

func TestDownloadManager_DistOnlyPackageDownload(t *testing.T) {
	expectDownloaded(t, downloadCase{distType: "pear"}, "dist")
}

func TestDownloadManager_SourceOnlyPackageDownload(t *testing.T) {
	expectDownloaded(t, downloadCase{sourceType: "git"}, "source")
}

func TestDownloadManager_MetapackagePackageDownload(t *testing.T) {
	p := dmPackage("a/b", false, "git", "")
	p.SetType("metapackage")

	m, _ := newManager(t)
	git := &fakeDownloader{source: "source"}
	m.SetDownloader("git", git)

	if err := await(m.Download(p, "target_dir", nil)); err != nil {
		t.Fatal(err)
	}

	if p.InstallationSource() != pkg.Str("source") {
		t.Fatalf("installation source %v", p.InstallationSource())
	}

	// There is no downloader for Metapackages.
	git.expectCalls(t)
}

func TestDownloadManager_FullPackageDownloadWithSourcePreferred(t *testing.T) {
	expectDownloaded(t, downloadCase{sourceType: "git", distType: "pear", preferSource: true}, "source")
}

func TestDownloadManager_DistOnlyPackageDownloadWithSourcePreferred(t *testing.T) {
	expectDownloaded(t, downloadCase{distType: "pear", preferSource: true}, "dist")
}

func TestDownloadManager_SourceOnlyPackageDownloadWithSourcePreferred(t *testing.T) {
	expectDownloaded(t, downloadCase{sourceType: "git", preferSource: true}, "source")
}

func TestDownloadManager_BadPackageDownloadWithSourcePreferred(t *testing.T) {
	_, err := runDownload(t, downloadCase{preferSource: true}, &fakeDownloader{source: "source"}, &fakeDownloader{source: "dist"})
	if _, ok := errors.AsType[*util.InvalidArgumentError](err); !ok {
		t.Fatalf("expected InvalidArgumentException, got %v", err)
	}
}

const bundlePath = "vendor/bundles/FOS/UserBundle"

func installed(p *pkg.Package, source string) *pkg.Package {
	p.SetInstallationSource(pkg.Str(source))

	return p
}

func TestDownloadManager_UpdateDistWithEqualTypes(t *testing.T) {
	initial := installed(dmPackage("a/b", false, "", "zip"), "dist")
	target := installed(dmPackage("a/b", false, "", "zip"), "dist")

	zip := &fakeDownloader{source: "dist"}
	m, _ := newManager(t)
	m.SetDownloader("zip", zip)

	if err := await(m.Update(initial, target, bundlePath)); err != nil {
		t.Fatal(err)
	}

	zip.expectCalls(t, "update")
}

func TestDownloadManager_UpdateDistWithNotEqualTypes(t *testing.T) {
	initial := installed(dmPackage("a/b", false, "", "xz"), "dist")
	target := installed(dmPackage("a/b", false, "", "zip"), "dist")

	xz := &fakeDownloader{source: "dist"}
	zip := &fakeDownloader{source: "dist"}
	m, _ := newManager(t)
	m.SetDownloader("xz", xz)
	m.SetDownloader("zip", zip)

	if err := await(m.Update(initial, target, bundlePath)); err != nil {
		t.Fatal(err)
	}

	xz.expectCalls(t, "prepare:uninstall", "remove")
	zip.expectCalls(t, "install")
}

func TestDownloadManager_UpdateRunsRemovalGuardWhenDownloaderTypeChanges(t *testing.T) {
	initial := installed(dmPackage("a/b", false, "git", ""), "source")
	target := installed(dmPackage("a/b", false, "", "zip"), "dist")

	git := &fakeDownloader{source: "source"}
	zip := &fakeDownloader{source: "dist"}
	m, _ := newManager(t)
	m.SetDownloader("git", git)
	m.SetDownloader("zip", zip)

	if err := await(m.Update(initial, target, bundlePath)); err != nil {
		t.Fatal(err)
	}

	git.expectCalls(t, "prepare:uninstall", "remove")
	zip.expectCalls(t, "install")

	if zip.paths[0] != bundlePath {
		t.Fatalf("installed to %q", zip.paths[0])
	}
}

func TestDownloadManager_UpdateDoesNotWipeWhenRemovalGuardAborts(t *testing.T) {
	initial := installed(dmPackage("a/b", false, "git", ""), "source")
	target := installed(dmPackage("a/b", false, "", "zip"), "dist")

	git := &fakeDownloader{source: "source", prepare: func() (*Promise, error) {
		return nil, &util.RuntimeError{Message: "Source directory has uncommitted changes."}
	}}
	zip := &fakeDownloader{source: "dist"}
	m, _ := newManager(t)
	m.SetDownloader("git", git)
	m.SetDownloader("zip", zip)

	err := await(m.Update(initial, target, bundlePath))
	if _, ok := errors.AsType[*util.RuntimeError](err); !ok {
		t.Fatalf("expected RuntimeException, got %v", err)
	}

	git.expectCalls(t, "prepare:uninstall")
	zip.expectCalls(t)
}

func TestDownloadManager_GetAvailableSourcesUpdateSticksToSameSource(t *testing.T) {
	cases := []struct {
		prevPkgSource   string
		prevPkgIsDev    bool
		targetAvailable []string
		targetIsDev     bool
		expected        []string
	}{
		// updates keep previous source as preference
		{"source", false, []string{"source", "dist"}, false, []string{"source", "dist"}},
		{"dist", false, []string{"source", "dist"}, false, []string{"dist", "source"}},
		// updates do not keep previous source if target package does not have it
		{"source", false, []string{"dist"}, false, []string{"dist"}},
		{"dist", false, []string{"source"}, false, []string{"source"}},
		// updates do not keep previous source if target is dev and prev wasn't dev and installed from dist
		{"source", false, []string{"source", "dist"}, true, []string{"source", "dist"}},
		{"dist", false, []string{"source", "dist"}, true, []string{"source", "dist"}},
		// install picks the right default
		{"", false, []string{"source", "dist"}, true, []string{"source", "dist"}},
		{"", false, []string{"dist"}, true, []string{"dist"}},
		{"", false, []string{"source"}, true, []string{"source"}},
		{"", false, []string{"source", "dist"}, false, []string{"dist", "source"}},
		{"", false, []string{"dist"}, false, []string{"dist"}},
		{"", false, []string{"source"}, false, []string{"source"}},
	}

	for i, c := range cases {
		var initial pkg.PackageInterface
		if c.prevPkgSource != "" {
			initial = installed(dmPackage("a/b", c.prevPkgIsDev, "", ""), c.prevPkgSource)
		}

		sourceType, distType := "", ""
		if slices.Contains(c.targetAvailable, "source") {
			sourceType = "git"
		}

		if slices.Contains(c.targetAvailable, "dist") {
			distType = "zip"
		}

		target := dmPackage("a/b", c.targetIsDev, sourceType, distType)
		m, _ := newManager(t)

		got, err := m.availableSources(target, initial)
		if err != nil || !slices.Equal(got, c.expected) {
			t.Errorf("case %d: got %q, %v, want %q", i, got, err, c.expected)
		}
	}
}

func TestDownloadManager_UpdateMetapackage(t *testing.T) {
	initial := dmPackage("a/b", false, "", "")
	initial.SetType("metapackage")

	target := dmPackage("a/b", false, "", "")
	target.SetType("metapackage")

	m, _ := newManager(t)

	if err := await(m.Update(initial, target, "vendor/pkg")); err != nil {
		t.Fatal(err)
	}
}

func TestDownloadManager_Remove(t *testing.T) {
	p := installed(dmPackage("a/b", false, "", "pear"), "dist")

	pear := &fakeDownloader{source: "dist"}
	m, _ := newManager(t)
	m.SetDownloader("pear", pear)

	if err := await(m.Remove(p, bundlePath)); err != nil {
		t.Fatal(err)
	}

	pear.expectCalls(t, "remove")

	if pear.paths[0] != bundlePath {
		t.Fatalf("removed %q", pear.paths[0])
	}
}

func TestDownloadManager_MetapackageRemove(t *testing.T) {
	p := dmPackage("a/b", false, "", "")
	p.SetType("metapackage")

	m, _ := newManager(t)

	if err := await(m.Remove(p, bundlePath)); err != nil {
		t.Fatal(err)
	}
}

func TestDownloadManager_InstallPreferenceWithoutPreferenceDev(t *testing.T) {
	expectDownloaded(t, downloadCase{sourceType: "git", distType: "pear", dev: true}, "source")
}

func TestDownloadManager_InstallPreferenceWithoutPreferenceNoDev(t *testing.T) {
	expectDownloaded(t, downloadCase{sourceType: "git", distType: "pear"}, "dist")
}

func TestDownloadManager_InstallPreferenceWithoutMatchDev(t *testing.T) {
	expectDownloaded(t, downloadCase{sourceType: "git", distType: "pear", dev: true, name: "bar/package", preferences: php.ArrayOf("foo/*", "source")}, "source")
}

func TestDownloadManager_InstallPreferenceWithoutMatchNoDev(t *testing.T) {
	expectDownloaded(t, downloadCase{sourceType: "git", distType: "pear", name: "bar/package", preferences: php.ArrayOf("foo/*", "source")}, "dist")
}

func TestDownloadManager_InstallPreferenceWithMatchAutoDev(t *testing.T) {
	expectDownloaded(t, downloadCase{sourceType: "git", distType: "pear", dev: true, name: "foo/package", preferences: php.ArrayOf("foo/*", "auto")}, "source")
}

func TestDownloadManager_InstallPreferenceWithMatchAutoNoDev(t *testing.T) {
	expectDownloaded(t, downloadCase{sourceType: "git", distType: "pear", name: "foo/package", preferences: php.ArrayOf("foo/*", "auto")}, "dist")
}

func TestDownloadManager_InstallPreferenceWithMatchSource(t *testing.T) {
	expectDownloaded(t, downloadCase{sourceType: "git", distType: "pear", name: "foo/package", preferences: php.ArrayOf("foo/*", "source")}, "source")
}

func TestDownloadManager_InstallPreferenceWithMatchDist(t *testing.T) {
	expectDownloaded(t, downloadCase{sourceType: "git", distType: "pear", name: "foo/package", preferences: php.ArrayOf("foo/*", "dist")}, "dist")
}

func failingThenSucceeding(failing string) (git, pear *fakeDownloader) {
	fail := func() (*Promise, error) { return nil, &util.RuntimeError{Message: "Foo"} }
	git = &fakeDownloader{source: "source"}
	pear = &fakeDownloader{source: "dist"}

	if failing == "dist" {
		pear.download = fail
	} else {
		git.download = fail
	}

	return git, pear
}

func TestDownloadManager_DownloadFailsWithoutFallbackWhenDisabled(t *testing.T) {
	git, pear := failingThenSucceeding("dist")
	p := dmPackage("foo/bar", false, "git", "pear")

	m, out := newManager(t)
	m.SetDownloader("git", git)
	m.SetDownloader("pear", pear)

	// Disable source fallback
	m.SetSourceFallback(false)

	err := await(m.Download(p, "target_dir", nil))
	if err == nil || err.Error() != "Foo" {
		t.Fatalf("expected RuntimeException Foo, got %v", err)
	}

	want := []string{
		"    <warning>Failed to download foo/bar from dist: Foo</warning>",
		"    <warning>Source fallback is disabled. Not trying alternative sources.</warning>",
	}
	if got := outputLines(out); !slices.Equal(got, want) {
		t.Fatalf("output %q, want %q", got, want)
	}

	git.expectCalls(t)
}

func TestDownloadManager_DownloadFallsBackWhenEnabled(t *testing.T) {
	git, pear := failingThenSucceeding("dist")
	p := dmPackage("foo/bar", false, "git", "pear")

	m, out := newManager(t)
	m.SetDownloader("git", git)
	m.SetDownloader("pear", pear)

	// Explicitly enable source fallback (opt-in, deprecated)
	m.SetSourceFallback(true)

	if err := await(m.Download(p, "target_dir", nil)); err != nil {
		t.Fatal(err)
	}

	// 4 writeError calls fire: 2 deprecation warnings, "Failed to download
	// from dist", and "Now trying to download from source" during the
	// retry.
	want := []string{
		"<warning>The source-fallback option is deprecated and will be removed in Composer 2.11 as automatic fallback between dist and source has security implications.</warning>",
		"<warning>If you have a legitimate use case and do not want us to remove it in 2.11, please open an issue at https://github.com/composer/composer/issues to let us know.</warning>",
		"    <warning>Failed to download foo/bar from dist: Foo</warning>",
		"    <warning>Now trying to download from source</warning>",
	}
	if got := outputLines(out); !slices.Equal(got, want) {
		t.Fatalf("output %q, want %q", got, want)
	}

	if p.InstallationSource() != pkg.Str("source") {
		t.Fatalf("installation source %v", p.InstallationSource())
	}
}

func TestDownloadManager_DownloadFallsBackFromSourceToDistByDefault(t *testing.T) {
	git, pear := failingThenSucceeding("source")
	p := dmPackage("foo/bar", false, "git", "pear")

	m, out := newManager(t)
	m.SetPreferSource(true)
	m.SetDownloader("git", git)
	m.SetDownloader("pear", pear)

	// Do NOT call setSourceFallback, fallback to dist must work by default.
	if err := await(m.Download(p, "target_dir", nil)); err != nil {
		t.Fatal(err)
	}

	// "Failed to download from source" and "Now trying to download from
	// dist" during the retry; no deprecation warning, no "Source fallback
	// is disabled" warning.
	want := []string{
		"    <warning>Failed to download foo/bar from source: Foo</warning>",
		"    <warning>Now trying to download from dist</warning>",
	}
	if got := outputLines(out); !slices.Equal(got, want) {
		t.Fatalf("output %q, want %q", got, want)
	}

	git.expectCalls(t, "download")
	pear.expectCalls(t, "download")
}

func TestDownloadManager_UpdateFailurePromptsReinstall(t *testing.T) {
	initial := installed(dmPackage("a/b", false, "", "zip"), "dist")
	target := installed(dmPackage("a/b", false, "", "zip"), "dist")

	zip := &updateFailing{source: "dist"}
	m, _ := newManager(t)
	m.SetDownloader("zip", zip)

	// non-interactive: the update failure is thrown
	_, err := m.Update(initial, target, bundlePath)
	if err == nil || err.Error() != "boom" {
		t.Fatalf("expected the update failure, got %v", err)
	}
}

// updateFailing fails update() synchronously.
type updateFailing struct{ fakeDownloader }

func (u *updateFailing) Update(pkg.PackageInterface, pkg.PackageInterface, string) (*Promise, error) {
	return nil, &util.RuntimeError{Message: "boom"}
}
