// Exceptions thrown by src/*.php and the Symfony Finder code they rely on.

package classmap

// Exception is an exception thrown by the ported PHP code. Class is the
// fully qualified name of the PHP exception class, which Composer prints
// when it renders an uncaught exception; Message is its exact message.
type Exception struct {
	Class   string
	Message string
	// Prev is the $previous exception (not unwrapped, as PHP's catch
	// does not look at it).
	Prev error
}

func (e *Exception) Error() string { return e.Message }

// PHPClass is get_class($e) and $e->getCode() (util.PHPClasser).
func (e *Exception) PHPClass() (string, int) { return e.Class, 0 }

// PHPPrevious implements phperr.Chained.
func (e *Exception) PHPPrevious() error { return e.Prev }

// IsRuntimeException reports whether the PHP class extends
// \RuntimeException, which is what AutoloadGenerator::createLoader()
// catches.
func (e *Exception) IsRuntimeException() bool {
	switch e.Class {
	case classRuntime, classUnexpectedValue, classAccessDenied:
		return true
	}

	return false
}

// The exception classes this package throws.
const (
	classRuntime           = "RuntimeException"
	classUnexpectedValue   = "UnexpectedValueException"
	classAccessDenied      = `Symfony\Component\Finder\Exception\AccessDeniedException`
	classDirectoryNotFound = `Symfony\Component\Finder\Exception\DirectoryNotFoundException`
	classLogic             = "LogicException"
	classOutOfBounds       = "OutOfBoundsException"
)

func newException(class, message string) *Exception {
	return &Exception{Class: class, Message: message}
}
