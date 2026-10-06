// Ports src/Composer/Command/CreateProjectCommand.php.

package command

import (
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/stubbedev/maestro/internal/advisory"
	"github.com/stubbedev/maestro/internal/composer"
	"github.com/stubbedev/maestro/internal/config"
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/filter"
	"github.com/stubbedev/maestro/internal/installer"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/json"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/version"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/resolver/operation"
	"github.com/stubbedev/maestro/internal/script"
	"github.com/stubbedev/maestro/internal/semver"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/http"
)

const createProjectFile = "CreateProjectCommand.php"

func init() {
	registerCommand(OrderCreateProject, func() console.Commander { return NewCreateProjectCommand() })
}

// CreateProjectCommand is Composer\Command\CreateProjectCommand.
type CreateProjectCommand struct {
	*BaseCommand

	suggestedPackagesReporter *installer.SuggestedPackagesReporter
}

// NewCreateProjectCommand ports new CreateProjectCommand() (configure()).
func NewCreateProjectCommand() *CreateProjectCommand {
	c := &CreateProjectCommand{BaseCommand: NewBaseCommand("")}
	c.SetImpl(c)
	c.SetName("create-project")
	c.SetDescription("Creates new project from a package into given directory")
	c.SetDefinitionItems(
		console.MustArgument("package", console.ArgumentOptional, "Package name to be installed", nil).WithSuggestFunc(c.SuggestAvailablePackage(99)),
		console.MustArgument("directory", console.ArgumentOptional, "Directory where the files should be created", nil),
		console.MustArgument("version", console.ArgumentOptional, "Version, will default to latest", nil),
		console.MustOption("stability", "s", console.OptionValueRequired, "Minimum-stability allowed (unless a version is specified).", nil),
		console.MustOption("prefer-source", "", console.OptionValueNone, "Forces installation from package sources when possible, including VCS information.", nil),
		console.MustOption("prefer-dist", "", console.OptionValueNone, "Forces installation from package dist (default behavior).", nil),
		optionWithSuggestions("prefer-install", "", console.OptionValueRequired, "Forces installation from package dist|source|auto (auto chooses source for dev versions, dist for the rest).", nil, SuggestPreferInstall()...),
		console.MustOption("repository", "", console.OptionValueRequired|console.OptionValueIsArray, "Add custom repositories to look the package up, either by URL or using JSON arrays", nil),
		console.MustOption("repository-url", "", console.OptionValueRequired, "DEPRECATED: Use --repository instead.", nil),
		console.MustOption("add-repository", "", console.OptionValueNone, "Add the custom repository in the composer.json. If a lock file is present it will be deleted and an update will be run instead of install.", nil),
		console.MustOption("require", "", console.OptionValueRequired|console.OptionValueIsArray, "Require additional package(s) to composer.json after installing the project. If a lock file is present it will be deleted and an update will be run instead of install.", nil),
		console.MustOption("dev", "", console.OptionValueNone, "Enables installation of require-dev packages (enabled by default, only present for BC).", nil),
		console.MustOption("no-dev", "", console.OptionValueNone, "Disables installation of require-dev packages.", nil),
		console.MustOption("no-custom-installers", "", console.OptionValueNone, "DEPRECATED: Use no-plugins instead.", nil),
		console.MustOption("no-scripts", "", console.OptionValueNone, "Whether to prevent execution of all defined scripts in the root package.", nil),
		console.MustOption("no-progress", "", console.OptionValueNone, "Do not output download progress.", nil),
		console.MustOption("no-secure-http", "", console.OptionValueNone, "Disable the secure-http config option temporarily while installing the root package. Use at your own risk. Using this flag is a bad idea.", nil),
		console.MustOption("keep-vcs", "", console.OptionValueNone, "Whether to prevent deleting the vcs folder.", nil),
		console.MustOption("remove-vcs", "", console.OptionValueNone, "Whether to force deletion of the vcs folder without prompting.", nil),
		console.MustOption("no-install", "", console.OptionValueNone, "Whether to skip installation of the package dependencies.", nil),
		console.MustOption("no-audit", "", console.OptionValueNone, "Whether to skip auditing of the installed package dependencies (can also be set via the COMPOSER_NO_AUDIT=1 env var).", nil),
		optionWithSuggestions("audit-format", "", console.OptionValueRequired, `Audit output format. Must be "table", "plain", "json" or "summary".`, advisory.FormatSummary, advisory.Formats[:]...),
		console.MustOption("no-security-blocking", "", console.OptionValueNone, "DEPRECATED: use --no-blocking instead. Allows installing packages with security advisories or that are abandoned (can also be set via the COMPOSER_NO_SECURITY_BLOCKING=1 env var).", nil),
		console.MustOption("no-blocking", "", console.OptionValueNone, "Disables all policy blocking during this command (can also be set via the COMPOSER_NO_BLOCKING=1 env var).", nil),
		console.MustOption("ignore-platform-req", "", console.OptionValueRequired|console.OptionValueIsArray, "Ignore a specific platform requirement (php & ext- packages).", nil),
		console.MustOption("ignore-platform-reqs", "", console.OptionValueNone, "Ignore all platform requirements (php & ext- packages).", nil),
		console.MustOption("ask", "", console.OptionValueNone, "Whether to ask for project directory.", nil),
	)
	c.SetHelp(`The <info>create-project</info> command creates a new project from a given
package into a new directory. If executed without params and in a directory
with a composer.json file it installs the packages for the current project.

You can use this command to bootstrap new projects or setup a clean
version-controlled installation for developers of your project.

<info>php composer.phar create-project vendor/project target-directory [version]</info>

You can also specify the version with the package name using = or : as separator.

<info>php composer.phar create-project vendor/project:version target-directory</info>

To install unstable packages, either specify the version you want, or use the
--stability=dev (where dev can be one of RC, beta, alpha or dev).

To setup a developer workable version you should create the project using the source
controlled code by appending the <info>'--prefer-source'</info> flag.

To install a package from another repository than the default one you
can pass the <info>'--repository=https://myrepository.org'</info> flag.

Read more at https://getcomposer.org/doc/03-cli.md#create-project`)

	return c
}

// ClassName implements console.ClassNamer.
func (*CreateProjectCommand) ClassName() string { return `Composer\Command\CreateProjectCommand` }

// nullableString is a PHP ?string input value: nil for null.
func nullableString(v any) *string {
	if v == nil {
		return nil
	}
	s := php.ToString(v)

	return &s
}

// Execute ports execute().
func (c *CreateProjectCommand) Execute(in console.Input, _ console.Output) (int, error) {
	cfg, err := c.factory().CreateConfig(nil, "")
	if err != nil {
		return 0, err
	}
	cio := c.IO()

	preferSource, preferDist, err := c.PreferredInstallOptions(cfg, in, true)
	if err != nil {
		return 0, err
	}

	if console.BoolOption(in, "dev") {
		cio.WriteError(`<warning>You are using the deprecated option "dev". Dev packages are installed by default now.</warning>`, true, io.Normal)
	}
	if console.BoolOption(in, "no-custom-installers") {
		cio.WriteError(`<warning>You are using the deprecated option "no-custom-installers". Use "no-plugins" instead.</warning>`, true, io.Normal)
		in.SetOption("no-plugins", true)
	}

	if in.IsInteractive() && console.BoolOption(in, "ask") {
		packageName := nullableString(in.Argument("package"))
		if packageName == nil {
			return 0, NewError(ClassRuntime, createProjectFile, 149, `Not enough arguments (missing: "package").`)
		}
		parts := strings.SplitN(strings.ToLower(*packageName), "/", 2)
		answer, err := cio.Ask("New project directory [<comment>"+parts[len(parts)-1]+"</comment>]: ", nil)
		if err != nil {
			return 0, err
		}
		in.SetArgument("directory", answer)
	}

	var repositories []string
	if repos := console.StringsOption(in, "repository"); len(repos) > 0 {
		repositories = repos
	} else if url := nullableString(in.Option("repository-url")); url != nil {
		repositories = []string{*url}
	}

	platformFilter, err := c.PlatformRequirementFilter(in)
	if err != nil {
		return 0, err
	}

	return c.installProjectAt(cio, cfg, in, InstallProjectOptions{
		PackageName:               nullableString(in.Argument("package")),
		Directory:                 nullableString(in.Argument("directory")),
		PackageVersion:            nullableString(in.Argument("version")),
		Stability:                 nullableString(in.Option("stability")),
		PreferSource:              preferSource,
		PreferDist:                preferDist,
		InstallDevPackages:        !console.BoolOption(in, "no-dev"),
		Repositories:              repositories,
		DisablePlugins:            console.BoolOption(in, "no-plugins"),
		DisableScripts:            console.BoolOption(in, "no-scripts"),
		NoProgress:                console.BoolOption(in, "no-progress"),
		NoInstall:                 console.BoolOption(in, "no-install"),
		PlatformRequirementFilter: platformFilter,
		SecureHTTP:                !console.BoolOption(in, "no-secure-http"),
		AddRepository:             console.BoolOption(in, "add-repository"),
		RequiredPackages:          console.StringsOption(in, "require"),
	})
}

// InstallProjectOptions are installProject's optional arguments. A nil
// string is PHP's null; Stability nil is null too (installRootPackage
// derives it), as Execute passes it.
type InstallProjectOptions struct {
	PackageName               *string
	Directory                 *string
	PackageVersion            *string
	Stability                 *string
	PreferSource              bool
	PreferDist                bool
	InstallDevPackages        bool
	Repositories              []string // nil is null
	DisablePlugins            bool
	DisableScripts            bool
	NoProgress                bool
	NoInstall                 bool
	PlatformRequirementFilter filter.PlatformRequirementFilter // nil: ignore nothing
	SecureHTTP                bool
	AddRepository             bool
	RequiredPackages          []string
}

// isPackagistDisabledConfig is `$repoConfig === ['packagist' => false]`
// or `$repoConfig === ['packagist.org' => false]`.
func isPackagistDisabledConfig(repoConfig any) bool {
	a, ok := repoConfig.(*php.Array)
	if !ok || a.Len() != 1 {
		return false
	}
	for _, key := range []string{"packagist", "packagist.org"} {
		if v, ok := a.Get(key); ok && v == false {
			return true
		}
	}

	return false
}

// pluginBlockedClass is Composer\Plugin\PluginBlockedException.
const pluginBlockedClass = `Composer\Plugin\PluginBlockedException`

// vcsNames are the VCS metadata directories create-project removes.
var vcsNames = []string{".svn", "_svn", "CVS", "_darcs", ".arch-params", ".monotone", ".bzr", ".git", ".hg", ".fslckout", "_FOSSIL_"}

// installProjectAt is execute()'s call of installProject (line 155).
func (c *CreateProjectCommand) installProjectAt(cio io.IO, cfg *config.Config, in console.Input, o InstallProjectOptions) (int, error) {
	code, err := c.InstallProject(cio, cfg, in, o)

	return code, err
}

// InstallProject ports installProject.
func (c *CreateProjectCommand) InstallProject(cio io.IO, cfg *config.Config, in console.Input, o InstallProjectOptions) (int, error) {
	oldCwd, err := util.GetCwd(false)
	if err != nil {
		return 0, err
	}

	platformRequirementFilter := o.PlatformRequirementFilter
	if platformRequirementFilter == nil {
		platformRequirementFilter = filter.IgnoreNothingFilter()
	}

	// we need to manually load the configuration to pass the auth credentials to the io interface!
	if err := cio.LoadConfiguration(cfg.ForIO(), util.SetProcessTimeout); err != nil {
		return 0, err
	}

	c.suggestedPackagesReporter = installer.NewSuggestedPackagesReporter(cio)

	installedFromVcs := false
	if o.PackageName != nil {
		installedFromVcs, err = c.installRootPackage(in, cio, cfg, *o.PackageName, platformRequirementFilter, o)
		if err != nil {
			return 0, err
		}
	}

	comp, err := c.CreateComposerInstance(in, cio, nil, o.DisablePlugins, o.DisableScripts)
	if err != nil {
		return 0, err
	}
	flushLockFile := false

	// add the repository to the composer.json and use it for the install run later
	if o.Repositories != nil && o.AddRepository {
		for index, repo := range o.Repositories {
			repoConfig, err := repository.ConfigFromString(repo, true, comp.RepositoryManager().HTTPDownloader())
			if err != nil {
				return 0, err
			}
			existing := comp.Config().Repositories()
			repoArray, _ := repoConfig.(*php.Array)
			if repoArray == nil {
				repoArray = php.NewArray()
			}
			name := repository.GenerateRepositoryName(php.IntKey(int64(index)), repoArray, func(n string) bool { return existing.Has(n) })
			file, err := json.NewFile("composer.json", nil, nil)
			if err != nil {
				return 0, err
			}
			configSource := config.NewJSONConfigSource(file, false)

			if isPackagistDisabledConfig(repoConfig) {
				err = configSource.AddRepository("packagist.org", false, true)
			} else {
				err = configSource.AddRepository(name, repoConfig, false)
			}
			if err != nil {
				return 0, err
			}
			flushLockFile = true

			if comp, err = c.CreateComposerInstance(in, cio, nil, o.DisablePlugins, false); err != nil {
				return 0, err
			}
		}
	}

	// add packages to be installed in package.json
	if len(o.RequiredPackages) > 0 {
		file, err := json.NewFile("composer.json", nil, nil)
		if err != nil {
			return 0, err
		}
		configSource := config.NewJSONConfigSource(file, false)
		flushLockFile = true

		for _, nameColonConstraint := range o.RequiredPackages {
			exploded := strings.Split(nameColonConstraint, ":")
			constraint := "*"
			if len(exploded) > 1 {
				constraint = exploded[1]
			}
			if err := configSource.AddLink("require", exploded[0], constraint); err != nil {
				return 0, err
			}
		}

		if comp, err = c.CreateComposerInstance(in, cio, nil, o.DisablePlugins, false); err != nil {
			return 0, err
		}
	}

	if flushLockFile {
		if st, err := os.Stat("composer.lock"); err == nil && st.Mode().IsRegular() {
			if err := os.Remove("composer.lock"); err != nil {
				return 0, &util.ErrorException{Message: "unlink(composer.lock): " + err.Error()}
			}
		}
	}

	process := comp.Loop().ProcessExecutor()
	fs := util.NewFilesystem(process)

	// dispatch event
	if _, err := comp.EventDispatcher().DispatchScript(script.PostRootPackageInstall, o.InstallDevPackages, nil, nil); err != nil {
		return 0, err
	}

	// use the new config including the newly installed project
	cfg = comp.Config()
	preferSource, preferDist, err := c.PreferredInstallOptions(cfg, in, false)
	if err != nil {
		return 0, err
	}

	// install dependencies of the created project
	if !o.NoInstall {
		setOutputProgress(comp.InstallationManager(), !o.NoProgress)

		inst, err := composer.CreateInstaller(cio, comp)
		if err != nil {
			return 0, err
		}
		optimize, err := configTruthy(cfg, "optimize-autoloader")
		if err != nil {
			return 0, err
		}
		authoritative, err := configTruthy(cfg, "classmap-authoritative")
		if err != nil {
			return 0, err
		}
		apcu, err := configTruthy(cfg, "apcu-autoloader")
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
		inst.SetPreferSource(preferSource).
			SetPreferDist(preferDist).
			SetDevMode(o.InstallDevPackages).
			SetPlatformRequirementFilter(platformRequirementFilter).
			SetSuggestedPackagesReporter(c.suggestedPackagesReporter).
			SetOptimizeAutoloader(optimize).
			SetClassMapAuthoritative(authoritative).
			SetApcuAutoloader(apcu, nil).
			SetPolicyConfig(policyConfig).
			SetAuditConfig(auditConfig)

		locked, err := comp.Locker().IsLocked()
		if err != nil {
			return 0, err
		}
		if !locked {
			inst.SetUpdate(true)
		}

		if o.DisablePlugins {
			if _, err := inst.DisablePlugins(); err != nil {
				return 0, err
			}
		}

		status, err := inst.Run()
		if err != nil {
			if phpClass(err) == pluginBlockedClass {
				cwd, _ := util.GetCwd(true)
				cio.WriteError("<error>Hint: To allow running the config command recommended below before dependencies are installed, run create-project with --no-install.</error>", true, io.Normal)
				cio.WriteError("<error>You can then cd into "+cwd+", configure allow-plugins, and finally run a composer install to complete the process.</error>", true, io.Normal)
			}

			return 0, err
		}
		if status != 0 {
			return status, nil
		}
	}

	hasVcs := installedFromVcs
	if !console.BoolOption(in, "keep-vcs") && installedFromVcs {
		remove := console.BoolOption(in, "remove-vcs") || !cio.IsInteractive()
		if !remove {
			if remove, err = cio.AskConfirmation("<info>Do you want to remove the existing VCS (.git, .svn..) history?</info> [<comment>y,n</comment>]? ", true); err != nil {
				return 0, err
			}
		}
		if remove {
			if err := removeVcsDirectories(fs); err != nil {
				cio.WriteError("<error>An error occurred while removing the VCS metadata: "+err.Error()+"</error>", true, io.Normal)
			}

			hasVcs = false
		}
	}

	// rewriting self.version dependencies with explicit version numbers if the package's vcs metadata is gone
	if !hasVcs {
		p := comp.Package()
		file, err := json.NewFile("composer.json", nil, nil)
		if err != nil {
			return 0, err
		}
		configSource := config.NewJSONConfigSource(file, false)
		for _, meta := range pkg.SupportedLinkTypes() {
			for link := range pkg.LinksByMethod(p, meta.Method).Values() {
				constraint, err := link.PrettyConstraint()
				if err != nil {
					return 0, err
				}
				if constraint == "self.version" {
					if err := configSource.AddLink(meta.Type, link.Target(), p.PrettyVersion()); err != nil {
						return 0, err
					}
				}
			}
		}
	}

	// dispatch event
	if _, err := comp.EventDispatcher().DispatchScript(script.PostCreateProjectCmd, o.InstallDevPackages, nil, nil); err != nil {
		return 0, err
	}

	if err := os.Chdir(oldCwd); err != nil {
		return 0, &util.ErrorException{Message: "chdir(): " + err.Error()}
	}

	return 0, nil
}

// removeVcsDirectories is the Finder loop over the VCS metadata
// directories of the working directory (depth 0, dot files and VCS
// directories included), in readdir() order as Symfony's Finder walks.
func removeVcsDirectories(fs *util.Filesystem) error {
	cwd, err := util.GetCwd(false)
	if err != nil {
		return err
	}
	dirs, err := vcsDirectories(cwd)
	if err != nil {
		return err
	}
	for _, dir := range dirs {
		ok, err := fs.RemoveDirectory(dir)
		if err != nil {
			return err
		}
		if !ok {
			return NewError(ClassRuntime, createProjectFile, 320, "Could not remove "+dir)
		}
	}

	return nil
}

// vcsDirectories is iterator_to_array($finder): the VCS metadata
// directories of cwd in readdir() order.
func vcsDirectories(cwd string) ([]string, error) {
	entries, err := util.ReadDirOrder(cwd)
	if err != nil {
		return nil, &util.UnexpectedValueError{Message: "RecursiveDirectoryIterator::__construct(" + cwd + "): Failed to open directory: " + err.Error()}
	}
	var dirs []string
	for _, entry := range entries {
		path := cwd + string(filepath.Separator) + entry.Name()
		st, err := os.Stat(path)
		if err != nil || !st.IsDir() {
			continue
		}
		for _, name := range vcsNames {
			if entry.Name() == name {
				dirs = append(dirs, path)

				break
			}
		}
	}

	return dirs, nil
}

// stabilitySuffix is the `{^[^,\s]*?@(stable|RC|beta|alpha|dev)$}i` check.
var stabilitySuffix = php.MustCompile(`{^[^,\s]*?@(` + strings.Join(pkg.StabilityNames(), "|") + `)$}i`)

// installRootPackage ports installRootPackage.
func (c *CreateProjectCommand) installRootPackage(in console.Input, cio io.IO, cfg *config.Config, packageName string, platformRequirementFilter filter.PlatformRequirementFilter, o InstallProjectOptions) (bool, error) {
	requirements := pkg.NewVersionParser().ParseNameVersionPairs([]string{packageName})
	name := strings.ToLower(requirements[0].Name)
	packageVersion := ""
	if o.PackageVersion != nil {
		packageVersion = *o.PackageVersion
	}
	if !php.ToBool(packageVersion) && requirements[0].Version.Valid {
		packageVersion = requirements[0].Version.S
	}

	cwd, err := util.GetCwd(false)
	if err != nil {
		return false, err
	}

	// if no directory was specified, use the 2nd part of the package name
	var directory string
	if o.Directory == nil {
		parts := strings.SplitN(name, "/", 2)
		directory = cwd + string(filepath.Separator) + parts[len(parts)-1]
	} else {
		directory = *o.Directory
	}
	directory = strings.TrimRight(directory, `/\`)

	process := http.NewProcessExecutor(cio)
	fs := util.NewFilesystem(process)
	if !util.IsAbsolutePath(directory) {
		directory = cwd + string(filepath.Separator) + directory
	}
	if directory == "" {
		return false, NewError(ClassUnexpectedValue, createProjectFile, 378, "Got an empty target directory, something went wrong")
	}

	// set the base dir to ensure $config->all() below resolves the correct absolute paths to vendor-dir etc
	cfg.SetBaseDir(directory)
	if !o.SecureHTTP {
		if err := cfg.Merge(php.ArrayOf("config", php.ArrayOf("secure-http", false)), config.SourceCommand); err != nil {
			return false, err
		}
	}

	shortest, err := util.FindShortestPath(cwd, directory, true, false)
	if err != nil {
		return false, err
	}
	cio.WriteError(`<info>Creating a "`+packageName+`" project at "`+shortest+`"</info>`, true, io.Normal)

	if st, err := os.Stat(directory); err == nil {
		if !st.IsDir() {
			return false, NewError(ClassInvalidArgument, createProjectFile, 391, `Cannot create project directory at "`+directory+`", it exists as a file.`)
		}
		empty, err := util.IsDirEmpty(directory)
		if err != nil {
			return false, err
		}
		if !empty {
			return false, NewError(ClassInvalidArgument, createProjectFile, 394, `Project directory "`+directory+`" is not empty.`)
		}
	}

	var stability string
	if o.Stability == nil {
		switch packageVersion {
		case "":
			stability = "stable"
		default:
			m, err := stabilitySuffix.MatchStrictGroups(packageVersion)
			if err != nil {
				return false, err
			}
			if m != nil {
				stability = m.Get(1)
			} else {
				stability = semver.ParseStability(packageVersion)
			}
		}
	} else {
		stability = *o.Stability
	}

	if stability, err = semver.NormalizeStability(stability); err != nil {
		// thrown by composer/semver's VersionParser::normalizeStability
		return false, NewError(ClassInvalidArgument, "VersionParser.php", 92, err.Error())
	}

	if _, ok := pkg.StabilityValue(stability); !ok {
		return false, NewError(ClassInvalidArgument, createProjectFile, 411, "Invalid stability provided ("+stability+"), must be one of: "+strings.Join(pkg.StabilityNames(), ", "))
	}

	all, err := cfg.All(0)
	if err != nil {
		return false, err
	}
	comp, err := c.CreateComposerInstance(in, cio, all, o.DisablePlugins, o.DisableScripts)
	if err != nil {
		return false, err
	}
	cfg = comp.Config()
	// set the base dir here again on the new config instance, as otherwise in case the vendor dir is defined in an env var for example it would still override the value set above by $config->all()
	cfg.SetBaseDir(directory)
	rm := comp.RepositoryManager()

	repositorySet, err := repository.NewRepositorySet(stability, nil, nil, nil, nil, nil)
	if err != nil {
		return false, err
	}
	if o.Repositories == nil {
		defaults, err := repository.DefaultRepos(cio, cfg, rm)
		if err != nil {
			return false, err
		}
		composite, err := repository.NewCompositeRepository(repoMapValues(defaults))
		if err != nil {
			return false, err
		}
		if err := repositorySet.AddRepository(composite); err != nil {
			return false, err
		}
	} else {
		for _, repo := range o.Repositories {
			repoConfig, err := repository.ConfigFromString(repo, true, rm.HTTPDownloader())
			if err != nil {
				return false, err
			}
			if isPackagistDisabledConfig(repoConfig) {
				continue
			}

			// disable symlinking for the root package by default as that most likely makes no sense
			if a, ok := repoConfig.(*php.Array); ok {
				if t, _ := a.Get("type"); t == "path" {
					// !isset($repoConfig['options']['symlink']), then
					// $repoConfig['options']['symlink'] = false: options
					// that are not an array fail there, or, false, become
					// one after a deprecation notice
					raw, _ := a.Get("options")
					options, _ := raw.(*php.Array)
					if options == nil || mustGet(options, "symlink") == nil {
						options, created, deprecated, e := php.WritableArray(raw)
						if e != nil {
							return false, e
						}
						if deprecated {
							util.RaiseDeprecation(php.FalseToArrayDeprecation, phperr.At(createProjectFile, 435))
						}
						options.Set("symlink", false)
						if created {
							a.Set("options", options)
						}
					}
				}
			}

			created, err := repository.CreateRepo(repoConfig, rm)
			if err != nil {
				return false, err
			}
			if err := repositorySet.AddRepository(created); err != nil {
				return false, err
			}
		}
	}

	platformOverrides, err := cfg.Get("platform", 0)
	if err != nil {
		return false, err
	}
	overrides, _ := platformOverrides.(*php.Array)
	platformOptions, err := comp.Runtime().PlatformOptions(comp.ProcessExecutor())
	if err != nil {
		return false, err
	}
	platformRepo, err := repository.NewPlatformRepository(nil, overrides, platformOptions)
	if err != nil {
		return false, err
	}
	platformPackages, err := platformRepo.Packages()
	if err != nil {
		return false, err
	}

	// find the latest version if there are multiple
	versionSelector := version.NewVersionSelector(repositorySet, platformPackages)
	p, err := versionSelector.FindBestCandidate(name, version.FindBestCandidateOptions{
		TargetPackageVersion:      packageVersion,
		PreferredStability:        stability,
		PlatformRequirementFilter: platformRequirementFilter,
		IO:                        cio,
	})
	if err != nil {
		return false, err
	}

	if p == nil {
		errorMessage := "Could not find package " + name + " with "
		if packageVersion != "" && packageVersion != "0" {
			errorMessage += "version " + packageVersion
		} else {
			errorMessage += "stability " + stability
		}
		if _, ignoresAll := platformRequirementFilter.(version.IgnoreAllPlatformRequirementFilter); !ignoresAll {
			candidate, err := versionSelector.FindBestCandidate(name, version.FindBestCandidateOptions{
				TargetPackageVersion:      packageVersion,
				PreferredStability:        stability,
				PlatformRequirementFilter: filter.IgnoreAllFilter(),
			})
			if err != nil {
				return false, err
			}
			if candidate != nil {
				return false, NewError(ClassInvalidArgument, createProjectFile, 452, errorMessage+" in a version installable using your PHP version, PHP extensions and Composer version.")
			}
		}

		return false, NewError(ClassInvalidArgument, createProjectFile, 455, errorMessage+".")
	}

	// handler Ctrl+C aborts gracefully
	_ = os.MkdirAll(directory, 0o777)
	unregister := func() {}
	if realDir, ok := util.RealpathOK(directory); ok {
		unregister = c.handleAbortSignals(realDir)
	}
	defer unregister()

	// avoid displaying 9999999-dev as version if default-branch was selected
	if alias, ok := p.(pkg.Alias); ok && p.PrettyVersion() == pkg.DefaultBranchAlias {
		p = alias.AliasOf()
	}

	cio.WriteError("<info>Installing "+p.Name()+" ("+p.FullPrettyVersion(false, pkg.DisplaySourceRefIfDev)+")</info>", true, io.Normal)

	if o.DisablePlugins {
		cio.WriteError("<info>Plugins have been disabled.</info>", true, io.Normal)
	}

	if alias, ok := p.(pkg.Alias); ok {
		p = alias.AliasOf()
	}

	dm := comp.DownloadManager()
	dm.SetPreferSource(o.PreferSource).SetPreferDist(o.PreferDist)

	projectInstaller := installer.NewProjectInstaller(directory, dm, fs)
	im, ok := comp.InstallationManager().(*installer.Manager)
	if !ok {
		return false, &util.LogicError{Message: "create-project needs Composer's InstallationManager"}
	}
	im.SetOutputProgress(!o.NoProgress)
	im.AddInstaller(projectInstaller)
	installedRepo, err := repository.NewInstalledArrayRepository(nil)
	if err != nil {
		return false, err
	}
	if err := im.Execute(installedRepo, []operation.Operation{operation.NewInstallOperation(p)}, true, true, false); err != nil {
		return false, err
	}
	im.NotifyInstalls(cio)

	// collect suggestions
	c.suggestedPackagesReporter.AddSuggestionsFromPackage(p)

	source := p.InstallationSource()
	installedFromVcs := source.Valid && source.S == "source"

	cio.WriteError("<info>Created project in "+directory+"</info>", true, io.Normal)
	if err := os.Chdir(directory); err != nil {
		return false, &util.ErrorException{Message: "chdir(): " + err.Error()}
	}

	// ensure that the env var being set does not interfere with create-project
	// as it is probably not meant to be used here, so we do not use it if a composer.json can be found
	// in the project
	if fileExists(directory + "/composer.json") {
		if _, ok := util.GetEnv("COMPOSER"); ok {
			util.ClearEnv("COMPOSER")
		}
	}

	util.PutEnv("COMPOSER_ROOT_VERSION", p.PrettyVersion())

	// once the root project is fully initialized, we do not need to wipe everything on user abort anymore even if it happens during deps install
	return installedFromVcs, nil
}

// mustGet is $array[$key] (null when missing).
func mustGet(a *php.Array, key string) any {
	v, _ := a.Get(key)

	return v
}

// handleAbortSignals is installRootPackage's SignalHandler: on SIGINT,
// SIGTERM or SIGHUP the project directory is removed and the process
// exits with the signal. It returns the unregister function.
func (c *CreateProjectCommand) handleAbortSignals(realDir string) func() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, util.HandledSignals()...)
	done := make(chan struct{})
	cio := c.IO()

	go func() {
		select {
		case sig := <-ch:
			cio.WriteError("Received "+util.SignalName(sig)+", aborting", true, io.Debug)
			_, _ = util.NewFilesystem(nil).RemoveDirectory(realDir)
			util.ExitWithSignal(sig)
		case <-done:
		}
	}()

	return func() {
		signal.Stop(ch)
		close(done)
	}
}
