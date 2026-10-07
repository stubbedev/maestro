// Ports the engine exceptions (Zend/zend_exceptions.c: ValueError,
// ArgumentCountError, TypeError) that the functions of this package report.

package php

// EngineError is a PHP engine \Error a ported function or Composer's
// strictly typed code raises, such as sprintf()'s ValueError or a
// TypeError for an argument of the wrong type. Message is exactly as PHP
// reports it.
type EngineError struct {
	Class   EngineClass
	Message string
}

// EngineClass is the class of an EngineError.
type EngineClass string

// The engine errors maestro raises.
const (
	ClassError              EngineClass = "Error"
	ClassTypeError          EngineClass = "TypeError"
	ClassValueError         EngineClass = "ValueError"
	ClassArgumentCountError EngineClass = "ArgumentCountError"
)

func (e *EngineError) Error() string { return e.Message }

// PHPClass implements phperr.Exception.
func (e *EngineError) PHPClass() string { return string(e.Class) }

func valueError(msg string) *EngineError { return &EngineError{Class: ClassValueError, Message: msg} }

// FalseToArrayDeprecation is the E_DEPRECATED PHP 8.1+ raises when a write
// auto-vivifies an array from false.
const FalseToArrayDeprecation = "Automatic conversion of false to array is deprecated"

// WritableArray is $v used as the array of a write ($v[$k] = ..., or a
// reference &$v[$k]), as Zend/zend_execute.c's assign-dim handlers treat
// it: an array is written in place; null, and false after the
// FalseToArrayDeprecation notice (deprecated true), become a new array
// (created true) the caller stores in $v; a string fails with "Cannot
// access offset of type string on string" (configuration keys are never
// the numeric offsets PHP would write a byte at), the other scalars with
// "Cannot use a scalar value as an array".
func WritableArray(v any) (arr *Array, created, deprecated bool, err *EngineError) {
	switch c := v.(type) {
	case *Array:
		return c, false, false, nil
	case nil:
		return NewArray(), true, false, nil
	case bool:
		if !c {
			return NewArray(), true, true, nil
		}
	case string:
		return nil, false, false, &EngineError{Class: "TypeError", Message: "Cannot access offset of type string on string"}
	}

	return nil, false, false, &EngineError{Class: "Error", Message: "Cannot use a scalar value as an array"}
}
