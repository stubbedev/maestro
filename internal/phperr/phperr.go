// Package phperr holds what maestro's errors keep of the PHP exceptions
// they stand for beyond their class (util.PHPClassOf): the previous
// exception ($e->getPrevious()), which errors expose through Chained, and
// the root Composer's sources are reported under (Root), which the plugin
// runtime gives the shim. How errors look is maestro's own (internal/ui,
// docs/PORTING.md, #13); maestro records neither throw sites nor PHP call
// stacks.
//
// An exception constructed with a $previous exception keeps it in a field
// the error type exposes through Chained (not Unwrap: PHP's catch never
// looks at the previous exception, so errors.As must not either).
package phperr

import (
	"os"
	"sync"
)

// Chained is implemented by errors carrying PHP's $previous exception
// ($e->getPrevious()), which errors show as their causes.
type Chained interface {
	PHPPrevious() error
}

// PreviousOf returns err's PHP previous exception ($e->getPrevious()), or
// nil. The error itself is consulted first; failing that, the first error
// in its Unwrap tree with the same message (a Go wrapper that only adds
// context Composer does not have, e.g. one that keeps the error's message
// as is).
func PreviousOf(err error) error {
	if c, ok := err.(Chained); ok { // the object itself, as PHP's getPrevious()
		return c.PHPPrevious()
	}
	var prev error
	msg := err.Error()
	walk(err, func(e error) bool {
		c, ok := e.(Chained) // walk visits every error of the tree itself
		if !ok || e.Error() != msg {
			return false
		}
		prev = c.PHPPrevious()

		return prev != nil
	})

	return prev
}

// walk visits the errors err wraps, depth first, until visit returns true.
func walk(err error, visit func(error) bool) bool {
	switch u := err.(type) { //nolint:errorlint // walking the Unwrap tree by hand
	case interface{ Unwrap() error }:
		if next := u.Unwrap(); next != nil {
			return visit(next) || walk(next, visit)
		}
	case interface{ Unwrap() []error }:
		for _, next := range u.Unwrap() {
			if next != nil && (visit(next) || walk(next, visit)) {
				return true
			}
		}
	}

	return false
}

var root struct {
	sync.Mutex
	path string
	set  bool
}

// Root is the directory Composer's sources are reported under: maestro is
// Composer's code built into one executable, as composer.phar is, so its
// files are those of a phar at the executable's path
// ("phar:///usr/local/bin/maestro/src/Composer/Factory.php"), where PHP
// names a composer.phar's "phar:///usr/local/bin/composer/src/...". The
// plugin runtime sends it to the shim at boot (composerRoot), which names
// the files of the bundled Composer classes under it.
func Root() string {
	root.Lock()
	defer root.Unlock()
	if !root.set {
		root.set = true
		if exe, err := os.Executable(); err == nil {
			root.path = "phar://" + exe
		}
	}

	return root.path
}

// SetRoot sets the directory Root reports (tests run maestro's code in
// their own executables).
func SetRoot(path string) {
	root.Lock()
	defer root.Unlock()
	root.path, root.set = path, true
}
