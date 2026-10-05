// Ports src/Composer/Package/Version/VersionBumper.php.

package version

import (
	"slices"
	"strings"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/dumper"
	"github.com/stubbedev/maestro/internal/pkg/loader"
	"github.com/stubbedev/maestro/internal/semver"
	"github.com/stubbedev/maestro/internal/util"
)

// VersionBumper ports Composer\Package\Version\VersionBumper.
type VersionBumper struct{}

var (
	bumpMajor          = php.MustCompile(`{^([1-9][0-9]*|0\.\d+).*}`)
	bumpTrailingZeros  = php.MustCompile(`{(?:\.(?:0|9999999))+(-dev)?$}`)
	simpleCaretVersion = php.MustCompile(`{^\^\d+(\.\d+)*$}`)
)

// BumpRequirement ports VersionBumper::bumpRequirement: constraint raised
// so that it requires at least the package's version, keeping its form
// ("^1.0" with 1.2.1 installed gives "^1.2.1"). Constraints it cannot bump
// come back as their pretty string.
func (VersionBumper) BumpRequirement(constraint semver.ConstraintInterface, p pkg.PackageInterface) (string, error) {
	parser := pkg.NewVersionParser()
	prettyConstraint := constraint.PrettyString()

	if strings.HasPrefix(prettyConstraint, "dev-") {
		return prettyConstraint, nil
	}

	version := p.Version()
	if strings.HasPrefix(version, "dev-") {
		dumped, err := dumper.ArrayDumper{}.Dump(p)
		if err != nil {
			return "", err
		}

		extra, ok, err := loader.NewArrayLoader(parser, false).GetBranchAlias(dumped)
		if err != nil {
			return "", err
		}

		// dev packages without branch alias cannot be processed
		if !ok || extra == pkg.DefaultBranchAlias {
			return prettyConstraint, nil
		}

		version = extra
	}

	// complex constraints with branch names are not bumped
	if len(semver.Intervals.Get(constraint).Branches.Names) > 0 {
		return prettyConstraint, nil
	}

	major := mustReplace(bumpMajor, version, "$1")
	versionWithoutSuffix := mustReplace(bumpTrailingZeros, version, "")
	newPrettyConstraint := "^" + versionWithoutSuffix

	// not a simple stable version, abort
	if ok, err := simpleCaretVersion.IsMatch(newPrettyConstraint); err != nil || !ok {
		return prettyConstraint, err
	}

	pattern := `{
            (?<=,|\ |\||^) # leading separator
            (?P<constraint>
                \^v?` + major + `(?:\.\d+)* # e.g. ^2.anything
                | ~v?` + major + `(?:\.\d+){1,3} # e.g. ~2.2 or ~2.2.2 or ~2.2.2.2
                | v?` + major + `(?:\.[*x])+ # e.g. 2.* or 2.*.* or 2.x.x.x etc
                | >=v?\d(?:\.\d+)* # e.g. >=2 or >=1.2 etc
                | \* # full wildcard
            )
            (?=,|$|\ |\||@) # trailing separator
        }x`

	re, err := php.Compile(pattern)
	if err != nil {
		return "", err
	}

	matches, err := re.MatchAll(prettyConstraint)
	if err != nil {
		return "", err
	}

	if len(matches) == 0 {
		return prettyConstraint, nil
	}

	modified := prettyConstraint

	for _, m := range slices.Backward(matches) {
		match, offset := m.Get(1), m.Offset(1)
		dots := strings.Count(match, ".")

		suffix := ""
		if dots == 2 && strings.Count(versionWithoutSuffix, ".") == 1 {
			suffix = ".0"
		}

		var replacement string

		switch {
		case strings.HasPrefix(match, "~") && dots != 1:
			// take as many version bits from the current version as we have in the constraint to bump it without making it more specific
			versionBits := strings.Split(versionWithoutSuffix, ".")
			for len(versionBits) < dots+1 {
				versionBits = append(versionBits, "0")
			}

			replacement = "~" + strings.Join(versionBits[:dots+1], ".")
		case match == "*" || strings.HasPrefix(match, ">="):
			replacement = ">=" + versionWithoutSuffix + suffix
		default:
			replacement = newPrettyConstraint + suffix
		}

		modified = modified[:offset] + replacement + modified[offset+util.Strlen(match):]
	}

	// if it is strictly equal to the previous one then no need to change anything
	newConstraint, err := parser.ParseConstraints(modified)
	if err != nil {
		return "", err
	}

	if semver.Intervals.IsSubsetOf(newConstraint, constraint) && semver.Intervals.IsSubsetOf(constraint, newConstraint) {
		return prettyConstraint, nil
	}

	return modified, nil
}

// mustReplace is Preg::replace for patterns that cannot fail at run time.
func mustReplace(re *php.Regexp, subject, replacement string) string {
	s, _, err := re.Replace(subject, replacement, -1)
	if err != nil {
		panic(err)
	}

	return s
}
