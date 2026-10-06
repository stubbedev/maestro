// Ports src/Composer/Package/Version/VersionSelector.php.

package version

import (
	"strings"

	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/dumper"
	"github.com/stubbedev/maestro/internal/pkg/loader"
	"github.com/stubbedev/maestro/internal/semver"
	"github.com/stubbedev/maestro/internal/util"
)

// RepositorySet is the part of Composer\Repository\RepositorySet the
// selector uses.
type RepositorySet interface {
	// FindPackages ports RepositorySet::findPackages; constraint may be
	// nil. *repository.RepositorySet implements it.
	FindPackages(name string, constraint semver.ConstraintInterface, flags int) ([]pkg.PackageInterface, error)
}

// PlatformRequirementFilter is Composer's PlatformRequirementFilterInterface.
type PlatformRequirementFilter interface {
	IsIgnored(req string) bool
}

// IgnoreAllPlatformRequirementFilter marks Composer's
// IgnoreAllPlatformRequirementFilter (`instanceof` checks look for it).
type IgnoreAllPlatformRequirementFilter interface {
	PlatformRequirementFilter
	IgnoresAllPlatformRequirements()
}

// IgnoreListPlatformRequirementFilter is the part of Composer's
// IgnoreListPlatformRequirementFilter the selector uses.
type IgnoreListPlatformRequirementFilter interface {
	PlatformRequirementFilter
	IsUpperBoundIgnored(req string) bool
	FilterConstraint(req string, constraint semver.ConstraintInterface) semver.ConstraintInterface
}

type ignoreNothing struct{}

func (ignoreNothing) IsIgnored(string) bool { return false }

// VersionSelector ports Composer\Package\Version\VersionSelector: it picks
// the best version of a package to require.
type VersionSelector struct {
	repositorySet       RepositorySet
	platformConstraints map[string][]*semver.Constraint
	parser              *pkg.VersionParser

	// PHPVersion is "major.minor.release" of the PHP running the project
	// (PHP_MAJOR_VERSION.PHP_MINOR_VERSION.PHP_RELEASE_VERSION in Composer):
	// extensions at that version are recommended as "*". Empty disables
	// that.
	PHPVersion string
}

// NewVersionSelector ports VersionSelector::__construct; platformPackages
// are the packages of the platform repository (nil when there is none).
func NewVersionSelector(repositorySet RepositorySet, platformPackages []pkg.PackageInterface) *VersionSelector {
	s := &VersionSelector{repositorySet: repositorySet, platformConstraints: map[string][]*semver.Constraint{}}

	for _, p := range platformPackages {
		s.platformConstraints[p.Name()] = append(s.platformConstraints[p.Name()], semver.NewConstraintOp(semver.OpEQ, p.Version()))
	}

	return s
}

// FindBestCandidateOptions are findBestCandidate's optional arguments.
type FindBestCandidateOptions struct {
	// TargetPackageVersion is a constraint the candidates must match;
	// falsy is none.
	TargetPackageVersion string
	// PreferredStability defaults to "stable".
	PreferredStability string
	// PlatformRequirementFilter defaults to ignoring nothing.
	PlatformRequirementFilter PlatformRequirementFilter
	RepoSetFlags              int
	IO                        io.IO
	// ShowWarnings decides per candidate whether to warn about it; nil
	// shows every warning (PHP's true).
	ShowWarnings func(p pkg.PackageInterface) bool
}

// FindBestCandidate ports VersionSelector::findBestCandidate: the best
// version of packageName, or nil (PHP's false) when there is none.
func (s *VersionSelector) FindBestCandidate(packageName string, opts FindBestCandidateOptions) (pkg.PackageInterface, error) {
	preferredStability := opts.PreferredStability
	if preferredStability == "" {
		preferredStability = "stable"
	}

	minPriority, ok := pkg.StabilityValue(preferredStability)
	if !ok {
		// If you get this, maybe you are still relying on the Composer 1.x signature where the 3rd arg was the php version
		return nil, &util.UnexpectedValueError{Message: "Expected a valid stability name as 3rd argument, got " + preferredStability}
	}

	filter := opts.PlatformRequirementFilter
	if filter == nil {
		filter = ignoreNothing{}
	}

	var constraint semver.ConstraintInterface

	if php.ToBool(opts.TargetPackageVersion) {
		var err error
		if constraint, err = s.getParser().ParseConstraints(opts.TargetPackageVersion); err != nil {
			return nil, err
		}
	}

	found, err := s.repositorySet.FindPackages(php.Strtolower(packageName), constraint, opts.RepoSetFlags)
	if err != nil {
		return nil, err
	}
	candidates := append([]pkg.PackageInterface(nil), found...)

	php.SortSlice(candidates, func(a, b pkg.PackageInterface) int {
		aPriority := a.StabilityPriority()
		bPriority := b.StabilityPriority()

		// A is less stable than our preferred stability,
		// and B is more stable than A, select B
		if minPriority < aPriority && bPriority < aPriority {
			return 1
		}

		// A is less stable than our preferred stability,
		// and B is less stable than A, select A
		if minPriority < aPriority && aPriority < bPriority {
			return -1
		}

		// A is more stable than our preferred stability,
		// and B is less stable than preferred stability, select A
		if minPriority >= aPriority && minPriority < bPriority {
			return -1
		}

		// select highest version of the two
		return semver.VersionCompare(b.Version(), a.Version())
	})

	var selected pkg.PackageInterface

	if _, ignoreAll := filter.(IgnoreAllPlatformRequirementFilter); len(s.platformConstraints) > 0 && !ignoreAll {
		var err error
		if selected, err = s.firstPlatformCompatible(candidates, filter, opts); err != nil {
			return nil, err
		}
	} else if len(candidates) > 0 {
		selected = candidates[0]
	}

	if selected == nil {
		return nil, nil
	}

	// if we end up with 9999999-dev as selected package, make sure we use the original version instead of the alias
	if a, ok := selected.(pkg.Alias); ok && a.Version() == pkg.DefaultBranchAlias {
		selected = a.AliasOf()
	}

	return selected, nil
}

// firstPlatformCompatible returns the first candidate whose platform
// requirements the platform satisfies, warning about the others.
func (s *VersionSelector) firstPlatformCompatible(candidates []pkg.PackageInterface, filter PlatformRequirementFilter, opts FindBestCandidateOptions) (pkg.PackageInterface, error) {
	alreadyWarnedNames := map[string]bool{}
	alreadySeenNames := map[string]bool{}
	ignoreList, _ := filter.(IgnoreListPlatformRequirementFilter)

	for _, p := range candidates {
		skip := false

	requires:
		for name, link := range p.Requires().All() {
			if !pkg.IsPlatformPackage(name) || filter.IsIgnored(name) {
				continue
			}

			reason := "is missing from your platform"

			if provided, ok := s.platformConstraints[name]; ok {
				for _, providedConstraint := range provided {
					if link.Constraint().Matches(providedConstraint) {
						// constraint satisfied, go to next require
						continue requires
					}

					if ignoreList != nil && ignoreList.IsUpperBoundIgnored(name) {
						if ignoreList.FilterConstraint(name, link.Constraint()).Matches(providedConstraint) {
							// constraint satisfied with the upper bound ignored, go to next require
							continue requires
						}
					}
				}

				// constraint not satisfied
				reason = "is not satisfied by your platform"
			}

			isLatestVersion := !alreadySeenNames[p.Name()]
			alreadySeenNames[p.Name()] = true

			if opts.IO != nil && (opts.ShowWarnings == nil || opts.ShowWarnings(p)) {
				warnKey := p.Name() + "/" + link.Target()
				isFirstWarning := !alreadyWarnedNames[warnKey]
				alreadyWarnedNames[warnKey] = true

				latest := ""
				if isLatestVersion {
					latest = "'s latest version"
				}

				prettyConstraint, err := link.PrettyConstraint()
				if err != nil {
					return nil, err
				}

				verbosity := io.Verbose
				if isFirstWarning {
					verbosity = io.Normal
				}

				opts.IO.WriteError("<warning>Cannot use "+p.PrettyName()+latest+" "+p.PrettyVersion()+" as it "+link.Description()+" "+
					link.Target()+" "+prettyConstraint+" which "+reason+".</>", true, verbosity)
			}

			// skip candidate
			skip = true
		}

		if !skip {
			return p, nil
		}
	}

	return nil, nil
}

var (
	branchAliasPatch = php.MustCompile(`{^(\d+\.\d+\.\d+)(\.9999999)-dev$}`)
	numericPart      = php.MustCompile(`{^\d+\D?}`)
)

// FindRecommendedRequireVersion ports
// VersionSelector::findRecommendedRequireVersion: the constraint to
// require the package with ("^2.1" for 2.1.3).
func (s *VersionSelector) FindRecommendedRequireVersion(p pkg.PackageInterface) (string, error) {
	// Extensions which are versioned in sync with PHP should rather be required as "*" to simplify
	// the requires and have only one required version to change when bumping the php requirement
	if strings.HasPrefix(p.Name(), "ext-") && s.PHPVersion != "" {
		parts := strings.Split(p.Version(), ".")
		if len(parts) > 3 {
			parts = parts[:3]
		}

		if strings.Join(parts, ".") == s.PHPVersion {
			return "*", nil
		}
	}

	version := p.Version()
	if !p.IsDev() {
		return transformVersion(version, p.PrettyVersion(), p.Stability()), nil
	}

	dumped, err := dumper.ArrayDumper{}.Dump(p)
	if err != nil {
		return "", err
	}

	extra, ok, err := loader.NewArrayLoader(s.getParser(), false).GetBranchAlias(dumped)
	if err != nil {
		return "", err
	}

	if ok && php.ToBool(extra) && extra != pkg.DefaultBranchAlias {
		replaced, count, err := branchAliasPatch.Replace(extra, "$1.0", -1)
		if err != nil {
			return "", err
		}

		if count > 0 {
			replaced = strings.ReplaceAll(replaced, ".9999999", ".0")

			return transformVersion(replaced, replaced, "dev"), nil
		}
	}

	return p.PrettyVersion(), nil
}

// transformVersion ports VersionSelector::transformVersion.
func transformVersion(version, prettyVersion, stability string) string {
	// attempt to transform 2.1.1 to 2.1
	// this allows you to upgrade through minor versions
	parts := strings.Split(version, ".")

	// check to see if we have a semver-looking version
	if len(parts) != 4 || !mustMatch(numericPart, parts[3]) {
		return prettyVersion
	}

	// remove the last parts (i.e. the patch version number and any extra)
	if parts[0] == "0" {
		parts = parts[:3]
	} else {
		parts = parts[:2]
	}

	version = strings.Join(parts, ".")

	// append stability flag if not default
	if stability != "stable" {
		version += "@" + stability
	}

	// 2.1 -> ^2.1
	return "^" + version
}

func (s *VersionSelector) getParser() *pkg.VersionParser {
	if s.parser == nil {
		s.parser = pkg.NewVersionParser()
	}

	return s.parser
}

// mustMatch is Preg::isMatch for patterns that cannot fail at run time.
func mustMatch(re *php.Regexp, subject string) bool {
	ok, err := re.IsMatch(subject)
	if err != nil {
		panic(err)
	}

	return ok
}
