package command_test

import "github.com/stubbedev/maestro/internal/command"

// phpClassOf is get_class($e).
func phpClassOf(err error) string { return command.PHPClass(err) }
