// Ports the PHP exception classes composer/semver throws: \UnexpectedValueException
// (VersionParser) and \InvalidArgumentException (Constraint,
// MultiConstraint, Bound, VersionParser::normalizeStability); the
// \ValueError version_compare() throws is a php.EngineError.

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

// PHPClass implements phperr.Exception.
func (*UnexpectedValueError) PHPClass() string { return "UnexpectedValueException" }

// PHPClass implements phperr.Exception.
func (*InvalidArgumentError) PHPClass() string { return "InvalidArgumentException" }
