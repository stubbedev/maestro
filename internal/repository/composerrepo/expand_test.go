package composerrepo

import (
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
)

// MetadataMinifier::expand on version lists holding entries that are not
// arrays, as Composer 2.10.3 runs it on PHP 8.4.25 under
// ErrorHandler::register(): the JSON of the result or the error with its
// site.
func TestExpandVersions(t *testing.T) {
	for _, c := range []struct {
		in, want string
		line     int
	}{
		{`[false,{"a":1},{"b":2}]`, `[false,{"a":1},{"a":1,"b":2}]`, 0},
		{`[{"a":1},"x"]`, "foreach() argument must be of type array|object, string given", 34},
		{`["abc",{"a":1}]`, "Cannot access offset of type string on string", 38},
		{`["abc",{"a":"__unset"}]`, "Cannot unset string offsets", 36},
		{`[5,{"a":1}]`, "Cannot use a scalar value as an array", 38},
		{`[5,{"a":"__unset"}]`, "Cannot unset offset in a non-array variable", 36},
		{`[{"a":1,"b":2},{"a":"__unset"},[]]`, `[{"a":1,"b":2},{"b":2},{"b":2}]`, 0},
		{`[null,0,{"v":1}]`, `[null,0,{"v":1}]`, 0},
	} {
		in, err := php.JSONDecode(c.in, true)
		if err != nil {
			t.Fatal(err)
		}
		got, err := expandVersions(in.(*php.Array).Values())
		if c.line != 0 {
			site, _ := phperr.SiteOf(err)
			if err == nil || err.Error() != c.want || site.File != "MetadataMinifier.php" || site.Line != c.line {
				t.Errorf("%s: got %v at %v, want %s at line %d", c.in, err, site, c.want, c.line)
			}

			continue
		}
		if err != nil {
			t.Fatalf("%s: %v", c.in, err)
		}
		enc, _ := php.JSONEncode(php.ListOf(got...), 0)
		if enc != c.want {
			t.Errorf("%s: got %s, want %s", c.in, enc, c.want)
		}
	}
}

// A version entry that is not an array fails reading its "version" for
// normalize() (ComposerRepository.php:1329): a TypeError for a string,
// the "Trying to access array offset" warning otherwise.
func TestVersionOffsetError(t *testing.T) {
	for _, c := range []struct {
		v    any
		want string
	}{
		{"abc", "Cannot access offset of type string on string"},
		{int64(1), "Trying to access array offset on int"},
		{true, "Trying to access array offset on true"},
		{false, "Trying to access array offset on false"},
		{nil, "Trying to access array offset on null"},
		{1.5, "Trying to access array offset on float"},
	} {
		err := versionOffsetError(c.v)
		if site, _ := phperr.SiteOf(err); err.Error() != c.want || site.Line != 1329 {
			t.Errorf("%v: got %v at %v", c.v, err, site)
		}
	}
}
