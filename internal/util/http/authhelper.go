// Ports src/Composer/Util/AuthHelper.php.

package http

import (
	"encoding/base64"
	"strconv"
	"strings"
	"sync"

	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
)

// StoreAuth is the $storeAuth value of AuthHelper: false, true or
// 'prompt'.
type StoreAuth int

// The StoreAuth values.
const (
	StoreAuthNo StoreAuth = iota
	StoreAuthYes
	StoreAuthPrompt
)

// storeAuthOf converts the store-auths config value.
func storeAuthOf(v any) StoreAuth {
	switch v {
	case true:
		return StoreAuthYes
	case "prompt":
		return StoreAuthPrompt
	}

	return StoreAuthNo
}

// AuthResult is promptAuthIfNeeded()'s ['retry' => ..., 'storeAuth' =>
// ...].
type AuthResult struct {
	Retry     bool
	StoreAuth StoreAuth
}

// AuthHelper ports Composer\Util\AuthHelper: adding credentials to
// requests and asking for them when a server refuses one.
type AuthHelper struct {
	io     io.IO
	config Config
	rt     Runtime

	mu                             sync.Mutex
	displayedOriginAuthentications map[string]string
	bitbucketRetry                 map[string]bool
	// newGitHub & co create the utilities promptAuthIfNeeded uses; tests
	// replace them.
	newGitHub    func() *GitHub
	newGitLab    func() *GitLab
	newBitbucket func() *Bitbucket
}

// NewAuthHelper is new AuthHelper($io, $config).
func NewAuthHelper(ioi io.IO, config Config) *AuthHelper {
	h := &AuthHelper{io: ioi, config: config}
	h.newGitHub = func() *GitHub {
		g := NewGitHub(ioi, config, nil, nil)
		g.http = lazyGetter(ioi, config, nil, h.rt)

		return g
	}
	h.newGitLab = func() *GitLab {
		g := NewGitLab(ioi, config, nil, nil)
		g.http = lazyGetter(ioi, config, nil, h.rt)

		return g
	}
	h.newBitbucket = func() *Bitbucket {
		b := NewBitbucket(ioi, config, nil, nil, 0)
		b.http = lazyGetter(ioi, config, nil, h.rt)

		return b
	}

	return h
}

// StoreAuth is storeAuth($origin, $storeAuth): save the origin's
// credentials as http-basic in the auth config, asking first for 'prompt'.
func (h *AuthHelper) StoreAuth(origin string, storeAuth StoreAuth) error {
	configSource := h.config.AuthConfigSource()

	var store ConfigSource

	switch storeAuth {
	case StoreAuthYes:
		store = configSource
	case StoreAuthPrompt:
		answer, err := h.io.AskAndValidate(
			"Do you want to store credentials for "+origin+" in "+configSource.Name()+" ? [Yn] ",
			func(value any) (any, error) {
				input := php.Strtolower(php.SubstrLen(php.Trim(php.ToString(value)), 0, 1))
				if input == "y" || input == "n" {
					return input, nil
				}

				return nil, &util.RuntimeError{Message: "Please answer (y)es or (n)o"}
			},
			0,
			"y",
		)
		if err != nil {
			return err
		}

		if answer == "y" {
			store = configSource
		}
	}

	if store != nil {
		return store.AddConfigSetting("http-basic."+origin, authArray(h.io.Authentication(origin)))
	}

	return nil
}

// PromptAuthIfNeeded is promptAuthIfNeeded($url, $origin, $statusCode,
// $reason, $headers, $retryCount, $responseBody): after a 401/403 (or a
// GitLab/Bitbucket login page), get credentials for origin and say whether
// to retry. reason and responseBody "" are null.
func (h *AuthHelper) PromptAuthIfNeeded(url, origin string, statusCode int, reason string, headers []string, retryCount int, responseBody string) (AuthResult, error) {
	storeAuth := StoreAuthNo

	switch {
	case configHas(h.config, "github-domains", origin):
		// if two or more requests are started together for the same GitHub
		// origin, the first one will prompt for and store a token; the
		// others retry with that token before prompting again
		if h.io.HasAuthentication(origin) && retryCount == 0 {
			return AuthResult{Retry: true}, nil
		}

		gitHubUtil := h.newGitHub()
		message := "\n"
		rateLimited := gitHubUtil.IsRateLimited(headers)

		if gitHubUtil.RequiresSSO(headers) {
			ssoURL, _ := gitHubUtil.SSOURL(headers)
			message = "GitHub API token requires SSO authorization. Authorize this token at " + util.SanitizeURL(ssoURL) + "\n"
			h.io.WriteError(message, true, io.Normal)

			if !h.io.IsInteractive() {
				return AuthResult{}, util.NewTransportError("Could not authenticate against "+origin, 403)
			}

			if _, err := h.io.Ask("After authorizing your token, confirm that you would like to retry the request", nil); err != nil {
				return AuthResult{}, err
			}

			return AuthResult{Retry: true, StoreAuth: storeAuth}, nil
		}

		if rateLimited {
			rateLimit := gitHubUtil.RateLimit(headers)

			advice := "Create a GitHub OAuth token to go over the API rate limit."
			if h.io.HasAuthentication(origin) {
				advice = "Review your configured GitHub OAuth token or enter a new one to go over the API rate limit."
			}

			message = "GitHub API limit (" + strconv.Itoa(rateLimit.Limit) + " calls/hr) is exhausted, could not fetch " + util.SanitizeURL(url) + ". " + advice + " You can also wait until " + rateLimit.Reset + " for the rate limit to reset.\n"
		} else {
			// Try to extract a more specific error message from GitHub's API
			// response.
			apiMessage, hasAPIMessage := "", false

			if responseBody != "" {
				decoded, _ := php.JSONDecode(responseBody, true)
				if a, ok := decoded.(*php.Array); ok {
					apiMessage, hasAPIMessage = arrayValue(a, "message").(string)
				}
			}

			if hasAPIMessage {
				message += "Could not fetch " + util.SanitizeURL(url) + ": " + apiMessage
			} else {
				message += "Could not fetch " + util.SanitizeURL(url) + ", please "
				if h.io.HasAuthentication(origin) {
					message += "review your configured GitHub OAuth token or enter a new one to access private repos"
				} else {
					message += "create a GitHub OAuth token to access private repos"
				}
			}
		}

		if !gitHubUtil.AuthorizeOAuth(origin) {
			ok := false
			if h.io.IsInteractive() {
				var err error
				if ok, err = gitHubUtil.AuthorizeOAuthInteractively(origin, message); err != nil {
					return AuthResult{}, err
				}
			}

			if !ok {
				return AuthResult{}, util.NewTransportError("Could not authenticate against "+origin, 401)
			}
		}

	case configHas(h.config, "gitlab-domains", origin):
		purpose := "to go over the API rate limit"
		if statusCode == 401 {
			purpose = "to access private repos"
		}

		message := "\nCould not fetch " + util.SanitizeURL(url) + ", enter your " + origin + " credentials " + purpose
		gitLabUtil := h.newGitLab()

		var auth *io.Authentication

		if h.io.HasAuthentication(origin) {
			a := h.io.Authentication(origin)
			auth = &a

			switch authString(a.Password) {
			case "gitlab-ci-token", "private-token", "oauth2":
				if a.Password != nil {
					return AuthResult{}, util.NewTransportError("Invalid credentials for '"+util.SanitizeURL(url)+"', aborting.", statusCode)
				}
			}
		}

		if !gitLabUtil.AuthorizeOAuth(origin) {
			ok := false
			if h.io.IsInteractive() {
				var err error
				if ok, err = gitLabUtil.AuthorizeOAuthInteractively(util.URLScheme(url), origin, message); err != nil {
					return AuthResult{}, err
				}
			}

			if !ok {
				return AuthResult{}, util.NewTransportError("Could not authenticate against "+origin, 401)
			}
		}

		if auth != nil && h.io.HasAuthentication(origin) && sameAuthentication(*auth, h.io.Authentication(origin)) {
			return AuthResult{}, util.NewTransportError("Invalid credentials for '"+util.SanitizeURL(url)+"', aborting.", statusCode)
		}

	case origin == "bitbucket.org" || origin == "api.bitbucket.org":
		askForOAuthToken := true
		origin = "bitbucket.org"

		if h.io.HasAuthentication(origin) {
			auth := h.io.Authentication(origin)

			switch {
			case authString(auth.Username) != "x-token-auth":
				accessToken, err := h.newBitbucket().RequestToken(origin, authString(auth.Username), authString(auth.Password))
				if err != nil {
					return AuthResult{}, err
				}

				if accessToken != "" && accessToken != "0" {
					h.io.SetAuthentication(origin, "x-token-auth", &accessToken)
					askForOAuthToken = false
				}
			case !h.bitbucketRetried(url):
				// when multiple requests fire at the same time, they all fail
				// and the first one resets the token above; the others reach
				// this path and retry once instead of failing
				askForOAuthToken = false
			default:
				return AuthResult{}, util.NewTransportError("Could not authenticate against "+origin, 401)
			}
		}

		if askForOAuthToken {
			purpose := "go over the API rate limit"
			if statusCode == 401 || statusCode == 403 {
				purpose = "access private repos"
			}

			message := "\nCould not fetch " + util.SanitizeURL(url) + ", please create a bitbucket OAuth token to " + purpose
			bitBucketUtil := h.newBitbucket()

			if !bitBucketUtil.AuthorizeOAuth(origin) {
				ok := false
				if h.io.IsInteractive() {
					var err error
					if ok, err = bitBucketUtil.AuthorizeOAuthInteractively(origin, message); err != nil {
						return AuthResult{}, err
					}
				}

				if !ok {
					return AuthResult{}, util.NewTransportError("Could not authenticate against "+origin, 401)
				}
			}
		}

	default:
		// 404s are only handled for github
		if statusCode == 404 {
			return AuthResult{}, nil
		}

		// fail if the console is not interactive
		if !h.io.IsInteractive() {
			var message string

			switch statusCode {
			case 401:
				message = "The '" + util.SanitizeURL(url) + "' URL required authentication (HTTP 401).\nYou must be using the interactive console to authenticate"
			case 403:
				message = "The '" + util.SanitizeURL(url) + "' URL could not be accessed (HTTP 403): " + reason
			default:
				message = "Unknown error code '" + strconv.Itoa(statusCode) + "', reason: " + reason
			}

			return AuthResult{}, util.NewTransportError(message, statusCode)
		}

		// fail if we already have auth
		if h.io.HasAuthentication(origin) {
			// if two or more requests are started together for the same
			// host, and the first received authentication already, the
			// others retry before failing
			if retryCount == 0 {
				return AuthResult{Retry: true}, nil
			}

			return AuthResult{}, util.NewTransportError("Invalid credentials (HTTP "+strconv.Itoa(statusCode)+") for '"+util.SanitizeURL(url)+"', aborting.", statusCode)
		}

		h.io.WriteError("    Authentication required (<info>"+origin+"</info>):", true, io.Normal)

		username, err := h.io.Ask("      Username: ", nil)
		if err != nil {
			return AuthResult{}, err
		}

		password, err := h.io.AskAndHideAnswer("      Password: ")
		if err != nil {
			return AuthResult{}, err
		}

		var pass *string
		if password != nil {
			pass = new(php.ToString(password))
		}

		h.io.SetAuthentication(origin, php.ToString(username), pass)
		storeAuth = storeAuthOf(h.config.Get("store-auths"))
	}

	return AuthResult{Retry: true, StoreAuth: storeAuth}, nil
}

// bitbucketRetried marks url as retried once and reports whether it
// already was.
func (h *AuthHelper) bitbucketRetried(url string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.bitbucketRetry[url] {
		return true
	}

	if h.bitbucketRetry == nil {
		h.bitbucketRetry = map[string]bool{}
	}

	h.bitbucketRetry[url] = true

	return false
}

// AddAuthenticationHeader is the deprecated addAuthenticationHeader($headers,
// $origin, $url).
func (h *AuthHelper) AddAuthenticationHeader(headers []string, origin, url string) []string {
	util.TriggerDeprecation("AuthHelper::addAuthenticationHeader is deprecated since Composer 2.9 use addAuthenticationOptions instead.")

	options := h.AddAuthenticationOptions(php.ArrayOf("http", php.ArrayOf("header", php.StringList(headers))), origin, url)

	return headerList(options)
}

// Infallible: the pattern does bounded work per start position, so Preg
// cannot throw on it and its call sites ignore the error.
var gitHubAPIRegex = php.MustCompile(`{^https?://api\.github\.com/}`)

// AddAuthenticationOptions is addAuthenticationOptions($options, $origin,
// $url): a copy of options with the origin's credentials added as headers
// (or ssl options for client certificates).
func (h *AuthHelper) AddAuthenticationOptions(options *php.Array, origin, url string) *php.Array {
	return h.addAuthenticationOptions(cloneOptions(options), origin, url)
}

// addAuthenticationOptions is AddAuthenticationOptions modifying options,
// which the caller owns.
func (h *AuthHelper) addAuthenticationOptions(options *php.Array, origin, url string) *php.Array {
	httpOptions := HTTPOptions(options)

	headerLines, isArray := arrayValue(httpOptions, "header").(*php.Array)
	if !isArray {
		existing := arrayValue(httpOptions, "header")
		headerLines = php.NewArray()

		if existing != nil {
			headerLines.Append(existing)
		}

		httpOptions.Set("header", headerLines)
	}

	authOrigin, found := FindAuthOrigin(h.io, origin)
	if !found {
		return options
	}

	origin = authOrigin
	displayMessage := ""
	auth := h.io.Authentication(origin)
	username, password := authString(auth.Username), authString(auth.Password)

	switch {
	case password == "bearer":
		headerLines.Append("Authorization: Bearer " + username)
	case password == "custom-headers":
		// Handle custom HTTP headers from auth.json
		if auth.Username != nil {
			decoded, _ := php.JSONDecode(username, true)
			if custom, ok := decoded.(*php.Array); ok {
				for _, header := range custom.All() {
					headerLines.Append(header)
				}

				displayMessage = "Using custom HTTP headers for authentication"
			}
		}
	case origin == "github.com" && password == "x-oauth-basic": //nolint:gosec // a credential marker, not a credential
		// only add the access_token if it is actually a github API URL
		if ok, _ := gitHubAPIRegex.IsMatch(url); ok {
			headerLines.Append("Authorization: token " + username)
			displayMessage = "Using GitHub token authentication"
		}
	case (password == "oauth2" || password == "private-token" || password == "gitlab-ci-token") && configHas(h.config, "gitlab-domains", origin): //nolint:gosec // credential markers
		if password == "oauth2" {
			headerLines.Append("Authorization: Bearer " + username)
			displayMessage = "Using GitLab OAuth token authentication"
		} else {
			headerLines.Append("PRIVATE-TOKEN: " + username)
			displayMessage = "Using GitLab private token authentication"
		}
	case origin == "bitbucket.org" && url != BitbucketOAuth2AccessTokenURL && username == "x-token-auth":
		if !IsPublicBitBucketDownload(url) {
			headerLines.Append("Authorization: Bearer " + password)
			displayMessage = "Using Bitbucket OAuth token authentication"
		}
	case username == "client-certificate":
		ssl, _ := arrayValue(options, "ssl").(*php.Array)
		if ssl == nil {
			ssl = php.NewArray()
		}

		decoded, _ := php.JSONDecode(password, true)
		if cert, ok := decoded.(*php.Array); ok {
			ssl = php.ArrayMerge(ssl, cert)
		}

		options.Set("ssl", ssl)
		displayMessage = "Using SSL client certificate"
	default:
		headerLines.Append("Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password)))
		displayMessage = `Using HTTP basic authentication with username "` + util.SanitizeUsername(username) + `"`
	}

	if displayMessage != "" && h.markDisplayed(origin, displayMessage) {
		h.io.WriteError(displayMessage, true, io.Debug)
	}

	return options
}

// markDisplayed records the authentication message shown for origin and
// reports whether it changed.
func (h *AuthHelper) markDisplayed(origin, message string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.displayedOriginAuthentications[origin] == message {
		return false
	}

	if h.displayedOriginAuthentications == nil {
		h.displayedOriginAuthentications = map[string]string{}
	}

	h.displayedOriginAuthentications[origin] = message

	return true
}

// FindAuthOrigin is AuthHelper::findAuthOrigin($io, $origin): the origin
// whose credentials apply (api.github.com and api.bitbucket.org fall back
// to their canonical hosts).
func FindAuthOrigin(ioi io.IO, origin string) (string, bool) {
	if ioi.HasAuthentication(origin) {
		return origin, true
	}

	if origin == "api.bitbucket.org" || origin == "api.github.com" {
		canonical := strings.ReplaceAll(origin, "api.", "")
		if ioi.HasAuthentication(canonical) {
			return canonical, true
		}
	}

	return "", false
}

// IsPublicBitBucketDownload is isPublicBitBucketDownload($url): Bitbucket
// downloads hosted elsewhere and /{user}/{repo}/downloads/ paths need no
// authentication.
func IsPublicBitBucketDownload(urlToBitBucketFile string) bool {
	if !strings.Contains(util.URLHost(urlToBitBucketFile), "bitbucket.org") {
		// Bitbucket downloads are hosted on amazonaws. We do not need to
		// authenticate there at all.
		return true
	}

	pathParts := strings.Split(util.URLPath(urlToBitBucketFile), "/")

	return len(pathParts) >= 4 && pathParts[3] == "downloads"
}
