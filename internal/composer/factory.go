// Ports src/Composer/Factory.php.

package composer

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"sync"

	"github.com/stubbedev/maestro/internal/autoload"
	"github.com/stubbedev/maestro/internal/cache"
	"github.com/stubbedev/maestro/internal/config"
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/downloader"
	dvcs "github.com/stubbedev/maestro/internal/downloader/vcs"
	"github.com/stubbedev/maestro/internal/eventdispatcher"
	"github.com/stubbedev/maestro/internal/installer"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/json"
	"github.com/stubbedev/maestro/internal/locker"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/archiver"
	"github.com/stubbedev/maestro/internal/pkg/loader"
	"github.com/stubbedev/maestro/internal/pkg/version"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/repository/composerrepo"
	rvcs "github.com/stubbedev/maestro/internal/repository/vcs"
	"github.com/stubbedev/maestro/internal/semver"
	"github.com/stubbedev/maestro/internal/store"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/http"
	vcsutil "github.com/stubbedev/maestro/internal/util/vcs"
)

// Factory ports Composer\Factory: it creates configured Composer instances.
//
// The function fields are the protected methods tests (FactoryMock) and
// the plugin runtime override; nil means Composer's own implementation.
// A Factory holds no other state, so it may be reused and nested.
type Factory struct {
	// Runtime is the process the instances are created in; nil is a new
	// one (with no client version).
	Runtime *Runtime

	// CreateConfigFunc replaces static::createConfig.
	CreateConfigFunc func(out io.IO, cwd string) (*config.Config, error)
	// LoadRootPackageFunc replaces loadRootPackage: the loader of the
	// root package.
	LoadRootPackageFunc func(rm *repository.RepositoryManager, cfg *config.Config, parser *pkg.VersionParser, guesser loader.VersionGuesser, out io.IO) *loader.RootPackageLoader
	// AddLocalRepositoryFunc replaces addLocalRepository.
	AddLocalRepositoryFunc func(out io.IO, rm *repository.RepositoryManager, vendorDir string, root pkg.RootPackageInterface, process *util.ProcessExecutor) error
	// CreateInstallationManagerFunc replaces createInstallationManager.
	CreateInstallationManagerFunc func(loop *http.Loop, out io.IO, dispatcher *eventdispatcher.EventDispatcher) (InstallationManager, error)
	// CreateDefaultInstallersFunc replaces createDefaultInstallers.
	CreateDefaultInstallersFunc func(im InstallationManager, c *PartialComposer, full *Composer, out io.IO, process *util.ProcessExecutor) error
	// PurgePackagesFunc replaces purgePackages.
	PurgePackagesFunc func(repo repository.InstalledRepositoryInterface, im InstallationManager) error
	// CreatePluginManagerFunc replaces createPluginManager: the plugin
	// runtime's seam. nil installs NoPluginManager.
	CreatePluginManagerFunc func(out io.IO, c *Composer, globalComposer *PartialComposer, disablePlugins DisablePlugins) (PluginManager, error)
	// ScriptRuntime is the runtime PHP listeners run in (the plugin
	// runtime); nil runs no PHP listeners.
	ScriptRuntime eventdispatcher.ScriptRuntime
	// EnsureComposerBinary extracts the COMPOSER_BINARY launcher before a
	// script process starts (eventdispatcher.SetEnsureComposerBinary).
	EnsureComposerBinary func() error

	// rootVersions are the root package versions guessed so far.
	rootVersions rootVersions
}

func (f *Factory) runtime() *Runtime {
	if f.Runtime == nil {
		f.Runtime = NewRuntime("", nil)
	}

	return f.Runtime
}

// CreateConfig ports Factory::createConfig: the global configuration with
// cwd ("" for null) as base directory.
func (f *Factory) CreateConfig(out io.IO, cwd string) (*config.Config, error) {
	if f.CreateConfigFunc != nil {
		return f.CreateConfigFunc(out, cwd)
	}

	return config.CreateConfig(out, cwd)
}

// rootVersions are root package versions a Factory guessed, by directory
// and what the guess reads of the root package configuration
// (version.GuessKey): a process loads the same root package
// more than once (validate: for the application and for the file it
// validates; require: before and after it writes composer.json), and the
// guess is then the same, its VCS commands run once. A guess, the guess
// that there is no version included, stands only while no foreign code
// ran (util.RunForeignCode): a script or a plugin may have changed the
// checkout since.
type rootVersions struct {
	mu      sync.Mutex
	guessed map[string]rootVersion
}

// rootVersion is a guess of rootVersions: its data (nil for none) and the
// foreign code generation it was made in.
type rootVersion struct {
	data       *loader.VersionData
	generation uint64
}

// PrefetchRootVersion starts the first command of the guess of the root
// package's version, for a process about to load the project in the
// working directory, when its composer.json leaves the version to guess
// (deliberate deviation 3: the guess then finds its first command done or
// under way). One whose composer.json may set a version, as one naming
// "version" anywhere may, is left alone.
func PrefetchRootVersion() {
	if env, _ := util.GetEnv("COMPOSER_ROOT_VERSION"); php.ToBool(env) {
		return
	}
	file, err := GetComposerFile()
	if err != nil {
		return
	}
	data, err := os.ReadFile(file)
	if err != nil || bytes.Contains(data, []byte(`"version"`)) {
		return
	}
	cwd, err := util.GetCwd(true)
	if err != nil {
		return
	}
	version.PrefetchGuess(cwd)
}

// rootVersionGuesser is the VersionGuesser of the root packages a Factory
// loads, answering from versions.
type rootVersionGuesser struct {
	loader.VersionGuesser
	versions *rootVersions
}

// GuessVersion implements loader.VersionGuesser.
func (g rootVersionGuesser) GuessVersion(packageConfig *php.Array, path string) (*loader.VersionData, error) {
	generation, quiet := util.ForeignCodeGeneration()
	if !quiet {
		return g.VersionGuesser.GuessVersion(packageConfig, path)
	}
	dir, ok := php.Realpath(path)
	if !ok {
		dir = path
	}
	key := dir + "\x00" + version.GuessKey(packageConfig)

	g.versions.mu.Lock()
	defer g.versions.mu.Unlock()
	if guess, ok := g.versions.guessed[key]; ok && guess.generation == generation {
		return cloneVersionData(guess.data), nil
	}
	data, err := g.VersionGuesser.GuessVersion(packageConfig, path)
	if err != nil {
		return data, err
	}
	if g.versions.guessed == nil {
		g.versions.guessed = map[string]rootVersion{}
	}
	g.versions.guessed[key] = rootVersion{data: cloneVersionData(data), generation: generation}

	return data, nil
}

// cloneVersionData is a copy of data, nil for nil: what a guess returns is
// the caller's.
func cloneVersionData(data *loader.VersionData) *loader.VersionData {
	if data == nil {
		return nil
	}
	c := *data

	return &c
}

// GetComposerFile ports Factory::getComposerFile.
func GetComposerFile() (string, error) { return config.ComposerFile() }

// GetLockFile ports Factory::getLockFile.
func GetLockFile(composerFile string) string { return config.LockFile(composerFile) }

// CreateAdditionalStyles ports Factory::createAdditionalStyles: the
// highlight and warning styles of Composer's output. They and Symfony's
// default tags take their look from maestro's theme (console.ThemeStyles),
// which also adds a tag per palette role.
func CreateAdditionalStyles() []console.NamedStyle { return console.ThemeStyles() }

// CreateOutput ports Factory::createOutput: a ConsoleOutput with
// Composer's styles.
func CreateOutput() *console.ConsoleOutput {
	return console.NewConsoleOutput(console.VerbosityNormal, nil, console.NewOutputFormatter(false, CreateAdditionalStyles()...))
}

// CreateHttpDownloader ports Factory::createHttpDownloader.
func (f *Factory) CreateHttpDownloader(out io.IO, cfg *config.Config, options *php.Array) (*http.HttpDownloader, error) {
	return http.CreateHttpDownloader(out, cfg.ForHTTP(), options, f.runtime())
}

// CreateComposer ports createComposer($io, $localConfig, $disablePlugins,
// $cwd, true, $disableScripts). localConfig is nil (the default
// composer.json), a file name or the configuration as a *php.Array; cwd
// "" is null.
func (f *Factory) CreateComposer(out io.IO, localConfig any, disablePlugins DisablePlugins, cwd string, disableScripts bool) (*Composer, error) {
	_, full, err := f.createComposer(out, localConfig, disablePlugins, cwd, true, disableScripts)

	return full, err
}

// CreatePartialComposer ports createComposer with $fullLoad false.
func (f *Factory) CreatePartialComposer(out io.IO, localConfig any, disablePlugins DisablePlugins, cwd string, disableScripts bool) (*PartialComposer, error) {
	partial, _, err := f.createComposer(out, localConfig, disablePlugins, cwd, false, disableScripts)

	return partial, err
}

// Create ports Factory::create: for BC, a configuration given as an array
// or a path other than the default composer.json disables local plugins.
func (f *Factory) Create(out io.IO, cfg any, disablePlugins DisablePlugins, disableScripts bool) (*Composer, error) {
	if cfg != nil && disablePlugins == PluginsEnabled {
		composerFile, err := GetComposerFile()
		if err != nil {
			return nil, err
		}
		if s, ok := cfg.(string); !ok || s != composerFile {
			disablePlugins = PluginsDisabledLocal
		}
	}

	full, err := f.CreateComposer(out, cfg, disablePlugins, "", disableScripts)

	return full, err
}

// CreateGlobal ports Factory::createGlobal: the global Composer (fully
// loaded), nil when it cannot be created.
func (f *Factory) CreateGlobal(out io.IO, disablePlugins, disableScripts bool) (*Composer, error) {
	cfg, err := f.CreateConfig(out, "")
	if err != nil {
		return nil, err
	}
	disable := PluginsEnabled
	if disablePlugins {
		disable = PluginsDisabled
	}
	_, full := f.createGlobalComposer(out, cfg, disable, disableScripts, true)

	return full, nil
}

// configMergeTypeError is the TypeError of $config->merge($localConfig,
// ...) (Factory.php:328) for a local configuration that is not an array.
func configMergeTypeError(localConfig any) error {
	return &php.EngineError{Class: "TypeError", Message: "Composer\\Config::merge(): Argument #1 ($config) must be of type array, " + php.ZvalValueName(localConfig) + " given"}
}

func (f *Factory) createComposer(out io.IO, localConfig any, disablePlugins DisablePlugins, cwd string, fullLoad, disableScripts bool) (*PartialComposer, *Composer, error) {
	// if a custom composer.json path is given, we change the default cwd to be that file's directory
	if s, ok := localConfig.(string); ok && php.IsFile(s) && cwd == "" {
		cwd = php.Dirname(s)
	}

	if cwd == "" {
		var err error
		if cwd, err = util.GetCwd(true); err != nil {
			return nil, nil, err
		}
	}

	// load Composer configuration
	if localConfig == nil {
		composerFile, err := GetComposerFile()
		if err != nil {
			return nil, nil, err
		}
		localConfig = composerFile
	}

	localConfigSource := config.SourceUnknown
	var mergeErr error // a local configuration that is not an array
	composerFile := ""
	var localConfigArray *php.Array
	switch lc := localConfig.(type) {
	case string:
		composerFile = lc

		file, err := json.NewFile(lc, nil, out)
		if err != nil {
			return nil, nil, err
		}

		if !file.Exists() {
			var message string
			if lc == "./composer.json" || lc == "composer.json" {
				message = "Composer could not find a composer.json file in " + cwd
			} else {
				message = "Composer could not find the config file: " + lc
			}
			instructions := ""
			if fullLoad {
				instructions = "To initialize a project, please create a composer.json file. See https://getcomposer.org/basic-usage"
			}

			return nil, nil, &util.InvalidArgumentError{Message: message + php.EOL + instructions}
		}

		if !util.IsInputCompletionProcess() {
			if err := file.ValidateSchema(json.LaxSchema, ""); err != nil {
				var ve *json.ValidationError
				if !errors.As(err, &ve) {
					return nil, nil, err
				}

				errs := " - " + strings.Join(ve.Errors, php.EOL+" - ")

				return nil, nil, &json.ValidationError{Message: ve.Message + ":" + php.EOL + errs}
			}
		}

		data, err := file.Read()
		if err != nil {
			return nil, nil, err
		}
		a, ok := data.(*php.Array)
		if !ok {
			mergeErr = configMergeTypeError(data)
		}
		localConfigArray = a
		localConfigSource = file.Path()
	case *php.Array:
		localConfigArray = lc
	default:
		mergeErr = configMergeTypeError(localConfig)
	}

	// Load config and override with local config/auth config
	cfg, err := f.CreateConfig(out, cwd)
	if err != nil {
		return nil, nil, err
	}
	isGlobal := false
	if localConfigSource != config.SourceUnknown {
		home, _ := cfg.Get("home", 0)
		homePath, homeOK := php.Realpath(php.ToString(home))
		dirPath, dirOK := php.Realpath(php.Dirname(localConfigSource))
		isGlobal = homeOK == dirOK && homePath == dirPath
	}
	if mergeErr != nil {
		// $config->merge($localConfig) at line 328, once createConfig()
		// succeeded
		return nil, nil, mergeErr
	}
	if err := cfg.Merge(localConfigArray, localConfigSource); err != nil {
		return nil, nil, err
	}

	if composerFile != "" {
		composerRealpath, _ := php.Realpath(composerFile)
		out.WriteError("Loading config file "+composerFile+" ("+composerRealpath+")", true, io.Debug)
		configFile, err := json.NewFile(composerRealpath, nil, out)
		if err != nil {
			return nil, nil, err
		}
		cfg.SetConfigSource(config.NewJSONConfigSource(configFile, false))

		localAuthFile, err := json.NewFile(php.Dirname(composerRealpath)+"/auth.json", nil, out)
		if err != nil {
			return nil, nil, err
		}
		if localAuthFile.Exists() {
			out.WriteError("Loading config file "+localAuthFile.Path(), true, io.Debug)
			if err := config.ValidateJSONSchema(out, localAuthFile, json.AuthSchema, ""); err != nil {
				return nil, nil, err
			}
			auth, err := localAuthFile.Read()
			if err != nil {
				return nil, nil, err
			}
			if err := cfg.Merge(php.ArrayOf("config", auth), localAuthFile.Path()); err != nil {
				return nil, nil, err
			}
			cfg.SetLocalAuthConfigSource(config.NewJSONConfigSource(localAuthFile, true))
		}
	}

	// make sure we load the auth env again over the local auth.json + composer.json config
	if err := config.LoadComposerAuthEnv(cfg, out); err != nil {
		return nil, nil, err
	}

	vendorDirValue, err := cfg.Get("vendor-dir", 0)
	if err != nil {
		return nil, nil, err
	}
	vendorDir := php.ToString(vendorDirValue)

	rt := f.runtime()

	// initialize composer
	var partial *PartialComposer
	var full *Composer
	if fullLoad {
		full = &Composer{}
		partial = &full.PartialComposer
	} else {
		partial = &PartialComposer{}
	}
	partial.runtime = rt
	partial.SetConfig(cfg)
	if isGlobal {
		partial.SetGlobal()
	}

	if fullLoad {
		// load auth configs into the IO instance
		if err := out.LoadConfiguration(cfg.ForIO(), util.SetProcessTimeout); err != nil {
			return nil, nil, err
		}

		// load existing Composer\InstalledVersions instance if available and scripts/plugins are allowed, as they might need it
		// we only load if the InstalledVersions class wasn't defined yet so that this is only loaded once
		installedVersionsPath := vendorDir + "/composer/installed.php"
		if disablePlugins == PluginsEnabled && !disableScripts && php.FileExists(installedVersionsPath) && !rt.MarkInstalledVersionsLoaded() {
			// read now, as Composer does, and evaluated once the plugin
			// runtime starts: most runs never start it
			if content, err := os.ReadFile(installedVersionsPath); err == nil {
				rt.SetInstalledVersionsFunc(func() *php.Array {
					data, _, _ := repository.InstalledVersionsFromContent(installedVersionsPath, content)

					return data
				})
			}
		}
	}

	httpDownloader, err := f.CreateHttpDownloader(out, cfg, nil)
	if err != nil {
		return nil, nil, err
	}
	if fullLoad && rt.Fetching() {
		preconnectRepositories(httpDownloader, cfg.Repositories())
	}
	process := http.NewProcessExecutor(out)
	partial.process = process
	loop := http.NewLoop(httpDownloader, process)
	partial.SetLoop(loop)

	// initialize event dispatcher
	var dispatcherComposer eventdispatcher.PartialComposer = partial
	if full != nil {
		dispatcherComposer = full
	}
	dispatcher := eventdispatcher.New(dispatcherComposer, out, process)
	dispatcher.SetRunScripts(!disableScripts)
	dispatcher.SetPHP(rt.PlatformPHP())
	if f.ScriptRuntime != nil {
		dispatcher.SetScriptRuntime(f.ScriptRuntime)
	}
	if f.EnsureComposerBinary != nil {
		dispatcher.SetEnsureComposerBinary(f.EnsureComposerBinary)
	}
	partial.SetEventDispatcher(dispatcher)

	// initialize repository manager; cached metadata files are kept
	// decoded between runs (deliberate deviation 3)
	composerrepo.UseDecodedCache(cache.DecodedMetadata())
	json.UseDecodedFiles(cache.DecodedFiles())
	// so is git's version, which the root package's version guess asks
	vcsutil.UseVersionCache(cache.GitVersion())
	rm := repository.Manager(out, cfg, httpDownloader, dispatcher, process, repository.ExternalTypes{
		Composer: composerrepo.Constructor,
		VCS:      rvcs.NewRepository,
	})
	partial.SetRepositoryManager(rm)

	// force-set the version of the global package if not defined as
	// guessing it adds no value and only takes time
	if !fullLoad {
		if v, ok := localConfigArray.Get("version"); !ok || v == nil {
			localConfigArray = localConfigArray.Clone()
			localConfigArray.Set("version", "1.0.0")
		}
	}

	// load package
	parser := pkg.NewVersionParser()
	guesser := rootVersionGuesser{version.NewVersionGuesser(version.NewProcessExecutor(process), out), &f.rootVersions}
	rootLoader := f.loadRootPackage(rm, cfg, parser, guesser, out)
	loaded, err := rootLoader.LoadIn(localConfigArray, pkg.ClassRootPackage, cwd)
	if err != nil {
		return nil, nil, err
	}
	root, ok := loaded.(pkg.RootPackageInterface)
	if !ok {
		return nil, nil, &util.LogicError{Message: "the root package loader returned a " + loaded.PHPClass()}
	}
	partial.SetPackage(root)

	// load local repository
	if err := f.addLocalRepository(out, rm, vendorDir, root, process); err != nil {
		return nil, nil, err
	}

	// initialize installation manager
	im, err := f.createInstallationManager(loop, out, dispatcher)
	if err != nil {
		return nil, nil, err
	}
	partial.SetInstallationManager(im)

	if full != nil {
		// initialize download manager
		metadata := downloader.NewMetadata()
		if manager, ok := im.(*installer.Manager); ok {
			manager.SetDownloadMetadata(metadata)
		}
		dm, err := f.createDownloadManager(out, cfg, httpDownloader, process, dispatcher, metadata)
		if err != nil {
			return nil, nil, err
		}
		full.SetDownloadManager(dm)

		// initialize autoload generator
		generator := autoload.NewGenerator(dispatcher, out)
		if view, _, err := rt.ComposerView(); err == nil && view != nil {
			// PhpFileParser runs php_strip_whitespace() on the PHP that
			// runs Composer: its short_open_tag and its scanner
			generator.Parser.ShortOpenTag = view.ShortOpenTag()
			generator.Parser.PHPVersionID = int(view.VersionID)
		}
		// classes found in files seen before, and in package store
		// releases (deliberate deviation 3)
		generator.UseParseCacheFile(cache.ClassMapParse())
		generator.UseScanRecords(cache.ClassMapRecords())
		if st, err := store.Open(cache.Store(), nil); err == nil {
			generator.UseStore(st)
			dm.DeriveWith(generator.Deriver)
		}
		full.SetAutoloadGenerator(generator)

		// initialize archive manager
		full.SetArchiveManager(f.CreateArchiveManager(cfg, dm, loop))
	}

	// add installers to the manager (must happen after download manager is created since they read it out of $composer)
	err = f.createDefaultInstallers(im, partial, full, out, process)
	if err != nil {
		return nil, nil, err
	}

	// init locker if possible
	if full != nil {
		if err := f.initLocker(out, full, cfg, composerFile, localConfigArray, process); err != nil {
			return nil, nil, err
		}

		var globalComposer *PartialComposer
		if !full.IsGlobal() {
			globalComposer, _ = f.createGlobalComposer(out, cfg, disablePlugins, disableScripts, false)
		}

		pm, err := f.createPluginManager(out, full, globalComposer, disablePlugins)
		if err != nil {
			return nil, nil, err
		}
		full.SetPluginManager(pm)

		if full.IsGlobal() {
			pm.SetRunningInGlobalDir(true)
		}

		if err := pm.LoadInstalledPlugins(); err != nil {
			return nil, nil, err
		}
	}

	if fullLoad {
		initEvent := eventdispatcher.NewEvent(eventdispatcher.PluginInit, nil, nil)
		if _, err := partial.EventDispatcher().Dispatch(initEvent.Name(), initEvent); err != nil {
			return nil, nil, err
		}

		// once everything is initialized we can
		// purge packages from local repos if they have been deleted on the filesystem
		if err := f.purgePackages(rm.LocalRepository(), im); err != nil {
			return nil, nil, err
		}
	}

	return partial, full, nil
}

// maxPreconnects bounds the repositories preconnectRepositories opens
// connections to.
const maxPreconnects = 4

// preconnectRepositories opens connections to the hosts of the first https
// composer repositories configured (Packagist's included), while the root
// package loads and the command prepares: a command that fetches then
// finds its first request's connection open (deliberate deviation 3).
// Nothing is sent on them; one never used is dropped.
func preconnectRepositories(h *http.HttpDownloader, repos *php.Array) {
	n := 0
	for _, value := range repos.All() {
		repo, ok := value.(*php.Array)
		if !ok {
			continue
		}

		typ, _ := repo.GetString("type")
		url, _ := repo.GetString("url")
		if typ != "composer" || !strings.HasPrefix(url, "https://") {
			continue
		}

		options, _ := repo.Get("options")
		opts, _ := options.(*php.Array)
		h.Preconnect(url, opts)

		if n++; n == maxPreconnects {
			return
		}
	}
}

// initLocker is createComposer's "init locker if possible".
func (f *Factory) initLocker(out io.IO, c *Composer, cfg *config.Config, composerFile string, localConfig *php.Array, process *util.ProcessExecutor) error {
	var lockPath, contents string
	if composerFile != "" {
		lockFile := GetLockFile(composerFile)
		lock, err := cfg.Get("lock", 0)
		if err != nil {
			return err
		}
		if !php.ToBool(lock) && php.FileExists(lockFile) {
			out.WriteError("<warning>"+lockFile+" is present but ignored as the \"lock\" config option is disabled.</warning>", true, io.Normal)
		}

		lockPath = util.GetDevNull()
		if php.ToBool(lock) {
			lockPath = lockFile
		}
		data, err := os.ReadFile(composerFile)
		if err != nil {
			return err
		}
		contents = string(data)
	} else {
		lockPath = util.GetDevNull()
		encoded, err := json.EncodeDefault(localConfig)
		if err != nil {
			return err
		}
		contents = encoded
	}

	file, err := json.NewFile(lockPath, nil, out)
	if err != nil {
		return err
	}
	l, err := locker.New(out, file, c.InstallationManager(), contents, process)
	if err != nil {
		return err
	}
	c.SetLocker(l)

	return nil
}

// createGlobalComposer ports createGlobalComposer: nil (with a debug
// message) when the global composer.json cannot be loaded.
func (f *Factory) createGlobalComposer(out io.IO, cfg *config.Config, disablePlugins DisablePlugins, disableScripts, fullLoad bool) (*PartialComposer, *Composer) {
	// make sure if disable plugins was 'local' it is now turned off
	disable := PluginsEnabled
	if disablePlugins == PluginsDisabledGlobal || disablePlugins == PluginsDisabled {
		disable = PluginsDisabled
	}

	home, err := cfg.Get("home", 0)
	if err == nil {
		homeDir := php.ToString(home)
		var partial *PartialComposer
		var full *Composer
		partial, full, err = f.createComposer(out, homeDir+"/composer.json", disable, homeDir, fullLoad, disableScripts)
		if err == nil {
			return partial, full
		}
	}
	out.WriteError("Failed to initialize global composer: "+err.Error(), true, io.Debug)

	return nil, nil
}

func (f *Factory) loadRootPackage(rm *repository.RepositoryManager, cfg *config.Config, parser *pkg.VersionParser, guesser loader.VersionGuesser, out io.IO) *loader.RootPackageLoader {
	if f.LoadRootPackageFunc != nil {
		return f.LoadRootPackageFunc(rm, cfg, parser, guesser, out)
	}

	return NewRootPackageLoader(rm, cfg, parser, guesser, out)
}

// NewRootPackageLoader is new RootPackageLoader($rm, $config, $parser,
// $guesser, $io).
func NewRootPackageLoader(rm *repository.RepositoryManager, cfg *config.Config, parser *pkg.VersionParser, guesser loader.VersionGuesser, out io.IO) *loader.RootPackageLoader {
	return loader.NewRootPackageLoader(rootRepositoryManager{rm, cfg}, cfg, parser, guesser, out)
}

// rootRepositoryManager gives RootPackageLoader the repository manager it
// adds the configured repositories to.
type rootRepositoryManager struct {
	rm  *repository.RepositoryManager
	cfg *config.Config
}

func (m rootRepositoryManager) AddDefaultRepositories() error {
	repos, err := repository.DefaultRepos(nil, m.cfg, m.rm)
	if err != nil {
		return err
	}
	for _, repo := range repos.All() {
		m.rm.AddRepository(repo)
	}

	return nil
}

func (f *Factory) addLocalRepository(out io.IO, rm *repository.RepositoryManager, vendorDir string, root pkg.RootPackageInterface, process *util.ProcessExecutor) error {
	if f.AddLocalRepositoryFunc != nil {
		return f.AddLocalRepositoryFunc(out, rm, vendorDir, root, process)
	}

	file, err := json.NewFile(vendorDir+"/composer/installed.json", nil, out)
	if err != nil {
		return err
	}
	// read on most runs, and large
	file.KeepDecoded()
	repo, err := repository.NewInstalledFilesystemRepository(file, true, root)
	if err != nil {
		return err
	}
	rt := f.runtime()
	rt.mu.Lock()
	sink := rt.installedVersionsSink
	rt.mu.Unlock()
	if sink != nil {
		repoDir := php.Dirname(file.Path())
		repo.SetInstalledVersionsSink(func(versions *php.Array) { sink(versions, repoDir) })
	}
	rm.SetLocalRepository(repo)

	return nil
}

// CreateDownloadManager ports createDownloadManager: the download manager
// with every downloader registered.
func (f *Factory) CreateDownloadManager(out io.IO, cfg *config.Config, httpDownloader *http.HttpDownloader, process *util.ProcessExecutor, dispatcher *eventdispatcher.EventDispatcher) (*downloader.DownloadManager, error) {
	return f.createDownloadManager(out, cfg, httpDownloader, process, dispatcher, downloader.NewMetadata())
}

// createDownloadManager is CreateDownloadManager with the
// FileDownloader::$downloadMetadata the installation manager shares.
func (f *Factory) createDownloadManager(out io.IO, cfg *config.Config, httpDownloader *http.HttpDownloader, process *util.ProcessExecutor, dispatcher *eventdispatcher.EventDispatcher, metadata *downloader.Metadata) (*downloader.DownloadManager, error) {
	var filesCache downloader.Cache
	ttl, err := cfg.Get("cache-files-ttl", 0)
	if err != nil {
		return nil, err
	}
	fs := util.NewFilesystem(process)
	if php.ToInt(ttl) > 0 {
		dir, err := cfg.Get("cache-files-dir", 0)
		if err != nil {
			return nil, err
		}
		readOnly, err := cfg.Get("cache-read-only", 0)
		if err != nil {
			return nil, err
		}
		c, err := cache.New(out, php.ToString(dir), "a-z0-9_./", fs, php.ToBool(readOnly))
		if err != nil {
			return nil, err
		}
		filesCache = c
	}

	dm := downloader.NewDownloadManager(out, false, fs)
	preferred, err := cfg.Get("preferred-install", 0)
	if err != nil {
		return nil, err
	}
	dm.SetInstallPreference(downloader.PreferenceOf(preferred))

	if a, ok := preferred.(*php.Array); ok {
		if _, err := dm.SetPreferences(a); err != nil {
			return nil, err
		}
	}

	sourceFallback, err := cfg.Get("source-fallback", 0)
	if err != nil {
		return nil, err
	}
	if php.ToBool(sourceFallback) {
		dm.SetSourceFallback(true)
	}

	method, err := store.ParseMethod(os.Getenv(store.MethodEnv))
	if err != nil {
		return nil, err
	}
	st, err := store.Open(cache.Store(), &store.Options{Method: method})
	if err != nil {
		return nil, err
	}

	dvcs.Register(dm, dvcs.Deps{IO: out, Config: cfg.ForHTTP(), Process: process, Filesystem: fs, Store: st})
	deps := downloader.Deps{
		IO:             out,
		Config:         cfg.ForHTTP(),
		HTTPDownloader: httpDownloader,
		Cache:          filesCache,
		Filesystem:     fs,
		Process:        process,
		Store:          st,
		Metadata:       metadata,
		IniFiles:       f.runtime().Environment().IniFiles,
		// extension_loaded() of the PHP Composer runs on; without php,
		// maestro extracts everything natively.
		ExtensionLoaded: func(name string) bool {
			if view, _, err := f.runtime().ComposerView(); err != nil || view == nil {
				return true
			}

			return f.runtime().Environment().ExtensionLoaded(name)
		},
	}
	if dispatcher != nil {
		deps.EventDispatcher = dispatcher
	}

	for _, d := range []struct {
		typ string
		new func(downloader.Deps) (downloader.Downloader, error)
	}{
		{"zip", archiveDownloader(downloader.NewZipDownloader)},
		{"rar", archiveDownloader(downloader.NewRarDownloader)},
		{"tar", archiveDownloader(downloader.NewTarDownloader)},
		{"gzip", archiveDownloader(downloader.NewGzipDownloader)},
		{"xz", archiveDownloader(downloader.NewXzDownloader)},
		{"phar", archiveDownloader(downloader.NewPharDownloader)},
		{"file", func(deps downloader.Deps) (downloader.Downloader, error) { return downloader.NewFileDownloader(deps) }},
		{"path", func(deps downloader.Deps) (downloader.Downloader, error) { return downloader.NewPathDownloader(deps) }},
	} {
		dl, err := d.new(deps)
		if err != nil {
			return nil, err
		}
		dm.SetDownloader(d.typ, dl)
	}

	return dm, nil
}

func archiveDownloader(ctor func(downloader.Deps) (*downloader.ArchiveDownloader, error)) func(downloader.Deps) (downloader.Downloader, error) {
	return func(deps downloader.Deps) (downloader.Downloader, error) { return ctor(deps) }
}

// CreateArchiveManager ports createArchiveManager: ZipArchive and Phar are
// always available.
func (f *Factory) CreateArchiveManager(_ *config.Config, dm *downloader.DownloadManager, loop *http.Loop) *archiver.ArchiveManager {
	am := archiver.NewArchiveManager(dm.Sync(), loop)
	am.AddArchiver(archiver.NewZipArchiver())
	am.AddArchiver(archiver.NewPharArchiver())

	return am
}

func (f *Factory) createPluginManager(out io.IO, c *Composer, globalComposer *PartialComposer, disablePlugins DisablePlugins) (PluginManager, error) {
	if err := readLockForAllowPlugins(c); err != nil {
		return nil, err
	}
	if f.CreatePluginManagerFunc != nil {
		return f.CreatePluginManagerFunc(out, c, globalComposer, disablePlugins)
	}

	return NewNoPluginManager(disablePlugins), nil
}

// readLockForAllowPlugins is the part of PluginManager::__construct every
// plugin manager shares: parseAllowedPlugins(allow-plugins, $locker) reads
// the lock file (Locker::isLocked, which prints "Reading ./composer.lock" at
// -vvv and fails on a broken lock file) when allow-plugins is []. The
// Locker caches what it read, so a plugin manager doing the same reads
// nothing twice.
func readLockForAllowPlugins(c *Composer) error {
	allow, err := c.Config().Get("allow-plugins", 0)
	if err != nil {
		return err
	}
	if a, ok := allow.(*php.Array); !ok || a.Len() != 0 || c.Locker() == nil {
		return nil
	}
	// $locker->isLocked() and getPluginApi() at PluginManager.php:692, in
	// parseAllowedPlugins() called at line 90
	locked, err := c.Locker().IsLocked()
	if err != nil {
		return err
	}
	if !locked {
		return nil
	}
	api, err := c.Locker().PluginAPI()
	if err != nil {
		return err
	}
	_, err = PluginAPIBefore22(api)

	return err
}

// PluginAPIBefore22 is parseAllowedPlugins' version_compare(
// $locker->getPluginApi(), '2.2.0', '<'). PluginManager.php declares
// strict_types, so a plugin-api-version that is not a string is a
// TypeError, which Application::hintCommonErrors' second Factory::create
// raises again outside every catch: PHP's uncaught fatal error.
func PluginAPIBefore22(api any) (bool, error) {
	s, ok := api.(string)
	if !ok {
		return false, &php.EngineError{Class: "TypeError", Message: "version_compare(): Argument #1 ($version1) must be of type string, " + php.ZvalValueName(api) + " given"}
	}

	return semver.VersionCompare(s, "2.2.0") < 0, nil
}

func (f *Factory) createInstallationManager(loop *http.Loop, out io.IO, dispatcher *eventdispatcher.EventDispatcher) (InstallationManager, error) {
	if f.CreateInstallationManagerFunc != nil {
		return f.CreateInstallationManagerFunc(loop, out, dispatcher)
	}

	return defaultInstallationManager(loop, out, dispatcher)
}

func (f *Factory) createDefaultInstallers(im InstallationManager, c *PartialComposer, full *Composer, out io.IO, process *util.ProcessExecutor) error {
	if f.CreateDefaultInstallersFunc != nil {
		return f.CreateDefaultInstallersFunc(im, c, full, out, process)
	}

	return defaultInstallers(im, c, full, out, process)
}

// purgePackages removes the packages that are no longer installed on the
// filesystem from the local repository.
func (f *Factory) purgePackages(repo repository.InstalledRepositoryInterface, im InstallationManager) error {
	if f.PurgePackagesFunc != nil {
		return f.PurgePackagesFunc(repo, im)
	}

	packages, err := repo.Packages()
	if err != nil {
		return err
	}
	for _, p := range packages {
		installed, err := im.IsPackageInstalled(repo, p)
		if err != nil {
			return err
		}
		if !installed {
			if err := repo.RemovePackage(p); err != nil {
				return err
			}
		}
	}

	return nil
}
