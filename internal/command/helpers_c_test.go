package command_test

import "github.com/stubbedev/maestro/internal/phperr"

// phpClassOf is get_class($e).
func phpClassOf(err error) string { return phperr.Class(err) }
