package composerrepo

import (
	"errors"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util/http/httpmock"
)

// The cache directory is named after Url::sanitize($url), which throws on a
// URL that exhausts its backtrack limit. An empty name would put the
// repository cache at the cache root itself.
func TestComposerRepository_CacheNamePcreError(t *testing.T) {
	url := "https://" + strings.Repeat("a", 1100000) + "/@"

	_, err := New(php.ArrayOf("url", url), io.NewNullIO(), createConfig(t), httpmock.New(), nil)
	if !errors.As(err, new(*php.PcreError)) {
		t.Fatalf("got %v, want a *php.PcreError", err)
	}
}
