// Ports src/Composer/Command/PackageDiscoveryTrait.php.

package command

import (
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/filter"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/version"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/semver"
	"github.com/stubbedev/maestro/internal/util"
)

// PackageDiscovery is Composer\Command\PackageDiscoveryTrait, used by
// require and init: embed it next to *BaseCommand and set it with
// NewPackageDiscovery(c.BaseCommand).
type PackageDiscovery struct {
	cmd            *BaseCommand
	repos          *repository.CompositeRepository
	repositorySets map[string]*repository.RepositorySet
}

// NewPackageDiscovery returns the trait state of cmd.
func NewPackageDiscovery(cmd *BaseCommand) PackageDiscovery {
	return PackageDiscovery{cmd: cmd, repositorySets: map[string]*repository.RepositorySet{}}
}

// Repos ports getRepos: the platform repository and the default
// repositories of the global configuration.
func (d *PackageDiscovery) Repos() (*repository.CompositeRepository, error) {
	if d.repos == nil {
		app := d.cmd.application()
		rt := d.cmd.factory().Runtime
		if app != nil {
			rt = app.Runtime()
		}
		var platformRepo *repository.PlatformRepository
		var err error
		if rt != nil {
			opts, err := rt.PlatformOptions(util.NewProcessExecutor(d.cmd.IO()))
			if err != nil {
				return nil, err
			}
			platformRepo, err = repository.NewPlatformRepository(nil, nil, opts)
			if err != nil {
				return nil, err
			}
		} else if platformRepo, err = repository.NewPlatformRepository(nil, nil, repository.PlatformOptions{}); err != nil {
			return nil, err
		}
		defaults, err := defaultReposWithDefaultManager(d.cmd.IO(), d.cmd.factory())
		if err != nil {
			return nil, err
		}
		repos := append([]repository.RepositoryInterface{platformRepo}, repoMapValues(defaults)...)
		if d.repos, err = repository.NewCompositeRepository(repos); err != nil {
			return nil, err
		}
	}

	return d.repos, nil
}

// RepositorySet ports getRepositorySet; minimumStability "" is null (the
// input's or composer.json's).
func (d *PackageDiscovery) RepositorySet(in console.Input, minimumStability string) (*repository.RepositorySet, error) {
	key := minimumStability
	if key == "" {
		key = "default"
	}

	if _, ok := d.repositorySets[key]; !ok {
		stability := minimumStability
		if stability == "" {
			var err error
			if stability, err = d.minimumStability(in); err != nil {
				return nil, err
			}
		}
		set, err := repository.NewRepositorySet(stability, nil, nil, nil, &repository.ConstraintMap{}, nil)
		if err != nil {
			return nil, err
		}
		repos, err := d.Repos()
		if err != nil {
			return nil, err
		}
		if err := set.AddRepository(repos); err != nil {
			return nil, err
		}
		if d.repositorySets == nil {
			d.repositorySets = map[string]*repository.RepositorySet{}
		}
		d.repositorySets[key] = set
	}

	return d.repositorySets[key], nil
}

// minimumStability ports getMinimumStability.
func (d *PackageDiscovery) minimumStability(in console.Input) (string, error) {
	if in.HasOption("stability") {
		s := "stable"
		if v := in.Option("stability"); v != nil {
			s = php.ToString(v)
		}

		return semver.NormalizeStability(s)
	}

	file, err := composerFile()
	if err != nil {
		return "", err
	}
	if st, serr := os.Stat(file); serr == nil && st.Mode().IsRegular() && util.IsReadable(file) {
		data, _ := os.ReadFile(file)
		if decoded, derr := php.JSONDecode(string(data), true); derr == nil {
			if composer, ok := decoded.(*php.Array); ok {
				if v, ok := composer.Get("minimum-stability"); ok && v != nil {
					return semver.NormalizeStability(php.ToString(v))
				}
			}
		}
	}

	return "stable", nil
}

var (
	tooStrictConstraint = php.MustCompile(`{^\d+(\.\d+)?$}`)
	packageSelection    = php.MustCompile(`{^\s*(?P<name>[\S/]+)(?:\s+(?P<version>\S+))?\s*$}`)
)

// DetermineRequirements ports determineRequirements: the "name version"
// requirements of requires (finding versions where none is given), or
// asked interactively when requires is empty.
func (d *PackageDiscovery) DetermineRequirements(in console.Input, _ console.Output, requires []string, platformRepo *repository.PlatformRepository, preferredStability string, useBestVersionConstraint, fixed bool) ([]string, error) {
	if len(requires) > 0 {
		for _, p := range requires {
			if strings.ToLower(p) == "as" {
				return nil, NewError(ClassInvalidArgument, `Cannot use "`+p+`" as a separate argument. Quote the inline alias as one argument, e.g. "vendor/package:dev-main as 1.2.x-dev".`)
			}
		}

		normalized := d.cmd.NormalizeRequirements(requires)
		result := make([]string, 0, len(normalized))
		out := d.cmd.IO()

		for _, requirement := range normalized {
			if requirement.Version.Valid {
				tooStrict, err := tooStrictConstraint.IsMatch(requirement.Version.S)
				if err != nil {
					return nil, err
				}
				if tooStrict {
					out.WriteError(`<warning>The "`+requirement.Version.S+`" constraint for "`+requirement.Name+`" appears too strict and will likely not match what you want. See https://getcomposer.org/constraints</warning>`, true, io.Normal)
				}
			}

			if !requirement.Version.Valid {
				// determine the best version automatically
				name, ver, err := d.FindBestVersionAndNameForPackage(d.cmd.IO(), in, requirement.Name, platformRepo, preferredStability, fixed)
				if err != nil {
					return nil, err
				}

				// replace package name from packagist.org
				requirement.Name = name

				if useBestVersionConstraint {
					requirement.Version = pkg.Str(ver)
					out.WriteError("Using version <info>"+ver+"</info> for <info>"+name+"</info>", true, io.Normal)
				} else {
					requirement.Version = pkg.Str("guess")
				}
			}

			result = append(result, requirement.Name+" "+requirement.Version.S)
		}

		return result, nil
	}

	versionParser := pkg.NewVersionParser()

	// Collect existing packages
	var existingPackages []string
	composer, err := d.cmd.TryComposer(nil, nil)
	if err != nil {
		return nil, err
	}
	if composer != nil {
		installed, err := composer.RepositoryManager().LocalRepository().Packages()
		if err != nil {
			return nil, err
		}
		for _, p := range installed {
			existingPackages = append(existingPackages, p.Name())
		}
	}

	out := d.cmd.IO()
	for {
		answer, err := out.Ask("Search for a package: ", nil)
		if err != nil {
			return nil, err
		}
		if answer == nil {
			break
		}
		var pkgAnswer any = php.ToString(answer)
		query := php.ToString(answer)

		repos, err := d.Repos()
		if err != nil {
			return nil, err
		}
		found, err := repos.Search(query, repository.SearchFulltext, "")
		if err != nil {
			return nil, err
		}
		if len(found) == 0 {
			continue
		}

		// Remove existing packages from search results.
		matches := make([]repository.SearchResult, 0, len(found))
		for _, m := range found {
			if !slices.Contains(existingPackages, m.Name) {
				matches = append(matches, m)
			}
		}

		exactMatch := false
		for _, m := range matches {
			if m.Name == query {
				exactMatch = true

				break
			}
		}

		// no match, prompt which to pick
		if !exactMatch {
			providers, err := repos.Providers(query)
			if err != nil {
				return nil, err
			}
			if len(providers) > 0 {
				matches = append([]repository.SearchResult{{Name: query, Description: pkg.Str("")}}, matches...)
			}

			choices := make([]string, 0, len(matches))
			for position, foundPackage := range matches {
				abandoned := ""
				if foundPackage.Abandoned != nil {
					replacement := "No replacement was suggested"
					if s, ok := foundPackage.Abandoned.(string); ok {
						replacement = "Use " + s + " instead"
					}
					abandoned = "<warning>Abandoned. " + replacement + ".</warning>"
				}

				choices = append(choices, fmt.Sprintf(" <info>%5s</info> %s %s", "["+strconv.Itoa(position)+"]", foundPackage.Name, abandoned))
			}

			out.WriteErrorMessages([]string{
				"",
				"Found <info>" + strconv.Itoa(len(matches)) + "</info> packages matching <info>" + query + "</info>",
				"",
			}, true, io.Normal)

			out.WriteErrorMessages(choices, true, io.Normal)
			out.WriteError("", true, io.Normal)

			validator := func(answer any) (any, error) {
				selection := php.ToString(answer)
				if selection == "" {
					return false, nil
				}

				if php.IsNumeric(selection) {
					if i := php.ToInt(selection); i >= 0 && i < int64(len(matches)) {
						return matches[i].Name, nil
					}
				}

				m, err := packageSelection.Match(selection)
				if err != nil {
					return nil, err
				}
				if m != nil {
					name, _ := m.Named("name")
					if v, ok := m.Named("version"); ok {
						// parsing `acme/example ~2.3`

						// validate version constraint
						if _, err := versionParser.ParseConstraints(v); err != nil {
							return nil, err
						}

						return name + " " + v, nil
					}

					// parsing `acme/example`
					return name, nil
				}

				return nil, NewError("Exception", "Not a valid selection")
			}

			if pkgAnswer, err = out.AskAndValidate("Enter package # to add, or the complete package name if it is not listed: ", validator, 3, ""); err != nil {
				return nil, err
			}
		}

		// no constraint yet, determine the best version automatically
		if s, ok := pkgAnswer.(string); ok && !strings.Contains(s, " ") {
			validator := func(answer any) (any, error) {
				input := php.Trim(php.ToString(answer))
				if len(input) > 0 {
					return input, nil
				}

				return false, nil
			}

			constraint, err := out.AskAndValidate("Enter the version constraint to require (or leave blank to use the latest version): ", validator, 3, "")
			if err != nil {
				return nil, err
			}

			if constraint == false {
				_, ver, err := d.FindBestVersionAndNameForPackage(d.cmd.IO(), in, s, platformRepo, preferredStability, false)
				if err != nil {
					return nil, err
				}
				constraint = ver

				out.WriteError("Using version <info>"+ver+"</info> for <info>"+s+"</info>", true, io.Normal)
			}

			pkgAnswer = s + " " + php.ToString(constraint)
		}

		if pkgAnswer != false {
			s := php.ToString(pkgAnswer)
			requires = append(requires, s)
			existingPackages = append(existingPackages, strings.SplitN(s, " ", 2)[0])
		}
	}

	return requires, nil
}

// phpVersionForSelector is PHP_MAJOR_VERSION.PHP_MINOR_VERSION.PHP_RELEASE_VERSION.
func phpVersionForSelector(v string) string {
	parts := strings.SplitN(v, ".", 3)
	if len(parts) != 3 {
		return v
	}
	release := parts[2]
	end := 0
	for end < len(release) && release[end] >= '0' && release[end] <= '9' {
		end++
	}

	return parts[0] + "." + parts[1] + "." + release[:end]
}

// FindBestVersionAndNameForPackage ports findBestVersionAndNameForPackage:
// the name and the version to require (with ^ or ~ where possible).
func (d *PackageDiscovery) FindBestVersionAndNameForPackage(out io.IO, in console.Input, name string, platformRepo *repository.PlatformRepository, preferredStability string, fixed bool) (string, string, error) {
	// handle ignore-platform-reqs flag if present
	var platformRequirementFilter filter.PlatformRequirementFilter
	if in.HasOption("ignore-platform-reqs") && in.HasOption("ignore-platform-req") {
		var err error
		if platformRequirementFilter, err = d.cmd.PlatformRequirementFilter(in); err != nil {
			return "", "", err
		}
	} else {
		platformRequirementFilter = filter.IgnoreNothingFilter()
	}

	// find the latest version allowed in this repo set
	repoSet, err := d.RepositorySet(in, "")
	if err != nil {
		return "", "", err
	}
	var platformPackages []pkg.PackageInterface
	if platformRepo != nil {
		if platformPackages, err = platformRepo.Packages(); err != nil {
			return "", "", err
		}
	}
	versionSelector := version.NewVersionSelector(repoSet, platformPackages)
	if app := d.cmd.application(); app != nil {
		versionSelector.PHPVersion = phpVersionForSelector(app.Runtime().PHPVersion())
	}
	effectiveMinimumStability, err := d.minimumStability(in)
	if err != nil {
		return "", "", err
	}

	find := func(f filter.PlatformRequirementFilter, flags int, withIO bool) (pkg.PackageInterface, error) {
		opts := version.FindBestCandidateOptions{PreferredStability: preferredStability, PlatformRequirementFilter: f, RepoSetFlags: flags}
		if withIO {
			opts.IO = d.cmd.IO()
		}

		return versionSelector.FindBestCandidate(name, opts)
	}

	p, err := find(platformRequirementFilter, 0, true)
	if err != nil {
		return "", "", err
	}

	if p == nil {
		// platform packages can not be found in the pool in versions other than the local platform's has
		// so if platform reqs are ignored we just take the user's word for it
		if platformRequirementFilter.IsIgnored(name) {
			return name, "*", nil
		}

		// Check if it is a virtual package provided by others
		providers, err := repoSet.Providers(name)
		if err != nil {
			return "", "", err
		}
		if len(providers) > 0 {
			var constraint any = "*"
			if in.IsInteractive() {
				constraint, err = d.cmd.IO().AskAndValidate(`Package "<info>`+name+`</info>" does not exist but is provided by `+strconv.Itoa(len(providers))+` packages. Which version constraint would you like to use? [<info>*</info>] `, func(value any) (any, error) {
					if _, err := pkg.NewVersionParser().ParseConstraints(php.ToString(value)); err != nil {
						return nil, err
					}

					return value, nil
				}, 3, "*")
				if err != nil {
					return "", "", err
				}
			}

			return name, php.ToString(constraint), nil
		}

		_, ignoresAll := platformRequirementFilter.(version.IgnoreAllPlatformRequirementFilter)

		// Check whether the package requirements were the problem
		if !ignoresAll {
			candidate, err := find(filter.IgnoreAllFilter(), 0, false)
			if err != nil {
				return "", "", err
			}
			if candidate != nil {
				details, err := d.PlatformExceptionDetails(candidate, platformRepo)
				if err != nil {
					return "", "", err
				}

				msg, err := php.Sprintf("Package %s has requirements incompatible with your PHP version, PHP extensions and Composer version"+details, name)
				if err != nil {
					return "", "", err
				}

				return "", "", NewError(ClassInvalidArgument, msg)
			}
		}
		// Check whether the minimum stability was the problem but the package exists
		if p, err = find(platformRequirementFilter, repository.AllowUnacceptableStabilities, false); err != nil {
			return "", "", err
		}
		if p != nil {
			// we must first verify if a valid package would be found in a lower priority repository
			allReposPackage, err := find(platformRequirementFilter, repository.AllowShadowedRepositories, false)
			if err != nil {
				return "", "", err
			}
			if allReposPackage != nil {
				return "", "", NewError(ClassInvalidArgument,
					"Package "+name+" exists in "+repoName(allReposPackage)+" and "+repoName(p)+" which has a higher repository priority. The packages from the higher priority repository do not match your minimum-stability and are therefore not installable. That repository is canonical so the lower priority repo's packages are not installable. See https://getcomposer.org/repoprio for details and assistance.")
			}

			return "", "", NewError(ClassInvalidArgument, "Could not find a version of package "+name+" matching your minimum-stability ("+effectiveMinimumStability+"). Require it with an explicit version constraint allowing its desired stability.")
		}
		// Check whether the PHP version was the problem for all versions
		if !ignoresAll {
			candidate, err := find(filter.IgnoreAllFilter(), repository.AllowUnacceptableStabilities, false)
			if err != nil {
				return "", "", err
			}
			if candidate != nil {
				additional := ""
				stable, err := find(filter.IgnoreAllFilter(), 0, false)
				if err != nil {
					return "", "", err
				}
				if stable == nil {
					additional = php.EOL + php.EOL + "Additionally, the package was only found with a stability of \"" + candidate.Stability() + "\" while your minimum stability is \"" + effectiveMinimumStability + "\"."
				}

				details, err := d.PlatformExceptionDetails(candidate, platformRepo)
				if err != nil {
					return "", "", err
				}

				msg, err := php.Sprintf("Could not find package %s in any version matching your PHP version, PHP extensions and Composer version"+details+"%s", name, additional)
				if err != nil {
					return "", "", err
				}

				return "", "", NewError(ClassInvalidArgument, msg)
			}
		}

		// Check for similar names/typos
		similar, err := d.findSimilar(name)
		if err != nil {
			return "", "", err
		}
		if len(similar) > 0 {
			if slices.Contains(similar, name) {
				return "", "", NewError(ClassInvalidArgument, "Could not find package "+name+". It was however found via repository search, which indicates a consistency issue with the repository.")
			}

			if in.IsInteractive() {
				choices := php.NewArray()
				for _, s := range similar {
					choices.Append(s)
				}
				result, err := out.Select("<error>Could not find package "+name+".</error>\nPick one of these or leave empty to abort:", choices, false, 1, `Value "%s" is invalid`, false)
				if err != nil {
					return "", "", err
				}
				if result != false {
					if i := php.ToInt(result); i >= 0 && i < int64(len(similar)) {
						name, ver, err := d.FindBestVersionAndNameForPackage(out, in, similar[i], platformRepo, preferredStability, fixed)

						return name, ver, err
					}
				}
			}

			which := "this"
			if len(similar) > 1 {
				which = "one of these"
			}

			return "", "", NewError(ClassInvalidArgument, "Could not find package "+name+".\n\nDid you mean "+which+"?\n    "+strings.Join(similar, "\n    "))
		}

		return "", "", NewError(ClassInvalidArgument, "Could not find a matching version of package "+name+". Check the package spelling, your version constraint and that the package is available in a stability which matches your minimum-stability ("+effectiveMinimumStability+").")
	}

	if fixed {
		return p.PrettyName(), p.PrettyVersion(), nil
	}
	recommended, err := versionSelector.FindRecommendedRequireVersion(p)
	if err != nil {
		return "", "", err
	}

	return p.PrettyName(), recommended, nil
}

// repoName is $package->getRepository()->getRepoName().
func repoName(p pkg.PackageInterface) string {
	if r := p.Repository(); r != nil {
		return r.RepoName()
	}

	return ""
}

// findSimilar ports findSimilar: up to five package names close to
// name, closest first.
func (d *PackageDiscovery) findSimilar(name string) ([]string, error) {
	if d.repos == nil {
		return nil, NewError(ClassLogic, "findSimilar was called before $this->repos was initialized")
	}
	results, err := d.repos.Search(name, repository.SearchFulltext, "")
	if err != nil {
		if phperr.InstanceOf(err, ClassLogic) {
			return nil, err
		}

		// ignore search errors
		return nil, nil //nolint:nilerr // as Composer
	}

	composer, err := d.cmd.RequireComposer(nil, nil)
	if err != nil {
		return nil, err
	}
	installedRepo := composer.RepositoryManager().LocalRepository()

	type similar struct {
		name     string
		distance int
	}
	var similarPackages []similar
	index := map[string]int{}
	for _, result := range results {
		installed, err := installedRepo.FindPackage(result.Name, nil)
		if err != nil {
			return nil, err
		}
		if installed != nil {
			// Ignore installed package
			continue
		}
		distance := php.Levenshtein(name, result.Name)
		if i, ok := index[result.Name]; ok {
			similarPackages[i].distance = distance
		} else {
			index[result.Name] = len(similarPackages)
			similarPackages = append(similarPackages, similar{result.Name, distance})
		}
	}
	// asort: stable since PHP 8
	slices.SortStableFunc(similarPackages, func(a, b similar) int { return a.distance - b.distance })

	names := make([]string, 0, 5)
	for _, s := range similarPackages[:min(5, len(similarPackages))] {
		names = append(names, s.name)
	}

	return names, nil
}

// PlatformExceptionDetails ports getPlatformExceptionDetails: why
// candidate's platform requirements do not match platformRepo ("" when
// platformRepo is nil or nothing explains it).
func (*PackageDiscovery) PlatformExceptionDetails(candidate pkg.PackageInterface, platformRepo *repository.PlatformRepository) (string, error) {
	if platformRepo == nil {
		return "", nil
	}

	var details []string
	for link := range candidate.Requires().Values() {
		if !repository.IsPlatformPackage(link.Target()) {
			continue
		}
		prettyConstraint, err := link.PrettyConstraint()
		if err != nil {
			return "", err
		}
		platformPkg, err := platformRepo.FindPackage(link.Target(), nil)
		if err != nil {
			return "", err
		}
		if platformPkg == nil {
			if platformRepo.IsPlatformPackageDisabled(link.Target()) {
				details = append(details, candidate.PrettyName()+" "+candidate.PrettyVersion()+" requires "+link.Target()+" "+prettyConstraint+` but it is disabled by your platform config. Enable it again with "composer config platform.`+link.Target()+` --unset".`)
			} else {
				details = append(details, candidate.PrettyName()+" "+candidate.PrettyVersion()+" requires "+link.Target()+" "+prettyConstraint+" but it is not present.")
			}

			continue
		}
		if !link.Constraint().Matches(semver.NewConstraintOp(semver.OpEQ, platformPkg.Version())) {
			platformPkgVersion := platformPkg.PrettyVersion()
			if v, ok := platformPkg.Extra().Get("config.platform"); ok && v != nil {
				if cp, ok := platformPkg.(pkg.CompletePackageInterface); ok {
					platformPkgVersion += " (" + cp.Description().S + ")"
				}
			}
			details = append(details, candidate.PrettyName()+" "+candidate.PrettyVersion()+" requires "+link.Target()+" "+prettyConstraint+" which does not match your installed version "+platformPkgVersion+".")
		}
	}

	if len(details) == 0 {
		return "", nil
	}

	return ":" + php.EOL + "  - " + strings.Join(details, php.EOL+"  - "), nil
}
