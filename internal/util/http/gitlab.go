// Ports src/Composer/Util/GitLab.php.

package http

import (
	"errors"
	"slices"
	"time"

	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
)

// GitLab ports Composer\Util\GitLab: GitLab token setup and refresh.
type GitLab struct {
	io      io.IO
	config  Config
	process Process
	http    func() (Getter, error)
	now     func() time.Time
}

// NewGitLab is new GitLab($io, $config, $process, $httpDownloader); nil
// arguments are created as Composer creates them.
func NewGitLab(ioi io.IO, config Config, process Process, httpDownloader Getter) *GitLab {
	if process == nil {
		process = newProcess(ioi)
	}

	return &GitLab{io: ioi, config: config, process: process, http: lazyGetter(ioi, config, httpDownloader, nil), now: time.Now}
}

// Infallible: the pattern does bounded work per start position, so Preg
// cannot throw on it and its call sites ignore the error.
var portRegex = php.MustCompile(`{:\d+}`)

// AuthorizeOAuth is authorizeOAuth($originUrl): use a token from git
// config or the gitlab-token setting if there is one.
func (g *GitLab) AuthorizeOAuth(originURL string) bool {
	// before composer 1.9, origin URLs had no port number in them
	bcOriginURL, _, _ := portRegex.Replace(originURL, "", -1)

	domains := configList(g.config, "gitlab-domains")
	if !slices.Contains(domains, originURL) && !slices.Contains(domains, bcOriginURL) {
		return false
	}

	// if available use token from git config
	if token, ok := gitConfig(g.process, "gitlab.accesstoken"); ok {
		g.io.SetAuthentication(originURL, token, new("oauth2"))

		return true
	}

	// if available use deploy token from git config
	if tokenUser, ok := gitConfig(g.process, "gitlab.deploytoken.user"); ok {
		if tokenPassword, ok := gitConfig(g.process, "gitlab.deploytoken.token"); ok {
			g.io.SetAuthentication(originURL, tokenUser, &tokenPassword)

			return true
		}
	}

	// if available use token from composer config
	authTokens, _ := g.config.Get("gitlab-token").(*php.Array)

	var token any

	if authTokens != nil {
		if v, ok := authTokens.Get(originURL); ok && v != nil {
			token = v
		}

		if v, ok := authTokens.Get(bcOriginURL); ok && v != nil {
			token = v
		}
	}

	if token == nil {
		return false
	}

	var username, password string

	if a, ok := token.(*php.Array); ok {
		u, _ := a.Get("username")
		t, _ := a.Get("token")
		username, password = php.ToString(u), php.ToString(t)
	} else {
		username, password = php.ToString(token), "private-token"
	}

	// Composer expects the GitLab token to be stored as username and
	// 'private-token' or 'gitlab-ci-token' to be stored as password.
	// Detect cases where this is reversed and resolve it automatically.
	if username == "private-token" || username == "gitlab-ci-token" || username == "oauth2" {
		g.io.SetAuthentication(originURL, password, &username)
	} else {
		g.io.SetAuthentication(originURL, username, &password)
	}

	return true
}

// AuthorizeOAuthInteractively is authorizeOAuthInteractively($scheme,
// $originUrl, $message): create a token from the user's credentials (five
// attempts) and store it.
func (g *GitLab) AuthorizeOAuthInteractively(scheme, originURL, message string) (bool, error) {
	if message != "" {
		g.io.WriteError(message, true, io.Normal)
	}

	localAuthConfig := g.config.LocalAuthConfigSource()
	personalAccessTokenLink := scheme + "://" + originURL + "/-/user_settings/personal_access_tokens"
	revokeLink := scheme + "://" + originURL + "/-/user_settings/applications"

	g.io.WriteError(`A token will be created and stored in "`+sourceNames(localAuthConfig, g.config.AuthConfigSource())+`", your password will never be stored`, true, io.Normal)
	g.io.WriteError("To revoke access to this token you can visit:", true, io.Normal)
	g.io.WriteError(revokeLink, true, io.Normal)
	g.io.WriteError("Alternatively you can setup an personal access token on:", true, io.Normal)
	g.io.WriteError(personalAccessTokenLink, true, io.Normal)
	g.io.WriteError(`and store it under "gitlab-token" see https://getcomposer.org/doc/articles/authentication-for-private-packages.md#gitlab-token for more details.`, true, io.Normal)
	g.io.WriteError("https://getcomposer.org/doc/articles/authentication-for-private-packages.md#gitlab-token", true, io.Normal)
	g.io.WriteError("for more details.", true, io.Normal)

	storeInLocalAuthConfig := false

	if localAuthConfig != nil {
		answer, err := g.io.AskConfirmation("A local auth config source was found, do you want to store the token there?", true)
		if err != nil {
			return false, err
		}

		storeInLocalAuthConfig = answer
	}

	for range 5 {
		response, err := g.createToken(scheme, originURL)
		if err != nil {
			te, ok := errors.AsType[*util.TransportError](err)
			if !ok || (te.Code != 403 && te.Code != 401) {
				return false, err
			}

			// 401 is bad credentials, 403 is max login attempts exceeded
			if te.Code == 401 {
				decoded, _ := php.JSONDecode(authString(te.Response), true)
				if a, ok := decoded.(*php.Array); ok && a.At("error") == "invalid_grant" {
					g.io.WriteError("Bad credentials. If you have two factor authentication enabled you will have to manually create a personal access token", true, io.Normal)
				} else {
					g.io.WriteError("Bad credentials.", true, io.Normal)
				}
			} else {
				g.io.WriteError("Maximum number of login attempts exceeded. Please try again later.", true, io.Normal)
			}

			g.io.WriteError(`You can also manually create a personal access token enabling the "read_api" scope at:`, true, io.Normal)
			g.io.WriteError(personalAccessTokenLink, true, io.Normal)
			g.io.WriteError(`Add it using "composer config --global --auth gitlab-token.`+originURL+` <token>"`, true, io.Normal)

			continue
		}

		accessToken := php.ToString(response.At("access_token"))
		g.io.SetAuthentication(originURL, accessToken, new("oauth2"))

		authConfigSource := g.config.AuthConfigSource()
		if storeInLocalAuthConfig && localAuthConfig != nil {
			authConfigSource = localAuthConfig
		}

		// store value in user config in auth file
		var value any = accessToken
		if response.Has("expires_in") {
			value = oauthSetting(response)
		}

		if err := authConfigSource.AddConfigSetting("gitlab-oauth."+originURL, value); err != nil {
			return false, err
		}

		return true, nil
	}

	return false, &util.RuntimeError{Message: "Invalid GitLab credentials 5 times in a row, aborting."}
}

// oauthSetting is the gitlab-oauth entry of a token response.
func oauthSetting(response *php.Array) *php.Array {
	return php.ArrayOf(
		"expires-at", php.ToInt(response.At("created_at"))+php.ToInt(response.At("expires_in")),
		"refresh-token", response.At("refresh_token"),
		"token", response.At("access_token"),
	)
}

// AuthorizeOAuthRefresh is authorizeOAuthRefresh($scheme, $originUrl).
func (g *GitLab) AuthorizeOAuthRefresh(scheme, originURL string) (bool, error) {
	response, err := g.refreshToken(scheme, originURL)
	if err != nil {
		if te, ok := errors.AsType[*util.TransportError](err); ok {
			g.io.WriteError("Couldn't refresh access token: "+te.Message, true, io.Normal)

			return false, nil
		}

		return false, err
	}

	g.io.SetAuthentication(originURL, php.ToString(response.At("access_token")), new("oauth2"))

	// store value in user config in auth file
	if err := g.config.AuthConfigSource().AddConfigSetting("gitlab-oauth."+originURL, oauthSetting(response)); err != nil {
		return false, err
	}

	return true, nil
}

func (g *GitLab) createToken(scheme, originURL string) (*php.Array, error) {
	username, err := g.io.Ask("Username: ", nil)
	if err != nil {
		return nil, err
	}

	password, err := g.io.AskAndHideAnswer("Password: ")
	if err != nil {
		return nil, err
	}

	data := php.HTTPBuildQuery(
		"username", php.ToString(username),
		"password", php.ToString(password),
		"grant_type", "password",
	)

	token, err := g.postForm(scheme+"://"+originURL+"/oauth/token", data)
	if err != nil {
		return nil, err
	}

	g.io.WriteError("Token successfully created", true, io.Normal)

	return token, nil
}

// IsOAuthExpired is isOAuthExpired($originUrl).
func (g *GitLab) IsOAuthExpired(originURL string) bool {
	authTokens, _ := g.config.Get("gitlab-oauth").(*php.Array)

	entry, _ := authTokens.At(originURL).(*php.Array)
	if expiresAt := entry.At("expires-at"); expiresAt != nil {
		return php.Compare(expiresAt, g.now().Unix()) < 0
	}

	return false
}

func (g *GitLab) refreshToken(scheme, originURL string) (*php.Array, error) {
	authTokens, _ := g.config.Get("gitlab-oauth").(*php.Array)
	entry, _ := authTokens.At(originURL).(*php.Array)

	refreshToken := entry.At("refresh-token")
	if refreshToken == nil {
		return nil, &util.RuntimeError{Message: "No GitLab refresh token present for " + originURL + "."}
	}

	data := php.HTTPBuildQuery(
		"refresh_token", php.ToString(refreshToken),
		"grant_type", "refresh_token",
	)

	token, err := g.postForm(scheme+"://"+originURL+"/oauth/token", data)
	if err != nil {
		return nil, err
	}

	g.io.WriteError("GitLab token successfully refreshed", true, io.VeryVerbose)
	g.io.WriteError("To revoke access to this token you can visit "+scheme+"://"+originURL+"/-/user_settings/applications", true, io.VeryVerbose)

	return token, nil
}

// postForm posts a form to the token endpoint and decodes the JSON
// answer.
func (g *GitLab) postForm(url, data string) (*php.Array, error) {
	options := php.ArrayOf(
		"retry-auth-failure", false,
		"http", php.ArrayOf(
			"method", "POST",
			"header", php.ListOf("Content-Type: application/x-www-form-urlencoded"),
			"content", data,
		),
	)

	downloader, err := g.http()
	if err != nil {
		return nil, err
	}

	response, err := downloader.Get(url, options)
	if err != nil {
		return nil, err
	}

	token, err := response.DecodeJSONArray()
	if err != nil {
		return nil, err
	}

	if token == nil {
		token = php.NewArray()
	}

	return token, nil
}
