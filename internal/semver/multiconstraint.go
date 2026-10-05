// Ports src/Constraint/MultiConstraint.php.

package semver

import (
	"strings"
	"sync"
	"sync/atomic"
)

// MultiConstraint ports Composer\Semver\Constraint\MultiConstraint: a
// conjunctive or disjunctive set of constraints.
type MultiConstraint struct {
	constraints  []ConstraintInterface
	conjunctive  bool
	prettyString string

	// str memoises String(), as PHP's $string property does.
	str atomic.Pointer[string]

	boundsOnce   sync.Once
	lower, upper Bound
}

// NewMultiConstraint ports new MultiConstraint($constraints, $conjunctive).
func NewMultiConstraint(constraints []ConstraintInterface, conjunctive bool) (*MultiConstraint, error) {
	if len(constraints) < 2 {
		return nil, &InvalidArgumentError{Message: "Must provide at least two constraints for a MultiConstraint. Use " +
			"the regular Constraint class for one constraint only or MatchAllConstraint for none. You may use " +
			"MultiConstraint::create() which optimizes and handles those cases automatically."}
	}

	return newMultiConstraint(constraints, conjunctive), nil
}

// newMultiConstraint is the constructor for callers that always pass at
// least two constraints.
func newMultiConstraint(constraints []ConstraintInterface, conjunctive bool) *MultiConstraint {
	return &MultiConstraint{constraints: constraints, conjunctive: conjunctive}
}

// Constraints ports getConstraints(). The slice must not be modified.
func (c *MultiConstraint) Constraints() []ConstraintInterface { return c.constraints }

// IsConjunctive ports isConjunctive().
func (c *MultiConstraint) IsConjunctive() bool { return c.conjunctive }

// IsDisjunctive ports isDisjunctive().
func (c *MultiConstraint) IsDisjunctive() bool { return !c.conjunctive }

// compile ports compile($otherOperator).
func (c *MultiConstraint) compile(otherOperator Op) compiledMatcher {
	var parts []compiledMatcher
	for _, constraint := range c.constraints {
		code := constraint.compile(otherOperator)
		switch code.kind {
		case matcherKindTrue:
			if !c.conjunctive {
				return matcherTrue
			}
		case matcherKindFalse:
			if c.conjunctive {
				return matcherFalse
			}
		default:
			parts = append(parts, code)
		}
	}

	if len(parts) == 0 {
		if c.conjunctive {
			return matcherTrue
		}

		return matcherFalse
	}

	if c.conjunctive {
		return matcherExpr(func(v string, b bool) bool {
			for _, p := range parts {
				if !p.eval(v, b) {
					return false
				}
			}

			return true
		})
	}

	return matcherExpr(func(v string, b bool) bool {
		for _, p := range parts {
			if p.eval(v, b) {
				return true
			}
		}

		return false
	})
}

// Matches ports matches().
func (c *MultiConstraint) Matches(provider ConstraintInterface) bool {
	if !c.conjunctive {
		for _, constraint := range c.constraints {
			if provider.Matches(constraint) {
				return true
			}
		}

		return false
	}

	// when matching a conjunctive and a disjunctive multi constraint we have to iterate over the disjunctive one
	// otherwise we'd return true if different parts of the disjunctive constraint match the conjunctive one
	// which would lead to incorrect results, e.g. [>1 and <2] would match [<1 or >2] although they do not intersect
	if p, ok := provider.(*MultiConstraint); ok && p.IsDisjunctive() {
		return p.Matches(c)
	}

	for _, constraint := range c.constraints {
		if !provider.Matches(constraint) {
			return false
		}
	}

	return true
}

// SetPrettyString ports setPrettyString().
func (c *MultiConstraint) SetPrettyString(prettyString string) { c.prettyString = prettyString }

// PrettyString ports getPrettyString().
func (c *MultiConstraint) PrettyString() string {
	if phpTruthy(c.prettyString) {
		return c.prettyString
	}

	return c.String()
}

// String ports __toString().
func (c *MultiConstraint) String() string {
	if s := c.str.Load(); s != nil {
		return *s
	}

	sep := " || "
	if c.conjunctive {
		sep = " "
	}
	var b strings.Builder
	b.WriteByte('[')
	for i, constraint := range c.constraints {
		if i > 0 {
			b.WriteString(sep)
		}
		b.WriteString(constraint.String())
	}
	b.WriteByte(']')
	s := b.String()
	c.str.Store(&s)

	return s
}

// LowerBound ports getLowerBound().
func (c *MultiConstraint) LowerBound() Bound {
	c.boundsOnce.Do(c.extractBounds)

	return c.lower
}

// UpperBound ports getUpperBound().
func (c *MultiConstraint) UpperBound() Bound {
	c.boundsOnce.Do(c.extractBounds)

	return c.upper
}

// extractBounds ports extractBounds().
func (c *MultiConstraint) extractBounds() {
	for i, constraint := range c.constraints {
		if i == 0 {
			c.lower = constraint.LowerBound()
			c.upper = constraint.UpperBound()

			continue
		}

		// $this->isConjunctive() ? '>' : '<'
		if lower := constraint.LowerBound(); lower.compareTo(c.lower, c.conjunctive) {
			c.lower = lower
		}

		// $this->isConjunctive() ? '<' : '>'
		if upper := constraint.UpperBound(); upper.compareTo(c.upper, !c.conjunctive) {
			c.upper = upper
		}
	}
}

// CreateMultiConstraint ports MultiConstraint::create(): it optimises the
// constraints as much as possible and does not necessarily return a
// MultiConstraint if things can be reduced to a simple constraint.
func CreateMultiConstraint(constraints []ConstraintInterface, conjunctive bool) ConstraintInterface {
	switch len(constraints) {
	case 0:
		return NewMatchAllConstraint()
	case 1:
		return constraints[0]
	}

	if optimized, ok := optimizeConstraints(constraints, conjunctive); ok {
		constraints, conjunctive = optimized, false
		if len(constraints) == 1 {
			return constraints[0]
		}
	}

	return newMultiConstraint(constraints, conjunctive)
}

// optimizeConstraints ports optimizeConstraints(): contiguous OR groups are
// collapsed into one constraint, [>= 1 < 2] || [>= 2 < 3] || [>= 3 < 4] =>
// [>= 1 < 4].
func optimizeConstraints(constraints []ConstraintInterface, conjunctive bool) ([]ConstraintInterface, bool) {
	if conjunctive {
		// TODO: Here's the place to put more optimizations
		return nil, false
	}

	left := constraints[0]
	var mergedConstraints []ConstraintInterface
	optimized := false
	for _, right := range constraints[1:] {
		if l, r, ok := contiguousRanges(left, right); ok {
			optimized = true
			left = newMultiConstraint([]ConstraintInterface{l.constraints[0], r.constraints[1]}, true)
		} else {
			mergedConstraints = append(mergedConstraints, left)
			left = right
		}
	}
	if !optimized {
		return nil, false
	}

	return append(mergedConstraints, left), true
}

// contiguousRanges is the condition of optimizeConstraints(): left and
// right are both [>= x < y] ranges and left ends where right starts.
func contiguousRanges(left, right ConstraintInterface) (l, r *MultiConstraint, ok bool) {
	l, ok = left.(*MultiConstraint)
	if !ok || !l.conjunctive || len(l.constraints) != 2 {
		return nil, nil, false
	}
	r, ok = right.(*MultiConstraint)
	if !ok || !r.conjunctive || len(r.constraints) != 2 {
		return nil, nil, false
	}
	left0 := l.constraints[0].String()
	if !phpTruthy(left0) || !strings.HasPrefix(left0, ">=") {
		return nil, nil, false
	}
	left1 := l.constraints[1].String()
	if !phpTruthy(left1) || left1[0] != '<' {
		return nil, nil, false
	}
	right0 := r.constraints[0].String()
	if !phpTruthy(right0) || !strings.HasPrefix(right0, ">=") {
		return nil, nil, false
	}
	right1 := r.constraints[1].String()
	if !phpTruthy(right1) || right1[0] != '<' {
		return nil, nil, false
	}
	if phpSubstr(left1, 2) != phpSubstr(right0, 3) {
		return nil, nil, false
	}

	return l, r, true
}
