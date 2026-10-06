package repository

import (
	"testing"

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
		if got, _ := util.PHPClassOf(err); got != want {
			t.Errorf("PHPClassOf(%T) = %s, want %s", err, got, want)
		}
	}
}
