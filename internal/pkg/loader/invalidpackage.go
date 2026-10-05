// Ports src/Composer/Package/Loader/InvalidPackageException.php.

package loader

import (
	"strings"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
)

// InvalidPackageError ports Composer\Package\Loader\InvalidPackageException.
type InvalidPackageError struct {
	errors   []string
	warnings []string
	data     *php.Array
	phperr.Site
}

// NewInvalidPackageError ports InvalidPackageException::__construct.
func NewInvalidPackageError(errors, warnings []string, data *php.Array) *InvalidPackageError {
	return &InvalidPackageError{errors: errors, warnings: warnings, data: data}
}

func (e *InvalidPackageError) Error() string {
	all := make([]string, 0, len(e.errors)+len(e.warnings))
	all = append(all, e.errors...)
	all = append(all, e.warnings...)

	return "Invalid package information: \n" + strings.Join(all, "\n")
}

// PHPClass implements util.PHPClasser.
func (*InvalidPackageError) PHPClass() (string, int) {
	return `Composer\Package\Loader\InvalidPackageException`, 0
}

// Data ports InvalidPackageException::getData.
func (e *InvalidPackageError) Data() *php.Array { return e.data }

// Errors ports InvalidPackageException::getErrors.
func (e *InvalidPackageError) Errors() []string { return e.errors }

// Warnings ports InvalidPackageException::getWarnings.
func (e *InvalidPackageError) Warnings() []string { return e.warnings }
