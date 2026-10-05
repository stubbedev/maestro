// Ports src/Composer/DependencyResolver/Request.php.

package resolver

import (
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/semver"
	"github.com/stubbedev/maestro/internal/util"
)

// The partial update modes of Request::setUpdateAllowList.
const (
	// UpdateOnlyListed updates the listed packages only, all dependencies
	// remain at their locked versions (UPDATE_ONLY_LISTED).
	UpdateOnlyListed = 0
	// UpdateListedWithTransitiveDepsNoRootRequire also updates the
	// dependencies of the listed packages, except those directly required
	// by the root composer.json (UPDATE_LISTED_WITH_TRANSITIVE_DEPS_NO_ROOT_REQUIRE).
	UpdateListedWithTransitiveDepsNoRootRequire = 1
	// UpdateListedWithTransitiveDeps also updates all dependencies of the
	// listed packages (UPDATE_LISTED_WITH_TRANSITIVE_DEPS).
	UpdateListedWithTransitiveDeps = 2
	// updateAllowFalse is PHP's false for $updateAllowTransitiveDependencies.
	updateAllowFalse = -1
)

// pkgSet is a PHP array of packages keyed by spl_object_id: insertion
// ordered, at most one entry per package object.
type pkgSet struct {
	list  []pkg.PackageInterface // nil entries are unset
	index map[pkg.PackageInterface]int
	live  int
}

func (s *pkgSet) add(p pkg.PackageInterface) {
	if _, ok := s.index[p]; ok {
		return
	}
	if s.index == nil {
		s.index = make(map[pkg.PackageInterface]int)
	}
	s.index[p] = len(s.list)
	s.list = append(s.list, p)
	s.live++
}

func (s *pkgSet) remove(p pkg.PackageInterface) {
	i, ok := s.index[p]
	if !ok {
		return
	}
	delete(s.index, p)
	s.list[i] = nil
	s.live--
}

func (s *pkgSet) has(p pkg.PackageInterface) bool {
	_, ok := s.index[p]

	return ok
}

func (s *pkgSet) len() int { return s.live }

// appendTo appends the packages, in order, to dst.
func (s *pkgSet) appendTo(dst []pkg.PackageInterface) []pkg.PackageInterface {
	for _, p := range s.list {
		if p != nil {
			dst = append(dst, p)
		}
	}

	return dst
}

// Request ports Composer\DependencyResolver\Request: what the solver must
// install, keep or may update.
type Request struct {
	lockedRepository                  *repository.LockArrayRepository
	requires                          *repository.ConstraintMap
	fixedPackages                     pkgSet
	lockedPackages                    pkgSet
	fixedLockedPackages               pkgSet
	updateAllowList                   []string
	updateAllowTransitiveDependencies int
	restrictedPackages                []string
}

// NewRequest is new Request($lockedRepository); lockedRepository may be
// nil.
func NewRequest(lockedRepository *repository.LockArrayRepository) *Request {
	return &Request{
		lockedRepository:                  lockedRepository,
		requires:                          &repository.ConstraintMap{},
		updateAllowTransitiveDependencies: updateAllowFalse,
	}
}

// RequireName ports requireName: constraint nil is any version. Requiring
// a name twice is a *util.LogicError.
func (r *Request) RequireName(packageName string, constraint semver.ConstraintInterface) error {
	packageName = php.Strtolower(packageName)

	if constraint == nil {
		constraint = semver.NewMatchAllConstraint()
	}
	if existing, ok := r.requires.Get(packageName); ok {
		return &util.LogicError{Message: "Overwriting requires seems like a bug (" + packageName + " " + existing.PrettyString() + " => " + constraint.PrettyString() + ", check why it is happening, might be a root alias"}
	}
	r.requires.Set(packageName, constraint)

	return nil
}

// FixPackage ports fixPackage: the package is present and must remain
// installed (platform packages).
func (r *Request) FixPackage(p pkg.PackageInterface) { r.fixedPackages.add(p) }

// LockPackage ports lockPackage: the package is locked to its version but
// removable, and can be unlocked by a partial update.
func (r *Request) LockPackage(p pkg.PackageInterface) { r.lockedPackages.add(p) }

// FixLockedPackage ports fixLockedPackage: a locked package that cannot be
// removed (composer install).
func (r *Request) FixLockedPackage(p pkg.PackageInterface) {
	r.fixedPackages.add(p)
	r.fixedLockedPackages.add(p)
}

// UnlockPackage ports unlockPackage.
func (r *Request) UnlockPackage(p pkg.PackageInterface) { r.lockedPackages.remove(p) }

// SetUpdateAllowList ports setUpdateAllowList; transitive is one of the
// Update* constants.
func (r *Request) SetUpdateAllowList(updateAllowList []string, transitive int) {
	r.updateAllowList = updateAllowList
	r.updateAllowTransitiveDependencies = transitive
}

// UpdateAllowList ports getUpdateAllowList.
func (r *Request) UpdateAllowList() []string { return r.updateAllowList }

// UpdateAllowTransitiveDependencies ports getUpdateAllowTransitiveDependencies.
// It is true until SetUpdateAllowList sets another mode than
// UpdateOnlyListed (PHP compares false !== 0).
func (r *Request) UpdateAllowTransitiveDependencies() bool {
	return r.updateAllowTransitiveDependencies != UpdateOnlyListed
}

// UpdateAllowTransitiveRootDependencies ports
// getUpdateAllowTransitiveRootDependencies.
func (r *Request) UpdateAllowTransitiveRootDependencies() bool {
	return r.updateAllowTransitiveDependencies == UpdateListedWithTransitiveDeps
}

// Requires ports getRequires: package name => constraint. The map must not
// be modified.
func (r *Request) Requires() *repository.ConstraintMap { return r.requires }

// FixedPackages ports getFixedPackages.
func (r *Request) FixedPackages() []pkg.PackageInterface { return r.fixedPackages.appendTo(nil) }

// IsFixedPackage ports isFixedPackage.
func (r *Request) IsFixedPackage(p pkg.PackageInterface) bool { return r.fixedPackages.has(p) }

// LockedPackages ports getLockedPackages: the locked packages, then the
// fixed locked ones (array_merge keeps a package in both twice).
func (r *Request) LockedPackages() []pkg.PackageInterface {
	return r.fixedLockedPackages.appendTo(r.lockedPackages.appendTo(make([]pkg.PackageInterface, 0, r.lockedPackages.len()+r.fixedLockedPackages.len())))
}

// IsLockedPackage ports isLockedPackage.
func (r *Request) IsLockedPackage(p pkg.PackageInterface) bool {
	return r.lockedPackages.has(p) || r.fixedLockedPackages.has(p)
}

// FixedOrLockedPackages ports getFixedOrLockedPackages: the fixed
// packages, then the locked ones.
func (r *Request) FixedOrLockedPackages() []pkg.PackageInterface {
	return r.lockedPackages.appendTo(r.fixedPackages.appendTo(make([]pkg.PackageInterface, 0, r.fixedPackages.len()+r.lockedPackages.len())))
}

// PresentMap ports getPresentMap(false): the locked repository's packages
// and the fixed packages, without duplicates.
func (r *Request) PresentMap() ([]pkg.PackageInterface, error) {
	var set pkgSet
	if r.lockedRepository != nil {
		packages, err := r.lockedRepository.Packages()
		if err != nil {
			return nil, err
		}
		for _, p := range packages {
			set.add(p)
		}
	}
	for _, p := range r.fixedPackages.list {
		if p != nil {
			set.add(p)
		}
	}

	return set.appendTo(nil), nil
}

// PresentIDMap ports getPresentMap(true): the ids (package ids, -1 or
// stale for packages not in the pool) of the present packages.
func (r *Request) PresentIDMap() (map[int]bool, error) {
	present, err := r.PresentMap()
	if err != nil {
		return nil, err
	}
	ids := make(map[int]bool, len(present))
	for _, p := range present {
		ids[p.ID()] = true
	}

	return ids, nil
}

// FixedPackagesMap ports getFixedPackagesMap: the fixed packages by id.
func (r *Request) FixedPackagesMap() map[int]pkg.PackageInterface {
	m := make(map[int]pkg.PackageInterface, r.fixedPackages.len())
	for _, p := range r.fixedPackages.list {
		if p != nil {
			m[p.ID()] = p
		}
	}

	return m
}

// LockedRepository ports getLockedRepository; nil is null.
func (r *Request) LockedRepository() *repository.LockArrayRepository { return r.lockedRepository }

// RestrictPackages ports restrictPackages: the pool builder loads only
// these names.
func (r *Request) RestrictPackages(names []string) { r.restrictedPackages = names }

// RestrictedPackages ports getRestrictedPackages; nil is null.
func (r *Request) RestrictedPackages() []string { return r.restrictedPackages }
