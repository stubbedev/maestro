// Ports the engine exceptions (Zend/zend_exceptions.c: ValueError,
// ArgumentCountError, TypeError) that the functions of this package report.

package php

import "github.com/stubbedev/maestro/internal/phperr"

// EngineError is a PHP engine Throwable a ported function throws, such as
// sprintf()'s ValueError or ArgumentCountError. Class is the PHP class name
// and Message the exception message, both exactly as PHP reports them.
type EngineError struct {
	Class   string
	Message string
	phperr.Site
}

func (e *EngineError) Error() string { return e.Message }

// Called makes e the TypeError PHP raises for a wrongly typed argument of
// the userland function declared at decl (the line its declaration starts
// on) and called at file:line: getFile()/getLine() name the declaration,
// the message gets where the call came from (phperr.CalledIn), and the
// trace starts with the call of function (as traces name it,
// "Composer\Config->merge").
func (e *EngineError) Called(function string, decl phperr.Site, file string, line int) *EngineError {
	e.Message += phperr.CalledIn(file, line)
	e.Site = decl
	phperr.Call(e, function, file, line)

	return e
}

// Raised makes e an engine error raised at file:line of Composer's
// sources: an internal function's TypeError or ValueError (whose call,
// function, heads the trace) or an operator's (function "").
func (e *EngineError) Raised(function, file string, line int) *EngineError {
	e.Site = phperr.At(file, line)
	if function != "" {
		phperr.Call(e, function, file, line)
	}

	return e
}

func valueError(msg string) *EngineError { return &EngineError{Class: "ValueError", Message: msg} }
