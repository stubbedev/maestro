// Ports src/Composer/Repository/Vcs/GitDriver.php.

package vcs

import (
	"os"
	"slices"
	"time"

	"github.com/stubbedev/maestro/internal/cache"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/util"
	uvcs "github.com/stubbedev/maestro/internal/util/vcs"
)

// GitDriver ports Composer\Repository\Vcs\GitDriver: a git repository
// read from a local clone, or from a bare mirror in cache-vcs-dir.
type GitDriver struct {
	vcsDriver
	// tags and branches are nil until loaded.
	tags, branches *php.Array
	// rootIdentifier is "" until computed.
	rootIdentifier string
	repoDir        string
}

func newGitDriver(repoConfig *php.Array, deps Deps) Driver { return NewGitDriver(repoConfig, deps) }

// NewGitDriver is new GitDriver($repoConfig, $io, $config,
// $httpDownloader, $process).
func NewGitDriver(repoConfig *php.Array, deps Deps) *GitDriver {
	d := &GitDriver{}
	d.init(d, repoConfig, deps)

	return d
}

// Class returns the PHP class name.
func (d *GitDriver) Class() string { return gitDriverType.Class }

var (
	gitDirSuffix      = php.MustCompile(`{[\\/]\.git/?$}`)
	cacheNameChars    = php.MustCompile(`{[^a-z0-9.]}i`)
	sshURLWithoutPort = php.MustCompile(`{^ssh://[^@]+@[^:]+:[^0-9]+}`)
)

// Initialize ports GitDriver::initialize.
func (d *GitDriver) Initialize() error {
	var cacheURL string

	if util.IsLocalPath(d.url) {
		d.url = replaceInfallible(gitDirSuffix, "", d.url)
		if !php.IsDir(d.url) {
			return &util.RuntimeError{Message: "Failed to read package information from " + util.SanitizeURL(d.url) + " as the path does not exist"}
		}

		d.repoDir = d.url
		cacheURL, _ = php.Realpath(d.url)
	} else {
		cacheVcsDir := php.ToString(d.config.Get("cache-vcs-dir"))
		if !cache.IsUsable(cacheVcsDir) {
			return &util.RuntimeError{Message: "GitDriver requires a usable cache directory, and it looks like you set it to be disabled"}
		}

		safeURL, err := util.SanitizeURLChecked(d.url)
		if err != nil {
			return err
		}
		d.repoDir = cacheVcsDir + "/" + replaceInfallible(cacheNameChars, "-", safeURL) + "/"

		if err := uvcs.CleanEnv(d.process); err != nil {
			return err
		}

		parent := php.Dirname(d.repoDir)
		if err := util.EnsureDirectoryExists(parent); err != nil {
			return err
		}

		if !util.IsWritable(parent) {
			return &util.RuntimeError{Message: "Can not clone " + util.SanitizeURL(d.url) + ` to access package information. The "` + parent + `" directory is not writable by the current user.`}
		}

		if ok, err := matches(sshURLWithoutPort, d.url); err != nil {
			return err
		} else if ok {
			return &util.InvalidArgumentError{Message: "The source URL " + util.SanitizeURL(d.url) + ` is invalid, ssh URLs should have a port number after ":".` + "\n" + "Use ssh://git@example.com:22/path or just git@example.com:path if you do not want to provide a password or custom port."}
		}

		gitUtil := uvcs.NewGit(d.io, d.config, d.process, util.NewFilesystem(nil))

		synced, err := gitUtil.SyncMirror(d.url, d.repoDir)
		if err != nil {
			return err
		}

		if !synced {
			if !php.IsDir(d.repoDir) {
				return &util.RuntimeError{Message: "Failed to clone " + util.SanitizeURL(d.url) + " to read package information from it"}
			}

			d.io.WriteError("<error>Failed to update "+util.SanitizeURL(d.url)+", package information from this repository may be outdated</error>", true, io.Normal)
		}

		cacheURL = d.url
	}

	if _, err := d.Tags(); err != nil {
		return err
	}

	if _, err := d.Branches(); err != nil {
		return err
	}

	safeCacheURL, err := util.SanitizeURLChecked(cacheURL)
	if err != nil {
		return err
	}

	return d.newCache(php.ToString(d.config.Get("cache-repo-dir")) + "/" + replaceInfallible(cacheNameChars, "-", safeCacheURL))
}

var currentBranch = php.MustCompile(`{^\* +(\S+)}`)

// RootIdentifier ports GitDriver::getRootIdentifier.
func (d *GitDriver) RootIdentifier() (string, error) {
	if d.rootIdentifier != "" {
		return d.rootIdentifier, nil
	}

	d.rootIdentifier = "master"

	if !util.IsLocalPath(d.url) {
		gitUtil := uvcs.NewGit(d.io, d.config, d.process, util.NewFilesystem(nil))
		if defaultBranch, ok := gitUtil.GetMirrorDefaultBranch(d.url, d.repoDir, false); ok {
			d.rootIdentifier = defaultBranch

			return defaultBranch, nil
		}
	}

	// select currently checked out branch as default branch
	var output string
	if _, err := d.process.Execute(util.Cmd("git", "branch", "--no-color"), &output, d.repoDir); err != nil {
		return "", err
	}

	branches := util.SplitLines(output)
	if !slices.Contains(branches, "* master") {
		for _, branch := range branches {
			if !php.ToBool(branch) {
				continue
			}

			m, err := match(currentBranch, branch)
			if err != nil {
				return "", err
			}
			if m != nil {
				d.rootIdentifier = m.Get(1)

				break
			}
		}
	}

	return d.rootIdentifier, nil
}

// URL ports GitDriver::getUrl.
func (d *GitDriver) URL() string { return d.url }

// Source ports GitDriver::getSource.
func (d *GitDriver) Source(identifier string) *php.Array {
	return php.ArrayOf("type", "git", "url", d.URL(), "reference", identifier)
}

// Dist ports GitDriver::getDist.
func (d *GitDriver) Dist(string) *php.Array { return nil }

// invalidIdentifier is the RuntimeException the CLI drivers throw for an
// identifier that would read as an option.
func invalidIdentifier(vcs, identifier string) error {
	if identifier != "" && identifier[0] == '-' {
		return &util.RuntimeError{Message: "Invalid " + vcs + " identifier detected. Identifier must not start with a -, given: " + identifier}
	}

	return nil
}

// FileContent ports GitDriver::getFileContent.
func (d *GitDriver) FileContent(file, identifier string) (string, bool, error) {
	if err := invalidIdentifier("git", identifier); err != nil {
		return "", false, err
	}

	var content string
	if _, err := d.process.Execute(util.Cmd("git", "show", identifier+":"+file), &content, d.repoDir); err != nil {
		return "", false, err
	}

	if php.Trim(content) == "" {
		return "", false, nil
	}

	return content, true, nil
}

// ChangeDate ports GitDriver::getChangeDate.
func (d *GitDriver) ChangeDate(identifier string) (time.Time, bool, error) {
	if err := invalidIdentifier("git", identifier); err != nil {
		return time.Time{}, false, err
	}

	command, err := uvcs.BuildRevListCommand(d.process, []string{"-n1", "--format=%at", identifier})
	if err != nil {
		return time.Time{}, false, err
	}

	var output string
	if _, err := d.process.Execute(util.Cmd(command...), &output, d.repoDir); err != nil {
		return time.Time{}, false, err
	}

	output, err = uvcs.ParseRevListOutput(output, d.process)
	if err != nil {
		return time.Time{}, false, err
	}

	date, err := parseDate("@" + php.Trim(output))

	return date, err == nil, err
}

var tagRef = php.MustCompile(`{^([a-f0-9]{40}) refs/tags/(\S+?)(\^\{\})?$}`)

// Tags ports GitDriver::getTags.
func (d *GitDriver) Tags() (*php.Array, error) {
	if d.tags != nil {
		return d.tags, nil
	}

	var output string
	if _, err := d.process.Execute(util.Cmd("git", "show-ref", "--tags", "--dereference"), &output, d.repoDir); err != nil {
		return nil, err
	}

	tags := php.NewArray()

	for _, tag := range util.SplitLines(output) {
		if tag == "" {
			continue
		}

		m, err := match(tagRef, tag)
		if err != nil {
			return nil, err
		}
		if m != nil {
			tags.Set(m.Get(2), m.Get(1))
		}
	}

	d.tags = tags

	return tags, nil
}

var (
	remoteHead = php.MustCompile(`{^ *[^/]+/HEAD }`)
	branchLine = php.MustCompile(`{^(?:\* )? *(\S+) *([a-f0-9]+)(?: .*)?$}`)
)

// Branches ports GitDriver::getBranches.
func (d *GitDriver) Branches() (*php.Array, error) {
	if d.branches != nil {
		return d.branches, nil
	}

	var output string
	if _, err := d.process.Execute(util.Cmd("git", "branch", "--no-color", "--no-abbrev", "-v"), &output, d.repoDir); err != nil {
		return nil, err
	}

	branches := php.NewArray()

	for _, branch := range util.SplitLines(output) {
		if branch == "" {
			continue
		}
		if isHead, err := matches(remoteHead, branch); err != nil {
			return nil, err
		} else if isHead {
			continue
		}

		m, err := match(branchLine, branch)
		if err != nil {
			return nil, err
		}
		if m != nil && m.Get(1)[0] != '-' {
			branches.Set(m.Get(1), m.Get(2))
		}
	}

	d.branches = branches

	return branches, nil
}

var gitURL = php.MustCompile(`#(^git://|\.git/?$|git(?:olite)?@|//git\.|//github.com/)#i`)

// gitSupports ports GitDriver::supports.
func gitSupports(deps Deps, url string, deep bool) (bool, error) {
	if ok, err := matches(gitURL, url); err != nil || ok {
		return ok, err
	}

	// local filesystem
	if util.IsLocalPath(url) {
		url = util.GetPlatformPath(url)
		if !php.IsDir(url) {
			return false, nil
		}

		// check whether there is a git repo in that path
		var output string

		code, err := deps.Process.Execute(util.Cmd("git", "tag"), &output, url)
		if err != nil || code == 0 {
			return err == nil, err
		}

		if err := uvcs.CheckForRepoOwnershipError(deps.Process.GetErrorOutput(), url, nil); err != nil {
			return false, err
		}
	}

	if !deep {
		return false, nil
	}

	gitUtil := uvcs.NewGit(deps.IO, deps.Config, deps.Process, util.NewFilesystem(nil))
	if err := uvcs.CleanEnv(deps.Process); err != nil {
		return false, err
	}

	if err := gitUtil.RunCommands([][]string{{"git", "ls-remote", "--heads", "--", "%url%"}}, url, os.TempDir(), false, nil); err != nil {
		if phperr.InstanceOf(err, "RuntimeException") {
			return false, nil
		}

		return false, err
	}

	return true, nil
}
