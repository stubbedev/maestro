// Ports src/Composer/Repository/Vcs/GitBitbucketDriver.php.

package vcs

import (
	"strings"
	"time"

	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/http"
)

// GitBitbucketDriver ports Composer\Repository\Vcs\GitBitbucketDriver: a
// Bitbucket git repository read through the API (2.0), or through a
// GitDriver when it cannot be read there.
type GitBitbucketDriver struct {
	vcsDriver
	owner      string
	repository string
	hasIssues  bool
	// rootIdentifier is "" for null.
	rootIdentifier string
	// tags and branches are nil until loaded.
	tags, branches *php.Array
	branchesURL    string
	tagsURL        string
	homeURL        string
	website        string
	cloneHTTPSURL  string
	repoData       *php.Array
	fallbackDriver *GitDriver
	// vcsType is "git" or "hg"; nil for null.
	vcsType any
	deps    Deps
}

func newGitBitbucketDriver(repoConfig *php.Array, deps Deps) Driver {
	return NewGitBitbucketDriver(repoConfig, deps)
}

// NewGitBitbucketDriver is new GitBitbucketDriver($repoConfig, $io,
// $config, $httpDownloader, $process).
func NewGitBitbucketDriver(repoConfig *php.Array, deps Deps) *GitBitbucketDriver {
	d := &GitBitbucketDriver{deps: deps}
	d.init(d, repoConfig, deps)

	return d
}

// Class returns the PHP class name.
func (d *GitBitbucketDriver) Class() string { return gitBitbucketDriverType.Class }

var (
	bitbucketRepoURL     = php.MustCompile(`#^https?://bitbucket\.org/([^/]+)/([^/]+?)(?:\.git|/?)?$#i`)
	bitbucketSupportsURL = php.MustCompile(`#^https?://bitbucket\.org/([^/]+)/([^/]+?)(\.git|/?)?$#i`)
	cloneURLUser         = php.MustCompile(`/https:\/\/([^@]+@)?/`)
)

// bitbucketAPI is the base url of the repositories in the Bitbucket API.
const bitbucketAPI = "https://api.bitbucket.org/2.0/repositories/"

// Initialize ports GitBitbucketDriver::initialize.
func (d *GitBitbucketDriver) Initialize() error {
	m, err := match(bitbucketRepoURL, d.url)
	if err != nil {
		return err
	}
	if m == nil {
		return &util.InvalidArgumentError{Site: phperr.At("GitBitbucketDriver.php", 68), Message: "The Bitbucket repository URL " + util.SanitizeURL(d.url) + " is invalid. It must be the HTTPS URL of a Bitbucket repository."}
	}

	d.owner = m.Get(1)
	d.repository = m.Get(2)
	d.originURL = "bitbucket.org"

	return d.newCache(strings.Join([]string{php.ToString(d.config.Get("cache-repo-dir")), d.originURL, d.owner, d.repository}, "/"))
}

// URL ports GitBitbucketDriver::getUrl.
func (d *GitBitbucketDriver) URL() string {
	if d.fallbackDriver != nil {
		return d.fallbackDriver.URL()
	}

	return d.cloneHTTPSURL
}

// getRepoData ports getRepoData(): fetches the repository data; false
// when the driver fell back to git.
func (d *GitBitbucketDriver) getRepoData() (bool, error) {
	resource := bitbucketAPI + d.owner + "/" + d.repository + "?" + php.HTTPBuildQuery("fields", "-project,-owner")

	repoData, err := d.getJSON(resource, true)
	if err != nil {
		return false, err
	}

	if d.fallbackDriver != nil {
		return false, nil
	}

	if err := d.parseCloneURLs(arrayPath(repoData, "links", "clone")); err != nil {
		return false, err
	}

	d.hasIssues = php.ToBool(arrayPath(repoData, "has_issues"))
	d.branchesURL = pathString(repoData, "links", "branches", "href")
	d.tagsURL = pathString(repoData, "links", "tags", "href")
	d.homeURL = pathString(repoData, "links", "html", "href")
	d.website = pathString(repoData, "website")
	d.vcsType = arrayPath(repoData, "scm")

	d.repoData = repoData

	return true, nil
}

// ComposerInformation ports GitBitbucketDriver::getComposerInformation.
func (d *GitBitbucketDriver) ComposerInformation(identifier string) (*php.Array, error) {
	if d.fallbackDriver != nil {
		return d.fallbackDriver.ComposerInformation(identifier)
	}

	if composer, ok := d.infoCache[identifier]; ok {
		return composer, nil
	}

	composer, err := d.cachedBaseComposerInformation(identifier, true)
	if err != nil {
		return nil, err
	}

	if composer != nil {
		// specials for bitbucket
		fixSupport(composer)

		if !isset(composer, "support", "source") {
			source, err := d.sourceURL(identifier)
			if err != nil {
				return nil, err
			}

			supportArray(composer).Set("source", source)
		}

		if !isset(composer, "support", "issues") && d.hasIssues {
			supportArray(composer).Set("issues", "https://"+d.originURL+"/"+d.owner+"/"+d.repository+"/issues")
		}

		if !isset(composer, "homepage") {
			homepage := d.website
			if !php.ToBool(homepage) {
				homepage = d.homeURL
			}

			composer.Set("homepage", homepage)
		}
	}

	d.storeInfo(identifier, composer)

	return composer, nil
}

// sourceURL is the support source url of identifier.
func (d *GitBitbucketDriver) sourceURL(identifier string) (string, error) {
	label, err := searchLabel(identifier, false, identifier, d.Tags, d.Branches)
	if err != nil {
		return "", err
	}

	var hash any

	for _, refs := range []func() (*php.Array, error){d.Tags, d.Branches} {
		r, err := refs()
		if err != nil {
			return "", err
		}

		if r.Has(label) {
			hash, _ = r.Get(label)

			break
		}
	}

	if hash == nil {
		return "https://" + d.originURL + "/" + d.owner + "/" + d.repository + "/src", nil
	}

	return "https://" + d.originURL + "/" + d.owner + "/" + d.repository + "/src/" + php.ToString(hash) + "/?at=" + label, nil
}

// resolveBranch maps a branch name with a "/" to its commit.
func (d *GitBitbucketDriver) resolveBranch(identifier string) (string, error) {
	if !strings.Contains(identifier, "/") {
		return identifier, nil
	}

	branches, err := d.Branches()
	if err != nil {
		return "", err
	}

	if v, _ := branches.Get(identifier); v != nil {
		return php.ToString(v), nil
	}

	return identifier, nil
}

// FileContent ports GitBitbucketDriver::getFileContent.
func (d *GitBitbucketDriver) FileContent(file, identifier string) (string, bool, error) {
	if d.fallbackDriver != nil {
		return d.fallbackDriver.FileContent(file, identifier)
	}

	identifier, err := d.resolveBranch(identifier)
	if err != nil {
		return "", false, err
	}

	response, err := d.fetchWithOAuthCredentials(bitbucketAPI+d.owner+"/"+d.repository+"/src/"+identifier+"/"+file, false)
	if err != nil {
		return "", false, err
	}

	return response.Body(), true, nil
}

// ChangeDate ports GitBitbucketDriver::getChangeDate.
func (d *GitBitbucketDriver) ChangeDate(identifier string) (time.Time, bool, error) {
	if d.fallbackDriver != nil {
		return d.fallbackDriver.ChangeDate(identifier)
	}

	identifier, err := d.resolveBranch(identifier)
	if err != nil {
		return time.Time{}, false, err
	}

	commit, err := d.getJSON(bitbucketAPI+d.owner+"/"+d.repository+"/commit/"+identifier+"?fields=date", false)
	if err != nil {
		return time.Time{}, false, err
	}

	date, err := parseDate(pathString(commit, "date"))

	return date, err == nil, err
}

// Source ports GitBitbucketDriver::getSource.
func (d *GitBitbucketDriver) Source(identifier string) *php.Array {
	if d.fallbackDriver != nil {
		return d.fallbackDriver.Source(identifier)
	}

	return php.ArrayOf("type", d.vcsType, "url", d.URL(), "reference", identifier)
}

// Dist ports GitBitbucketDriver::getDist.
func (d *GitBitbucketDriver) Dist(identifier string) *php.Array {
	if d.fallbackDriver != nil {
		return d.fallbackDriver.Dist(identifier)
	}

	url := "https://bitbucket.org/" + d.owner + "/" + d.repository + "/get/" + identifier + ".zip"

	return php.ArrayOf("type", "zip", "url", url, "reference", identifier, "shasum", "")
}

// Tags ports GitBitbucketDriver::getTags.
func (d *GitBitbucketDriver) Tags() (*php.Array, error) {
	if d.fallbackDriver != nil {
		return d.fallbackDriver.Tags()
	}

	if d.tags == nil {
		tags, err := d.references(d.tagsURL, "values.name,values.target.hash,next")
		if err != nil {
			return nil, err
		}

		d.tags = tags
	}

	return d.tags, nil
}

// Branches ports GitBitbucketDriver::getBranches.
func (d *GitBitbucketDriver) Branches() (*php.Array, error) {
	if d.fallbackDriver != nil {
		return d.fallbackDriver.Branches()
	}

	if d.branches == nil {
		branches, err := d.references(d.branchesURL, "values.name,values.target.hash,values.heads,next")
		if err != nil {
			return nil, err
		}

		d.branches = branches
	}

	return d.branches, nil
}

// references reads every page of a refs list: names to target hashes.
func (d *GitBitbucketDriver) references(url, fields string) (*php.Array, error) {
	refs := php.NewArray()
	resource := url + "?" + php.HTTPBuildQuery("pagelen", "100", "fields", fields, "sort", "-target.date")

	for {
		data, err := d.getJSON(resource, false)
		if err != nil {
			return nil, err
		}

		if values, ok := arrayPath(data, "values").(*php.Array); ok {
			for _, v := range values.All() {
				ref, _ := v.(*php.Array)
				refs.Set(pathString(ref, "name"), pathString(ref, "target", "hash"))
			}
		}

		next := arrayPath(data, "next")
		if !php.ToBool(next) {
			return refs, nil
		}

		resource = php.ToString(next)
	}
}

// getJSON is $this->fetchWithOAuthCredentials($url,
// $fetchingRepoData)->decodeJson() for an array.
func (d *GitBitbucketDriver) getJSON(url string, fetchingRepoData bool) (*php.Array, error) {
	r, err := d.fetchWithOAuthCredentials(url, fetchingRepoData)
	if err != nil {
		return nil, err
	}

	return r.DecodeJSONArray()
}

// fetchWithOAuthCredentials ports fetchWithOAuthCredentials: the request,
// with Bitbucket OAuth authorization and the git fallback of repository
// data requests.
func (d *GitBitbucketDriver) fetchWithOAuthCredentials(url string, fetchingRepoData bool) (*http.Response, error) {
	response, err := d.getContents(url)

	e, ok := asTransportError(err)
	if !ok {
		return response, err
	}

	if e.Code == 403 || e.Code == 404 || (e.Code == 401 && strings.HasPrefix(e.Message, "Could not authenticate against")) {
		bitbucketUtil := http.NewBitbucket(d.io, d.config, d.process, d.httpDownloader, 0)

		if !d.io.HasAuthentication(d.originURL) && bitbucketUtil.AuthorizeOAuth(d.originURL) {
			return d.getContents(url)
		}

		if !d.io.IsInteractive() && fetchingRepoData {
			if err := d.attemptCloneFallback(); err != nil {
				return nil, err
			}

			return dummyResponse(), nil
		}
	}

	return nil, err
}

// generateSSHURL ports generateSshUrl.
func (d *GitBitbucketDriver) generateSSHURL() string {
	return "git@" + d.originURL + ":" + d.owner + "/" + d.repository + ".git"
}

// attemptCloneFallback ports attemptCloneFallback: switch to a GitDriver
// on the SSH url.
func (d *GitBitbucketDriver) attemptCloneFallback() error {
	err := d.setupFallbackDriver(d.generateSSHURL())
	if err != nil && util.IsRuntimeException(err) {
		d.fallbackDriver = nil

		d.io.WriteError("<error>Failed to clone the "+d.generateSSHURL()+" repository, try running in interactive mode"+
			" so that you can enter your Bitbucket OAuth consumer credentials</error>", true, io.Normal)
	}

	return err
}

// setupFallbackDriver ports setupFallbackDriver.
func (d *GitBitbucketDriver) setupFallbackDriver(url string) error {
	d.fallbackDriver = NewGitDriver(php.ArrayOf("url", url), d.deps)

	return d.fallbackDriver.Initialize()
}

// parseCloneURLs ports parseCloneUrls: the https clone url, without the
// username of private repositories.
func (d *GitBitbucketDriver) parseCloneURLs(cloneLinks any) error {
	links, _ := cloneLinks.(*php.Array)
	if links == nil {
		return nil
	}

	for _, v := range links.All() {
		link, _ := v.(*php.Array)
		if arrayPath(link, "name") == "https" {
			// Format: https://(user@)bitbucket.org/{user}/{repo}
			// Strip username from URL (only present in clone URL's for private repositories)
			url, err := replace(cloneURLUser, "https://", pathString(link, "href"))
			if err != nil {
				return err
			}
			d.cloneHTTPSURL = url
		}
	}

	return nil
}

// RootIdentifier ports GitBitbucketDriver::getRootIdentifier.
func (d *GitBitbucketDriver) RootIdentifier() (string, error) {
	if d.fallbackDriver != nil {
		return d.fallbackDriver.RootIdentifier()
	}

	if d.rootIdentifier != "" {
		return d.rootIdentifier, nil
	}

	ok, err := d.getRepoData()
	if err != nil {
		return "", err
	}

	if !ok {
		if d.fallbackDriver == nil {
			return "", &util.LogicError{Site: phperr.At("GitBitbucketDriver.php", 486), Message: "A fallback driver should be setup if getRepoData returns false"}
		}

		return d.fallbackDriver.RootIdentifier()
	}

	if d.vcsType != "git" {
		return "", &util.RuntimeError{Site: phperr.At("GitBitbucketDriver.php", 493), Message: util.SanitizeURL(d.url) + " does not appear to be a git repository, use " +
			d.cloneHTTPSURL + " but remember that Bitbucket no longer supports the mercurial repositories. " +
			"https://bitbucket.org/blog/sunsetting-mercurial-support-in-bitbucket"}
	}

	d.rootIdentifier = "master"
	if name := arrayPath(d.repoData, "mainbranch", "name"); name != nil {
		d.rootIdentifier = php.ToString(name)
	}

	return d.rootIdentifier, nil
}

// gitBitbucketSupports ports GitBitbucketDriver::supports.
func gitBitbucketSupports(_ Deps, url string, _ bool) (bool, error) {
	return matches(bitbucketSupportsURL, url)
}
