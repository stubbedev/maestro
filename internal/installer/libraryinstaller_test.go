package installer

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/util"
)

// libraryFixture is LibraryInstallerTest::setUp.
type libraryFixture struct {
	rootDir, vendorDir, binDir string
	composer                   *testComposer
	dm                         *mockDM
}

func newLibraryFixture(t *testing.T) (*libraryFixture, *testComposer) {
	t.Helper()

	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	// Composer's test joins with DIRECTORY_SEPARATOR.
	sep := string(filepath.Separator)
	f := &libraryFixture{rootDir: root, vendorDir: root + sep + "vendor", binDir: root + sep + "bin", dm: newMockDM()}
	mustMkdir(t, f.vendorDir)
	mustMkdir(t, f.binDir)

	c := &testComposer{cfg: newConfig(t, f.vendorDir, f.binDir)}
	f.composer = c

	return f, c
}

func (f *libraryFixture) installer(t *testing.T, typ pkg.NullString, fs *util.Filesystem, bi Binaries) *LibraryInstaller {
	t.Helper()

	l, err := NewLibraryInstaller(newBufferIO(t), f.composer, typ, fs, bi)
	if err != nil {
		t.Fatal(err)
	}

	l.downloadManager = f.dm

	return l
}

func TestLibraryInstaller_InstallerCreationShouldNotCreateVendorDirectory(t *testing.T) {
	f, _ := newLibraryFixture(t)
	_ = os.RemoveAll(f.vendorDir)

	f.installer(t, pkg.Str("library"), nil, nil)

	if fileExists(f.vendorDir) {
		t.Error("vendor dir created")
	}
}

func TestLibraryInstaller_InstallerCreationShouldNotCreateBinDirectory(t *testing.T) {
	f, _ := newLibraryFixture(t)
	_ = os.RemoveAll(f.binDir)

	f.installer(t, pkg.Str("library"), nil, nil)

	if fileExists(f.binDir) {
		t.Error("bin dir created")
	}
}

func TestLibraryInstaller_IsInstalled(t *testing.T) {
	f, _ := newLibraryFixture(t)
	library := f.installer(t, pkg.Str("library"), nil, nil)
	p := newPackage("test/pkg", "1.0.0")

	repo, err := repository.NewInstalledArrayRepository(nil)
	if err != nil {
		t.Fatal(err)
	}

	assertInstalled := func(want bool) {
		t.Helper()

		got, err := library.IsInstalled(repo, p)
		if err != nil || got != want {
			t.Fatalf("IsInstalled = %v, %v; want %v", got, err, want)
		}
	}

	assertInstalled(false)

	// package being in repo is not enough to be installed
	if err := repo.AddPackage(p); err != nil {
		t.Fatal(err)
	}

	assertInstalled(false)

	// package being in repo and vendor/pkg/foo dir present means it is
	// seen as installed
	mustMkdir(t, f.vendorDir+"/"+p.PrettyName())
	assertInstalled(true)

	if err := repo.RemovePackage(p); err != nil {
		t.Fatal(err)
	}

	assertInstalled(false)
}

func TestLibraryInstaller_IsInstalledDanglingSymlink(t *testing.T) {
	f, _ := newLibraryFixture(t)
	library := f.installer(t, pkg.Str("library"), nil, nil)
	p := newPackage("test/pkg", "1.0.0")

	repo, _ := repository.NewInstalledArrayRepository([]pkg.PackageInterface{p})

	mustMkdir(t, f.vendorDir+"/test")

	if err := os.Symlink(f.rootDir+"/nowhere", f.vendorDir+"/test/pkg"); err != nil {
		t.Fatal(err)
	}

	if got, err := library.IsInstalled(repo, p); err != nil || got {
		t.Fatalf("IsInstalled = %v, %v; want false", got, err)
	}

	mustMkdir(t, f.rootDir+"/nowhere")

	if got, err := library.IsInstalled(repo, p); err != nil || !got {
		t.Fatalf("IsInstalled = %v, %v; want true", got, err)
	}
}

func TestLibraryInstaller_Install(t *testing.T) {
	f, _ := newLibraryFixture(t)
	library := f.installer(t, pkg.Str("library"), nil, nil)
	p := newPackage("some/package", "1.0.0")
	repo := newMockRepo(t)

	promise, err := library.Install(repo, p)
	if err != nil {
		t.Fatal(err)
	}

	if err := Await(nil, promise); err != nil {
		t.Fatal(err)
	}

	equalCalls(t, f.dm.rec.list(), []string{"install some/package-1.0.0.0 " + f.vendorDir + "/some/package"})

	if n := repo.rec.count("addPackage some/package-1.0.0.0"); n != 1 {
		t.Errorf("addPackage called %d times", n)
	}

	if !fileExists(f.vendorDir) {
		t.Error("Vendor dir should be created")
	}

	if !fileExists(f.binDir) {
		t.Error("Bin dir should be created")
	}
}

func TestLibraryInstaller_Update(t *testing.T) {
	f, _ := newLibraryFixture(t)

	initial := newPackage("vendor/package1", "1.0.0")
	target := newPackage("vendor/package1", "2.0.0")

	initial.SetTargetDir(pkg.Str("oldtarget"))
	target.SetTargetDir(pkg.Str("newtarget"))

	// the filesystem's rename is real here: the initial dir is moved
	mustMkdir(t, f.vendorDir+"/vendor/package1/oldtarget")

	repo := newMockRepo(t, true, false, false)

	library := f.installer(t, pkg.Str("library"), nil, nil)

	promise, err := library.Update(repo, initial, target)
	if err != nil {
		t.Fatal(err)
	}

	if err := Await(nil, promise); err != nil {
		t.Fatal(err)
	}

	if !isDir(f.vendorDir+"/vendor/package1/newtarget") || fileExists(f.vendorDir+"/vendor/package1/oldtarget") {
		t.Error("initial dir not renamed to the target dir")
	}

	equalCalls(t, f.dm.rec.list(), []string{"update vendor/package1-1.0.0.0 vendor/package1-2.0.0.0 " + f.vendorDir + "/vendor/package1/newtarget"})

	if repo.rec.count("removePackage vendor/package1-1.0.0.0") != 1 || repo.rec.count("addPackage vendor/package1-2.0.0.0") != 1 {
		t.Errorf("repository calls: %v", repo.rec.list())
	}

	if !fileExists(f.vendorDir) || !fileExists(f.binDir) {
		t.Error("vendor and bin dirs should exist")
	}

	_, err = library.Update(repo, initial, target)

	var iae *util.InvalidArgumentError
	if !errors.As(err, &iae) || iae.Message != "Package is not installed: vendor/package1-1.0.0.0" {
		t.Errorf("err = %v", err)
	}

	if repo.hasCalls != 3 {
		t.Errorf("hasPackage called %d times, want 3", repo.hasCalls)
	}
}

func TestLibraryInstaller_Uninstall(t *testing.T) {
	f, _ := newLibraryFixture(t)
	library := f.installer(t, pkg.Str("library"), nil, nil)
	p := newPackage("vendor/pkg", "1.0.0")

	repo := newMockRepo(t, true, false)

	promise, err := library.Uninstall(repo, p)
	if err != nil {
		t.Fatal(err)
	}

	if err := Await(nil, promise); err != nil {
		t.Fatal(err)
	}

	equalCalls(t, f.dm.rec.list(), []string{"remove vendor/pkg-1.0.0.0 " + f.vendorDir + "/vendor/pkg"})

	if repo.rec.count("removePackage vendor/pkg-1.0.0.0") != 1 {
		t.Errorf("repository calls: %v", repo.rec.list())
	}

	_, err = library.Uninstall(repo, p)

	if _, ok := errors.AsType[*util.InvalidArgumentError](err); !ok {
		t.Errorf("err = %v, want InvalidArgumentException", err)
	}
}

func TestLibraryInstaller_GetInstallPathWithoutTargetDir(t *testing.T) {
	f, _ := newLibraryFixture(t)
	library := f.installer(t, pkg.Str("library"), nil, nil)
	p := newPackage("Vendor/Pkg", "1.0.0")

	if got, ok, err := library.InstallPath(p); err != nil || !ok || got != f.vendorDir+"/"+p.PrettyName() {
		t.Errorf("InstallPath = %q, %v, %v", got, ok, err)
	}
}

func TestLibraryInstaller_GetInstallPathWithTargetDir(t *testing.T) {
	f, _ := newLibraryFixture(t)
	library := f.installer(t, pkg.Str("library"), nil, nil)
	p := newPackage("Foo/Bar", "1.0.0")
	p.SetTargetDir(pkg.Str("Some/Namespace"))

	if got, ok, err := library.InstallPath(p); err != nil || !ok || got != f.vendorDir+"/"+p.PrettyName()+"/Some/Namespace" {
		t.Errorf("InstallPath = %q, %v, %v", got, ok, err)
	}
}

func TestLibraryInstaller_EnsureBinariesInstalled(t *testing.T) {
	f, _ := newLibraryFixture(t)
	bi := &mockBinaries{rec: &recorder{}}
	library := f.installer(t, pkg.Str("library"), nil, bi)
	p := newPackage("foo/bar", "1.0.0")

	if err := library.EnsureBinariesPresence(p); err != nil {
		t.Fatal(err)
	}

	installPath, _, _ := library.InstallPath(p)
	equalCalls(t, bi.rec.list(), []string{"installBinaries foo/bar-1.0.0.0 " + installPath + " false"})
}

// TestLibraryInstaller_PackageBasePath covers getPackageBasePath's
// target-dir stripping, which uninstall relies on.
func TestLibraryInstaller_PackageBasePath(t *testing.T) {
	f, _ := newLibraryFixture(t)
	library := f.installer(t, pkg.Str("library"), nil, nil)

	for _, tc := range []struct{ targetDir, want string }{
		{"", "/foo/bar"},
		{"Some/Namespace", "/foo/bar"},
		{"a.b{1}", "/foo/bar"},
		{"0", "/foo/bar"},
	} {
		p := newPackage("foo/bar", "1.0.0")
		if tc.targetDir != "" {
			p.SetTargetDir(pkg.Str(tc.targetDir))
		}

		got, err := library.PackageBasePath(p)
		if err != nil || got != f.vendorDir+tc.want {
			t.Errorf("%q: PackageBasePath = %q, %v", tc.targetDir, got, err)
		}
	}
}

// TestLibraryInstaller_Virtuals checks that the inherited methods call an
// overriding getInstallPath (composer/installers).
func TestLibraryInstaller_Virtuals(t *testing.T) {
	f, _ := newLibraryFixture(t)
	library := f.installer(t, pkg.NullString{}, nil, nil)
	library.SetVirtuals(&customPath{LibraryInstaller: library, path: f.rootDir + "/custom/place"})

	p := newPackage("foo/bar", "1.0.0")
	repo := newMockRepo(t)

	promise, err := library.Install(repo, p)
	if err != nil {
		t.Fatal(err)
	}

	if err := Await(nil, promise); err != nil {
		t.Fatal(err)
	}

	equalCalls(t, f.dm.rec.list(), []string{"install foo/bar-1.0.0.0 " + f.rootDir + "/custom/place"})

	if ok, _ := library.Supports("anything"); !ok {
		t.Error("null type should support every type")
	}
}

type customPath struct {
	*LibraryInstaller
	path string
}

func (c *customPath) InstallPath(pkg.PackageInterface) (string, bool, error) {
	return c.path, true, nil
}

func TestLibraryInstaller_WithoutDownloadManager(t *testing.T) {
	f, _ := newLibraryFixture(t)

	library, err := NewLibraryInstaller(newBufferIO(t), f.composer, pkg.Str("library"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	_, err = library.Download(newPackage("foo/bar", "1.0.0"), nil)

	if _, ok := errors.AsType[*util.LogicError](err); !ok {
		t.Errorf("err = %v, want LogicException", err)
	}
}
