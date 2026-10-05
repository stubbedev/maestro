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
// PHP appends where the call came from; the message here stops before
// that part.
type TypeError struct{ Message string }

func (e *TypeError) Error() string { return e.Message }

// ArgumentTypeError is the TypeError PHP throws when argument n (named
// param) of fn receives given where expected was declared:
// "fn(): Argument #n ($param) must be of type expected, T given", T being
// zend_zval_value_name (booleans are named by their value).
func ArgumentTypeError(fn string, n int, param, expected string, given any) *TypeError {
	name := php.TypeName(given)
	if b, ok := given.(bool); ok {
		name = strconv.FormatBool(b)
	}

	return &TypeError{Message: fn + "(): Argument #" + strconv.Itoa(n) + " ($" + param + ") must be of type " + expected + ", " + name + " given"}
}

// SecurityError is Composer\Exception\SecurityException (util owns it, so
// errors.As matches across packages).
type SecurityError = util.SecurityError
