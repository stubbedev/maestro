// Ports src/Composer/Util/Forgejo.php.

package http

import (
	"errors"

	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
)

// Forgejo ports Composer\Util\Forgejo: access token setup.
type Forgejo struct {
	io     io.IO
	config Config
	http   Getter
}

// NewForgejo is new Forgejo($io, $config, $httpDownloader).
func NewForgejo(ioi io.IO, config Config, httpDownloader Getter) *Forgejo {
	return &Forgejo{io: ioi, config: config, http: httpDownloader}
}

// AuthorizeOAuthInteractively is authorizeOAuthInteractively($originUrl,
// $message): ask for a username and token, check them against the API and
// store them. An empty message is null.
func (f *Forgejo) AuthorizeOAuthInteractively(originURL, message string) (bool, error) {
	if message != "" {
		f.io.WriteError(message, true, io.Normal)
	}

	url := "https://" + originURL + "/user/settings/applications"
	f.io.WriteError("Setup a personal access token with repository:read permissions on:", true, io.Normal)
	f.io.WriteError(url, true, io.Normal)

	localAuthConfig := f.config.LocalAuthConfigSource()
	f.io.WriteError(`Tokens will be stored in plain text in "`+sourceNames(localAuthConfig, f.config.AuthConfigSource())+`" for future use by Composer.`, true, io.Normal)
	f.io.WriteError("For additional information, check https://getcomposer.org/doc/articles/authentication-for-private-packages.md#forgejo-token", true, io.Normal)

	storeInLocalAuthConfig := false

	if localAuthConfig != nil {
		answer, err := f.io.AskConfirmation("A local auth config source was found, do you want to store the token there?", true)
		if err != nil {
			return false, err
		}

		storeInLocalAuthConfig = answer
	}

	answer, err := f.io.Ask("Username: ", nil)
	if err != nil {
		return false, err
	}

	username := php.Trim(php.ToString(answer))

	if answer, err = f.io.AskAndHideAnswer("Token (hidden): "); err != nil {
		return false, err
	}

	token := php.Trim(php.ToString(answer))

	addTokenManually := `You can also add it manually later by using "composer config --global --auth forgejo-token.` + originURL + ` <username> <token>"`

	if token == "" || username == "" {
		f.io.WriteError("<warning>No username/token given, aborting.</warning>", true, io.Normal)
		f.io.WriteError(addTokenManually, true, io.Normal)

		return false, nil
	}

	f.io.SetAuthentication(originURL, username, &token)

	if _, err := f.http.Get("https://"+originURL+"/api/v1/version", php.ArrayOf("retry-auth-failure", false)); err != nil {
		if te, ok := errors.AsType[*util.TransportError](err); ok && (te.Code == 403 || te.Code == 401 || te.Code == 404) {
			f.io.WriteError("<error>Invalid access token provided.</error>", true, io.Normal)
			f.io.WriteError(addTokenManually, true, io.Normal)

			return false, nil
		}

		return false, err
	}

	// store value in local/user config
	authConfigSource := f.config.AuthConfigSource()
	if storeInLocalAuthConfig && localAuthConfig != nil {
		authConfigSource = localAuthConfig
	}

	if err := f.config.ConfigSource().RemoveConfigSetting("forgejo-token." + originURL); err != nil {
		return false, err
	}

	if err := authConfigSource.AddConfigSetting("forgejo-token."+originURL, php.ArrayOf("username", username, "token", token)); err != nil {
		return false, err
	}

	f.io.WriteError("<info>Token stored successfully.</info>", true, io.Normal)

	return true, nil
}
