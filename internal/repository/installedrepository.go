// Ports src/Composer/Repository/InstalledRepository.php.

package repository

import (
	"slices"

	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/semver"
	"github.com/stubbedev/maestro/internal/util"
)

// InstalledRepository ports Composer\Repository\InstalledRepository: the
// composite of the installed repository types (LockArrayRepository,
// InstalledRepositoryInterface, RootPackageRepository and
// PlatformRepository), with lookups of providers, replacers and
// dependents.
type InstalledRepository struct {
	CompositeRepository
}

// NewInstalledRepository ports new InstalledRepository($repositories).
func NewInstalledRepository(repositories []RepositoryInterface) (*InstalledRepository, error) {
	r := &InstalledRepository{}
	r.self = r
	if err := r.addRepositories(repositories); err != nil {
		return nil, err
	}

	return r, nil
}

// Class returns the PHP class name.
func (r *InstalledRepository) Class() string { return `Composer\Repository\InstalledRepository` }

// RepoName ports InstalledRepository::getRepoName.
func (r *InstalledRepository) RepoName() string {
	return "installed repo (" + r.repoNames() + ")"
}

// AddRepository ports InstalledRepository::addRepository: only installed
// repository types are accepted.
func (r *InstalledRepository) AddRepository(repository RepositoryInterface) error {
	switch repository.(type) {
	case *LockArrayRepository, InstalledRepositoryInterface, *RootPackageRepository, *PlatformRepository:
		return r.CompositeRepository.AddRepository(repository)
	}

	return &util.LogicError{Site: phperr.At("InstalledRepository.php", 275), Message: "An InstalledRepository can not contain a repository of type " + repository.Class() + " (" + repository.RepoName() + ")"}
}

// FindPackagesWithReplacersAndProviders ports
// InstalledRepository::findPackagesWithReplacersAndProviders: the packages
// named name, and those providing or replacing it, whose version (or
// provided/replaced constraint) matches constraint (nil: any).
func (r *InstalledRepository) FindPackagesWithReplacersAndProviders(name string, constraint semver.ConstraintInterface) ([]pkg.PackageInterface, error) {
	name = strtolower(name)

	var matches []pkg.PackageInterface
	for _, repo := range r.repositories {
		packages, err := repo.Packages()
		if err != nil {
			return nil, err
		}
	candidates:
		for _, candidate := range packages {
			if name == candidate.Name() {
				if versionMatches(constraint, candidate.Version()) {
					matches = append(matches, candidate)
				}

				continue
			}

			for link := range MergeLinks(candidate.Provides(), candidate.Replaces()).Values() {
				if name == link.Target() && (constraint == nil || constraint.Matches(link.Constraint())) {
					matches = append(matches, candidate)

					continue candidates
				}
			}
		}
	}

	return matches, nil
}

// Dependent is an entry of InstalledRepository::getDependents' result: a
// package and the link that makes it depend on (or conflict with) a
// needle.
type Dependent struct {
	Package pkg.PackageInterface
	Link    *pkg.Link
	// Dependents are the entries of Package's own dependents, up to the
	// root package, when the lookup recursed; empty otherwise.
	Dependents []Dependent
	// Cut is PHP's false in place of the dependents: a circular
	// dependency cut short, or a conflict or requirement entry.
	Cut bool
}

// GetDependents ports InstalledRepository::getDependents: the links
// causing the needle packages to be installed (or, with invert, not to
// be), each with its dependents up to the root package when recurse is
// set. constraint (nil: any) filters the links.
func (r *InstalledRepository) GetDependents(needle []string, constraint semver.ConstraintInterface, invert, recurse bool) ([]Dependent, error) {
	return r.getDependents(needle, constraint, invert, recurse, nil)
}

func (r *InstalledRepository) getDependents(needle []string, constraint semver.ConstraintInterface, invert, recurse bool, packagesFound []string) ([]Dependent, error) {
	needles := make([]string, len(needle))
	for i, n := range needle {
		needles[i] = strtolower(n)
	}
	var results []Dependent

	// initialize the array with the needles before any recursion occurs
	if packagesFound == nil {
		packagesFound = slices.Clone(needles)
	}

	packages, err := r.Packages()
	if err != nil {
		return nil, err
	}

	// locate root package for use below
	var rootPackage pkg.RootPackageInterface
	for _, p := range packages {
		if root, ok := p.(pkg.RootPackageInterface); ok {
			rootPackage = root

			break
		}
	}

	recurseFrom := func(name string, packagesInTree []string) ([]Dependent, error) {
		if !recurse {
			return []Dependent{}, nil
		}

		return r.getDependents([]string{name}, nil, false, true, slices.Clip(packagesInTree))
	}

	// Loop over all currently installed packages.
	for _, p := range packages {
		links := p.Requires()

		// each loop needs its own "tree" as we want to show the complete dependent set of every needle
		// without warning all the time about finding circular deps
		packagesInTree := slices.Clone(packagesFound)

		// Replacements are considered valid reasons for a package to be installed during forward resolution
		if !invert {
			links = linksUnion(links, p.Replaces())

			// On forward search, check if any replaced package was required and add the replaced
			// packages to the list of needles. Contrary to the cross-reference link check below,
			// replaced packages are the target of links.
			for link := range p.Replaces().Values() {
				for _, n := range needles {
					if link.Source() != n || (constraint != nil && !link.Constraint().Matches(constraint)) {
						continue
					}
					// already displayed this node's dependencies, cutting short
					if inArrayLoose(link.Target(), packagesInTree) {
						results = append(results, Dependent{Package: p, Link: link, Cut: true})

						continue
					}
					packagesInTree = append(packagesInTree, link.Target())
					dependents, err := recurseFrom(link.Target(), packagesInTree)
					if err != nil {
						return nil, err
					}
					results = append(results, Dependent{Package: p, Link: link, Dependents: dependents})
					needles = append(needles, link.Target())
				}
			}
		}

		// Require-dev is only relevant for the root package
		if _, ok := p.(pkg.RootPackageInterface); ok {
			links = linksUnion(links, p.DevRequires())
		}

		// Cross-reference all discovered links to the needles
		for link := range links.Values() {
			for _, n := range needles {
				if link.Target() != n || (constraint != nil && link.Constraint().Matches(constraint) == invert) {
					continue
				}
				// already displayed this node's dependencies, cutting short
				if inArrayLoose(link.Source(), packagesInTree) {
					results = append(results, Dependent{Package: p, Link: link, Cut: true})

					continue
				}
				packagesInTree = append(packagesInTree, link.Source())
				dependents, err := recurseFrom(link.Source(), packagesInTree)
				if err != nil {
					return nil, err
				}
				results = append(results, Dependent{Package: p, Link: link, Dependents: dependents})
			}
		}

		// When inverting, we need to check for conflicts of the needles against installed packages
		if invert && slices.Contains(needles, p.Name()) {
			if results, err = r.appendConflicts(results, p, invert, nil); err != nil {
				return nil, err
			}
		}

		// List conflicts against X as they may explain why the current version was selected, or explain why it is rejected if the conflict matched when inverting
		if results, err = r.appendConflicts(results, p, invert, needles); err != nil {
			return nil, err
		}

		// When inverting, we need to check for conflicts of the needles' requirements against installed packages
		if invert && constraint != nil && slices.Contains(needles, p.Name()) && constraint.Matches(semver.NewConstraintOp(semver.OpEQ, p.Version())) {
			if results, err = r.appendRequirementConflicts(results, p, packages, rootPackage); err != nil {
				return nil, err
			}
		}
	}

	return results, nil
}

// appendConflicts adds the conflicts of p (with a target in needles, unless
// needles is nil) against installed packages that match (or, with
// invert, do not match).
func (r *InstalledRepository) appendConflicts(results []Dependent, p pkg.PackageInterface, invert bool, needles []string) ([]Dependent, error) {
	for link := range p.Conflicts().Values() {
		if needles != nil && !slices.Contains(needles, link.Target()) {
			continue
		}
		found, err := r.FindPackages(link.Target(), nil)
		if err != nil {
			return nil, err
		}
		for _, installed := range found {
			if link.Constraint().Matches(semver.NewConstraintOp(semver.OpEQ, installed.Version())) == invert {
				results = append(results, Dependent{Package: p, Link: link, Cut: true})
			}
		}
	}

	return results, nil
}

// appendRequirementConflicts adds the requirements of p that the
// installed packages do not satisfy, with the root requirement explaining
// why when there is one.
func (r *InstalledRepository) appendRequirementConflicts(results []Dependent, p pkg.PackageInterface, packages []pkg.PackageInterface, rootPackage pkg.RootPackageInterface) ([]Dependent, error) {
requires:
	for link := range p.Requires().Values() {
		if pkg.IsPlatformPackage(link.Target()) {
			found, err := r.FindPackage(link.Target(), link.Constraint())
			if err != nil {
				return nil, err
			}
			if found != nil {
				continue
			}

			platformPkg, err := r.FindPackage(link.Target(), semver.NewMatchAllConstraint())
			if err != nil {
				return nil, err
			}
			description := "but it is missing"
			if platformPkg != nil {
				description = "but " + platformPkg.PrettyVersion() + " is installed"
			}
			prettyConstraint, err := link.PrettyConstraint()
			if err != nil {
				return nil, err
			}
			results = append(results, Dependent{
				Package: p,
				Link:    pkg.NewLink(p.Name(), link.Target(), semver.NewMatchAllConstraint(), pkg.TypeRequire, pkg.Str(prettyConstraint+" "+description)),
				Cut:     true,
			})

			continue
		}

		for _, installed := range packages {
			if !inArrayLoose(link.Target(), installed.Names(true)) {
				continue
			}

			var version semver.ConstraintInterface = semver.NewConstraintOp(semver.OpEQ, installed.Version())
			if link.Target() != installed.Name() {
				for prov := range MergeLinks(installed.Replaces(), installed.Provides()).Values() {
					if link.Target() == prov.Target() {
						version = prov.Constraint()

						break
					}
				}
			}

			if !link.Constraint().Matches(version) {
				// if we have a root package (we should but can not guarantee..) we show
				// the root requires as well to perhaps allow to find an issue there
				if rootPackage == nil {
					// no root so let's just print whatever we found
					results = append(results, Dependent{Package: p, Link: link, Cut: true})

					continue requires
				}
				for rootReq := range MergeLinks(rootPackage.Requires(), rootPackage.DevRequires()).Values() {
					if inArrayLoose(rootReq.Target(), installed.Names(true)) && !rootReq.Constraint().Matches(link.Constraint()) {
						results = append(results,
							Dependent{Package: p, Link: link, Cut: true},
							Dependent{Package: rootPackage, Link: rootReq, Cut: true})

						continue requires
					}
				}

				results = append(results,
					Dependent{Package: p, Link: link, Cut: true},
					Dependent{
						Package: rootPackage,
						Link:    pkg.NewLink(rootPackage.Name(), link.Target(), semver.NewMatchAllConstraint(), pkg.TypeDoesNotRequire, pkg.Str("but "+installed.PrettyVersion()+" is installed")),
						Cut:     true,
					})
			}

			continue requires
		}
	}

	return results, nil
}
