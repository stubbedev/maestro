// Test helpers shared by group B's command tests (show, outdated, depends,
// prohibits, licenses, fund, ...). The gb prefix keeps them apart from the
// other groups' helpers in package command_test.

package command_test

import (
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/command"
	"github.com/stubbedev/maestro/internal/command/commandtest"
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/semver"
)

// gbKV is an ordered list of ArrayInput parameters.
type gbKV []console.Param

func gbParams(pairs ...any) gbKV {
	out := make(gbKV, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, console.P(pairs[i].(string), pairs[i+1]))
	}

	return out
}

// merge is array_merge($a, $b) / $a + $b for string-keyed parameters:
// with override, later keys replace earlier values in place.
func gbMerge(a, b gbKV, override bool) gbKV {
	out := append(gbKV{}, a...)
	for _, p := range b {
		found := false
		for i := range out {
			if out[i].Key == p.Key {
				if override {
					out[i] = p
				}
				found = true
			}
		}
		if !found {
			out = append(out, p)
		}
	}

	return out
}

func gbRun(t *testing.T, ps gbKV) *commandtest.ApplicationTester {
	t.Helper()
	appTester := commandtest.GetApplicationTester(t)
	if _, err := appTester.Run(ps, commandtest.Options{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	return appTester
}

func gbRunErr(t *testing.T, ps gbKV) error {
	t.Helper()
	appTester := commandtest.GetApplicationTester(t)
	_, err := appTester.Run(ps, commandtest.Options{})
	if err == nil {
		t.Fatal("expected an error")
	}

	return err
}

func gbAssertSame(t *testing.T, expected, actual string) {
	t.Helper()
	if expected != actual {
		t.Errorf("mismatch\n--- expected\n%s\n--- actual\n%s", expected, actual)
	}
}

func gbContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Errorf("%q does not contain %q", haystack, needle)
	}
}

func gbNotContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if strings.Contains(haystack, needle) {
		t.Errorf("%q contains %q", haystack, needle)
	}
}

func gbPkgs(packages ...pkg.PackageInterface) []pkg.PackageInterface { return packages }

func gbTrim(a *commandtest.ApplicationTester) string { return php.Trim(a.Display(true)) }

func gbRepoPackage(kvs ...any) *php.Array { return php.ArrayOf(kvs...) }

func gbPackageRepo(packages ...*php.Array) *php.Array {
	list := php.NewArray()
	for _, p := range packages {
		list.Append(p)
	}

	return php.ArrayOf("packages", php.ArrayOf("type", "package", "package", list))
}

func gbRequireMap(pairs ...string) any {
	if len(pairs) == 0 {
		return php.NewObject()
	}
	a := php.NewArray()
	for i := 0; i+1 < len(pairs); i += 2 {
		a.Set(pairs[i], pairs[i+1])
	}

	return a
}

func gbLink(t *testing.T, source, target, version string) *pkg.Link {
	t.Helper()
	normalized, err := pkg.NewVersionParser().Normalize(version)
	if err != nil {
		t.Fatal(err)
	}
	// TestCase::getVersionConstraint('=', $version)
	c := semver.NewConstraintOp(semver.OpEQ, normalized)
	c.SetPrettyString("= " + version)

	return pkg.NewLink(source, target, c, pkg.TypeRequire, pkg.Str(version))
}

func gbItoa(i int) string { return php.ToString(int64(i)) }

// gbIsInvalidArgument is `$e instanceof \InvalidArgumentException`.
func gbIsInvalidArgument(err error) bool { return phperr.InstanceOf(err, command.ClassInvalidArgument) }
