package repository

import (
	"errors"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/pkg/loader"
	"github.com/stubbedev/maestro/internal/util"
)

// get_class() of the repository exceptions (src/Composer/Repository/
// InvalidRepositoryException.php, RepositorySecurityException.php), which
// the console's [Class] box shows at -v.
func TestErrorClasses(t *testing.T) {
	for err, want := range map[error]string{
		&InvalidRepositoryError{Message: "x"}: `Composer\Repository\InvalidRepositoryException`,
		&SecurityError{Message: "x"}:          `Composer\Repository\RepositorySecurityException`,
	} {
		if got, _ := phperr.ClassOf(err); got != want {
			t.Errorf("phperr.ClassOf(%T) = %s, want %s", err, got, want)
		}
	}
}

// An exception created with a previous one keeps its own class, whatever
// the previous one is (PathRepository.php:229, ArtifactRepository.php's
// catch): get_class() and catch never look at getPrevious().
func TestWrappedErrorKeepsItsClass(t *testing.T) {
	for _, prev := range []error{
		loader.NewInvalidPackageError([]string{"bad"}, nil, php.NewArray()),
		&util.UnexpectedValueError{Message: "bad"},
		util.NewTransportError("bad", 404),
	} {
		for err, want := range map[error]string{
			&wrappedError{err: &util.RuntimeError{Message: "x"}, previous: prev}:         "RuntimeException",
			&wrappedError{err: &util.UnexpectedValueError{Message: "x"}, previous: prev}: "UnexpectedValueException",
		} {
			if got, _ := phperr.ClassOf(err); got != want {
				t.Errorf("phperr.ClassOf(%s with previous %T) = %s", want, prev, got)
			}
			if !errors.Is(phperr.PreviousOf(err), prev) {
				t.Errorf("PreviousOf(%s) = %v, want %v", want, phperr.PreviousOf(err), prev)
			}
		}
	}
}
