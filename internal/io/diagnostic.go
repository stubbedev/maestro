package io

import "github.com/stubbedev/maestro/internal/ui"

// IsErrorDecorated reports whether out's error output (stderr, where
// diagnostics go) is decorated: the error output's own decoration for a
// ConsoleIO, else IsDecorated.
func IsErrorDecorated(out IO) bool {
	if d, ok := out.(interface{ IsErrorDecorated() bool }); ok {
		return d.IsErrorDecorated()
	}

	return out.IsDecorated()
}

// IsErrorDecorated reports whether the error output is decorated.
func (c *ConsoleIO) IsErrorDecorated() bool { return c.errorOutput().IsDecorated() }

// WriteDiagnostic writes d to out's error output as internal/ui renders it
// (styled when that output is decorated, with its details when out is
// verbose), shown from verbosity on.
func WriteDiagnostic(out IO, d ui.Diagnostic, verbosity Verbosity) {
	for _, l := range d.Lines(ui.Options{Decorated: IsErrorDecorated(out), Verbose: out.IsVerbose()}) {
		out.WriteErrorRaw(l, true, verbosity)
	}
}
