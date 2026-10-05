// Ports src/Composer/Repository/InvalidRepositoryException.php and
// src/Composer/Repository/RepositorySecurityException.php.

package repository

// InvalidRepositoryError is Composer\Repository\InvalidRepositoryException:
// a package repository is utterly broken.
type InvalidRepositoryError struct{ Message string }

func (e *InvalidRepositoryError) Error() string { return e.Message }

// SecurityError is Composer\Repository\RepositorySecurityException: a
// security problem, like a broken or missing signature.
type SecurityError struct{ Message string }

func (e *SecurityError) Error() string { return e.Message }

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
