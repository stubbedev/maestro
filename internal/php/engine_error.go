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
