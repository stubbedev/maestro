package repository

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/util"
)

// PartialSecurityAdvisory::create and FilterListEntry::create with an
// affectedVersions/constraint that is not a string, as Composer 2.10.3
// runs them on PHP 8.4.25 (fresh constraint cache, ErrorHandler registered
// with a verbose BufferIO): Composer's VersionParser::parseConstraints is
// untyped, so scalars are cast; an array fails its cache's isset(); null
// fails Preg::replace() in the advisory's fallback and the parse of "" in
// the filter list entry; a float with a fraction raises two deprecation
// notices.
func TestAdvisoryConstraintNotString(t *testing.T) {
	type result struct{ constraint, err string }
	for _, c := range []struct {
		value            any
		advisory, filter result
	}{
		{
			nil,
			result{err: "$subject must be a string, NULL given."},
			result{err: `Could not parse version constraint : Invalid version string ""`},
		},
		{int64(1), result{constraint: "1"}, result{constraint: "1"}},
		{true, result{constraint: "1"}, result{constraint: "1"}},
		{false, result{constraint: "== 0.0.0-invalid-version"}, result{err: `Could not parse version constraint : Invalid version string ""`}},
		{1.5, result{constraint: "1.5"}, result{constraint: "1.5"}},
		{
			php.ListOf("x"),
			result{err: "Cannot access offset of type array in isset or empty"},
			result{err: "Cannot access offset of type array in isset or empty"},
		},
	} {
		check := func(kind string, want result, constraint string, err error) {
			t.Helper()
			if want.err != "" {
				if err == nil || err.Error() != want.err {
					t.Errorf("%v %s: got %v, want %s", c.value, kind, err, want.err)
				}

				return
			}
			if err != nil || constraint != want.constraint {
				t.Errorf("%v %s: got %q, %v, want %q", c.value, kind, constraint, err, want.constraint)
			}
		}

		a, err := CreatePartialSecurityAdvisory("a/b", php.ArrayOf("affectedVersions", c.value, "advisoryId", "X"), pkg.NewVersionParser())
		var got string
		if err == nil {
			got = a.Partial().AffectedVersions.PrettyString()
		}
		check("advisory", c.advisory, got, err)

		e, err := CreateFilterListEntry("l", php.ArrayOf("constraint", c.value, "package", "a/b"), pkg.NewVersionParser())
		got = ""
		if err == nil {
			got = e.Constraint.PrettyString()
		}
		check("filter", c.filter, got, err)
	}
}

// new SecurityAdvisory(...) evaluates its arguments, new
// \DateTimeImmutable($data['reportedAt']) among them, before checking its
// parameters (Composer 2.10.3, PHP 8.4.25). PHP's call site of a
// TypeError (", called in X on line N") is free.
func TestSecurityAdvisoryArgumentOrder(t *testing.T) {
	for _, c := range []struct {
		data *php.Array
		want string
	}{
		{
			php.ArrayOf("affectedVersions", "1", "advisoryId", int64(5), "title", "t", "sources", php.NewArray(), "reportedAt", "garbage"),
			"Failed to parse time string (garbage) at position 0 (g): The timezone could not be found in the database",
		},
		{
			php.ArrayOf("affectedVersions", "1", "advisoryId", int64(5), "title", "t", "sources", php.NewArray(), "reportedAt", int64(7)),
			"DateTimeImmutable::__construct(): Argument #1 ($datetime) must be of type string, int given",
		},
		{
			php.ArrayOf("affectedVersions", "1", "advisoryId", int64(5), "title", int64(1), "sources", php.NewArray(), "reportedAt", "2020-01-01"),
			`Composer\Advisory\SecurityAdvisory::__construct(): Argument #2 ($advisoryId) must be of type string, int given`,
		},
		{
			php.ArrayOf("affectedVersions", "1", "title", int64(1), "sources", php.NewArray(), "reportedAt", "garbage"),
			`Undefined array key "advisoryId"`,
		},
	} {
		_, err := CreatePartialSecurityAdvisory("a/b", c.data, pkg.NewVersionParser())
		if err == nil || calledIn.ReplaceAllString(err.Error(), "") != c.want {
			t.Errorf("got %v\nwant %s", err, c.want)
		}
	}
}

func TestAdvisoryConstraintFloatDeprecation(t *testing.T) {
	util.ResetErrorHandler()
	t.Cleanup(util.ResetErrorHandler)
	out, err := io.NewBufferIO("", 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	util.RegisterErrorHandler(out)

	if _, err := CreateFilterListEntry("l", php.ArrayOf("constraint", 1.5, "package", "a/b"), pkg.NewVersionParser()); err != nil {
		t.Fatal(err)
	}
	// the notices' format and site are free
	got := php.NormalizeEOL(out.Output())
	for _, want := range []string{
		"Implicit conversion from float 1.5 to int loses precision",
		"More deprecation notices were hidden, run again with `-v` to show them.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("got %q\nwant %q", got, want)
		}
	}
}

// calledIn is PHP's call site in a TypeError's message.
var calledIn = regexp.MustCompile(`, called in .* on line [0-9]+$`)
