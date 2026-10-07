// Ports src/Composer/Command/ReinstallCommand.php.

package command

import (
	"slices"

	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/eventdispatcher"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/resolver"
	"github.com/stubbedev/maestro/internal/resolver/operation"
	"github.com/stubbedev/maestro/internal/script"
	"github.com/stubbedev/maestro/internal/util"
)

func init() {
	registerCommand(OrderReinstall, func() console.Commander { return NewReinstallCommand() })
}

// ReinstallCommand is Composer\Command\ReinstallCommand.
type ReinstallCommand struct{ *BaseCommand }

// NewReinstallCommand ports new ReinstallCommand() (configure()).
func NewReinstallCommand() *ReinstallCommand {
	c := &ReinstallCommand{BaseCommand: NewBaseCommand("")}
	c.SetImpl(c)
	c.SetName("reinstall")
	c.SetDescription("Uninstalls and reinstalls the given package names")
	c.SetDefinitionItems(
		console.MustOption("prefer-source", "", console.OptionValueNone, "Forces installation from package sources when possible, including VCS information.", nil),
		console.MustOption("prefer-dist", "", console.OptionValueNone, "Forces installation from package dist (default behavior).", nil),
		optionWithSuggestions("prefer-install", "", console.OptionValueRequired, "Forces installation from package dist|source|auto (auto chooses source for dev versions, dist for the rest).", nil, SuggestPreferInstall()...),
		console.MustOption("no-autoloader", "", console.OptionValueNone, "Skips autoloader generation", nil),
		console.MustOption("no-progress", "", console.OptionValueNone, "Do not output download progress.", nil),
		console.MustOption("optimize-autoloader", "o", console.OptionValueNone, "Optimize autoloader during autoloader dump", nil),
		console.MustOption("classmap-authoritative", "a", console.OptionValueNone, "Autoload classes from the classmap only. Implicitly enables `--optimize-autoloader`.", nil),
		console.MustOption("apcu-autoloader", "", console.OptionValueNone, "Use APCu to cache found/not-found classes.", nil),
		console.MustOption("apcu-autoloader-prefix", "", console.OptionValueRequired, "Use a custom prefix for the APCu autoloader cache. Implicitly enables --apcu-autoloader", nil),
		console.MustOption("ignore-platform-req", "", console.OptionValueRequired|console.OptionValueIsArray, "Ignore a specific platform requirement (php & ext- packages).", nil),
		console.MustOption("ignore-platform-reqs", "", console.OptionValueNone, "Ignore all platform requirements (php & ext- packages).", nil),
		optionWithSuggestFunc("type", "", console.OptionValueRequired|console.OptionValueIsArray, "Filter packages to reinstall by type(s)", nil, c.SuggestInstalledPackageTypes(false)),
		console.MustArgument("packages", console.ArgumentIsArray, "List of package names to reinstall, can include a wildcard (*) to match any substring.", nil).WithSuggestFunc(c.SuggestInstalledPackage(false, false)),
	)
	c.SetHelp(`The <info>reinstall</info> command looks up installed packages by name,
uninstalls them and reinstalls them. This lets you do a clean install
of a package if you messed with its files, or if you wish to change
the installation type using --prefer-install.

<info>php composer.phar reinstall acme/foo "acme/bar-*"</info>

Read more at https://getcomposer.org/doc/03-cli.md#reinstall`)

	return c
}

// PHPClass implements php.Classer.
func (*ReinstallCommand) PHPClass() string { return `Composer\Command\ReinstallCommand` }

// Execute ports execute().
func (c *ReinstallCommand) Execute(in console.Input, out console.Output) (int, error) {
	cio := c.IO()

	comp, err := c.RequireComposer(nil, nil)
	if err != nil {
		return 0, err
	}

	localRepo := comp.RepositoryManager().LocalRepository()
	var packagesToReinstall []pkg.PackageInterface
	var packageNamesToReinstall []string
	canonical, err := localRepo.CanonicalPackages()
	if err != nil {
		return 0, err
	}
	if types := console.StringsOption(in, "type"); len(types) > 0 {
		if len(console.StringsArgument(in, "packages")) > 0 {
			return 0, NewError(ClassInvalidArgument, "You cannot specify package names and filter by type at the same time.")
		}
		for _, p := range canonical {
			if slices.Contains(types, p.Type()) {
				packagesToReinstall = append(packagesToReinstall, p)
				packageNamesToReinstall = append(packageNamesToReinstall, p.Name())
			}
		}
	} else {
		patterns := console.StringsArgument(in, "packages")
		if len(patterns) == 0 {
			return 0, NewError(ClassInvalidArgument, "You must pass one or more package names to be reinstalled.")
		}
		for _, pattern := range patterns {
			patternRegexp := pkg.PackageNameToRegexp(pattern, "{^%s$}i")
			matched := false
			for _, p := range canonical {
				ok, err := php.PregIsMatch(patternRegexp, p.Name())
				if err != nil {
					return 0, err
				}
				if ok {
					matched = true
					packagesToReinstall = append(packagesToReinstall, p)
					packageNamesToReinstall = append(packageNamesToReinstall, p.Name())
				}
			}

			if !matched {
				cio.WriteError(`<warning>Pattern "`+pattern+`" does not match any currently installed packages.</warning>`, true, io.Normal)
			}
		}
	}

	if len(packagesToReinstall) == 0 {
		cio.WriteError("<warning>Found no packages to reinstall, aborting.</warning>", true, io.Normal)

		return 1, nil
	}

	uninstallOperations := make([]*operation.UninstallOperation, 0, len(packagesToReinstall))
	for _, p := range packagesToReinstall {
		uninstallOperations = append(uninstallOperations, operation.NewUninstallOperation(p))
	}

	// make sure we have a list of install operations ordered by dependency/plugins
	resultPackages, err := localRepo.Packages()
	if err != nil {
		return 0, err
	}
	presentPackages := make([]pkg.PackageInterface, 0, len(resultPackages))
	for _, p := range resultPackages {
		if !slices.Contains(packageNamesToReinstall, p.Name()) {
			presentPackages = append(presentPackages, p)
		}
	}
	transaction := resolver.NewTransaction(presentPackages, resultPackages)
	installOperations := transaction.Operations()

	// reverse-sort the uninstalls based on the install order
	installOrder := map[string]int{}
	for index, op := range installOperations {
		if install, ok := op.(*operation.InstallOperation); ok {
			if _, isAlias := install.Package().(pkg.Alias); !isAlias {
				installOrder[install.Package().Name()] = index
			}
		}
	}
	php.SortSlice(uninstallOperations, func(a, b *operation.UninstallOperation) int {
		return installOrder[b.Package().Name()] - installOrder[a.Package().Name()]
	})

	commandEvent := eventdispatcher.NewCommandEvent(eventdispatcher.PluginCommand, "reinstall", in, out, nil, nil)
	eventDispatcher := comp.EventDispatcher()
	if _, err := eventDispatcher.Dispatch(commandEvent.Name(), commandEvent); err != nil {
		return 0, err
	}

	cfg := comp.Config()
	installPreference, err := c.PreferredInstallOptions(cfg, in, false)
	if err != nil {
		return 0, err
	}

	installationManager := comp.InstallationManager()
	downloadManager := comp.DownloadManager()
	rootPackage := comp.Package()

	setOutputProgress(installationManager, !console.BoolOption(in, "no-progress"))
	if console.BoolOption(in, "no-plugins") {
		if err := installationManager.DisablePlugins(); err != nil {
			return 0, err
		}
	}

	downloadManager.SetInstallPreference(installPreference)

	devMode, ok := localRepo.DevMode()
	if !ok {
		devMode = true
	}

	util.PutEnv("COMPOSER_DEV_MODE", map[bool]string{true: "1", false: "0"}[devMode])
	// dispatchScript(string $eventName, bool $devMode) rejects an
	// installed.json "dev" that is not a bool (strict_types)
	if r, ok := localRepo.(interface{ DevModeValue() any }); ok && r.DevModeValue() != nil {
		return 0, pkg.ArgumentTypeError(`Composer\EventDispatcher\EventDispatcher::dispatchScript`, 2, "devMode", "bool", r.DevModeValue())
	}
	if _, err := eventDispatcher.DispatchScript(script.PreInstallCmd, devMode, nil, nil); err != nil {
		return 0, err
	}

	uninstalls := make([]operation.Operation, len(uninstallOperations))
	for i, op := range uninstallOperations {
		uninstalls[i] = op
	}
	if err := installationManager.Execute(localRepo, uninstalls, devMode, true, false); err != nil {
		return 0, err
	}
	if err := installationManager.Execute(localRepo, installOperations, devMode, true, false); err != nil {
		return 0, err
	}
	// the package store's upkeep, once everything else is done
	defer downloadManager.MaintainStore()

	if !console.BoolOption(in, "no-autoloader") {
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

		generator := comp.AutoloadGenerator()
		generator.SetClassMapAuthoritative(authoritative)
		generator.SetApcu(apcu, apcuPrefix)
		platformFilter, err := c.PlatformRequirementFilter(in)
		if err != nil {
			return 0, err
		}
		generator.SetPlatformRequirementFilter(platformFilter)
		if _, err := dumpAutoloader(generator, cfg, localRepo, rootPackage, installationManager, optimize, comp.Locker(), false); err != nil {
			return 0, err
		}
	}

	if _, err := eventDispatcher.DispatchScript(script.PostInstallCmd, devMode, nil, nil); err != nil {
		return 0, err
	}

	return 0, nil
}
