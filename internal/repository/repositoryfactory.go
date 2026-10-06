// Ports src/Composer/Repository/RepositoryFactory.php.

package repository

import (
	"path/filepath"
	"strings"

	"github.com/stubbedev/maestro/internal/config"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/json"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/http"
)

// ExternalTypes are the constructors of the repository types implemented
// outside this package: ComposerRepository (internal/repository/composerrepo)
// and VcsRepository (internal/repository/vcs). A nil constructor leaves
// its types unregistered.
type ExternalTypes struct {
	Composer Constructor
	VCS      Constructor
}

// vcsTypes are the types RepositoryFactory::manager maps to VcsRepository,
// after "vcs" itself.
var vcsTypes = [...]string{"git", "bitbucket", "git-bitbucket", "github", "gitlab", "svn", "fossil", "perforce", "hg"}

// Manager ports RepositoryFactory::manager: a RepositoryManager with every
// repository type registered, in Composer's order. A nil process is a new
// ProcessExecutor with async enabled. Composer creates the HttpDownloader
// when none is given; here the caller does (http.CreateHttpDownloader).
func Manager(out io.IO, cfg *config.Config, httpDownloader *http.HttpDownloader, eventDispatcher EventDispatcher, process *util.ProcessExecutor, external ExternalTypes) *RepositoryManager {
	if process == nil {
		process = http.NewProcessExecutor(out)
		process.EnableAsync()
	}

	rm := NewRepositoryManager(out, cfg, httpDownloader, eventDispatcher, process)
	if external.Composer != nil {
		rm.SetRepositoryClass("composer", external.Composer)
	}
	if external.VCS != nil {
		rm.SetRepositoryClass("vcs", external.VCS)
	}
	rm.SetRepositoryClass("package", newPackageRepository)
	rm.SetRepositoryClass("pear", newPearRepository)
	if external.VCS != nil {
		for _, typ := range vcsTypes {
			rm.SetRepositoryClass(typ, external.VCS)
		}
	}
	rm.SetRepositoryClass("artifact", newArtifactRepository)
	rm.SetRepositoryClass("path", newPathRepository)

	return rm
}

func newPackageRepository(config *php.Array, _ Deps) (RepositoryInterface, error) {
	return NewPackageRepository(config)
}

func newPearRepository(*php.Array, Deps) (RepositoryInterface, error) { return NewPearRepository() }

func newArtifactRepository(config *php.Array, deps Deps) (RepositoryInterface, error) {
	return NewArtifactRepository(config, deps.IO)
}

func newPathRepository(config *php.Array, deps Deps) (RepositoryInterface, error) {
	return NewPathRepository(config, deps.IO, deps.Process)
}

// ConfigFromString ports RepositoryFactory::configFromString: the
// repository config a --repository argument stands for (an http URL, a
// .json file or a JSON object). httpDownloader reads .json files given by
// URL; it may be nil (Composer creates one from $io and $config, its
// only use of them).
//
// A .json file that is not a Composer repository becomes, with
// allowFilesystem, ['type' => 'filesystem', 'json' => <path>]: PHP puts
// the JsonFile object there, a *php.Array holds its path.
func ConfigFromString(repository string, allowFilesystem bool, httpDownloader *http.HttpDownloader) (any, error) {
	switch {
	case len(repository) >= 4 && php.Strcasecmp(repository[:4], "http") == 0:
		return php.ArrayOf("type", "composer", "url", repository), nil
	case filepath.Ext(repository) == ".json":
		var downloader json.HTTPDownloader
		if httpDownloader != nil {
			downloader = jsonDownloader{httpDownloader}
		}
		file, err := json.NewFile(repository, downloader, nil)
		if err != nil {
			return nil, err
		}
		decoded, err := file.Read()
		if err != nil {
			return nil, err
		}
		data, _ := decoded.(*php.Array)
		if data != nil && (nonEmpty(data, "packages") || nonEmpty(data, "includes") || nonEmpty(data, "provider-includes")) {
			real := ""
			if abs, err := filepath.Abs(repository); err == nil {
				if resolved, err := filepath.EvalSymlinks(abs); err == nil {
					real = resolved
				}
			}

			return php.ArrayOf("type", "composer", "url", "file://"+strings.ReplaceAll(real, `\`, "/")), nil
		}
		if allowFilesystem {
			return php.ArrayOf("type", "filesystem", "json", repository), nil
		}

		return nil, &util.InvalidArgumentError{Site: phperr.At("RepositoryFactory.php", 45), Message: "Invalid repository URL (" + repository + ") given. This file does not contain a valid composer repository."}
	case strings.HasPrefix(repository, "{"):
		// assume it is a json object that makes a repo config
		return json.ParseJSON(repository, "")
	}

	return nil, &util.InvalidArgumentError{Site: phperr.At("RepositoryFactory.php", 51), Message: "Invalid repository url (" + util.SanitizeURL(repository) + ") given. Has to be a .json file, an http url or a JSON object."}
}

// jsonDownloader adapts an HttpDownloader to json.HTTPDownloader.
type jsonDownloader struct{ h *http.HttpDownloader }

func (d jsonDownloader) Get(url string) (string, error) {
	response, err := d.h.Get(url, nil)
	if err != nil {
		return "", err
	}

	return response.Body(), nil
}

// nonEmpty is !empty($data[$key]).
func nonEmpty(data *php.Array, key string) bool {
	v, _ := data.Get(key)

	return php.ToBool(v)
}

// FromString ports RepositoryFactory::fromString.
func FromString(repository string, allowFilesystem bool, rm *RepositoryManager) (RepositoryInterface, error) {
	repoConfig, err := ConfigFromString(repository, allowFilesystem, rm.HTTPDownloader())
	if err != nil {
		return nil, phperr.Call(err, `Composer\Repository\RepositoryFactory::configFromString`, "RepositoryFactory.php", 59)
	}
	repo, err := CreateRepo(repoConfig, rm)

	return repo, phperr.Call(err, `Composer\Repository\RepositoryFactory::createRepo`, "RepositoryFactory.php", 61)
}

// CreateRepo ports RepositoryFactory::createRepo (Composer deprecated
// calling it without a manager; one is required here).
func CreateRepo(repoConfig any, rm *RepositoryManager) (RepositoryInterface, error) {
	repos, err := createRepos(rm, php.ListOf(repoConfig))
	if err != nil {
		return nil, phperr.Call(err, `Composer\Repository\RepositoryFactory::createRepos`, "RepositoryFactory.php", 73)
	}
	for _, repo := range repos.All() {
		return repo, nil
	}

	return nil, nil
}

// DefaultRepos ports RepositoryFactory::defaultRepos: the repositories of
// the configuration (Packagist included unless disabled), by name. io may
// be nil; with one, it loads the configuration's credentials first.
func DefaultRepos(out io.IO, cfg *config.Config, rm *RepositoryManager) (*NameMap[RepositoryInterface], error) {
	if out != nil {
		if err := out.LoadConfiguration(cfg.ForIO(), util.SetProcessTimeout); err != nil {
			return nil, phperr.Call(err, `Composer\IO\BaseIO->loadConfiguration`, "RepositoryFactory.php", 91)
		}
	}

	repos, err := createRepos(rm, cfg.Repositories())

	return repos, phperr.Call(err, `Composer\Repository\RepositoryFactory::createRepos`, "RepositoryFactory.php", 100)
}

// createRepos ports RepositoryFactory::createRepos.
func createRepos(rm *RepositoryManager, repoConfigs *php.Array) (*NameMap[RepositoryInterface], error) {
	repos := &NameMap[RepositoryInterface]{}

	for index, repoValue := range repoConfigs.All() {
		if _, ok := repoValue.(string); ok {
			return nil, &util.UnexpectedValueError{Site: phperr.At("RepositoryFactory.php", 155), Message: `"repositories" should be an array of repository definitions, only a single repository was given`}
		}
		repo, ok := repoValue.(*php.Array)
		if !ok {
			encoded, _ := php.JSONEncode(repoValue, 0)

			return nil, &util.UnexpectedValueError{Site: phperr.At("RepositoryFactory.php", 158), Message: `Repository "` + index.String() + `" (` + encoded + `) should be an array, ` + php.TypeName(repoValue) + " given"}
		}
		typeValue, _ := repo.Get("type")
		if typeValue == nil {
			encoded, _ := php.JSONEncode(repo, 0)

			return nil, &util.UnexpectedValueError{Site: phperr.At("RepositoryFactory.php", 161), Message: `Repository "` + index.String() + `" (` + encoded + `) must have a type defined`}
		}
		typ, ok := typeValue.(string)
		if !ok {
			return nil, pkg.ArgumentTypeError(`Composer\Repository\RepositoryManager::createRepository`, 1, "type", "string", typeValue).
				Called(`Composer\Repository\RepositoryManager->createRepository`, phperr.At("RepositoryManager.php", 124), "RepositoryFactory.php", 168)
		}

		name := GenerateRepositoryName(index, repo, repos.Has)
		if typ == "filesystem" {
			path, _ := repo.GetString("json")
			file, err := json.NewFile(path, nil, nil)
			if err != nil {
				return nil, err
			}
			fsRepo, err := NewFilesystemRepository(file, false, nil)
			if err != nil {
				return nil, err
			}
			repos.Set(name, fsRepo)

			continue
		}
		created, err := rm.CreateRepository(typ, repo, index.String())
		if err != nil {
			return nil, phperr.Call(err, `Composer\Repository\RepositoryManager->createRepository`, "RepositoryFactory.php", 168)
		}
		repos.Set(name, created)
	}

	return repos, nil
}

var httpScheme = php.MustCompile(`{^https?://}i`)

// GenerateRepositoryName ports RepositoryFactory::generateRepositoryName:
// the repository's url without http(s):// for a list index, else the
// index, with "2" appended while exists reports the name taken.
func GenerateRepositoryName(index php.Key, repo *php.Array, exists func(name string) bool) string {
	name := index.String()
	if url, ok := repo.Get("url"); index.IsInt() && ok && url != nil {
		// Anchored and fixed-length: Preg::replace cannot fail here.
		name, _, _ = httpScheme.Replace(php.ToString(url), "", -1)
	}
	for exists(name) {
		name += "2"
	}

	return name
}
