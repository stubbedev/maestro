// The PHP exception classes the Composer\Package classes throw that
// internal/util does not already provide.

package pkg

import (
	"strconv"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/util"
)

// TypeError is PHP's \TypeError, raised where Composer's strictly typed
// code receives a value of the wrong type (a malformed package array).
// Called and Raised give it the throw site, message and first frame PHP
// gives it.
type TypeError struct {
	Message string
	phperr.Site
}

func (e *TypeError) Error() string { return e.Message }

// Called makes e the TypeError PHP raises for a wrongly typed argument of
// the userland function declared at decl (the line its declaration starts
// on) and called at file:line: getFile()/getLine() name the declaration,
// the message gets where the call came from (phperr.CalledIn), and the
// trace starts with the call of function (as traces name it,
// "Composer\Config->merge").
func (e *TypeError) Called(function string, decl phperr.Site, file string, line int) *TypeError {
	e.Message += phperr.CalledIn(file, line)
	e.Site = decl
	phperr.Call(e, function, file, line)

	return e
}

// Raised makes e a TypeError raised at file:line of Composer's sources:
// an internal function's (whose call, function, heads the trace), a
// closure's called by one, a return type's or an operator's (function
// "").
func (e *TypeError) Raised(function, file string, line int) *TypeError {
	e.Site = phperr.At(file, line)
	if function != "" {
		phperr.Call(e, function, file, line)
	}

	return e
}

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
