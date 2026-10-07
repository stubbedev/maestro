// Ports src/Composer/Composer.php and src/Composer/PartialComposer.php.

package composer

import (
	"github.com/stubbedev/maestro/internal/autoload"
	"github.com/stubbedev/maestro/internal/config"
	"github.com/stubbedev/maestro/internal/downloader"
	"github.com/stubbedev/maestro/internal/eventdispatcher"
	"github.com/stubbedev/maestro/internal/locker"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/archiver"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/upstream"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/http"
)

// Composer::VERSION and friends, of the release maestro ports (internal/upstream).
const (
	Version            = upstream.ComposerVersion
	BranchAliasVersion = ""
	ReleaseDate        = upstream.ComposerReleaseDate
	SourceVersion      = ""
	RuntimeAPIVersion  = upstream.RuntimeAPIVersion
)

var commitIDPattern = php.MustCompile(`{^[a-f0-9]{40}$}`)

// GetVersion ports Composer::getVersion.
func GetVersion() string {
	// no replacement done, this must be a source checkout
	if Version == "@package_version"+"@" {
		return SourceVersion
	}

	// we have a branch alias and version is a commit id, this must be a snapshot build
	if BranchAliasVersion != "" {
		if ok, _ := commitIDPattern.IsMatch(Version); ok {
			return BranchAliasVersion + "+" + Version
		}
	}

	return Version
}

// PartialComposer ports Composer\PartialComposer: the object graph of a
// project loaded without the parts only installs need (the global
// Composer's, by default).
type PartialComposer struct {
	global              bool
	pkg                 pkg.RootPackageInterface
	loop                *http.Loop
	repositoryManager   *repository.RepositoryManager
	installationManager InstallationManager
	config              *config.Config
	eventDispatcher     *eventdispatcher.EventDispatcher

	// runtime and process are what PHP reaches through statics and
	// `new ProcessExecutor()`: the process the Composer instance was
	// created in, and the Factory's ProcessExecutor.
	runtime *Runtime
	process *util.ProcessExecutor
}

// Runtime returns the process runtime the instance was created in.
func (c *PartialComposer) Runtime() *Runtime { return c.runtime }

// ProcessExecutor returns the ProcessExecutor the Factory created.
func (c *PartialComposer) ProcessExecutor() *util.ProcessExecutor { return c.process }

// SetPackage ports setPackage.
func (c *PartialComposer) SetPackage(p pkg.RootPackageInterface) { c.pkg = p }

// Package ports getPackage.
func (c *PartialComposer) Package() pkg.RootPackageInterface { return c.pkg }

// SetConfig ports setConfig.
func (c *PartialComposer) SetConfig(cfg *config.Config) { c.config = cfg }

// Config ports getConfig.
func (c *PartialComposer) Config() *config.Config { return c.config }

// SetLoop ports setLoop.
func (c *PartialComposer) SetLoop(loop *http.Loop) { c.loop = loop }

// Loop ports getLoop.
func (c *PartialComposer) Loop() *http.Loop { return c.loop }

// SetRepositoryManager ports setRepositoryManager.
func (c *PartialComposer) SetRepositoryManager(m *repository.RepositoryManager) {
	c.repositoryManager = m
}

// RepositoryManager ports getRepositoryManager.
func (c *PartialComposer) RepositoryManager() *repository.RepositoryManager {
	return c.repositoryManager
}

// SetInstallationManager ports setInstallationManager.
func (c *PartialComposer) SetInstallationManager(m InstallationManager) {
	c.installationManager = m
}

// InstallationManager ports getInstallationManager.
func (c *PartialComposer) InstallationManager() InstallationManager {
	return c.installationManager
}

// SetEventDispatcher ports setEventDispatcher.
func (c *PartialComposer) SetEventDispatcher(d *eventdispatcher.EventDispatcher) {
	c.eventDispatcher = d
}

// EventDispatcher ports getEventDispatcher.
func (c *PartialComposer) EventDispatcher() *eventdispatcher.EventDispatcher {
	return c.eventDispatcher
}

// IsGlobal ports isGlobal.
func (c *PartialComposer) IsGlobal() bool { return c.global }

// SetGlobal ports setGlobal.
func (c *PartialComposer) SetGlobal() { c.global = true }

// Composer ports Composer\Composer: the fully loaded object graph of a
// project. `instanceof Composer` (rather than only PartialComposer) is a
// *Composer.
type Composer struct {
	PartialComposer

	locker            *locker.Locker
	downloadManager   *downloader.DownloadManager
	pluginManager     PluginManager
	autoloadGenerator *autoload.Generator
	archiveManager    *archiver.ArchiveManager
}

// Partial returns the PartialComposer part, for callers taking either.
func (c *Composer) Partial() *PartialComposer { return &c.PartialComposer }

// SetLocker ports setLocker.
func (c *Composer) SetLocker(l *locker.Locker) { c.locker = l }

// Locker ports getLocker.
func (c *Composer) Locker() *locker.Locker { return c.locker }

// SetDownloadManager ports setDownloadManager.
func (c *Composer) SetDownloadManager(m *downloader.DownloadManager) { c.downloadManager = m }

// DownloadManager ports getDownloadManager.
func (c *Composer) DownloadManager() *downloader.DownloadManager { return c.downloadManager }

// SetArchiveManager ports setArchiveManager.
func (c *Composer) SetArchiveManager(m *archiver.ArchiveManager) { c.archiveManager = m }

// ArchiveManager ports getArchiveManager.
func (c *Composer) ArchiveManager() *archiver.ArchiveManager { return c.archiveManager }

// SetPluginManager ports setPluginManager.
func (c *Composer) SetPluginManager(m PluginManager) { c.pluginManager = m }

// PluginManager ports getPluginManager.
func (c *Composer) PluginManager() PluginManager { return c.pluginManager }

// SetAutoloadGenerator ports setAutoloadGenerator.
func (c *Composer) SetAutoloadGenerator(g *autoload.Generator) { c.autoloadGenerator = g }

// AutoloadGenerator ports getAutoloadGenerator.
func (c *Composer) AutoloadGenerator() *autoload.Generator { return c.autoloadGenerator }

// ScriptAutoloader implements eventdispatcher.Composer: what
// EventDispatcher::makeAutoloader reads from the Composer instance, at the
// time of the call.
func (c *Composer) ScriptAutoloader() eventdispatcher.ScriptAutoloader {
	return scriptAutoloader{c}
}

type scriptAutoloader struct{ c *Composer }

func (s scriptAutoloader) CanonicalLocalPackages() ([]pkg.PackageInterface, error) {
	return s.c.RepositoryManager().LocalRepository().CanonicalPackages()
}

func (s scriptAutoloader) SetDevMode(devMode bool) {
	s.c.AutoloadGenerator().SetDevMode(devMode)
}

func (s scriptAutoloader) CreateLoader(packages []pkg.PackageInterface) (*eventdispatcher.LoaderContents, error) {
	generator := s.c.AutoloadGenerator()
	root := s.c.Package()
	packageMap, err := generator.BuildPackageMap(s.c.InstallationManager(), root, packages)
	if err != nil {
		return nil, err
	}
	autoloads, err := generator.ParseAutoloads(packageMap, root, autoload.NoDevFilter)
	if err != nil {
		return nil, err
	}
	vendorDir, err := s.c.Config().Get("vendor-dir", 0)
	if err != nil {
		return nil, err
	}
	loader, err := generator.CreateLoader(autoloads, php.ToString(vendorDir))
	if err != nil {
		return nil, err
	}

	return &eventdispatcher.LoaderContents{
		VendorDir: loader.VendorDir,
		Psr0:      autoloads.PSR0,
		Psr4:      autoloads.PSR4,
		ClassMap:  loader.ClassMap,
	}, nil
}

var (
	_ eventdispatcher.PartialComposer = (*PartialComposer)(nil)
	_ eventdispatcher.Composer        = (*Composer)(nil)
)
