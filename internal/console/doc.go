// Package console is a port of the subset of symfony/console v5.4.47 (the
// version Composer 2.10.3 ships) that Composer uses, plus Composer's own
// console helpers (src/Composer/Console/*, src/Composer/Question/*). Output
// must be byte-identical to the PHP implementation, so the code follows the
// PHP sources closely, quirks included.
//
// PHP exceptions map to errors as follows. Failures caused by user input
// (argv parsing, binding, validation, command lookup, question answers) are
// returned as errors. Failures that PHP raises from deep inside calls that
// are effectively infallible for well-formed programs (an unknown option name
// passed to Input.Option, a badly nested style tag reaching the formatter, an
// invalid colour) are raised as panics carrying a *Error; Application.Run
// recovers those and renders them exactly like PHP's run() renders an
// uncaught exception.
//
// PHP values held by inputs (argument and option values, defaults) are
// represented as any, holding one of: nil, bool, string, []string, int or
// float64.
package console
