// Ports src/Composer/IO/BaseIO.php (Composer).

package io

import (
	"github.com/stubbedev/maestro/internal/php"
)

// writer is the part of IO the shared BaseIO code dispatches to, like
// $this->writeError() resolving to the concrete class.
type writer interface {
	Write(message string, newline bool, verbosity Verbosity)
	WriteError(message string, newline bool, verbosity Verbosity)
}

// BaseIO holds the authentication store and the logger and configuration
// code shared by the IO implementations. Embedders call init with
// themselves so that writes reach their overrides.
type BaseIO struct {
	self  writer
	names []string
	auths map[string]Authentication
}

func (b *BaseIO) init(self writer) { b.self = self }

// Authentications returns the stored authentications in insertion order.
func (b *BaseIO) Authentications() []RepositoryAuthentication {
	out := make([]RepositoryAuthentication, len(b.names))
	for i, n := range b.names {
		out[i] = RepositoryAuthentication{Repository: n, Authentication: b.auths[n]}
	}

	return out
}

// ResetAuthentications empties the authentication store.
func (b *BaseIO) ResetAuthentications() {
	b.names = nil
	b.auths = nil
}

// HasAuthentication reports whether credentials are stored for the
// repository.
func (b *BaseIO) HasAuthentication(repositoryName string) bool {
	_, ok := b.auths[repositoryName]

	return ok
}

// Authentication returns the stored credentials, or a nil username and
// password.
func (b *BaseIO) Authentication(repositoryName string) Authentication {
	return b.auths[repositoryName]
}

// SetAuthentication stores credentials for the repository.
func (b *BaseIO) SetAuthentication(repositoryName, username string, password *string) {
	if b.auths == nil {
		b.auths = map[string]Authentication{}
	}
	if _, ok := b.auths[repositoryName]; !ok {
		b.names = append(b.names, repositoryName)
	}
	b.auths[repositoryName] = Authentication{Username: &username, Password: password}
}

// WriteRaw writes without formatting; BaseIO falls back to Write.
func (b *BaseIO) WriteRaw(message string, newline bool, verbosity Verbosity) {
	b.self.Write(message, newline, verbosity)
}

// WriteErrorRaw writes to the error output without formatting; BaseIO falls
// back to WriteError.
func (b *BaseIO) WriteErrorRaw(message string, newline bool, verbosity Verbosity) {
	b.self.WriteError(message, newline, verbosity)
}

func (b *BaseIO) checkAndSetAuthentication(repositoryName, username string, password *string) {
	if b.HasAuthentication(repositoryName) {
		auth := b.Authentication(repositoryName)
		if *auth.Username == username && equalNullable(auth.Password, password) {
			return
		}

		b.self.WriteError("<warning>Warning: You should avoid overwriting already defined auth settings for "+repositoryName+".</warning>", true, Normal)
	}
	b.SetAuthentication(repositoryName, username, password)
}

func equalNullable(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}

	return *a == *b
}

// array returns a config value as an array (an empty one when it is not).
func array(v any) *php.Array {
	if a, ok := v.(*php.Array); ok {
		return a
	}

	return php.NewArray()
}

// field is $array[$key] of a credentials array, as a string.
func field(v any, key string) string {
	f, _ := array(v).Get(key)

	return php.ToString(f)
}

// nullableField is $array[$key] ?? null.
func nullableField(v any, key string) *string {
	f, ok := array(v).Get(key)
	if !ok || f == nil {
		return nil
	}

	return new(php.ToString(f))
}

// LoadConfiguration loads the auth settings of config (loadConfiguration).
func (b *BaseIO) LoadConfiguration(config Config, setTimeout func(timeout int)) {
	bitbucketOauth := array(config.Get("bitbucket-oauth"))
	githubOauth := array(config.Get("github-oauth"))
	gitlabOauth := array(config.Get("gitlab-oauth"))
	gitlabToken := array(config.Get("gitlab-token"))
	forgejoToken := array(config.Get("forgejo-token"))
	httpBasic := array(config.Get("http-basic"))
	bearerToken := array(config.Get("bearer"))
	customHeaders := array(config.Get("custom-headers"))
	clientCertificate := array(config.Get("client-certificate"))

	// reload oauth tokens from config if available

	for domain, cred := range bitbucketOauth.All() {
		b.checkAndSetAuthentication(domain.String(), field(cred, "consumer-key"), new(field(cred, "consumer-secret")))
	}

	for domain, token := range githubOauth.All() {
		d := domain.String()
		if d != "github.com" {
			b.addImplicitDomain(config, "github-domains", d)
		}

		b.checkAndSetAuthentication(d, php.ToString(token), new("x-oauth-basic"))
	}

	for domain, token := range gitlabOauth.All() {
		d := domain.String()
		if d != "gitlab.com" {
			b.addImplicitDomain(config, "gitlab-domains", d)
		}

		t := php.ToString(token)
		if _, ok := token.(*php.Array); ok {
			t = field(token, "token")
		}
		b.checkAndSetAuthentication(d, t, new("oauth2"))
	}

	for domain, token := range gitlabToken.All() {
		d := domain.String()
		if d != "gitlab.com" {
			b.addImplicitDomain(config, "gitlab-domains", d)
		}

		username, password := php.ToString(token), "private-token"
		if _, ok := token.(*php.Array); ok {
			username, password = field(token, "username"), field(token, "token")
		}
		b.checkAndSetAuthentication(d, username, new(password))
	}

	for domain, cred := range forgejoToken.All() {
		d := domain.String()
		b.addImplicitDomain(config, "forgejo-domains", d)

		b.checkAndSetAuthentication(d, field(cred, "username"), new(field(cred, "token")))
	}

	// reload http basic credentials from config if available
	for domain, cred := range httpBasic.All() {
		b.checkAndSetAuthentication(domain.String(), field(cred, "username"), new(field(cred, "password")))
	}

	for domain, token := range bearerToken.All() {
		b.checkAndSetAuthentication(domain.String(), php.ToString(token), new("bearer"))
	}

	// load custom HTTP headers from config
	for domain, headers := range customHeaders.All() {
		if headers != nil {
			// (string) json_encode($headers): "" when encoding fails.
			encoded, _ := php.JSONEncode(headers, 0)
			b.checkAndSetAuthentication(domain.String(), encoded, new("custom-headers"))
		}
	}

	// reload ssl client certificate credentials from config if available
	for domain, cred := range clientCertificate.All() {
		sslOptions := php.NewArray()
		for _, key := range []string{"local_cert", "local_pk", "passphrase"} {
			if v := nullableField(cred, key); v != nil {
				sslOptions.Set(key, *v)
			}
		}
		if !sslOptions.Has("local_cert") {
			b.self.WriteError("<warning>Warning: Client certificate configuration is missing key `local_cert` for "+domain.String()+".</warning>", true, Normal)

			continue
		}
		encoded, _ := php.JSONEncode(sslOptions, 0)
		b.checkAndSetAuthentication(domain.String(), "client-certificate", new(encoded))
	}

	// setup process timeout
	if setTimeout != nil {
		setTimeout(int(php.ToInt(config.Get("process-timeout"))))
	}
}

// addImplicitDomain adds domain to the configured *-domains list when it is
// not in it yet.
func (b *BaseIO) addImplicitDomain(config Config, key, domain string) {
	domains := array(config.Get(key))
	if php.InArray(domain, domains, true) {
		return
	}

	b.Debug(domain+" is not in the configured "+key+", adding it implicitly as authentication is configured for this domain", nil)
	merged := php.ArrayMerge(domains, php.ListOf(domain))
	config.Merge(php.ArrayOf("config", php.ArrayOf(key, merged)), "implicit-due-to-auth")
}

// Emergency logs at the emergency level.
func (b *BaseIO) Emergency(message string, context *php.Array) {
	b.Log(LevelEmergency, message, context)
}

// Alert logs at the alert level.
func (b *BaseIO) Alert(message string, context *php.Array) { b.Log(LevelAlert, message, context) }

// Critical logs at the critical level.
func (b *BaseIO) Critical(message string, context *php.Array) {
	b.Log(LevelCritical, message, context)
}

// Error logs at the error level.
func (b *BaseIO) Error(message string, context *php.Array) { b.Log(LevelError, message, context) }

// Warning logs at the warning level.
func (b *BaseIO) Warning(message string, context *php.Array) {
	b.Log(LevelWarning, message, context)
}

// Notice logs at the notice level.
func (b *BaseIO) Notice(message string, context *php.Array) { b.Log(LevelNotice, message, context) }

// Info logs at the info level.
func (b *BaseIO) Info(message string, context *php.Array) { b.Log(LevelInfo, message, context) }

// Debug logs at the debug level.
func (b *BaseIO) Debug(message string, context *php.Array) { b.Log(LevelDebug, message, context) }

// Log writes message to the error output with the verbosity and style of
// level; a non-empty context is appended as JSON.
func (b *BaseIO) Log(level, message string, context *php.Array) {
	if context != nil && context.Len() > 0 {
		json, err := php.JSONEncode(context, php.JSONInvalidUTF8Ignore|php.JSONUnescapedSlashes|php.JSONUnescapedUnicode)
		if err == nil {
			message += " " + json
		}
	}

	switch level {
	case LevelEmergency, LevelAlert, LevelCritical, LevelError:
		b.self.WriteError("<error>"+message+"</error>", true, Normal)
	case LevelWarning:
		b.self.WriteError("<warning>"+message+"</warning>", true, Normal)
	case LevelNotice:
		b.self.WriteError("<info>"+message+"</info>", true, Verbose)
	case LevelInfo:
		b.self.WriteError("<info>"+message+"</info>", true, VeryVerbose)
	default:
		b.self.WriteError(message, true, Debug)
	}
}
