// Ports tests/VersionParserTest.php.

package semver

import (
	"errors"
	"strings"
	"testing"
)

// expectUnexpectedValue asserts that err is an *UnexpectedValueError,
// containing message when it is not empty (expectExceptionMessage()).
func expectUnexpectedValue(t *testing.T, err error, message string) {
	t.Helper()
	uve, ok := errors.AsType[*UnexpectedValueError](err)
	if !ok {
		t.Fatalf("expected *UnexpectedValueError, got %T %v", err, err)
	}
	if !strings.Contains(uve.Message, message) {
		t.Fatalf("expected message containing %q, got %q", message, uve.Message)
	}
}

func TestVersionParser_ParseNumericAliasPrefix(t *testing.T) {
	for _, tc := range provider(t, "VersionParserTest::numericAliasVersions") {
		prefix, ok := VersionParser{}.ParseNumericAliasPrefix(tc.args[0].(string))
		if expected, isString := tc.args[1].(string); isString {
			if !ok || prefix != expected {
				t.Errorf("%s: got %q %v, want %q", tc.name, prefix, ok, expected)
			}
		} else if ok {
			t.Errorf("%s: got %q, want false", tc.name, prefix)
		}
	}
}

func TestVersionParser_NormalizeSucceeds(t *testing.T) {
	for _, tc := range provider(t, "VersionParserTest::successfulNormalizedVersions") {
		got, err := VersionParser{}.Normalize(tc.args[0].(string))
		if err != nil || got != tc.args[1].(string) {
			t.Errorf("%s: Normalize(%q) = %q, %v; want %q", tc.name, tc.args[0], got, err, tc.args[1])
		}
	}
}

func TestVersionParser_NormalizeFails(t *testing.T) {
	for _, tc := range provider(t, "VersionParserTest::failingNormalizedVersions") {
		_, err := VersionParser{}.Normalize(tc.args[0].(string))
		expectUnexpectedValue(t, err, "")
	}
}

// splitAlias is the regex the alias tests split their input with,
// '{^([^,\s#]+)(?:#[^ ]+)? +as +([^,\s]+)$}', for the inputs they use.
func splitAlias(t *testing.T, fullInput string) (source, alias string) {
	t.Helper()
	source, alias, ok := strings.Cut(fullInput, " as ")
	if !ok {
		t.Fatalf("%s did not match the regex", fullInput)
	}

	return strings.TrimSpace(source), strings.TrimSpace(alias)
}

func TestVersionParser_NormalizeFailsAndReportsAliasIssue(t *testing.T) {
	for _, tc := range provider(t, "VersionParserTest::failingNormalizedVersionsWithBadAlias") {
		fullInput := tc.args[0].(string)
		source, alias := splitAlias(t, fullInput)
		if _, err := (VersionParser{}).NormalizeWithFullVersion(source, fullInput); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		_, err := VersionParser{}.NormalizeWithFullVersion(alias, fullInput)
		want := "Invalid version string \"" + alias + "\" in \"" + fullInput + "\", the alias must be an exact version"
		if err == nil || err.Error() != want {
			t.Errorf("%s: got %v, want %q", tc.name, err, want)
		}
	}
}

func TestVersionParser_NormalizeFailsAndReportsAliaseeIssue(t *testing.T) {
	for _, tc := range provider(t, "VersionParserTest::failingNormalizedVersionsWithBadAliasee") {
		fullInput := tc.args[0].(string)
		source, alias := splitAlias(t, fullInput)
		_, err := VersionParser{}.NormalizeWithFullVersion(source, fullInput)
		want := "Invalid version string \"" + source + "\" in \"" + fullInput +
			"\", the alias source must be an exact version, if it is a branch name you should prefix it with dev-"
		if err == nil || err.Error() != want {
			t.Errorf("%s: got %v, want %q", tc.name, err, want)
		}
		if _, err := (VersionParser{}).NormalizeWithFullVersion(alias, fullInput); err != nil {
			t.Errorf("%s: %v", tc.name, err)
		}
	}
}

func TestVersionParser_NormalizeBranch(t *testing.T) {
	for _, tc := range provider(t, "VersionParserTest::successfulNormalizedBranches") {
		if got := (VersionParser{}).NormalizeBranch(tc.args[0].(string)); got != tc.args[1].(string) {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.args[1])
		}
	}
}

func assertParsesTo(t *testing.T, expected, input string) {
	t.Helper()
	if got := mustParse(t, input).String(); got != expected {
		t.Errorf("ParseConstraints(%q) = %q, want %q", input, got, expected)
	}
}

func TestVersionParser_ParseConstraintsIgnoresStabilityFlag(t *testing.T) {
	assertParsesTo(t, mustConstraint(t, "=", "1.0.0.0").String(), "1.0@dev")
	assertParsesTo(t, mustConstraint(t, ">=", "1.0.0.0-beta").String(), ">=1.0@beta")
	assertParsesTo(t, mustConstraint(t, "=", "dev-load-varnish-only-when-used").String(), "dev-load-varnish-only-when-used as ^2.0@dev")
	assertParsesTo(t, mustConstraint(t, "=", "dev-load-varnish-only-when-used").String(), "dev-load-varnish-only-when-used@dev as ^2.0@dev")
}

func TestVersionParser_ParseConstraintsIgnoresReferenceOnDevVersion(t *testing.T) {
	assertParsesTo(t, mustConstraint(t, "=", "1.0.9999999.9999999-dev").String(), "1.0.x-dev#abcd123")
	assertParsesTo(t, mustConstraint(t, "=", "1.0.9999999.9999999-dev").String(), "1.0.x-dev#trunk/@123")
}

func TestVersionParser_ParseConstraintsFailsOnBadReference(t *testing.T) {
	// PHPUnit stops at the first exception; both inputs fail.
	for _, input := range []string{"1.0#abcd123", "1.0#trunk/@123"} {
		_, err := VersionParser{}.ParseConstraints(input)
		expectUnexpectedValue(t, err, "")
	}
}

func TestVersionParser_ParseConstraintsNudgesRubyDevsTowardsThePathOfRighteousness(t *testing.T) {
	_, err := VersionParser{}.ParseConstraints("~>1.2")
	expectUnexpectedValue(t, err, "Invalid operator \"~>\", you probably meant to use the \"~\" operator")
}

func TestVersionParser_ParseConstraintsSimple(t *testing.T) {
	for _, tc := range provider(t, "VersionParserTest::simpleConstraints") {
		assertParsesTo(t, tc.args[1].(ConstraintInterface).String(), tc.args[0].(string))
	}
}

// rangeExpected builds the expected constraint of the wildcard, tilde,
// caret and hyphen tests: [min max], or max alone when min is null.
func rangeExpected(t *testing.T, minimum, maximum any) string {
	t.Helper()
	if minimum == nil {
		return maximum.(ConstraintInterface).String()
	}

	return mustMulti(t, true, minimum.(ConstraintInterface), maximum.(ConstraintInterface)).String()
}

func TestVersionParser_ParseConstraintsWildcard(t *testing.T) {
	for _, tc := range provider(t, "VersionParserTest::wildcardConstraints") {
		assertParsesTo(t, rangeExpected(t, tc.args[1], tc.args[2]), tc.args[0].(string))
	}
}

func TestVersionParser_ParseTildeWildcard(t *testing.T) {
	for _, tc := range provider(t, "VersionParserTest::tildeConstraints") {
		assertParsesTo(t, rangeExpected(t, tc.args[1], tc.args[2]), tc.args[0].(string))
	}
}

func TestVersionParser_ParseCaretWildcard(t *testing.T) {
	for _, tc := range provider(t, "VersionParserTest::caretConstraints") {
		assertParsesTo(t, rangeExpected(t, tc.args[1], tc.args[2]), tc.args[0].(string))
	}
}

func TestVersionParser_ParseHyphen(t *testing.T) {
	for _, tc := range provider(t, "VersionParserTest::hyphenConstraints") {
		assertParsesTo(t, rangeExpected(t, tc.args[1], tc.args[2]), tc.args[0].(string))
	}
}

func TestVersionParser_ParseConstraints(t *testing.T) {
	for _, tc := range provider(t, "VersionParserTest::constraintProvider") {
		assertParsesTo(t, tc.args[1].(string), tc.args[0].(string))
	}
}

func TestVersionParser_ParseConstraintsMulti(t *testing.T) {
	multi := mustMulti(t, true, mustConstraint(t, ">", "2.0.0.0"), mustConstraint(t, "<=", "3.0.0.0"))
	for _, tc := range provider(t, "VersionParserTest::multiConstraintProvider") {
		assertParsesTo(t, multi.String(), tc.args[0].(string))
	}
}

func TestVersionParser_ParseConstraintsMultiWithStabilitySuffix(t *testing.T) {
	multi := mustMulti(t, true, mustConstraint(t, ">=", "1.1.0.0-alpha4"), mustConstraint(t, "<", "1.2.9999999.9999999-dev"))
	assertParsesTo(t, multi.String(), ">=1.1.0-alpha4,<1.2.x-dev")

	multi = mustMulti(t, true, mustConstraint(t, ">=", "1.1.0.0-alpha4"), mustConstraint(t, "<", "1.2.0.0-beta2"))
	assertParsesTo(t, multi.String(), ">=1.1.0-alpha4,<1.2-beta2")
}

func TestVersionParser_ParseConstraintsMultiDisjunctiveHasPrioOverConjuctive(t *testing.T) {
	multi1 := mustMulti(t, true, mustConstraint(t, ">", "2.0.0.0"), mustConstraint(t, "<", "2.0.5.0-dev"))
	multi2 := mustMulti(t, false, multi1, mustConstraint(t, ">", "2.0.6.0"))
	for _, tc := range provider(t, "VersionParserTest::multiConstraintProvider2") {
		assertParsesTo(t, multi2.String(), tc.args[0].(string))
	}
}

func TestVersionParser_ParseConstraintsMultiWithStabilities(t *testing.T) {
	multi := mustMulti(t, true, mustConstraint(t, ">", "2.0.0.0"), mustConstraint(t, "<=", "3.0.0.0-dev"))
	assertParsesTo(t, multi.String(), ">2.0@stable,<=3.0@dev")
}

func TestVersionParser_ParseConstraintsMultiWithStabilitiesWildcard(t *testing.T) {
	multi := mustMulti(t, true, mustConstraint(t, ">", "2.0.0.0"), NewMatchAllConstraint())
	assertParsesTo(t, multi.String(), ">2.0@stable,@dev")
}

func TestVersionParser_ParseConstraintsMultiWithStabilitiesZero(t *testing.T) {
	multi := mustMulti(t, false, mustConstraint(t, ">", "2.0.0.0"), mustConstraint(t, "==", "0.0.0.0"))
	assertParsesTo(t, multi.String(), ">2.0@stable || 0@dev")
}

func TestVersionParser_ParseConstraintsFails(t *testing.T) {
	for _, tc := range provider(t, "VersionParserTest::failingConstraints") {
		_, err := VersionParser{}.ParseConstraints(tc.args[0].(string))
		expectUnexpectedValue(t, err, "")
	}
}

func TestVersionParser_ParseStability(t *testing.T) {
	for _, tc := range provider(t, "VersionParserTest::stabilityProvider") {
		if got := ParseStability(tc.args[1].(string)); got != tc.args[0].(string) {
			t.Errorf("ParseStability(%q) = %q, want %q", tc.args[1], got, tc.args[0])
		}
	}
}

func TestVersionParser_NormalizeStability(t *testing.T) {
	for stability, expected := range map[string]string{"rc": "RC", "BeTa": "beta"} {
		if got, err := NormalizeStability(stability); err != nil || got != expected {
			t.Errorf("NormalizeStability(%q) = %q, %v; want %q", stability, got, err, expected)
		}
	}
}

func TestVersionParser_ManipulateVersionStringWithReturnNull(t *testing.T) {
	matches := [5]string{"-1", "-3", "-2", "-5", "-9"}
	if result, ok := manipulateVersionString(matches, 1, 2); ok {
		t.Errorf("got %q, want null", result)
	}
}

func TestVersionParser_ComplexConjunctive(t *testing.T) {
	version := mustConstraint(t, "=", "1.0.1.0")
	parsed := mustParse(t, "~0.1 || ~1.0 !=1.0.1")
	if parsed.Matches(version) {
		t.Error(`"~0.1 || ~1.0 !=1.0.1" should not allow version "1.0.1.0"`)
	}
}
