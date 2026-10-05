// Ports tests/Composer/Test/Downloader/HgDownloaderTest.php.

package vcs

import (
	"slices"
	"testing"

	mio "github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/processmock"
)

func hgDownloader(t *testing.T, process *processmock.Mock, fs *fakeFS) *HgDownloader {
	t.Helper()

	if process == nil {
		process = processmock.New()
	}

	if fs == nil {
		fs = &fakeFS{}
	}

	return NewHgDownloader(Deps{IO: mio.NewNullIO(), Config: newConfig(t), Process: process, Filesystem: fs})
}

func TestHgDownloader_DownloadForPackageWithoutSourceReference(t *testing.T) {
	_, err := hgDownloader(t, nil, nil).Install(sourcePackage("1.0.0.0", "1.0.0", ""), "/path")
	wantError[*util.InvalidArgumentError](t, err, "missing reference information")
}

func TestHgDownloader_Download(t *testing.T) {
	dir := t.TempDir()
	p := sourcePackage("1.0.0.0", "1.0.0", "ref", "https://mercurial.dev/l3l0/composer")

	process := processmock.New()
	process.Expects([]processmock.Expectation{
		processmock.Cmd("hg", "clone", "--", "https://mercurial.dev/l3l0/composer", dir),
		processmock.Cmd("hg", "up", "--", "ref"),
	}, true, nil)

	noError(t, run(func() (*Promise, error) { return hgDownloader(t, process, nil).Install(p, dir) }))
	assertComplete(t, process)
}

func TestHgDownloader_UpdateforPackageWithoutSourceReference(t *testing.T) {
	d := hgDownloader(t, nil, nil)
	initial := sourcePackage("1.0.0.0", "1.0.0", "ref")
	target := sourcePackage("1.0.0.0", "1.0.0", "")

	err := run(updateSteps(d, initial, target, "/path")[1:]...)
	wantError[*util.InvalidArgumentError](t, err, "missing reference information")
}

func TestHgDownloader_Update(t *testing.T) {
	dir := gitDir(t, ".hg")
	p := sourcePackage("1.0.0.0", "1.0.0", "ref", "https://github.com/l3l0/composer")

	process := processmock.New()
	process.Expects([]processmock.Expectation{
		processmock.Cmd("hg", "st"),
		processmock.Cmd("hg", "pull", "--", "https://github.com/l3l0/composer"),
		processmock.Cmd("hg", "up", "--", "ref"),
	}, true, nil)

	noError(t, run(updateSteps(hgDownloader(t, process, nil), p, p, dir)[1:]...))
	assertComplete(t, process)
}

func TestHgDownloader_Remove(t *testing.T) {
	dir := gitDir(t, ".hg")
	p := sourcePackage("1.0.0.0", "1.0.0", "ref")

	process := processmock.New()
	process.Expects([]processmock.Expectation{processmock.Cmd("hg", "st")}, true, nil)

	fs := &fakeFS{}
	noError(t, run(removeSteps(hgDownloader(t, process, fs), p, dir)...))

	if !slices.Equal(fs.removed, []string{dir}) {
		t.Fatalf("removeDirectoryAsync calls: %v", fs.removed)
	}

	assertComplete(t, process)
}

func TestHgDownloader_GetInstallationSource(t *testing.T) {
	if got := hgDownloader(t, nil, nil).InstallationSource(); got != "source" {
		t.Fatalf("got %q", got)
	}
}
