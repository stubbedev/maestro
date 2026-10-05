// The PHP exception classes the Composer\Package classes throw that
// internal/util does not already provide.

package pkg

// TypeError is PHP's \TypeError, raised where Composer's strictly typed
// code receives a value of the wrong type (a malformed package array).
// PHP appends where the call came from; the message here stops before
// that part.
type TypeError struct{ Message string }

func (e *TypeError) Error() string { return e.Message }

// SecurityError is Composer\Exception\SecurityException.
type SecurityError struct{ Message string }

func (e *SecurityError) Error() string { return e.Message }
