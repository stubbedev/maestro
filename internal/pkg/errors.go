// The PHP exception classes the Composer\Package classes throw that
// internal/util does not already provide.

package pkg

import (
	"strconv"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
)

// ArgumentTypeError is the TypeError PHP throws when argument n (named
// param) of fn receives given where expected was declared:
// "fn(): Argument #n ($param) must be of type expected, T given", T being
// zend_zval_value_name (booleans are named by their value).
func ArgumentTypeError(fn string, n int, param, expected string, given any) *php.EngineError {
	return &php.EngineError{Class: php.ClassTypeError, Message: fn + "(): Argument #" + strconv.Itoa(n) + " ($" + param + ") must be of type " + expected + ", " + php.ZvalValueName(given) + " given"}
}

// AdvisoryDataTypeError is the TypeError Composer's getSecurityAdvisories
// throws when the advisory data a repository lists for packageName is not
// an array: the closure it maps the data with declares array $data.
func AdvisoryDataTypeError(packageName string, given any) *php.EngineError {
	return &php.EngineError{Class: php.ClassTypeError, Message: "Security advisory data for " + packageName + " must be of type array, " + php.ZvalValueName(given) + " given"}
}

// SecurityError is Composer\Exception\SecurityException (util owns it, so
// errors.As matches across packages).
type SecurityError = util.SecurityError
