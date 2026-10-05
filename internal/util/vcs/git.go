// Ports src/Composer/Util/Git.php.

package vcs

import (
	"slices"
	"strings"

	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/semver"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/http"
)

// gitVersion is Git::$version.
var gitVersion versionCache

// Filesystem is the part of Composer\Util\Filesystem Git uses.
// *util.Filesystem implements it.
type Filesystem interface {
	RemoveDirectory(directory string) (bool, error)
}

// Git ports Composer\Util\Git.
type Git struct {
	io             io.IO
	config         http.Config
	process        Process
	filesystem     Filesystem
	httpDownloader http.Getter
}

// NewGit is new Git($io, $config, $process, $fs).
func NewGit(ioi io.IO, config http.Config, process Process, fs Filesystem) *Git {
	return &Git{io: ioi, config: config, process: process, filesystem: fs}
}

// CheckForRepoOwnershipError ports Git::checkForRepoOwnershipError. With
// a non-nil io a warning is written there instead of returning the error,
// so pass io only where this is a soft failure.
func CheckForRepoOwnershipError(output, path string, ioi io.IO) error {
	if strings.Contains(output, "fatal: detected dubious ownership") {
		msg := `The repository at "` + path + `" does not have the correct ownership and git refuses to use it:` + "\n\n" + output
		if ioi == nil {
			return &util.RuntimeError{Message: msg}
		}

		ioi.WriteError("<warning>"+msg+"</warning>", true, io.Normal)
	}

	return nil
}

// SetHttpDownloader is setHttpDownloader().
func (g *Git) SetHttpDownloader(httpDownloader http.Getter) {
	g.httpDownloader = httpDownloader
}

// commandFuncs turns commands with %url% and %sanitizedUrl% placeholder
// arguments into the callables of runCommand.
func commandFuncs(commands [][]string) []CommandFunc {
	callables := make([]CommandFunc, len(commands))
	for i, cmd := range commands {
		callables[i] = func(url string) util.Command {
			args := make([]string, len(cmd))
			for j, value := range cmd {
				switch value {
				case "%url%":
					args[j] = url
				case "%sanitizedUrl%":
					args[j] = util.StripCredentials(url)
				default:
					args[j] = value
				}
			}

			return util.Cmd(args...)
		}
	}

	return callables
}

// RunCommands ports runCommands(): runs commands, stopping at the first
// failure, with url or a variation of it (with auth, ssh, ...) in place of
// their %url% arguments; %sanitizedUrl% is url without credentials. The
// output of all commands is stored into output when it is non-nil. An
// empty cwd is null.
func (g *Git) RunCommands(commands [][]string, url, cwd string, initialClone bool, output *string) error {
	return g.runCommand(commandFuncs(commands), url, cwd, initialClone, output, nil)
}

// RunCommandsFunc is runCommands() with a callable $commandOutput: handler
// receives the commands' output.
func (g *Git) RunCommandsFunc(commands [][]string, url, cwd string, initialClone bool, handler func(typ, buffer string)) error {
	return g.runCommand(commandFuncs(commands), url, cwd, initialClone, nil, handler)
}

// RunCommand ports the deprecated runCommand() taking callables.
func (g *Git) RunCommand(commandCallables []CommandFunc, url, cwd string, initialClone bool, output *string) error {
	return g.runCommand(commandCallables, url, cwd, initialClone, output, nil)
}

func (g *Git) runCommand(commandCallables []CommandFunc, url, cwd string, initialClone bool, commandOutput *string, handler func(typ, buffer string)) error {
	var lastCommand util.Command

	// Ensure we are allowed to use this URL by config
	if err := g.config.ProhibitURLByConfig(url, g.io, nil); err != nil {
		return err
	}

	// isset($origCwd)
	origCwd := ""
	if initialClone {
		origCwd = cwd
	}

	runCommands := func(url string) (int, error) {
		collectOutputs := handler == nil

		var outputs strings.Builder

		status := 0

		for counter, callable := range commandCallables {
			lastCommand = callable(url)

			dir := cwd
			if initialClone && counter == 0 {
				dir = ""
			}

			var err error

			if collectOutputs {
				var output string

				status, err = g.process.Execute(lastCommand, &output, dir)
				outputs.WriteString(output)
			} else {
				status, err = g.process.ExecuteFunc(lastCommand, handler, dir)
			}

			if err != nil {
				return status, err
			}

			if status != 0 {
				break
			}
		}

		if collectOutputs && commandOutput != nil {
			*commandOutput = outputs.String()
		}

		return status, nil
	}

	// succeeded is 0 === $runCommands($url).
	succeeded := func(url string) (bool, error) {
		status, err := runCommands(url)

		return err == nil && status == 0, err
	}

	if ok, err := php.PregIsMatch(`{^ssh://[^@]+@[^:]+:[^0-9]+}`, url); err != nil {
		return err
	} else if ok {
		return &util.InvalidArgumentError{Message: "The source URL " + util.SanitizeURL(url) + ` is invalid, ssh URLs should have a port number after ":".` + "\n" + "Use ssh://git@example.com:22/path or just git@example.com:path if you do not want to provide a password or custom port."}
	}

	if !initialClone {
		// capture username/password from URL if there is one and we have no auth configured yet
		var output string
		if _, err := g.process.Execute(util.Cmd("git", "remote", "-v"), &output, cwd); err != nil {
			return err
		}

		match, err := php.PregMatchStrictGroups(`{^(?:composer|origin)\s+https?://(.+):(.+)@([^/]+)}im`, output)
		if err != nil {
			return err
		}

		if match != nil && !g.io.HasAuthentication(match.Get(3)) {
			password := php.Rawurldecode(match.Get(2))
			g.io.SetAuthentication(match.Get(3), php.Rawurldecode(match.Get(1)), &password)
		}
	}

	protocols := http.ConfigList(g.config, "github-protocols")
	gitHubDomains := GetGitHubDomainsRegex(g.config)

	// public github, autoswitch protocols
	match, err := php.PregMatchStrictGroups(`{^(?:https?|git)://`+gitHubDomains+`/(.*)}`, url)
	if err != nil {
		return err
	}

	if match != nil {
		var messages []string

		for _, protocol := range protocols {
			var protoURL string
			if protocol == "ssh" {
				protoURL = "git@" + match.Get(1) + ":" + match.Get(2)
			} else {
				protoURL = protocol + "://" + match.Get(1) + "/" + match.Get(2)
			}

			if ok, err := succeeded(protoURL); ok || err != nil {
				return err
			}

			indented, _, err := php.PregReplace(`#^#m`, "  ", g.process.GetErrorOutput(), -1)
			if err != nil {
				return err
			}

			messages = append(messages, "- "+protoURL+"\n"+indented)

			if initialClone && origCwd != "" {
				if _, err := g.filesystem.RemoveDirectory(origCwd); err != nil {
					return err
				}
			}
		}

		// failed to checkout, first check git accessibility
		if !g.io.HasAuthentication(match.Get(1)) && !g.io.IsInteractive() {
			return g.throwException("Failed to clone "+url+" via "+strings.Join(protocols, ", ")+" protocols, aborting."+"\n\n"+strings.Join(messages, "\n"), url)
		}
	}

	// if we have a private github url and the ssh protocol is disabled then we skip it and directly fallback to https
	bypassSSHForGitHub, err := php.PregIsMatch(`{^git@`+gitHubDomains+`:(.+?)\.git$}i`, url)
	if err != nil {
		return err
	}

	bypassSSHForGitHub = bypassSSHForGitHub && !slices.Contains(protocols, "ssh")

	if !bypassSSHForGitHub {
		if ok, err := succeeded(url); ok || err != nil {
			return err
		}
	}

	return g.retryWithAuth(url, origCwd, initialClone, succeeded, &lastCommand)
}

// retryWithAuth is the part of runCommand() after the URL failed as is:
// the GitHub, Bitbucket, GitLab and generic credential fallbacks, then the
// "Failed to execute" error.
func (g *Git) retryWithAuth(url, origCwd string, initialClone bool, succeeded func(string) (bool, error), lastCommand *util.Command) error {
	errorMsg := g.process.GetErrorOutput()

	var credentials []string

	gitHubDomains := GetGitHubDomainsRegex(g.config)
	gitLabDomains := GetGitLabDomainsRegex(g.config)

	// private github repository without ssh key access, try https with auth
	if match, err := firstMatch(url, `{^git@`+gitHubDomains+`:(.+?)\.git$}i`, `{^https?://`+gitHubDomains+`/(.*?)(?:\.git)?$}i`); err != nil {
		return err
	} else if match != nil {
		host := match.Get(1)

		if !g.io.HasAuthentication(host) {
			gitHubUtil := http.NewGitHub(g.io, g.config, g.process, nil)
			message := "Cloning failed using an ssh key for authentication, enter your GitHub credentials to access private repos"

			if !gitHubUtil.AuthorizeOAuth(host) && g.io.IsInteractive() {
				if _, err := gitHubUtil.AuthorizeOAuthInteractively(host, message); err != nil {
					return err
				}
			}
		}

		if g.io.HasAuthentication(host) {
			auth := g.io.Authentication(host)
			user, pass := php.Rawurlencode(strOf(auth.Username)), php.Rawurlencode(strOf(auth.Password))
			authURL := "https://" + user + ":" + pass + "@" + host + "/" + match.Get(2) + ".git"

			if ok, err := succeeded(authURL); ok || err != nil {
				return err
			}

			credentials = []string{user, pass}
			errorMsg = g.process.GetErrorOutput()
		}
	} else if match, err := firstMatch(url, `{^(https?)://(bitbucket\.org)/(.*?)(?:\.git)?$}i`, `{^(git)@(bitbucket\.org):(.+?\.git)$}i`); err != nil {
		return err
	} else if match != nil { // bitbucket either through oauth or app password, with fallback to ssh.
		var (
			done bool
			err  error
		)

		done, credentials, err = g.retryBitbucket(match.Get(2), match.Get(3), succeeded)
		if done || err != nil {
			return err
		}

		errorMsg = g.process.GetErrorOutput()
	} else if match, err := firstMatch(url, `{^(git)@`+gitLabDomains+`:(.+?\.git)$}i`, `{^(https?)://`+gitLabDomains+`/(.*)}i`); err != nil {
		return err
	} else if match != nil {
		scheme, host := match.Get(1), match.Get(2)
		if scheme == "git" {
			scheme = "https"
		}

		if !g.io.HasAuthentication(host) {
			gitLabUtil := http.NewGitLab(g.io, g.config, g.process, nil)
			message := "Cloning failed, enter your GitLab credentials to access private repos"

			if !gitLabUtil.AuthorizeOAuth(host) && g.io.IsInteractive() {
				if _, err := gitLabUtil.AuthorizeOAuthInteractively(scheme, host, message); err != nil {
					return err
				}
			}
		}

		if g.io.HasAuthentication(host) {
			auth := g.io.Authentication(host)
			user, pass := php.Rawurlencode(strOf(auth.Username)), php.Rawurlencode(strOf(auth.Password))

			var authURL string

			switch strOf(auth.Password) {
			case "private-token", "oauth2", "gitlab-ci-token":
				authURL = scheme + "://" + pass + ":" + user + "@" + host + "/" + match.Get(3) // swap username and password
			default:
				authURL = scheme + "://" + user + ":" + pass + "@" + host + "/" + match.Get(3)
			}

			if ok, err := succeeded(authURL); ok || err != nil {
				return err
			}

			credentials = []string{user, pass}
			errorMsg = g.process.GetErrorOutput()
		}
	} else if match, err := g.getAuthenticationFailure(url); err != nil {
		return err
	} else if match != nil { // private non-github/gitlab/bitbucket repo that failed to authenticate
		var (
			done bool
			err  error
		)

		done, credentials, err = g.retryPrompted(match, errorMsg, succeeded)
		if done || err != nil {
			return err
		}

		if credentials != nil {
			errorMsg = g.process.GetErrorOutput()
		}
	}

	if initialClone && origCwd != "" {
		if _, err := g.filesystem.RemoveDirectory(origCwd); err != nil {
			return err
		}
	}

	last := commandString(*lastCommand)
	if len(credentials) > 0 {
		last = maskCredentials(last, credentials)
		errorMsg = maskCredentials(errorMsg, credentials)
	}

	return g.throwException("Failed to execute "+last+"\n\n"+errorMsg, url)
}

// retryBitbucket is the Bitbucket branch of runCommand(): OAuth or app
// password, falling back to ssh. done reports success.
func (g *Git) retryBitbucket(domain, repoWithGitPart string, succeeded func(string) (bool, error)) (done bool, credentials []string, err error) {
	bitbucketUtil := http.NewBitbucket(g.io, g.config, g.process, g.httpDownloader, 0)

	if !strings.HasSuffix(repoWithGitPart, ".git") {
		repoWithGitPart += ".git"
	}

	if !g.io.HasAuthentication(domain) {
		message := "Enter your Bitbucket credentials to access private repos"

		if !bitbucketUtil.AuthorizeOAuth(domain) && g.io.IsInteractive() {
			if _, err := bitbucketUtil.AuthorizeOAuthInteractively(domain, message); err != nil {
				return false, nil, err
			}

			accessToken := bitbucketUtil.Token()
			g.io.SetAuthentication(domain, "x-token-auth", &accessToken)
		}
	}

	// First we try to authenticate with whatever we have stored.
	// This will be successful if there is for example an app
	// password in there.
	if g.io.HasAuthentication(domain) {
		auth := g.io.Authentication(domain)
		username, password := strOf(auth.Username), strOf(auth.Password)

		// Bitbucket API tokens use the email address as the username for HTTP API calls and
		// either the Bitbucket username or 'x-bitbucket-api-token-auth' as the username for git operations.
		if strings.HasPrefix(password, "ATAT") {
			username = "x-bitbucket-api-token-auth"
		}

		authURL := "https://" + php.Rawurlencode(username) + ":" + php.Rawurlencode(password) + "@" + domain + "/" + repoWithGitPart

		if ok, err := succeeded(authURL); ok || err != nil {
			// Well if that succeeded on our first try, let's just
			// take the win.
			return ok, nil, err
		}

		// We already have an access_token from a previous request.
		if username != "x-token-auth" {
			accessToken, err := bitbucketUtil.RequestToken(domain, username, password)
			if err != nil {
				return false, nil, err
			}

			if php.ToBool(accessToken) {
				g.io.SetAuthentication(domain, "x-token-auth", &accessToken)
			}
		}
	}

	if g.io.HasAuthentication(domain) {
		auth := g.io.Authentication(domain)
		user, pass := php.Rawurlencode(strOf(auth.Username)), php.Rawurlencode(strOf(auth.Password))
		authURL := "https://" + user + ":" + pass + "@" + domain + "/" + repoWithGitPart

		if ok, err := succeeded(authURL); ok || err != nil {
			return ok, nil, err
		}

		credentials = []string{user, pass}
	}

	// Falling back to ssh
	sshURL := "git@bitbucket.org:" + repoWithGitPart
	g.io.WriteError("    No bitbucket authentication configured. Falling back to ssh.", true, io.Normal)

	ok, err := succeeded(sshURL)

	return ok, credentials, err
}

// retryPrompted is the generic branch of runCommand(): stored or prompted
// credentials for a host that refused the clone. credentials is nil when
// none were tried.
func (g *Git) retryPrompted(match *php.Match, errorMsg string, succeeded func(string) (bool, error)) (done bool, credentials []string, err error) {
	host := match.Get(2)

	authParts, hasAuthParts := "", false
	if before, after, found := strings.Cut(host, "@"); found {
		authParts, host, hasAuthParts = before, after, true
	}

	var (
		storeAuth          any = false
		username, password any
		haveAuth           bool
	)

	if g.io.HasAuthentication(host) {
		auth := g.io.Authentication(host)
		username, password, haveAuth = anyOf(auth.Username), anyOf(auth.Password), true
	} else if g.io.IsInteractive() {
		var defaultUsername any
		if hasAuthParts && authParts != "" {
			defaultUsername, _, _ = strings.Cut(authParts, ":")
		}

		g.io.WriteError("    Authentication required (<info>"+host+"</info>):", true, io.Normal)
		g.io.WriteError("<warning>"+php.Trim(errorMsg)+"</warning>", true, io.Verbose)

		if username, err = g.io.Ask("      Username: ", defaultUsername); err != nil {
			return false, nil, err
		}

		if password, err = g.io.AskAndHideAnswer("      Password: "); err != nil {
			return false, nil, err
		}

		haveAuth = true
		storeAuth = g.config.Get("store-auths")
	}

	if !haveAuth {
		return false, nil, nil
	}

	user, pass := php.Rawurlencode(php.ToString(username)), php.Rawurlencode(php.ToString(password))
	authURL := match.Get(1) + user + ":" + pass + "@" + host + match.Get(3)

	if ok, err := succeeded(authURL); err != nil {
		return false, nil, err
	} else if ok {
		var passwordPtr *string
		if password != nil {
			p := php.ToString(password)
			passwordPtr = &p
		}

		g.io.SetAuthentication(host, php.ToString(username), passwordPtr)

		authHelper := http.NewAuthHelper(g.io, g.config)

		return true, nil, authHelper.StoreAuth(host, http.StoreAuthOf(storeAuth))
	}

	return false, []string{user, pass}, nil
}

// firstMatch is Preg::isMatchStrictGroups($a, $url) ||
// Preg::isMatchStrictGroups($b, $url): the first match, nil for none.
func firstMatch(url string, patterns ...string) (*php.Match, error) {
	for _, pattern := range patterns {
		m, err := php.PregMatchStrictGroups(pattern, url)
		if err != nil || m != nil {
			return m, err
		}
	}

	return nil, nil
}

// SyncMirror ports syncMirror(): updates the bare mirror of url in dir,
// or clones it afresh when dir is not one. False means the update failed
// (or the network is disabled).
func (g *Git) SyncMirror(url, dir string) (bool, error) {
	if v, truthy := envTruthy("COMPOSER_DISABLE_NETWORK"); truthy && v != "prime" {
		g.io.WriteError("<warning>Aborting git mirror sync of "+util.SanitizeURL(url)+" as network is disabled</warning>", true, io.Normal)

		return false, nil
	}

	// update the repo if it is a valid git repository
	if isMirror, err := g.isBareRepository(dir); err != nil {
		return false, err
	} else if isMirror {
		err := g.RunCommands([][]string{
			{"git", "remote", "set-url", "origin", "--", "%url%"},
			{"git", "remote", "update", "--prune", "origin"},
			{"git", "gc", "--auto"},
		}, url, dir, false, nil)

		// finally
		if ferr := g.RunCommands([][]string{{"git", "remote", "set-url", "origin", "--", "%sanitizedUrl%"}}, url, dir, false, nil); ferr != nil {
			err = ferr
		}

		if err != nil {
			g.io.WriteError("<error>Sync mirror failed: "+err.Error()+"</error>", true, io.Debug)

			return false, nil
		}

		return true, nil
	}

	if err := CheckForRepoOwnershipError(g.process.GetErrorOutput(), dir, nil); err != nil {
		return false, err
	}

	// clean up directory and do a fresh clone into it
	if _, err := g.filesystem.RemoveDirectory(dir); err != nil {
		return false, err
	}

	if err := g.RunCommands([][]string{{"git", "clone", "--mirror", "--", "%url%", dir}}, url, dir, true, nil); err != nil {
		return false, err
	}

	if err := g.RunCommands([][]string{{"git", "remote", "set-url", "origin", "--", "%sanitizedUrl%"}}, url, dir, false, nil); err != nil {
		return false, err
	}

	return true, nil
}

// isBareRepository is is_dir($dir) && 0 === git rev-parse --git-dir in
// $dir && trim($output) === '.'.
func (g *Git) isBareRepository(dir string) (bool, error) {
	if !isDir(dir) {
		return false, nil
	}

	var output string

	code, err := g.process.Execute(util.Cmd("git", "rev-parse", "--git-dir"), &output, dir)

	return err == nil && code == 0 && php.Trim(output) == ".", err
}

var sha1Ref = php.MustCompile(`{^[a-f0-9]{40}$}`)

// FetchRefOrSyncMirror ports fetchRefOrSyncMirror(): whether ref is in
// the mirror at dir, syncing it first if it is not. An empty prettyVersion
// is null.
func (g *Git) FetchRefOrSyncMirror(url, dir, ref, prettyVersion string) (bool, error) {
	inMirror, err := g.checkRefIsInMirror(dir, ref)
	if err != nil {
		return false, err
	}

	if inMirror {
		// Anchored and fixed-length: Preg::isMatch cannot throw.
		if isSha, _ := sha1Ref.IsMatch(ref); isSha && prettyVersion != "" {
			branch, _, err := php.PregReplace(`{(?:^dev-|(?:\.x)?-dev$)}i`, "", prettyVersion, -1)
			if err != nil {
				return false, err
			}

			var branches, tags, output string

			hasBranches, hasTags := false, false

			if code, err := g.process.Execute(util.Cmd("git", "branch"), &output, dir); err != nil {
				return false, err
			} else if code == 0 {
				branches, hasBranches = output, true
			}

			if code, err := g.process.Execute(util.Cmd("git", "tag"), &output, dir); err != nil {
				return false, err
			} else if code == 0 {
				tags, hasTags = output, true
			}

			// if the pretty version cannot be found as a branch (nor branch with 'v' in front of the branch as it may have been stripped when generating pretty name),
			// nor as a tag, then we sync the mirror as otherwise it will likely fail during install.
			// this can occur if a git tag gets created *after* the reference is already put into the cache, as the ref check above will then not sync the new tags
			// see https://github.com/composer/composer/discussions/11002
			quoted := php.PregQuote(branch, "")
			notFound := hasBranches
			if notFound {
				inBranches, err := php.PregIsMatch(`{^[\s*]*v?`+quoted+`$}m`, branches)
				if err != nil {
					return false, err
				}
				notFound = !inBranches && hasTags
			}
			if notFound {
				inTags, err := php.PregIsMatch(`{^[\s*]*`+quoted+`$}m`, tags)
				if err != nil {
					return false, err
				}
				notFound = !inTags
			}
			if notFound {
				if _, err := g.SyncMirror(url, dir); err != nil {
					return false, err
				}
			}
		}

		return true, nil
	}

	if synced, err := g.SyncMirror(url, dir); err != nil || !synced {
		return false, err
	}

	return g.checkRefIsInMirror(dir, ref)
}

func (g *Git) checkRefIsInMirror(dir, ref string) (bool, error) {
	if isMirror, err := g.isBareRepository(dir); err != nil {
		return false, err
	} else if isMirror {
		var ignoredOutput string

		exitCode, err := g.process.Execute(util.Cmd("git", "rev-parse", "--quiet", "--verify", ref+"^{commit}"), &ignoredOutput, dir)
		if err != nil {
			return false, err
		}

		if exitCode == 0 {
			return true, nil
		}
	}

	return false, CheckForRepoOwnershipError(g.process.GetErrorOutput(), dir, nil)
}

// authFailures are the git messages getAuthenticationFailure() looks for.
var authFailures = []string{
	"fatal: Authentication failed",
	"remote error: Invalid username or password.",
	"error: 401 Unauthorized",
	"fatal: unable to access",
	"fatal: could not read Username",
}

func (g *Git) getAuthenticationFailure(url string) (*php.Match, error) {
	match, err := php.PregMatchStrictGroups(`{^(https?://)([^/]+)(.*)$}i`, url)
	if err != nil || match == nil {
		return nil, err
	}

	errorOutput := g.process.GetErrorOutput()
	for _, authFailure := range authFailures {
		if strings.Contains(errorOutput, authFailure) {
			return match, nil
		}
	}

	return nil, nil
}

// GetMirrorDefaultBranch ports getMirrorDefaultBranch(): the HEAD branch
// of the remote of the mirror at dir; false for null.
func (g *Git) GetMirrorDefaultBranch(url, dir string, isLocalPathRepository bool) (string, bool) {
	if _, truthy := envTruthy("COMPOSER_DISABLE_NETWORK"); truthy {
		return "", false
	}

	var output string

	var err error

	if isLocalPathRepository {
		_, err = g.process.Execute(util.Cmd("git", "remote", "show", "origin"), &output, dir)
	} else {
		err = g.RunCommands([][]string{
			{"git", "remote", "set-url", "origin", "--", "%url%"},
			{"git", "remote", "show", "origin"},
		}, url, dir, false, &output)

		// finally
		if ferr := g.RunCommands([][]string{{"git", "remote", "set-url", "origin", "--", "%sanitizedUrl%"}}, url, dir, false, nil); ferr != nil {
			err = ferr
		}
	}

	if err != nil {
		g.io.WriteError("<error>Failed to fetch root identifier from remote: "+err.Error()+"</error>", true, io.Debug)

		return "", false
	}

	for _, line := range util.SplitLines(output) {
		m, err := headBranch.Match(line)
		if err != nil {
			// the PcreException is caught like the command failures above
			g.io.WriteError("<error>Failed to fetch root identifier from remote: "+err.Error()+"</error>", true, io.Debug)

			return "", false
		}
		if m != nil {
			return m.Get(1), true
		}
	}

	return "", false
}

var headBranch = php.MustCompile(`{^\s*HEAD branch:\s(.+)\s*$}m`)

// GetNoShowSignatureFlag ports Git::getNoShowSignatureFlag.
func GetNoShowSignatureFlag(process Process) (string, error) {
	ok, err := gitVersionAtLeast(process, "2.10.0-rc0")
	if err != nil || !ok {
		return "", err
	}

	return " --no-show-signature", nil
}

// GetNoShowSignatureFlags ports Git::getNoShowSignatureFlags.
func GetNoShowSignatureFlags(process Process) ([]string, error) {
	flags, err := GetNoShowSignatureFlag(process)
	if err != nil || flags == "" {
		return []string{}, err
	}

	return strings.Split(flags[1:], " "), nil
}

// SupportsNoCommitHeaderFlag ports Git::supportsNoCommitHeaderFlag: git
// 2.33+.
func SupportsNoCommitHeaderFlag(process Process) (bool, error) {
	return gitVersionAtLeast(process, "2.33.0-rc0")
}

func gitVersionAtLeast(process Process, min string) (bool, error) {
	version, ok, err := GetVersion(process)
	if err != nil || !ok {
		return false, err
	}

	return semver.VersionCompareOp(version, min, ">=")
}

// BuildRevListCommand ports Git::buildRevListCommand: git rev-list with
// --no-commit-header when supported (git 2.33+), then arguments.
func BuildRevListCommand(process Process, arguments []string) ([]string, error) {
	command := []string{"git", "rev-list"}

	supported, err := SupportsNoCommitHeaderFlag(process)
	if err != nil {
		return nil, err
	}

	if supported {
		command = append(command, "--no-commit-header")
	}

	return append(command, arguments...), nil
}

// ParseRevListOutput ports Git::parseRevListOutput: removes the
// "commit <hash>" header lines git < 2.33 outputs.
func ParseRevListOutput(output string, process Process) (string, error) {
	// If git supports --no-commit-header, output is already clean
	if supported, err := SupportsNoCommitHeaderFlag(process); err != nil || supported {
		return output, err
	}

	// Filter out "commit <hash>" lines for older git versions
	output, _, err := php.PregReplace(`{^commit [a-f0-9]{40}\n?}m`, "", output, -1)

	return output, err
}

// CleanEnv ports Git::cleanEnv: sets up the environment for running git
// non-interactively. A nil process is a new ProcessExecutor.
func CleanEnv(process Process) error {
	if process == nil {
		process = util.NewProcessExecutor(nil)
	}

	ok, err := gitVersionAtLeast(process, "2.3.0")
	if err != nil {
		return err
	}

	if ok {
		// added in git 2.3.0, prevents prompting the user for username/password
		if v, _ := util.GetEnv("GIT_TERMINAL_PROMPT"); v != "0" {
			util.PutEnv("GIT_TERMINAL_PROMPT", "0")
		}
	} else {
		// added in git 1.7.1, prevents prompting the user for username/password
		if v, _ := util.GetEnv("GIT_ASKPASS"); v != "echo" {
			util.PutEnv("GIT_ASKPASS", "echo")
		}
	}

	// clean up rogue git env vars in case this is running in a git hook
	if _, truthy := envTruthy("GIT_DIR"); truthy {
		util.ClearEnv("GIT_DIR")
	}

	if _, truthy := envTruthy("GIT_WORK_TREE"); truthy {
		util.ClearEnv("GIT_WORK_TREE")
	}

	// Run processes with predictable LANGUAGE
	if v, _ := util.GetEnv("LANGUAGE"); v != "C" {
		util.PutEnv("LANGUAGE", "C")
	}

	// clean up env for OSX, see https://github.com/composer/composer/issues/2146#issuecomment-35478940
	util.ClearEnv("DYLD_LIBRARY_PATH")

	return nil
}

// GetGitHubDomainsRegex ports Git::getGitHubDomainsRegex: a group
// alternating the github-domains.
func GetGitHubDomainsRegex(config http.Config) string {
	return domainsRegex(config, "github-domains")
}

// GetGitLabDomainsRegex ports Git::getGitLabDomainsRegex: a group
// alternating the gitlab-domains.
func GetGitLabDomainsRegex(config http.Config) string {
	return domainsRegex(config, "gitlab-domains")
}

func domainsRegex(config http.Config, key string) string {
	domains := http.ConfigList(config, key)

	var b strings.Builder

	b.WriteByte('(')

	for i, domain := range domains {
		if i > 0 {
			b.WriteByte('|')
		}

		b.WriteString(php.PregQuote(domain, ""))
	}

	b.WriteByte(')')

	return b.String()
}

// throwException returns the RuntimeException for message, with URLs
// sanitized, or the one saying git is missing.
func (g *Git) throwException(message, url string) error {
	var ignoredOutput string

	code, err := g.process.Execute(util.Cmd("git", "--version"), &ignoredOutput, "")
	if err != nil {
		return err
	}

	if code != 0 {
		return &util.RuntimeError{Message: util.SanitizeURL("Failed to clone " + url + ", git was not found, check that it is installed and in your PATH env." + "\n\n" + g.process.GetErrorOutput())}
	}

	return &util.RuntimeError{Message: util.SanitizeURL(message)}
}

// GetVersion ports Git::getVersion: the git version, false when git is
// not found. It runs `git --version` once per process.
func GetVersion(process Process) (string, bool, error) {
	return gitVersion.get(func() (string, bool, error) {
		return versionMatch(process, util.Cmd("git", "--version"), `/^git version (\d+(?:\.\d+)+)/m`)
	}, false)
}

// SetVersion sets the cached git version (Git::$version), as Composer's
// tests do by reflection; "" with known false resets it.
func SetVersion(version string, known bool) { gitVersion.set(version, known) }

// maskCredentials masks credentials in msg, keeping well-known
// non-secret markers.
func maskCredentials(msg string, credentials []string) string {
	for _, credential := range credentials {
		var masked string

		switch {
		case slices.Contains(util.NonSecretCredentials, credential):
			masked = credential
		case len(credential) > 6:
			masked = credential[:3] + "..." + credential[len(credential)-3:]
		case len(credential) > 3:
			masked = credential[:3] + "..."
		default:
			masked = "XXX"
		}

		// str_replace skips empty search strings
		if credential != "" {
			msg = strings.ReplaceAll(msg, credential, masked)
		}
	}

	return msg
}
