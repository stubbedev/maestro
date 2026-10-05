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

// The patterns of UpdateDistReference: Preg::isMatch/Preg::replace throw
// a PcreException (*php.PcreError) on them for very long URLs.
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
func UpdateDistReference(url, ref string, githubDomains, gitlabDomains []string) (string, error) {
	u, _ := parseURL(url)
	host := u.host

	// firstMatch is the if/elseif chain of Preg::isMatch calls.
	firstMatch := func(res ...*php.Regexp) (*php.Match, error) {
		for _, re := range res {
			if m, err := re.Match(url); err != nil || m != nil {
				return m, err
			}
		}

		return nil, nil
	}

	switch {
	case u.hasHost && (host == "api.github.com" || host == "github.com" || host == "www.github.com"):
		// Update legacy GitHub archives, current GitHub web archives and
		// API archives to API calls with the proper reference.
		m, err := firstMatch(githubLegacyArchive, githubWebArchive, githubAPIArchive)
		if err != nil {
			return "", err
		}
		if m != nil {
			url = "https://api.github.com/repos/" + m.Get(1) + "/" + m.Get(2) + "/" + m.Get(3) + "ball/" + ref
		}
	case u.hasHost && (host == "bitbucket.org" || host == "www.bitbucket.org"):
		m, err := firstMatch(bitbucketArchive)
		if err != nil {
			return "", err
		}
		if m != nil {
			url = "https://bitbucket.org/" + m.Get(1) + "/" + m.Get(2) + "/get/" + ref + "." + m.Get(4)
		}
	case u.hasHost && (host == "gitlab.com" || host == "www.gitlab.com"):
		m, err := firstMatch(gitlabArchive)
		if err != nil {
			return "", err
		}
		if m != nil {
			url = "https://gitlab.com/api/v4/projects/" + m.Get(1) + "/repository/archive." + m.Get(2) + "?sha=" + ref
		}
	case u.hasHost && slices.Contains(githubDomains, host):
		var err error
		if url, _, err = githubDomainArchive.Replace(url, "$1/"+ref, -1); err != nil {
			return "", err
		}
	case u.hasHost && slices.Contains(gitlabDomains, host):
		var err error
		if url, _, err = gitlabDomainArchive.Replace(url, "${1}"+ref, -1); err != nil {
			return "", err
		}
	}

	return url, nil
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
			// gitlab-domains entries are short host names, far below what
			// could exhaust the backtrack limit: Preg::replace cannot throw.
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

	return ok && u.hasScheme && (php.Strcasecmp(u.scheme, "http") == 0 || php.Strcasecmp(u.scheme, "https") == 0)
}

var (
	accessTokenParam = php.MustCompile(`{([&?]access_token=)[^&]+}`)
	urlCredentials   = php.MustCompile(`{(?:(?P<prefix>[a-z0-9][a-z0-9+.-]*://)|\A)(?P<user>[^:/\s?#]*)(?::(?P<password>[^\s/?#]+))?@}i`)
)

// SanitizeURL ports Url::sanitize: masks access tokens and the credentials
// of every scheme://user:pass@ in s (plus a scheme-less one at its start).
//
// Divergence: the credentials pattern exhausts the backtrack limit on a
// run of about a megabyte of [a-z0-9+.-] followed by an unreachable '@',
// where Preg::replaceCallback throws a PcreException. SanitizeURL has no
// error result (it is called from ~100 places, mostly while building
// messages), so it returns "" then, which leaks nothing.
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

// urlUserinfo and gitHubTokenRegex cannot exhaust PCRE's limits (checked
// against megabyte subjects): their errors are not checked.
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
