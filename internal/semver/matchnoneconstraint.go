// Ports src/Constraint/MatchNoneConstraint.php.

package semver

// MatchNoneConstraint ports Composer\Semver\Constraint\MatchNoneConstraint:
// the blackhole of constraints, nothing escapes it.
type MatchNoneConstraint struct {
	prettyString string
}

// NewMatchNoneConstraint ports new MatchNoneConstraint().
func NewMatchNoneConstraint() *MatchNoneConstraint { return &MatchNoneConstraint{} }

// Matches ports matches().
func (c *MatchNoneConstraint) Matches(ConstraintInterface) bool { return false }

func (c *MatchNoneConstraint) compile(Op) compiledMatcher { return matcherFalse }

// SetPrettyString ports setPrettyString().
func (c *MatchNoneConstraint) SetPrettyString(prettyString string) { c.prettyString = prettyString }

// PrettyString ports getPrettyString().
func (c *MatchNoneConstraint) PrettyString() string {
	if phpTruthy(c.prettyString) {
		return c.prettyString
	}

	return c.String()
}

// String ports __toString().
func (c *MatchNoneConstraint) String() string { return "[]" }

// UpperBound ports getUpperBound().
func (c *MatchNoneConstraint) UpperBound() Bound { return NewBound(zeroVersion, false) }

// LowerBound ports getLowerBound().
func (c *MatchNoneConstraint) LowerBound() Bound { return NewBound(zeroVersion, false) }
