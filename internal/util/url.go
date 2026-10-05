// Ports src/Composer/Util/Url.php.

package util

import (
	"slices"
	"strconv"
	"strings"

	"github.com/stubbedev/maestro/internal/php"
)

// NonSecretCredentials is Url::NON_SECRET_CREDENTIALS: well-known markers
// used in the user or password slot of URLs (e.g. Bitbucket's x-token-auth)
// that are safe to display.
var NonSecretCredentials = []string{"private-token", "x-token-auth", "oauth2", "gitlab-ci-token", "x-oauth-basic"}

// gitHubTokenRegex is GitHub::GITHUB_TOKEN_REGEX.
var gitHubTokenRegex = php.MustCompile(`{^([a-f0-9]{12,}|gh[a-z]_[a-zA-Z0-9_.-]+|github_pat_[a-zA-Z0-9_]+)$}`)

// The patterns of this file cannot fail to match (no /u, no runaway
// backtracking), so their errors (PHP's PcreException) are not checked.
var (
	githubLegacyArchive = php.MustCompile(`{^https?://(?:www\.)?github\.com/([^/]+)/([^/]+)/(zip|tar)ball/(.+)$}i`)
	githubWebArchive    = php.MustCompile(`{^https?://(?:www\.)?github\.com/([^/]+)/([^/]+)/archive/.+\.(zip|tar)(?:\.gz)?$}i`)
	githubAPIArchive    = php.MustCompile(`{^https?://api\.github\.com/repos/([^/]+)/([^/]+)/(zip|tar)ball(?:/.+)?$}i`)
	bitbucketArchive    = php.MustCompile(`{^https?://(?:www\.)?bitbucket\.org/([^/]+)/([^/]+)/get/(.+)\.(zip|tar\.gz|tar\.bz2)$}i`)
	gitlabArchive       = php.MustCompile(`{^https?://(?:www\.)?gitlab\.com/api/v[34]/projects/([^/]+)/repository/archive\.(zip|tar\.gz|tar\.bz2|tar)\?sha=.+$}i`)
	githubDomainArchive = php.MustCompile(`{(/repos/[^/]+/[^/]+/(zip|tar)ball)(?:/.+)?$}i`)
	gitlabDomainArchive = php.MustCompile(`{(/api/v[34]/projects/[^/]+/repository/archive\.(?:zip|tar\.gz|tar\.bz2|tar)\?sha=).+$}i`)
)

// UpdateDistReference ports Url::updateDistReference: points a GitHub,
// Bitbucket or GitLab archive URL at ref. githubDomains and gitlabDomains
// are the github-domains and gitlab-domains config values.
func UpdateDistReference(url, ref string, githubDomains, gitlabDomains []string) string {
	u, _ := parseURL(url)
	host := u.host

	switch {
	case u.hasHost && (host == "api.github.com" || host == "github.com" || host == "www.github.com"):
		if m, _ := githubLegacyArchive.Match(url); m != nil {
			// Update legacy GitHub archives to API calls with the proper
			// reference.
			url = "https://api.github.com/repos/" + m.Get(1) + "/" + m.Get(2) + "/" + m.Get(3) + "ball/" + ref
		} else if m, _ := githubWebArchive.Match(url); m != nil {
			// Update current GitHub web archives to API calls with the
			// proper reference.
			url = "https://api.github.com/repos/" + m.Get(1) + "/" + m.Get(2) + "/" + m.Get(3) + "ball/" + ref
		} else if m, _ := githubAPIArchive.Match(url); m != nil {
			// Update API archives to the proper reference.
			url = "https://api.github.com/repos/" + m.Get(1) + "/" + m.Get(2) + "/" + m.Get(3) + "ball/" + ref
		}
	case u.hasHost && (host == "bitbucket.org" || host == "www.bitbucket.org"):
		if m, _ := bitbucketArchive.Match(url); m != nil {
			url = "https://bitbucket.org/" + m.Get(1) + "/" + m.Get(2) + "/get/" + ref + "." + m.Get(4)
		}
	case u.hasHost && (host == "gitlab.com" || host == "www.gitlab.com"):
		if m, _ := gitlabArchive.Match(url); m != nil {
			url = "https://gitlab.com/api/v4/projects/" + m.Get(1) + "/repository/archive." + m.Get(2) + "?sha=" + ref
		}
	case u.hasHost && slices.Contains(githubDomains, host):
		url, _, _ = githubDomainArchive.Replace(url, "$1/"+ref, -1)
	case u.hasHost && slices.Contains(gitlabDomains, host):
		url, _, _ = gitlabDomainArchive.Replace(url, "${1}"+ref, -1)
	}

	return url
}

var hostPortPrefix = php.MustCompile(`{^([^/]+):\d+}`)

// GetOrigin ports Url::getOrigin: the host (and port) credentials are kept
// under. gitlabDomains is the gitlab-domains config value.
func GetOrigin(url string, gitlabDomains []string) string {
	if strings.HasPrefix(url, "file://") {
		return url
	}

	u, _ := parseURL(url)
	origin := u.host

	if u.hasPort && u.port != 0 {
		origin += ":" + strconv.Itoa(u.port)
	}

	if strings.HasSuffix(origin, ".github.com") && origin != "codeload.github.com" {
		return "github.com"
	}

	if origin == "repo.packagist.org" {
		return "packagist.org"
	}

	if origin == "" {
		origin = url
	}

	// GitLab can be installed in a non-root context (i.e. gitlab.com/foo).
	// When downloading archives the originUrl is the host without the path,
	// so look for the registered gitlab-domains matching the host here.
	if !strings.Contains(origin, "/") && !slices.Contains(gitlabDomains, origin) {
		for _, gitlabDomain := range gitlabDomains {
			// Configured domains may spell out a port the URL omits, see
			// GitLab::authorizeOAuth.
			bcDomain, _, _ := hostPortPrefix.Replace(gitlabDomain, "$1", -1)
			if gitlabDomain != "" && (bcDomain == origin || strings.HasPrefix(bcDomain, origin+"/")) {
				return gitlabDomain
			}
		}
	}

	return origin
}

// IsAllowedRedirect ports Url::isAllowedRedirect: only http(s) redirects
// are followed.
func IsAllowedRedirect(url string) bool {
	u, ok := parseURL(url)

	return ok && u.hasScheme && (equalFoldASCII(u.scheme, "http") || equalFoldASCII(u.scheme, "https"))
}

var (
	accessTokenParam = php.MustCompile(`{([&?]access_token=)[^&]+}`)
	urlCredentials   = php.MustCompile(`{(?:(?P<prefix>[a-z0-9][a-z0-9+.-]*://)|\A)(?P<user>[^:/\s?#]*)(?::(?P<password>[^\s/?#]+))?@}i`)
)

// SanitizeURL ports Url::sanitize: masks access tokens and the credentials
// of every scheme://user:pass@ in s (plus a scheme-less one at its start).
func SanitizeURL(s string) string {
	// GitHub repository renames redirect to locations holding the
	// access_token as GET parameter.
	s, _, _ = accessTokenParam.Replace(s, "$1***", -1)

	s, _, _ = urlCredentials.ReplaceCallback(s, func(m *php.Match) string {
		prefix, _ := m.Named("prefix")
		user, _ := m.Named("user")
		user = SanitizeUsername(user)
		if password, _ := m.Named("password"); password != "" {
			return prefix + user + ":***@"
		}

		return prefix + user + "@"
	}, -1)

	return s
}

var urlUserinfo = php.MustCompile(`{://[^/\s?#]+@}`)

// StripCredentials ports Url::stripCredentials: removes the whole userinfo
// from URLs that get persisted (e.g. as a git remote).
func StripCredentials(url string) string {
	url, _, _ = urlUserinfo.Replace(url, "://", -1)

	return url
}

// SanitizeUsername ports Url::sanitizeUsername: tokens and other long
// (12+ bytes) values keep their first 3 bytes; short values and well-known
// markers are shown as is.
func SanitizeUsername(user string) string {
	if slices.Contains(NonSecretCredentials, user) {
		return user
	}

	if isToken, _ := gitHubTokenRegex.IsMatch(user); isToken || len(user) >= 12 {
		return user[:min(3, len(user))] + "***"
	}

	return user
}
