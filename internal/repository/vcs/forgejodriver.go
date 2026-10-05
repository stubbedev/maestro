// Ports src/Composer/Repository/Vcs/ForgejoDriver.php.

package vcs

import (
	"time"

	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/http"
)

// ForgejoDriver ports Composer\Repository\Vcs\ForgejoDriver: a Forgejo
// (Codeberg, ...) repository read through the API, or through a GitDriver
// when it cannot be read there.
type ForgejoDriver struct {
	vcsDriver
	forgejoURL     *util.ForgejoURL
	repositoryData *util.ForgejoRepositoryData
	gitDriver      *GitDriver
	// tags and branches are nil until loaded.
	tags, branches *php.Array
	deps           Deps
}

func newForgejoDriver(repoConfig *php.Array, deps Deps) Driver {
	return NewForgejoDriver(repoConfig, deps)
}

// NewForgejoDriver is new ForgejoDriver($repoConfig, $io, $config,
// $httpDownloader, $process).
func NewForgejoDriver(repoConfig *php.Array, deps Deps) *ForgejoDriver {
	d := &ForgejoDriver{deps: deps}
	d.init(d, repoConfig, deps)

	return d
}

// Class returns the PHP class name.
func (d *ForgejoDriver) Class() string { return forgejoDriverType.Class }

// Initialize ports ForgejoDriver::initialize.
func (d *ForgejoDriver) Initialize() error {
	forgejoURL, err := util.CreateForgejoURL(d.url)
	if err != nil {
		return err
	}

	d.forgejoURL = forgejoURL
	d.originURL = forgejoURL.OriginURL

	if err := d.newCache(php.ToString(d.config.Get("cache-repo-dir")) + "/" + d.originURL + "/" + forgejoURL.Owner + "/" + forgejoURL.Repository); err != nil {
		return err
	}

	return d.fetchRepositoryData()
}

// FileContent ports ForgejoDriver::getFileContent.
func (d *ForgejoDriver) FileContent(file, identifier string) (string, bool, error) {
	if d.gitDriver != nil {
		return d.gitDriver.FileContent(file, identifier)
	}

	resource, err := d.getJSON(d.forgejoURL.APIURL+"/contents/"+file+"?ref="+http.Urlencode(identifier), false)
	if err != nil {
		return "", false, err
	}

	if resource != nil && resource.Len() == 0 {
		return "[]", true, nil
	}

	// The Forgejo contents API only returns files up to 1MB as base64 encoded files
	// larger files either need be fetched with a raw accept header or by using the git blob endpoint
	if content := arrayPath(resource, "content"); (content == nil || content == "") && arrayPath(resource, "encoding") == "none" && isset(resource, "git_url") {
		resource, err = d.getJSON(pathString(resource, "git_url"), false)
		if err != nil {
			return "", false, err
		}
	}

	if content, ok := base64Content(resource, true); ok {
		return content, true, nil
	}

	return "", false, &util.RuntimeError{Message: "Could not retrieve " + file + " for " + identifier}
}

// ChangeDate ports ForgejoDriver::getChangeDate.
func (d *ForgejoDriver) ChangeDate(identifier string) (time.Time, bool, error) {
	if d.gitDriver != nil {
		return d.gitDriver.ChangeDate(identifier)
	}

	commit, err := d.getJSON(d.forgejoURL.APIURL+"/git/commits/"+http.Urlencode(identifier)+"?verification=false&files=false", false)
	if err != nil {
		return time.Time{}, false, err
	}

	date, err := parseDate(pathString(commit, "commit", "committer", "date"))

	return date, err == nil, err
}

// RootIdentifier ports ForgejoDriver::getRootIdentifier.
func (d *ForgejoDriver) RootIdentifier() (string, error) {
	if d.gitDriver != nil {
		return d.gitDriver.RootIdentifier()
	}

	return d.repositoryData.DefaultBranch, nil
}

// Branches ports ForgejoDriver::getBranches.
func (d *ForgejoDriver) Branches() (*php.Array, error) {
	if d.gitDriver != nil {
		return d.gitDriver.Branches()
	}

	if d.branches == nil {
		branches, err := d.paginate(d.forgejoURL.APIURL+"/branches?per_page=100", "id")
		if err != nil {
			return nil, err
		}

		d.branches = branches
	}

	return d.branches, nil
}

// Tags ports ForgejoDriver::getTags.
func (d *ForgejoDriver) Tags() (*php.Array, error) {
	if d.gitDriver != nil {
		return d.gitDriver.Tags()
	}

	if d.tags == nil {
		tags, err := d.paginate(d.forgejoURL.APIURL+"/tags?per_page=100", "sha")
		if err != nil {
			return nil, err
		}

		d.tags = tags
	}

	return d.tags, nil
}

// paginate maps the names of the references of every page of a list to
// their $ref['commit'][$commitKey].
func (d *ForgejoDriver) paginate(resource, commitKey string) (*php.Array, error) {
	refs := php.NewArray()

	for resource != "" {
		response, err := d.getContents(resource, false)
		if err != nil {
			return nil, err
		}

		data, err := response.DecodeJSON()
		if err != nil {
			return nil, err
		}

		if data == nil {
			break
		}

		if list, ok := data.(*php.Array); ok {
			for _, v := range list.All() {
				ref, _ := v.(*php.Array)
				refs.Set(pathString(ref, "name"), pathString(ref, "commit", commitKey))
			}
		}

		resource = nextPage(response)
	}

	return refs, nil
}

// Dist ports ForgejoDriver::getDist.
func (d *ForgejoDriver) Dist(identifier string) *php.Array {
	return php.ArrayOf("type", "zip", "url", d.forgejoURL.APIURL+"/archive/"+identifier+".zip", "reference", identifier, "shasum", "")
}

// ComposerInformation ports ForgejoDriver::getComposerInformation.
func (d *ForgejoDriver) ComposerInformation(identifier string) (*php.Array, error) {
	if d.gitDriver != nil {
		return d.gitDriver.ComposerInformation(identifier)
	}

	if composer, ok := d.infoCache[identifier]; ok {
		return composer, nil
	}

	composer, err := d.cachedBaseComposerInformation(identifier, false)
	if err != nil {
		return nil, err
	}

	if composer != nil {
		// specials for forgejo
		fixSupport(composer)

		if !isset(composer, "support", "source") {
			source, err := d.sourceURL(identifier)
			if err != nil {
				return nil, err
			}

			supportArray(composer).Set("source", source)
		}

		if !isset(composer, "support", "issues") && d.repositoryData.HasIssues {
			supportArray(composer).Set("issues", d.repositoryData.HTMLURL+"/issues")
		}

		if !isset(composer, "abandoned") && d.repositoryData.IsArchived {
			composer.Set("abandoned", true)
		}
	}

	d.storeInfo(identifier, composer)

	return composer, nil
}

// sourceURL is the support source url of identifier: its tag, branch or
// commit page.
func (d *ForgejoDriver) sourceURL(identifier string) (string, error) {
	tags, err := d.Tags()
	if err != nil {
		return "", err
	}

	if label, ok := php.ArraySearch(identifier, tags, true); ok {
		return d.repositoryData.HTMLURL + "/tag/" + label.String(), nil
	}

	branches, err := d.Branches()
	if err != nil {
		return "", err
	}

	if label, ok := php.ArraySearch(identifier, branches, true); ok {
		return d.repositoryData.HTMLURL + "/branch/" + label.String(), nil
	}

	return d.repositoryData.HTMLURL + "/commit/" + identifier, nil
}

// Source ports ForgejoDriver::getSource.
func (d *ForgejoDriver) Source(identifier string) *php.Array {
	if d.gitDriver != nil {
		return d.gitDriver.Source(identifier)
	}

	return php.ArrayOf("type", "git", "url", d.URL(), "reference", identifier)
}

// URL ports ForgejoDriver::getUrl.
func (d *ForgejoDriver) URL() string {
	if d.gitDriver != nil {
		return d.gitDriver.URL()
	}

	if d.repositoryData.IsPrivate {
		return d.repositoryData.SSHURL
	}

	return d.repositoryData.HTTPCloneURL
}

// forgejoSupports ports ForgejoDriver::supports.
func forgejoSupports(deps Deps, url string, _ bool) (bool, error) {
	forgejoURL := util.TryForgejoURL(url)
	if forgejoURL == nil {
		return false, nil
	}

	domains, _ := deps.Config.Get("forgejo-domains").(*php.Array)

	return domains != nil && php.InArray(php.Strtolower(forgejoURL.OriginURL), domains, true), nil
}

// setupGitDriver ports setupGitDriver.
func (d *ForgejoDriver) setupGitDriver(url string) error {
	d.gitDriver = NewGitDriver(php.ArrayOf("url", url), d.deps)

	return d.gitDriver.Initialize()
}

// fetchRepositoryData ports fetchRepositoryData.
func (d *ForgejoDriver) fetchRepositoryData() error {
	if d.repositoryData != nil {
		return nil
	}

	data, err := d.getJSON(d.forgejoURL.APIURL, true)
	if err != nil {
		return err
	}

	if data == nil && d.gitDriver != nil {
		return nil
	}

	if data == nil {
		data = php.NewArray()
	}

	d.repositoryData, err = util.ForgejoRepositoryDataFromRemote(data)

	return err
}

// getJSON is $this->getContents($url, $fetchingRepoData)->decodeJson()
// for an array.
func (d *ForgejoDriver) getJSON(url string, fetchingRepoData bool) (*php.Array, error) {
	r, err := d.getContents(url, fetchingRepoData)
	if err != nil {
		return nil, err
	}

	return r.DecodeJSONArray()
}

// getContents ports ForgejoDriver::getContents: the request, with Forgejo
// authorization and the git fallback of repository data requests.
func (d *ForgejoDriver) getContents(url string, fetchingRepoData bool) (*http.Response, error) {
	forgejo := http.NewForgejo(d.io, d.config, d.httpDownloader)

	response, err := d.vcsDriver.getContents(url)

	e, ok := asTransportError(err)
	if !ok {
		return response, err
	}

	switch e.Code {
	case 401, 403, 404, 429:
		if !fetchingRepoData {
			return nil, err
		}

		if !d.io.IsInteractive() {
			if err := d.attemptCloneFallback(); err != nil {
				return nil, err
			}

			return dummyResponse(), nil
		}

		if !d.io.HasAuthentication(d.originURL) {
			message := ""
			if e.Code == 429 {
				message = "API limit exhausted. Enter your Forgejo credentials to get a larger API limit (<info>" + util.SanitizeURL(d.url) + "</info>)"
			}

			authorized, aerr := forgejo.AuthorizeOAuthInteractively(d.forgejoURL.OriginURL, message)
			if aerr != nil {
				return nil, aerr
			}

			if authorized {
				return d.vcsDriver.getContents(url)
			}
		}
	}

	return nil, err
}

// attemptCloneFallback ports attemptCloneFallback: switch to a GitDriver
// on the SSH url.
func (d *ForgejoDriver) attemptCloneFallback() error {
	// If this repository may be private (hard to say for sure,
	// Forgejo returns 404 for private repositories) and we
	// cannot ask for authentication credentials (because we
	// are not interactive) then we fallback to GitDriver.
	err := d.setupGitDriver(d.forgejoURL.GenerateSSHURL())
	if err != nil && util.IsRuntimeException(err) {
		d.gitDriver = nil

		d.io.WriteError("<error>Failed to clone the "+d.forgejoURL.GenerateSSHURL()+" repository, try running in interactive mode so that you can enter your Forgejo credentials</error>", true, io.Normal)
	}

	return err
}
