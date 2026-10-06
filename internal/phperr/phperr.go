// Package phperr records where Composer throws an exception and the PHP
// call stack it was constructed in, so the console can render a maestro
// error exactly as Symfony renders the PHP exception ("In Factory.php line
// 317:", and at -v the "Exception trace:" frames).
//
// An error type standing for a PHP exception embeds Site, and each
// construction site fills it with the PHP file that constructs the
// exception in Composer 2.10.3 (or the vendored library Composer runs:
// "JsonParser.php", "VersionParser.php", ...) and the line of its `new`
// expression, which is what getFile()/getLine() report:
//
//	return &util.RuntimeError{Message: msg, Site: phperr.At("Factory.php", 317)}
//
// The file is a basename that Path resolves within Composer's tree, or the
// path relative to Composer's root where the basename is ambiguous
// ("vendor/symfony/filesystem/Filesystem.php").
//
// An exception constructed with a $previous exception keeps it in a field
// the error type exposes through Chained (not Unwrap: PHP's catch never
// looks at the previous exception, so errors.As must not either).
//
// A zero Site is an unknown throw site and renders as "n/a".
//
// # Call stacks
//
// PHP records an exception's trace ($e->getTrace()) when it is
// constructed: the chain of calls from bin/composer down to the function
// constructing it. maestro records the same frames as the error travels
// back up through the ports of those functions: where a ported function
// gets an error from its port of a PHP call, it adds that call's frame,
//
//	if err := file.ValidateSchema(...); err != nil {
//		return phperr.Call(err, `Composer\Json\JsonFile->validateSchema`, "Factory.php", 313)
//	}
//
// i.e. the callee as PHP's trace names it and the file and line of the call
// in the caller's PHP source. As the error goes up the port of each frame
// once, innermost first, the frames end up in getTrace() order. Frames are
// added to the error and to its previous exceptions (constructed further
// down the same stack); an error that is caught and replaced starts a trace
// of its own, as a new PHP exception does. Calls the Go port does not make
// (or makes in another shape) are not recorded, so a trace is complete only
// along the paths whose ports add their frames.
package phperr

import (
	"os"
	"slices"
	"strings"
	"sync"
)

// Site is the throw site of a PHP exception: the file constructing it and
// the line of the `new` expression there, and the frames of the call stack
// it was constructed in (Frames).
type Site struct {
	File string
	Line int
	Frames
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

// Frame is a frame of a PHP exception's trace: the function called
// (Function: "Class->method", "Class::method", "function" or a closure's
// name, "Class->{closure:Class::method():159}"), and the file and line of
// the call (File is a file as At takes it).
type Frame struct {
	Function string
	File     string
	Line     int
}

// Frames holds the trace frames of an exception; error types embed it
// (Site does) and their pointers implement Traced.
type Frames struct {
	// a pointer keeps Site comparable
	list *[]Frame
}

// AddFrame implements Traced.
func (f *Frames) AddFrame(fr Frame) {
	if f.list == nil {
		f.list = new([]Frame)
	}
	*f.list = append(*f.list, fr)
}

// PHPTrace implements Traced.
func (f *Frames) PHPTrace() []Frame {
	if f.list == nil {
		return nil
	}

	return *f.list
}

// Traced is implemented by errors recording the trace frames of the PHP
// exception they stand for (pointers to types embedding Site or Frames).
type Traced interface {
	error
	AddFrame(Frame)
	PHPTrace() []Frame
}

// Chained is implemented by errors carrying PHP's $previous exception
// ($e->getPrevious()), which Symfony renders below the exception.
type Chained interface {
	PHPPrevious() error
}

// Call records that err reached the caller of function through its call
// at file:line: it adds the frame to err's trace and to the traces of its
// previous exceptions, and returns err (nil for nil).
func Call(err error, function, file string, line int) error {
	if err == nil {
		return nil
	}
	fr := Frame{Function: function, File: file, Line: line}
	seen := map[error]bool{}
	for e := err; e != nil; e = PreviousOf(e) {
		t := tracedOf(e)
		if t == nil {
			continue
		}
		if seen[t] {
			break
		}
		seen[t] = true
		t.AddFrame(fr)
	}

	return err
}

// Calls records several frames, innermost first, as successive Calls.
func Calls(err error, frames ...Frame) error {
	for _, f := range frames {
		err = Call(err, f.Function, f.File, f.Line)
	}

	return err
}

// TraceOf returns the trace frames recorded for err, innermost first.
func TraceOf(err error) []Frame {
	if t := tracedOf(err); t != nil {
		return t.PHPTrace()
	}

	return nil
}

// tracedOf returns the error holding err's trace: err itself, or the
// error SiteOf takes the site from (a Go wrapper keeping the message).
func tracedOf(err error) Traced {
	if t, ok := err.(Traced); ok { // the object itself first
		if s, ok := err.(Sited); !ok || s.ThrowSite().Known() {
			return t
		}
	}
	msg := err.Error()
	var found Traced
	walk(err, func(e error) bool {
		t, ok := e.(Traced) // walk visits every error of the tree itself
		if !ok || e.Error() != msg {
			return false
		}
		if s, ok := e.(Sited); ok && !s.ThrowSite().Known() {
			return false
		}
		found = t

		return true
	})
	if found == nil {
		if t, ok := err.(Traced); ok {
			return t
		}
	}

	return found
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

// Is reports whether err's throw site (SiteOf) is file:line (file compared
// by basename).
func Is(err error, file string, line int) bool {
	site, ok := SiteOf(err)

	return ok && basename(site.File) == basename(file) && site.Line == line
}

func basename(file string) string { return file[strings.LastIndexByte(file, '/')+1:] }

// Path returns file (as At and Call take it) relative to Composer's root:
// "src/Composer/Factory.php" for "Factory.php". A file holding a slash is
// already one; a basename Composer's tree does not have is returned as is.
func Path(file string) string {
	if strings.Contains(file, "/") {
		return file
	}
	if p, ok := paths[file]; ok {
		return p
	}

	return file
}

// Basenames returns the basenames Path resolves, sorted.
func Basenames() []string {
	names := make([]string, 0, len(paths))
	for name := range paths {
		names = append(names, name)
	}
	slices.Sort(names)

	return names
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
// names a composer.phar's "phar:///usr/local/bin/composer/src/...".
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

// AbsPath is file's path as PHP's __FILE__ shows it: under Root. A path
// that is already absolute (a PHP file of the plugin runtime) is returned
// as is.
func AbsPath(file string) string {
	if file == "" || IsAbs(file) {
		return file
	}

	return Root() + "/" + Path(file)
}

// IsAbs reports whether file is an absolute path or a stream URL
// ("phar://...") rather than a file within Composer's tree.
func IsAbs(file string) bool {
	return strings.HasPrefix(file, "/") || strings.Contains(file, "://") ||
		(len(file) > 2 && file[1] == ':' && (file[2] == '\\' || file[2] == '/'))
}
