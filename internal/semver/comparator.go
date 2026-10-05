// Ports src/Comparator.php.

package semver

// comparator is the namespace for Comparator's static methods.
type comparator struct{}

// Comparator ports Composer\Semver\Comparator: version comparisons with
// branches being comparable.
var Comparator comparator

// GreaterThan ports Comparator::greaterThan(): whether version1 > version2.
func (comparator) GreaterThan(version1, version2 string) bool {
	return compareOp(version1, OpGT, version2)
}

// GreaterThanOrEqualTo ports Comparator::greaterThanOrEqualTo(): whether
// version1 >= version2.
func (comparator) GreaterThanOrEqualTo(version1, version2 string) bool {
	return compareOp(version1, OpGE, version2)
}

// LessThan ports Comparator::lessThan(): whether version1 < version2.
func (comparator) LessThan(version1, version2 string) bool {
	return compareOp(version1, OpLT, version2)
}

// LessThanOrEqualTo ports Comparator::lessThanOrEqualTo(): whether
// version1 <= version2.
func (comparator) LessThanOrEqualTo(version1, version2 string) bool {
	return compareOp(version1, OpLE, version2)
}

// EqualTo ports Comparator::equalTo(): whether version1 == version2.
func (comparator) EqualTo(version1, version2 string) bool {
	return compareOp(version1, OpEQ, version2)
}

// NotEqualTo ports Comparator::notEqualTo(): whether version1 != version2.
func (comparator) NotEqualTo(version1, version2 string) bool {
	return compareOp(version1, OpNE, version2)
}

// Compare ports Comparator::compare(): it evaluates the expression
// version1 operator version2. An unsupported operator is the
// *InvalidArgumentError Constraint's constructor throws.
func (comparator) Compare(version1, operator, version2 string) (bool, error) {
	op, ok := OperatorConstant(operator)
	if !ok {
		return false, invalidOperatorError(operator, 100)
	}

	return compareOp(version1, op, version2), nil
}

func compareOp(version1 string, op Op, version2 string) bool {
	constraint := Constraint{operator: op, version: version2}
	provider := Constraint{operator: OpEQ, version: version1}

	return constraint.matchSpecific(&provider, true)
}
