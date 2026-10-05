// Ports src/Composer/Command/InstallCommand.php.

package command

import (
	"strings"

	"github.com/stubbedev/maestro/internal/advisory"
	"github.com/stubbedev/maestro/internal/composer"
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/eventdispatcher"
	"github.com/stubbedev/maestro/internal/io"
)

func init() {
	registerCommand(OrderInstall, func() console.Commander { return NewInstallCommand() })
}

// InstallCommand is Composer\Command\InstallCommand.
type InstallCommand struct{ *BaseCommand }

// NewInstallCommand ports new InstallCommand() (configure()).
func NewInstallCommand() *InstallCommand {
	c := &InstallCommand{BaseCommand: NewBaseCommand("")}
	c.SetImpl(c)
	c.SetName("install")
	c.SetAliases("i")
	c.SetDescription("Installs the project dependencies from the composer.lock file if present, or falls back on the composer.json")
	c.SetDefinitionItems(
		console.MustOption("prefer-source", "", console.OptionValueNone, "Forces installation from package sources when possible, including VCS information.", nil),
		console.MustOption("prefer-dist", "", console.OptionValueNone, "Forces installation from package dist (default behavior).", nil),
		optionWithSuggestions("prefer-install", "", console.OptionValueRequired, "Forces installation from package dist|source|auto (auto chooses source for dev versions, dist for the rest).", nil, SuggestPreferInstall()...),
		console.MustOption("dry-run", "", console.OptionValueNone, "Outputs the operations but will not execute anything (implicitly enables --verbose).", nil),
		console.MustOption("download-only", "", console.OptionValueNone, "Download only, do not install packages.", nil),
		console.MustOption("dev", "", console.OptionValueNone, "DEPRECATED: Enables installation of require-dev packages (enabled by default, only present for BC).", nil),
		console.MustOption("no-suggest", "", console.OptionValueNone, "DEPRECATED: This flag does not exist anymore.", nil),
		console.MustOption("no-dev", "", console.OptionValueNone, "Disables installation of require-dev packages.", nil),
		console.MustOption("no-security-blocking", "", console.OptionValueNone, "DEPRECATED: use --no-blocking instead. Allows installing packages with security advisories or that are abandoned (can also be set via the COMPOSER_NO_SECURITY_BLOCKING=1 env var).", nil),
		console.MustOption("no-blocking", "", console.OptionValueNone, "Disables all policy blocking during this command (can also be set via the COMPOSER_NO_BLOCKING=1 env var).", nil),
		console.MustOption("no-autoloader", "", console.OptionValueNone, "Skips autoloader generation", nil),
		console.MustOption("no-progress", "", console.OptionValueNone, "Do not output download progress.", nil),
		console.MustOption("no-install", "", console.OptionValueNone, "Do not use, only defined here to catch misuse of the install command.", nil),
		console.MustOption("audit", "", console.OptionValueNone, "Run an audit after installation is complete.", nil),
		optionWithSuggestions("audit-format", "", console.OptionValueRequired, `Audit output format. Must be "table", "plain", "json", or "summary".`, advisory.FormatSummary, advisory.Formats[:]...),
		console.MustOption("verbose", "v|vv|vvv", console.OptionValueNone, "Shows more details including new commits pulled in when updating packages.", nil),
		console.MustOption("optimize-autoloader", "o", console.OptionValueNone, "Optimize autoloader during autoloader dump", nil),
		console.MustOption("classmap-authoritative", "a", console.OptionValueNone, "Autoload classes from the classmap only. Implicitly enables `--optimize-autoloader`.", nil),
		console.MustOption("strict-psr-autoloader", "", console.OptionValueNone, "Return a failed status code (6) if PSR-4 or PSR-0 mapping errors are present. Requires --optimize-autoloader to work.", nil),
		console.MustOption("apcu-autoloader", "", console.OptionValueNone, "Use APCu to cache found/not-found classes.", nil),
		console.MustOption("apcu-autoloader-prefix", "", console.OptionValueRequired, "Use a custom prefix for the APCu autoloader cache. Implicitly enables --apcu-autoloader", nil),
		console.MustOption("ignore-platform-req", "", console.OptionValueRequired|console.OptionValueIsArray, "Ignore a specific platform requirement (php & ext- packages).", nil),
		console.MustOption("ignore-platform-reqs", "", console.OptionValueNone, "Ignore all platform requirements (php & ext- packages).", nil),
		console.MustArgument("packages", console.ArgumentIsArray|console.ArgumentOptional, "Should not be provided, use composer require instead to add a given package to composer.json.", nil),
	)
	c.SetHelp(`The <info>install</info> command reads the composer.lock file from
the current directory, processes it, and downloads and installs all the
libraries and dependencies outlined in that file. If the file does not
exist it will look for composer.json and do the same.

<info>php composer.phar install</info>

Read more at https://getcomposer.org/doc/03-cli.md#install-i`)

	return c
}

// ClassName implements console.ClassNamer.
func (*InstallCommand) ClassName() string { return `Composer\Command\InstallCommand` }

// Execute ports execute().
func (c *InstallCommand) Execute(in console.Input, out console.Output) (int, error) {
	cio := c.IO()
	if console.BoolOption(in, "dev") {
		cio.WriteError(`<warning>You are using the deprecated option "--dev". It has no effect and will break in Composer 3.</warning>`, true, io.Normal)
	}
	if console.BoolOption(in, "no-suggest") {
		cio.WriteError(`<warning>You are using the deprecated option "--no-suggest". It has no effect and will break in Composer 3.</warning>`, true, io.Normal)
	}

	if args := console.StringsArgument(in, "packages"); len(args) > 0 {
		joined := strings.Join(args, " ")
		cio.WriteError(`<error>Invalid argument `+joined+`. Use "composer require `+joined+`" instead to add packages to your composer.json.</error>`, true, io.Normal)

		return 1, nil
	}

	if console.BoolOption(in, "no-install") {
		cio.WriteError(`<error>Invalid option "--no-install". Use "composer update --no-install" instead if you are trying to update the composer.lock file.</error>`, true, io.Normal)

		return 1, nil
	}

	c2, err := c.RequireComposer(nil, nil)
	if err != nil {
		return 0, err
	}

	// HttpDownloader::isCurlEnabled() is always true: maestro's HTTP
	// client is native, so the "PHP curl extension" warning never shows.

	commandEvent := eventdispatcher.NewCommandEvent(eventdispatcher.PluginCommand, "install", in, out, nil, nil)
	if _, err := c2.EventDispatcher().Dispatch(commandEvent.Name(), commandEvent); err != nil {
		return 0, err
	}

	install, err := composer.CreateInstaller(cio, c2)
	if err != nil {
		return 0, err
	}

	cfg := c2.Config()
	preferSource, preferDist, err := c.PreferredInstallOptions(cfg, in, false)
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

	if console.BoolOption(in, "strict-psr-autoloader") && !optimize && !authoritative {
		return 0, NewError(ClassInvalidArgument, "InstallCommand.php", 125, "--strict-psr-autoloader mode only works with optimized autoloader, use --optimize-autoloader or --classmap-authoritative if you want a strict return value.")
	}

	setOutputProgress(c2.InstallationManager(), !console.BoolOption(in, "no-progress"))

	platformFilter, err := c.PlatformRequirementFilter(in)
	if err != nil {
		return 0, err
	}
	policyConfig, err := c.CreatePolicyConfig(c2.Config(), in)
	if err != nil {
		return 0, err
	}
	auditConfig, err := c.CreateAuditConfig(in)
	if err != nil {
		return 0, err
	}

	install.
		SetDryRun(console.BoolOption(in, "dry-run")).
		SetDownloadOnly(console.BoolOption(in, "download-only")).
		SetVerbose(console.BoolOption(in, "verbose")).
		SetPreferSource(preferSource).
		SetPreferDist(preferDist).
		SetDevMode(!console.BoolOption(in, "no-dev")).
		SetDumpAutoloader(!console.BoolOption(in, "no-autoloader")).
		SetOptimizeAutoloader(optimize).
		SetClassMapAuthoritative(authoritative).
		SetStrictPsrAutoloader(console.BoolOption(in, "strict-psr-autoloader")).
		SetApcuAutoloader(apcu, apcuPrefix).
		SetPlatformRequirementFilter(platformFilter).
		SetPolicyConfig(policyConfig).
		SetAuditConfig(auditConfig).
		SetErrorOnAudit(console.BoolOption(in, "audit"))

	if console.BoolOption(in, "no-plugins") {
		if _, err := install.DisablePlugins(); err != nil {
			return 0, err
		}
	}

	return install.Run()
}
