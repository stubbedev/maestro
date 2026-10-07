// Fuzz targets: parsing is stable, and nothing panics.

package semver

import (
	"strings"
	"testing"
)

// simpleLeaves reports whether every version in the String() form of a
// parsed constraint is plain enough to survive being parsed again as
// written: no separators, aliases or brackets the parser would read.
func simpleLeaves(s string) bool {
	for _, part := range strings.FieldsFunc(s, func(r rune) bool { return r == '[' || r == ']' || r == '|' || r == ' ' }) {
		if _, ok := OperatorConstant(part); ok || part == "*" {
			continue
		}
		if strings.HasSuffix(part, "as") || strings.HasSuffix(part, "-") || strings.HasPrefix(part, "-") ||
			strings.HasPrefix(part, "as") || strings.Contains(part, "--") {
			return false
		}
		for _, c := range []byte(part) {
			if !isAlnum(c) && c != '.' && c != '-' {
				return false
			}
		}
	}

	return true
}

// source turns a String() form back into constraint syntax.
func source(s string) string { return strings.NewReplacer("[", "", "]", "").Replace(s) }

func FuzzParseConstraints(f *testing.F) {
	for _, s := range []string{
		"^1.2 || ~2.0.3, !=2.0.5", "1.0 - 2.0", ">=1.0@dev <2.0-beta", "dev-master as 1.0.x-dev", "1.*", "~>1.2",
		"2010-01-02", "!= dev-foo", "*", "^0.0.3-alpha", "1.0.x-dev#abc",
	} {
		f.Add(s)
	}
	probes := []ConstraintInterface{
		NewConstraintOp(OpEQ, "1.0.0.0"), NewConstraintOp(OpNE, "2.0.0.0-dev"), NewConstraintOp(OpGE, "dev-master"),
		NewConstraintOp(OpLT, "1.5.0.0"), NewMatchAllConstraint(), NewMatchNoneConstraint(),
		newMultiConstraint([]ConstraintInterface{NewConstraintOp(OpGT, "1.0"), NewConstraintOp(OpLE, "3.0")}, false),
	}
	f.Fuzz(func(t *testing.T, s string) {
		var p VersionParser
		c, err := p.ParseConstraints(s)
		again, againErr := p.ParseConstraints(s)
		if (err == nil) != (againErr == nil) {
			t.Fatalf("%q: unstable error %v / %v", s, err, againErr)
		}
		if err != nil {
			if err.Error() != againErr.Error() {
				t.Fatalf("%q: unstable error %v / %v", s, err, againErr)
			}

			return
		}
		if c.String() != again.String() || c.PrettyString() != again.PrettyString() {
			t.Fatalf("%q: unstable %s / %s", s, c, again)
		}

		// Matching never panics.
		_, _ = c.LowerBound(), c.UpperBound()
		for _, probe := range probes {
			c.Matches(probe)
			probe.Matches(c)
			Intervals.HaveIntersections(c, probe)
			Intervals.IsSubsetOf(c, probe)
		}
		Intervals.CompactConstraint(c).Matches(c)
		for _, op := range oracleProbeOps {
			for _, v := range []string{"1.0.0.0", "dev-master", "2.0.0.0-beta1", ""} {
				CompilingMatcher.Match(c, op, v)
			}
		}

		// The String() form parses back, and after one more round of
		// normalization it is a fixed point.
		if !simpleLeaves(c.String()) {
			return
		}
		form := c.String()
		for round := range 3 {
			reparsed, err := p.ParseConstraints(source(form))
			if err != nil {
				t.Fatalf("%q: %s does not parse back: %v", s, form, err)
			}
			if reparsed.String() == form {
				return
			}
			if round == 2 {
				t.Fatalf("%q: %s parses back as %s", s, form, reparsed)
			}
			form = reparsed.String()
		}
	})
}

func FuzzNormalize(f *testing.F) {
	for _, s := range []string{"1.0.0", "v1.0.0-beta.1+meta", "2010-01-02", "dev-master as 1.0", "1.x-dev", "foo"} {
		f.Add(s, "")
	}
	f.Fuzz(func(t *testing.T, version, full string) {
		var p VersionParser
		normalized, err := p.Normalize(version)
		if err == nil {
			ParseStability(normalized)
			if _, err := p.Normalize(normalized); err != nil && !strings.Contains(normalized, " ") {
				t.Fatalf("%q normalizes to %q, which does not normalize: %v", version, normalized, err)
			}
		}
		_, _ = p.NormalizeWithFullVersion(version, full)
		p.NormalizeBranch(version)
		p.ParseNumericAliasPrefix(version)
		ParseStability(version)
	})
}

func FuzzVersionCompare(f *testing.F) {
	for _, s := range [][2]string{{"1.0", "1.0.0"}, {"1.0-dev", "1.0"}, {"1.0#", "1.0pl"}, {"", "1"}, {"a.b", "1.a"}, {"6.4.12.0", "6.4.0.0-dev"}, {"1..2", "1.2."}, {".5", "0"}, {"10.0.0", "9.99999999999999999999.1"}, {"007.1", "7.1.0"}, {"1.2\x00.5", "1.2"}, {"1.10\x002", "1.12"}, {"6.4.12.0", "6.4.3.0-beta"}, {"1.0.", "1.0.1"}, {"1.0.0", "1.0.a"}, {"999999999999999999.1", "9223372036854775807"}} {
		f.Add(s[0], s[1])
	}
	// PHP's version_compare() is not antisymmetric (version_compare('.',
	// '.') is -1), so the only invariants are that it does not panic and
	// that the operator forms agree with it.
	f.Fuzz(func(t *testing.T, a, b string) {
		cmp := VersionCompare(a, b)
		if cmp < -1 || cmp > 1 {
			t.Fatalf("version_compare(%q, %q) = %d", a, b, cmp)
		}
		for op, want := range map[string]bool{"<": cmp < 0, "<=": cmp <= 0, ">": cmp > 0, ">=": cmp >= 0, "==": cmp == 0, "!=": cmp != 0} {
			if got, err := VersionCompareOp(a, b, op); err != nil || got != want {
				t.Fatalf("version_compare(%q, %q, %q) = %v, %v; version_compare() = %d", a, b, op, got, err, cmp)
			}
		}
		Comparator.LessThan(a, b)

		// the shortcut decides only as the C code's token loop would
		if got, ok := compareLeadingNumbers(a, b); ok {
			if want := phpVersionCompare(cString(a), cString(b)); got != want {
				t.Fatalf("compareLeadingNumbers(%q, %q) = %d, php_version_compare() = %d", a, b, got, want)
			}
		}

		// plain versions are copied as the C code would canonicalize them
		for _, v := range []string{a, b} {
			if v != "" && string(canonicalize(nil, v)) != string(canonicalizeBytes(nil, v)) {
				t.Fatalf("canonicalize(%q) = %q, want %q", v, canonicalize(nil, v), canonicalizeBytes(nil, v))
			}
		}

		// a compiled constraint's prepared version compares alike
		prepared := prepareVersion(b)
		reversed := VersionCompare(b, a)
		for _, op := range []Op{OpEQ, OpLT, OpLE, OpGT, OpGE, OpNE} {
			if got, want := prepared.opWith(a, op), opResult(cmp, op); got != want {
				t.Fatalf("prepared %q: opWith(%q, %v) = %v, want %v", b, a, op, got, want)
			}
			if got, want := prepared.opBefore(a, op), opResult(reversed, op); got != want {
				t.Fatalf("prepared %q: opBefore(%q, %v) = %v, want %v", b, a, op, got, want)
			}
		}
	})
}
