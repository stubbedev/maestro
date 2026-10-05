package util

import "github.com/stubbedev/maestro/internal/io"

// IO is the part of Composer\IO\IOInterface the Util classes write to;
// every io.IO is one.
type IO interface {
	IsDebug() bool
	WriteError(message string, newline bool, verbosity io.Verbosity)
	WriteRaw(message string, newline bool, verbosity io.Verbosity)
	WriteErrorRaw(message string, newline bool, verbosity io.Verbosity)
}

// IOInterface verbosity levels (io's constants).
const (
	VerbosityQuiet       = io.Quiet
	VerbosityNormal      = io.Normal
	VerbosityVerbose     = io.Verbose
	VerbosityVeryVerbose = io.VeryVerbose
	VerbosityDebug       = io.Debug
)
