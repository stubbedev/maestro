// Ports src/Composer/Repository/WritableArrayRepository.php,
// InstalledArrayRepository.php, LockArrayRepository.php,
// RootPackageRepository.php and CanonicalPackagesTrait.php.

package repository

import "github.com/stubbedev/maestro/internal/pkg"

// canonicalPackages ports CanonicalPackagesTrait::getCanonicalPackages:
// at most one package of each name, preferring non-aliased ones, with
// aliases resolved.
func canonicalPackages(packages []pkg.PackageInterface) []pkg.PackageInterface {
	// get at most one package of each name, preferring non-aliased ones
	byName := make(map[string]int, len(packages))
	var canonical []pkg.PackageInterface
	for _, p := range packages {
		i, ok := byName[p.Name()]
		if !ok {
			byName[p.Name()] = len(canonical)
			canonical = append(canonical, p)
		} else if _, isAlias := canonical[i].(pkg.Alias); isAlias {
			canonical[i] = p
		}
	}

	// unfold aliased packages
	for i, p := range canonical {
		for {
			alias, ok := p.(pkg.Alias)
			if !ok {
				break
			}
			p = alias.AliasOf()
		}
		canonical[i] = p
	}

	return canonical
}

// WritableArrayRepository ports Composer\Repository\WritableArrayRepository.
type WritableArrayRepository struct {
	ArrayRepository
	devPackageNames []string
	devMode         bool
	devModeKnown    bool
}

var _ WritableRepository = (*WritableArrayRepository)(nil)

// PHPClass returns the PHP class name.
func (r *WritableArrayRepository) PHPClass() string {
	return `Composer\Repository\WritableArrayRepository`
}

// DevMode ports WritableArrayRepository::getDevMode; ok false is null.
func (r *WritableArrayRepository) DevMode() (devMode, ok bool) {
	return r.devMode, r.devModeKnown
}

// SetDevPackageNames ports setDevPackageNames.
func (r *WritableArrayRepository) SetDevPackageNames(names []string) { r.devPackageNames = names }

// DevPackageNames ports getDevPackageNames.
func (r *WritableArrayRepository) DevPackageNames() []string { return r.devPackageNames }

// Write ports WritableArrayRepository::write.
func (r *WritableArrayRepository) Write(devMode bool, _ InstallationManager) error {
	r.devMode, r.devModeKnown = devMode, true

	return nil
}

// Reload ports WritableArrayRepository::reload.
func (r *WritableArrayRepository) Reload() error {
	r.devMode, r.devModeKnown = false, false

	return nil
}

// CanonicalPackages ports getCanonicalPackages.
func (r *WritableArrayRepository) CanonicalPackages() ([]pkg.PackageInterface, error) {
	packages, err := r.Packages()
	if err != nil {
		return nil, err
	}

	return canonicalPackages(packages), nil
}

// InstalledArrayRepository ports Composer\Repository\InstalledArrayRepository,
// an in-memory installed repository (mostly for tests).
type InstalledArrayRepository struct {
	WritableArrayRepository
}

var _ InstalledRepositoryInterface = (*InstalledArrayRepository)(nil)

// NewInstalledArrayRepository ports new InstalledArrayRepository($packages).
func NewInstalledArrayRepository(packages []pkg.PackageInterface) (*InstalledArrayRepository, error) {
	r := &InstalledArrayRepository{}
	r.bind(r, &r.ArrayRepository)
	if err := r.addPackages(packages); err != nil {
		return nil, err
	}

	return r, nil
}

// PHPClass returns the PHP class name.
func (r *InstalledArrayRepository) PHPClass() string {
	return `Composer\Repository\InstalledArrayRepository`
}

// RepoName ports InstalledArrayRepository::getRepoName.
func (r *InstalledArrayRepository) RepoName() string {
	return "installed " + r.WritableArrayRepository.RepoName()
}

// IsFresh ports InstalledArrayRepository::isFresh: whether it holds no
// packages.
func (r *InstalledArrayRepository) IsFresh() (bool, error) {
	// this is not a completely correct implementation but there is no way to
	// distinguish an empty repo and a newly created one given this is all in-memory
	n, err := r.Count()

	return n == 0, err
}

// LockArrayRepository ports Composer\Repository\LockArrayRepository: the
// packages of the lock file.
type LockArrayRepository struct {
	ArrayRepository
}

// NewLockArrayRepository ports new LockArrayRepository($packages).
func NewLockArrayRepository(packages []pkg.PackageInterface) (*LockArrayRepository, error) {
	r := &LockArrayRepository{}
	r.bind(r, &r.ArrayRepository)
	if err := r.addPackages(packages); err != nil {
		return nil, err
	}

	return r, nil
}

// PHPClass returns the PHP class name.
func (r *LockArrayRepository) PHPClass() string { return `Composer\Repository\LockArrayRepository` }

// RepoName ports LockArrayRepository::getRepoName.
func (r *LockArrayRepository) RepoName() string { return "lock repo" }

// CanonicalPackages ports getCanonicalPackages.
func (r *LockArrayRepository) CanonicalPackages() ([]pkg.PackageInterface, error) {
	packages, err := r.Packages()
	if err != nil {
		return nil, err
	}

	return canonicalPackages(packages), nil
}

// RootPackageRepository ports Composer\Repository\RootPackageRepository,
// which serves the root package in an InstalledRepository.
type RootPackageRepository struct {
	ArrayRepository
}

// NewRootPackageRepository ports new RootPackageRepository($package).
func NewRootPackageRepository(root pkg.RootPackageInterface) (*RootPackageRepository, error) {
	r := &RootPackageRepository{}
	r.bind(r, &r.ArrayRepository)
	if err := r.addPackages([]pkg.PackageInterface{root}); err != nil {
		return nil, err
	}

	return r, nil
}

// PHPClass returns the PHP class name.
func (r *RootPackageRepository) PHPClass() string { return `Composer\Repository\RootPackageRepository` }

// RepoName ports RootPackageRepository::getRepoName.
func (r *RootPackageRepository) RepoName() string { return "root package repo" }
