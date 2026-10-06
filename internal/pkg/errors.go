// The PHP exception classes the Composer\Package classes throw that
// internal/util does not already provide.

package pkg

import (
	"strconv"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
)

// TypeError is PHP's \TypeError, raised where Composer's strictly typed
// code receives a value of the wrong type (a malformed package array).
type TypeError struct {
	Message string
}

func (e *TypeError) Error() string { return e.Message }

// PHPClass implements util.PHPClasser.
func (*TypeError) PHPClass() (string, int) { return "TypeError", 0 }

// ArgumentTypeError is the TypeError PHP throws when argument n (named
// param) of fn receives given where expected was declared:
// "fn(): Argument #n ($param) must be of type expected, T given", T being
// zend_zval_value_name (booleans are named by their value).
func ArgumentTypeError(fn string, n int, param, expected string, given any) *TypeError {
	return &TypeError{Message: fn + "(): Argument #" + strconv.Itoa(n) + " ($" + param + ") must be of type " + expected + ", " + php.ZvalValueName(given) + " given"}
}

// SecurityError is Composer\Exception\SecurityException (util owns it, so
// errors.As matches across packages).
type SecurityError = util.SecurityError
