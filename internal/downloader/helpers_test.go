package downloader

import (
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/stubbedev/maestro/internal/config"
	"github.com/stubbedev/maestro/internal/eventdispatcher"
	mio "github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/http"
)

// configAdapter gives *config.Config the downloaders' Get(key).
type configAdapter struct{ c *config.Config }

func (a configAdapter) Get(key string) any {
	v, _ := a.c.Get(key, 0)

	return v
}

// getConfig is TestCase::getConfig($options).
func getConfig(t *testing.T, kv ...any) Config {
	t.Helper()

	c := config.New(false, "")
	if err := c.Merge(php.ArrayOf("config", php.ArrayOf(kv...)), "test"); err != nil {
		t.Fatal(err)
	}

	return configAdapter{c}
}

// getPackage is TestCase::getPackage($name, $version).
func getPackage(name, version, prettyVersion string) *pkg.CompletePackage {
	return pkg.NewCompletePackage(name, version, prettyVersion)
}

func dummyPackage() *pkg.CompletePackage {
	return getPackage("dummy/pkg", "1.0.0.0", "1.0.0")
}

// bufferIO is a BufferIO at the given console verbosity.
func bufferIO(t *testing.T, verbosity int) *mio.BufferIO {
	t.Helper()

	b, err := mio.NewBufferIO("", verbosity, nil)
	if err != nil {
		t.Fatal(err)
	}

	return b
}

func nullIO() mio.IO { return mio.NewNullIO() }

// fakeHTTP is an HttpDownloader mock: addCopy answers with copy, which by
// default resolves with an empty response without writing the file.
type fakeHTTP struct {
	copy  func(url, to string) (*http.Response, error)
	mu    sync.Mutex
	calls []string
}

func (f *fakeHTTP) AddCopy(url, to string, _ *php.Array) (*util.Promise[*http.Response], error) {
	f.mu.Lock()
	f.calls = append(f.calls, url)
	f.mu.Unlock()

	if f.copy == nil {
		return util.Resolved(http.NewResponse("http://example.org/", 200, nil, "file~")), nil
	}

	r, err := f.copy(url, to)
	if err != nil {
		return util.Rejected[*http.Response](err), nil
	}

	return util.Resolved(r), nil
}

func (f *fakeHTTP) Wait() {}

// fakeDispatcher is an EventDispatcher calling a listener.
type fakeDispatcher struct {
	listener func(eventdispatcher.Event) error
}

func (f *fakeDispatcher) Dispatch(_ string, e eventdispatcher.Event) (int, error) {
	if f.listener == nil {
		return 0, nil
	}

	return 0, f.listener(e)
}

// fakeCache is a Composer\Cache mock.
type fakeCache struct {
	t        *testing.T
	copyTo   func(key, target string) bool
	copyFrom func(key, source string) bool
	gc       func(ttl int, maxSize int64)
	gcNeeded bool
	readOnly bool
}

func (c *fakeCache) IsEnabled() bool  { return true }
func (c *fakeCache) IsReadOnly() bool { return c.readOnly }

func (c *fakeCache) Sha1(string) (string, bool, error) { return "", false, nil }

func (c *fakeCache) CopyTo(key, target string) (bool, error) {
	if c.copyTo == nil {
		return false, nil
	}

	return c.copyTo(key, target), nil
}

func (c *fakeCache) CopyFrom(key, source string) (bool, error) {
	if c.copyFrom == nil {
		return false, nil
	}

	return c.copyFrom(key, source), nil
}

func (c *fakeCache) Remove(string) (bool, error) { return true, nil }
func (c *fakeCache) GcIsNecessary() bool         { return c.gcNeeded }

func (c *fakeCache) Gc(ttl int, maxSize int64) (bool, error) {
	if c.gc != nil {
		c.gc(ttl, maxSize)
	}

	return true, nil
}

// asyncProcess is a ProcessExecutor with async enabled, as a Loop's.
func asyncProcess() *util.ProcessExecutor {
	p := util.NewProcessExecutor(nil)
	p.EnableAsync()

	return p
}

func mustContain(t *testing.T, err error, want string) {
	t.Helper()

	if err == nil {
		t.Fatalf("expected an error containing %q, got none", want)
	}

	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q does not contain %q", err.Error(), want)
	}
}

func writeFile(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()

	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
}

func outputLines(b *mio.BufferIO) []string {
	out := strings.TrimRight(b.Output(), "\n")
	if out == "" {
		return nil
	}

	return strings.Split(out, "\n")
}
