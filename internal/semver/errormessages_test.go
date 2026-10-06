package semver

import (
	"testing"
)

// The errors carry the messages of composer/semver's exceptions (where
// they were thrown is free, docs/PORTING.md "The contract").
func TestErrorMessages(t *testing.T) {
	_, normalizeErr := VersionParser{}.Normalize("foo bar")
	_, tildeErr := VersionParser{}.ParseConstraints("~>1.2")
	_, basicErr := VersionParser{}.ParseConstraints(">= foo")
	_, operatorErr := NewConstraint("=>", "1.0.0")
	_, compareErr := (&Constraint{}).VersionCompare("1", "2", "=>", false)
	_, stabilityErr := NormalizeStability("foo")
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"normalize", normalizeErr, `Invalid version string "foo bar"`},
		{"tilde", tildeErr, `Could not parse version constraint ~>1.2: Invalid operator "~>", you probably meant to use the "~" operator`},
		{"basic", basicErr, `Could not parse version constraint >= foo: Invalid version string "foo"`},
		{"constraint", operatorErr, `Invalid operator "=>" given, expected one of: =, ==, <, <=, >, >=, <>, !=`},
		{"versionCompare", compareErr, `Invalid operator "=>" given, expected one of: =, ==, <, <=, >, >=, <>, !=`},
		{"stability", stabilityErr, `Invalid stability string "foo", expected one of stable, RC, beta, alpha or dev`},
	} {
		if tc.err == nil || tc.err.Error() != tc.want {
			t.Errorf("%s: %v, want %q", tc.name, tc.err, tc.want)
		}
	}
}
