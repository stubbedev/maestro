// Ports src/Semver.php.

package semver

// Semver::SORT_ASC and Semver::SORT_DESC.
const (
	SortAsc  = 1
	SortDesc = -1
)

// semverStatic is the namespace for Semver's static methods.
type semverStatic struct{}

// Semver ports Composer\Semver\Semver.
var Semver semverStatic

// Satisfies ports Semver::satisfies(): whether version satisfies the
// constraints. Errors are those of normalizing the version or parsing the
// constraints (*UnexpectedValueError).
func (semverStatic) Satisfies(version, constraints string) (bool, error) {
	var versionParser VersionParser
	normalized, err := versionParser.Normalize(version)
	if err != nil {
		return false, err
	}
	parsedConstraints, err := versionParser.ParseConstraints(constraints)
	if err != nil {
		return false, err
	}

	return satisfies(normalized, parsedConstraints), nil
}

func satisfies(normalized string, parsedConstraints ConstraintInterface) bool {
	provider := Constraint{operator: OpEQ, version: normalized}

	return parsedConstraints.Matches(&provider)
}

// SatisfiedBy ports Semver::satisfiedBy(): the versions that satisfy the
// constraints, in order.
func (semverStatic) SatisfiedBy(versions []string, constraints string) ([]string, error) {
	// PHP parses the constraints again for every version, after
	// normalizing it; parsing once, at the same point, fails the same way.
	var versionParser VersionParser
	var parsedConstraints ConstraintInterface
	satisfied := make([]string, 0, len(versions))
	for _, version := range versions {
		normalized, err := versionParser.Normalize(version)
		if err != nil {
			return nil, err
		}
		if parsedConstraints == nil {
			if parsedConstraints, err = versionParser.ParseConstraints(constraints); err != nil {
				return nil, err
			}
		}
		if satisfies(normalized, parsedConstraints) {
			satisfied = append(satisfied, version)
		}
	}

	return satisfied, nil
}

// Sort ports Semver::sort(): the versions sorted from lowest to highest.
func (semverStatic) Sort(versions []string) ([]string, error) {
	return semverUsort(versions, SortAsc)
}

// Rsort ports Semver::rsort(): the versions sorted from highest to lowest.
func (semverStatic) Rsort(versions []string) ([]string, error) {
	return semverUsort(versions, SortDesc)
}

// semverUsort ports Semver::usort().
func semverUsort(versions []string, direction int) ([]string, error) {
	var versionParser VersionParser

	type item struct {
		normalized string
		key        int
	}

	// Normalize outside of usort() scope for minor performance increase.
	normalized := make([]item, len(versions))
	for key, version := range versions {
		normalizedVersion, err := versionParser.Normalize(version)
		if err != nil {
			return nil, err
		}
		normalized[key] = item{versionParser.NormalizeDefaultBranch(normalizedVersion), key}
	}

	phpUsort(normalized, func(left, right item) int {
		if left.normalized == right.normalized {
			return 0
		}

		if Comparator.LessThan(left.normalized, right.normalized) {
			return -direction
		}

		return direction
	})

	// Recreate input array, using the original indexes which are now in sorted order.
	sorted := make([]string, len(normalized))
	for i, item := range normalized {
		sorted[i] = versions[item.key]
	}

	return sorted, nil
}
