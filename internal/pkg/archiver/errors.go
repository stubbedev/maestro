// The exceptions of PHP's phar extension that PharArchiver lets through.

package archiver

import (
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/util"
)

// PharError is \PharException, e.g. a name too long for the tar format.
type PharError struct {
	Message string
	phperr.Site
}

func (e *PharError) Error() string { return e.Message }

// BadMethodCallError is \BadMethodCallException, which PharData throws for
// entry names it does not accept.
type BadMethodCallError struct {
	Message string
	phperr.Site
}

func (e *BadMethodCallError) Error() string { return e.Message }

// atSite gives an exception the phar extension throws the throw site PHP
// reports for it: the line of the PharArchiver code calling into the
// extension. Errors that already have a site keep it.
func atSite(err error, site phperr.Site) error {
	switch e := err.(type) { //nolint:errorlint // the exception object itself
	case *util.UnexpectedValueError:
		if !e.Known() {
			e.Site = site
		}
	case *PharError:
		if !e.Known() {
			e.Site = site
		}
	case *BadMethodCallError:
		if !e.Known() {
			e.Site = site
		}
	}

	return err
}
