// Ports src/Composer/Command/DumpAutoloadCommand.php.

package command

import (
	"os"
	"strconv"

	"github.com/stubbedev/maestro/internal/autoload"
	"github.com/stubbedev/maestro/internal/classmap"
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/eventdispatcher"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/locker"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/repository"
)

func init() {
	registerCommand(OrderDumpAutoload, func() console.Commander { return NewDumpAutoloadCommand() })
}

// DumpAutoloadCommand is Composer\Command\DumpAutoloadCommand.
type DumpAutoloadCommand struct{ *BaseCommand }

// NewDumpAutoloadCommand ports new DumpAutoloadCommand() (configure()).
func NewDumpAutoloadCommand() *DumpAutoloadCommand {
	c := &DumpAutoloadCommand{BaseCommand: NewBaseCommand("")}
	c.SetImpl(c)
	c.SetName("dump-autoload")
	c.SetAliases("dumpautoload")
	c.SetDescription("Dumps the autoloader")
	c.SetDefinitionItems(
		console.MustOption("optimize", "o", console.OptionValueNone, "Optimizes PSR0 and PSR4 packages to be loaded with classmaps too, good for production.", nil),
		console.MustOption("classmap-authoritative", "a", console.OptionValueNone, "Autoload classes from the classmap only. Implicitly enables `--optimize`.", nil),
		console.MustOption("apcu", "", console.OptionValueNone, "Use APCu to cache found/not-found classes.", nil),
		console.MustOption("apcu-prefix", "", console.OptionValueRequired, "Use a custom prefix for the APCu autoloader cache. Implicitly enables --apcu", nil),
		console.MustOption("dry-run", "", console.OptionValueNone, "Outputs the operations but will not execute anything.", nil),
		console.MustOption("dev", "", console.OptionValueNone, "Enables autoload-dev rules. Composer will by default infer this automatically according to the last install or update --no-dev state.", nil),
		console.MustOption("no-dev", "", console.OptionValueNone, "Disables autoload-dev rules. Composer will by default infer this automatically according to the last install or update --no-dev state.", nil),
		console.MustOption("ignore-platform-req", "", console.OptionValueRequired|console.OptionValueIsArray, "Ignore a specific platform requirement (php & ext- packages).", nil),
		console.MustOption("ignore-platform-reqs", "", console.OptionValueNone, "Ignore all platform requirements (php & ext- packages).", nil),
		console.MustOption("strict-psr", "", console.OptionValueNone, "Return a failed status code (1) if PSR-4 or PSR-0 mapping errors are present. Requires --optimize to work.", nil),
		console.MustOption("strict-ambiguous", "", console.OptionValueNone, "Return a failed status code (2) if the same class is found in multiple files. Requires --optimize to work.", nil),
	)
	c.SetHelp(`<info>php composer.phar dump-autoload</info>

Read more at https://getcomposer.org/doc/03-cli.md#dump-autoload-dumpautoload`)

	return c
}

// ClassName implements console.ClassNamer.
func (*DumpAutoloadCommand) ClassName() string { return `Composer\Command\DumpAutoloadCommand` }

// Execute ports execute().
func (c *DumpAutoloadCommand) Execute(in console.Input, out console.Output) (int, error) {
	comp, err := c.RequireComposer(nil, nil)
	if err != nil {
		return 0, err
	}

	commandEvent := eventdispatcher.NewCommandEvent(eventdispatcher.PluginCommand, "dump-autoload", in, out, nil, nil)
	if _, err := comp.EventDispatcher().Dispatch(commandEvent.Name(), commandEvent); err != nil {
		return 0, err
	}

	installationManager := comp.InstallationManager()
	localRepo := comp.RepositoryManager().LocalRepository()
	rootPackage := comp.Package()
	cfg := comp.Config()

	missingDependencies := false
	canonical, err := localRepo.CanonicalPackages()
	if err != nil {
		return 0, err
	}
	for _, localPkg := range canonical {
		installPath, ok, err := installationManager.InstallPath(localPkg)
		if err != nil {
			return 0, err
		}
		if ok {
			if _, serr := os.Stat(installPath); serr != nil {
				missingDependencies = true
				c.IO().Write(`<warning>Not all dependencies are installed. Make sure to run a "composer install" to install missing dependencies</warning>`, true, io.Normal)

				break
			}
		}
	}

	optimize, err := optionOrConfig(in, "optimize", cfg, "optimize-autoloader")
	if err != nil {
		return 0, err
	}
	authoritative, err := optionOrConfig(in, "classmap-authoritative", cfg, "classmap-authoritative")
	if err != nil {
		return 0, err
	}
	apcu, apcuPrefix, err := apcuOptions(in, cfg, "apcu", "apcu-prefix")
	if err != nil {
		return 0, err
	}

	if console.BoolOption(in, "strict-psr") && !optimize && !authoritative {
		return 0, NewError(ClassInvalidArgument, "--strict-psr mode only works with optimized autoloader, use --optimize or --classmap-authoritative if you want a strict return value.")
	}
	if console.BoolOption(in, "strict-ambiguous") && !optimize && !authoritative {
		return 0, NewError(ClassInvalidArgument, "--strict-ambiguous mode only works with optimized autoloader, use --optimize or --classmap-authoritative if you want a strict return value.")
	}

	switch {
	case authoritative:
		c.IO().Write("<info>Generating optimized autoload files (authoritative)</info>", true, io.Normal)
	case optimize:
		c.IO().Write("<info>Generating optimized autoload files</info>", true, io.Normal)
	default:
		c.IO().Write("<info>Generating autoload files</info>", true, io.Normal)
	}

	generator := comp.AutoloadGenerator()
	if console.BoolOption(in, "dry-run") {
		generator.SetDryRun(true)
	}
	if console.BoolOption(in, "no-dev") {
		generator.SetDevMode(false)
	}
	if console.BoolOption(in, "dev") {
		if console.BoolOption(in, "no-dev") {
			return 0, NewError(ClassInvalidArgument, "You can not use both --no-dev and --dev as they conflict with each other.")
		}
		generator.SetDevMode(true)
	}
	generator.SetClassMapAuthoritative(authoritative)
	generator.SetRunScripts(true)
	generator.SetApcu(apcu, apcuPrefix)
	platformFilter, err := c.PlatformRequirementFilter(in)
	if err != nil {
		return 0, err
	}
	generator.SetPlatformRequirementFilter(platformFilter)
	classMap, err := dumpAutoloader(generator, comp.Config(), localRepo, rootPackage, installationManager, optimize, comp.Locker(), console.BoolOption(in, "strict-ambiguous"))
	if err != nil {
		return 0, err
	}
	numberOfClasses := strconv.Itoa(classMap.Count())

	switch {
	case authoritative:
		c.IO().Write("<info>Generated optimized autoload files (authoritative) containing "+numberOfClasses+" classes</info>", true, io.Normal)
	case optimize:
		c.IO().Write("<info>Generated optimized autoload files containing "+numberOfClasses+" classes</info>", true, io.Normal)
	default:
		c.IO().Write("<info>Generated autoload files</info>", true, io.Normal)
	}

	if missingDependencies || (console.BoolOption(in, "strict-psr") && len(classMap.PsrViolations()) > 0) {
		return 1, nil
	}

	if console.BoolOption(in, "strict-ambiguous") {
		ambiguous, err := classMap.AmbiguousClasses(nil)
		if err != nil {
			return 0, err
		}
		if len(ambiguous) > 0 {
			return 2, nil
		}
	}

	return 0, nil
}

// dumpAutoloader is `$generator->dump($config, $localRepo, $package,
// $installationManager, 'composer', $optimize, null, $locker,
// $strictAmbiguous)`, adapting the local repository to the generator's
// interface (its getCanonicalPackages can fail here).
func dumpAutoloader(generator *autoload.Generator, cfg autoload.Config, localRepo repository.InstalledRepositoryInterface, root pkg.RootPackageInterface, im autoload.InstallationManager, optimize bool, l *locker.Locker, strictAmbiguous bool) (*classmap.ClassMap, error) {
	repo := &autoloadLocalRepo{repo: localRepo}
	var al autoload.Locker
	if l != nil {
		al = l
	}
	classMap, err := generator.Dump(cfg, repo, root, im, "composer", optimize, "", al, strictAmbiguous)
	if err != nil {
		return nil, err
	}
	if repo.err != nil {
		return nil, repo.err
	}

	return classMap, nil
}

// autoloadLocalRepo adapts the local repository to
// autoload.InstalledRepository, keeping the first getCanonicalPackages
// error for after the dump.
type autoloadLocalRepo struct {
	repo repository.InstalledRepositoryInterface
	err  error
}

func (r *autoloadLocalRepo) DevPackageNames() []string { return r.repo.DevPackageNames() }

func (r *autoloadLocalRepo) CanonicalPackages() []pkg.PackageInterface {
	packages, err := r.repo.CanonicalPackages()
	if err != nil && r.err == nil {
		r.err = err
	}

	return packages
}
