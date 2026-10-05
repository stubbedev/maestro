// Ports tests/Constraint/MultiConstraintTest.php.

package semver

import "testing"

func multiTestRequire(t *testing.T) (start, end *Constraint) {
	t.Helper()

	return mustConstraint(t, ">", "1.0"), mustConstraint(t, "<", "1.2")
}

func TestMultiConstraint_IsConjunctive(t *testing.T) {
	start, end := multiTestRequire(t)
	multi := mustMulti(t, true, start, end)
	if !multi.IsConjunctive() || multi.IsDisjunctive() {
		t.Error("should be conjunctive")
	}
}

func TestMultiConstraint_IsDisjunctive(t *testing.T) {
	start, end := multiTestRequire(t)
	multi := mustMulti(t, false, start, end)
	if multi.IsConjunctive() || !multi.IsDisjunctive() {
		t.Error("should be disjunctive")
	}
}

// assertAllMatch checks matches() both ways, the intersection and the
// compacted constraints; compiled additionally checks the compiled
// matcher with provider's operator and version.
func assertAllMatch(t *testing.T, want bool, require, provide ConstraintInterface, compiled *[2]string) {
	t.Helper()
	checks := []bool{
		require.Matches(provide),
		provide.Matches(require),
		Intervals.HaveIntersections(require, provide),
		Intervals.CompactConstraint(require).Matches(Intervals.CompactConstraint(provide)),
		Intervals.CompactConstraint(provide).Matches(Intervals.CompactConstraint(require)),
	}
	if compiled != nil {
		checks = append(checks, matchCompiled(t, require, compiled[0], compiled[1]))
	}
	for i, got := range checks {
		if got != want {
			t.Errorf("%s vs %s: assertion %d: got %v, want %v", require, provide, i, got, want)
		}
	}
}

func TestMultiConstraint_MultiVersionMatchSucceeds(t *testing.T) {
	start, end := multiTestRequire(t)
	assertAllMatch(t, true, mustMulti(t, true, start, end), mustConstraint(t, "==", "1.1"), &[2]string{"==", "1.1"})
}

func TestMultiConstraint_MultiVersionProvidedMatchSucceeds(t *testing.T) {
	start, end := multiTestRequire(t)
	multiProvide := mustMulti(t, true, mustConstraint(t, ">=", "1.1"), mustConstraint(t, "<", "2.0"))
	assertAllMatch(t, true, mustMulti(t, true, start, end), multiProvide, nil)
}

func TestMultiConstraint_MultiVersionMatchSucceedsInsideForeachLoop(t *testing.T) {
	start, end := multiTestRequire(t)
	multiProvide := mustMulti(t, false, mustConstraint(t, ">", "1.0"), mustConstraint(t, "<", "1.2"))
	assertAllMatch(t, true, mustMulti(t, false, start, end), multiProvide, nil)
}

func TestMultiConstraint_ConjunctiveMatchesDisjunctiveFalse(t *testing.T) {
	start, end := multiTestRequire(t)
	multiProvide := mustMulti(t, false, mustConstraint(t, "<", "1.0"), mustConstraint(t, ">", "2.0"))
	assertAllMatch(t, false, mustMulti(t, true, start, end), multiProvide, nil)
}

func TestMultiConstraint_MultiVersionMatchFails(t *testing.T) {
	start, end := multiTestRequire(t)
	assertAllMatch(t, false, mustMulti(t, true, start, end), mustConstraint(t, "==", "1.2"), &[2]string{"==", "1.2"})
}

func TestMultiConstraint_GetPrettyString(t *testing.T) {
	start, end := multiTestRequire(t)
	multi := mustMulti(t, true, start, end)
	multi.SetPrettyString("pretty-string")
	if got := multi.PrettyString(); got != "pretty-string" {
		t.Errorf("got %q", got)
	}
	multi.SetPrettyString("")
	if got := multi.PrettyString(); got != "[> 1.0 < 1.2]" {
		t.Errorf("got %q", got)
	}
}

func assertBounds(t *testing.T, name string, c ConstraintInterface, lower, upper Bound) {
	t.Helper()
	if got := c.LowerBound(); got != lower {
		t.Errorf("%s: Expected lower bound does not match: got %v, want %v", name, got, lower)
	}
	if got := c.UpperBound(); got != upper {
		t.Errorf("%s: Expected upper bound does not match: got %v, want %v", name, got, upper)
	}
}

func TestMultiConstraint_Bounds(t *testing.T) {
	for _, tc := range provider(t, "MultiConstraintTest::bounds") {
		var constraints []ConstraintInterface
		for _, c := range tc.args[0].(*phpArray).values {
			constraints = append(constraints, c.(ConstraintInterface))
		}
		multi, err := NewMultiConstraint(constraints, tc.args[1].(bool))
		if err != nil {
			t.Fatal(err)
		}
		assertBounds(t, tc.name, multi, tc.args[2].(Bound), tc.args[3].(Bound))
	}
}

func TestMultiConstraint_BoundsIntegrationWithVersionParser(t *testing.T) {
	for _, tc := range provider(t, "MultiConstraintTest::boundsIntegration") {
		assertBounds(t, tc.name, mustParse(t, tc.args[0].(string)), tc.args[1].(Bound), tc.args[2].(Bound))
	}
}

func TestMultiConstraint_MultipleMultiConstraintsMerging(t *testing.T) {
	var constraints []ConstraintInterface
	for _, s := range []string{"^7.0", "^7.2", "7.4.*", "7.2.* || 7.4.*"} {
		constraints = append(constraints, mustParse(t, s))
	}
	assertBounds(t, "merging", mustMulti(t, true, constraints...),
		NewBound("7.4.0.0-dev", true), NewBound("7.5.0.0-dev", false))
}

func TestMultiConstraint_MultipleMultiConstraintsMergingWithGaps(t *testing.T) {
	constraint := mustMulti(t, true, mustParse(t, "^7.1.15 || ^7.2.3"), mustParse(t, "^7.2.2"))
	assertBounds(t, "gaps", constraint, NewBound("7.2.2.0-dev", true), NewBound("8.0.0.0-dev", false))
}

func TestMultiConstraint_CreatesMatchAllConstraintIfNoneGiven(t *testing.T) {
	if _, ok := CreateMultiConstraint(nil, true).(*MatchAllConstraint); !ok {
		t.Error("expected a MatchAllConstraint")
	}
}

func TestMultiConstraint_MatchAllConstraintWithinConjunctiveMultiConstraint(t *testing.T) {
	c := CreateMultiConstraint([]ConstraintInterface{
		mustConstraint(t, ">=", "2.5.0.0-dev"), mustConstraint(t, "<=", "3.0.0.0-dev"), NewMatchAllConstraint(),
	}, true)
	if got := c.String(); got != "[>= 2.5.0.0-dev <= 3.0.0.0-dev *]" {
		t.Errorf("got %q", got)
	}
}

func TestMultiConstraint_MatchAllConstraintWithinDisjunctiveMultiConstraint(t *testing.T) {
	c := CreateMultiConstraint([]ConstraintInterface{mustConstraint(t, ">=", "2.5.0.0-dev"), NewMatchAllConstraint()}, false)
	if got := c.String(); got != "[>= 2.5.0.0-dev || *]" {
		t.Errorf("got %q", got)
	}
}

func TestMultiConstraint_MultiConstraintOptimizations(t *testing.T) {
	// We're using the version parser here because that uses MultiConstraint::create() internally and
	// thus tests our optimizations.
	for _, tc := range provider(t, "MultiConstraintTest::multiConstraintOptimizations") {
		assertParsesTo(t, tc.args[1].(ConstraintInterface).String(), tc.args[0].(string))
	}
}

func TestMultiConstraint_MultiConstraintNotconjunctiveFillWithFalse(t *testing.T) {
	versionProvide := mustConstraint(t, "==", "1.1")
	multiRequire := mustMulti(t, false,
		mustConstraint(t, ">", "dev-foo"), // always false
		mustConstraint(t, ">", "dev-bar"), // always false
	)
	if multiRequire.Matches(versionProvide) || versionProvide.Matches(multiRequire) ||
		matchCompiled(t, multiRequire, "==", "1.1") || Intervals.HaveIntersections(multiRequire, versionProvide) {
		t.Error("should not match")
	}
}

func TestMultiConstraint_MultiConstraintConjunctiveFillWithTrue(t *testing.T) {
	versionProvide := mustConstraint(t, "!=", "1.1")
	multiRequire := mustMulti(t, true,
		mustConstraint(t, "!=", "dev-foo"), // always true
		mustConstraint(t, "!=", "dev-bar"), // always true
	)
	if !multiRequire.Matches(versionProvide) || !versionProvide.Matches(multiRequire) ||
		!matchCompiled(t, multiRequire, "!=", "1.1") || !Intervals.HaveIntersections(multiRequire, versionProvide) {
		t.Error("should match")
	}
}
