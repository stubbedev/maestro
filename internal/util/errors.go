// The PHP exception classes Composer\Util throws, as Go errors carrying the
// exact message, so callers can tell them apart with errors.As as Composer
// tells them apart with catch.

package util

// RuntimeError is PHP's \RuntimeException, or a library's subclass of it
// (Class).
type RuntimeError struct {
	Message string
	// Prev is the $previous exception, rendered after this one (it is
	// not unwrapped: PHP's catch does not look at it).
	Prev error
	// Class is get_class($e) of a library's subclass of \RuntimeException
	// that adds nothing Composer catches (symfony/process's
	// Symfony\Component\Process\Exception\RuntimeException); empty for
	// \RuntimeException itself.
	Class string
}

func (e *RuntimeError) Error() string { return e.Message }

// PHPClass implements phperr.Exception.
func (e *RuntimeError) PHPClass() string { return orClass(e.Class, "RuntimeException") }

// PHPPrevious implements phperr.Chained.
func (e *RuntimeError) PHPPrevious() error { return e.Prev }

// InvalidArgumentError is PHP's \InvalidArgumentException, or a library's
// subclass of it (Class).
type InvalidArgumentError struct {
	Message string
	// Prev is the $previous exception, rendered after this one (it is
	// not unwrapped: PHP's catch does not look at it).
	Prev error
	// Class is get_class($e) of a library's subclass of
	// \InvalidArgumentException (symfony/finder's
	// DirectoryNotFoundException, symfony/process's
	// InvalidArgumentException); empty for \InvalidArgumentException
	// itself.
	Class string
}

func (e *InvalidArgumentError) Error() string { return e.Message }

// PHPClass implements phperr.Exception.
func (e *InvalidArgumentError) PHPClass() string {
	return orClass(e.Class, "InvalidArgumentException")
}

// PHPPrevious implements phperr.Chained.
func (e *InvalidArgumentError) PHPPrevious() error { return e.Prev }

// LogicError is PHP's \LogicException, or a library's subclass of it
// (Class).
type LogicError struct {
	Message string
	// Prev is the $previous exception, rendered after this one (it is
	// not unwrapped: PHP's catch does not look at it).
	Prev error
	// Class is get_class($e) of a library's subclass of \LogicException
	// (symfony/process's Symfony\Component\Process\Exception\LogicException);
	// empty for \LogicException itself.
	Class string
}

func (e *LogicError) Error() string { return e.Message }

// PHPClass implements phperr.Exception.
func (e *LogicError) PHPClass() string { return orClass(e.Class, "LogicException") }

// PHPPrevious implements phperr.Chained.
func (e *LogicError) PHPPrevious() error { return e.Prev }

// ErrorException is the \ErrorException Composer's ErrorHandler turns a PHP
// warning into, e.g. "copy(a): Failed to open stream: No such file or
// directory".
type ErrorException struct {
	Message string
}

func (e *ErrorException) Error() string { return e.Message }

// PHPClass implements phperr.Exception.
func (*ErrorException) PHPClass() string { return "ErrorException" }

// IOError is symfony/filesystem's IOException, carrying the path involved,
// or its subclass FileNotFoundException (Class).
type IOError struct {
	Message string
	Path    string
	// Class is get_class($e) of a subclass of IOException
	// (ClassFileNotFound); empty for IOException itself.
	Class string
}

// PHPClass implements phperr.Exception.
func (e *IOError) PHPClass() string { return orClass(e.Class, ClassIOException) }

// The symfony/filesystem, symfony/finder and symfony/process exception
// classes maestro represents with util's generic error types (their Class).
const (
	ClassIOException       = `Symfony\Component\Filesystem\Exception\IOException`
	ClassFileNotFound      = `Symfony\Component\Filesystem\Exception\FileNotFoundException`
	ClassDirectoryNotFound = `Symfony\Component\Finder\Exception\DirectoryNotFoundException`
	ClassAccessDenied      = `Symfony\Component\Finder\Exception\AccessDeniedException`
	ClassProcessRuntime    = `Symfony\Component\Process\Exception\RuntimeException`
	ClassProcessLogic      = `Symfony\Component\Process\Exception\LogicException`
	ClassProcessInvalidArg = `Symfony\Component\Process\Exception\InvalidArgumentException`
)

func (e *IOError) Error() string { return e.Message }

// UnexpectedValueError is PHP's \UnexpectedValueException, or a library's
// subclass of it (Class).
type UnexpectedValueError struct {
	Message string
	// Prev is the $previous exception, rendered after this one (it is
	// not unwrapped: PHP's catch does not look at it).
	Prev error
	// Class is get_class($e) of a library's subclass of
	// \UnexpectedValueException (symfony/finder's AccessDeniedException);
	// empty for \UnexpectedValueException itself.
	Class string
}

func (e *UnexpectedValueError) Error() string { return e.Message }

// PHPClass implements phperr.Exception.
func (e *UnexpectedValueError) PHPClass() string {
	return orClass(e.Class, "UnexpectedValueException")
}

// PHPPrevious implements phperr.Chained.
func (e *UnexpectedValueError) PHPPrevious() error { return e.Prev }

// SecurityError is Composer\Exception\SecurityException.
type SecurityError struct {
	Message string
}

func (e *SecurityError) Error() string { return e.Message }

// PHPClass implements phperr.Exception.
func (*SecurityError) PHPClass() string { return `Composer\Exception\SecurityException` }

// orClass is class, or base when the error is of the base class itself.
func orClass(class, base string) string {
	if class != "" {
		return class
	}

	return base
}
