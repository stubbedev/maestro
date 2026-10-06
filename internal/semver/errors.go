// Ports the PHP exception classes composer/semver throws: \UnexpectedValueException
// (VersionParser), \InvalidArgumentException (Constraint, MultiConstraint,
// Bound, VersionParser::normalizeStability) and the \ValueError thrown by
// version_compare().

package semver

// UnexpectedValueError is PHP's \UnexpectedValueException, thrown when a
// version or constraint string cannot be parsed. Composer catches this
// class specifically, so callers should match it with errors.As.
type UnexpectedValueError struct {
	Message string
}

func (e *UnexpectedValueError) Error() string { return e.Message }

// InvalidArgumentError is PHP's \InvalidArgumentException.
type InvalidArgumentError struct {
	Message string
}

func (e *InvalidArgumentError) Error() string { return e.Message }

// ValueError is PHP's \ValueError, thrown by version_compare() for an
// unknown operator.
type ValueError struct {
	Message string
}

func (e *ValueError) Error() string { return e.Message }
