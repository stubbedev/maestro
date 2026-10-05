// Ports src/Composer/Util/Bitbucket.php.

package http

import (
	"errors"
	"time"

	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/util"
)

// BitbucketOAuth2AccessTokenURL is Bitbucket::OAUTH2_ACCESS_TOKEN_URL.
const BitbucketOAuth2AccessTokenURL = "https://bitbucket.org/site/oauth2/access_token" //nolint:gosec // a URL, not a credential

// Bitbucket ports Composer\Util\Bitbucket: OAuth consumer setup and access
// tokens.
type Bitbucket struct {
	io      io.IO
	config  Config
	process Process
	http    func() (Getter, error)
	// token is the access token response, nil for null.
	token *php.Array
	// time is the fixed time() of tests; zero uses the clock.
	time int64
	now  func() time.Time
}

// NewBitbucket is new Bitbucket($io, $config, $process, $httpDownloader,
// $time); nil arguments are created as Composer creates them and a zero
// time means time().
func NewBitbucket(ioi io.IO, config Config, process Process, httpDownloader Getter, t int64) *Bitbucket {
	if process == nil {
		process = newProcess(ioi)
	}

	return &Bitbucket{io: ioi, config: config, process: process, http: lazyGetter(ioi, config, httpDownloader, nil), time: t, now: time.Now}
}

// Token is getToken(): the access token, "" when there is none.
func (b *Bitbucket) Token() string {
	return php.ToString(arrayValue(b.token, "access_token"))
}

// AuthorizeOAuth is authorizeOAuth($originUrl): use the token in git
// config (bitbucket.accesstoken) if there is one.
func (b *Bitbucket) AuthorizeOAuth(originURL string) bool {
	if originURL != "bitbucket.org" {
		return false
	}

	if token, ok := gitConfig(b.process, "bitbucket.accesstoken"); ok {
		b.io.SetAuthentication(originURL, "x-token-auth", &token)

		return true
	}

	return false
}

func (b *Bitbucket) requestAccessToken() (bool, error) {
	downloader, err := b.http()
	if err != nil {
		return false, err
	}

	response, err := downloader.Get(BitbucketOAuth2AccessTokenURL, php.ArrayOf(
		"retry-auth-failure", false,
		"http", php.ArrayOf(
			"method", "POST",
			"content", "grant_type=client_credentials",
		),
	))
	if err == nil {
		var token *php.Array

		if token, err = response.DecodeJSONArray(); err == nil {
			if arrayValue(token, "expires_in") == nil || arrayValue(token, "access_token") == nil {
				encoded, _ := php.JSONEncode(decodedOrNull(token), 0)

				return false, &util.LogicError{Message: "Expected a token configured with expires_in and access_token present, got " + encoded, Site: phperr.At("Bitbucket.php", 102)}
			}

			b.token = token

			return true, nil
		}
	}

	te, ok := errors.AsType[*util.TransportError](err)
	if !ok {
		return false, err
	}

	switch te.Code {
	case 400:
		b.io.WriteError("<error>Invalid OAuth consumer provided.</error>", true, io.Normal)
		b.io.WriteError("This can have three reasons:", true, io.Normal)
		b.io.WriteError("1. You are authenticating with a bitbucket username/password combination", true, io.Normal)
		b.io.WriteError("2. You are using an OAuth consumer, but didn't configure a (dummy) callback url", true, io.Normal)
		b.io.WriteError("3. You are using an OAuth consumer, but didn't configure it as private consumer", true, io.Normal)

		return false, nil
	case 403, 401:
		b.io.WriteError("<error>Invalid OAuth consumer provided.</error>", true, io.Normal)
		b.io.WriteError(`You can also add it manually later by using "composer config --global --auth bitbucket-oauth.bitbucket.org <consumer-key> <consumer-secret>"`, true, io.Normal)

		return false, nil
	}

	return false, err
}

// decodedOrNull gives json_encode a nil array as null.
func decodedOrNull(a *php.Array) any {
	if a == nil {
		return nil
	}

	return a
}

// AuthorizeOAuthInteractively is authorizeOAuthInteractively($originUrl,
// $message): ask for an OAuth consumer, fetch a token and store both.
func (b *Bitbucket) AuthorizeOAuthInteractively(originURL, message string) (bool, error) {
	if message != "" {
		b.io.WriteError(message, true, io.Normal)
	}

	localAuthConfig := b.config.LocalAuthConfigSource()
	url := "https://support.atlassian.com/bitbucket-cloud/docs/use-oauth-on-bitbucket-cloud/"
	b.io.WriteError("Follow the instructions here:", true, io.Normal)
	b.io.WriteError(url, true, io.Normal)
	b.io.WriteError(`to create a consumer. It will be stored in "`+sourceNames(localAuthConfig, b.config.AuthConfigSource())+`" for future use by Composer.`, true, io.Normal)
	b.io.WriteError(`Ensure you enter a "Callback URL" (http://example.com is fine) or it will not be possible to create an Access Token (this callback url will not be used by composer)`, true, io.Normal)

	storeInLocalAuthConfig := false

	if localAuthConfig != nil {
		answer, err := b.io.AskConfirmation("A local auth config source was found, do you want to store the token there?", true)
		if err != nil {
			return false, err
		}

		storeInLocalAuthConfig = answer
	}

	const manually = `You can also add it manually later by using "composer config --global --auth bitbucket-oauth.bitbucket.org <consumer-key> <consumer-secret>"`

	answer, err := b.io.AskAndHideAnswer("Consumer Key (hidden): ")
	if err != nil {
		return false, err
	}

	consumerKey := php.Trim(php.ToString(answer))
	if !php.ToBool(consumerKey) {
		b.io.WriteError("<warning>No consumer key given, aborting.</warning>", true, io.Normal)
		b.io.WriteError(manually, true, io.Normal)

		return false, nil
	}

	if answer, err = b.io.AskAndHideAnswer("Consumer Secret (hidden): "); err != nil {
		return false, err
	}

	consumerSecret := php.Trim(php.ToString(answer))
	if !php.ToBool(consumerSecret) {
		b.io.WriteError("<warning>No consumer secret given, aborting.</warning>", true, io.Normal)
		b.io.WriteError(manually, true, io.Normal)

		return false, nil
	}

	b.io.SetAuthentication(originURL, consumerKey, &consumerSecret)

	if ok, err := b.requestAccessToken(); !ok || err != nil {
		return false, err
	}

	// store value in user config
	authConfigSource := b.config.AuthConfigSource()
	if storeInLocalAuthConfig && localAuthConfig != nil {
		authConfigSource = localAuthConfig
	}

	if err := b.storeInAuthConfig(authConfigSource, originURL, consumerKey, consumerSecret); err != nil {
		return false, err
	}

	// Remove conflicting basic auth credentials (if available)
	if err := b.config.AuthConfigSource().RemoveConfigSetting("http-basic." + originURL); err != nil {
		return false, err
	}

	b.io.WriteError("<info>Consumer stored successfully.</info>", true, io.Normal)

	return true, nil
}

// RequestToken is requestToken($originUrl, $consumerKey, $consumerSecret):
// a stored unexpired token, or a new one fetched with the consumer; ""
// when the consumer is refused.
func (b *Bitbucket) RequestToken(originURL, consumerKey, consumerSecret string) (string, error) {
	if b.token != nil || b.tokenFromConfig(originURL) {
		return php.ToString(arrayValue(b.token, "access_token")), nil
	}

	b.io.SetAuthentication(originURL, consumerKey, &consumerSecret)

	if ok, err := b.requestAccessToken(); !ok || err != nil {
		return "", err
	}

	source := b.config.LocalAuthConfigSource()
	if source == nil {
		source = b.config.AuthConfigSource()
	}

	if err := b.storeInAuthConfig(source, originURL, consumerKey, consumerSecret); err != nil {
		return "", err
	}

	if arrayValue(b.token, "access_token") == nil {
		return "", &util.LogicError{Message: "Failed to initialize token above", Site: phperr.At("Bitbucket.php", 209)}
	}

	return php.ToString(arrayValue(b.token, "access_token")), nil
}

// storeInAuthConfig stores the consumer and its token. Like Composer, it
// writes to the global auth source whatever authConfigSource says.
func (b *Bitbucket) storeInAuthConfig(_ ConfigSource, originURL, consumerKey, consumerSecret string) error {
	if err := b.config.ConfigSource().RemoveConfigSetting("bitbucket-oauth." + originURL); err != nil {
		return err
	}

	if b.token == nil || arrayValue(b.token, "expires_in") == nil {
		encoded, _ := php.JSONEncode(decodedOrNull(b.token), 0)

		return &util.LogicError{Message: "Expected a token configured with expires_in present, got " + encoded, Site: phperr.At("Bitbucket.php", 223)}
	}

	t := b.time
	if t == 0 {
		t = b.now().Unix()
	}

	consumer := php.ArrayOf(
		"consumer-key", consumerKey,
		"consumer-secret", consumerSecret,
		"access-token", arrayValue(b.token, "access_token"),
		"access-token-expiration", t+php.ToInt(arrayValue(b.token, "expires_in")),
	)

	return b.config.AuthConfigSource().AddConfigSetting("bitbucket-oauth."+originURL, consumer)
}

// tokenFromConfig is getTokenFromConfig($originUrl).
func (b *Bitbucket) tokenFromConfig(originURL string) bool {
	authConfig, _ := b.config.Get("bitbucket-oauth").(*php.Array)
	entry, _ := arrayValue(authConfig, originURL).(*php.Array)

	accessToken := arrayValue(entry, "access-token")
	expiration := arrayValue(entry, "access-token-expiration")

	if accessToken == nil || expiration == nil || php.Compare(b.now().Unix(), expiration) > 0 {
		return false
	}

	b.token = php.ArrayOf("access_token", accessToken)

	return true
}
