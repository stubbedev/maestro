package semver

import (
	"testing"

	"github.com/stubbedev/maestro/internal/phperr"
)

// The errors carry the throw site of composer/semver's exceptions, which
// the console renders ("In VersionParser.php line 191:").
func TestThrowSites(t *testing.T) {
	_, normalizeErr := VersionParser{}.Normalize("foo bar")
	_, tildeErr := VersionParser{}.ParseConstraints("~>1.2")
	_, basicErr := VersionParser{}.ParseConstraints(">= foo")
	_, operatorErr := NewConstraint("=>", "1.0.0")
	_, compareErr := (&Constraint{}).VersionCompare("1", "2", "=>", false)
	_, stabilityErr := NormalizeStability("foo")
	for _, tc := range []struct {
		name string
		err  error
		file string
		line int
	}{
		{"normalize", normalizeErr, "VersionParser.php", 191},
		{"tilde", tildeErr, "VersionParser.php", 346},
		{"basic", basicErr, "VersionParser.php", 526},
		{"constraint", operatorErr, "Constraint.php", 100},
		{"versionCompare", compareErr, "Constraint.php", 203},
		{"stability", stabilityErr, "VersionParser.php", 92},
	} {
		if !phperr.Is(tc.err, tc.file, tc.line) {
			site, _ := phperr.SiteOf(tc.err)
			t.Errorf("%s: %v at %v, want %s:%d", tc.name, tc.err, site, tc.file, tc.line)
		}
	}
}
