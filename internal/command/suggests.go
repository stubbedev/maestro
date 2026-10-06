// Ports src/Composer/Command/SuggestsCommand.php.

package command

import (
	"slices"

	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/installer"
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

// ClassName implements console.ClassNamer.
func (*SuggestsCommand) ClassName() string { return `Composer\Command\SuggestsCommand` }

// Execute ports execute().
func (c *SuggestsCommand) Execute(in console.Input, _ console.Output) (int, error) {
	comp, err := c.requireComposerAt("SuggestsCommand.php", 54)
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
	reporter := installer.NewSuggestedPackagesReporter(c.IO())

	filter := console.StringsArgument(in, "packages")
	packages, err := installedRepo.Packages()
	if err != nil {
		return 0, err
	}
	packages = append(slices.Clone(packages), comp.Package())
	for _, p := range packages {
		if len(filter) > 0 && !slices.Contains(filter, p.Name()) {
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
	if len(filter) == 0 && !console.BoolOption(in, "all") {
		onlyDependentsOf = comp.Package()
	}
	if err := reporter.Output(mode, installedRepo, onlyDependentsOf); err != nil {
		return 0, err
	}

	return 0, nil
}
