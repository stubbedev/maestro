// Ports src/Composer/IO/IOInterface.php (Composer).

package io

import (
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/php"
)

// Verbosity is an IOInterface verbosity level.
type Verbosity int

// The IOInterface verbosity levels.
const (
	Quiet       Verbosity = 1
	Normal      Verbosity = 2
	Verbose     Verbosity = 4
	VeryVerbose Verbosity = 8
	Debug       Verbosity = 16
)

// consoleVerbosity maps an IO verbosity to the console's
// (ConsoleIO::$verbosityMap). An unknown level maps to 0, which the console
// output treats as normal.
func consoleVerbosity(v Verbosity) int {
	switch v {
	case Quiet:
		return console.VerbosityQuiet
	case Normal:
		return console.VerbosityNormal
	case Verbose:
		return console.VerbosityVerbose
	case VeryVerbose:
		return console.VerbosityVeryVerbose
	case Debug:
		return console.VerbosityDebug
	}

	return 0
}

// PSR-3 log levels (Psr\Log\LogLevel).
const (
	LevelEmergency = "emergency"
	LevelAlert     = "alert"
	LevelCritical  = "critical"
	LevelError     = "error"
	LevelWarning   = "warning"
	LevelNotice    = "notice"
	LevelInfo      = "info"
	LevelDebug     = "debug"
)

// DefaultSelectErrorMessage is the default $errorMessage of select().
const DefaultSelectErrorMessage = `Value "%s" is invalid`

// Authentication is one entry of the authentication store. Username is nil
// only for the "not set" value getAuthentication() returns; Password may be
// nil (PHP null).
type Authentication struct {
	Username *string
	Password *string
}

// RepositoryAuthentication is one entry of getAuthentications(), in
// insertion order.
type RepositoryAuthentication struct {
	Repository string
	Authentication
}

// Config is the part of Composer\Config that loadConfiguration() uses
// (internal/config implements it). Get returns values in the internal/php
// value model; Merge is Config::merge($config, $source).
type Config interface {
	Get(key string) any
	Merge(config *php.Array, source string)
}

// IO is IOInterface (which extends PSR-3's LoggerInterface).
//
// PHP's string|array $messages parameters are split into the string
// methods and the WriteMessages/WriteErrorMessages variants. Questions
// return the answer in the internal/php value model, and an error where PHP
// throws.
type IO interface {
	// PHPClass is get_class($io).
	php.Classer
	IsInteractive() bool
	IsVerbose() bool
	IsVeryVerbose() bool
	IsDebug() bool
	IsDecorated() bool

	Write(message string, newline bool, verbosity Verbosity)
	WriteMessages(messages []string, newline bool, verbosity Verbosity)
	WriteError(message string, newline bool, verbosity Verbosity)
	WriteErrorMessages(messages []string, newline bool, verbosity Verbosity)
	WriteRaw(message string, newline bool, verbosity Verbosity)
	WriteErrorRaw(message string, newline bool, verbosity Verbosity)
	// Overwrite and OverwriteError take PHP's ?int $size as size, with -1
	// meaning null (the length of the last message).
	Overwrite(message string, newline bool, size int, verbosity Verbosity)
	OverwriteError(message string, newline bool, size int, verbosity Verbosity)

	Ask(question string, def any) (any, error)
	AskConfirmation(question string, def bool) (bool, error)
	// AskAndValidate takes PHP's ?int $attempts as attempts, 0 meaning null.
	AskAndValidate(question string, validator console.Validator, attempts int, def any) (any, error)
	AskAndHideAnswer(question string) (any, error)
	// Select takes PHP's int|false $attempts as attempts, 0 meaning false.
	Select(question string, choices *php.Array, def any, attempts int, errorMessage string, multiselect bool) (any, error)

	Authentications() []RepositoryAuthentication
	HasAuthentication(repositoryName string) bool
	Authentication(repositoryName string) Authentication
	SetAuthentication(repositoryName, username string, password *string)
	// LoadConfiguration loads the auth settings of config. setTimeout
	// receives the "process-timeout" value (ProcessExecutor::setTimeout(),
	// which lives above this package); nil skips it.
	LoadConfiguration(config Config, setTimeout func(timeout int)) error

	Emergency(message string, context *php.Array)
	Alert(message string, context *php.Array)
	Critical(message string, context *php.Array)
	Error(message string, context *php.Array)
	Warning(message string, context *php.Array)
	Notice(message string, context *php.Array)
	Info(message string, context *php.Array)
	Debug(message string, context *php.Array)
	Log(level, message string, context *php.Array)
}

// Foreign is implemented by IOs whose methods are code maestro does not
// know (an IO created in PHP by plugin code, docs/PLUGINS.md §5.9): every
// call is observable there, so maestro makes exactly the calls Composer
// makes, in its order, where it would otherwise take shortcuts that skip
// or reorder calls nothing would see on its own IOs.
type Foreign interface {
	IO
	ForeignIO()
}

// IsForeign reports whether out is a Foreign IO.
func IsForeign(out IO) bool {
	_, ok := out.(Foreign)

	return ok
}
