// Ports src/Composer/Command/UpdateCommand.php.

package command

import (
	"slices"
	"strings"

	"github.com/stubbedev/maestro/internal/advisory"
	"github.com/stubbedev/maestro/internal/composer"
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/eventdispatcher"
	"github.com/stubbedev/maestro/internal/filter"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/loader"
	"github.com/stubbedev/maestro/internal/pkg/version"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/resolver"
	"github.com/stubbedev/maestro/internal/resolver/operation"
	"github.com/stubbedev/maestro/internal/semver"
)

func init() {
	registerCommand(OrderUpdate, func() console.Commander { return NewUpdateCommand() })
}

// UpdateCommand is Composer\Command\UpdateCommand.
type UpdateCommand struct{ *BaseCommand }

// NewUpdateCommand ports new UpdateCommand() (configure()).
func NewUpdateCommand() *UpdateCommand {
	c := &UpdateCommand{BaseCommand: NewBaseCommand("")}
	c.SetImpl(c)
	c.SetName("update")
	c.SetAliases("u", "upgrade")
	c.SetDescription("Updates your dependencies to the latest version according to composer.json, and updates the composer.lock file")
	c.SetDefinitionItems(
		console.MustArgument("packages", console.ArgumentIsArray|console.ArgumentOptional, "Packages that should be updated, if not provided all packages are.", nil).WithSuggestFunc(c.SuggestInstalledPackage(false, false)),
		console.MustOption("with", "", console.OptionValueIsArray|console.OptionValueRequired, "Temporary version constraint to add, e.g. foo/bar:1.0.0 or foo/bar=1.0.0", nil),
		console.MustOption("prefer-source", "", console.OptionValueNone, "Forces installation from package sources when possible, including VCS information.", nil),
		console.MustOption("prefer-dist", "", console.OptionValueNone, "Forces installation from package dist (default behavior).", nil),
		optionWithSuggestions("prefer-install", "", console.OptionValueRequired, "Forces installation from package dist|source|auto (auto chooses source for dev versions, dist for the rest).", nil, SuggestPreferInstall()...),
		console.MustOption("dry-run", "", console.OptionValueNone, "Outputs the operations but will not execute anything (implicitly enables --verbose).", nil),
		console.MustOption("dev", "", console.OptionValueNone, "DEPRECATED: Enables installation of require-dev packages (enabled by default, only present for BC).", nil),
		console.MustOption("no-dev", "", console.OptionValueNone, "Disables installation of require-dev packages.", nil),
		console.MustOption("lock", "", console.OptionValueNone, "Overwrites the lock file hash to suppress warning about the lock file being out of date without updating package versions. Package metadata like mirrors and URLs are updated if they changed.", nil),
		console.MustOption("no-install", "", console.OptionValueNone, "Skip the install step after updating the composer.lock file.", nil),
		console.MustOption("no-audit", "", console.OptionValueNone, "Skip the audit step after updating the composer.lock file (can also be set via the COMPOSER_NO_AUDIT=1 env var).", nil),
		optionWithSuggestions("audit-format", "", console.OptionValueRequired, `Audit output format. Must be "table", "plain", "json", or "summary".`, advisory.FormatSummary, advisory.Formats[:]...),
		console.MustOption("no-security-blocking", "", console.OptionValueNone, "DEPRECATED: use --no-blocking instead. Allows installing packages with security advisories or that are abandoned (can also be set via the COMPOSER_NO_SECURITY_BLOCKING=1 env var).", nil),
		console.MustOption("no-blocking", "", console.OptionValueNone, "Disables all policy blocking during this command (can also be set via the COMPOSER_NO_BLOCKING=1 env var).", nil),
		console.MustOption("no-autoloader", "", console.OptionValueNone, "Skips autoloader generation", nil),
		console.MustOption("no-suggest", "", console.OptionValueNone, "DEPRECATED: This flag does not exist anymore.", nil),
		console.MustOption("no-progress", "", console.OptionValueNone, "Do not output download progress.", nil),
		console.MustOption("with-dependencies", "w", console.OptionValueNone, "Update also dependencies of packages in the argument list, except those which are root requirements (can also be set via the COMPOSER_WITH_DEPENDENCIES=1 env var).", nil),
		console.MustOption("with-all-dependencies", "W", console.OptionValueNone, "Update also dependencies of packages in the argument list, including those which are root requirements (can also be set via the COMPOSER_WITH_ALL_DEPENDENCIES=1 env var).", nil),
		console.MustOption("verbose", "v|vv|vvv", console.OptionValueNone, "Shows more details including new commits pulled in when updating packages.", nil),
		console.MustOption("optimize-autoloader", "o", console.OptionValueNone, "Optimize autoloader during autoloader dump.", nil),
		console.MustOption("classmap-authoritative", "a", console.OptionValueNone, "Autoload classes from the classmap only. Implicitly enables `--optimize-autoloader`.", nil),
		console.MustOption("strict-psr-autoloader", "", console.OptionValueNone, "Return a failed status code (6) if PSR-4 or PSR-0 mapping errors are present. Requires --optimize-autoloader to work.", nil),
		console.MustOption("apcu-autoloader", "", console.OptionValueNone, "Use APCu to cache found/not-found classes.", nil),
		console.MustOption("apcu-autoloader-prefix", "", console.OptionValueRequired, "Use a custom prefix for the APCu autoloader cache. Implicitly enables --apcu-autoloader", nil),
		console.MustOption("ignore-platform-req", "", console.OptionValueRequired|console.OptionValueIsArray, "Ignore a specific platform requirement (php & ext- packages).", nil),
		console.MustOption("ignore-platform-reqs", "", console.OptionValueNone, "Ignore all platform requirements (php & ext- packages).", nil),
		console.MustOption("prefer-stable", "", console.OptionValueNone, "Prefer stable versions of dependencies (can also be set via the COMPOSER_PREFER_STABLE=1 env var).", nil),
		console.MustOption("prefer-lowest", "", console.OptionValueNone, "Prefer lowest versions of dependencies (can also be set via the COMPOSER_PREFER_LOWEST=1 env var).", nil),
		console.MustOption("minimal-changes", "m", console.OptionValueNone, "Only perform absolutely necessary changes to dependencies. If packages cannot be kept at their currently locked version they are updated. For partial updates the allow-listed packages are always updated fully. (can also be set via the COMPOSER_MINIMAL_CHANGES=1 env var).", nil),
		console.MustOption("patch-only", "", console.OptionValueNone, "Only allow patch version updates for currently installed dependencies.", nil),
		console.MustOption("interactive", "i", console.OptionValueNone, "Interactive interface with autocompletion to select the packages to update.", nil),
		console.MustOption("root-reqs", "", console.OptionValueNone, "Restricts the update to your first degree dependencies.", nil),
		optionWithSuggestions("bump-after-update", "", console.OptionValueOptional, "Runs bump after performing the update.", false, "dev", "no-dev", "all"),
	)
	c.SetHelp(`The <info>update</info> command reads the composer.json file from the
current directory, processes it, and updates, removes or installs all the
dependencies.

<info>php composer.phar update</info>

To limit the update operation to a few packages, you can list the package(s)
you want to update as such:

<info>php composer.phar update vendor/package1 foo/mypackage [...]</info>

You may also use an asterisk (*) pattern to limit the update operation to package(s)
from a specific vendor:

<info>php composer.phar update vendor/package1 foo/* [...]</info>

To run an update with more restrictive constraints you can use:

<info>php composer.phar update --with vendor/package:1.0.*</info>

To run a partial update with more restrictive constraints you can use the shorthand:

<info>php composer.phar update vendor/package:1.0.*</info>

To select packages names interactively with auto-completion use <info>-i</info>.

Read more at https://getcomposer.org/doc/03-cli.md#update-u-upgrade`)

	return c
}

// PHPClass implements php.Classer.
func (*UpdateCommand) PHPClass() string { return `Composer\Command\UpdateCommand` }

// updateLink is an entry of a PHP array of links keyed by package name.
type updateLink struct {
	name string
	link *pkg.Link
}

// updateMergeLinks is array_merge() of link arrays keyed by package name: a
// later key replaces the value of an earlier one in its position.
func updateMergeLinks(lists ...pkg.Links) []updateLink {
	var merged []updateLink
	index := map[string]int{}
	for _, links := range lists {
		for name, link := range links.All() {
			if i, ok := index[name]; ok {
				merged[i].link = link

				continue
			}
			index[name] = len(merged)
			merged = append(merged, updateLink{name, link})
		}
	}

	return merged
}

func updateFindLink(links []updateLink, name string) *pkg.Link {
	for _, l := range links {
		if l.name == name {
			return l.link
		}
	}

	return nil
}

// Execute ports execute().
func (c *UpdateCommand) Execute(in console.Input, out console.Output) (int, error) {
	cio := c.IO()
	if console.BoolOption(in, "dev") {
		cio.WriteError(`<warning>You are using the deprecated option "--dev". It has no effect and will break in Composer 3.</warning>`, true, io.Normal)
	}
	if console.BoolOption(in, "no-suggest") {
		cio.WriteError(`<warning>You are using the deprecated option "--no-suggest". It has no effect and will break in Composer 3.</warning>`, true, io.Normal)
	}

	comp, err := c.RequireComposer(nil, nil)
	if err != nil {
		return 0, err
	}

	// HttpDownloader::isCurlEnabled() is always true (native HTTP client).

	packages := console.StringsArgument(in, "packages")
	reqs, err := c.FormatRequirements(console.StringsOption(in, "with"))
	if err != nil {
		return 0, err
	}

	// extract --with shorthands from the allowlist
	if len(packages) > 0 {
		var allowlistPackagesWithRequirements []string
		for _, p := range packages {
			ok, err := php.PregIsMatch(`{\S+[ =:]\S+}`, p)
			if err != nil {
				return 0, err
			}
			if ok {
				allowlistPackagesWithRequirements = append(allowlistPackagesWithRequirements, p)
			}
		}
		formatted, err := c.FormatRequirements(allowlistPackagesWithRequirements)
		if err != nil {
			return 0, err
		}
		for k, v := range formatted.All() {
			reqs.SetKey(k, v)
		}

		// replace the foo/bar:req by foo/bar in the allowlist
		for _, p := range allowlistPackagesWithRequirements {
			packageName, _, err := php.PregReplace(`{^([^ =:]+)[ =:].*$}`, "$1", p, -1)
			if err != nil {
				return 0, err
			}
			for i, existing := range packages {
				if existing == p {
					packages[i] = packageName

					break
				}
			}
		}
	}

	rootPackage := comp.Package()
	references, err := loader.ExtractReferences(reqs, rootPackage.References())
	if err != nil {
		return 0, err
	}
	rootPackage.SetReferences(references)
	stabilityFlags, err := loader.ExtractStabilityFlags(reqs, rootPackage.MinimumStability(), rootPackage.StabilityFlags())
	if err != nil {
		return 0, err
	}
	rootPackage.SetStabilityFlags(stabilityFlags)

	parser := pkg.NewVersionParser()
	temporaryConstraints := &repository.ConstraintMap{}
	rootRequirements := updateMergeLinks(rootPackage.Requires(), rootPackage.DevRequires())
	for k, v := range reqs.All() {
		packageName := php.Strtolower(k.String())
		constraint := php.ToString(v)
		parsedConstraint, err := parser.ParseConstraints(constraint)
		if err != nil {
			return 0, err
		}

		// handling wildcard packages will only work for root requirements, not for transient dependencies
		if strings.Contains(packageName, "*") {
			packageFilterRegex := pkg.PackageNameToRegexp(packageName, "{^%s$}i")
			for _, rootRequirement := range rootRequirements {
				ok, err := php.PregIsMatch(packageFilterRegex, rootRequirement.name)
				if err != nil {
					return 0, err
				}
				if !ok {
					continue
				}

				temporaryConstraints.Set(rootRequirement.name, parsedConstraint)
				if !semver.Intervals.HaveIntersections(parsedConstraint, rootRequirement.link.Constraint()) {
					pretty, err := rootRequirement.link.PrettyConstraint()
					if err != nil {
						return 0, err
					}
					cio.WriteError(`<error>The temporary constraint "`+constraint+`" for "`+packageName+`" matching "`+rootRequirement.name+`" must be a subset of the constraint in your composer.json (`+pretty+`)</error>`, true, io.Normal)

					return 1, nil
				}
			}
		} else {
			temporaryConstraints.Set(packageName, parsedConstraint)
			if rootRequirement := updateFindLink(rootRequirements, packageName); rootRequirement != nil && !semver.Intervals.HaveIntersections(parsedConstraint, rootRequirement.Constraint()) {
				pretty, err := rootRequirement.PrettyConstraint()
				if err != nil {
					return 0, err
				}
				cio.WriteError(`<error>The temporary constraint "`+constraint+`" for "`+packageName+`" must be a subset of the constraint in your composer.json (`+pretty+`)</error>`, true, io.Normal)
				cio.Write("<info>Run `composer require "+packageName+"` or `composer require "+packageName+":"+constraint+"` instead to replace the constraint</info>", true, io.Normal)

				return 1, nil
			}
		}
	}

	if console.BoolOption(in, "patch-only") {
		locked, err := comp.Locker().IsLocked()
		if err != nil {
			return 0, err
		}
		if !locked {
			return 0, NewError(ClassInvalidArgument, "patch-only can only be used with a lock file present")
		}
		lockedRepo, err := comp.Locker().LockedRepository(true)
		if err != nil {
			return 0, err
		}
		canonical, err := lockedRepo.CanonicalPackages()
		if err != nil {
			return 0, err
		}
		for _, p := range canonical {
			if p.IsDev() {
				continue
			}
			match, err := php.PregMatch(`{^(\d+\.\d+\.\d+)}`, p.Version())
			if err != nil {
				return 0, err
			}
			if match == nil {
				continue
			}
			constraint, err := parser.ParseConstraints("~" + updateMatchGroup(match, 1))
			if err != nil {
				return 0, err
			}
			if existing, ok := temporaryConstraints.Get(p.Name()); ok {
				temporaryConstraints.Set(p.Name(), semver.CreateMultiConstraint([]semver.ConstraintInterface{existing, constraint}, true))
			} else {
				temporaryConstraints.Set(p.Name(), constraint)
			}
		}
	}

	if console.BoolOption(in, "interactive") {
		if packages, err = c.packagesInteractively(cio, in, out, comp, packages); err != nil {
			return 0, err
		}
	}

	if console.BoolOption(in, "root-reqs") {
		var requires []string
		for name := range rootPackage.Requires().All() {
			requires = append(requires, name)
		}
		if !console.BoolOption(in, "no-dev") {
			for name := range rootPackage.DevRequires().All() {
				requires = append(requires, name)
			}
		}

		if len(packages) > 0 {
			var intersected []string
			for _, p := range packages {
				if slices.Contains(requires, p) {
					intersected = append(intersected, p)
				}
			}
			packages = intersected
		} else {
			packages = requires
		}
	}

	// the arguments lock/nothing/mirrors are not package names but trigger a mirror update instead
	// they are further mutually exclusive with listing actual package names
	var filteredPackages []string
	for _, p := range packages {
		if p != "lock" && p != "nothing" && p != "mirrors" {
			filteredPackages = append(filteredPackages, p)
		}
	}
	updateMirrors := console.BoolOption(in, "lock") || len(filteredPackages) != len(packages)
	packages = filteredPackages

	if updateMirrors && len(packages) > 0 {
		cio.WriteError("<error>You cannot simultaneously update only a selection of packages and regenerate the lock file metadata.</error>", true, io.Normal)

		return -1, nil
	}

	commandEvent := eventdispatcher.NewCommandEvent(eventdispatcher.PluginCommand, "update", in, out, nil, nil)
	if _, err := comp.EventDispatcher().Dispatch(commandEvent.Name(), commandEvent); err != nil {
		return 0, err
	}

	setOutputProgress(comp.InstallationManager(), !console.BoolOption(in, "no-progress"))

	install, err := composer.CreateInstaller(cio, comp)
	if err != nil {
		return 0, err
	}

	cfg := comp.Config()
	installPreference, err := c.PreferredInstallOptions(cfg, in, false)
	if err != nil {
		return 0, err
	}

	optimize, err := optionOrConfig(in, "optimize-autoloader", cfg, "optimize-autoloader")
	if err != nil {
		return 0, err
	}
	authoritative, err := optionOrConfig(in, "classmap-authoritative", cfg, "classmap-authoritative")
	if err != nil {
		return 0, err
	}
	apcu, apcuPrefix, err := apcuOptions(in, cfg, "apcu-autoloader", "apcu-autoloader-prefix")
	if err != nil {
		return 0, err
	}
	minimalChanges, err := optionOrConfig(in, "minimal-changes", cfg, "update-with-minimal-changes")
	if err != nil {
		return 0, err
	}

	if err := checkStrictPSRAutoloader(in, optimize, authoritative); err != nil {
		return 0, err
	}

	updateAllowTransitiveDependencies := resolver.UpdateOnlyListed
	if console.BoolOption(in, "with-all-dependencies") {
		updateAllowTransitiveDependencies = resolver.UpdateListedWithTransitiveDeps
	} else if console.BoolOption(in, "with-dependencies") {
		updateAllowTransitiveDependencies = resolver.UpdateListedWithTransitiveDepsNoRootRequire
	}

	platformFilter, err := c.PlatformRequirementFilter(in)
	if err != nil {
		return 0, err
	}
	policyConfig, err := c.CreatePolicyConfig(cfg, in)
	if err != nil {
		return 0, err
	}
	auditConfig, err := c.CreateAuditConfig(in)
	if err != nil {
		return 0, err
	}

	install.
		SetDryRun(console.BoolOption(in, "dry-run")).
		SetVerbose(console.BoolOption(in, "verbose")).
		SetInstallPreference(installPreference).
		SetDevMode(!console.BoolOption(in, "no-dev")).
		SetDumpAutoloader(!console.BoolOption(in, "no-autoloader")).
		SetOptimizeAutoloader(optimize).
		SetClassMapAuthoritative(authoritative).
		SetStrictPsrAutoloader(console.BoolOption(in, "strict-psr-autoloader")).
		SetApcuAutoloader(apcu, apcuPrefix).
		SetUpdate(true).
		SetInstall(!console.BoolOption(in, "no-install")).
		SetUpdateMirrors(updateMirrors).
		SetUpdateAllowList(packages)
	if _, err := install.SetUpdateAllowTransitiveDependencies(updateAllowTransitiveDependencies); err != nil {
		return 0, err
	}
	install.
		SetPlatformRequirementFilter(platformFilter).
		SetPreferStable(console.BoolOption(in, "prefer-stable")).
		SetPreferLowest(console.BoolOption(in, "prefer-lowest")).
		SetTemporaryConstraints(temporaryConstraints).
		SetPolicyConfig(policyConfig).
		SetAuditConfig(auditConfig).
		SetMinimalUpdate(minimalChanges)

	if console.BoolOption(in, "no-plugins") {
		if _, err := install.DisablePlugins(); err != nil {
			return 0, err
		}
	}

	result, err := install.Run()
	if err != nil {
		return 0, err
	}

	if result == 0 && !console.BoolOption(in, "lock") {
		bumpAfterUpdate := in.Option("bump-after-update")
		if bumpAfterUpdate == false {
			if bumpAfterUpdate, err = cfg.Get("bump-after-update", 0); err != nil {
				return 0, err
			}
		}

		if bumpAfterUpdate != false {
			var updatedPackages []string
			if lockTransaction := install.LockTransaction(); lockTransaction != nil {
				for _, op := range lockTransaction.Operations() {
					switch o := op.(type) {
					case *operation.InstallOperation:
						updatedPackages = append(updatedPackages, o.Package().Name())
					case *operation.UpdateOperation:
						updatedPackages = append(updatedPackages, o.TargetPackage().Name())
					}
				}
			}

			if len(updatedPackages) > 0 {
				cio.WriteError("<info>Bumping dependencies</info>", true, io.Normal)
				bumpCommand := NewBumpCommand()
				bumpCommand.SetComposer(comp)
				result, err = bumpCommand.DoBump(
					cio,
					bumpAfterUpdate == "dev",
					bumpAfterUpdate == "no-dev",
					console.BoolOption(in, "dry-run"),
					updatedPackages,
					"--bump-after-update=dev",
				)
				if err != nil {
					return 0, err
				}
			}
		}
	}

	return result, nil
}

// packagesInteractively ports getPackagesInteractively.
func (c *UpdateCommand) packagesInteractively(cio io.IO, in console.Input, out console.Output, comp *composer.Composer, packages []string) ([]string, error) {
	if !in.IsInteractive() {
		return nil, NewError(ClassInvalidArgument, "--interactive cannot be used in non-interactive terminals.")
	}

	platformReqFilter, err := c.PlatformRequirementFilter(in)
	if err != nil {
		return nil, err
	}
	root := comp.Package()
	stabilityFlags := root.StabilityFlags()
	requires := updateMergeLinks(root.Requires(), root.DevRequires())

	var filterRegex string
	if len(packages) > 0 {
		filterRegex = pkg.PackageNamesToRegexp(packages, "{^(?:%s)$}iD")
	}

	cio.WriteError("<info>Loading packages that can be updated...</info>", true, io.Normal)
	autocompleterValues := php.NewArray()
	locked, err := comp.Locker().IsLocked()
	if err != nil {
		return nil, err
	}
	var installedPackages []pkg.PackageInterface
	if locked {
		lockedRepo, err := comp.Locker().LockedRepository(true)
		if err != nil {
			return nil, err
		}
		if installedPackages, err = lockedRepo.Packages(); err != nil {
			return nil, err
		}
	} else if installedPackages, err = comp.RepositoryManager().LocalRepository().Packages(); err != nil {
		return nil, err
	}
	versionSelector, err := c.createVersionSelector(comp)
	if err != nil {
		return nil, err
	}
	for _, p := range installedPackages {
		if filterRegex != "" {
			ok, err := php.PregIsMatch(filterRegex, p.Name())
			if err != nil {
				return nil, err
			}
			if !ok {
				continue
			}
		}
		currentVersion := p.PrettyVersion()
		var constraint string
		if link := updateFindLink(requires, p.Name()); link != nil {
			if constraint, err = link.PrettyConstraint(); err != nil {
				return nil, err
			}
		}
		stability := root.MinimumStability()
		if stabilityFlags.Has(p.Name()) {
			stability = ""
			flag, _ := stabilityFlags.Get(p.Name())
			for name, value := range pkg.Stabilities().All() {
				if php.StrictEquals(value, flag) {
					stability = name.String()

					break
				}
			}
		}
		latestVersion, err := versionSelector.FindBestCandidate(p.Name(), version.FindBestCandidateOptions{
			TargetPackageVersion:      constraint,
			PreferredStability:        stability,
			PlatformRequirementFilter: platformReqFilter,
		})
		if err != nil {
			return nil, err
		}
		if latestVersion != nil && (p.Version() != latestVersion.Version() || latestVersion.IsDev()) {
			autocompleterValues.Set(p.Name(), "<comment>"+currentVersion+"</comment> => <comment>"+latestVersion.PrettyVersion()+"</comment>")
		}
	}
	if len(installedPackages) == 0 {
		for _, req := range requires {
			if pkg.IsPlatformPackage(req.name) {
				continue
			}
			autocompleterValues.Set(req.name, "")
		}
	}

	if autocompleterValues.Len() == 0 {
		return nil, NewError(ClassRuntime, "Could not find any package with new versions available")
	}

	selected, err := cio.Select(
		"Select packages: (Select more than one value separated by comma) ",
		autocompleterValues,
		false,
		1,
		`No package named "%s" is installed.`,
		true,
	)
	if err != nil {
		return nil, err
	}
	packages = selectedStrings(selected)

	table := console.NewTable(out)
	table.SetHeaders([]any{"Selected packages"})
	for _, p := range packages {
		if err := table.AddRow([]any{p}); err != nil {
			return nil, err
		}
	}
	if err := table.Render(); err != nil {
		return nil, err
	}

	plural := "s"
	if len(packages) == 1 {
		plural = ""
	}
	ok, err := cio.AskConfirmation("Would you like to continue and update the above package"+plural+" [<comment>yes</comment>]? ", true)
	if err != nil {
		return nil, err
	}
	if ok {
		return packages, nil
	}

	return nil, NewError(ClassRuntime, "Installation aborted.")
}

// selectedStrings converts IOInterface::select's multiselect result to a
// list of strings.
func selectedStrings(v any) []string {
	switch x := v.(type) {
	case *php.Array:
		out := make([]string, 0, x.Len())
		for _, e := range x.All() {
			out = append(out, php.ToString(e))
		}

		return out
	case []string:
		return x
	case []any:
		out := make([]string, len(x))
		for i, e := range x {
			out[i] = php.ToString(e)
		}

		return out
	case nil:
		return nil
	}

	return []string{php.ToString(v)}
}

// createVersionSelector ports createVersionSelector.
func (*UpdateCommand) createVersionSelector(comp *composer.Composer) (*version.VersionSelector, error) {
	repositorySet, err := repository.NewRepositorySet("stable", nil, nil, nil, nil, nil)
	if err != nil {
		return nil, err
	}
	var repos []repository.RepositoryInterface
	for _, r := range comp.RepositoryManager().Repositories() {
		if _, ok := r.(*repository.PlatformRepository); !ok {
			repos = append(repos, r)
		}
	}
	composite, err := repository.NewCompositeRepository(repos)
	if err != nil {
		return nil, err
	}
	if err := repositorySet.AddRepository(composite); err != nil {
		return nil, err
	}

	selector := version.NewVersionSelector(repositorySet, nil)
	if rt := comp.Runtime(); rt != nil {
		selector.PHPVersion = rt.PHPVersion()
	}

	return selector, nil
}

var _ version.PlatformRequirementFilter = filter.IgnoreNothingFilter()

// updateMatchGroup is $match[n] of a successful match.
func updateMatchGroup(m *php.Match, n int) string {
	g, _ := m.Group(n)

	return g
}
