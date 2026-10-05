// Package phperr records where Composer throws an exception, so the
// console can render a maestro error exactly as Symfony renders the PHP
// exception ("In Factory.php line 317:").
//
// An error type standing for a PHP exception embeds Site, and each
// construction site fills it with the basename of the PHP file that
// constructs the exception in Composer 2.10.3 (or the vendored library
// Composer runs: "JsonParser.php", "VersionParser.php", ...) and the line
// of its `new` expression, which is what getFile()/getLine() report:
//
//	return &util.RuntimeError{Message: msg, Site: phperr.At("Factory.php", 317)}
//
// An exception constructed with a $previous exception keeps it in a field
// the error type exposes through Chained (not Unwrap: PHP's catch never
// looks at the previous exception, so errors.As must not either).
//
// A zero Site is an unknown throw site and renders as "n/a".
package phperr

// Site is the throw site of a PHP exception: the basename of the file
// constructing it and the line of the `new` expression there.
type Site struct {
	File string
	Line int
}

// At returns the Site file:line.
func At(file string, line int) Site { return Site{File: file, Line: line} }

// ThrowSite implements Sited.
func (s Site) ThrowSite() Site { return s }

// Known reports whether the site is set.
func (s Site) Known() bool { return s.File != "" }

// Sited is implemented by errors that carry a PHP throw site (every type
// embedding Site).
type Sited interface {
	ThrowSite() Site
}

// Chained is implemented by errors carrying PHP's $previous exception
// ($e->getPrevious()), which Symfony renders below the exception.
type Chained interface {
	PHPPrevious() error
}

// SiteOf returns the throw site of err. The error itself is consulted
// first; failing that, the first error in its Unwrap tree with a known
// site and the same message (a Go wrapper that only adds context Composer
// does not have, e.g. one that keeps the error's message as is).
func SiteOf(err error) (Site, bool) {
	if err == nil {
		return Site{}, false
	}
	if s, ok := err.(Sited); ok { // the object itself first, as PHP's getFile()
		if site := s.ThrowSite(); site.Known() {
			return site, true
		}
	}
	msg := err.Error()
	var found Site
	walk(err, func(e error) bool {
		s, ok := e.(Sited) // walk visits every error of the tree itself
		if !ok || e.Error() != msg {
			return false
		}
		if site := s.ThrowSite(); site.Known() {
			found = site

			return true
		}

		return false
	})

	return found, found.Known()
}

// PreviousOf returns err's PHP previous exception ($e->getPrevious()), or
// nil.
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

// Is reports whether err's throw site (SiteOf) is file:line.
func Is(err error, file string, line int) bool {
	site, ok := SiteOf(err)

	return ok && site.File == file && site.Line == line
}
