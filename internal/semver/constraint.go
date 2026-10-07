// Ports src/Constraint/ConstraintInterface.php and src/Constraint/Constraint.php.

package semver

import (
	"strings"

	"github.com/stubbedev/maestro/internal/php"
)

// ConstraintInterface ports Composer\Semver\Constraint\ConstraintInterface.
//
// Like the PHP interface ("DO NOT IMPLEMENT this interface"), it is sealed:
// the only implementations are *Constraint, *MultiConstraint,
// *MatchAllConstraint and *MatchNoneConstraint.
type ConstraintInterface interface {
	// Matches checks whether the given constraint intersects in any way
	// with this constraint.
	Matches(provider ConstraintInterface) bool
	UpperBound() Bound
	LowerBound() Bound
	// PrettyString returns the pretty string if one was set (and is
	// truthy in PHP terms), the String() form otherwise.
	PrettyString() string
	// SetPrettyString sets the pretty string; "" means null.
	SetPrettyString(prettyString string)
	// String ports __toString().
	String() string

	// compile ports compile($otherOperator) as an evaluable matcher
	// instead of PHP source code.
	compile(otherOperator Op) compiledMatcher
}

// Op is one of Constraint's operator integer values (Constraint::OP_*).
type Op int

// Constraint::OP_* values.
const (
	OpEQ Op = iota
	OpLT
	OpLE
	OpGT
	OpGE
	OpNE
)

// Constraint::STR_OP_* values.
const (
	StrOpEQ    = "=="
	StrOpEQAlt = "="
	StrOpLT    = "<"
	StrOpLE    = "<="
	StrOpGT    = ">"
	StrOpGE    = ">="
	StrOpNE    = "!="
	StrOpNEAlt = "<>"
)

// transOpInt ports Constraint::$transOpInt.
var transOpInt = [...]string{
	OpEQ: StrOpEQ,
	OpLT: StrOpLT,
	OpLE: StrOpLE,
	OpGT: StrOpGT,
	OpGE: StrOpGE,
	OpNE: StrOpNE,
}

// String returns the operator's string form (Constraint::$transOpInt).
func (op Op) String() string { return transOpInt[op] }

// supportedOperators is array_keys(Constraint::$transOpStr), in order.
var supportedOperators = [...]string{"=", "==", "<", "<=", ">", ">=", "<>", "!="}

// OperatorConstant ports Constraint::getOperatorConstant() (the
// Constraint::$transOpStr lookup); ok is false for an unsupported operator.
func OperatorConstant(operator string) (op Op, ok bool) {
	switch operator {
	case "=", "==":
		return OpEQ, true
	case "<":
		return OpLT, true
	case "<=":
		return OpLE, true
	case ">":
		return OpGT, true
	case ">=":
		return OpGE, true
	case "<>", "!=":
		return OpNE, true
	}

	return 0, false
}

// invalidOperatorError is the InvalidArgumentException Constraint.php throws
// for an unknown operator (the constructor, versionCompare).
func invalidOperatorError(operator string) error {
	return &InvalidArgumentError{Message: "Invalid operator \"" + operator + "\" given, expected one of: " +
		strings.Join(supportedOperators[:], ", ")}
}

// Constraint ports Composer\Semver\Constraint\Constraint: a single
// operator/version pair.
type Constraint struct {
	operator     Op
	version      string
	prettyString string
}

// NewConstraint ports new Constraint($operator, $version).
func NewConstraint(operator, version string) (*Constraint, error) {
	op, ok := OperatorConstant(operator)
	if !ok {
		return nil, invalidOperatorError(operator)
	}

	return NewConstraintOp(op, version), nil
}

// NewConstraintOp creates a constraint from an operator constant. op must
// be one of the Op constants.
func NewConstraintOp(op Op, version string) *Constraint {
	return &Constraint{operator: op, version: version}
}

// Version ports getVersion().
func (c *Constraint) Version() string { return c.version }

// Operator ports getOperator(): the operator's normalised string form.
func (c *Constraint) Operator() string { return transOpInt[c.operator] }

// Op returns the operator constant.
func (c *Constraint) Op() Op { return c.operator }

// Matches ports matches().
func (c *Constraint) Matches(provider ConstraintInterface) bool {
	if p, ok := provider.(*Constraint); ok {
		return c.matchSpecific(p, false)
	}

	// turn matching around to find a match
	return provider.Matches(c)
}

// SetPrettyString ports setPrettyString().
func (c *Constraint) SetPrettyString(prettyString string) { c.prettyString = prettyString }

// PrettyString ports getPrettyString().
func (c *Constraint) PrettyString() string {
	if php.Truthy(c.prettyString) {
		return c.prettyString
	}

	return c.String()
}

// String ports __toString().
func (c *Constraint) String() string {
	return transOpInt[c.operator] + " " + c.version
}

func (c *Constraint) appendString(dst []byte) []byte {
	dst = append(dst, transOpInt[c.operator]...)
	dst = append(dst, ' ')

	return append(dst, c.version...)
}

// sameString reports whether two constraints have the same String() form
// without building it: operator strings never contain a space.
func (c *Constraint) sameString(other *Constraint) bool {
	return c.operator == other.operator && c.version == other.version
}

func isBranch(version string) bool { return strings.HasPrefix(version, "dev-") }

// VersionCompare ports versionCompare($a, $b, $operator, $compareBranches).
// Note that, as in PHP, only "==" and "!=" (not "=" and "<>") get the
// special branch treatment.
func (c *Constraint) VersionCompare(a, b, operator string, compareBranches bool) (bool, error) {
	op, ok := OperatorConstant(operator)
	if !ok {
		return false, invalidOperatorError(operator)
	}
	aIsBranch, bIsBranch := isBranch(a), isBranch(b)
	if operator == StrOpNE && (aIsBranch || bIsBranch) {
		return a != b, nil
	}
	if aIsBranch && bIsBranch {
		return operator == StrOpEQ && a == b, nil
	}
	// when branches are not comparable, we make sure dev branches never match anything
	if !compareBranches && (aIsBranch || bIsBranch) {
		return false, nil
	}

	return versionCompareOp(a, b, op), nil
}

// versionCompare is VersionCompare for the normalised operator strings
// Constraint passes itself.
func versionCompare(a, b string, op Op, compareBranches bool) bool {
	aIsBranch, bIsBranch := isBranch(a), isBranch(b)
	if op == OpNE && (aIsBranch || bIsBranch) {
		return a != b
	}
	if aIsBranch && bIsBranch {
		return op == OpEQ && a == b
	}
	// when branches are not comparable, we make sure dev branches never match anything
	if !compareBranches && (aIsBranch || bIsBranch) {
		return false
	}

	return versionCompareOp(a, b, op)
}

// noEqualOp is str_replace('=', ”, $transOpInt[$op]), encoded so that
// equal values mean equal strings.
var noEqualOp = [...]byte{OpEQ: 0, OpLT: '<', OpLE: '<', OpGT: '>', OpGE: '>', OpNE: '!'}

// hasNoEqualSign reports whether $transOpInt[$op] === str_replace('=', ”, ...).
func hasNoEqualSign(op Op) bool { return op == OpLT || op == OpGT }

// MatchSpecific ports matchSpecific($provider, $compareBranches).
func (c *Constraint) MatchSpecific(provider *Constraint, compareBranches bool) bool {
	return c.matchSpecific(provider, compareBranches)
}

func (c *Constraint) matchSpecific(provider *Constraint, compareBranches bool) bool {
	isEqualOp := c.operator == OpEQ
	isNonEqualOp := c.operator == OpNE
	isProviderEqualOp := provider.operator == OpEQ
	isProviderNonEqualOp := provider.operator == OpNE

	// '!=' operator is match when other operator is not '==' operator or version is not match
	// these kinds of comparisons always have a solution
	if isNonEqualOp || isProviderNonEqualOp {
		if isNonEqualOp && !isProviderNonEqualOp && !isProviderEqualOp && isBranch(provider.version) {
			return false
		}
		if isProviderNonEqualOp && !isNonEqualOp && !isEqualOp && isBranch(c.version) {
			return false
		}
		if !isEqualOp && !isProviderEqualOp {
			return true
		}

		return versionCompare(provider.version, c.version, OpNE, compareBranches)
	}

	// an example for the condition is <= 2.0 & < 1.0
	// these kinds of comparisons always have a solution
	if c.operator != OpEQ && noEqualOp[c.operator] == noEqualOp[provider.operator] {
		return !isBranch(c.version) && !isBranch(provider.version)
	}

	version1, version2, operator := provider.version, c.version, c.operator
	if isEqualOp {
		version1, version2, operator = c.version, provider.version, provider.operator
	}
	if versionCompare(version1, version2, operator, compareBranches) {
		// special case, e.g. require >= 1.0 and provide < 1.0
		// 1.0 >= 1.0 but 1.0 is outside of the provided interval
		return !hasNoEqualSign(provider.operator) ||
			hasNoEqualSign(c.operator) ||
			!versionCompareOp(provider.version, c.version, OpEQ)
	}

	return false
}

// LowerBound ports getLowerBound().
func (c *Constraint) LowerBound() Bound {
	lower, _ := c.bounds()

	return lower
}

// UpperBound ports getUpperBound().
func (c *Constraint) UpperBound() Bound {
	_, upper := c.bounds()

	return upper
}

// bounds ports extractBounds(). PHP memoises the result; computing it is
// cheaper than storing it.
func (c *Constraint) bounds() (lower, upper Bound) {
	// Branches
	if isBranch(c.version) {
		return ZeroBound(), PositiveInfinityBound()
	}

	switch c.operator {
	case OpEQ:
		return NewBound(c.version, true), NewBound(c.version, true)
	case OpLT:
		return ZeroBound(), NewBound(c.version, false)
	case OpLE:
		return ZeroBound(), NewBound(c.version, true)
	case OpGT:
		return NewBound(c.version, false), PositiveInfinityBound()
	case OpGE:
		return NewBound(c.version, true), PositiveInfinityBound()
	default: // OpNE
		return ZeroBound(), PositiveInfinityBound()
	}
}

// compile ports compile($otherOperator).
func (c *Constraint) compile(otherOperator Op) compiledMatcher {
	version := c.version
	if isBranch(version) {
		switch c.operator {
		case OpEQ:
			switch otherOperator {
			case OpEQ: // $b && $v === %s
				return matcherExpr(func(v string, b bool) bool { return b && v == version })
			case OpNE: // !$b || $v !== %s
				return matcherExpr(func(v string, b bool) bool { return !b || v != version })
			}

			return matcherFalse
		case OpNE:
			switch otherOperator {
			case OpEQ: // !$b || $v !== %s
				return matcherExpr(func(v string, b bool) bool { return !b || v != version })
			case OpNE:
				return matcherTrue
			}

			return matcherExpr(notBranch)
		}

		return matcherFalse
	}

	// the constraint's version is compared with every version matched
	prepared := prepareVersion(version)
	switch c.operator {
	case OpEQ:
		switch otherOperator {
		case OpEQ: // \version_compare($v, %s, '==')
			return matcherExpr(func(v string, _ bool) bool { return prepared.opWith(v, OpEQ) })
		case OpNE: // $b || \version_compare($v, %s, '!=')
			return matcherExpr(func(v string, b bool) bool { return b || prepared.opWith(v, OpNE) })
		}

		// !$b && \version_compare(%s, $v, '%s')
		return matcherExpr(func(v string, b bool) bool { return !b && prepared.opBefore(v, otherOperator) })
	case OpNE:
		switch otherOperator {
		case OpEQ: // $b || (!$b && \version_compare($v, %s, '!='))
			return matcherExpr(func(v string, b bool) bool { return b || prepared.opWith(v, OpNE) })
		case OpNE:
			return matcherTrue
		}

		return matcherExpr(notBranch)
	case OpLT, OpLE:
		if otherOperator == OpLT || otherOperator == OpLE {
			return matcherExpr(notBranch)
		}
	default: // OpGT, OpGE
		if otherOperator == OpGT || otherOperator == OpGE {
			return matcherExpr(notBranch)
		}
	}

	if otherOperator == OpNE {
		return matcherTrue
	}

	op := c.operator
	if (op == OpLE && otherOperator == OpGT) || (op == OpGE && otherOperator == OpLT) {
		// !$b && \version_compare($v, %s, '!=') && \version_compare($v, %s, '%s')
		return matcherExpr(func(v string, b bool) bool {
			return !b && prepared.opWith(v, OpNE) && prepared.opWith(v, op)
		})
	}

	// !$b && \version_compare($v, %s, '%s')
	return matcherExpr(func(v string, b bool) bool { return !b && prepared.opWith(v, op) })
}

func notBranch(_ string, b bool) bool { return !b }
