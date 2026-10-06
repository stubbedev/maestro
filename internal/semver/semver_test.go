// Ports tests/SemverTest.php, tests/ComparatorTest.php and
// tests/CompilingMatcherTest.php.

package semver

import (
	"slices"
	"testing"
)

func TestSemver_Satisfies(t *testing.T) {
	for _, tc := range provider(t, "SemverTest::satisfiesProvider") {
		got, err := Semver.Satisfies(tc.args[1].(string), tc.args[2].(string))
		if err != nil || got != tc.args[0].(bool) {
			t.Errorf("Satisfies(%q, %q) = %v, %v; want %v", tc.args[1], tc.args[2], got, err, tc.args[0])
		}
	}
}

func TestSemver_SatisfiedBy(t *testing.T) {
	for _, tc := range provider(t, "SemverTest::satisfiedByProvider") {
		got, err := Semver.SatisfiedBy(tc.args[1].(*phpArray).strings(), tc.args[0].(string))
		if want := tc.args[2].(*phpArray).strings(); err != nil || !slices.Equal(got, want) {
			t.Errorf("SatisfiedBy(%v, %q) = %v, %v; want %v", tc.args[1], tc.args[0], got, err, want)
		}
	}
}

func TestSemver_Sort(t *testing.T) {
	for _, tc := range provider(t, "SemverTest::sortProvider") {
		versions := tc.args[0].(*phpArray).strings()
		if got, err := Semver.Sort(versions); err != nil || !slices.Equal(got, tc.args[1].(*phpArray).strings()) {
			t.Errorf("Sort(%v) = %v, %v", versions, got, err)
		}
		if got, err := Semver.Rsort(versions); err != nil || !slices.Equal(got, tc.args[2].(*phpArray).strings()) {
			t.Errorf("Rsort(%v) = %v, %v", versions, got, err)
		}
	}
}

func TestSemver_UsortShouldInitialVersionParserClass(t *testing.T) {
	versions := []string{"1.0", "2.0", "2.1"}
	result, err := semverUsort(versions, 1)
	if err != nil || len(result) != 3 || len(versions) != 3 {
		t.Errorf("got %v, %v", result, err)
	}
}

func comparatorTest(t *testing.T, providerName string, method func(string, string) bool) {
	t.Helper()
	for _, tc := range provider(t, "ComparatorTest::"+providerName) {
		if got := method(tc.args[0].(string), tc.args[1].(string)); got != tc.args[2].(bool) {
			t.Errorf("%v: got %v", tc.args, got)
		}
	}
}

func TestComparator_GreaterThan(t *testing.T) {
	comparatorTest(t, "greaterThanProvider", Comparator.GreaterThan)
}

func TestComparator_GreaterThanOrEqualTo(t *testing.T) {
	comparatorTest(t, "greaterThanOrEqualToProvider", Comparator.GreaterThanOrEqualTo)
}

func TestComparator_LessThan(t *testing.T) {
	comparatorTest(t, "lessThanProvider", Comparator.LessThan)
}

func TestComparator_LessThanOrEqualTo(t *testing.T) {
	comparatorTest(t, "lessThanOrEqualToProvider", Comparator.LessThanOrEqualTo)
}

func TestComparator_EqualTo(t *testing.T) {
	comparatorTest(t, "equalToProvider", Comparator.EqualTo)
}

func TestComparator_NotEqualTo(t *testing.T) {
	comparatorTest(t, "notEqualToProvider", Comparator.NotEqualTo)
}

func TestComparator_Compare(t *testing.T) {
	for _, tc := range provider(t, "ComparatorTest::compareProvider") {
		got, err := Comparator.Compare(tc.args[0].(string), tc.args[1].(string), tc.args[2].(string))
		if err != nil || got != tc.args[3].(bool) {
			t.Errorf("%v: got %v, %v", tc.args, got, err)
		}
	}
}

func TestCompilingMatcher_Match(t *testing.T) {
	if !CompilingMatcher.Match(mustConstraint(t, ">=", "1"), OpEQ, "2") {
		t.Error(">= 1 should match 2")
	}
}

// TestCompilingMatcher_Matcher checks Matcher against Match for every
// operator, constraints of each kind and versions including branches.
func TestCompilingMatcher_Matcher(t *testing.T) {
	parser := VersionParser{}
	versions := []string{"1.0.0.0", "1.2.3.0", "2.0.0.0-beta1", "2.0.0.0", "10.0.0.0", "dev-main", "1.0.x-dev", "9999999-dev", ""}
	for _, input := range []string{"*", "^1.0", "~1.2.3", ">=1.0 <2.0", "^1.0 || ^2.0", "dev-main", "!= 2.0", "<1.0 || >=10", "1.0.x-dev", "< 1.0 >= 2.0"} {
		constraint, err := parser.ParseConstraints(input)
		if err != nil {
			t.Fatal(err)
		}
		for _, op := range []Op{OpEQ, OpLT, OpLE, OpGT, OpGE, OpNE} {
			matches := CompilingMatcher.Matcher(constraint, op)
			for _, v := range versions {
				CompilingMatcher.Clear()
				if got, want := matches(v), CompilingMatcher.Match(constraint, op, v); got != want {
					t.Errorf("%q %d %q: %v, Match %v", input, op, v, got, want)
				}
			}
		}
	}
}

func TestCompilingMatcher_CacheKey(t *testing.T) {
	if CompilingMatcher.Match(mustConstraint(t, ">=", "2.11"), OpEQ, "1.0") {
		t.Error(">= 2.11 should not match 1.0")
	}
	if !CompilingMatcher.Match(mustConstraint(t, ">=", "2.1"), OpEQ, "11.0") {
		t.Error(">= 2.1 should match 11.0")
	}
}
