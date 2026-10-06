// Ports src/Composer/IO/BaseIO.php (Composer).

package io

import (
	"errors"
	"strconv"
	"sync"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
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
//
// The store has its own lock: PHP has one thread, but here the metadata
// prefetches and the speculated loads (internal/repository/composerrepo)
// read credentials from other goroutines while the main one may store
// them (a plugin, or a prompt for credentials, during the pool build).
type BaseIO struct {
	self  writer
	mu    sync.RWMutex
	names []string
	auths map[string]Authentication
}

func (b *BaseIO) init(self writer) { b.self = self }

// Authentications returns the stored authentications in insertion order.
func (b *BaseIO) Authentications() []RepositoryAuthentication {
	b.mu.RLock()
	defer b.mu.RUnlock()

	out := make([]RepositoryAuthentication, len(b.names))
	for i, n := range b.names {
		out[i] = RepositoryAuthentication{Repository: n, Authentication: b.auths[n]}
	}

	return out
}

// ResetAuthentications empties the authentication store.
func (b *BaseIO) ResetAuthentications() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.names = nil
	b.auths = nil
}

// HasAuthentication reports whether credentials are stored for the
// repository.
func (b *BaseIO) HasAuthentication(repositoryName string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()

	_, ok := b.auths[repositoryName]

	return ok
}

// Authentication returns the stored credentials, or a nil username and
// password.
func (b *BaseIO) Authentication(repositoryName string) Authentication {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return b.auths[repositoryName]
}

// SetAuthentication stores credentials for the repository.
func (b *BaseIO) SetAuthentication(repositoryName, username string, password *string) {
	b.mu.Lock()
	defer b.mu.Unlock()

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

// array returns a config value as an array (an empty one when it is not:
// Config::get gives arrays for the auth keys, Config::merge rejects
// others).
func array(v any) *php.Array {
	if a, ok := v.(*php.Array); ok {
		return a
	}

	return php.NewArray()
}

// NewWarning makes the \ErrorException Composer's ErrorHandler throws for
// a PHP warning raised at site, or nil when the warning is silenced (the
// code then goes on with null); internal/util, above this package, sets
// it to its ErrorException.
var NewWarning = func(message string, site phperr.Site) error { return errors.New(message) }

const baseIOFile = "BaseIO.php"

// dim is $v[$key] read (not in isset()) at line of BaseIO.php: a missing
// key is the "Undefined array key" warning, a string the TypeError of a
// string offset, other scalars the "Trying to access array offset"
// warning.
func dim(v any, key string, line int) (any, error) {
	switch c := v.(type) {
	case *php.Array:
		f, ok := c.Get(key)
		if !ok {
			return nil, NewWarning(`Undefined array key "`+key+`"`, phperr.At(baseIOFile, line)) //nolint:nilnil // null when silenced
		}

		return f, nil
	case string:
		return nil, (&php.EngineError{Class: "TypeError", Message: "Cannot access offset of type string on string"}).Raised("", baseIOFile, line)
	}

	return nil, NewWarning("Trying to access array offset on "+php.ZvalValueName(v), phperr.At(baseIOFile, line))
}

// authenticate is checkAndSetAuthentication(string $repositoryName,
// string $username, ?string $password) called at line of BaseIO.php with
// a configuration's values, whose types strict_types checks.
func (b *BaseIO) authenticate(domain php.Key, username, password any, line int) error {
	for i, a := range []struct {
		name     string
		v        any
		nullable bool
	}{{"repositoryName", domain.Value(), false}, {"username", username, false}, {"password", password, true}} {
		if _, ok := a.v.(string); ok || a.nullable && a.v == nil {
			continue
		}
		typ := "string"
		if a.nullable {
			typ = "?string"
		}

		return (&php.EngineError{Class: "TypeError", Message: `Composer\IO\BaseIO::checkAndSetAuthentication(): Argument #` + strconv.Itoa(i+1) + " ($" + a.name + ") must be of type " + typ + ", " + php.ZvalValueName(a.v) + " given"}).
			Called(`Composer\IO\BaseIO->checkAndSetAuthentication`, phperr.At(baseIOFile, 95), baseIOFile, line)
	}
	var pw *string
	if s, ok := password.(string); ok {
		pw = &s
	}
	user, _ := username.(string) // checked above
	b.checkAndSetAuthentication(domain.String(), user, pw)

	return nil
}

// credentials reads $cred[$k1] and $cred[$k2] at line, in order.
func credentials(cred any, k1, k2 string, line int) (any, any, error) {
	v1, err := dim(cred, k1, line)
	if err != nil {
		return nil, nil, err
	}
	v2, err := dim(cred, k2, line)

	return v1, v2, err
}

// LoadConfiguration loads the auth settings of config (loadConfiguration).
// Values of the wrong type fail as under Composer's strict_types: the
// TypeErrors of checkAndSetAuthentication() and
// ProcessExecutor::setTimeout(int), the warnings of missing keys.
func (b *BaseIO) LoadConfiguration(config Config, setTimeout func(timeout int)) error {
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
		key, secret, err := credentials(cred, "consumer-key", "consumer-secret", 131)
		if err != nil {
			return err
		}
		if err := b.authenticate(domain, key, secret, 131); err != nil {
			return err
		}
	}

	for domain, token := range githubOauth.All() {
		if d := domain.Value(); d != "github.com" {
			b.addImplicitDomain(config, "github-domains", domain.String())
		}

		if err := b.authenticate(domain, token, "x-oauth-basic", 140); err != nil {
			return err
		}
	}

	for domain, token := range gitlabOauth.All() {
		if d := domain.Value(); d != "gitlab.com" {
			b.addImplicitDomain(config, "gitlab-domains", domain.String())
		}

		if _, ok := token.(*php.Array); ok {
			var err error
			if token, err = dim(token, "token", 149); err != nil {
				return err
			}
		}
		if err := b.authenticate(domain, token, "oauth2", 150); err != nil {
			return err
		}
	}

	for domain, token := range gitlabToken.All() {
		if d := domain.Value(); d != "gitlab.com" {
			b.addImplicitDomain(config, "gitlab-domains", domain.String())
		}

		var username, password any = token, "private-token"
		if _, ok := token.(*php.Array); ok {
			var err error
			if username, err = dim(token, "username", 159); err != nil {
				return err
			}
			if password, err = dim(token, "token", 160); err != nil {
				return err
			}
		}
		if err := b.authenticate(domain, username, password, 161); err != nil {
			return err
		}
	}

	for domain, cred := range forgejoToken.All() {
		b.addImplicitDomain(config, "forgejo-domains", domain.String())

		username, token, err := credentials(cred, "username", "token", 170)
		if err != nil {
			return err
		}
		if err := b.authenticate(domain, username, token, 170); err != nil {
			return err
		}
	}

	// reload http basic credentials from config if available
	for domain, cred := range httpBasic.All() {
		username, password, err := credentials(cred, "username", "password", 175)
		if err != nil {
			return err
		}
		if err := b.authenticate(domain, username, password, 175); err != nil {
			return err
		}
	}

	for domain, token := range bearerToken.All() {
		if err := b.authenticate(domain, token, "bearer", 179); err != nil {
			return err
		}
	}

	// load custom HTTP headers from config
	for domain, headers := range customHeaders.All() {
		if headers != nil {
			// (string) json_encode($headers): "" when encoding fails.
			encoded, _ := php.JSONEncode(headers, 0)
			if err := b.authenticate(domain, encoded, "custom-headers", 185); err != nil {
				return err
			}
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
		if err := b.authenticate(domain, "client-certificate", encoded, 208); err != nil {
			return err
		}
	}

	// setup process timeout: ProcessExecutor::setTimeout(int $timeout)
	timeout := config.Get("process-timeout")
	n, ok := timeout.(int64)
	if !ok {
		return (&php.EngineError{Class: "TypeError", Message: `Composer\Util\ProcessExecutor::setTimeout(): Argument #1 ($timeout) must be of type int, ` + php.ZvalValueName(timeout) + " given"}).
			Called(`Composer\Util\ProcessExecutor::setTimeout`, phperr.At("ProcessExecutor.php", 458), baseIOFile, 212)
	}
	if setTimeout != nil {
		setTimeout(int(n))
	}

	return nil
}

// nullableField is $array[$key] ?? null.
func nullableField(v any, key string) *string {
	f, ok := array(v).Get(key)
	if !ok || f == nil {
		return nil
	}

	return new(php.ToString(f))
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
