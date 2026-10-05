// Ports tests/Composer/Test/Package/Version/VersionBumperTest.php.

package version_test

import (
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/internal/pkgtest"
	"github.com/stubbedev/maestro/internal/pkg/version"
)

func TestVersionBumper_BumpRequirement(t *testing.T) {
	parser := pkg.NewVersionParser()

	for _, c := range pkgtest.Providers(t)["VersionBumperTest::provideBumpRequirementTests"] {
		requirement, prettyVersion, expected := c.Args.String(0), c.Args.String(1), c.Args.String(2)

		normalized, err := parser.Normalize(prettyVersion)
		if err != nil {
			t.Fatal(err)
		}

		p := pkg.NewPackage("foo/bar", normalized, prettyVersion)
		if c.Args.Len() > 3 {
			p.SetExtra(php.ArrayOf("branch-alias", php.ArrayOf(prettyVersion, c.Args.String(3))))
		}

		constraint, err := parser.ParseConstraints(requirement)
		if err != nil {
			t.Fatal(err)
		}

		got, err := version.VersionBumper{}.BumpRequirement(constraint, p)
		if err != nil || got != expected {
			t.Errorf("%s: got %q %v, want %q", c.Name, got, err, expected)
		}
	}
}
