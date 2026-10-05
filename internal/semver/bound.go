// Ports src/Constraint/Bound.php.

package semver

import (
	"strconv"

	"github.com/stubbedev/maestro/internal/php"
)

// positiveInfinityVersion is PHP_INT_MAX.'.0.0.0'.
var positiveInfinityVersion = strconv.FormatInt(1<<63-1, 10) + ".0.0.0"

// zeroVersion is the version of the zero bound.
const zeroVersion = "0.0.0.0-dev"

// Bound ports Composer\Semver\Constraint\Bound: an immutable value.
type Bound struct {
	version     string
	isInclusive bool
}

// NewBound ports new Bound($version, $isInclusive).
func NewBound(version string, isInclusive bool) Bound {
	return Bound{version: version, isInclusive: isInclusive}
}

// ZeroBound ports Bound::zero().
func ZeroBound() Bound { return Bound{version: zeroVersion, isInclusive: true} }

// PositiveInfinityBound ports Bound::positiveInfinity().
func PositiveInfinityBound() Bound { return Bound{version: positiveInfinityVersion} }

// Version ports getVersion().
func (b Bound) Version() string { return b.version }

// IsInclusive ports isInclusive().
func (b Bound) IsInclusive() bool { return b.isInclusive }

// IsZero ports isZero().
func (b Bound) IsZero() bool { return b.version == zeroVersion && b.isInclusive }

// IsPositiveInfinity ports isPositiveInfinity().
func (b Bound) IsPositiveInfinity() bool {
	return b.version == positiveInfinityVersion && !b.isInclusive
}

// CompareTo ports compareTo($other, $operator): whether this bound is
// higher (">") or lower ("<") than other.
func (b Bound) CompareTo(other Bound, operator string) (bool, error) {
	if operator != "<" && operator != ">" {
		return false, &InvalidArgumentError{Message: "Does not support any other operator other than > or <."}
	}

	return b.compareTo(other, operator == ">"), nil
}

func (b Bound) compareTo(other Bound, greater bool) bool {
	// If they are the same it doesn't matter. PHP compares the objects
	// with ==, so numeric version strings compare as numbers.
	if b.isInclusive == other.isInclusive && php.StringsLooseEqual(b.version, other.version) {
		return false
	}

	compareResult := VersionCompare(b.version, other.version)

	// Not the same version means we don't need to check if the bounds are inclusive or not
	if compareResult != 0 {
		if greater {
			return compareResult == 1
		}

		return compareResult == -1
	}

	// Question we're answering here is "am I higher than $other?"
	if greater {
		return other.isInclusive
	}

	return !other.isInclusive
}

// String ports __toString().
func (b Bound) String() string {
	if b.isInclusive {
		return b.version + " [inclusive]"
	}

	return b.version + " [exclusive]"
}
