package util

// IO is the part of Composer\IO\IOInterface the Util classes write to.
// Verbosity values are IOInterface's constants.
type IO interface {
	IsDebug() bool
	WriteError(message string, newline bool, verbosity int)
	WriteRaw(message string, newline bool, verbosity int)
	WriteErrorRaw(message string, newline bool, verbosity int)
}

// IOInterface verbosity levels.
const (
	VerbosityQuiet       = 1
	VerbosityNormal      = 2
	VerbosityVerbose     = 4
	VerbosityVeryVerbose = 8
	VerbosityDebug       = 16
)
