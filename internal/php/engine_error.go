// Ports the engine exceptions (Zend/zend_exceptions.c: ValueError,
// ArgumentCountError, TypeError) that the functions of this package report.

package php

// EngineError is a PHP engine Throwable a ported function throws, such as
// sprintf()'s ValueError or ArgumentCountError. Class is the PHP class name
// and Message the exception message, both exactly as PHP reports them.
type EngineError struct {
	Class   string
	Message string
}

func (e *EngineError) Error() string { return e.Message }

func valueError(msg string) *EngineError { return &EngineError{Class: "ValueError", Message: msg} }

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
// "Cannot use a scalar value as an array". The caller sets the error's
// site (Raised).
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
