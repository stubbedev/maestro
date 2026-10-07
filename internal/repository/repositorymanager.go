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
}

// PrependRepository ports RepositoryManager::prependRepository: the
// repository takes precedence over the others (Packagist included).
func (m *RepositoryManager) PrependRepository(repository RepositoryInterface) {
	m.repositories = append([]RepositoryInterface{repository}, m.repositories...)
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
