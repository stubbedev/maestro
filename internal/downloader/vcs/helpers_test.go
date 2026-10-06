package vcs

import (
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/stubbedev/maestro/internal/config"
	"github.com/stubbedev/maestro/internal/downloader"
	mio "github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/http"
	"github.com/stubbedev/maestro/internal/util/processmock"
	vcsutil "github.com/stubbedev/maestro/internal/util/vcs"
)

// newConfig is `new Config()` with a temporary home (setupConfig) and the
// given config settings merged in.
func newConfig(t *testing.T, kv ...any) http.Config {
	t.Helper()

	c := config.New(false, "")
	settings := append([]any{"home", t.TempDir()}, kv...)

	if err := c.Merge(php.ArrayOf("config", php.ArrayOf(settings...)), "test"); err != nil {
		t.Fatal(err)
	}

	return c.ForHTTP()
}

// fakeFS is the PHPUnit mock of Composer\Util\Filesystem: emptyDirectory
// does nothing, removeDirectoryAsync resolves with true.
type fakeFS struct {
	mu      sync.Mutex
	removed []string
}

func (f *fakeFS) RemoveDirectory(string) (bool, error) { return true, nil }
func (f *fakeFS) EmptyDirectory(string, bool) error    { return nil }
func (f *fakeFS) RemoveDirectoryAsync(directory string) (*util.Promise[bool], error) {
	f.mu.Lock()
	f.removed = append(f.removed, directory)
	f.mu.Unlock()

	return util.Resolved(true), nil
}

// sourcePackage is a git package whose getSourceUrls() lists urls: the
// last is the source URL, the others preferred mirrors in order.
func sourcePackage(version, prettyVersion, ref string, urls ...string) *pkg.CompletePackage {
	p := pkg.NewCompletePackage("dummy/pkg", version, prettyVersion)
	p.SetSourceType(pkg.Str("git"))

	if ref != "" {
		p.SetSourceReference(pkg.Str(ref))
	}

	if len(urls) > 0 {
		p.SetSourceURL(pkg.Str(urls[len(urls)-1]))

		if len(urls) > 1 {
			mirrors := php.NewArray()
			// preferred mirrors are unshifted: add them in reverse
			for i := len(urls) - 2; i >= 0; i-- {
				mirrors.Append(php.ArrayOf("url", urls[i], "preferred", true))
			}

			p.SetSourceMirrors(mirrors)
		}
	}

	return p
}

// initGitVersion is GitDownloaderTest::initGitVersion: pins Git::$version
// for the test and resets it afterwards.
func initGitVersion(t *testing.T, version string) {
	t.Helper()

	vcsutil.SetVersion(version, true)
	t.Cleanup(func() { vcsutil.SetVersion("", false) })
}

// keepEnv restores the variables Git::cleanEnv changes after the test.
func keepEnv(t *testing.T) {
	t.Helper()

	for _, name := range []string{"GIT_TERMINAL_PROMPT", "GIT_ASKPASS", "LANGUAGE", "GIT_DIR", "GIT_WORK_TREE"} {
		if v, ok := os.LookupEnv(name); ok {
			t.Setenv(name, v)
		} else {
			t.Setenv(name, "")
			_ = os.Unsetenv(name) // restored by t.Setenv
		}
	}
}

func assertComplete(t *testing.T, m *processmock.Mock) {
	t.Helper()

	if err := m.AssertComplete(); err != nil {
		t.Fatal(err)
	}
}

func bufferIO(t *testing.T, verbosity int) *mio.BufferIO {
	t.Helper()

	b, err := mio.NewBufferIO("", verbosity, nil)
	if err != nil {
		t.Fatal(err)
	}

	return b
}

// run is the download/prepare/<op>/cleanup sequence of the PHP tests,
// stopping at the first error. Each promise is awaited (SyncHelper::await).
func run(steps ...func() (*downloader.Promise, error)) error {
	for _, step := range steps {
		promise, err := step()
		if err != nil {
			return err
		}

		if _, err := promise.Await(); err != nil {
			return err
		}
	}

	return nil
}

func wantError[E error](t *testing.T, err error, contains string) {
	t.Helper()

	if _, ok := errors.AsType[E](err); !ok {
		var target E
		t.Fatalf("expected a %T, got %v", target, err)
	}

	if !strings.Contains(err.Error(), contains) {
		t.Fatalf("error %q does not contain %q", err.Error(), contains)
	}
}

func noError(t *testing.T, err error) {
	t.Helper()

	if err != nil {
		t.Fatal(err)
	}
}

// installSteps are download, prepare, install and cleanup of an install.
func installSteps(d downloader.Downloader, p pkg.PackageInterface, path string) []func() (*downloader.Promise, error) {
	return []func() (*downloader.Promise, error){
		func() (*downloader.Promise, error) { return d.Download(p, path, nil) },
		func() (*downloader.Promise, error) { return d.Prepare("install", p, path, nil) },
		func() (*downloader.Promise, error) { return d.Install(p, path) },
		func() (*downloader.Promise, error) { return d.Cleanup("install", p, path, nil) },
	}
}

// updateSteps are download, prepare, update and cleanup of an update from
// initial to target.
func updateSteps(d downloader.Downloader, initial, target pkg.PackageInterface, path string) []func() (*downloader.Promise, error) {
	return []func() (*downloader.Promise, error){
		func() (*downloader.Promise, error) { return d.Download(target, path, initial) },
		func() (*downloader.Promise, error) { return d.Prepare("update", target, path, initial) },
		func() (*downloader.Promise, error) { return d.Update(initial, target, path) },
		func() (*downloader.Promise, error) { return d.Cleanup("update", target, path, initial) },
	}
}

// removeSteps are prepare, remove and cleanup of an uninstall.
func removeSteps(d downloader.Downloader, p pkg.PackageInterface, path string) []func() (*downloader.Promise, error) {
	return []func() (*downloader.Promise, error){
		func() (*downloader.Promise, error) { return d.Prepare("uninstall", p, path, nil) },
		func() (*downloader.Promise, error) { return d.Remove(p, path) },
		func() (*downloader.Promise, error) { return d.Cleanup("uninstall", p, path, nil) },
	}
}

// gitDir is a temporary working directory holding a .git directory.
// composerPath is what GitDownloader::normalizePath() makes of the
// relative "composerPath": itself, or on Windows the realpath() of its
// closest existing parent (the working directory) joined with a slash, as
// Composer's GitDownloaderTest expects.
func composerPath(t *testing.T) string {
	t.Helper()

	if !util.IsWindows() {
		return "composerPath"
	}

	cwd, err := util.GetCwd(false)
	noError(t, err)

	return util.Realpath(cwd) + "/composerPath"
}

func gitDir(t *testing.T, marker string) string {
	t.Helper()

	dir := t.TempDir()
	noError(t, os.MkdirAll(dir+"/"+marker, 0o755))

	return dir
}

type Promise = downloader.Promise
