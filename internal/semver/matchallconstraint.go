// Ports src/Constraint/MatchAllConstraint.php.

package semver

// MatchAllConstraint ports Composer\Semver\Constraint\MatchAllConstraint:
// the absence of a constraint, which matches everything.
type MatchAllConstraint struct {
	prettyString string
}

// NewMatchAllConstraint ports new MatchAllConstraint().
func NewMatchAllConstraint() *MatchAllConstraint { return &MatchAllConstraint{} }

// Matches ports matches().
func (c *MatchAllConstraint) Matches(ConstraintInterface) bool { return true }

func (c *MatchAllConstraint) compile(Op) compiledMatcher { return matcherTrue }

// SetPrettyString ports setPrettyString().
func (c *MatchAllConstraint) SetPrettyString(prettyString string) { c.prettyString = prettyString }

// PrettyString ports getPrettyString().
func (c *MatchAllConstraint) PrettyString() string {
	if phpTruthy(c.prettyString) {
		return c.prettyString
	}

	return c.String()
}

// String ports __toString().
func (c *MatchAllConstraint) String() string { return "*" }

// UpperBound ports getUpperBound().
func (c *MatchAllConstraint) UpperBound() Bound { return PositiveInfinityBound() }

// LowerBound ports getLowerBound().
func (c *MatchAllConstraint) LowerBound() Bound { return ZeroBound() }
