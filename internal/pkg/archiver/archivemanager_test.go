package archiver_test

import (
	"os"
	"os/exec"
	"testing"

	"github.com/stubbedev/maestro/internal/downloader"
	mio "github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/archiver"
	"github.com/stubbedev/maestro/internal/resolver/operation"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/http"
)

// Ports tests/Composer/Test/Package/Archiver/ArchiveManagerTest.php.
//
// Composer's test builds the managers with its Factory and downloads the
// package with the real GitDownloader. Here the DownloadManager is real,
// with a "git" downloader that clones the source URL, and the loop waits
// for each promise.

// syncLoop is SyncHelper::await for promises that settle on their own.
type syncLoop struct{}

func (syncLoop) Await(promise http.Waitable, err error) error {
	if err != nil || promise == nil {
		return err
	}

	<-promise.Done()

	return promise.Err()
}

// cloneDownloader installs a git source by cloning it.
type cloneDownloader struct{}

func (cloneDownloader) InstallationSource() pkg.InstallationSource { return pkg.FromSource }
func (cloneDownloader) PHPClass() string                           { return "Mock_DownloaderInterface" }

func (cloneDownloader) Download(pkg.PackageInterface, string, pkg.PackageInterface) (*downloader.Promise, error) {
	return util.Resolved(""), nil
}

func (cloneDownloader) Prepare(operation.Type, pkg.PackageInterface, string, pkg.PackageInterface) (*downloader.Promise, error) {
	return util.Resolved(""), nil
}

func (cloneDownloader) Install(p pkg.PackageInterface, path string) (*downloader.Promise, error) {
	out, err := exec.Command("git", "clone", "-q", "-b", p.SourceReference().S, p.SourceURL().S, path).CombinedOutput()
	if err != nil {
		return nil, &util.RuntimeError{Message: string(out)}
	}

	return util.Resolved(""), nil
}

func (cloneDownloader) Update(pkg.PackageInterface, pkg.PackageInterface, string) (*downloader.Promise, error) {
	return util.Resolved(""), nil
}

func (cloneDownloader) Remove(pkg.PackageInterface, string) (*downloader.Promise, error) {
	return util.Resolved(""), nil
}

func (cloneDownloader) Cleanup(operation.Type, pkg.PackageInterface, string, pkg.PackageInterface) (*downloader.Promise, error) {
	return util.Resolved(""), nil
}

// newArchiveManager is the test's setUp: the manager Factory::
// createArchiveManager builds, and the test directory.
func newArchiveManager(t *testing.T) (*archiver.ArchiveManager, string) {
	t.Helper()

	dm := downloader.NewDownloadManager(mio.NewNullIO(), false, util.NewFilesystem(nil))
	dm.SetDownloader("git", cloneDownloader{})

	m := archiver.NewArchiveManager(dm.Sync(), syncLoop{})
	m.AddArchiver(archiver.NewZipArchiver())
	m.AddArchiver(archiver.NewPharArchiver())

	return m, util.Realpath(t.TempDir())
}

// setupPackage is ArchiverTestCase::setupPackage.
func setupPackage(testDir string) *pkg.CompletePackage {
	p := pkg.NewCompletePackage("archivertest/archivertest", "master", "master")
	p.SetSourceURL(pkg.Str(util.Realpath(testDir)))
	p.SetSourceReference(pkg.Str("master"))
	p.SetSourceType(pkg.Str("git"))

	return p
}

// setupGitRepo creates the local git repository the tests archive.
func setupGitRepo(t *testing.T, testDir string) {
	t.Helper()

	if err := os.WriteFile(testDir+"/composer.json", []byte(`{"name":"faker/faker", "description": "description", "license": "MIT"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("sh", "-c", `git init -q && git checkout -q -b master && git config user.email "you@example.com" && `+
		`git config commit.gpgsign false && git config user.name "Your Name" && git add composer.json && git commit -m "commit composer.json" -q`)
	cmd.Dir = testDir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "HOME="+t.TempDir())

	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git: %v\n%s", err, out)
	}
}

func skipIfNotExecutable(t *testing.T, name string) {
	t.Helper()

	if _, err := exec.LookPath(name); err != nil {
		t.Skip(name + " is not executable")
	}
}

func TestArchiveManager_UnknownFormat(t *testing.T) {
	m, testDir := newArchiveManager(t)

	_, err := m.Archive(setupPackage(testDir), "__unknown_format__", testDir+"/composer_archiver_tests", pkg.NullString{}, false)
	if _, ok := err.(*util.RuntimeError); !ok { //nolint:errorlint // the exception class
		t.Fatalf("error %v, want a RuntimeException", err)
	}
}

func TestArchiveManager_ArchiveTar(t *testing.T) {
	skipIfNotExecutable(t, "git")

	m, testDir := newArchiveManager(t)
	setupGitRepo(t, testDir)

	p := setupPackage(testDir)
	targetDir := testDir + "/composer_archiver_tests"

	if _, err := m.Archive(p, "tar", targetDir, pkg.NullString{}, false); err != nil {
		t.Fatal(err)
	}

	target := targetDir + "/" + m.PackageFilename(p) + ".tar"
	if _, err := os.Stat(target); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(os.TempDir() + "/composer_archiver/" + m.PackageFilename(p)); err == nil {
		t.Fatal("the temporary path exists")
	}
}

func TestArchiveManager_ArchiveCustomFileName(t *testing.T) {
	skipIfNotExecutable(t, "git")

	m, testDir := newArchiveManager(t)
	setupGitRepo(t, testDir)

	p := setupPackage(testDir)
	targetDir := testDir + "/composer_archiver_tests"

	if _, err := m.Archive(p, "tar", targetDir, pkg.Str("testArchiveName"), false); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(targetDir + "/testArchiveName.tar"); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(os.TempDir() + "/composer_archiver/" + m.PackageFilename(p)); err == nil {
		t.Fatal("the temporary path exists")
	}
}

func TestArchiveManager_GetPackageFilenameParts(t *testing.T) {
	m, testDir := newArchiveManager(t)

	want := []archiver.FilenamePart{
		{Key: "base", Value: "archivertest-archivertest"},
		{Key: "version", Value: "master"},
		{Key: "source_reference", Value: "4f26ae"},
	}

	got := m.PackageFilenameParts(setupPackage(testDir))
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestArchiveManager_GetPackageFilename(t *testing.T) {
	m, testDir := newArchiveManager(t)

	if got := m.PackageFilename(setupPackage(testDir)); got != "archivertest-archivertest-master-4f26ae" {
		t.Fatalf("got %q", got)
	}
}
