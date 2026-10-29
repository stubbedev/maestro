// Ports src/Composer/Command/SuggestsCommand.php.

package command

import (
	"slices"
	"strings"

	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/installer"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/repository"
)

func init() {
	registerCommand(OrderSuggests, func() console.Commander { return NewSuggestsCommand() })
}

// SuggestsCommand is Composer\Command\SuggestsCommand.
type SuggestsCommand struct{ *BaseCommand }

// NewSuggestsCommand ports new SuggestsCommand() (configure()).
func NewSuggestsCommand() *SuggestsCommand {
	c := &SuggestsCommand{BaseCommand: NewBaseCommand("")}
	c.SetImpl(c)
	c.SetName("suggests")
	c.SetDescription("Shows package suggestions")
	c.SetDefinitionItems(
		console.MustOption("by-package", "", console.OptionValueNone, "Groups output by suggesting package (default)", nil),
		console.MustOption("by-suggestion", "", console.OptionValueNone, "Groups output by suggested package", nil),
		console.MustOption("all", "a", console.OptionValueNone, "Show suggestions from all dependencies, including transitive ones", nil),
		console.MustOption("list", "", console.OptionValueNone, "Show only list of suggested package names", nil),
		console.MustOption("no-dev", "", console.OptionValueNone, "Exclude suggestions from require-dev packages", nil),
		console.MustArgument("packages", console.ArgumentIsArray|console.ArgumentOptional, "Packages that you want to list suggestions from.", nil).WithSuggestFunc(c.SuggestInstalledPackage(true, false)),
	)
	c.SetHelp(`
The <info>%command.name%</info> command shows a sorted list of suggested packages.

Read more at https://getcomposer.org/doc/03-cli.md#suggests`)

	return c
}

// PHPClass implements php.Classer.
func (*SuggestsCommand) PHPClass() string { return `Composer\Command\SuggestsCommand` }

// Execute ports execute().
func (c *SuggestsCommand) Execute(in console.Input, _ console.Output) (int, error) {
	comp, err := c.RequireComposer(nil, nil)
	if err != nil {
		return 0, err
	}

	root, _ := pkg.Clone(comp.Package()).(pkg.RootPackageInterface)
	rootRepo, err := repository.NewRootPackageRepository(root)
	if err != nil {
		return 0, err
	}
	installedRepos := []repository.RepositoryInterface{rootRepo}

	locker := comp.Locker()
	locked, err := locker.IsLocked()
	if err != nil {
		return 0, err
	}
	if locked {
		overrides, err := locker.PlatformOverrides()
		if err != nil {
			return 0, err
		}
		platformRepo, err := c.newPlatformRepository(overrides)
		if err != nil {
			return 0, err
		}
		lockedRepo, err := locker.LockedRepository(!console.BoolOption(in, "no-dev"))
		if err != nil {
			return 0, err
		}
		installedRepos = append(installedRepos, platformRepo, lockedRepo)
	} else {
		v, err := comp.Config().Get("platform", 0)
		if err != nil {
			return 0, err
		}
		overrides, _ := v.(*php.Array)
		platformRepo, err := c.newPlatformRepository(overrides)
		if err != nil {
			return 0, err
		}
		installedRepos = append(installedRepos, platformRepo, comp.RepositoryManager().LocalRepository())
	}

	installedRepo, err := repository.NewInstalledRepository(installedRepos)
	if err != nil {
		return 0, err
	}

	filter := console.StringsArgument(in, "packages")
	normalizedFilter := make([]string, len(filter))
	for i, f := range filter {
		normalizedFilter[i] = php.Strtolower(f)
	}
	validationRepo := installedRepo
	if locked && console.BoolOption(in, "no-dev") {
		lockedRepo, err := locker.LockedRepository(true)
		if err != nil {
			return 0, err
		}
		validationRepo, err = repository.NewInstalledRepository([]repository.RepositoryInterface{lockedRepo})
		if err != nil {
			return 0, err
		}
	}

	var missingPackages, excludedPackages []string
	for index, name := range normalizedFilter {
		found, err := installedRepo.FindPackages(name, nil)
		if err != nil {
			return 0, err
		}
		if len(found) != 0 {
			continue
		}

		found, err = validationRepo.FindPackages(name, nil)
		if err != nil {
			return 0, err
		}
		if len(found) != 0 {
			excludedPackages = append(excludedPackages, filter[index])
		} else {
			missingPackages = append(missingPackages, filter[index])
		}
	}

	var errs []string
	if len(missingPackages) != 0 {
		if len(missingPackages) == 1 {
			errs = append(errs, `Package "`+missingPackages[0]+`" is not installed.`)
		} else {
			errs = append(errs, `Packages "`+strings.Join(missingPackages, `", "`)+`" are not installed.`)
		}
	}
	if len(excludedPackages) != 0 {
		if len(excludedPackages) == 1 {
			errs = append(errs, `Package "`+excludedPackages[0]+`" is excluded by --no-dev.`)
		} else {
			errs = append(errs, `Packages "`+strings.Join(excludedPackages, `", "`)+`" are excluded by --no-dev.`)
		}
	}
	if len(errs) != 0 {
		for _, e := range errs {
			c.IO().WriteError("<error>"+e+"</error>", true, io.Normal)
		}

		return 1, nil
	}

	reporter := installer.NewSuggestedPackagesReporter(c.IO())
	packages, err := installedRepo.Packages()
	if err != nil {
		return 0, err
	}
	packages = append(slices.Clone(packages), comp.Package())
	for _, p := range packages {
		if len(normalizedFilter) > 0 && !slices.Contains(normalizedFilter, p.Name()) {
			continue
		}

		reporter.AddSuggestionsFromPackage(p)
	}

	// Determine output mode, default is by-package
	mode := installer.ModeByPackage

	// if by-suggestion is given we override the default
	if console.BoolOption(in, "by-suggestion") {
		mode = installer.ModeBySuggestion
	}
	// unless by-package is also present then we enable both
	if console.BoolOption(in, "by-package") {
		mode |= installer.ModeByPackage
	}
	// list is exclusive and overrides everything else
	if console.BoolOption(in, "list") {
		mode = installer.ModeList
	}

	var onlyDependentsOf pkg.PackageInterface
	if len(normalizedFilter) == 0 && !console.BoolOption(in, "all") {
		onlyDependentsOf = comp.Package()
	}
	if err := reporter.Output(mode, installedRepo, onlyDependentsOf); err != nil {
		return 0, err
	}

	return 0, nil
}
