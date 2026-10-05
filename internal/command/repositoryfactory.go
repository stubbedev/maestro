// Ports RepositoryFactory::defaultReposWithDefaultManager and ::manager
// (src/Composer/Repository/RepositoryFactory.php), which need Factory and
// so cannot live in internal/repository.

package command

import (
	"github.com/stubbedev/maestro/internal/composer"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/repository/composerrepo"
	rvcs "github.com/stubbedev/maestro/internal/repository/vcs"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/http"
)

// defaultReposWithDefaultManager ports
// RepositoryFactory::defaultReposWithDefaultManager: the default
// repositories of the global configuration, by name.
func defaultReposWithDefaultManager(out io.IO, factory *composer.Factory) (*repository.NameMap[repository.RepositoryInterface], error) {
	cfg, err := factory.CreateConfig(out, "")
	if err != nil {
		return nil, err
	}
	httpDownloader, err := factory.CreateHttpDownloader(out, cfg, nil)
	if err != nil {
		return nil, err
	}
	process := http.NewProcessExecutor(out)
	process.EnableAsync()
	rm := repository.Manager(out, cfg, httpDownloader, nil, process, repository.ExternalTypes{
		Composer: composerrepo.Constructor,
		VCS:      rvcs.NewRepository,
	})
	out.LoadConfiguration(cfg.ForIO(), util.SetProcessTimeout)

	return repository.DefaultRepos(out, cfg, rm)
}

// repoMapValues is array_values() of a name => repository map, and
// repoMapNames its array_keys().
func repoMapValues(m *repository.NameMap[repository.RepositoryInterface]) []repository.RepositoryInterface {
	var repos []repository.RepositoryInterface
	for _, r := range m.All() {
		repos = append(repos, r)
	}

	return repos
}

func repoMapNames(m *repository.NameMap[repository.RepositoryInterface]) []string { return m.Keys() }
