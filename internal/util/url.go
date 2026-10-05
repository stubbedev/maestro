// Ports src/Composer/Util/Url.php.

package util

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// NonSecretCredentials is Url::NON_SECRET_CREDENTIALS: well-known markers
// used in the user or password slot of URLs (e.g. Bitbucket's x-token-auth)
// that are safe to display.
var NonSecretCredentials = []string{"private-token", "x-token-auth", "oauth2", "gitlab-ci-token", "x-oauth-basic"}

// GitHubTokenRegex is GitHub::GITHUB_TOKEN_REGEX.
var gitHubTokenRegex = mustPCRE(`^([a-f0-9]{12,}|gh[a-z]_[a-zA-Z0-9_.-]+|github_pat_[a-zA-Z0-9_]+)$`, false)

var (
	githubLegacyArchive = mustPCRE(`^https?://(?:www\.)?github\.com/([^/]+)/([^/]+)/(zip|tar)ball/(.+)$`, true)
	githubWebArchive    = mustPCRE(`^https?://(?:www\.)?github\.com/([^/]+)/([^/]+)/archive/.+\.(zip|tar)(?:\.gz)?$`, true)
	githubAPIArchive    = mustPCRE(`^https?://api\.github\.com/repos/([^/]+)/([^/]+)/(zip|tar)ball(?:/.+)?$`, true)
	bitbucketArchive    = mustPCRE(`^https?://(?:www\.)?bitbucket\.org/([^/]+)/([^/]+)/get/(.+)\.(zip|tar\.gz|tar\.bz2)$`, true)
	gitlabArchive       = mustPCRE(`^https?://(?:www\.)?gitlab\.com/api/v[34]/projects/([^/]+)/repository/archive\.(zip|tar\.gz|tar\.bz2|tar)\?sha=.+$`, true)
	githubDomainArchive = mustPCRE(`(/repos/[^/]+/[^/]+/(zip|tar)ball)(?:/.+)?$`, true)
	gitlabDomainArchive = mustPCRE(`(/api/v[34]/projects/[^/]+/repository/archive\.(?:zip|tar\.gz|tar\.bz2|tar)\?sha=).+$`, true)
)

// UpdateDistReference ports Url::updateDistReference: points a GitHub,
// Bitbucket or GitLab archive URL at ref. githubDomains and gitlabDomains
// are the github-domains and gitlab-domains config values.
func UpdateDistReference(url, ref string, githubDomains, gitlabDomains []string) string {
	u, _ := parseURL(url)
	host := u.host

	switch {
	case u.hasHost && (host == "api.github.com" || host == "github.com" || host == "www.github.com"):
		if m := githubLegacyArchive.FindStringSubmatch(url); m != nil {
			// Update legacy GitHub archives to API calls with the proper
			// reference.
			url = "https://api.github.com/repos/" + m[1] + "/" + m[2] + "/" + m[3] + "ball/" + ref
		} else if m := githubWebArchive.FindStringSubmatch(url); m != nil {
			// Update current GitHub web archives to API calls with the
			// proper reference.
			url = "https://api.github.com/repos/" + m[1] + "/" + m[2] + "/" + m[3] + "ball/" + ref
		} else if m := githubAPIArchive.FindStringSubmatch(url); m != nil {
			// Update API archives to the proper reference.
			url = "https://api.github.com/repos/" + m[1] + "/" + m[2] + "/" + m[3] + "ball/" + ref
		}
	case u.hasHost && (host == "bitbucket.org" || host == "www.bitbucket.org"):
		if m := bitbucketArchive.FindStringSubmatch(url); m != nil {
			url = "https://bitbucket.org/" + m[1] + "/" + m[2] + "/get/" + ref + "." + m[4]
		}
	case u.hasHost && (host == "gitlab.com" || host == "www.gitlab.com"):
		if m := gitlabArchive.FindStringSubmatch(url); m != nil {
			url = "https://gitlab.com/api/v4/projects/" + m[1] + "/repository/archive." + m[2] + "?sha=" + ref
		}
	case u.hasHost && slices.Contains(githubDomains, host):
		url = githubDomainArchive.Replace(url, "$1/"+ref)
	case u.hasHost && slices.Contains(gitlabDomains, host):
		url = gitlabDomainArchive.Replace(url, "${1}"+ref)
	}

	return url
}

var hostPortPrefix = regexp.MustCompile(`^([^:/]+):[0-9]+`)

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
			bcDomain := hostPortPrefix.ReplaceAllString(gitlabDomain, "$1")
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

	return ok && u.hasScheme && (strings.EqualFold(u.scheme, "http") || strings.EqualFold(u.scheme, "https"))
}

var (
	accessTokenParam = regexp.MustCompile(`([&?]access_token=)[^&]+`)
	// {(?:(?P<prefix>[a-z0-9][a-z0-9+.-]*://)|\A)(?P<user>[^:/\s?#]*)(?::(?P<password>[^\s/?#]+))?@}i
	// with PCRE's \s (which includes \v) and ASCII-only case folding.
	urlCredentials = regexp.MustCompile(`(?:([a-zA-Z0-9][a-zA-Z0-9+.\-]*://)|\A)([^:/\t\n\v\f\r ?#]*)(?::([^\t\n\v\f\r /?#]+))?@`)
)

// SanitizeURL ports Url::sanitize: masks access tokens and the credentials
// of every scheme://user:pass@ in s (plus a scheme-less one at its start).
func SanitizeURL(s string) string {
	// GitHub repository renames redirect to locations holding the
	// access_token as GET parameter.
	s = accessTokenParam.ReplaceAllString(s, "${1}***")

	return replaceAllSubmatchFunc(urlCredentials, s, func(m []string) string {
		user := SanitizeUsername(m[2])
		if m[3] != "" {
			return m[1] + user + ":***@"
		}

		return m[1] + user + "@"
	})
}

// replaceAllSubmatchFunc is preg_replace_callback for a pattern that never
// matches empty.
func replaceAllSubmatchFunc(re *regexp.Regexp, s string, f func(m []string) string) string {
	locs := re.FindAllStringSubmatchIndex(s, -1)
	if locs == nil {
		return s
	}

	var b strings.Builder

	last := 0
	m := make([]string, re.NumSubexp()+1)

	for _, loc := range locs {
		for i := range m {
			m[i] = ""
			if loc[2*i] >= 0 {
				m[i] = s[loc[2*i]:loc[2*i+1]]
			}
		}

		b.WriteString(s[last:loc[0]])
		b.WriteString(f(m))
		last = loc[1]
	}

	b.WriteString(s[last:])

	return b.String()
}

var urlUserinfo = regexp.MustCompile(`://[^/\t\n\v\f\r ?#]+@`)

// StripCredentials ports Url::stripCredentials: removes the whole userinfo
// from URLs that get persisted (e.g. as a git remote).
func StripCredentials(url string) string {
	return urlUserinfo.ReplaceAllLiteralString(url, "://")
}

// SanitizeUsername ports Url::sanitizeUsername: tokens and other long
// (12+ bytes) values keep their first 3 bytes; short values and well-known
// markers are shown as is.
func SanitizeUsername(user string) string {
	if slices.Contains(NonSecretCredentials, user) {
		return user
	}

	if gitHubTokenRegex.MatchString(user) || len(user) >= 12 {
		return user[:min(3, len(user))] + "***"
	}

	return user
}
