// Ports src/Composer/Command/RemoveCommand.php.

package command

import (
	"os"
	"strings"

	"github.com/stubbedev/maestro/internal/advisory"
	"github.com/stubbedev/maestro/internal/composer"
	"github.com/stubbedev/maestro/internal/config"
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/eventdispatcher"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/json"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/resolver"
)

func init() {
	registerCommand(OrderRemove, func() console.Commander { return NewRemoveCommand() })
}

// RemoveCommand is Composer\Command\RemoveCommand.
type RemoveCommand struct{ *BaseCommand }

// ClassName implements console.ClassNamer.
func (*RemoveCommand) ClassName() string { return `Composer\Command\RemoveCommand` }

// NewRemoveCommand ports new RemoveCommand() (configure()).
func NewRemoveCommand() *RemoveCommand {
	c := &RemoveCommand{BaseCommand: NewBaseCommand("")}
	c.SetImpl(c)
	c.SetName("remove").
		SetAliases("rm", "uninstall").
		SetDescription("Removes a package from the require or require-dev").
		SetDefinitionItems(
			console.MustArgument("packages", console.ArgumentIsArray, "Packages that should be removed.", nil).WithSuggestFunc(c.SuggestRootRequirement()),
			console.MustOption("dev", "", console.OptionValueNone, "Removes a package from the require-dev section.", nil),
			console.MustOption("dry-run", "", console.OptionValueNone, "Outputs the operations but will not execute anything (implicitly enables --verbose).", nil),
			console.MustOption("no-progress", "", console.OptionValueNone, "Do not output download progress.", nil),
			console.MustOption("no-update", "", console.OptionValueNone, "Disables the automatic update of the dependencies (implies --no-install).", nil),
			console.MustOption("no-install", "", console.OptionValueNone, "Skip the install step after updating the composer.lock file.", nil),
			console.MustOption("no-audit", "", console.OptionValueNone, "Skip the audit step after updating the composer.lock file (can also be set via the COMPOSER_NO_AUDIT=1 env var).", nil),
			optionWithSuggestions("audit-format", "", console.OptionValueRequired, `Audit output format. Must be "table", "plain", "json", or "summary".`, advisory.FormatSummary, advisory.Formats[:]...),
			console.MustOption("no-security-blocking", "", console.OptionValueNone, "DEPRECATED: use --no-blocking instead. Allows installing packages with security advisories or that are abandoned (can also be set via the COMPOSER_NO_SECURITY_BLOCKING=1 env var).", nil),
			console.MustOption("no-blocking", "", console.OptionValueNone, "Disables all policy blocking during this command (can also be set via the COMPOSER_NO_BLOCKING=1 env var).", nil),
			console.MustOption("update-no-dev", "", console.OptionValueNone, "Run the dependency update with the --no-dev option.", nil),
			console.MustOption("update-with-dependencies", "w", console.OptionValueNone, "Allows inherited dependencies to be updated with explicit dependencies (can also be set via the COMPOSER_WITH_DEPENDENCIES=1 env var). (Deprecated, is now default behavior)", nil),
			console.MustOption("update-with-all-dependencies", "W", console.OptionValueNone, "Allows all inherited dependencies to be updated, including those that are root requirements (can also be set via the COMPOSER_WITH_ALL_DEPENDENCIES=1 env var).", nil),
			console.MustOption("with-all-dependencies", "", console.OptionValueNone, "Alias for --update-with-all-dependencies", nil),
			console.MustOption("no-update-with-dependencies", "", console.OptionValueNone, "Does not allow inherited dependencies to be updated with explicit dependencies.", nil),
			console.MustOption("minimal-changes", "m", console.OptionValueNone, "During an update with -w/-W, only perform absolutely necessary changes to transitive dependencies (can also be set via the COMPOSER_MINIMAL_CHANGES=1 env var).", nil),
			console.MustOption("unused", "", console.OptionValueNone, "Remove all packages which are locked but not required by any other package.", nil),
			console.MustOption("ignore-platform-req", "", console.OptionValueRequired|console.OptionValueIsArray, "Ignore a specific platform requirement (php & ext- packages).", nil),
			console.MustOption("ignore-platform-reqs", "", console.OptionValueNone, "Ignore all platform requirements (php & ext- packages).", nil),
			console.MustOption("optimize-autoloader", "o", console.OptionValueNone, "Optimize autoloader during autoloader dump", nil),
			console.MustOption("classmap-authoritative", "a", console.OptionValueNone, "Autoload classes from the classmap only. Implicitly enables `--optimize-autoloader`.", nil),
			console.MustOption("apcu-autoloader", "", console.OptionValueNone, "Use APCu to cache found/not-found classes.", nil),
			console.MustOption("apcu-autoloader-prefix", "", console.OptionValueRequired, "Use a custom prefix for the APCu autoloader cache. Implicitly enables --apcu-autoloader", nil),
		).
		SetHelp(`The <info>remove</info> command removes a package from the current
list of installed packages

<info>php composer.phar remove</info>

Read more at https://getcomposer.org/doc/03-cli.md#remove-rm`)

	return c
}

// Execute ports execute().
func (c *RemoveCommand) Execute(in console.Input, out console.Output) (int, error) {
	packages := console.StringsArgument(in, "packages")
	if len(packages) == 0 && !console.BoolOption(in, "unused") {
		return 0, &console.Error{Kind: console.KindInvalidArgument, Message: `Not enough arguments (missing: "packages").`}
	}

	for i, p := range packages {
		packages[i] = php.Strtolower(p)
	}

	if console.BoolOption(in, "unused") {
		comp, err := c.RequireComposer(nil, nil)
		if err != nil {
			return 0, err
		}
		locker := comp.Locker()
		locked, err := locker.IsLocked()
		if err != nil {
			return 0, err
		}
		if !locked {
			return 0, NewError(ClassUnexpectedValue, "A valid composer.lock file is required to run this command with --unused")
		}

		lockedRepo, err := locker.LockedRepository(false)
		if err != nil {
			return 0, err
		}
		lockedPackages, err := lockedRepo.Packages()
		if err != nil {
			return 0, err
		}

		required := map[string]bool{}
		root := comp.Package()
		for _, links := range []pkg.Links{root.Requires(), root.DevRequires()} {
			for link := range links.Values() {
				required[link.Target()] = true
			}
		}

		removed := make([]bool, len(lockedPackages))
		for found := true; found; {
			found = false
			for index, p := range lockedPackages {
				if removed[index] {
					continue
				}
				for _, name := range p.Names(true) {
					if required[name] {
						for link := range p.Requires().Values() {
							required[link.Target()] = true
						}
						found = true
						removed[index] = true

						break
					}
				}
			}
		}

		for index, p := range lockedPackages {
			if !removed[index] {
				packages = append(packages, p.Name())
			}
		}

		if len(packages) == 0 {
			c.IO().WriteError("<info>No unused packages to remove</info>", true, io.Normal)

			return 0, nil
		}
	}

	file, err := composer.GetComposerFile()
	if err != nil {
		return 0, err
	}

	jsonFile, err := json.NewFile(file, nil, nil)
	if err != nil {
		return 0, err
	}
	read, err := jsonFile.Read()
	if err != nil {
		return 0, err
	}
	composerJSON, _ := read.(*php.Array)
	if composerJSON == nil {
		composerJSON = php.NewArray()
	}
	composerBackup, err := os.ReadFile(jsonFile.Path())
	if err != nil {
		return 0, err
	}

	jsonSource := config.NewJSONConfigSource(jsonFile, false)

	typ := "require"
	altType := "require-dev"
	if console.BoolOption(in, "dev") {
		typ, altType = altType, typ
	}
	out2 := c.IO()

	if console.BoolOption(in, "update-with-dependencies") {
		out2.WriteError(`<warning>You are using the deprecated option "update-with-dependencies". This is now default behaviour. The --no-update-with-dependencies option can be used to remove a package without its dependencies.</warning>`, true, io.Normal)
	}

	// make sure name checks are done case insensitively
	links := map[string]*php.Array{}
	for _, linkType := range []string{"require", "require-dev"} {
		v, _ := composerJSON.Get(linkType)
		if a, ok := v.(*php.Array); ok {
			a = a.Clone()
			for k := range a.All() {
				name := k.String()
				a.Set(php.Strtolower(name), name)
			}
			links[linkType] = a
		}
	}
	lookup := func(linkType, name string) (string, bool) {
		a := links[linkType]
		if a == nil {
			return "", false
		}
		v, ok := a.Get(name)
		if !ok || v == nil {
			return "", false
		}

		return php.ToString(v), true
	}
	grep := func(linkType, name string) ([]string, error) {
		a := links[linkType]
		if a == nil {
			return nil, nil
		}
		keys := php.NewArray()
		for k := range a.All() {
			keys.Append(k.String())
		}
		matches, err := php.PregGrep(pkg.PackageNameToRegexp(name, "{^%s$}i"), keys, 0)
		if err != nil {
			return nil, err
		}
		var result []string
		for _, m := range matches.Values() {
			result = append(result, php.ToString(m))
		}

		return result, nil
	}

	dryRun := console.BoolOption(in, "dry-run")
	toRemove := map[string][]string{}
	remove := func(linkType, name string) error {
		if dryRun {
			toRemove[linkType] = append(toRemove[linkType], name)

			return nil
		}

		return jsonSource.RemoveLink(linkType, name)
	}
	confirmAlt := func(name string) error {
		out2.WriteError("<warning>"+name+" could not be found in "+typ+" but it is present in "+altType+"</warning>", true, io.Normal)
		if out2.IsInteractive() {
			ok, err := out2.AskConfirmation("Do you want to remove it from "+altType+" [<comment>yes</comment>]? ", true)
			if err != nil {
				return err
			}
			if ok {
				return remove(altType, name)
			}
		}

		return nil
	}
	for _, p := range packages {
		if name, ok := lookup(typ, p); ok {
			if err := remove(typ, name); err != nil {
				return 0, err
			}

			continue
		}
		if name, ok := lookup(altType, p); ok {
			if err := confirmAlt(name); err != nil {
				return 0, err
			}

			continue
		}
		matches, err := grep(typ, p)
		if err != nil {
			return 0, err
		}
		if len(matches) > 0 {
			for _, m := range matches {
				if err := remove(typ, m); err != nil {
					return 0, err
				}
			}

			continue
		}
		if matches, err = grep(altType, p); err != nil {
			return 0, err
		}
		if len(matches) > 0 {
			for _, m := range matches {
				if err := confirmAlt(m); err != nil {
					return 0, err
				}
			}

			continue
		}
		out2.WriteError("<warning>"+p+" is not required in your composer.json and has not been removed</warning>", true, io.Normal)
	}

	out2.WriteError("<info>"+file+" has been updated</info>", true, io.Normal)

	if console.BoolOption(in, "no-update") {
		return 0, nil
	}

	comp, err := c.TryComposer(nil, nil)
	if err != nil {
		return 0, err
	}
	if comp != nil {
		if err := deactivateInstalledPlugins(comp); err != nil {
			return 0, err
		}
	}

	// Update packages
	if err := c.ResetComposer(); err != nil {
		return 0, err
	}
	comp, err = c.RequireComposer(nil, nil)
	if err != nil {
		return 0, err
	}

	if dryRun {
		root := comp.Package()
		req, dev := root.Requires(), root.DevRequires()
		for _, name := range toRemove["require"] {
			req = req.Without(name)
		}
		for _, name := range toRemove["require-dev"] {
			dev = dev.Without(name)
		}
		root.SetRequires(req)
		root.SetDevRequires(dev)
	}

	commandEvent := eventdispatcher.NewCommandEvent(eventdispatcher.PluginCommand, "remove", in, out, nil, nil)
	if _, err := comp.EventDispatcher().Dispatch(commandEvent.Name(), commandEvent); err != nil {
		return 0, err
	}

	cfg := comp.Config()
	allowPlugins, err := cfg.Get("allow-plugins", 0)
	if err != nil {
		return 0, err
	}
	if ap, ok := allowPlugins.(*php.Array); ok && !dryRun {
		var removedPlugins []string
		for k := range ap.All() {
			for _, p := range packages {
				if k.String() == p {
					removedPlugins = append(removedPlugins, p)

					break
				}
			}
		}
		if len(removedPlugins) > 0 {
			if ap.Len() == len(removedPlugins) {
				if err := jsonSource.RemoveConfigSetting("allow-plugins"); err != nil {
					return 0, err
				}
			} else {
				for _, plugin := range removedPlugins {
					if err := jsonSource.RemoveConfigSetting("allow-plugins." + plugin); err != nil {
						return 0, err
					}
				}
			}
		}
	}

	setOutputProgress(comp.InstallationManager(), !console.BoolOption(in, "no-progress"))

	install, err := composer.CreateInstaller(out2, comp)
	if err != nil {
		return 0, err
	}

	updateDevMode := !console.BoolOption(in, "update-no-dev")
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

	updateAllowTransitiveDependencies := resolver.UpdateListedWithTransitiveDepsNoRootRequire
	flags := ""
	if console.BoolOption(in, "update-with-all-dependencies") || console.BoolOption(in, "with-all-dependencies") {
		updateAllowTransitiveDependencies = resolver.UpdateListedWithTransitiveDeps
		flags += " --with-all-dependencies"
	} else if console.BoolOption(in, "no-update-with-dependencies") {
		updateAllowTransitiveDependencies = resolver.UpdateOnlyListed
		flags += " --with-dependencies"
	}

	out2.WriteError("<info>Running composer update "+strings.Join(packages, " ")+flags+"</info>", true, io.Normal)

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
		SetVerbose(console.BoolOption(in, "verbose")).
		SetDevMode(updateDevMode).
		SetOptimizeAutoloader(optimize).
		SetClassMapAuthoritative(authoritative).
		SetApcuAutoloader(apcu, apcuPrefix).
		SetUpdate(true).
		SetInstall(!console.BoolOption(in, "no-install"))
	if _, err := install.SetUpdateAllowTransitiveDependencies(updateAllowTransitiveDependencies); err != nil {
		return 0, err
	}
	install.
		SetPlatformRequirementFilter(platformFilter).
		SetDryRun(dryRun).
		SetPolicyConfig(policyConfig).
		SetAuditConfig(auditConfig).
		SetMinimalUpdate(minimalChanges)

	// if no lock is present, we do not do a partial update as
	// this is not supported by the Installer
	locked, err := comp.Locker().IsLocked()
	if err != nil {
		return 0, err
	}
	if locked {
		install.SetUpdateAllowList(packages)
	}

	status, err := install.Run()
	if err != nil {
		return 0, err
	}
	if status != 0 {
		out2.WriteError("\n<error>Removal failed, reverting "+file+" to its original content.</error>", true, io.Normal)
		if err := os.WriteFile(jsonFile.Path(), composerBackup, 0o666); err != nil {
			return 0, err
		}
	}

	if !dryRun {
		for _, p := range packages {
			found, err := comp.RepositoryManager().LocalRepository().FindPackages(p, nil)
			if err != nil {
				return 0, err
			}
			if len(found) > 0 {
				out2.WriteError("<error>Removal failed, "+p+" is still present, it may be required by another package. See `composer why "+p+"`.</error>", true, io.Normal)

				return 2, nil
			}
		}
	}

	return status, nil
}
