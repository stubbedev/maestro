// PHP runtime functions the Composer\Util ports depend on, with PHP's exact
// semantics: strerror messages and the warnings built from them.

package util

import (
	"github.com/stubbedev/maestro/internal/php"
)

func isASCIIAlpha(c byte) bool {
	return (c|0x20) >= 'a' && (c|0x20) <= 'z'
}

func isASCIIDigit(c byte) bool {
	return c >= '0' && c <= '9'
}

func isASCIIAlnum(c byte) bool {
	return isASCIIDigit(c) || isASCIIAlpha(c)
}

// isPCRESpace reports whether c matches PCRE2's \s without UCP: space, \t,
// \n, \v, \f and \r.
func isPCRESpace(c byte) bool {
	return c == ' ' || (c >= '\t' && c <= '\r')
}

// phpWarning formats the message error_get_last() holds after a filesystem
// function failed: "func(arg): Message".
func phpWarning(fn, arg string, err error) string {
	return fn + "(" + arg + "): " + php.Strerror(err)
}

// warning is the ErrorException Composer's error handler makes of the
// warning a failed filesystem function emits.
func warning(fn, arg string, err error) error {
	return &ErrorException{Message: phpWarning(fn, arg, err)}
}

// streamWarning is the warning of a function that failed to open arg:
// "func(arg): Failed to open stream: Message".
func streamWarning(fn, arg string, err error) error {
	return &ErrorException{Message: fn + "(" + arg + "): Failed to open stream: " + php.Strerror(err)}
}

// dirIteratorError is the UnexpectedValueException
// RecursiveDirectoryIterator throws for a directory it cannot open.
func dirIteratorError(dir string, err error) error {
	return &UnexpectedValueError{Message: "RecursiveDirectoryIterator::__construct(" + dir + "): Failed to open directory: " + php.Strerror(err)}
}
