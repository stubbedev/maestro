package composerrepo

import (
	"testing"

	"github.com/stubbedev/maestro/internal/config"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/json"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util/http/httpmock"
)

// createConfig is FactoryMock::createConfig(): a config whose home (and
// cache) is a fresh temporary directory, merged with extra config keys.
func createConfig(t *testing.T, kv ...any) *config.Config {
	t.Helper()

	home := t.TempDir()
	c := config.New(false, "")
	settings := php.ArrayOf(append([]any{"home", home, "cache-dir", home + "/cache"}, kv...)...)
	if err := c.Merge(php.ArrayOf("config", settings), "test"); err != nil {
		t.Fatal(err)
	}

	return c
}

// newRepo is new ComposerRepository($repoConfig, new NullIO(), $config,
// $httpDownloader).
func newRepo(t *testing.T, repoConfig *php.Array, cfg *config.Config, httpDownloader HTTPDownloader) *ComposerRepository {
	t.Helper()

	r, err := New(repoConfig, io.NewNullIO(), cfg, httpDownloader, nil)
	if err != nil {
		t.Fatal(err)
	}

	return r
}

// encode is JsonFile::encode($data).
func encode(t *testing.T, data any) string {
	t.Helper()

	s, err := json.EncodeDefault(data)
	if err != nil {
		t.Fatal(err)
	}

	return s
}

// mock is getHttpDownloaderMock()->expects($expectations, true).
func mock(t *testing.T, expectations ...httpmock.Expectation) *httpmock.Downloader {
	t.Helper()

	d := httpmock.New()
	d.Expects(expectations, true, nil)
	t.Cleanup(func() {
		if err := d.AssertComplete(); err != nil {
			t.Error(err)
		}
	})

	return d
}

// must panics on err, failing the test.
func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}

	return v
}

func verifyPeerOptions() *php.Array {
	return php.ArrayOf("http", php.ArrayOf("verify_peer", false))
}
