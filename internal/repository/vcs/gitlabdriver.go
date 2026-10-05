// Ports src/Composer/Repository/Vcs/GitLabDriver.php.

package vcs

import (
	"strings"
	"time"

	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/http"
)

// GitLabURLRegex is GitLabDriver::URL_REGEX.
const GitLabURLRegex = `#^(?:(?P<scheme>https?)://(?P<domain>.+?)(?::(?P<port>[0-9]+))?/|git@(?P<domain2>[^:]+):)(?P<parts>.+)/(?P<repo>[^/]+?)(?:\.git|/)?$#`

var gitLabURL = php.MustCompile(GitLabURLRegex)

// GitLabDriver ports Composer\Repository\Vcs\GitLabDriver: a GitLab
// project read through the API (v4), or through a GitDriver when it
// cannot be read there.
type GitLabDriver struct {
	vcsDriver
	scheme     string
	namespace  string
	repository string
	// project is the API's project data, nil until fetched.
	project *php.Array
	// commits keeps the commits the API returned, by id.
	commits map[string]*php.Array
	// tags and branches are nil until loaded.
	tags, branches *php.Array
	gitDriver      *GitDriver
	// protocol is the protocol forced for repository URLs ("ssh" or
	// "http"), "" for none.
	protocol             string
	isPrivate            bool
	hasNonstandardOrigin bool
	deps                 Deps
}

func newGitLabDriver(repoConfig *php.Array, deps Deps) Driver {
	return NewGitLabDriver(repoConfig, deps)
}

// NewGitLabDriver is new GitLabDriver($repoConfig, $io, $config,
// $httpDownloader, $process).
func NewGitLabDriver(repoConfig *php.Array, deps Deps) *GitLabDriver {
	d := &GitLabDriver{commits: map[string]*php.Array{}, isPrivate: true, deps: deps}
	d.init(d, repoConfig, deps)

	return d
}

// Class returns the PHP class name.
func (d *GitLabDriver) Class() string { return gitLabDriverType.Class }

var dotGitSuffix = php.MustCompile(`#(\.git)$#`)

// gitLabURLParts are the parts of a GitLab repository URL.
type gitLabURLParts struct {
	scheme        string
	guessedDomain string
	port          string
	hasPort       bool
	urlParts      []string
	repo          string
}

// parseGitLabURL matches url against URL_REGEX.
// The error is the PcreException Preg::isMatch throws.
func parseGitLabURL(url string) (gitLabURLParts, bool, error) {
	m, err := match(gitLabURL, url)
	if err != nil || m == nil {
		return gitLabURLParts{}, false, err
	}

	p := gitLabURLParts{}
	p.scheme, _ = m.Named("scheme")

	var ok bool
	if p.guessedDomain, ok = m.Named("domain"); !ok {
		p.guessedDomain, _ = m.Named("domain2")
	}

	p.port, p.hasPort = m.Named("port")
	parts, _ := m.Named("parts")
	p.urlParts = strings.Split(parts, "/")
	p.repo, _ = m.Named("repo")

	return p, true, nil
}

// Initialize ports GitLabDriver::initialize: SSH urls use https by
// default; set "secure-http": false on the repository config to use http
// instead.
func (d *GitLabDriver) Initialize() error {
	p, ok, err := parseGitLabURL(d.url)
	if err != nil {
		return err
	}
	if !ok {
		return &util.InvalidArgumentError{Message: "The GitLab repository URL " + util.SanitizeURL(d.url) + " is invalid. It must be the HTTP URL of a GitLab project."}
	}

	switch {
	case p.scheme == "https" || p.scheme == "http":
		d.scheme = p.scheme
	case arrayPath(d.repoConfig, "secure-http") == false:
		d.scheme = "http"
	default:
		d.scheme = "https"
	}

	origin, ok := determineOrigin(d.config.Get("gitlab-domains"), p.guessedDomain, &p.urlParts, p.port, p.hasPort)
	if !ok {
		return &util.LogicError{Message: "It should not be possible to create a gitlab driver with an unparsable origin URL (" + util.SanitizeURL(d.url) + ")"}
	}

	d.originURL = origin

	if protocol, ok := d.config.Get("gitlab-protocol").(string); ok {
		// https treated as a synonym for http.
		if protocol != "git" && protocol != "http" && protocol != "https" {
			return &util.RuntimeError{Message: "gitlab-protocol must be one of git, http."}
		}

		d.protocol = "http"
		if protocol == "git" {
			d.protocol = "ssh"
		}
	}

	if strings.ContainsAny(d.originURL, ":/") {
		d.hasNonstandardOrigin = true
	}

	d.namespace = strings.Join(p.urlParts, "/")
	d.repository = replaceInfallible(dotGitSuffix, "", p.repo)

	if err := d.newCache(php.ToString(d.config.Get("cache-repo-dir")) + "/" + d.originURL + "/" + d.namespace + "/" + d.repository); err != nil {
		return err
	}

	return d.fetchProject()
}

// SetHttpDownloader ports setHttpDownloader (for tests).
func (d *GitLabDriver) SetHttpDownloader(httpDownloader http.Getter) {
	d.httpDownloader = httpDownloader
	d.deps.HTTPDownloader = httpDownloader
}

// ComposerInformation ports GitLabDriver::getComposerInformation.
func (d *GitLabDriver) ComposerInformation(identifier string) (*php.Array, error) {
	if d.gitDriver != nil {
		return d.gitDriver.ComposerInformation(identifier)
	}

	if composer, ok := d.infoCache[identifier]; ok {
		return composer, nil
	}

	composer, err := d.cachedBaseComposerInformation(identifier, true)
	if err != nil {
		return nil, err
	}

	if composer != nil {
		// specials for gitlab (this data is only available if authentication is provided)
		fixSupport(composer)

		webURL := pathString(d.project, "web_url")

		if !isset(composer, "support", "source") && isset(d.project, "web_url") {
			label, err := searchLabel(identifier, true, identifier, d.Tags, d.Branches)
			if err != nil {
				return nil, err
			}

			supportArray(composer).Set("source", webURL+"/-/tree/"+label)
		}

		if !isset(composer, "support", "issues") && php.ToBool(arrayPath(d.project, "issues_enabled")) && isset(d.project, "web_url") {
			supportArray(composer).Set("issues", webURL+"/-/issues")
		}

		if !isset(composer, "abandoned") && php.ToBool(arrayPath(d.project, "archived")) {
			composer.Set("abandoned", true)
		}
	}

	d.storeInfo(identifier, composer)

	return composer, nil
}

var sha1Anywhere = php.MustCompile(`{[a-f0-9]{40}}i`)

// FileContent ports GitLabDriver::getFileContent.
func (d *GitLabDriver) FileContent(file, identifier string) (string, bool, error) {
	if d.gitDriver != nil {
		return d.gitDriver.FileContent(file, identifier)
	}

	// Convert the root identifier to a cacheable commit id
	// At most 40 characters per start position: Preg::isMatch cannot throw.
	if isSha, _ := sha1Anywhere.IsMatch(identifier); !isSha {
		branches, err := d.Branches()
		if err != nil {
			return "", false, err
		}

		if v, _ := branches.Get(identifier); v != nil {
			identifier = php.ToString(v)
		}
	}

	resource := d.APIURL() + "/repository/files/" + urlEncodeAll(file) + "/raw?ref=" + identifier

	response, err := d.getContents(resource, false)
	if err != nil {
		if e, ok := asTransportError(err); ok && e.Code == 404 {
			return "", false, nil
		}

		return "", false, err
	}

	return response.Body(), true, nil
}

// ChangeDate ports GitLabDriver::getChangeDate.
func (d *GitLabDriver) ChangeDate(identifier string) (time.Time, bool, error) {
	if d.gitDriver != nil {
		return d.gitDriver.ChangeDate(identifier)
	}

	commit := d.commits[identifier]
	if commit == nil {
		return time.Time{}, false, nil
	}

	date, err := parseDate(pathString(commit, "committed_date"))

	return date, err == nil, err
}

// RepositoryURL ports getRepositoryUrl.
func (d *GitLabDriver) RepositoryURL() string {
	if d.protocol != "" {
		return pathString(d.project, d.protocol+"_url_to_repo")
	}

	if d.isPrivate {
		return pathString(d.project, "ssh_url_to_repo")
	}

	return pathString(d.project, "http_url_to_repo")
}

// URL ports GitLabDriver::getUrl.
func (d *GitLabDriver) URL() string {
	if d.gitDriver != nil {
		return d.gitDriver.URL()
	}

	return pathString(d.project, "web_url")
}

// Dist ports GitLabDriver::getDist.
func (d *GitLabDriver) Dist(identifier string) *php.Array {
	return php.ArrayOf("type", "zip", "url", d.APIURL()+"/repository/archive.zip?sha="+identifier, "reference", identifier, "shasum", "")
}

// Source ports GitLabDriver::getSource.
func (d *GitLabDriver) Source(identifier string) *php.Array {
	if d.gitDriver != nil {
		return d.gitDriver.Source(identifier)
	}

	return php.ArrayOf("type", "git", "url", d.RepositoryURL(), "reference", identifier)
}

// RootIdentifier ports GitLabDriver::getRootIdentifier.
func (d *GitLabDriver) RootIdentifier() (string, error) {
	if d.gitDriver != nil {
		return d.gitDriver.RootIdentifier()
	}

	return pathString(d.project, "default_branch"), nil
}

// Branches ports GitLabDriver::getBranches.
func (d *GitLabDriver) Branches() (*php.Array, error) {
	if d.gitDriver != nil {
		return d.gitDriver.Branches()
	}

	if d.branches == nil {
		branches, err := d.references("branches")
		if err != nil {
			return nil, err
		}

		d.branches = branches
	}

	return d.branches, nil
}

// Tags ports GitLabDriver::getTags.
func (d *GitLabDriver) Tags() (*php.Array, error) {
	if d.gitDriver != nil {
		return d.gitDriver.Tags()
	}

	if d.tags == nil {
		tags, err := d.references("tags")
		if err != nil {
			return nil, err
		}

		d.tags = tags
	}

	return d.tags, nil
}

// APIURL ports getApiUrl: the base URL of the project in the GitLab API.
func (d *GitLabDriver) APIURL() string {
	return d.scheme + "://" + d.originURL + "/api/v4/projects/" + urlEncodeAll(d.namespace) + "%2F" + urlEncodeAll(d.repository)
}

// urlEncodeAll ports urlEncodeAll(): every character but ASCII letters,
// digits, - and _ percent-encoded (rawurlencode() keeps ".").
func urlEncodeAll(s string) string {
	const hex = "0123456789ABCDEF"

	var b strings.Builder

	for i := range len(s) {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' {
			b.WriteByte(c)
		} else {
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&0x0f])
		}
	}

	return b.String()
}

// references ports getReferences(): named references (tags or branches)
// to their commit ids.
func (d *GitLabDriver) references(typ string) (*php.Array, error) {
	const perPage = 100

	resource := d.APIURL() + "/repository/" + typ + "?per_page=100"
	references := php.NewArray()

	for resource != "" {
		response, err := d.getContents(resource, false)
		if err != nil {
			return nil, err
		}

		data, err := response.DecodeJSONArray()
		if err != nil {
			return nil, err
		}

		if data == nil {
			data = php.NewArray()
		}

		for _, v := range data.All() {
			datum, _ := v.(*php.Array)
			id := pathString(datum, "commit", "id")
			references.Set(pathString(datum, "name"), id)

			// Keep the last commit date of a reference to avoid
			// unnecessary API call when retrieving the composer file.
			commit, _ := arrayPath(datum, "commit").(*php.Array)
			d.commits[id] = commit
		}

		resource = ""
		if data.Len() >= perPage {
			var err error
			if resource, err = nextPage(response); err != nil {
				return nil, err
			}
		}
	}

	return references, nil
}

// fetchProject ports fetchProject.
func (d *GitLabDriver) fetchProject() error {
	if d.project != nil {
		return nil
	}

	// we need to fetch the default branch from the api
	project, err := d.getJSON(d.APIURL(), true)
	if err != nil {
		return err
	}

	d.project = project

	if visibility := arrayPath(project, "visibility"); visibility != nil {
		d.isPrivate = visibility != "public"
	} else {
		// client is not authenticated, therefore repository has to be public
		d.isPrivate = false
	}

	return nil
}

// getJSON is $this->getContents($url, $fetchingRepoData)->decodeJson()
// for an array.
func (d *GitLabDriver) getJSON(url string, fetchingRepoData bool) (*php.Array, error) {
	r, err := d.getContents(url, fetchingRepoData)
	if err != nil {
		return nil, err
	}

	return r.DecodeJSONArray()
}

// attemptCloneFallback ports attemptCloneFallback: switch to a GitDriver.
func (d *GitLabDriver) attemptCloneFallback() error {
	url := d.generateSSHURL()
	if !d.isPrivate {
		url = d.generatePublicURL()
	}

	// If this repository may be private and we
	// cannot ask for authentication credentials (because we
	// are not interactive) then we fallback to GitDriver.
	err := d.setupGitDriver(url)
	if err != nil && util.IsRuntimeException(err) {
		d.gitDriver = nil

		d.io.WriteError("<error>Failed to clone the "+util.SanitizeURL(url)+" repository, try running in interactive mode so that you can enter your credentials</error>", true, io.Normal)
	}

	return err
}

// generateSSHURL ports generateSshUrl.
func (d *GitLabDriver) generateSSHURL() string {
	if d.hasNonstandardOrigin {
		return "ssh://git@" + d.originURL + "/" + d.namespace + "/" + d.repository + ".git"
	}

	return "git@" + d.originURL + ":" + d.namespace + "/" + d.repository + ".git"
}

// generatePublicURL ports generatePublicUrl.
func (d *GitLabDriver) generatePublicURL() string {
	return d.scheme + "://" + d.originURL + "/" + d.namespace + "/" + d.repository + ".git"
}

// setupGitDriver ports setupGitDriver.
func (d *GitLabDriver) setupGitDriver(url string) error {
	d.gitDriver = NewGitDriver(php.ArrayOf("url", url), d.deps)

	return d.gitDriver.Initialize()
}

// getContents ports GitLabDriver::getContents: the request, with GitLab
// authorization and the git fallback of project data requests.
func (d *GitLabDriver) getContents(url string, fetchingRepoData bool) (*http.Response, error) {
	response, err := d.tryGetContents(url, fetchingRepoData)

	e, ok := asTransportError(err)
	if !ok {
		return response, err
	}

	gitLabUtil := http.NewGitLab(d.io, d.config, d.process, d.httpDownloader)

	switch e.Code {
	case 401, 404:
		// try to authorize only if we are fetching the main /repos/foo/bar data, otherwise it must be a real 404
		if !fetchingRepoData {
			return nil, err
		}

		if gitLabUtil.AuthorizeOAuth(d.originURL) {
			return d.vcsDriver.getContents(url)
		}

		if gitLabUtil.IsOAuthExpired(d.originURL) {
			refreshed, rerr := gitLabUtil.AuthorizeOAuthRefresh(d.scheme, d.originURL)
			if rerr != nil {
				return nil, rerr
			}

			if refreshed {
				return d.vcsDriver.getContents(url)
			}
		}

		if !d.io.IsInteractive() {
			if err := d.attemptCloneFallback(); err != nil {
				return nil, err
			}

			return dummyResponse(), nil
		}

		d.io.WriteError("<warning>Failed to download "+d.namespace+"/"+d.repository+":"+e.Message+"</warning>", true, io.Normal)

		if _, err := gitLabUtil.AuthorizeOAuthInteractively(d.scheme, d.originURL, "Your credentials are required to fetch private repository metadata (<info>"+util.SanitizeURL(d.url)+"</info>)"); err != nil {
			return nil, err
		}

		return d.vcsDriver.getContents(url)

	case 403:
		if !d.io.HasAuthentication(d.originURL) && gitLabUtil.AuthorizeOAuth(d.originURL) {
			return d.vcsDriver.getContents(url)
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

// tryGetContents is the try block of getContents: the request, and the
// checks of project data responses, which throw TransportExceptions the
// catch block handles.
func (d *GitLabDriver) tryGetContents(url string, fetchingRepoData bool) (*http.Response, error) {
	response, err := d.vcsDriver.getContents(url)
	if err != nil || !fetchingRepoData {
		return response, err
	}

	json, err := response.DecodeJSONArray()
	if err != nil {
		return nil, err
	}

	// Accessing the API with a token with Guest (10) or Planner (15) access will return
	// more data than unauthenticated access but no default_branch data
	// accessing files via the API will then also fail
	if !isset(json, "default_branch") && isset(json, "permissions") {
		d.isPrivate = arrayPath(json, "visibility") != "public"

		moreThanGuestAccess := false

		// Check both access levels (e.g. project, group)
		// - value will be null if no access is set
		// - value will be array with key access_level if set
		if permissions, ok := arrayPath(json, "permissions").(*php.Array); ok {
			for _, permission := range permissions.All() {
				if p, ok := permission.(*php.Array); ok && p.Len() > 0 && php.ToInt(arrayPath(p, "access_level")) >= 20 {
					moreThanGuestAccess = true
				}
			}
		}

		if !moreThanGuestAccess {
			d.io.WriteError("<warning>GitLab token with Guest or Planner only access detected</warning>", true, io.Normal)

			if err := d.attemptCloneFallback(); err != nil {
				return nil, err
			}

			return dummyResponse(), nil
		}
	}

	// force auth as the unauthenticated version of the API is broken
	if !isset(json, "default_branch") {
		// GitLab allows you to disable the repository inside a project to use a project only for issues and wiki
		if arrayPath(json, "repository_access_level") == "disabled" {
			return nil, util.NewTransportError("The GitLab repository is disabled in the project", 400)
		}

		if php.ToBool(arrayPath(json, "id")) {
			d.isPrivate = false
		}

		return nil, util.NewTransportError("GitLab API seems to not be authenticated as it did not return a default_branch", 401)
	}

	return response, nil
}

// gitLabSupports ports GitLabDriver::supports: whether gitlab-domains
// names the host of url.
func gitLabSupports(deps Deps, url string, _ bool) (bool, error) {
	p, ok, err := parseGitLabURL(url)
	if err != nil || !ok {
		return false, err
	}

	_, ok = determineOrigin(deps.Config.Get("gitlab-domains"), p.guessedDomain, &p.urlParts, p.port, p.hasPort)

	return ok, nil
}

// RepoData ports getRepoData: the <gitlab-api>/projects/<owner>/<repo>
// result.
func (d *GitLabDriver) RepoData() (*php.Array, error) {
	if err := d.fetchProject(); err != nil {
		return nil, err
	}

	return d.project, nil
}

var portNumber = php.MustCompile(`{:\d+}`)

// determineOrigin ports determineOrigin(): the origin of a GitLab url
// among the configured domains, consuming the path parts it includes;
// false when there is none.
func determineOrigin(configuredDomains any, guessedDomain string, urlParts *[]string, port string, hasPort bool) (string, bool) {
	domains, _ := configuredDomains.(*php.Array)
	if domains == nil {
		domains = php.NewArray()
	}

	configured := func(domain string) bool { return php.InArray(domain, domains, false) }

	guessedDomain = php.Strtolower(guessedDomain)

	if configured(guessedDomain) || (hasPort && configured(guessedDomain+":"+port)) {
		if hasPort {
			return guessedDomain + ":" + port, true
		}

		return guessedDomain, true
	}

	if hasPort {
		guessedDomain += ":" + port
	}

	for len(*urlParts) > 0 {
		part := (*urlParts)[0]
		*urlParts = (*urlParts)[1:]
		guessedDomain += "/" + part

		if configured(guessedDomain) || (hasPort && configured(replaceInfallible(portNumber, "", guessedDomain))) {
			return guessedDomain, true
		}
	}

	return "", false
}
