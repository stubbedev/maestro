// Ports src/Composer/Command/RequireCommand.php.

package command

import (
	"os"
	"os/signal"
	"slices"
	"strings"

	"github.com/stubbedev/maestro/internal/advisory"
	"github.com/stubbedev/maestro/internal/composer"
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/eventdispatcher"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/json"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/loader"
	"github.com/stubbedev/maestro/internal/pkg/version"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/resolver"
	"github.com/stubbedev/maestro/internal/util"
)

func init() {
	registerCommand(OrderRequire, func() console.Commander { return NewRequireCommand() })
}

// installedPluginsDeactivator is PluginManager::deactivateInstalledPlugins,
// which composer.PluginManager leaves out: the plugin runtime implements
// it; without one (composer.NoPluginManager) no plugin is active and the
// call does nothing.
type installedPluginsDeactivator interface {
	DeactivateInstalledPlugins() error
}

// deactivateInstalledPlugins ports
// $composer->getPluginManager()->deactivateInstalledPlugins().
func deactivateInstalledPlugins(c *composer.Composer) error {
	if d, ok := c.PluginManager().(installedPluginsDeactivator); ok {
		return d.DeactivateInstalledPlugins()
	}

	return nil
}

// RequireCommand is Composer\Command\RequireCommand.
type RequireCommand struct {
	*BaseCommand
	PackageDiscovery

	newlyCreated   bool
	firstRequire   bool
	json           *json.File
	file           string
	composerBackup []byte
	lock           string
	// lockBackup is nil when there was no lock file.
	lockBackup                    []byte
	dependencyResolutionCompleted bool
}

// ClassName implements console.ClassNamer.
func (*RequireCommand) ClassName() string { return `Composer\Command\RequireCommand` }

// NewRequireCommand ports new RequireCommand() (configure()).
func NewRequireCommand() *RequireCommand {
	c := &RequireCommand{BaseCommand: NewBaseCommand("")}
	c.PackageDiscovery = NewPackageDiscovery(c.BaseCommand)
	c.SetImpl(c)
	c.SetName("require").
		SetAliases("r").
		SetDescription("Adds required packages to your composer.json and installs them").
		SetDefinitionItems(
			console.MustArgument("packages", console.ArgumentIsArray|console.ArgumentOptional, `Optional package name can also include a version constraint, e.g. foo/bar or foo/bar:1.0.0 or foo/bar=1.0.0 or "foo/bar 1.0.0"`, nil).WithSuggestFunc(c.SuggestAvailablePackageInclPlatform()),
			console.MustOption("dev", "", console.OptionValueNone, "Add requirement to require-dev.", nil),
			console.MustOption("dry-run", "", console.OptionValueNone, "Outputs the operations but will not execute anything (implicitly enables --verbose).", nil),
			console.MustOption("prefer-source", "", console.OptionValueNone, "Forces installation from package sources when possible, including VCS information.", nil),
			console.MustOption("prefer-dist", "", console.OptionValueNone, "Forces installation from package dist (default behavior).", nil),
			optionWithSuggestions("prefer-install", "", console.OptionValueRequired, "Forces installation from package dist|source|auto (auto chooses source for dev versions, dist for the rest).", nil, SuggestPreferInstall()...),
			console.MustOption("fixed", "", console.OptionValueNone, "Write fixed version to the composer.json.", nil),
			console.MustOption("no-suggest", "", console.OptionValueNone, "DEPRECATED: This flag does not exist anymore.", nil),
			console.MustOption("no-progress", "", console.OptionValueNone, "Do not output download progress.", nil),
			console.MustOption("no-update", "", console.OptionValueNone, "Disables the automatic update of the dependencies (implies --no-install).", nil),
			console.MustOption("no-install", "", console.OptionValueNone, "Skip the install step after updating the composer.lock file.", nil),
			console.MustOption("no-audit", "", console.OptionValueNone, "Skip the audit step after updating the composer.lock file (can also be set via the COMPOSER_NO_AUDIT=1 env var).", nil),
			optionWithSuggestions("audit-format", "", console.OptionValueRequired, `Audit output format. Must be "table", "plain", "json", or "summary".`, advisory.FormatSummary, advisory.Formats[:]...),
			console.MustOption("no-security-blocking", "", console.OptionValueNone, "DEPRECATED: use --no-blocking instead. Allows installing packages with security advisories or that are abandoned (can also be set via the COMPOSER_NO_SECURITY_BLOCKING=1 env var).", nil),
			console.MustOption("no-blocking", "", console.OptionValueNone, "Disables all policy blocking during this command (can also be set via the COMPOSER_NO_BLOCKING=1 env var).", nil),
			console.MustOption("update-no-dev", "", console.OptionValueNone, "Run the dependency update with the --no-dev option.", nil),
			console.MustOption("update-with-dependencies", "w", console.OptionValueNone, "Allows inherited dependencies to be updated, except those that are root requirements (can also be set via the COMPOSER_WITH_DEPENDENCIES=1 env var).", nil),
			console.MustOption("update-with-all-dependencies", "W", console.OptionValueNone, "Allows all inherited dependencies to be updated, including those that are root requirements (can also be set via the COMPOSER_WITH_ALL_DEPENDENCIES=1 env var).", nil),
			console.MustOption("with-dependencies", "", console.OptionValueNone, "Alias for --update-with-dependencies", nil),
			console.MustOption("with-all-dependencies", "", console.OptionValueNone, "Alias for --update-with-all-dependencies", nil),
			console.MustOption("ignore-platform-req", "", console.OptionValueRequired|console.OptionValueIsArray, "Ignore a specific platform requirement (php & ext- packages).", nil),
			console.MustOption("ignore-platform-reqs", "", console.OptionValueNone, "Ignore all platform requirements (php & ext- packages).", nil),
			console.MustOption("prefer-stable", "", console.OptionValueNone, "Prefer stable versions of dependencies (can also be set via the COMPOSER_PREFER_STABLE=1 env var).", nil),
			console.MustOption("prefer-lowest", "", console.OptionValueNone, "Prefer lowest versions of dependencies (can also be set via the COMPOSER_PREFER_LOWEST=1 env var).", nil),
			console.MustOption("minimal-changes", "m", console.OptionValueNone, "During an update with -w/-W, only perform absolutely necessary changes to transitive dependencies (can also be set via the COMPOSER_MINIMAL_CHANGES=1 env var).", nil),
			console.MustOption("sort-packages", "", console.OptionValueNone, "Sorts packages when adding/updating a new dependency", nil),
			console.MustOption("optimize-autoloader", "o", console.OptionValueNone, "Optimize autoloader during autoloader dump", nil),
			console.MustOption("classmap-authoritative", "a", console.OptionValueNone, "Autoload classes from the classmap only. Implicitly enables `--optimize-autoloader`.", nil),
			console.MustOption("apcu-autoloader", "", console.OptionValueNone, "Use APCu to cache found/not-found classes.", nil),
			console.MustOption("apcu-autoloader-prefix", "", console.OptionValueRequired, "Use a custom prefix for the APCu autoloader cache. Implicitly enables --apcu-autoloader", nil),
		).
		SetHelp(`The require command adds required packages to your composer.json and installs them.

If you do not specify a package, composer will prompt you to search for a package, and given results, provide a list of
matches to require.

If you do not specify a version constraint, composer will choose a suitable one based on the available package versions.

If you do not want to install the new dependencies immediately you can call it with --no-update

Read more at https://getcomposer.org/doc/03-cli.md#require-r`)

	return c
}

// Interact ports interact() (empty: requirements are asked for in
// execute()).
func (*RequireCommand) Interact(console.Input, console.Output) error { return nil }

// Execute ports execute().
func (c *RequireCommand) Execute(in console.Input, out console.Output) (code int, err error) {
	c.file, err = composer.GetComposerFile()
	if err != nil {
		return 0, err
	}
	cio := c.IO()

	if console.BoolOption(in, "no-suggest") {
		cio.WriteError(`<warning>You are using the deprecated option "--no-suggest". It has no effect and will break in Composer 3.</warning>`, true, io.Normal)
	}

	_, statErr := os.Stat(c.file)
	c.newlyCreated = statErr != nil
	if c.newlyCreated && os.WriteFile(c.file, []byte("{\n}\n"), 0o666) != nil {
		cio.WriteError("<error>"+c.file+" could not be created.</error>", true, io.Normal)

		return 1, nil
	}
	if !util.IsReadable(c.file) {
		cio.WriteError("<error>"+c.file+" is not readable.</error>", true, io.Normal)

		return 1, nil
	}

	if st, err := os.Stat(c.file); err == nil && st.Size() == 0 {
		_ = os.WriteFile(c.file, []byte("{\n}\n"), 0o666)
	}

	if c.json, err = json.NewFile(c.file, nil, nil); err != nil {
		return 0, err
	}
	c.lock = composer.GetLockFile(c.file)
	if c.composerBackup, err = os.ReadFile(c.json.Path()); err != nil {
		return 0, &util.ErrorException{Message: "file_get_contents(" + c.json.Path() + "): Failed to open stream: " + err.Error()}
	}
	c.lockBackup = nil
	if data, err := os.ReadFile(c.lock); err == nil {
		c.lockBackup = data
	}

	unregister := c.handleAbortSignals()

	// check for writability by writing to the file as is_writable can not be trusted on network-mounts
	// see https://github.com/composer/composer/issues/8231 and https://bugs.php.net/bug.php?id=68926
	if !util.IsWritable(c.file) && os.WriteFile(c.file, c.composerBackup, 0o666) != nil {
		unregister()
		cio.WriteError("<error>"+c.file+" is not writable.</error>", true, io.Normal)

		return 1, nil
	}

	if in.Option("fixed") == true {
		cfg, err := c.readComposerJSON()
		if err != nil {
			unregister()

			return 0, err
		}

		packageType := "library"
		if v, _ := cfg.Get("type"); php.ToBool(v) {
			packageType = php.ToString(v)
		}

		// @see https://github.com/composer/composer/pull/8313#issuecomment-532637955
		if packageType != "project" && !console.BoolOption(in, "dev") {
			cio.WriteError(`<error>The "--fixed" option is only allowed for packages with a "project" type or for dev dependencies to prevent possible misuses.</error>`, true, io.Normal)

			if !arrayIsset(cfg, "type") {
				cio.WriteError(`<error>If your package is not a library, you can explicitly specify the "type" by using "composer config type project".</error>`, true, io.Normal)
			}
			unregister()

			return 1, nil
		}
	}

	return c.execute(in, out, cio, unregister)
}

// execute is the part of execute() after the --fixed check; it
// unregisters the signal handler on every path.
func (c *RequireCommand) execute(in console.Input, out console.Output, cio io.IO, unregister func()) (int, error) {
	updateStarted := false
	defer func() {
		if !updateStarted {
			unregister()
		}
	}()

	comp, err := c.RequireComposer(nil, nil)
	if err != nil {
		return 0, err
	}
	repos := comp.RepositoryManager().Repositories()

	platformOverrides, err := comp.Config().Get("platform", 0)
	if err != nil {
		return 0, err
	}
	overrides, _ := platformOverrides.(*php.Array)
	platformOptions, err := comp.Runtime().PlatformOptions(comp.ProcessExecutor())
	if err != nil {
		return 0, err
	}
	platformRepo, err := repository.NewPlatformRepository(nil, overrides, platformOptions)
	if err != nil {
		return 0, err
	}
	// initialize $this->repos as it is used by the PackageDiscoveryTrait
	composite, err := repository.NewCompositeRepository(append([]repository.RepositoryInterface{platformRepo}, repos...))
	if err != nil {
		return 0, err
	}
	c.repos = composite

	preferredStability := comp.Package().MinimumStability()
	if comp.Package().PreferStable() {
		preferredStability = "stable"
	}

	requirementsList, err := c.DetermineRequirements(
		in,
		out,
		console.StringsArgument(in, "packages"),
		platformRepo,
		preferredStability,
		console.BoolOption(in, "no-update"), // if there is no update, we need to use the best possible version constraint directly as we cannot rely on the solver to guess the best constraint
		console.BoolOption(in, "fixed"),
	)
	if err != nil {
		if c.newlyCreated {
			if rerr := c.revertComposerFile(); rerr != nil {
				return 0, rerr
			}

			e := NewError(ClassRuntime, "No composer.json present in the current directory ("+c.file+"), this may be the cause of the following exception.")
			e.Prev = asThrowable(err, -1)

			return 0, e
		}

		return 0, err
	}

	requirements, err := c.FormatRequirements(requirementsList)
	if err != nil {
		return 0, err
	}

	if !console.BoolOption(in, "dev") && cio.IsInteractive() && !comp.IsGlobal() {
		if err := c.suggestDev(in, cio, requirements); err != nil {
			return 0, err
		}
	}

	requireKey, removeKey := "require", "require-dev"
	if console.BoolOption(in, "dev") {
		requireKey, removeKey = removeKey, requireKey
	}

	// check which requirements need the version guessed
	var requirementsToGuess []string
	for k, v := range requirements.All() {
		if v == "guess" {
			requirementsToGuess = append(requirementsToGuess, k.String())
		}
	}
	for _, name := range requirementsToGuess {
		requirements.Set(name, "*")
	}

	// validate requirements format
	versionParser := pkg.NewVersionParser()
	for k, v := range requirements.All() {
		name, constraint := k.String(), php.ToString(v)
		if php.Strtolower(name) == comp.Package().Name() {
			cio.WriteError("<error>Root package '"+name+"' cannot require itself in its composer.json</error>", true, io.Normal)

			return 1, nil
		}
		if constraint == "self.version" {
			continue
		}
		if _, err := versionParser.ParseConstraints(constraint); err != nil {
			return 0, err
		}
	}

	inconsistentRequireKeys, err := c.inconsistentRequireKeys(requirements, requireKey)
	if err != nil {
		return 0, err
	}
	if len(inconsistentRequireKeys) > 0 {
		withFlag := "without"
		if console.BoolOption(in, "dev") {
			withFlag = "with"
		}
		for _, p := range inconsistentRequireKeys {
			cio.Warning(p+" is currently present in the "+removeKey+" key and you ran the command "+withFlag+" the --dev flag, which will move it to the "+requireKey+" key.", nil)
		}

		if cio.IsInteractive() {
			which := "this requirement"
			if len(inconsistentRequireKeys) > 1 {
				which = "these requirements"
			}
			move, err := cio.AskConfirmation("<info>Do you want to move "+which+"?</info> [<comment>no</comment>]? ", false)
			if err != nil {
				return 0, err
			}
			if !move {
				rerun := "with"
				if console.BoolOption(in, "dev") {
					rerun = "without"
				}
				ok, err := cio.AskConfirmation("<info>Do you want to re-run the command "+rerun+" --dev?</info> [<comment>yes</comment>]? ", true)
				if err != nil {
					return 0, err
				}
				if !ok {
					return 0, nil
				}

				in.SetOption("dev", true)
				requireKey, removeKey = removeKey, requireKey
			}
		}
	}

	sortPackages, err := optionOrConfig(in, "sort-packages", comp.Config(), "sort-packages")
	if err != nil {
		return 0, err
	}

	c.firstRequire = c.newlyCreated
	if !c.firstRequire {
		composerDefinition, err := c.readComposerJSON()
		if err != nil {
			return 0, err
		}
		if phpCount(composerDefinition, "require") == 0 && phpCount(composerDefinition, "require-dev") == 0 {
			c.firstRequire = true
		}
	}

	if !console.BoolOption(in, "dry-run") {
		if err := c.updateFile(requirements, requireKey, removeKey, sortPackages); err != nil {
			return 0, err
		}
	}

	state := "updated"
	if c.newlyCreated {
		state = "created"
	}
	cio.WriteError("<info>"+c.file+" has been "+state+"</info>", true, io.Normal)

	if console.BoolOption(in, "no-update") {
		return 0, nil
	}

	if err := deactivateInstalledPlugins(comp); err != nil {
		return 0, err
	}

	updateStarted = true

	return c.runUpdate(in, out, cio, requirements, requirementsToGuess, requireKey, removeKey, sortPackages, unregister)
}

// runUpdate is the try/catch/finally block around doUpdate.
func (c *RequireCommand) runUpdate(in console.Input, out console.Output, cio io.IO, requirements *php.Array, requirementsToGuess []string, requireKey, removeKey string, sortPackages bool, unregister func()) (result int, err error) {
	defer func() {
		if console.BoolOption(in, "dry-run") && c.newlyCreated {
			_ = os.Remove(c.json.Path())
		}

		unregister()
	}()

	result, err = c.doUpdate(in, out, cio, requirements, requireKey, removeKey)
	if err == nil && result == 0 && len(requirementsToGuess) > 0 {
		result, err = c.updateRequirementsAfterResolution(requirementsToGuess, requireKey, removeKey, sortPackages, console.BoolOption(in, "dry-run"), console.BoolOption(in, "fixed"))
	}
	if err != nil && isPHPException(err) {
		if !c.dependencyResolutionCompleted {
			if rerr := c.revertComposerFile(); rerr != nil {
				return 0, rerr
			}
		}
	}

	return result, err
}

// isPHPException is `catch (\Exception $e)`: every error except a PHP
// \Error (a TypeError, ...), which the catch lets through.
func isPHPException(err error) bool {
	class := phpClass(err)

	return class != "TypeError" && class != "ValueError" && class != "Error" && class != "ArgumentCountError"
}

// suggestDev is the block asking to re-run with --dev when every new
// package is tagged as a dev tool.
func (c *RequireCommand) suggestDev(in console.Input, cio io.IO, requirements *php.Array) error {
	devTags := []string{"dev", "testing", "static analysis"}
	var devPackages [][]string
	currentRequiresByKey, err := c.packagesByRequireKey()
	if err != nil {
		return err
	}
	repos, err := c.Repos()
	if err != nil {
		return err
	}
	for k := range requirements.All() {
		name := k.String()
		// skip packages which are already in the composer.json as those have already been decided
		if currentRequiresByKey.Has(name) {
			continue
		}

		found, err := repos.FindPackages(name, nil)
		if err != nil {
			return err
		}
		p, ok := pkg.GetMostCurrentVersion(found).(pkg.CompletePackageInterface)
		if !ok {
			continue
		}
		keywords := map[string]bool{}
		if kw := p.Keywords(); kw != nil {
			for _, v := range kw.Values() {
				keywords[php.Strtolower(php.ToString(v))] = true
			}
		}
		var pkgDevTags []string
		for _, tag := range devTags {
			if keywords[tag] {
				pkgDevTags = append(pkgDevTags, tag)
			}
		}
		if len(pkgDevTags) > 0 {
			devPackages = append(devPackages, pkgDevTags)
		}
	}

	if len(devPackages) == requirements.Len() {
		plural, plural2, plural3 := "", "is", "it is"
		if requirements.Len() > 1 {
			plural, plural2, plural3 = "s", "are", "they are"
		}
		var pkgDevTags []string
		for _, tags := range devPackages {
			for _, tag := range tags {
				if !slices.Contains(pkgDevTags, tag) {
					pkgDevTags = append(pkgDevTags, tag)
				}
			}
		}
		cio.Warning("The package"+plural+" you required "+plural2+" recommended to be placed in require-dev (because "+plural3+` tagged as "`+strings.Join(pkgDevTags, `", "`)+`") but you did not use --dev.`, nil)
		ok, err := cio.AskConfirmation("<info>Do you want to re-run the command with --dev?</> [<comment>yes</>]? ", true)
		if err != nil {
			return err
		}
		if ok {
			in.SetOption("dev", true)
		}
	}

	return nil
}

// readComposerJSON is $this->json->read() as an array.
func (c *RequireCommand) readComposerJSON() (*php.Array, error) {
	v, err := c.json.Read()
	if err != nil {
		return nil, err
	}
	a, _ := v.(*php.Array)
	if a == nil {
		a = php.NewArray()
	}

	return a, nil
}

// phpCount is count($a[$key] ?? []).
func phpCount(a *php.Array, key string) int {
	v, _ := a.Get(key)
	switch v := v.(type) {
	case nil:
		return 0
	case *php.Array:
		return v.Len()
	}

	return 1
}

// inconsistentRequireKeys ports getInconsistentRequireKeys.
func (c *RequireCommand) inconsistentRequireKeys(newRequirements *php.Array, requireKey string) ([]string, error) {
	requireKeys, err := c.packagesByRequireKey()
	if err != nil {
		return nil, err
	}
	var inconsistentRequirements []string
	for k, v := range requireKeys.All() {
		if !newRequirements.Has(k.String()) {
			continue
		}
		if requireKey != v {
			inconsistentRequirements = append(inconsistentRequirements, k.String())
		}
	}

	return inconsistentRequirements, nil
}

// packagesByRequireKey ports getPackagesByRequireKey: package =>
// "require" or "require-dev".
func (c *RequireCommand) packagesByRequireKey() (*php.Array, error) {
	composerDefinition, err := c.readComposerJSON()
	if err != nil {
		return nil, err
	}

	result := php.NewArray()
	for _, key := range []string{"require", "require-dev"} {
		v, _ := composerDefinition.Get(key)
		if a, ok := v.(*php.Array); ok {
			for k := range a.All() {
				result.SetKey(k, key)
			}
		}
	}

	return result, nil
}

// doUpdate ports doUpdate.
func (c *RequireCommand) doUpdate(in console.Input, out console.Output, cio io.IO, requirements *php.Array, requireKey, removeKey string) (int, error) {
	// Update packages
	if err := c.ResetComposer(); err != nil {
		return 0, err
	}
	comp, err := c.RequireComposer(nil, nil)
	if err != nil {
		return 0, err
	}

	c.dependencyResolutionCompleted = false
	comp.EventDispatcher().AddListener(eventdispatcher.PreOperationsExec, eventdispatcher.GoFunc(func(eventdispatcher.Event) error {
		c.dependencyResolutionCompleted = true

		return nil
	}), 10000)

	if console.BoolOption(in, "dry-run") {
		rootPackage := comp.Package()
		links := map[string]pkg.Links{
			"require":     rootPackage.Requires(),
			"require-dev": rootPackage.DevRequires(),
		}
		method := pkg.TypeRequire
		if requireKey == "require-dev" {
			method = pkg.TypeDevRequire
		}
		arrayLoader := loader.NewArrayLoader(nil, false)
		newLinks, err := arrayLoader.ParseLinks(rootPackage.Name(), rootPackage.PrettyVersion(), method, requirements)
		if err != nil {
			return 0, err
		}
		merged := links[requireKey]
		for k, l := range newLinks.All() {
			merged = merged.With(k, l)
		}
		links[requireKey] = merged
		for k := range requirements.All() {
			links[removeKey] = links[removeKey].Without(k.String())
		}
		rootPackage.SetRequires(links["require"])
		rootPackage.SetDevRequires(links["require-dev"])

		// extract stability flags & references as they weren't present when loading the unmodified composer.json
		references, err := loader.ExtractReferences(requirements, rootPackage.References())
		if err != nil {
			return 0, err
		}
		rootPackage.SetReferences(references)
		stabilityFlags, err := loader.ExtractStabilityFlags(requirements, rootPackage.MinimumStability(), rootPackage.StabilityFlags())
		if err != nil {
			return 0, err
		}
		rootPackage.SetStabilityFlags(stabilityFlags)
	}

	cfg := comp.Config()
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

	updateAllowTransitiveDependencies := resolver.UpdateOnlyListed
	flags := ""
	if console.BoolOption(in, "update-with-all-dependencies") || console.BoolOption(in, "with-all-dependencies") {
		updateAllowTransitiveDependencies = resolver.UpdateListedWithTransitiveDeps
		flags += " --with-all-dependencies"
	} else if console.BoolOption(in, "update-with-dependencies") || console.BoolOption(in, "with-dependencies") {
		updateAllowTransitiveDependencies = resolver.UpdateListedWithTransitiveDepsNoRootRequire
		flags += " --with-dependencies"
	}

	names := make([]string, 0, requirements.Len())
	for k := range requirements.All() {
		names = append(names, k.String())
	}
	cio.WriteError("<info>Running composer update "+strings.Join(names, " ")+flags+"</info>", true, io.Normal)

	commandEvent := eventdispatcher.NewCommandEvent(eventdispatcher.PluginCommand, "require", in, out, nil, nil)
	if _, err := comp.EventDispatcher().Dispatch(commandEvent.Name(), commandEvent); err != nil {
		return 0, err
	}

	setOutputProgress(comp.InstallationManager(), !console.BoolOption(in, "no-progress"))

	install, err := composer.CreateInstaller(cio, comp)
	if err != nil {
		return 0, err
	}

	preferSource, preferDist, err := c.PreferredInstallOptions(cfg, in, false)
	if err != nil {
		return 0, err
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
		SetPreferSource(preferSource).
		SetPreferDist(preferDist).
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
		SetPreferStable(console.BoolOption(in, "prefer-stable")).
		SetPreferLowest(console.BoolOption(in, "prefer-lowest")).
		SetPolicyConfig(policyConfig).
		SetAuditConfig(auditConfig).
		SetMinimalUpdate(minimalChanges)

	// if no lock is present, or the file is brand new, we do not do a
	// partial update as this is not supported by the Installer
	if !c.firstRequire {
		locked, err := comp.Locker().IsLocked()
		if err != nil {
			return 0, err
		}
		if locked {
			install.SetUpdateAllowList(names)
		}
	}

	status, err := install.Run()
	if err != nil {
		return 0, err
	}
	if status != 0 && status != composer.ErrorAuditFailed {
		if status == composer.ErrorDependencyResolutionFailed {
			for _, req := range c.NormalizeRequirements(console.StringsArgument(in, "packages")) {
				if !req.Version.Valid {
					cio.WriteError(`You can also try re-running composer require with an explicit version constraint, e.g. "composer require `+req.Name+`:*" to figure out if any version is installable, or "composer require `+req.Name+`:^2.1" if you know which you need.`, true, io.Normal)

					break
				}
			}
		}
		if err := c.revertComposerFile(); err != nil {
			return 0, err
		}
	}

	return status, nil
}

// devBranchRegexp is the feature branch check of
// updateRequirementsAfterResolution.
var devBranchRegexp = php.MustCompile(`{^dev-(?!main$|master$|trunk$|latest$)}`)

// updateRequirementsAfterResolution ports updateRequirementsAfterResolution.
func (c *RequireCommand) updateRequirementsAfterResolution(requirementsToUpdate []string, requireKey, removeKey string, sortPackages, dryRun, fixed bool) (int, error) {
	comp, err := c.RequireComposer(nil, nil)
	if err != nil {
		return 0, err
	}
	locker := comp.Locker()
	requirements := php.NewArray()
	repositorySet, err := repository.NewRepositorySet("stable", nil, nil, nil, nil, nil)
	if err != nil {
		return 0, err
	}
	versionSelector := version.NewVersionSelector(repositorySet, nil)
	locked, err := locker.IsLocked()
	if err != nil {
		return 0, err
	}
	var repo repository.RepositoryInterface
	if locked {
		if repo, err = lockedRepository(locker.LockedRepository(true)); err != nil {
			return 0, err
		}
	} else {
		repo = comp.RepositoryManager().LocalRepository()
	}
	cio := c.IO()
	for _, packageName := range requirementsToUpdate {
		p, err := repo.FindPackage(packageName, nil)
		if err != nil {
			return 0, err
		}
		for {
			a, ok := p.(pkg.Alias)
			if !ok {
				break
			}
			p = a.AliasOf()
		}

		if p == nil {
			continue
		}

		var constraint string
		if fixed {
			constraint = p.PrettyVersion()
		} else if constraint, err = versionSelector.FindRecommendedRequireVersion(p); err != nil {
			return 0, err
		}
		requirements.Set(packageName, constraint)
		cio.WriteError("Using version <info>"+constraint+"</info> for <info>"+packageName+"</info>", true, io.Normal)

		isFeatureBranch, err := devBranchRegexp.IsMatch(constraint)
		if err != nil {
			return 0, err
		}
		if isFeatureBranch {
			cio.Warning("Version "+constraint+" looks like it may be a feature branch which is unlikely to keep working in the long run and may be in an unstable state", nil)
			if cio.IsInteractive() {
				ok, err := cio.AskConfirmation("Are you sure you want to use this constraint (<comment>y</comment>) or would you rather abort (<comment>n</comment>) the whole operation [<comment>y,n</comment>]? ", true)
				if err != nil {
					return 0, err
				}
				if !ok {
					if err := c.revertComposerFile(); err != nil {
						return 0, err
					}

					return 1, nil
				}
			}
		}
	}

	if !dryRun {
		if err := c.updateFile(requirements, requireKey, removeKey, sortPackages); err != nil {
			return 0, err
		}
		lockEnabled, err := configTruthy(comp.Config(), "lock")
		if err != nil {
			return 0, err
		}
		if locked && lockEnabled {
			stabilityFlags, err := loader.ExtractStabilityFlags(requirements, comp.Package().MinimumStability(), php.NewArray())
			if err != nil {
				return 0, err
			}
			err = locker.UpdateHash(c.json.Path(), func(lockData *php.Array) *php.Array {
				for k, flag := range stabilityFlags.All() {
					arraySetNested(lockData, "stability-flags", k.String(), flag)
				}

				return lockData
			})
			if err != nil {
				return 0, err
			}
		}
	}

	return 0, nil
}

// updateFile ports updateFile.
func (c *RequireCommand) updateFile(newRequirements *php.Array, requireKey, removeKey string, sortPackages bool) error {
	ok, err := c.updateFileCleanly(newRequirements, requireKey, removeKey, sortPackages)
	if err != nil || ok {
		return err
	}

	composerDefinition, err := c.readComposerJSON()
	if err != nil {
		return err
	}
	for k, version := range newRequirements.All() {
		name := k.String()
		arraySetNested(composerDefinition, requireKey, name, version)
		v, _ := composerDefinition.Get(removeKey)
		if a, ok := v.(*php.Array); ok {
			a.Delete(name)
			if a.Len() == 0 {
				composerDefinition.Delete(removeKey)
			}
		}
	}

	return c.json.Write(composerDefinition, json.DefaultEncodeFlags)
}

// updateFileCleanly ports updateFileCleanly.
func (c *RequireCommand) updateFileCleanly(newRequirements *php.Array, requireKey, removeKey string, sortPackages bool) (bool, error) {
	contents, err := os.ReadFile(c.json.Path())
	if err != nil {
		return false, &util.ErrorException{Message: "file_get_contents(" + c.json.Path() + "): Failed to open stream: " + err.Error()}
	}

	manipulator, err := json.NewManipulator(string(contents))
	if err != nil {
		return false, err
	}

	for k, constraint := range newRequirements.All() {
		ok, err := manipulator.AddLink(requireKey, k.String(), php.ToString(constraint), sortPackages)
		if err != nil || !ok {
			return false, err
		}
		if ok, err = manipulator.RemoveSubNode(removeKey, k.String()); err != nil || !ok {
			return false, err
		}
	}

	if _, err := manipulator.RemoveMainKeyIfEmpty(removeKey); err != nil {
		return false, err
	}

	if err := os.WriteFile(c.json.Path(), []byte(manipulator.Contents()), 0o666); err != nil {
		return false, &util.ErrorException{Message: "file_put_contents(" + c.json.Path() + "): Failed to open stream: " + err.Error()}
	}

	return true, nil
}

// revertComposerFile ports revertComposerFile.
func (c *RequireCommand) revertComposerFile() error {
	cio := c.IO()

	if c.newlyCreated {
		cio.WriteError("\n<error>Installation failed, deleting "+c.file+".</error>", true, io.Normal)
		if err := os.Remove(c.json.Path()); err != nil {
			return &util.ErrorException{Message: "unlink(" + c.json.Path() + "): " + err.Error()}
		}
		if _, err := os.Stat(c.lock); err == nil {
			if err := os.Remove(c.lock); err != nil {
				return &util.ErrorException{Message: "unlink(" + c.lock + "): " + err.Error()}
			}
		}

		return nil
	}

	msg := " to its "
	if len(c.lockBackup) > 0 {
		msg = " and " + c.lock + " to their "
	}
	cio.WriteError("\n<error>Installation failed, reverting "+c.file+msg+"original content.</error>", true, io.Normal)
	if err := os.WriteFile(c.json.Path(), c.composerBackup, 0o666); err != nil {
		return &util.ErrorException{Message: "file_put_contents(" + c.json.Path() + "): Failed to open stream: " + err.Error()}
	}
	if len(c.lockBackup) > 0 {
		if err := os.WriteFile(c.lock, c.lockBackup, 0o666); err != nil {
			return &util.ErrorException{Message: "file_put_contents(" + c.lock + "): Failed to open stream: " + err.Error()}
		}
	}

	return nil
}

// handleAbortSignals is execute()'s SignalHandler: on SIGINT, SIGTERM or
// SIGHUP composer.json (and the lock file) are reverted and the process
// exits with the signal. It returns the unregister function.
func (c *RequireCommand) handleAbortSignals() func() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, util.HandledSignals()...)
	done := make(chan struct{})
	cio := c.IO()

	go func() {
		select {
		case sig := <-ch:
			cio.WriteError("Received "+util.SignalName(sig)+", aborting", true, io.Debug)
			_ = c.revertComposerFile()
			util.ExitWithSignal(sig)
		case <-done:
		}
	}()

	var once bool

	return func() {
		if once {
			return
		}
		once = true
		signal.Stop(ch)
		close(done)
	}
}
