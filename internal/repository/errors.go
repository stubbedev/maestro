// Ports src/Composer/Repository/InvalidRepositoryException.php and
// src/Composer/Repository/RepositorySecurityException.php.

package repository

// InvalidRepositoryError is Composer\Repository\InvalidRepositoryException:
// a package repository is utterly broken.
type InvalidRepositoryError struct {
	Message string
}

func (e *InvalidRepositoryError) Error() string { return e.Message }

// PHPClass implements phperr.Exception.
func (*InvalidRepositoryError) PHPClass() string {
	return `Composer\Repository\InvalidRepositoryException`
}

// SecurityError is Composer\Repository\RepositorySecurityException: a
// security problem, like a broken or missing signature.
type SecurityError struct {
	Message string
}

func (e *SecurityError) Error() string { return e.Message }

// PHPClass implements phperr.Exception.
func (*SecurityError) PHPClass() string {
	return `Composer\Repository\RepositorySecurityException`
}

// wrappedError is a PHP exception created with a previous exception
// (new \RuntimeException($message, 0, $previous)).
type wrappedError struct {
	err      error
	previous error
}

func (e *wrappedError) Error() string { return e.err.Error() }

// Unwrap returns the exception itself, so errors.As finds its class; the
// previous one is only getPrevious() (PHPPrevious), which `catch` does not
// look at.
func (e *wrappedError) Unwrap() error { return e.err }

// PHPPrevious implements phperr.Chained.
func (e *wrappedError) PHPPrevious() error { return e.previous }
