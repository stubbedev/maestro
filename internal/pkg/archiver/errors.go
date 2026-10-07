// The exceptions of PHP's phar extension that PharArchiver lets through.

package archiver

// PharError is \PharException, e.g. a name too long for the tar format.
type PharError struct {
	Message string
}

// PHPClass implements phperr.Exception.
func (*PharError) PHPClass() string { return "PharException" }

func (e *PharError) Error() string { return e.Message }

// BadMethodCallError is \BadMethodCallException, which PharData throws for
// entry names it does not accept.
type BadMethodCallError struct {
	Message string
}

// PHPClass implements phperr.Exception.
func (*BadMethodCallError) PHPClass() string { return "BadMethodCallException" }

func (e *BadMethodCallError) Error() string { return e.Message }
