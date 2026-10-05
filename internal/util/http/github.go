// Ports src/Composer/Util/GitHub.php.

package http

import (
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
)

// GitHubTokenRegex is GitHub::GITHUB_TOKEN_REGEX.
const GitHubTokenRegex = `{^([a-f0-9]{12,}|gh[a-z]_[a-zA-Z0-9_.-]+|github_pat_[a-zA-Z0-9_]+)$}` //nolint:gosec // a pattern, not a credential

// GitHub ports Composer\Util\GitHub: GitHub token setup and API response
// inspection.
type GitHub struct {
	io      io.IO
	config  Config
	process Process
	http    func() (Getter, error)
	// now is time() (tests freeze it).
	now func() time.Time
}

// NewGitHub is new GitHub($io, $config, $process, $httpDownloader). A nil
// process is a new ProcessExecutor; a nil downloader is created with
// CreateHttpDownloader on first use.
func NewGitHub(ioi io.IO, config Config, process Process, httpDownloader Getter) *GitHub {
	if process == nil {
		process = newProcess(ioi)
	}

	return &GitHub{io: ioi, config: config, process: process, http: lazyGetter(ioi, config, httpDownloader, nil), now: time.Now}
}

// lazyGetter returns httpDownloader, or a function creating one like
// Factory::createHttpDownloader($io, $config) does.
func lazyGetter(ioi io.IO, config Config, httpDownloader Getter, rt Runtime) func() (Getter, error) {
	if httpDownloader != nil {
		return func() (Getter, error) { return httpDownloader, nil }
	}

	var (
		created Getter
		err     error
		done    bool
	)

	return func() (Getter, error) {
		if !done {
			var d *HttpDownloader

			d, err = CreateHttpDownloader(ioi, config, nil, rt)
			if err == nil {
				created = d
			}

			done = true
		}

		return created, err
	}
}

// AuthorizeOAuth is authorizeOAuth($originUrl): use the token in git
// config (github.accesstoken) if there is one.
func (g *GitHub) AuthorizeOAuth(originURL string) bool {
	if !slices.Contains(configList(g.config, "github-domains"), originURL) {
		return false
	}

	if token, ok := gitConfig(g.process, "github.accesstoken"); ok {
		g.io.SetAuthentication(originURL, token, new("x-oauth-basic"))

		return true
	}

	return false
}

// AuthorizeOAuthInteractively is authorizeOAuthInteractively($originUrl,
// $message): ask for a token, check it against the API and store it.
func (g *GitHub) AuthorizeOAuthInteractively(originURL, message string) (bool, error) {
	if message != "" {
		g.io.WriteError(message, true, io.Normal)
	}

	note := "Composer"
	if g.config.Get("github-expose-hostname") == true {
		var output string
		if code, err := g.process.Execute(util.Cmd("hostname"), &output, ""); err == nil && code == 0 {
			note += " on " + php.Trim(output)
		}
	}

	note += " " + php.Date("Y-m-d Hi", g.now().Unix())
	encodedNote := strings.ReplaceAll(php.Rawurlencode(note), "%20", "+")

	localAuthConfig := g.config.LocalAuthConfigSource()

	g.io.WriteErrorMessages([]string{
		"You need to provide a GitHub access token.",
		`Tokens will be stored in plain text in "` + sourceNames(localAuthConfig, g.config.AuthConfigSource()) + `" for future use by Composer.`,
		"Due to the security risk of tokens being exfiltrated, use tokens with short expiration times and only the minimum permissions necessary.",
		"",
		"Carefully consider the following options in order:",
		"",
	}, true, io.Normal)
	g.io.WriteErrorMessages([]string{
		"1. When you don't use 'vcs'  type 'repositories'  in composer.json and do not need to clone source or download dist files",
		"from private GitHub repositories over HTTPS, use a fine-grained token with read-only access to public information.",
		"Use the following URL to create such a token:",
		"https://" + originURL + "/settings/personal-access-tokens/new?name=" + encodedNote,
		"",
	}, true, io.Normal)
	g.io.WriteErrorMessages([]string{
		`2. When all relevant _private_ GitHub repositories belong to a single user or organisation, use a fine-grained token with`,
		`repository "content" read-only permissions. You can start with the following URL, but you may need to change the resource owner`,
		"to the right user or organisation. Additionally, you can scope permissions down to apply only to selected repositories.",
		"https://" + originURL + "/settings/personal-access-tokens/new?contents=read&name=" + encodedNote,
		"",
	}, true, io.Normal)
	g.io.WriteErrorMessages([]string{
		`3. A "classic" token grants broad permissions on your behalf to all repositories accessible by you.`,
		"This may include write permissions, even though not needed by Composer. Use it only when you need to access",
		"private repositories across multiple organisations at the same time and using directory-specific authentication sources",
		"is not an option. You can generate a classic token here:",
		"https://" + originURL + "/settings/tokens/new?scopes=repo&description=" + encodedNote,
		"",
	}, true, io.Normal)
	g.io.WriteError("For additional information, check https://getcomposer.org/doc/articles/authentication-for-private-packages.md#github-oauth", true, io.Normal)

	storeInLocalAuthConfig := false

	if localAuthConfig != nil {
		answer, err := g.io.AskConfirmation("A local auth config source was found, do you want to store the token there?", true)
		if err != nil {
			return false, err
		}

		storeInLocalAuthConfig = answer
	}

	answer, err := g.io.AskAndHideAnswer("Token (hidden): ")
	if err != nil {
		return false, err
	}

	token := php.Trim(php.ToString(answer))
	if token == "" {
		g.io.WriteError("<warning>No token given, aborting.</warning>", true, io.Normal)
		g.io.WriteError(`You can also add it manually later by using "composer config --global --auth github-oauth.github.com <token>"`, true, io.Normal)

		return false, nil
	}

	g.io.SetAuthentication(originURL, token, new("x-oauth-basic"))

	apiURL := originURL + "/api/v3/"
	if originURL == "github.com" {
		apiURL = "api.github.com/"
	}

	downloader, err := g.http()
	if err != nil {
		return false, err
	}

	if _, err := downloader.Get("https://"+apiURL, php.ArrayOf("retry-auth-failure", false)); err != nil {
		if te, ok := errors.AsType[*util.TransportError](err); ok && (te.Code == 403 || te.Code == 401) {
			g.io.WriteError("<error>Invalid token provided.</error>", true, io.Normal)
			g.io.WriteError(`You can also add it manually later by using "composer config --global --auth github-oauth.github.com <token>"`, true, io.Normal)

			return false, nil
		}

		return false, err
	}

	// store value in local/user config
	authConfigSource := g.config.AuthConfigSource()
	if storeInLocalAuthConfig && localAuthConfig != nil {
		authConfigSource = localAuthConfig
	}

	if err := g.config.ConfigSource().RemoveConfigSetting("github-oauth." + originURL); err != nil {
		return false, err
	}

	if err := authConfigSource.AddConfigSetting("github-oauth."+originURL, token); err != nil {
		return false, err
	}

	g.io.WriteError("<info>Token stored successfully.</info>", true, io.Normal)

	return true, nil
}

// sourceNames is the "local OR global" auth source naming of the token
// prompts.
func sourceNames(local, global ConfigSource) string {
	if local != nil {
		return local.Name() + " OR " + global.Name()
	}

	return global.Name()
}

// RateLimit is getRateLimit()'s result.
type RateLimit struct {
	// Limit is x-ratelimit-limit; HasLimit is false for Composer's '?'.
	Limit    int
	HasLimit bool
	// Reset is x-ratelimit-reset formatted as a date, or "?".
	Reset string
}

// RateLimit is getRateLimit($headers).
func (g *GitHub) RateLimit(headers []string) RateLimit {
	rateLimit := RateLimit{Reset: "?"}

	for _, header := range headers {
		header = php.Trim(header)
		if php.Stripos(header, "x-ratelimit-") < 0 {
			continue
		}

		typ, value, _ := strings.Cut(header, ":")

		switch php.Strtolower(typ) {
		case "x-ratelimit-limit":
			rateLimit.Limit, rateLimit.HasLimit = int(php.ToInt(php.Trim(value))), true
		case "x-ratelimit-reset":
			rateLimit.Reset = php.Date("Y-m-d H:i:s", php.ToInt(php.Trim(value)))
		}
	}

	return rateLimit
}

// Infallible: the pattern does bounded work per start position, so Preg
// cannot throw on it and its call sites ignore the error.
var ssoURLRegex = php.MustCompile(`{\burl=(?P<url>[^\s;]+)}`)

// SSOURL is getSsoUrl($headers); false for null.
func (g *GitHub) SSOURL(headers []string) (string, bool) {
	for _, header := range headers {
		header = php.Trim(header)
		if php.Stripos(header, "x-github-sso: required") < 0 {
			continue
		}

		if m, _ := ssoURLRegex.Match(header); m != nil {
			return m.Named("url")
		}
	}

	return "", false
}

// Infallible: anchored, and ' *' is possessive; call sites ignore the
// error.
var (
	rateLimitedRegex = php.MustCompile(`{^x-ratelimit-remaining: *0$}i`)
	requiresSSORegex = php.MustCompile(`{^x-github-sso: required}i`)
)

// IsRateLimited is isRateLimited($headers).
func (g *GitHub) IsRateLimited(headers []string) bool {
	for _, header := range headers {
		if ok, _ := rateLimitedRegex.IsMatch(php.Trim(header)); ok {
			return true
		}
	}

	return false
}

// RequiresSSO is requiresSso($headers).
func (g *GitHub) RequiresSSO(headers []string) bool {
	for _, header := range headers {
		if ok, _ := requiresSSORegex.IsMatch(php.Trim(header)); ok {
			return true
		}
	}

	return false
}
