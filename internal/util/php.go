// PHP runtime functions the Composer\Util ports depend on, with PHP's exact
// semantics: dirname, basename, var_export of a string, strerror messages.

package util

import (
	"errors"
	"syscall"
	"unicode"
	"unicode/utf8"
)

// isPathSep reports whether c separates path segments for PHP's dirname and
// basename: only '/' on Unix, '/' and '\' on Windows.
func isPathSep(c byte, windows bool) bool {
	return c == '/' || (windows && c == '\\')
}

// phpDirname ports zend_dirname (PHP's dirname with one level).
func phpDirname(path string, windows bool) string {
	if path == "" {
		return ""
	}

	drive := ""
	if windows && len(path) >= 2 && isASCIIAlpha(path[0]) && path[1] == ':' {
		// The drive spec is kept as is; dirname("c:") is "c:".
		drive, path = path[:2], path[2:]
		if path == "" {
			return drive
		}
	}

	sep := "/"
	if windows {
		sep = `\`
	}

	end := len(path) - 1
	for end >= 0 && isPathSep(path[end], windows) {
		end--
	}

	if end < 0 {
		return drive + sep
	}

	for end >= 0 && !isPathSep(path[end], windows) {
		end--
	}

	if end < 0 {
		return drive + "."
	}

	for end >= 0 && isPathSep(path[end], windows) {
		end--
	}

	if end < 0 {
		return drive + sep
	}

	return drive + path[:end+1]
}

// phpBasename ports php_basename without a suffix.
func phpBasename(path string, windows bool) string {
	end := len(path)
	for end > 0 && isPathSep(path[end-1], windows) {
		end--
	}

	start := end
	for start > 0 && !isPathSep(path[start-1], windows) {
		start--
	}

	return path[start:end]
}

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

// strerror renders an OS error the way PHP's warnings do, from the C
// library's strerror: Go's errno texts are the glibc ones, lowercased.
func strerror(err error) string {
	if errno, ok := errors.AsType[syscall.Errno](err); ok {
		err = errno
	}

	msg := err.Error()
	r, size := utf8.DecodeRuneInString(msg)

	return string(unicode.ToUpper(r)) + msg[size:]
}

// phpWarning formats the message error_get_last() holds after a filesystem
// function failed: "func(arg): Message".
func phpWarning(fn, arg string, err error) string {
	return fn + "(" + arg + "): " + strerror(err)
}

// warning is the ErrorException Composer's error handler makes of the
// warning a failed filesystem function emits.
func warning(fn, arg string, err error) error {
	return &ErrorException{Message: phpWarning(fn, arg, err)}
}

// streamWarning is the warning of a function that failed to open arg:
// "func(arg): Failed to open stream: Message".
func streamWarning(fn, arg string, err error) error {
	return &ErrorException{Message: fn + "(" + arg + "): Failed to open stream: " + strerror(err)}
}

// dirIteratorError is the UnexpectedValueException
// RecursiveDirectoryIterator throws for a directory it cannot open.
func dirIteratorError(dir string, err error) error {
	return &UnexpectedValueError{Message: "RecursiveDirectoryIterator::__construct(" + dir + "): Failed to open directory: " + strerror(err)}
}
