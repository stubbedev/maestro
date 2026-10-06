// Ports src/Composer/Repository/InvalidRepositoryException.php and
// src/Composer/Repository/RepositorySecurityException.php.

package repository

import (
	"github.com/stubbedev/maestro/internal/util"
)

// InvalidRepositoryError is Composer\Repository\InvalidRepositoryException:
// a package repository is utterly broken.
type InvalidRepositoryError struct {
	Message string
}

func (e *InvalidRepositoryError) Error() string { return e.Message }

// PHPClass implements util.PHPClasser.
func (*InvalidRepositoryError) PHPClass() (string, int) {
	return `Composer\Repository\InvalidRepositoryException`, 0
}

// SecurityError is Composer\Repository\RepositorySecurityException: a
// security problem, like a broken or missing signature.
type SecurityError struct {
	Message string
}

func (e *SecurityError) Error() string { return e.Message }

// PHPClass implements util.PHPClasser.
func (*SecurityError) PHPClass() (string, int) {
	return `Composer\Repository\RepositorySecurityException`, 0
}

// wrappedError is a PHP exception created with a previous exception
// (new \RuntimeException($message, 0, $previous)).
type wrappedError struct {
	err      error
	previous error
}

func (e *wrappedError) Error() string { return e.err.Error() }

// Unwrap returns the exception itself and the previous one, so errors.As
// finds both.
func (e *wrappedError) Unwrap() []error { return []error{e.err, e.previous} }

// PHPPrevious implements phperr.Chained.
func (e *wrappedError) PHPPrevious() error { return e.previous }

// isPHPError reports whether err is one of PHP's \Error classes (a
// TypeError, ...), which `catch (\Exception $e)` does not catch.
func isPHPError(err error) bool {
	switch class, _ := util.PHPClassOf(err); class {
	case "Error", "TypeError", "ValueError", "ArgumentCountError", "ArithmeticError", "DivisionByZeroError":
		return true
	}

	return false
}
