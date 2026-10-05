// Differential tests against the goldens tools/oracle/semver/oracle.php
// records by running the real composer/semver 3.4.4 over a generated
// corpus.

package semver

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/testutil"
)

// phpException is an {"e": [class, message]} golden.
type phpException struct{ class, message string }

// oracleResult decodes a golden that is either a value or an exception.
type oracleResult struct {
	value     json.RawMessage
	exception *phpException
}

func (r *oracleResult) UnmarshalJSON(data []byte) error {
	var e struct {
		E []string `json:"e"`
	}
	if data[0] == '{' && json.Unmarshal(data, &e) == nil && len(e.E) == 2 {
		r.exception = &phpException{e.E[0], e.E[1]}

		return nil
	}
	r.value = append(json.RawMessage(nil), data...)

	return nil
}

// check compares a Go result with the golden: the same exception class
// and message, or no error and a value that decodes equal to got.
func (r *oracleResult) check(t *testing.T, what string, got any, err error) {
	t.Helper()
	if r.exception != nil {
		if !isPHPException(err, r.exception.class) || err.Error() != r.exception.message {
			t.Errorf("%s: got %#v, %v; want %s(%q)", what, got, err, r.exception.class, r.exception.message)
		}

		return
	}
	if err != nil {
		t.Errorf("%s: got error %v, want %s", what, err, r.value)

		return
	}
	gotJSON, _ := json.Marshal(got)
	if !jsonEqual(gotJSON, r.value) {
		t.Errorf("%s: got %s, want %s", what, gotJSON, r.value)
	}
}

func jsonEqual(a, b []byte) bool {
	var va, vb any
	if json.Unmarshal(a, &va) != nil || json.Unmarshal(b, &vb) != nil {
		return false
	}

	return fmt.Sprint(va) == fmt.Sprint(vb)
}

func isPHPException(err error, class string) bool {
	var ok bool
	switch class {
	case "UnexpectedValueException":
		_, ok = errors.AsType[*UnexpectedValueError](err)
	case "InvalidArgumentException":
		_, ok = errors.AsType[*InvalidArgumentError](err)
	case "ValueError":
		_, ok = errors.AsType[*ValueError](err)
	}

	return ok
}

func loadOracle(t *testing.T, name string, rows any) {
	t.Helper()
	data, err := testutil.ReadGoldenFile("testdata/oracle/" + name)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, rows); err != nil {
		t.Fatal(err)
	}
}

// tuple decodes a JSON array into the pointed-to values, in order.
func tuple(t *testing.T, raw json.RawMessage, dst ...any) {
	t.Helper()
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil || len(items) != len(dst) {
		t.Fatalf("bad row %s: %v", raw, err)
	}
	for i, item := range items {
		if err := json.Unmarshal(item, dst[i]); err != nil {
			t.Fatalf("bad row %s: %v", raw, err)
		}
	}
}

func TestOracle_Versions(t *testing.T) {
	var rows []struct {
		V  string       `json:"v"`
		N  oracleResult `json:"n"`
		B  string       `json:"b"`
		A  *string      `json:"a"`
		S  string       `json:"s"`
		NS oracleResult `json:"ns"`
		D  string       `json:"d"`
	}
	loadOracle(t, "versions.json.gz", &rows)
	var p VersionParser
	for _, row := range rows {
		normalized, err := p.Normalize(row.V)
		row.N.check(t, "Normalize("+strconv.Quote(row.V)+")", normalized, err)

		if got := p.NormalizeBranch(row.V); got != row.B {
			t.Errorf("NormalizeBranch(%q) = %q, want %q", row.V, got, row.B)
		}
		prefix, ok := p.ParseNumericAliasPrefix(row.V)
		if ok != (row.A != nil) || (ok && prefix != *row.A) {
			t.Errorf("ParseNumericAliasPrefix(%q) = %q, %v; want %v", row.V, prefix, ok, row.A)
		}
		if got := ParseStability(row.V); got != row.S {
			t.Errorf("ParseStability(%q) = %q, want %q", row.V, got, row.S)
		}
		stability, err := NormalizeStability(row.V)
		row.NS.check(t, "NormalizeStability("+strconv.Quote(row.V)+")", stability, err)
		if got := p.NormalizeDefaultBranch(row.V); got != row.D {
			t.Errorf("NormalizeDefaultBranch(%q) = %q, want %q", row.V, got, row.D)
		}
	}
	t.Logf("%d versions", len(rows))
}

func TestOracle_FullVersions(t *testing.T) {
	var rows []json.RawMessage
	loadOracle(t, "fullversions.json", &rows)
	var p VersionParser
	for _, raw := range rows {
		var version, full string
		var want oracleResult
		tuple(t, raw, &version, &full, &want)
		got, err := p.NormalizeWithFullVersion(version, full)
		want.check(t, fmt.Sprintf("Normalize(%q, %q)", version, full), got, err)
	}
	t.Logf("%d versions", len(rows))
}

// The versions and operators oracle.php matches parsed constraints against.
var (
	oracleProbeVersions = []string{
		"0.0.0.0-dev", "0.0.0.0", "0.1.0.0", "0.9.9.9", "1.0.0.0-dev", "1.0.0.0-alpha1", "1.0.0.0-beta2",
		"1.0.0.0-RC1", "1.0.0.0", "1.0.0.0-patch1", "1.0.1.0", "1.2.0.0", "1.2.3.0", "1.9999999.9999999.9999999-dev", "2.0.0.0-dev",
		"2.0.0.0", "2.1.0.0", "3.0.0.0", "3.5.0.0", "10.0.0.0", "20100102", "9999999-dev", "dev-master", "dev-foo",
		"dev-bar", "1.0", "2", "1.0.0-beta",
	}
	oracleProbeOps = []Op{OpEQ, OpNE, OpLT, OpLE, OpGT, OpGE}
)

// intervalsGolden renders an IntervalSet as oracle.php's intervalsData().
func intervalsGolden(set IntervalSet) []any {
	numeric := make([]any, 0, len(set.Numeric))
	for k, interval := range set.Numeric {
		numeric = append(numeric, []any{k, interval.Start().String(), interval.End().String()})
	}
	names := make([]any, 0, len(set.Branches.Names))
	for i, name := range set.Branches.Names {
		names = append(names, []any{set.Branches.key(i), name})
	}

	return []any{numeric, names, set.Branches.Exclude}
}

func TestOracle_Constraints(t *testing.T) {
	// The goldens were recorded from empty caches; Intervals' cache
	// decides which constraint objects compacted constraints share.
	Intervals.Clear()
	CompilingMatcher.Clear()

	var rows []struct {
		C   string         `json:"c"`
		R   oracleResult   `json:"r"`
		Sat []oracleResult `json:"sat"`
	}
	loadOracle(t, "constraints.json.gz", &rows)
	valid := 0
	for _, row := range rows {
		c, err := VersionParser{}.ParseConstraints(row.C)
		if row.R.exception != nil || err != nil {
			row.R.check(t, "ParseConstraints("+strconv.Quote(row.C)+")", nil, err)

			continue
		}
		valid++

		var matches, compiled strings.Builder
		for _, v := range oracleProbeVersions {
			for _, op := range oracleProbeOps {
				matches.WriteString(map[bool]string{false: "0", true: "1"}[c.Matches(NewConstraintOp(op, v))])
				compiled.WriteString(map[bool]string{false: "0", true: "1"}[CompilingMatcher.Match(c, op, v)])
			}
		}
		compact := Intervals.CompactConstraint(c)
		got := map[string]any{
			"s":   c.String(),
			"p":   c.PrettyString(),
			"lb":  c.LowerBound().String(),
			"ub":  c.UpperBound().String(),
			"m":   matches.String(),
			"cm":  compiled.String(),
			"iv":  intervalsGolden(Intervals.Get(c)),
			"cc":  compact.String(),
			"ccp": compact.PrettyString(),
		}
		var want map[string]json.RawMessage
		if err := json.Unmarshal(row.R.value, &want); err != nil {
			t.Fatal(err)
		}
		for key, value := range want {
			if gotJSON, _ := json.Marshal(got[key]); !jsonEqual(gotJSON, value) {
				t.Errorf("ParseConstraints(%q) %s: got %s, want %s", row.C, key, gotJSON, value)
			}
		}

		for i, v := range []string{"1.0.0", "2.0", "dev-master", "v1.2.3-beta", "foo", "1.0.x-dev"} {
			ok, err := Semver.Satisfies(v, row.C)
			row.Sat[i].check(t, fmt.Sprintf("Satisfies(%q, %q)", v, row.C), ok, err)
		}
	}
	t.Logf("%d constraints, %d valid", len(rows), valid)
}

func TestOracle_Pairs(t *testing.T) {
	var rows []json.RawMessage
	loadOracle(t, "pairs.json", &rows)
	for _, raw := range rows {
		var a, b string
		var subset, intersects, matches bool
		tuple(t, raw, &a, &b, &subset, &intersects, &matches)
		ca, cb := mustParse(t, a), mustParse(t, b)
		if got := Intervals.IsSubsetOf(ca, cb); got != subset {
			t.Errorf("IsSubsetOf(%q, %q) = %v", a, b, got)
		}
		if got := Intervals.HaveIntersections(ca, cb); got != intersects {
			t.Errorf("HaveIntersections(%q, %q) = %v", a, b, got)
		}
		if got := ca.Matches(cb); got != matches {
			t.Errorf("(%q).Matches(%q) = %v", a, b, got)
		}
	}
	t.Logf("%d pairs", len(rows))
}

func TestOracle_Compare(t *testing.T) {
	var rows []json.RawMessage
	loadOracle(t, "compare.json.gz", &rows)
	constraint := mustConstraint(t, "==", "1")
	for _, raw := range rows {
		var a, b, op string
		var vc int
		var vcOp, comparator, versionCompare, versionCompareBranches oracleResult
		tuple(t, raw, &a, &b, &op, &vc, &vcOp, &comparator, &versionCompare, &versionCompareBranches)
		if got := VersionCompare(a, b); got != vc {
			t.Errorf("version_compare(%q, %q) = %d, want %d", a, b, got, vc)
		}
		got, err := VersionCompareOp(a, b, op)
		vcOp.check(t, fmt.Sprintf("version_compare(%q, %q, %q)", a, b, op), got, err)
		got, err = Comparator.Compare(a, op, b)
		comparator.check(t, fmt.Sprintf("Comparator::compare(%q, %q, %q)", a, op, b), got, err)
		got, err = constraint.VersionCompare(a, b, op, false)
		versionCompare.check(t, fmt.Sprintf("versionCompare(%q, %q, %q)", a, b, op), got, err)
		got, err = constraint.VersionCompare(a, b, op, true)
		versionCompareBranches.check(t, fmt.Sprintf("versionCompare(%q, %q, %q, true)", a, b, op), got, err)
	}
	t.Logf("%d comparisons", len(rows))
}

func TestOracle_VersionComparePairs(t *testing.T) {
	var rows []json.RawMessage
	loadOracle(t, "versioncompare.json", &rows)
	for _, raw := range rows {
		var a, b string
		var want int
		tuple(t, raw, &a, &b, &want)
		if got := VersionCompare(a, b); got != want {
			t.Errorf("version_compare(%q, %q) = %d, want %d", a, b, got, want)
		}
	}
	t.Logf("%d pairs", len(rows))
}

func TestOracle_Sort(t *testing.T) {
	var rows []json.RawMessage
	loadOracle(t, "sort.json", &rows)
	for _, raw := range rows {
		var list, sorted, rsorted []string
		var constraint string
		var satisfiedBy oracleResult
		tuple(t, raw, &list, &sorted, &rsorted, &constraint, &satisfiedBy)
		if list == nil {
			list = []string{}
		}
		if got, err := Semver.Sort(list); err != nil || !slices.Equal(got, sorted) {
			t.Errorf("Sort(%q) = %q, %v; want %q", list, got, err, sorted)
		}
		if got, err := Semver.Rsort(list); err != nil || !slices.Equal(got, rsorted) {
			t.Errorf("Rsort(%q) = %q, %v; want %q", list, got, err, rsorted)
		}
		got, err := Semver.SatisfiedBy(list, constraint)
		satisfiedBy.check(t, fmt.Sprintf("SatisfiedBy(%q, %q)", list, constraint), got, err)
	}
	t.Logf("%d lists", len(rows))
}

func TestOracle_LooseEquals(t *testing.T) {
	var rows []json.RawMessage
	loadOracle(t, "looseequals.json", &rows)
	for _, raw := range rows {
		var a, b string
		var equal bool
		tuple(t, raw, &a, &b, &equal)
		if got := php.StringsLooseEqual(a, b); got != equal {
			t.Errorf("%q == %q: got %v, want %v", a, b, got, equal)
		}
	}
}

func TestOracle_Increment(t *testing.T) {
	var rows []json.RawMessage
	loadOracle(t, "increment.json", &rows)
	for _, raw := range rows {
		var s, want string
		var increment int
		var negative bool
		tuple(t, raw, &s, &increment, &want, &negative)
		if got, neg := phpAddInt(s, increment); got != want || neg != negative {
			t.Errorf("%q + %d: got %q %v, want %q %v", s, increment, got, neg, want, negative)
		}
	}
}
