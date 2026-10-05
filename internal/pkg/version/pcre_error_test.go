package version_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg/internal/pkgtest"
	"github.com/stubbedev/maestro/internal/pkg/version"
)

// A COMPOSER_ROOT_VERSION long enough to exhaust the backtrack limit makes
// Preg::isMatch throw instead of matching.
func TestVersionGuesser_RootVersionFromEnvPcreError(t *testing.T) {
	t.Setenv("COMPOSER_ROOT_VERSION", strings.Repeat("1.", 600000))

	_, err := version.NewVersionGuesser(pkgtest.NewProcessExecutorMock(t), nil).RootVersionFromEnv()
	if _, ok := errors.AsType[*php.PcreError](err); !ok {
		t.Fatalf("got %v, want a *php.PcreError", err)
	}
}
