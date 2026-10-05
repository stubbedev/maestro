// Ports tests/Constraint/ConstraintTest.php, MatchAllConstraintTest.php and
// MatchNoneConstraintTest.php.

package semver

import (
	"errors"
	"strings"
	"testing"
)

func TestConstraint_VersionCompareInvalidArgumentException(t *testing.T) {
	constraint := mustConstraint(t, "==", "1")
	_, err := constraint.VersionCompare("1.1", "1.2", "!==", false)
	if _, ok := errors.AsType[*InvalidArgumentError](err); !ok {
		t.Fatalf("expected *InvalidArgumentError, got %v", err)
	}
}

func TestConstraint_GetPrettyString(t *testing.T) {
	constraint := mustConstraint(t, "==", "1")
	constraint.SetPrettyString("pretty-string")
	if got := constraint.PrettyString(); got != "pretty-string" {
		t.Errorf("got %q", got)
	}

	constraint.SetPrettyString("")
	if got := constraint.PrettyString(); got != "== 1" {
		t.Errorf("got %q", got)
	}
}

func TestConstraint_VersionMatchSucceeds(t *testing.T) {
	for _, tc := range provider(t, "ConstraintTest::successfulVersionMatches") {
		requireOperator, requireVersion := tc.args[0].(string), tc.args[1].(string)
		provideOperator, provideVersion := tc.args[2].(string), tc.args[3].(string)
		versionRequire := mustConstraint(t, requireOperator, requireVersion)
		versionProvide := mustConstraint(t, provideOperator, provideVersion)

		checks := []bool{
			versionRequire.Matches(versionProvide),
			matchCompiled(t, versionRequire, provideOperator, provideVersion),
			Intervals.HaveIntersections(versionRequire, versionProvide),
			Intervals.CompactConstraint(versionRequire).Matches(Intervals.CompactConstraint(versionProvide)),
			// the operation should be commutative
			versionProvide.Matches(versionRequire),
			matchCompiled(t, versionProvide, requireOperator, requireVersion),
			Intervals.HaveIntersections(versionProvide, versionRequire),
			Intervals.CompactConstraint(versionProvide).Matches(Intervals.CompactConstraint(versionRequire)),
		}
		for i, ok := range checks {
			if !ok {
				t.Errorf("%v: assertion %d failed", tc.args, i)
			}
		}
	}
}

func TestConstraint_VersionMatchFails(t *testing.T) {
	for _, tc := range provider(t, "ConstraintTest::failingVersionMatches") {
		requireOperator, requireVersion := tc.args[0].(string), tc.args[1].(string)
		provideOperator, provideVersion := tc.args[2].(string), tc.args[3].(string)
		versionRequire := mustConstraint(t, requireOperator, requireVersion)
		versionProvide := mustConstraint(t, provideOperator, provideVersion)

		checks := []bool{
			versionRequire.Matches(versionProvide),
			matchCompiled(t, versionRequire, provideOperator, provideVersion),
			Intervals.CompactConstraint(versionRequire).Matches(Intervals.CompactConstraint(versionProvide)),
			// the operation should be commutative
			versionProvide.Matches(versionRequire),
			matchCompiled(t, versionProvide, requireOperator, requireVersion),
			Intervals.CompactConstraint(versionProvide).Matches(Intervals.CompactConstraint(versionRequire)),
		}
		// do not test intersections with >/</>=/<= for dev versions as these are not supported
		if !isUnsupportedDevRange(requireOperator, requireVersion) && !isUnsupportedDevRange(provideOperator, provideVersion) {
			checks = append(checks,
				Intervals.HaveIntersections(versionRequire, versionProvide),
				Intervals.HaveIntersections(versionProvide, versionRequire))
		}
		for i, ok := range checks {
			if ok {
				t.Errorf("%v: assertion %d failed", tc.args, i)
			}
		}
	}
}

func isUnsupportedDevRange(operator, version string) bool {
	return strings.HasPrefix(version, "dev-") && operator != "==" && operator != "!="
}

// The PHP test mocks MultiConstraint and MatchAllConstraint to check that
// Constraint::matches() hands other constraint types the matching. The
// interface is sealed in Go, so real instances stand in for the mocks:
// the result must be theirs.
func TestConstraint_InverseMatchingOtherConstraints(t *testing.T) {
	constraint := mustConstraint(t, ">", "1.0.0")
	others := []ConstraintInterface{
		mustMulti(t, true, mustConstraint(t, ">", "1.0.0"), mustConstraint(t, "<", "2.0.0")),
		NewMatchAllConstraint(),
		NewMatchNoneConstraint(),
	}
	for _, other := range others {
		if got, want := constraint.Matches(other), other.Matches(constraint); got != want {
			t.Errorf("%s: got %v, want %v", other, got, want)
		}
	}
}

func TestConstraint_ComparableBranches(t *testing.T) {
	versionProvide := mustConstraint(t, "==", "dev-foo")

	versionRequire := mustConstraint(t, ">", "0.12")
	if versionRequire.Matches(versionProvide) ||
		matchCompiled(t, versionRequire, "==", "dev-foo") ||
		Intervals.HaveIntersections(versionRequire, versionProvide) ||
		Intervals.CompactConstraint(versionRequire).Matches(Intervals.CompactConstraint(versionProvide)) ||
		versionRequire.MatchSpecific(versionProvide, true) {
		t.Error("> 0.12 should not match dev-foo")
	}

	versionRequire = mustConstraint(t, "<", "0.12")
	if versionRequire.Matches(versionProvide) ||
		matchCompiled(t, versionRequire, "==", "dev-foo") ||
		Intervals.HaveIntersections(versionRequire, versionProvide) ||
		Intervals.CompactConstraint(versionRequire).Matches(Intervals.CompactConstraint(versionProvide)) {
		t.Error("< 0.12 should not match dev-foo")
	}
	if !versionRequire.MatchSpecific(versionProvide, true) {
		t.Error("< 0.12 should match dev-foo with comparable branches")
	}
}

func TestConstraint_InvalidOperators(t *testing.T) {
	for _, tc := range provider(t, "ConstraintTest::invalidOperators") {
		_, err := NewConstraint(tc.args[1].(string), tc.args[0].(string))
		if _, ok := errors.AsType[*InvalidArgumentError](err); tc.args[2].(string) != "InvalidArgumentException" || !ok {
			t.Errorf("%v: got %v", tc.args, err)
		}
	}
}

func TestConstraint_Bounds(t *testing.T) {
	for _, tc := range provider(t, "ConstraintTest::bounds") {
		constraint := mustConstraint(t, tc.args[0].(string), tc.args[1].(string))
		if got := constraint.LowerBound(); got != tc.args[2].(Bound) {
			t.Errorf("%s: Expected lower bound does not match: got %v, want %v", tc.name, got, tc.args[2])
		}
		if got := constraint.UpperBound(); got != tc.args[3].(Bound) {
			t.Errorf("%s: Expected upper bound does not match: got %v, want %v", tc.name, got, tc.args[3])
		}
	}
}

func TestConstraint_Compile(t *testing.T) {
	for _, tc := range provider(t, "ConstraintTest::matrix") {
		requireOperator, requireVersion := tc.args[0].(string), tc.args[1].(string)
		provideOperator, provideVersion := tc.args[2].(string), tc.args[3].(string)
		require := mustConstraint(t, requireOperator, requireVersion)
		provide := mustConstraint(t, provideOperator, provideVersion)

		// Asserts Compiled version returns the same result than standard
		m := require.Matches(provide)
		if got := matchCompiled(t, require, provideOperator, provideVersion); got != m {
			t.Errorf("%v: compiled %v, matches %v", tc.args, got, m)
		}
		if got := Intervals.CompactConstraint(require).Matches(Intervals.CompactConstraint(provide)); got != m {
			t.Errorf("%v: compacted %v, matches %v", tc.args, got, m)
		}
		if got := matchCompiled(t, Intervals.CompactConstraint(require), provideOperator, provideVersion); got != m {
			t.Errorf("%v: compacted compiled %v, matches %v", tc.args, got, m)
		}

		// do not test >/</>=/<= for dev versions as these are not supported
		if isUnsupportedDevRange(requireOperator, requireVersion) || isUnsupportedDevRange(provideOperator, provideVersion) {
			continue
		}
		if got := Intervals.HaveIntersections(require, provide); got != m {
			t.Errorf("%v: intersections %v, matches %v", tc.args, got, m)
		}
	}
}

func TestMatchAllConstraint_Matches(t *testing.T) {
	if !NewMatchAllConstraint().Matches(mustConstraint(t, "==", "1.1")) {
		t.Error("MatchAllConstraint should match == 1.1")
	}
}

func TestMatchAllConstraint_GetPrettyString(t *testing.T) {
	c := NewMatchAllConstraint()
	c.SetPrettyString("pretty-string")
	if got := c.PrettyString(); got != "pretty-string" {
		t.Errorf("got %q", got)
	}
	c.SetPrettyString("")
	if got := c.PrettyString(); got != "*" {
		t.Errorf("got %q", got)
	}
}

func TestMatchNoneConstraint_Matches(t *testing.T) {
	c := NewMatchNoneConstraint()
	for _, p := range [][2]string{{"==", "1.1"}, {"!=", "1.1"}, {"==", "dev-foo"}, {"!=", "dev-foo"}} {
		if c.Matches(mustConstraint(t, p[0], p[1])) {
			t.Errorf("MatchNoneConstraint should not match %s %s", p[0], p[1])
		}
	}
}

func TestMatchNoneConstraint_GetPrettyString(t *testing.T) {
	c := NewMatchNoneConstraint()
	c.SetPrettyString("pretty-string")
	if got := c.PrettyString(); got != "pretty-string" {
		t.Errorf("got %q", got)
	}
	c.SetPrettyString("")
	if got := c.PrettyString(); got != "[]" {
		t.Errorf("got %q", got)
	}
}
