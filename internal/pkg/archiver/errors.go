// The exceptions of PHP's phar extension that PharArchiver lets through.

package archiver

// PharError is \PharException, e.g. a name too long for the tar format.
type PharError struct{ Message string }

func (e *PharError) Error() string { return e.Message }

// BadMethodCallError is \BadMethodCallException, which PharData throws for
// entry names it does not accept.
type BadMethodCallError struct{ Message string }

func (e *BadMethodCallError) Error() string { return e.Message }
