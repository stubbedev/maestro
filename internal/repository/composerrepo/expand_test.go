package composerrepo

import (
	"testing"

	"github.com/stubbedev/maestro/internal/php"
)

// MetadataMinifier::expand on version lists holding entries that are not
// arrays, as Composer 2.10.3 runs it on PHP 8.4.25 under
// ErrorHandler::register(): the JSON of the result or the error.
func TestExpandVersions(t *testing.T) {
	for _, c := range []struct {
		in, want string
		fails    bool
	}{
		{`[false,{"a":1},{"b":2}]`, `[false,{"a":1},{"a":1,"b":2}]`, false},
		{`[{"a":1},"x"]`, "foreach() argument must be of type array|object, string given", true},
		{`["abc",{"a":1}]`, "Cannot access offset of type string on string", true},
		{`["abc",{"a":"__unset"}]`, "Cannot unset string offsets", true},
		{`[5,{"a":1}]`, "Cannot use a scalar value as an array", true},
		{`[5,{"a":"__unset"}]`, "Cannot unset offset in a non-array variable", true},
		{`[{"a":1,"b":2},{"a":"__unset"},[]]`, `[{"a":1,"b":2},{"b":2},{"b":2}]`, false},
		{`[null,0,{"v":1}]`, `[null,0,{"v":1}]`, false},
	} {
		in, err := php.JSONDecode(c.in, true)
		if err != nil {
			t.Fatal(err)
		}
		got, err := expandVersions(in.(*php.Array).Values())
		if c.fails {
			if err == nil || err.Error() != c.want {
				t.Errorf("%s: got %v, want %s", c.in, err, c.want)
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
		if err.Error() != c.want {
			t.Errorf("%v: got %v, want %s", c.v, err, c.want)
		}
	}
}
