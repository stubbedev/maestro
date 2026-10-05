// Ports src/Composer/Repository/Vcs/GitHubDriver.php.

package vcs

import (
	"strconv"
	"strings"
	"time"

	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/http"
)

// GitHubDriver ports Composer\Repository\Vcs\GitHubDriver: a GitHub
// repository read through the API, or through a GitDriver when the API is
// disabled or the repository is private and cannot be authorized.
type GitHubDriver struct {
	vcsDriver
	owner      string
	repository string
	// tags and branches are nil until loaded.
	tags, branches *php.Array
	rootIdentifier string
	repoData       *php.Array
	hasIssues      bool
	isPrivate      bool
	isArchived     bool
	// fundingInfo is the funding list once fundingKnown; nil for false.
	fundingInfo      *php.Array
	fundingKnown     bool
	allowGitFallback bool
	gitDriver        *GitDriver
	deps             Deps
}

func newGitHubDriver(repoConfig *php.Array, deps Deps) Driver {
	return NewGitHubDriver(repoConfig, deps)
}

// NewGitHubDriver is new GitHubDriver($repoConfig, $io, $config,
// $httpDownloader, $process).
func NewGitHubDriver(repoConfig *php.Array, deps Deps) *GitHubDriver {
	d := &GitHubDriver{allowGitFallback: true, deps: deps}
	d.init(d, repoConfig, deps)

	return d
}

// Class returns the PHP class name.
func (d *GitHubDriver) Class() string { return gitHubDriverType.Class }

var (
	gitHubRepoURL     = php.MustCompile(`#^(?:(?:https?|git)://([^/]+)/|git@([^:]+):/?)([^/]+)/([^/]+?)(?:\.git|/)?$#`)
	gitHubSupportsURL = php.MustCompile(`#^((?:https?|git)://([^/]+)/|git@([^:]+):/?)([^/]+)/([^/]+?)(?:\.git|/)?$#`)
	wwwPrefix         = php.MustCompile(`{^www\.}i`)
)

// Initialize ports GitHubDriver::initialize.
func (d *GitHubDriver) Initialize() error {
	m, err := match(gitHubRepoURL, d.url)
	if err != nil {
		return err
	}
	if m == nil {
		return &util.InvalidArgumentError{Site: phperr.At("GitHubDriver.php", 66), Message: "The GitHub repository URL " + util.SanitizeURL(d.url) + " is invalid."}
	}

	d.owner = m.Get(3)
	d.repository = m.Get(4)

	origin, ok := m.Group(1)
	if !ok {
		origin = m.Get(2)
	}

	d.originURL = php.Strtolower(origin)
	if d.originURL == "www.github.com" {
		d.originURL = "github.com"
	}

	if err := d.newCache(php.ToString(d.config.Get("cache-repo-dir")) + "/" + d.originURL + "/" + d.owner + "/" + d.repository); err != nil {
		return err
	}

	if v, _ := d.repoConfig.Get("allow-git-fallback"); v == false {
		d.allowGitFallback = false
	}

	if v, _ := d.repoConfig.Get("no-api"); d.config.Get("use-github-api") == false || php.ToBool(v) {
		return d.setupGitDriver(d.url)
	}

	return d.fetchRootIdentifier()
}

// RepositoryURL ports getRepositoryUrl.
func (d *GitHubDriver) RepositoryURL() string {
	return "https://" + d.originURL + "/" + d.owner + "/" + d.repository
}

// RootIdentifier ports GitHubDriver::getRootIdentifier.
func (d *GitHubDriver) RootIdentifier() (string, error) {
	if d.gitDriver != nil {
		return d.gitDriver.RootIdentifier()
	}

	return d.rootIdentifier, nil
}

// URL ports GitHubDriver::getUrl.
func (d *GitHubDriver) URL() string {
	if d.gitDriver != nil {
		return d.gitDriver.URL()
	}

	return "https://" + d.originURL + "/" + d.owner + "/" + d.repository + ".git"
}

// apiURL ports getApiUrl.
func (d *GitHubDriver) apiURL() string {
	if d.originURL == "github.com" {
		return "https://api.github.com"
	}

	return "https://" + d.originURL + "/api/v3"
}

// repoAPIURL is the API url of the repository.
func (d *GitHubDriver) repoAPIURL() string {
	return d.apiURL() + "/repos/" + d.owner + "/" + d.repository
}

// Source ports GitHubDriver::getSource.
func (d *GitHubDriver) Source(identifier string) *php.Array {
	if d.gitDriver != nil {
		return d.gitDriver.Source(identifier)
	}

	url := d.URL()
	if d.isPrivate {
		// Private GitHub repositories should be accessed using the
		// SSH version of the URL.
		url = d.generateSSHURL()
	}

	return php.ArrayOf("type", "git", "url", url, "reference", identifier)
}

// Dist ports GitHubDriver::getDist.
func (d *GitHubDriver) Dist(identifier string) *php.Array {
	return php.ArrayOf("type", "zip", "url", d.repoAPIURL()+"/zipball/"+identifier, "reference", identifier, "shasum", "")
}

// ComposerInformation ports GitHubDriver::getComposerInformation.
func (d *GitHubDriver) ComposerInformation(identifier string) (*php.Array, error) {
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
		// specials for github
		fixSupport(composer)

		if !isset(composer, "support", "source") {
			label, err := searchLabel(identifier, false, identifier, d.Tags, d.Branches)
			if err != nil {
				return nil, err
			}

			supportArray(composer).Set("source", "https://"+d.originURL+"/"+d.owner+"/"+d.repository+"/tree/"+label)
		}

		if !isset(composer, "support", "issues") && d.hasIssues {
			supportArray(composer).Set("issues", "https://"+d.originURL+"/"+d.owner+"/"+d.repository+"/issues")
		}

		if !isset(composer, "abandoned") && d.isArchived {
			composer.Set("abandoned", true)
		}

		if !isset(composer, "funding") {
			funding, err := d.getFundingInfo()
			if err != nil {
				return nil, err
			}

			if funding != nil && funding.Len() > 0 {
				composer.Set("funding", funding)
			}
		}
	}

	d.storeInfo(identifier, composer)

	return composer, nil
}

var (
	lineBreak           = php.MustCompile(`{\r?\n}`)
	fundingEntry        = php.MustCompile(`{^(\w+)\s*:\s*(.+)$}`)
	fundingInlineList   = php.MustCompile(`{^\[(.*?)\](?:\s*#.*)?$}`)
	fundingListSep      = php.MustCompile(`{['"]?\s*,\s*['"]?}`)
	fundingValue        = php.MustCompile(`{^([^#].*?)(?:\s+#.*)?$}`)
	fundingKeyOnly      = php.MustCompile(`{^(\w+)\s*:\s*#\s*$}`)
	fundingDashItem     = php.MustCompile(`{^-\s*(.+)(?:\s+#.*)?$}`)
	fundingCommaItem    = php.MustCompile(`{^(.+),(?:\s*#.*)?$}`)
	fundingSimpleDomain = php.MustCompile(`{^[a-z0-9-]++\.[a-z]{2,3}$}`)
)

// fundingQuotes are the characters trimmed off funding URLs.
const fundingQuotes = "\"' "

// getFundingInfo ports getFundingInfo(): the funding entries of
// .github/FUNDING.yml, nil for false.
func (d *GitHubDriver) getFundingInfo() (*php.Array, error) {
	if d.fundingKnown {
		return d.fundingInfo, nil
	}

	d.fundingKnown = true

	if d.originURL != "github.com" {
		return nil, nil
	}

	funding, err := d.fetchFundingFile()
	if err != nil || !php.ToBool(funding) {
		return nil, err
	}

	result, err := parseFunding(funding)
	if err != nil {
		return nil, err
	}
	if d.fundingInfo, err = d.normalizeFunding(result); err != nil {
		return nil, err
	}

	return d.fundingInfo, nil
}

// fetchFundingFile returns the content of the first FUNDING.yml found,
// the repository's or the owner's ($funding, possibly empty).
func (d *GitHubDriver) fetchFundingFile() (string, error) {
	funding := ""

	for _, file := range []string{d.repoAPIURL() + "/contents/.github/FUNDING.yml", d.apiURL() + "/repos/" + d.owner + "/.github/contents/FUNDING.yml"} {
		r, err := d.httpDownloader.Get(file, php.ArrayOf("retry-auth-failure", false))
		if err != nil {
			if isTransportError(err) {
				continue
			}

			return "", err
		}

		response, err := r.DecodeJSONArray()
		if err != nil {
			return "", err
		}

		content, _ := arrayPath(response, "content").(string)
		if !php.ToBool(arrayPath(response, "content")) || arrayPath(response, "encoding") != "base64" {
			continue
		}

		funding, _ = php.Base64Decode(content, false)
		if !php.ToBool(funding) {
			continue
		}

		break
	}

	return funding, nil
}

// parseFunding parses FUNDING.yml into [type, url] entries. The error is
// the PcreException Preg throws.
func parseFunding(funding string) (*php.Array, error) {
	result := php.NewArray()
	add := func(typ, url string) { result.Append(php.ArrayOf("type", typ, "url", url)) }

	lines, err := lineBreak.Split(funding, -1, 0)
	if err != nil {
		return nil, err
	}

	var key string

	hasKey := false

	for _, line := range lines {
		line = php.Trim(line)

		entry, err := match(fundingEntry, line)
		if err != nil {
			return nil, err
		}
		if entry != nil {
			typ, value := entry.Get(1), entry.Get(2)
			if value == "[" {
				key, hasKey = typ, true

				continue
			}

			inline, err := match(fundingInlineList, value)
			if err != nil {
				return nil, err
			}
			if inline != nil {
				items, err := fundingListSep.Split(inline.Get(1), -1, 0)
				if err != nil {
					return nil, err
				}
				for _, item := range items {
					add(typ, php.TrimSet(php.Trim(item), fundingQuotes))
				}
			} else if single, err := match(fundingValue, value); err != nil {
				return nil, err
			} else if single != nil {
				add(typ, php.TrimSet(single.Get(1), fundingQuotes))
			}

			hasKey = false

			continue
		}
		keyOnly, err := match(fundingKeyOnly, line)
		if err != nil {
			return nil, err
		}
		if keyOnly != nil {
			key, hasKey = keyOnly.Get(1), true
		} else if hasKey {
			m, err := match(fundingDashItem, line)
			if err != nil {
				return nil, err
			}
			if m == nil {
				if m, err = match(fundingCommaItem, line); err != nil {
					return nil, err
				}
			}

			if m != nil {
				add(key, php.TrimSet(m.Get(1), fundingQuotes))
			} else if line == "]" {
				hasKey = false
			}
		}
	}

	return result, nil
}

// fundingURLs maps the funding platforms to their URL prefix, and whether
// only the basename of the configured value is kept.
var fundingURLs = map[string]struct {
	prefix   string
	basename bool
}{
	"community_bridge": {"https://funding.communitybridge.org/projects/", true},
	"github":           {"https://github.com/", true},
	"issuehunt":        {"https://issuehunt.io/r/", false},
	"ko_fi":            {"https://ko-fi.com/", true},
	"liberapay":        {"https://liberapay.com/", true},
	"open_collective":  {"https://opencollective.com/", true},
	"patreon":          {"https://www.patreon.com/", true},
	"tidelift":         {"https://tidelift.com/funding/github/", false},
	"polar":            {"https://polar.sh/", true},
	"buy_me_a_coffee":  {"https://www.buymeacoffee.com/", true},
	"thanks_dev":       {"https://thanks.dev/", false},
	"otechie":          {"https://otechie.com/", true},
}

// normalizeFunding turns the platform names of the entries into URLs,
// dropping custom URLs Composer does not support (their keys are not
// renumbered, as in PHP).
func (d *GitHubDriver) normalizeFunding(result *php.Array) (*php.Array, error) {
	for _, k := range result.Keys() {
		v, _ := result.GetKey(k)
		item, _ := v.(*php.Array)
		typ, _ := item.GetString("type")
		url, _ := item.GetString("url")

		if platform, ok := fundingURLs[typ]; ok {
			if platform.basename {
				url = php.Basename(url, "")
			}

			item.Set("url", platform.prefix+url)

			continue
		}

		if typ != "custom" {
			continue
		}

		bits, ok := util.ParseURL(url)
		if !ok {
			result.DeleteKey(k)

			continue
		}

		if !bits.HasScheme && !bits.HasHost {
			if ok, err := matches(fundingSimpleDomain, url); err != nil {
				return nil, err
			} else if ok {
				item.Set("url", "https://"+url)

				continue
			}

			d.io.WriteError("<warning>Funding URL "+url+" not in a supported format.</warning>", true, io.Normal)
			result.DeleteKey(k)
		}
	}

	return result, nil
}

// FileContent ports GitHubDriver::getFileContent.
func (d *GitHubDriver) FileContent(file, identifier string) (string, bool, error) {
	if d.gitDriver != nil {
		return d.gitDriver.FileContent(file, identifier)
	}

	resource, err := d.getJSON(d.repoAPIURL()+"/contents/"+file+"?ref="+php.Urlencode(identifier), false)
	if err != nil {
		return "", false, err
	}

	// The GitHub contents API only returns files up to 1MB as base64 encoded files
	// larger files either need be fetched with a raw accept header or by using the git blob endpoint
	if content := arrayPath(resource, "content"); (content == nil || content == "") && arrayPath(resource, "encoding") == "none" && isset(resource, "git_url") {
		resource, err = d.getJSON(pathString(resource, "git_url"), false)
		if err != nil {
			return "", false, err
		}
	}

	if content, ok := base64Content(resource, false); ok {
		return content, true, nil
	}

	return "", false, &util.RuntimeError{Site: phperr.At("GitHubDriver.php", 346), Message: "Could not retrieve " + file + " for " + identifier}
}

// base64Content is the decoded content of a contents API response: ok is
// false unless it is set, base64 encoded and decodable.
func base64Content(resource *php.Array, strict bool) (string, bool) {
	if !isset(resource, "content") || arrayPath(resource, "encoding") != "base64" {
		return "", false
	}

	return php.Base64Decode(pathString(resource, "content"), strict)
}

// getJSON is $this->getContents($url)->decodeJson() for an array.
func (d *GitHubDriver) getJSON(url string, fetchingRepoData bool) (*php.Array, error) {
	r, err := d.getContents(url, fetchingRepoData)
	if err != nil {
		return nil, err
	}

	return r.DecodeJSONArray()
}

// ChangeDate ports GitHubDriver::getChangeDate.
func (d *GitHubDriver) ChangeDate(identifier string) (time.Time, bool, error) {
	if d.gitDriver != nil {
		return d.gitDriver.ChangeDate(identifier)
	}

	commit, err := d.getJSON(d.repoAPIURL()+"/commits/"+php.Urlencode(identifier), false)
	if err != nil {
		return time.Time{}, false, err
	}

	date, err := parseDate(pathString(commit, "commit", "committer", "date"))

	return date, err == nil, err
}

// Tags ports GitHubDriver::getTags.
func (d *GitHubDriver) Tags() (*php.Array, error) {
	if d.gitDriver != nil {
		return d.gitDriver.Tags()
	}

	if d.tags == nil {
		tags, err := d.paginate(d.repoAPIURL()+"/tags?per_page=100", func(refs *php.Array, tag *php.Array) {
			refs.Set(pathString(tag, "name"), pathString(tag, "commit", "sha"))
		})
		if err != nil {
			return nil, err
		}

		d.tags = tags
	}

	return d.tags, nil
}

// Branches ports GitHubDriver::getBranches.
func (d *GitHubDriver) Branches() (*php.Array, error) {
	if d.gitDriver != nil {
		return d.gitDriver.Branches()
	}

	if d.branches == nil {
		branches, err := d.paginate(d.repoAPIURL()+"/git/refs/heads?per_page=100", func(refs *php.Array, branch *php.Array) {
			ref := pathString(branch, "ref")
			if len(ref) > 11 {
				ref = ref[11:]
			} else {
				ref = ""
			}

			if ref != "gh-pages" {
				refs.Set(ref, pathString(branch, "object", "sha"))
			}
		})
		if err != nil {
			return nil, err
		}

		d.branches = branches
	}

	return d.branches, nil
}

// paginate collects the references of every page of a list starting at
// resource.
func (d *GitHubDriver) paginate(resource string, add func(refs, item *php.Array)) (*php.Array, error) {
	refs := php.NewArray()

	for resource != "" {
		response, err := d.getContents(resource, false)
		if err != nil {
			return nil, err
		}

		data, err := response.DecodeJSONArray()
		if err != nil {
			return nil, err
		}

		if data != nil {
			for _, v := range data.All() {
				item, _ := v.(*php.Array)
				add(refs, item)
			}
		}

		if resource, err = nextPage(response); err != nil {
			return nil, err
		}
	}

	return refs, nil
}

// gitHubSupports ports GitHubDriver::supports.
func gitHubSupports(deps Deps, url string, _ bool) (bool, error) {
	m, err := match(gitHubSupportsURL, url)
	if err != nil || m == nil {
		return false, err
	}

	origin, ok := m.Group(2)
	if !ok {
		origin = m.Get(3)
	}

	return inConfigList(deps.Config, "github-domains", php.Strtolower(replaceInfallible(wwwPrefix, "", origin))), nil
}

// inConfigList is in_array($value, $config->get($key)).
func inConfigList(config http.Config, key, value string) bool {
	list, _ := config.Get(key).(*php.Array)

	return list != nil && php.InArray(value, list, false)
}

// RepoData ports getRepoData: the <github-api>/repos/<owner>/<repo>
// result.
func (d *GitHubDriver) RepoData() (*php.Array, error) {
	if err := d.fetchRootIdentifier(); err != nil {
		return nil, err
	}

	return d.repoData, nil
}

// generateSSHURL ports generateSshUrl.
func (d *GitHubDriver) generateSSHURL() string {
	if strings.Contains(d.originURL, ":") {
		return "ssh://git@" + d.originURL + "/" + d.owner + "/" + d.repository + ".git"
	}

	return "git@" + d.originURL + ":" + d.owner + "/" + d.repository + ".git"
}

// getContents ports GitHubDriver::getContents: the request, with GitHub
// authorization, rate limit handling and the git fallback of
// repository data requests.
func (d *GitHubDriver) getContents(url string, fetchingRepoData bool) (*http.Response, error) {
	response, err := d.vcsDriver.getContents(url)

	e, ok := asTransportError(err)
	if !ok {
		return response, err
	}

	gitHubUtil := http.NewGitHub(d.io, d.config, d.process, d.httpDownloader)

	switch e.Code {
	case 401, 404:
		// try to authorize only if we are fetching the main /repos/foo/bar data, otherwise it must be a real 404
		if !fetchingRepoData {
			return nil, err
		}

		if gitHubUtil.AuthorizeOAuth(d.originURL) {
			return d.vcsDriver.getContents(url)
		}

		if !d.io.IsInteractive() {
			if err := d.attemptCloneFallback(e); err != nil {
				return nil, err
			}

			return dummyResponse(), nil
		}

		var scopesIssued, scopesNeeded []string

		if len(e.Headers) > 0 {
			if scopes, ok := http.FindHeaderValue(e.Headers, "X-OAuth-Scopes"); ok && php.ToBool(scopes) {
				scopesIssued = strings.Split(scopes, " ")
			}

			if scopes, ok := http.FindHeaderValue(e.Headers, "X-Accepted-OAuth-Scopes"); ok && php.ToBool(scopes) {
				scopesNeeded = strings.Split(scopes, " ")
			}
		}

		scopesFailed := php.ArrayDiff(php.StringList(scopesNeeded), php.StringList(scopesIssued))
		// non-authenticated requests get no scopesNeeded, so ask for credentials
		// authenticated requests which failed some scopes should ask for new credentials too
		if len(e.Headers) == 0 || len(scopesNeeded) == 0 || scopesFailed.Len() > 0 {
			if _, err := gitHubUtil.AuthorizeOAuthInteractively(d.originURL, "Your GitHub credentials are required to fetch private repository metadata (<info>"+util.SanitizeURL(d.url)+"</info>)"); err != nil {
				return nil, err
			}
		}

		return d.vcsDriver.getContents(url)

	case 403:
		if !d.io.HasAuthentication(d.originURL) && gitHubUtil.AuthorizeOAuth(d.originURL) {
			return d.vcsDriver.getContents(url)
		}

		if !d.io.IsInteractive() && fetchingRepoData {
			if err := d.attemptCloneFallback(e); err != nil {
				return nil, err
			}

			return dummyResponse(), nil
		}

		rateLimited := gitHubUtil.IsRateLimited(e.Headers)

		if !d.io.HasAuthentication(d.originURL) {
			if !d.io.IsInteractive() {
				d.io.WriteError("<error>GitHub API limit exhausted. Failed to get metadata for the "+util.SanitizeURL(d.url)+" repository, try running in interactive mode so that you can enter your GitHub credentials to increase the API limit</error>", true, io.Normal)

				return nil, err
			}

			if _, err := gitHubUtil.AuthorizeOAuthInteractively(d.originURL, "API limit exhausted. Enter your GitHub credentials to get a larger API limit (<info>"+util.SanitizeURL(d.url)+"</info>)"); err != nil {
				return nil, err
			}

			return d.vcsDriver.getContents(url)
		}

		if rateLimited {
			rateLimit := gitHubUtil.RateLimit(e.Headers)
			d.io.WriteError("<error>GitHub API limit ("+strconv.Itoa(rateLimit.Limit)+" calls/hr) is exhausted. You are already authorized so you have to wait until "+rateLimit.Reset+" before doing more requests</error>", true, io.Normal)
		}

		return nil, err
	}

	return nil, err
}

// fetchRootIdentifier ports fetchRootIdentifier: the repository data.
func (d *GitHubDriver) fetchRootIdentifier() error {
	if d.repoData != nil && d.repoData.Len() > 0 {
		return nil
	}

	repoData, err := d.getJSON(d.repoAPIURL(), true)
	if err != nil {
		e, ok := asTransportError(err)
		if !ok || e.Code != 499 {
			return err
		}

		if err := d.attemptCloneFallback(e); err != nil {
			return err
		}
	} else {
		d.repoData = repoData
	}

	if d.repoData == nil && d.gitDriver != nil {
		return nil
	}

	d.owner = pathString(d.repoData, "owner", "login")
	d.repository = pathString(d.repoData, "name")

	d.isPrivate = php.ToBool(arrayPath(d.repoData, "private"))

	switch {
	case isset(d.repoData, "default_branch"):
		d.rootIdentifier = pathString(d.repoData, "default_branch")
	case isset(d.repoData, "master_branch"):
		d.rootIdentifier = pathString(d.repoData, "master_branch")
	default:
		d.rootIdentifier = "master"
	}

	d.hasIssues = php.ToBool(arrayPath(d.repoData, "has_issues"))
	d.isArchived = php.ToBool(arrayPath(d.repoData, "archived"))

	return nil
}

// attemptCloneFallback ports attemptCloneFallback: switch to a GitDriver
// on the SSH url.
func (d *GitHubDriver) attemptCloneFallback(previous *util.TransportError) error {
	if !d.allowGitFallback {
		fallbackErr := &util.RuntimeError{Site: phperr.At("GitHubDriver.php", 611), Message: "Fallback to git driver disabled"}
		if previous != nil {
			fallbackErr.Prev = previous
		}

		return fallbackErr
	}

	d.isPrivate = true

	// If this repository may be private (hard to say for sure,
	// GitHub returns 404 for private repositories) and we
	// cannot ask for authentication credentials (because we
	// are not interactive) then we fallback to GitDriver.
	err := d.setupGitDriver(d.generateSSHURL())
	if err != nil && util.IsRuntimeException(err) {
		d.gitDriver = nil

		d.io.WriteError("<error>Failed to clone the "+d.generateSSHURL()+" repository, try running in interactive mode so that you can enter your GitHub credentials</error>", true, io.Normal)
	}

	return err
}

// setupGitDriver ports setupGitDriver.
func (d *GitHubDriver) setupGitDriver(url string) error {
	if !d.allowGitFallback {
		return &util.RuntimeError{Site: phperr.At("GitHubDriver.php", 635), Message: "Fallback to git driver disabled"}
	}

	d.gitDriver = NewGitDriver(php.ArrayOf("url", url), d.deps)

	return d.gitDriver.Initialize()
}

var nextLink = php.MustCompile(`{<(.+?)>; *rel="next"}`)

// nextPage ports getNextPage: the url of the next page of a paginated
// API response, "" for null.
func nextPage(response *http.Response) (string, error) {
	header, ok := response.Header("link")
	if !ok || !php.ToBool(header) {
		return "", nil
	}

	for link := range strings.SplitSeq(header, ",") {
		if m, err := match(nextLink, link); err != nil || m != nil {
			if err != nil {
				return "", err
			}

			return m.Get(1), nil
		}
	}

	return "", nil
}
