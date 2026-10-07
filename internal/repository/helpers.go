// Helpers shared by the repository ports.

package repository

import (
	"strconv"
	"strings"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/semver"
)

func itoa(i int) string { return strconv.Itoa(i) }

// ParseConstraint is what the repositories do with a string constraint:
// (new VersionParser())->parseConstraints($constraint).
func ParseConstraint(constraint string) (semver.ConstraintInterface, error) {
	return pkg.NewVersionParser().ParseConstraints(constraint)
}

// versionMatches reports whether constraint (nil: any) matches
// new Constraint('==', $version).
func versionMatches(constraint semver.ConstraintInterface, version string) bool {
	return constraint == nil || constraint.Matches(semver.NewConstraintOp(semver.OpEQ, version))
}

// linksUnion returns $a + $b: the links of b whose keys a lacks are
// appended.
func linksUnion(a, b pkg.Links) pkg.Links {
	if b.Len() == 0 {
		return a
	}
	if a.Len() == 0 {
		return b
	}
	var builder pkg.LinksBuilder
	builder.Grow(a.Len() + b.Len())
	for k, l := range a.All() {
		builder.Append(k, l)
	}
	for k, l := range b.All() {
		if !a.Has(k) {
			builder.Append(k, l)
		}
	}

	return builder.Build()
}

// MergeLinks returns array_merge($a, $b, ...) for link maps: later string
// keys replace earlier ones in place, int keys are renumbered.
func MergeLinks(lists ...pkg.Links) pkg.Links {
	var builder pkg.LinksBuilder
	next := 0
	for _, links := range lists {
		for k, l := range links.All() {
			if php.StrKey(k).IsInt() {
				builder.Append(strconv.Itoa(next), l)
				next++
			} else {
				builder.Set(k, l)
			}
		}
	}

	return builder.Build()
}

// inArrayLoose is in_array($needle, $haystack) for a list of strings.
func inArrayLoose(needle string, haystack []string) bool {
	for _, s := range haystack {
		if php.StringsLooseEqual(needle, s) {
			return true
		}
	}

	return false
}

// firstSegment is [$first] = explode('/', $s).
func firstSegment(s string) string {
	first, _, _ := strings.Cut(s, "/")

	return first
}
