// Ports src/Composer/EventDispatcher/ScriptExecutionException.php and the
// other exceptions EventDispatcher throws.

package eventdispatcher

// ScriptExecutionError is Composer\EventDispatcher\ScriptExecutionException,
// thrown when a script exits with a non-zero code. Code is that exit code,
// which Application::doRun returns.
type ScriptExecutionError struct {
	Message string
	Code    int
}

func (e *ScriptExecutionError) Error() string { return e.Message }

// PHPClass implements phperr.Exception.
func (*ScriptExecutionError) PHPClass() string {
	return `Composer\EventDispatcher\ScriptExecutionException`
}

// PHPCode implements phperr.Coded: the script's exit code.
func (e *ScriptExecutionError) PHPCode() int { return e.Code }

// Error is any other exception the dispatcher throws (RuntimeException,
// LogicException, PHP's \Error), with its PHP class.
type Error struct {
	Class   string
	Message string
}

func (e *Error) Error() string { return e.Message }

// PHPClass implements phperr.Exception.
func (e *Error) PHPClass() string { return e.Class }

func runtimeError(message string) *Error {
	return &Error{Class: "RuntimeException", Message: message}
}
