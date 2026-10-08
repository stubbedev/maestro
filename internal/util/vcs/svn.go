// Ports src/Composer/Util/Svn.php.

package vcs

import (
	"strings"

	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/capacity"
	"github.com/stubbedev/maestro/internal/util/http"
)

// svnMaxQtyAuthTries is Svn::MAX_QTY_AUTH_TRIES.
const svnMaxQtyAuthTries = 5

// svnVersion is Svn::$version.
var svnVersion versionCache

// Svn ports Composer\Util\Svn.
type Svn struct {
	// credentials are the username and password; hasCredentials is
	// false for null.
	username, password string
	hasCredentials     bool
	// hasAuth is $hasAuth: nil until detected.
	hasAuth          *bool
	io               io.IO
	url              string
	cacheCredentials bool
	process          Process
	qtyAuthTries     int
	config           http.Config
}

// NewSvn is new Svn($url, $io, $config, $process); a nil process is a new
// ProcessExecutor.
func NewSvn(url string, ioi io.IO, config http.Config, process Process) *Svn {
	if process == nil {
		process = http.NewProcessExecutor(ioi)
	}

	return &Svn{url: url, io: ioi, config: config, process: process, cacheCredentials: true}
}

// SvnCleanEnv ports Svn::cleanEnv.
func SvnCleanEnv() {
	// clean up env for OSX, see https://github.com/composer/composer/issues/2146#issuecomment-35478940
	util.ClearEnv("DYLD_LIBRARY_PATH")
}

// Execute ports execute(): runs an SVN remote command, asking for
// credentials and retrying when the server wants them. An empty cwd or
// path is null; verbose writes all output to the user.
func (s *Svn) Execute(command []string, url, cwd, path string, verbose bool) (string, error) {
	// Ensure we are allowed to use this URL by config
	if err := s.config.ProhibitURLByConfig(url, s.io, nil); err != nil {
		return "", err
	}

	return s.executeWithAuthRetry(command, cwd, url, path, verbose)
}

// ExecuteLocal ports executeLocal(): runs an SVN local command (one with
// no remote url) like Execute.
func (s *Svn) ExecuteLocal(command []string, path, cwd string, verbose bool) (string, error) {
	// A local command has no remote url
	return s.executeWithAuthRetry(command, cwd, "", path, verbose)
}

// svnAuthErrors are the messages executeWithAuthRetry treats as an
// authentication failure, lowercased for stripos().
var svnAuthErrors = []string{"could not authenticate to server:", "authorization failed", "svn: e170001:", "svn: e215004:"}

func (s *Svn) executeWithAuthRetry(svnCommand []string, cwd, url, path string, verbose bool) (string, error) {
	// Regenerate the command at each try, to use the newly user-provided credentials
	command, err := s.getCommand(svnCommand, url, path)
	if err != nil {
		return "", err
	}

	var output strings.Builder

	handler := func(typ, buffer string) {
		if typ != util.ProcessOut {
			return
		}

		if strings.HasPrefix(buffer, "Redirecting to URL ") {
			return
		}

		output.WriteString(buffer)

		if verbose {
			s.io.WriteError(buffer, false, io.Normal)
		}
	}

	status, err := s.process.ExecuteFunc(util.Cmd(command...), handler, cwd)
	if err != nil {
		return "", err
	}

	if status == 0 {
		return output.String(), nil
	}

	errorOutput := s.process.GetErrorOutput()
	fullOutput := php.Trim(output.String() + "\n" + errorOutput)

	// the error is not auth-related
	lower := php.Strtolower(fullOutput)
	authRelated := false

	for _, needle := range svnAuthErrors {
		if strings.Contains(lower, needle) {
			authRelated = true

			break
		}
	}

	if !authRelated {
		return "", &util.RuntimeError{Message: fullOutput}
	}

	if !s.HasAuth() {
		if err := s.doAuthDance(); err != nil {
			return "", err
		}
	}

	// try to authenticate if maximum quantity of tries not reached
	s.qtyAuthTries++
	if s.qtyAuthTries-1 < svnMaxQtyAuthTries {
		// restart the process
		return s.executeWithAuthRetry(svnCommand, cwd, url, path, verbose)
	}

	return "", &util.RuntimeError{Message: "wrong credentials provided (" + fullOutput + ")"}
}

// SetCacheCredentials is setCacheCredentials().
func (s *Svn) SetCacheCredentials(cacheCredentials bool) {
	s.cacheCredentials = cacheCredentials
}

// doAuthDance asks for the credentials the repository requested.
func (s *Svn) doAuthDance() error {
	// cannot ask for credentials in non interactive mode
	if !s.io.IsInteractive() {
		return &util.RuntimeError{Message: "can not ask for authentication in non interactive mode"}
	}

	s.io.WriteError("The Subversion server ("+util.SanitizeURL(s.url)+") requested credentials:", true, io.Normal)

	hasAuth := true
	s.hasAuth = &hasAuth

	username, err := s.io.Ask("Username: ", "")
	if err != nil {
		return err
	}

	password, err := s.io.AskAndHideAnswer("Password: ")
	if err != nil {
		return err
	}

	s.username, s.password, s.hasCredentials = php.ToString(username), php.ToString(password), true

	s.cacheCredentials, err = s.io.AskConfirmation("Should Subversion cache these credentials? (yes/no) ", true)

	return err
}

// getCommand builds the svn command run: cmd (usually `svn ls` or
// similar), --non-interactive, the credentials, then url and path (the
// target of a checkout, "" for none).
func (s *Svn) getCommand(cmd []string, url, path string) ([]string, error) {
	credentials, err := s.getCredentialArgs()
	if err != nil {
		return nil, err
	}

	out := make([]string, 0, capacity.Sum(len(cmd), len(credentials), 4))
	out = append(out, cmd...)
	out = append(out, "--non-interactive")
	out = append(out, credentials...)
	out = append(out, "--", url)

	if path != "" {
		out = append(out, path)
	}

	return out, nil
}

// getCredentialArgs returns the credential arguments of the svn command,
// with --no-auth-cache unless credentials are cached.
func (s *Svn) getCredentialArgs() ([]string, error) {
	if !s.HasAuth() {
		return []string{}, nil
	}

	username, err := s.getUsername()
	if err != nil {
		return nil, err
	}

	password, err := s.getPassword()
	if err != nil {
		return nil, err
	}

	return append(s.getAuthCacheArgs(), "--username", username, "--password", password), nil
}

func noSvnAuthError() error {
	return &util.LogicError{Message: "No svn auth detected."}
}

func (s *Svn) getPassword() (string, error) {
	if !s.hasCredentials {
		return "", noSvnAuthError()
	}

	return s.password, nil
}

func (s *Svn) getUsername() (string, error) {
	if !s.hasCredentials {
		return "", noSvnAuthError()
	}

	return s.username, nil
}

// HasAuth ports hasAuth(): whether credentials were found in the
// http-basic config or the URL.
func (s *Svn) HasAuth() bool {
	if s.hasAuth != nil {
		return *s.hasAuth
	}

	if !s.createAuthFromConfig() {
		s.createAuthFromURL()
	}

	return *s.hasAuth
}

func (s *Svn) setHasAuth(v bool) bool {
	s.hasAuth = &v

	return v
}

// getAuthCacheArgs returns the no-auth-cache switch.
func (s *Svn) getAuthCacheArgs() []string {
	if s.cacheCredentials {
		return []string{}
	}

	return []string{"--no-auth-cache"}
}

// createAuthFromConfig creates the credentials from the http-basic
// configuration. A null http-basic counts as absent, as
// $config->has('http-basic') followed by isset() of its entry does.
func (s *Svn) createAuthFromConfig() bool {
	authConfig, ok := s.config.Get("http-basic").(*php.Array)
	if !ok {
		return s.setHasAuth(false)
	}

	host := util.URLHost(s.url)

	if entry, ok := authConfig.Get(host); ok && entry != nil {
		creds, _ := entry.(*php.Array)

		var username, password any
		if creds != nil {
			username, _ = creds.Get("username")
			password, _ = creds.Get("password")
		}

		s.username, s.password, s.hasCredentials = php.ToString(username), php.ToString(password), true

		return s.setHasAuth(true)
	}

	return s.setHasAuth(false)
}

// createAuthFromURL creates the credentials from the URL.
func (s *Svn) createAuthFromURL() bool {
	uri, _ := util.ParseURL(s.url)
	if !php.ToBool(uri.User) {
		return s.setHasAuth(false)
	}

	password := ""
	if php.ToBool(uri.Pass) {
		password = uri.Pass
	}

	s.username, s.password, s.hasCredentials = uri.User, password, true

	return s.setHasAuth(true)
}

// BinaryVersion ports binaryVersion(): the version of the svn binary in
// PATH, false when it cannot be determined. A found version is cached for
// the process; a failure is retried on the next call, as in Composer.
func (s *Svn) BinaryVersion() (string, bool, error) {
	return svnVersion.get(func() (string, bool, error) {
		return versionMatch(s.process, util.Cmd("svn", "--version"), `{(\d+(?:\.\d+)+)}`)
	}, true)
}

// SetSvnVersion sets the cached svn binary version (Svn::$version), for
// tests; known false clears it.
func SetSvnVersion(version string, known bool) { svnVersion.set(version, known) }
