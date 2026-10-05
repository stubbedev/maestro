// Ports tests/IntervalsTest.php and tests/SubsetsTest.php.

package semver

import (
	"fmt"
	"testing"
)

const (
	intervalAnyTest      = "*/dev*"
	intervalAnyNoDevTest = "*"
	intervalNoneTest     = ""
	compactNoneTest      = ""
)

func TestIntervals_CompactConstraint(t *testing.T) {
	for _, tc := range provider(t, "IntervalsTest::compactProvider") {
		var parts []ConstraintInterface
		for _, part := range tc.args[1].(*phpArray).strings() {
			parts = append(parts, mustParse(t, part))
		}

		var expected ConstraintInterface
		if s := tc.args[0].(string); s == compactNoneTest {
			expected = NewMatchNoneConstraint()
		} else {
			expected = mustParse(t, s)
		}

		multi, err := NewMultiConstraint(parts, tc.args[2].(bool))
		if err != nil {
			t.Fatal(err)
		}
		if got := Intervals.CompactConstraint(multi).String(); got != expected.String() {
			t.Errorf("%s: got %q, want %q", tc.name, got, expected.String())
		}
	}
}

// intervalSetString renders an interval set the way the PHP test compares
// it: the string forms of the interval ends, and the branch names with
// their array keys.
func intervalSetString(numeric [][2]string, names [][2]any, exclude bool) string {
	return fmt.Sprintf("numeric=%q names=%v exclude=%v", numeric, names, exclude)
}

func TestIntervals_GetIntervals(t *testing.T) {
	anyNumeric := [][2]string{{">= 0.0.0.0-dev", "< " + positiveInfinityVersion}}
	for _, tc := range provider(t, "IntervalsTest::intervalsProvider") {
		constraint, ok := tc.args[1].(ConstraintInterface)
		if !ok {
			constraint = mustParse(t, tc.args[1].(string))
		}

		result := Intervals.Get(constraint)
		numeric := make([][2]string, 0)
		for _, interval := range result.Numeric {
			numeric = append(numeric, [2]string{interval.Start().String(), interval.End().String()})
		}
		names := make([][2]any, 0)
		for i, name := range result.Branches.Names {
			names = append(names, [2]any{int64(result.Branches.key(i)), name})
		}
		got := intervalSetString(numeric, names, result.Branches.Exclude)

		var want string
		switch expected := tc.args[0].(type) {
		case string:
			switch expected {
			case intervalAnyTest:
				want = intervalSetString(anyNumeric, [][2]any{}, true)
			case intervalAnyNoDevTest:
				want = intervalSetString(anyNumeric, [][2]any{}, false)
			case intervalNoneTest:
				want = intervalSetString([][2]string{}, [][2]any{}, false)
			}
		case *phpArray:
			wantNumeric := make([][2]string, 0)
			for _, interval := range expected.get("numeric").(*phpArray).values {
				i := interval.(*phpArray)
				wantNumeric = append(wantNumeric, [2]string{i.get("start").(string), i.get("end").(string)})
			}
			branches := expected.get("branches").(*phpArray)
			wantNames := make([][2]any, 0)
			n := branches.get("names").(*phpArray)
			for i, key := range n.keys {
				wantNames = append(wantNames, [2]any{key, n.values[i]})
			}
			want = intervalSetString(wantNumeric, wantNames, branches.get("exclude").(bool))
		}
		if got != want {
			t.Errorf("%s:\n got %s\nwant %s", tc.name, got, want)
		}
	}
}

func TestSubsets_IsSubsetOf(t *testing.T) {
	for _, tc := range provider(t, "SubsetsTest::subsets") {
		a, b := mustParse(t, tc.args[0].(string)), mustParse(t, tc.args[1].(string))
		if !Intervals.IsSubsetOf(a, b) {
			t.Errorf("%s (%s) should be seen as a subset of %s (%s)", tc.args[0], a, tc.args[1], b)
		}
	}
}

func TestSubsets_IsNotSubsetOf(t *testing.T) {
	for _, tc := range provider(t, "SubsetsTest::notSubsets") {
		a, b := mustParse(t, tc.args[0].(string)), mustParse(t, tc.args[1].(string))
		if Intervals.IsSubsetOf(a, b) {
			t.Errorf("%s (%s) should not be seen as a subset of %s (%s)", tc.args[0], a, tc.args[1], b)
		}
	}
}

func TestSubsets_MatchNoneIsNoSubsetNorSupersetExceptOfMatchAll(t *testing.T) {
	matchNone := NewMatchNoneConstraint()
	for _, constraint := range []string{"1.0.0", "^1.0", ">3", "<3", "dev-foo", "!= 1", "!= dev-foo", "<= dev-foo"} {
		c := mustParse(t, constraint)
		if Intervals.IsSubsetOf(c, matchNone) {
			t.Errorf("%s (%s) should not be seen as a subset of %s", constraint, c, matchNone)
		}
		if Intervals.IsSubsetOf(matchNone, c) {
			t.Errorf("%s should not be seen as a subset of %s (%s)", matchNone, constraint, c)
		}
	}

	empty := NewMatchAllConstraint()
	if Intervals.IsSubsetOf(empty, matchNone) {
		t.Errorf("%s should not be seen as a subset of %s", empty, matchNone)
	}
	if !Intervals.IsSubsetOf(matchNone, empty) {
		t.Errorf("%s should be seen as a subset of %s", matchNone, empty)
	}
}
