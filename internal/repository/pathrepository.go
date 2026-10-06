// Ports src/Composer/Repository/PathRepository.php.

package repository

import (
	"crypto/sha1" //nolint:gosec // PathRepository's references are sha1 hashes, as Composer computes them
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"

	"github.com/stubbedev/maestro/internal/classmap"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/json"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/loader"
	"github.com/stubbedev/maestro/internal/pkg/version"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/vcs"
)

// PathRepository ports Composer\Repository\PathRepository: local packages,
// not necessarily under their own VCS, found at a path or glob ("url"),
// installed by symlink or copy. Its "options" are symlink, relative,
// reference ("auto", "config" or "none") and versions (package name =>
// version).
type PathRepository struct {
	ArrayRepository
	loader         *loader.ArrayLoader
	versionGuesser *version.VersionGuesser
	url            string
	repoConfig     *php.Array
	process        Process
	options        *php.Array
}

var _ ConfigurableRepository = (*PathRepository)(nil)

// NewPathRepository ports new PathRepository($repoConfig, $io, $config,
// $httpDownloader, $dispatcher, $process): process runs git (and the
// version guesser).
func NewPathRepository(repoConfig *php.Array, out io.IO, process Process) (*PathRepository, error) {
	r := &PathRepository{}
	r.bind(r, r)

	urlValue, _ := repoConfig.Get("url")
	if urlValue == nil {
		return nil, &util.RuntimeError{Message: "You must specify the `url` configuration for the path repository"}
	}
	rawURL, ok := urlValue.(string)
	if !ok {
		return nil, pkg.ArgumentTypeError(`Composer\Util\Platform::expandPath`, 1, "path", "string", urlValue)
	}

	r.loader = loader.NewArrayLoader(nil, true)
	url, err := util.ExpandPath(rawURL)
	if err != nil {
		return nil, err
	}
	r.url = url
	r.process = process
	r.versionGuesser = version.NewVersionGuesser(NewGuesserProcess(process), out)
	r.repoConfig = repoConfig
	// $this->options = $repoConfig['options'] ?? [] (an untyped property)
	options, _ := repoConfig.Get("options")
	if a, ok := options.(*php.Array); ok {
		r.options = a.Clone()
	}
	// if (!isset($this->options['relative'])) $this->options['relative'] =
	// ...: options that are not an array fail there, or, false, become
	// one after a deprecation notice
	var relative any
	if r.options != nil {
		relative, _ = r.options.Get("relative")
	}
	if relative == nil {
		if r.options == nil {
			arr, _, deprecated, e := php.WritableArray(options)
			if e != nil {
				return nil, e
			}
			if deprecated {
				util.RaiseDeprecation(php.FalseToArrayDeprecation)
			}
			r.options = arr
		}
		r.options.Set("relative", !util.IsAbsolutePath(r.url))
	}

	return r, nil
}

// Class returns the PHP class name.
func (r *PathRepository) Class() string { return `Composer\Repository\PathRepository` }

// RepoName ports PathRepository::getRepoName.
func (r *PathRepository) RepoName() string {
	url, _ := r.repoConfig.GetString("url")

	return "path repo (" + util.SanitizeURL(url) + ")"
}

// RepoConfig ports PathRepository::getRepoConfig.
func (r *PathRepository) RepoConfig() *php.Array { return r.repoConfig }

// pathWildcard does constant work per start position, so Preg::isMatch
// cannot fail on it.
var pathWildcard = php.MustCompile(`{[*{}]}`)

// initialize ports PathRepository::initialize: it reads the composer.json
// of each directory the url matches.
func (r *PathRepository) initialize() error {
	r.baseInitialize()

	urlMatches := r.urlMatches()

	if len(urlMatches) == 0 {
		if wildcard, _ := pathWildcard.IsMatch(r.url); wildcard {
			url := r.url
			for {
				if wildcard, _ := pathWildcard.IsMatch(url); !wildcard {
					break
				}
				url = util.Dirname(url)
			}
			// the parent directory before any wildcard exists, so we assume it is correctly configured but simply empty
			if info, err := os.Stat(url); err == nil && info.IsDir() {
				return nil
			}
		}

		return &util.RuntimeError{Message: "The `url` supplied for the path (" + util.SanitizeURL(r.url) + ") repository does not exist"}
	}

	reference := "auto"
	if v, _ := r.options.Get("reference"); v != nil {
		reference = php.ToString(v)
	}

	for _, url := range urlMatches {
		path := util.Realpath(url) + "/"
		composerFilePath := path + "composer.json"

		content, err := os.ReadFile(composerFilePath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}

			return err
		}
		jsonContent := string(content)
		decoded, err := json.ParseJSON(jsonContent, composerFilePath)
		if err != nil {
			return err
		}
		packageData, ok := decoded.(*php.Array)
		if !ok {
			return &util.ErrorException{Message: "Cannot use a scalar value as an array"}
		}
		dist := php.ArrayOf("type", "path", "url", url)
		packageData.Set("dist", dist)
		switch reference {
		case "none":
			dist.Set("reference", nil)
		case "config", "auto":
			dist.Set("reference", sha1Hex(jsonContent+php.Serialize(r.options)))
		}

		// copy symlink/relative options to transport options
		packageData.Set("transport-options", php.ArrayIntersectKey(r.options, php.ArrayOf("symlink", true, "relative", true)))
		// use the version provided as option if available
		if name, _ := packageData.Get("name"); name != nil {
			if versions, ok := r.options.GetArray("versions"); ok {
				if v, _ := versions.Get(php.ToKey(name)); v != nil {
					packageData.Set("version", v)
				}
			}
		}

		// carry over the root package version if this path repo is in the same git repository as root package
		if v, _ := packageData.Get("version"); v == nil {
			if rootVersion, _ := util.GetEnv("COMPOSER_ROOT_VERSION"); php.ToBool(rootVersion) {
				same, err := r.sameGitHead(path)
				if err != nil {
					return err
				}
				if same {
					v, err := r.versionGuesser.RootVersionFromEnv()
					if err != nil {
						return err
					}
					packageData.Set("version", v)
				}
			}
		}

		if err := r.gitReference(reference, path, dist); err != nil {
			return err
		}

		if v, _ := packageData.Get("version"); v == nil {
			versionData, err := r.versionGuesser.GuessVersion(packageData, path)
			if err != nil {
				return err
			}
			if versionData != nil && php.ToBool(versionData.PrettyVersion) {
				// if there is a feature branch detected, we add a second packages with the feature branch version
				if php.ToBool(versionData.FeaturePrettyVersion.S) {
					packageData.Set("version", versionData.FeaturePrettyVersion.S)
					p, err := r.loader.Load(packageData, pkg.ClassCompletePackage)
					if err != nil {
						return err
					}
					if err := r.hooks.addPackage(p); err != nil {
						return err
					}
				}

				packageData.Set("version", versionData.PrettyVersion)
			} else {
				packageData.Set("version", "dev-main")
			}
		}

		p, err := r.loader.Load(packageData, pkg.ClassCompletePackage)
		if err == nil {
			err = r.hooks.addPackage(p)
		}
		if err != nil {
			return &wrappedError{err: &util.RuntimeError{Message: "Failed loading the package in " + composerFilePath}, previous: err}
		}
	}

	return nil
}

// sameGitHead reports whether the checkout at path is at the HEAD of the
// working directory's.
func (r *PathRepository) sameGitHead(path string) (bool, error) {
	var ref1, ref2 string
	code, err := r.process.Execute(util.Cmd("git", "rev-parse", "HEAD"), &ref1, path)
	if err != nil || code != 0 {
		return false, err
	}
	code, err = r.process.Execute(util.Cmd("git", "rev-parse", "HEAD"), &ref2, "")
	if err != nil || code != 0 {
		return false, err
	}

	return ref1 == ref2, nil
}

// gitReference sets the dist reference to the commit checked out at path
// when the reference strategy is "auto" and path is a git checkout.
func (r *PathRepository) gitReference(reference, path string, dist *php.Array) error {
	flags, err := vcs.GetNoShowSignatureFlags(r.process)
	if err != nil {
		return err
	}
	command, err := vcs.BuildRevListCommand(r.process, append([]string{"-n1", "--format=%H", "HEAD"}, flags...))
	if err != nil {
		return err
	}
	if reference != "auto" {
		return nil
	}
	if info, err := os.Stat(path + "/.git"); err != nil || !info.IsDir() {
		return nil
	}
	var output string
	code, err := r.process.Execute(util.Cmd(command...), &output, path)
	if err != nil || code != 0 {
		return err
	}
	parsed, err := vcs.ParseRevListOutput(output, r.process)
	if err != nil {
		return err
	}
	dist.Set("reference", php.Trim(parsed))

	return nil
}

// urlMatches ports PathRepository::getUrlMatches: the paths the url
// matches, as a glob with braces, without trailing slashes.
func (r *PathRepository) urlMatches() []string {
	matches := classmap.GlobOnlyDir(r.url)
	for i, m := range matches {
		// Ensure environment-specific path separators are normalized to URL separators
		matches[i] = strings.TrimRight(filepath.ToSlash(m), "/")
	}

	return matches
}

func sha1Hex(s string) string {
	sum := sha1.Sum([]byte(s)) //nolint:gosec // see the import

	return hex.EncodeToString(sum[:])
}
