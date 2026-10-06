package loader_test

import (
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/loader"
)

// A minimum-stability that is not a string goes through
// normalizeStability's (string) cast (composer/semver declares no
// strict_types). Composer 2.10.3 on PHP 8.4.25, RootPackageLoader::load of
// ['name' => 'a/b', 'version' => '1.0.0', 'minimum-stability' => $v]
// under ErrorHandler::register().
func TestRootPackageLoader_MinimumStabilityNotString(t *testing.T) {
	for _, c := range []struct {
		value any
		want  string
		site  int
	}{
		{int64(1), `Invalid stability string "1", expected one of stable, RC, beta, alpha or dev`, 92},
		{true, `Invalid stability string "1", expected one of stable, RC, beta, alpha or dev`, 92},
		{false, `Invalid stability string "", expected one of stable, RC, beta, alpha or dev`, 92},
		{1.5, `Invalid stability string "1.5", expected one of stable, RC, beta, alpha or dev`, 92},
		{php.ListOf("x"), "Array to string conversion", 89},
	} {
		config := php.ArrayOf("name", "a/b", "version", "1.0.0", "minimum-stability", c.value)
		_, err := loader.NewRootPackageLoader(repositoryManagerStub{}, rootConfigStub{}, nil, nil, nil).Load(config, pkg.ClassRootPackage)
		if err == nil || err.Error() != c.want {
			t.Errorf("%v: got %v, want %s", c.value, err, c.want)

			continue
		}
		if site, _ := phperr.SiteOf(err); site.File != "VersionParser.php" || site.Line != c.site {
			t.Errorf("%v: site %s:%d", c.value, site.File, site.Line)
		}
	}
}
