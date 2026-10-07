package phperr

import (
	"errors"
	"slices"

	"github.com/stubbedev/maestro/internal/php"
)

// Exception is implemented by a Go error that stands for a PHP exception:
// PHPClass is get_class($e), a class of the class table (parents). It may
// also implement Coded ($e->getCode()) and Chained ($e->getPrevious()).
type Exception interface {
	error
	php.Classer
}

// Coded is implemented by an Exception whose code ($e->getCode()) is not
// always 0.
type Coded interface {
	PHPCode() int
}

// hierarchy is implemented by an Exception whose class hierarchy PHP
// reported itself (one a plugin threw), which may name classes the table
// does not have.
type hierarchy interface {
	InstanceOf(class string) bool
}

// DefaultClass is the class of an error that stands for no PHP exception
// (a failure of maestro's own, with no counterpart in Composer).
const DefaultClass = "RuntimeException"

// Of returns the Exception err stands for: err itself, or else the first
// Exception in its Unwrap tree (a Go wrapper adding context Composer does
// not have stands for the exception it wraps); nil when there is none.
func Of(err error) Exception {
	e, _ := errors.AsType[Exception](err)

	return e
}

// ClassOf is get_class($e) and $e->getCode() for the exception err stands
// for (Of); DefaultClass and 0 when it stands for none.
func ClassOf(err error) (class string, code int) {
	e := Of(err)
	if e == nil {
		return DefaultClass, 0
	}
	if c, ok := e.(Coded); ok {
		code = c.PHPCode()
	}

	return e.PHPClass(), code
}

// Class is get_class($e) for the exception err stands for (ClassOf).
func Class(err error) string {
	class, _ := ClassOf(err)

	return class
}

// InstanceOf is `$e instanceof $class` for the exception err stands for
// (Of; DefaultClass when none), which `catch ($class $e)` catches.
func InstanceOf(err error, class string) bool {
	e := Of(err)
	if e == nil {
		return IsSubclass(DefaultClass, class)
	}
	if h, ok := e.(hierarchy); ok {
		return h.InstanceOf(class)
	}

	return IsSubclass(e.PHPClass(), class)
}

// IsSubclass reports whether class is of, a subclass of it or, for an
// interface of, implements it, in the class table.
func IsSubclass(class, of string) bool {
	for class != "" {
		if class == of || slices.Contains(implements[class], of) {
			return true
		}
		class = parents[class]
	}

	return false
}
