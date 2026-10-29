// Ports src/Composer/Repository/RepositoryManager.php.

package repository

import (
	"github.com/stubbedev/maestro/internal/config"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/semver"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/http"
)

// Deps are the arguments RepositoryManager hands every repository
// constructor after the repository config ($io, $config, $httpDownloader,
// $eventDispatcher, $process).
type Deps struct {
	IO              io.IO
	Config          *config.Config
	HTTPDownloader  *http.HttpDownloader
	EventDispatcher EventDispatcher
	Process         *util.ProcessExecutor
}

// Constructor creates a repository of a registered type from its config:
// what `new $class($config, $io, $config, $httpDownloader,
// $eventDispatcher, $process)` does for a registered class.
type Constructor func(config *php.Array, deps Deps) (RepositoryInterface, error)

// RepositoryManager ports Composer\Repository\RepositoryManager: the
// project's repositories, the local repository, and the registry of
// repository types.
type RepositoryManager struct {
	localRepository InstalledRepositoryInterface
	repositories    []RepositoryInterface
	types           *NameMap[Constructor]
	deps            Deps
	// headerRepositories are the repositories whose config carries http
	// headers, the most specific first; nil until findHttpHeaders looks.
	headerRepositories []HeaderRepository
}

// HeaderRepository is the part of Composer\Repository\ComposerRepository
// findHttpHeaders reads, which a FilterRepository unwraps to.
type HeaderRepository interface {
	RepositoryInterface
	// IsOwnDistUrl is isOwnDistUrl: whether a dist URL of the package
	// belongs to the repository.
	IsOwnDistUrl(p pkg.PackageInterface) bool
	// BaseURL is getBaseUrl.
	BaseURL() string
	// RepoConfig is getRepoConfig.
	RepoConfig() *php.Array
}

// NewRepositoryManager ports new RepositoryManager($io, $config,
// $httpDownloader, $eventDispatcher, $process); a nil process is a new
// ProcessExecutor.
func NewRepositoryManager(out io.IO, cfg *config.Config, httpDownloader *http.HttpDownloader, eventDispatcher EventDispatcher, process *util.ProcessExecutor) *RepositoryManager {
	if process == nil {
		process = http.NewProcessExecutor(out)
	}

	return &RepositoryManager{
		types: &NameMap[Constructor]{},
		deps:  Deps{IO: out, Config: cfg, HTTPDownloader: httpDownloader, EventDispatcher: eventDispatcher, Process: process},
	}
}

// FindPackage ports RepositoryManager::findPackage.
func (m *RepositoryManager) FindPackage(name string, constraint semver.ConstraintInterface) (pkg.PackageInterface, error) {
	for _, repo := range m.repositories {
		p, err := repo.FindPackage(name, constraint)
		if err != nil || p != nil {
			// a repository written in PHP left its code through the call
			// (docs/PLUGINS.md §5.12)
			return p, err
		}
	}

	return nil, nil
}

// FindPackages ports RepositoryManager::findPackages.
func (m *RepositoryManager) FindPackages(name string, constraint semver.ConstraintInterface) ([]pkg.PackageInterface, error) {
	var packages []pkg.PackageInterface
	for _, repo := range m.repositories {
		found, err := repo.FindPackages(name, constraint)
		if err != nil {
			return nil, err
		}
		packages = append(packages, found...)
	}

	return packages, nil
}

// AddRepository ports RepositoryManager::addRepository.
func (m *RepositoryManager) AddRepository(repository RepositoryInterface) {
	m.repositories = append(m.repositories, repository)
	m.headerRepositories = nil
}

// PrependRepository ports RepositoryManager::prependRepository: the
// repository takes precedence over the others (Packagist included).
func (m *RepositoryManager) PrependRepository(repository RepositoryInterface) {
	m.repositories = append([]RepositoryInterface{repository}, m.repositories...)
	m.headerRepositories = nil
}

// CreateRepository ports RepositoryManager::createRepository: a repository
// of a registered type, wrapped in a FilterRepository when config has
// "only", "exclude" or "canonical". name "" is null.
func (m *RepositoryManager) CreateRepository(typ string, config *php.Array, name string) (RepositoryInterface, error) {
	constructor, ok := m.types.Get(typ)
	if !ok {
		return nil, &util.InvalidArgumentError{Message: "Repository type is not registered: " + typ}
	}

	if v, ok := config.Get("packagist"); ok && v == false {
		encoded, _ := php.JSONEncode(config, 0)
		m.deps.IO.WriteError(`<warning>Repository "`+name+`" (`+encoded+`) has a packagist key which should be in its own repository definition</warning>`, true, io.Normal)
	}

	var filterConfig *php.Array
	if config.Isset("only") || config.Isset("exclude") || config.Isset("canonical") {
		filterConfig = config
		config = config.Clone()
		config.Delete("only")
		config.Delete("exclude")
		config.Delete("canonical")
	}

	repository, err := constructor(config, m.deps)
	if err != nil {
		return nil, err
	}

	if filterConfig != nil {
		return NewFilterRepository(repository, filterConfig)
	}

	return repository, nil
}

// SetRepositoryClass ports RepositoryManager::setRepositoryClass: the
// constructor of the repositories of a type.
func (m *RepositoryManager) SetRepositoryClass(typ string, constructor Constructor) {
	m.types.Set(typ, constructor)
}

// RepositoryTypes returns the registered types in registration order
// (array_keys($this->repositoryClasses)).
func (m *RepositoryManager) RepositoryTypes() []string { return m.types.Keys() }

// Repositories ports getRepositories: all repositories but the local one.
func (m *RepositoryManager) Repositories() []RepositoryInterface { return m.repositories }

// SetLocalRepository ports setLocalRepository.
func (m *RepositoryManager) SetLocalRepository(repository InstalledRepositoryInterface) {
	m.localRepository = repository
}

// LocalRepository ports getLocalRepository.
func (m *RepositoryManager) LocalRepository() InstalledRepositoryInterface {
	return m.localRepository
}

// HTTPDownloader ports getHttpDownloader.
func (m *RepositoryManager) HTTPDownloader() *http.HttpDownloader { return m.deps.HTTPDownloader }

// Deps returns the collaborators handed to repository constructors.
func (m *RepositoryManager) Deps() Deps { return m.deps }

// RestoreHttpHeaders ports RepositoryManager::restoreHttpHeaders: the
// headers configured on the composer repository the package's dist URL
// belongs to. Headers often contain credentials so they are not stored in
// composer.lock and installed.json, see RemoveRestorableHttpHeaders.
func (m *RepositoryManager) RestoreHttpHeaders(p pkg.PackageInterface) {
	options := p.TransportOptions()
	if _, isAlias := p.(pkg.Alias); isAlias || options.Path("http", "header") != nil {
		return
	}

	if headers := m.findHttpHeaders(p); headers != nil {
		options.ArrayAtOrCreate("http").Set("header", headers)
		p.SetTransportOptions(options)
	}
}

// RemoveRestorableHttpHeaders ports RepositoryManager::removeRestorableHttpHeaders:
// data, a dumped package, without the http headers RestoreHttpHeaders can
// restore.
func (m *RepositoryManager) RemoveRestorableHttpHeaders(p pkg.PackageInterface, data *php.Array) *php.Array {
	headers := data.Path("transport-options", "http", "header")
	if headers == nil || !phpEquals(headers, m.findHttpHeaders(p)) {
		return data
	}

	transportOptions := data.ArrayAt("transport-options").Clone()
	httpOptions := transportOptions.ArrayAt("http").Clone()
	httpOptions.Delete("header")
	if httpOptions.Len() == 0 {
		transportOptions.Delete("http")
	} else {
		transportOptions.Set("http", httpOptions)
	}
	if transportOptions.Len() == 0 {
		data.Delete("transport-options")
	} else {
		data.Set("transport-options", transportOptions)
	}

	return data
}

// findHttpHeaders is RepositoryManager::findHttpHeaders: the http headers
// of the most specific composer repository the package's dist URL belongs
// to.
func (m *RepositoryManager) findHttpHeaders(p pkg.PackageInterface) any {
	if m.headerRepositories == nil {
		m.headerRepositories = []HeaderRepository{}
		hasHeaders := false
		for _, repo := range m.repositories {
			if filterRepo, ok := repo.(*FilterRepository); ok {
				repo = filterRepo.Repository()
			}
			headerRepo, ok := repo.(HeaderRepository)
			if !ok {
				continue
			}
			m.headerRepositories = append(m.headerRepositories, headerRepo)
			hasHeaders = hasHeaders || headerRepo.RepoConfig().Path("options", "http", "header") != nil
		}
		if !hasHeaders {
			m.headerRepositories = []HeaderRepository{}
		}
	}

	var match HeaderRepository
	for _, repo := range m.headerRepositories {
		if (match == nil || len(repo.BaseURL()) > len(match.BaseURL())) && repo.IsOwnDistUrl(p) {
			match = repo
		}
	}

	if match == nil {
		return nil
	}

	return match.RepoConfig().Path("options", "http", "header")
}

// phpEquals is $a === $b for http header values, which are lists of
// strings.
func phpEquals(a, b any) bool {
	aa, aIsArray := a.(*php.Array)
	bb, bIsArray := b.(*php.Array)
	if aIsArray != bIsArray {
		return a == b
	}

	return php.StrictEquals(aa, bb)
}
