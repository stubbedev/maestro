// `extends ArrayRepository` for repository types outside this package.

package repository

import "github.com/stubbedev/maestro/internal/pkg"

// Extend makes r the ArrayRepository of outer, a type outside this package
// that embeds it (PHP's `class X extends ArrayRepository`): packages get
// outer as their repository, and initialize overrides
// ArrayRepository::initialize. initialize must call InitializeBase first,
// as PHP overrides call parent::initialize(); the packages it adds with
// AddPackage are kept even when it then fails, as in PHP. Call Extend
// before anything else.
func (r *ArrayRepository) Extend(outer RepositoryInterface, initialize func() error) {
	r.bind(outer, extendedHooks{r: r, override: initialize})
}

// InitializeBase is parent::initialize() for the initialize of Extend.
func (r *ArrayRepository) InitializeBase() { r.baseInitialize() }

// extendedHooks dispatches initialize() to an Extend override.
type extendedHooks struct {
	r        *ArrayRepository
	override func() error
}

func (h extendedHooks) initialize() error { return h.override() }

func (h extendedHooks) addPackage(p pkg.PackageInterface) error { return h.r.addPackageBase(p) }
