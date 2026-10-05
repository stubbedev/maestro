// Ports tests/Composer/Test/Downloader/FossilDownloaderTest.php.

package vcs

import (
	"os"
	"slices"
	"testing"

	mio "github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/processmock"
)

func fossilDownloader(t *testing.T, process *processmock.Mock, fs *fakeFS) *FossilDownloader {
	t.Helper()

	if process == nil {
		process = processmock.New()
	}

	if fs == nil {
		fs = &fakeFS{}
	}

	return NewFossilDownloader(Deps{IO: mio.NewNullIO(), Config: newConfig(t, "secure-http", false), Process: process, Filesystem: fs})
}

func fossilCheckout(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	noError(t, os.WriteFile(dir+"/.fslckout", nil, 0o644))

	return dir
}

func TestFossilDownloader_InstallForPackageWithoutSourceReference(t *testing.T) {
	_, err := fossilDownloader(t, nil, nil).Install(sourcePackage("1.0.0.0", "1.0.0", ""), t.TempDir()+"/path")
	wantError[*util.InvalidArgumentError](t, err, "missing reference information")
}

func TestFossilDownloader_Install(t *testing.T) {
	dir := t.TempDir()
	p := sourcePackage("1.0.0.0", "1.0.0", "trunk", "http://fossil.kd2.org/kd2fw/")

	process := processmock.New()
	process.Expects([]processmock.Expectation{
		processmock.Cmd("fossil", "clone", "--", "http://fossil.kd2.org/kd2fw/", dir+".fossil"),
		processmock.Cmd("fossil", "open", "--nested", "--", dir+".fossil"),
		processmock.Cmd("fossil", "update", "--", "trunk"),
	}, true, nil)

	noError(t, run(func() (*Promise, error) { return fossilDownloader(t, process, nil).Install(p, dir) }))
	assertComplete(t, process)
}

func TestFossilDownloader_UpdateforPackageWithoutSourceReference(t *testing.T) {
	initial := sourcePackage("1.0.0.0", "1.0.0", "trunk")
	target := sourcePackage("1.0.0.0", "1.0.0", "")

	err := run(updateSteps(fossilDownloader(t, nil, nil), initial, target, "/path")[1:]...)
	wantError[*util.InvalidArgumentError](t, err, "missing reference information")
}

func TestFossilDownloader_Update(t *testing.T) {
	dir := fossilCheckout(t)
	p := sourcePackage("1.0.0.0", "1.0.0", "trunk", "http://fossil.kd2.org/kd2fw/")

	process := processmock.New()
	process.Expects([]processmock.Expectation{
		processmock.Cmd("fossil", "changes"),
		processmock.Cmd("fossil", "pull"),
		processmock.Cmd("fossil", "up", "--", "trunk"),
	}, true, nil)

	noError(t, run(updateSteps(fossilDownloader(t, process, nil), p, p, dir)[1:]...))
	assertComplete(t, process)
}

func TestFossilDownloader_Remove(t *testing.T) {
	dir := fossilCheckout(t)
	p := sourcePackage("1.0.0.0", "1.0.0", "trunk")

	process := processmock.New()
	process.Expects([]processmock.Expectation{processmock.Cmd("fossil", "changes")}, true, nil)

	fs := &fakeFS{}
	noError(t, run(removeSteps(fossilDownloader(t, process, fs), p, dir)...))

	if !slices.Equal(fs.removed, []string{dir}) {
		t.Fatalf("removeDirectoryAsync calls: %v", fs.removed)
	}

	assertComplete(t, process)
}

func TestFossilDownloader_GetInstallationSource(t *testing.T) {
	if got := fossilDownloader(t, nil, nil).InstallationSource(); got != "source" {
		t.Fatalf("got %q", got)
	}
}
