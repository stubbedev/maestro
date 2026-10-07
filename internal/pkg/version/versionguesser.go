// Ports src/Composer/Package/Version/VersionGuesser.php, with the parts of
// Composer\Util\Git, Composer\Util\Svn and Composer\Repository\Vcs\HgDriver
// it calls.

package version

import (
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/loader"
	"github.com/stubbedev/maestro/internal/semver"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/vcs"
)

// ProcessResult is the part of a finished Symfony Process the guesser
// reads.
type ProcessResult interface {
	IsSuccessful() bool
	GetOutput() string
}

// ProcessExecutor is the part of Composer\Util\ProcessExecutor the
// guesser uses. NewProcessExecutor adapts a *util.ProcessExecutor.
type ProcessExecutor interface {
	Execute(command []string, output *string, cwd string) (int, error)
	ExecuteAsync(command []string, cwd string) (*util.Promise[ProcessResult], error)
	GetErrorOutput() string
	SplitLines(output string) []string
	SetMaxJobs(maxJobs int)
	ResetMaxJobs()
	Wait()
}

type processExecutor struct{ *util.ProcessExecutor }

// NewProcessExecutor adapts p to ProcessExecutor.
func NewProcessExecutor(p *util.ProcessExecutor) ProcessExecutor { return processExecutor{p} }

func (p processExecutor) Execute(command []string, output *string, cwd string) (int, error) {
	if !p.LogsCommands() && absentTool(command) {
		// what the shell's exec of a command it cannot find gives
		if output != nil {
			*output = ""
		}

		return 127, nil
	}

	return p.ProcessExecutor.Execute(util.Cmd(command...), output, cwd)
}

// Prefetch starts command ahead of its Execute (util.ProcessExecutor's
// Prefetch).
func (p processExecutor) Prefetch(command []string, cwd string) {
	if !absentTool(command) {
		p.ProcessExecutor.Prefetch(util.Cmd(command...), cwd)
	}
}

// absentTools are the tools whose absence absentTool checks: those the
// guesser only asks for a version, whose error output nothing reads.
var absentTools = map[string]bool{"hg": true, "fossil": true, "svn": true}

// absentTool reports whether command runs one of absentTools that is in
// no directory of PATH, so that the shell would answer "not found" (exit
// code 127) and the guess goes on: it is then not started (deliberate
// deviation 3), unless commands are logged (-vvv), where Composer's log
// shows it run. With a relative PATH entry, which the shell resolves
// against the command's directory, or on Windows, it is not decided.
func absentTool(command []string) bool {
	// without PATH the shell searches its default one
	path := os.Getenv("PATH")
	if len(command) == 0 || !absentTools[command[0]] || util.IsWindows() || path == "" {
		return false
	}

	for _, dir := range filepath.SplitList(path) {
		if !filepath.IsAbs(dir) {
			return false
		}

		if fi, err := os.Stat(filepath.Join(dir, command[0])); err == nil && !fi.IsDir() {
			return false
		}
	}

	return true
}

// prefetcher is a ProcessExecutor that can start commands ahead of their
// Execute.
type prefetcher interface {
	Prefetch(command []string, cwd string)
}

func (p processExecutor) ExecuteAsync(command []string, cwd string) (*util.Promise[ProcessResult], error) {
	promise, err := p.ProcessExecutor.ExecuteAsync(util.Cmd(command...), cwd)
	if err != nil {
		return nil, err
	}

	return util.Then(promise, func(proc *util.Process) (ProcessResult, error) { return proc, nil }), nil
}

// VersionGuesser ports Composer\Package\Version\VersionGuesser: it guesses
// the root package's version from the VCS checkout it lives in.
//
// The git version is Git::getVersion's process-wide cache
// (vcs.GetVersion), as in Composer.
type VersionGuesser struct {
	process       ProcessExecutor
	versionParser semver.VersionParser
	io            io.IO
}

// NewVersionGuesser ports VersionGuesser::__construct (Composer also takes
// the Config and a VersionParser, which only its HgDriver and semver
// parsing need). io may be nil.
func NewVersionGuesser(process ProcessExecutor, io io.IO) *VersionGuesser {
	return &VersionGuesser{process: process, io: io}
}

var _ loader.VersionGuesser = (*VersionGuesser)(nil)

// versionData is the array the guess* methods return; Version null means
// nothing was found.
type versionData struct {
	version              pkg.NullString
	commit               pkg.NullString
	prettyVersion        pkg.NullString
	featureVersion       pkg.NullString
	featurePrettyVersion pkg.NullString
}

// GuessVersion ports VersionGuesser::guessVersion: the version of the
// checkout at path from git, hg, fossil or svn, or nil.
func (g *VersionGuesser) GuessVersion(packageConfig *php.Array, path string) (*loader.VersionData, error) {
	// bypass version guessing in bash completions as it takes time to create
	// new processes and the root version is usually not that important
	if util.IsInputCompletionProcess() {
		return nil, nil
	}

	return g.guess(newGuessConfig(packageConfig), path)
}

// guess is GuessVersion for the parts of the package configuration it
// reads.
func (g *VersionGuesser) guess(packageConfig guessConfig, path string) (*loader.VersionData, error) {
	data, err := g.guessGitVersion(packageConfig, path)
	if err != nil {
		return nil, err
	}

	if data.version.Valid {
		return postprocess(data)
	}

	for _, guess := range [...]func(guessConfig, string) (*versionData, error){
		g.guessHgVersion,
		func(_ guessConfig, path string) (*versionData, error) { return g.guessFossilVersion(path) },
		g.guessSvnVersion,
	} {
		data, err := guess(packageConfig, path)
		if err != nil {
			return nil, err
		}

		if data != nil && data.version.Valid {
			return postprocess(data)
		}
	}

	return nil, nil
}

var (
	nineRun   = php.MustCompile(`{\.9{7}}`)
	nineRuns  = php.MustCompile(`{(\.9{7})+}`)
	devPrefix = php.MustCompile(`{^dev-}`)
)

func postprocess(d *versionData) (*loader.VersionData, error) {
	if php.ToBool(d.featureVersion.Value()) && d.featureVersion == d.version && d.featurePrettyVersion == d.prettyVersion {
		d.featureVersion, d.featurePrettyVersion = pkg.NullString{}, pkg.NullString{}
	}

	// nineRun is a fixed string; nineRuns repeats a group, which exhausts
	// PCRE's limits on a long enough run, and Preg::replace throws.
	if strings.HasSuffix(d.version.S, "-dev") && mustMatch(nineRun, d.version.S) {
		pretty, _, err := nineRuns.Replace(d.version.S, ".x", -1)
		if err != nil {
			return nil, err
		}
		d.prettyVersion = pkg.Str(pretty)
	}

	if php.ToBool(d.featureVersion.Value()) && strings.HasSuffix(d.featureVersion.S, "-dev") && mustMatch(nineRun, d.featureVersion.S) {
		pretty, _, err := nineRuns.Replace(d.featureVersion.S, ".x", -1)
		if err != nil {
			return nil, err
		}
		d.featurePrettyVersion = pkg.Str(pretty)
	}

	return &loader.VersionData{
		Version:              d.version.S,
		PrettyVersion:        d.prettyVersion.S,
		Commit:               d.commit,
		FeatureVersion:       d.featureVersion,
		FeaturePrettyVersion: d.featurePrettyVersion,
	}, nil
}

var (
	gitCurrentBranch = php.MustCompile(`{^(?:\* ) *(\(no branch\)|\(detached from \S+\)|\(HEAD detached at \S+\)|\S+) *([a-f0-9]+) .*$}`)
	gitRemoteHead    = php.MustCompile(`{^ *.+/HEAD }`)
	gitBranch        = php.MustCompile(`{^(?:\* )? *((?:remotes/(?:origin|upstream)/)?[^\s/]+) *([a-f0-9]+) .*$}`)
	remotePrefix     = php.MustCompile(`{^remotes/\S+/}`)
	rootVersionDev   = php.MustCompile(`{^(\d+(?:\.\d+)*)-dev$}i`)
)

func (g *VersionGuesser) guessGitVersion(packageConfig guessConfig, path string) (*versionData, error) {
	if err := vcs.CleanEnv(g.vcsProcess()); err != nil {
		return nil, err
	}

	var (
		commit, version, prettyVersion       pkg.NullString
		featureVersion, featurePrettyVersion pkg.NullString
		isDetached                           bool
		output                               string
	)

	// try to fetch current version from git branch
	code, err := g.process.Execute([]string{"git", "branch", "-a", "--no-color", "--no-abbrev", "-v"}, &output, path)
	if err != nil {
		return nil, err
	}

	if code != 0 {
		g.prefetchFallbacks(path)
	}

	if code == 0 {
		var branches []string

		isFeatureBranch := false

		// find current branch and collect all branch names
		for _, branch := range g.process.SplitLines(output) {
			if !php.ToBool(branch) {
				continue
			}

			if match, err := gitCurrentBranch.MatchStrictGroups(branch); err != nil {
				return nil, err
			} else if match != nil {
				name := match.Get(1)
				if name == "(no branch)" || strings.HasPrefix(name, "(detached ") || strings.HasPrefix(name, "(HEAD detached at") {
					version = pkg.Str("dev-" + match.Get(2))
					prettyVersion = version
					isFeatureBranch = true
					isDetached = true
				} else {
					version = pkg.Str(g.versionParser.NormalizeBranch(name))
					prettyVersion = pkg.Str("dev-" + name)

					if isFeatureBranch, err = isFeature(packageConfig, name); err != nil {
						return nil, err
					}
				}

				commit = pkg.Str(match.Get(2))
			}

			isRemoteHead, err := gitRemoteHead.IsMatch(branch)
			if err != nil {
				return nil, err
			}
			if !isRemoteHead {
				if match, err := gitBranch.MatchStrictGroups(branch); err != nil {
					return nil, err
				} else if match != nil {
					branches = append(branches, match.Get(1))
				}
			}
		}

		if isFeatureBranch {
			featureVersion = version
			featurePrettyVersion = prettyVersion

			// try to find the best (nearest) version branch to assume this feature's version
			version, prettyVersion, err = g.guessFeatureVersion(packageConfig, version, branches, []string{"git", "rev-list", "%candidate%..%branch%"}, path)
			if err != nil {
				return nil, err
			}
		}
	}

	if err := vcs.CheckForRepoOwnershipError(g.process.GetErrorOutput(), path, g.io); err != nil {
		return nil, err
	}

	if !php.ToBool(version.Value()) || isDetached {
		v, pretty, ok, err := g.versionFromGitTags(path)
		if err != nil {
			return nil, err
		}

		if ok {
			version, prettyVersion = pkg.Str(v), pkg.Str(pretty)
			featureVersion, featurePrettyVersion = pkg.NullString{}, pkg.NullString{}
		}
	}

	if !commit.Valid {
		if commit, err = g.headCommit(path); err != nil {
			return nil, err
		}
	}

	data := &versionData{version: version, commit: commit, prettyVersion: prettyVersion}
	if php.ToBool(featureVersion.Value()) {
		data.featureVersion, data.featurePrettyVersion = featureVersion, featurePrettyVersion
	}

	return data, nil
}

// prefetchFallbacks starts at once the commands guessVersion runs one
// after the other when the checkout at path is not on a git branch: git's
// tag and HEAD lookups, then hg's, fossil's and svn's (deliberate
// deviation 3: each takes a process start, and they are independent).
// Each is still executed, and so logged and read, in Composer's order;
// those it never reaches only ran for nothing (they change nothing).
func (g *VersionGuesser) prefetchFallbacks(path string) {
	pf, ok := g.process.(prefetcher)
	if !ok {
		return
	}

	pf.Prefetch([]string{"git", "describe", "--exact-match", "--tags"}, path)

	if command, err := g.headCommitCommand(); err == nil {
		pf.Prefetch(command, path)
	}

	pf.Prefetch([]string{"hg", "branch"}, path)
	pf.Prefetch([]string{"fossil", "branch", "list"}, path)
	pf.Prefetch([]string{"fossil", "tag", "list"}, path)
	pf.Prefetch([]string{"svn", "info", "--xml"}, path)
}

// headCommitCommand is the git rev-list command for HEAD's commit hash.
func (g *VersionGuesser) headCommitCommand() ([]string, error) {
	noShowSignature, err := vcs.GetNoShowSignatureFlags(g.vcsProcess())
	if err != nil {
		return nil, err
	}

	return vcs.BuildRevListCommand(g.vcsProcess(), append([]string{"--format=%H", "-n1", "HEAD"}, noShowSignature...))
}

// headCommit runs git rev-list for HEAD's commit hash.
func (g *VersionGuesser) headCommit(path string) (pkg.NullString, error) {
	command, err := g.headCommitCommand()
	if err != nil {
		return pkg.NullString{}, err
	}

	var output string

	code, err := g.process.Execute(command, &output, path)
	if err != nil || code != 0 {
		return pkg.NullString{}, err
	}

	parsed, err := vcs.ParseRevListOutput(output, g.vcsProcess())
	if err != nil {
		return pkg.NullString{}, err
	}

	if c := php.Trim(parsed); php.ToBool(c) {
		return pkg.Str(c), nil
	}

	return pkg.NullString{}, nil
}

func (g *VersionGuesser) versionFromGitTags(path string) (version, prettyVersion string, ok bool, err error) {
	// try to fetch current version from git tags
	var output string

	code, err := g.process.Execute([]string{"git", "describe", "--exact-match", "--tags"}, &output, path)
	if err != nil || code != 0 {
		return "", "", false, err
	}

	if version, err := g.versionParser.Normalize(php.Trim(output)); err == nil {
		return version, php.Trim(output), true, nil
	}

	return "", "", false, nil
}

func (g *VersionGuesser) guessHgVersion(packageConfig guessConfig, path string) (*versionData, error) {
	// try to fetch current version from hg branch
	var output string

	code, err := g.process.Execute([]string{"hg", "branch"}, &output, path)
	if err != nil || code != 0 {
		return nil, err
	}

	branch := php.Trim(output)
	version := g.versionParser.NormalizeBranch(branch)
	isFeatureBranch := strings.HasPrefix(version, "dev-")

	if version == pkg.DefaultBranchAlias {
		return &versionData{version: pkg.Str(version), prettyVersion: pkg.Str("dev-" + branch)}, nil
	}

	if !isFeatureBranch {
		return &versionData{version: pkg.Str(version), prettyVersion: pkg.Str(version)}, nil
	}

	// re-use the HgDriver to fetch branches (this properly includes bookmarks)
	branches, err := g.hgBranches()
	if err != nil {
		return nil, err
	}

	// try to find the best (nearest) version branch to assume this feature's version
	v, pretty, err := g.guessFeatureVersion(packageConfig, pkg.Str(version), branches,
		[]string{"hg", "log", "-r", "not ancestors('%candidate%') and ancestors('%branch%')", "--template", `"{node}\n"`}, path)
	if err != nil {
		return nil, err
	}

	return &versionData{
		version:              v,
		prettyVersion:        pretty,
		commit:               pkg.Str(""),
		featureVersion:       pkg.Str(version),
		featurePrettyVersion: pkg.Str(version),
	}, nil
}

var (
	hgBranchLine   = php.MustCompile(`(^([^\s]+)\s+\d+:([a-f0-9]+))`)
	hgBookmarkLine = php.MustCompile(`(^(?:[\s*]*)([^\s]+)\s+\d+:(.*)$)`)
)

// hgBranches ports array_map('strval', array_keys(HgDriver::getBranches()))
// for the driver VersionGuesser builds, which is never initialized: its
// commands run in the current directory.
func (g *VersionGuesser) hgBranches() ([]string, error) {
	collect := func(command []string, re *php.Regexp) (*php.Array, error) {
		var output string
		if _, err := g.process.Execute(command, &output, ""); err != nil {
			return nil, err
		}

		found := php.NewArray()

		for _, line := range g.process.SplitLines(output) {
			if !php.ToBool(line) {
				continue
			}

			match, err := re.MatchStrictGroups(line)
			if err != nil {
				return nil, err
			}

			if match != nil && match.Get(1)[0] != '-' {
				found.Set(match.Get(1), match.Get(2))
			}
		}

		return found, nil
	}

	branches, err := collect([]string{"hg", "branches"}, hgBranchLine)
	if err != nil {
		return nil, err
	}

	bookmarks, err := collect([]string{"hg", "bookmarks"}, hgBookmarkLine)
	if err != nil {
		return nil, err
	}

	// Branches will have preference over bookmarks
	var names []string
	for k := range php.ArrayMerge(bookmarks, branches).All() {
		names = append(names, k.String())
	}

	return names, nil
}

// guessFeatureVersion ports VersionGuesser::guessFeatureVersion: the version
// of the nearest non-feature branch, found by running scmCmdline for each
// candidate. PHP handles the results as the processes finish, cancelling
// the rest once a candidate has no commits in between; here the results
// are handled in candidate order, which picks the same branch unless
// several have none.
func (g *VersionGuesser) guessFeatureVersion(packageConfig guessConfig, version pkg.NullString, branches []string, scmCmdline []string, path string) (pkg.NullString, pkg.NullString, error) {
	prettyVersion := version

	// ignore feature branches if they have no branch-alias or self.version is used
	// and find the branch they came from to use as a version instead
	if packageConfig.branchAlias.Isset(version.S) && !packageConfig.selfVersion {
		return version, prettyVersion, nil
	}

	branch := mustReplace(devPrefix, version.S, "")

	// return directly, if branch is configured to be non-feature branch
	if feature, err := isFeature(packageConfig, branch); err != nil || !feature {
		return version, prettyVersion, err
	}

	// sort local branches first then remote ones
	// and sort numeric branches below named ones, to make sure if the branch has the same distance from main and 1.10 and 1.9 for example, 1.9 is picked
	// and sort using natural sort so that 1.10 will appear before 1.9
	branches = append([]string(nil), branches...)
	php.SortSlice(branches, func(a, b string) int {
		aRemote := strings.HasPrefix(a, "remotes/")
		bRemote := strings.HasPrefix(b, "remotes/")

		if aRemote != bRemote {
			if aRemote {
				return 1
			}

			return -1
		}

		return php.Strnatcasecmp(b, a)
	})

	type candidateRun struct {
		index            int
		candidateVersion string
		promise          *util.Promise[ProcessResult]
	}

	var runs []candidateRun

	g.process.SetMaxJobs(30)
	defer g.process.ResetMaxJobs()

	for index, candidate := range branches {
		candidateVersion, _, err := remotePrefix.Replace(candidate, "", -1)
		if err != nil {
			return pkg.NullString{}, pkg.NullString{}, err
		}

		// do not compare against itself or other feature branches
		if candidate == branch {
			continue
		}

		if feature, err := isFeature(packageConfig, candidateVersion); err != nil {
			return pkg.NullString{}, pkg.NullString{}, err
		} else if feature {
			continue
		}

		cmdLine := make([]string, len(scmCmdline))
		for i, component := range scmCmdline {
			cmdLine[i] = strings.ReplaceAll(strings.ReplaceAll(component, "%candidate%", candidate), "%branch%", branch)
		}

		promise, err := g.process.ExecuteAsync(cmdLine, path)
		if err != nil {
			return pkg.NullString{}, pkg.NullString{}, err
		}

		runs = append(runs, candidateRun{index, candidateVersion, promise})
	}

	g.process.Wait()

	lastIndex := -1
	length := math.MaxInt

	for i, run := range runs {
		process, err := run.promise.Wait()
		if err != nil || !process.IsSuccessful() {
			continue
		}

		output := process.GetOutput()

		// overwrite existing if we have a shorter diff, or we have an equal diff and an index that comes later in the array (i.e. older version)
		// as newer versions typically have more commits, if the feature branch is based on a newer branch it should have a longer diff to the old version
		// but if it doesn't and they have equal diffs, then it probably is based on the old version
		if len(output) < length || (len(output) == length && lastIndex < run.index) {
			lastIndex = run.index
			length = len(output)
			version = pkg.Str(g.versionParser.NormalizeBranch(run.candidateVersion))
			prettyVersion = pkg.Str("dev-" + run.candidateVersion)

			if length == 0 {
				for _, other := range runs[i+1:] {
					other.promise.Cancel()
				}
			}
		}
	}

	return version, prettyVersion, nil
}

// isFeature ports VersionGuesser::isFeatureBranch.
func isFeature(packageConfig guessConfig, branchName string) (bool, error) {
	ok, err := php.PregIsMatch(`{^(`+packageConfig.nonFeatureBranches+`|master|main|latest|next|current|support|tip|trunk|default|develop|\d+\..+)$}`, branchName)
	if err != nil {
		return false, err
	}

	return !ok, nil
}

func (g *VersionGuesser) guessFossilVersion(path string) (*versionData, error) {
	var (
		version, prettyVersion pkg.NullString
		output                 string
	)

	// try to fetch current version from fossil
	code, err := g.process.Execute([]string{"fossil", "branch", "list"}, &output, path)
	if err != nil {
		return nil, err
	}

	if code == 0 {
		branch := php.Trim(output)
		version = pkg.Str(g.versionParser.NormalizeBranch(branch))
		prettyVersion = pkg.Str("dev-" + branch)
	}

	// try to fetch current version from fossil tags
	if code, err = g.process.Execute([]string{"fossil", "tag", "list"}, &output, path); err != nil {
		return nil, err
	}

	if code == 0 {
		if v, err := g.versionParser.Normalize(php.Trim(output)); err == nil {
			version = pkg.Str(v)
			prettyVersion = pkg.Str(php.Trim(output))
		}
	}

	return &versionData{version: version, commit: pkg.Str(""), prettyVersion: prettyVersion}, nil
}

func (g *VersionGuesser) guessSvnVersion(packageConfig guessConfig, path string) (*versionData, error) {
	vcs.SvnCleanEnv()

	// try to fetch current version from svn
	var output string

	code, err := g.process.Execute([]string{"svn", "info", "--xml"}, &output, path)
	if err != nil || code != 0 {
		return nil, err
	}

	trunkPath, branchesPath, tagsPath := packageConfig.trunkPath, packageConfig.branchesPath, packageConfig.tagsPath
	urlPattern := "#<url>.*/(" + trunkPath + "|(" + branchesPath + "|" + tagsPath + ")/(.*))</url>#"

	re, err := php.Compile(urlPattern)
	if err != nil {
		return nil, err
	}

	matches, err := re.Match(output)
	if err != nil || matches == nil {
		return nil, err
	}

	m2, ok2 := matches.Group(2)
	m3, ok3 := matches.Group(3)

	if ok2 && ok3 && (branchesPath == m2 || tagsPath == m2) {
		// we are in a branches path
		return &versionData{
			version:       pkg.Str(g.versionParser.NormalizeBranch(m3)),
			commit:        pkg.Str(""),
			prettyVersion: pkg.Str("dev-" + m3),
		}, nil
	}

	prettyVersion := php.Trim(matches.Get(1))

	version := "dev-trunk"
	if prettyVersion != "trunk" {
		if version, err = g.versionParser.Normalize(prettyVersion); err != nil {
			return nil, err
		}
	}

	return &versionData{version: pkg.Str(version), commit: pkg.Str(""), prettyVersion: pkg.Str(prettyVersion)}, nil
}

// RootVersionFromEnv ports VersionGuesser::getRootVersionFromEnv:
// COMPOSER_ROOT_VERSION, with "1.2-dev" turned into "1.2.x-dev".
func (g *VersionGuesser) RootVersionFromEnv() (string, error) {
	version, _ := util.GetEnv("COMPOSER_ROOT_VERSION")
	if version == "" {
		return "", &util.RuntimeError{Message: "COMPOSER_ROOT_VERSION not set or empty"}
	}

	// COMPOSER_ROOT_VERSION can be long enough to exhaust the backtrack
	// limit: Preg::isMatch's PcreException is returned.
	match, err := rootVersionDev.Match(version)
	if err != nil {
		return "", err
	}
	if match != nil {
		version = match.Get(1) + ".x-dev"
	}

	return version, nil
}

// vcsProcess adapts the guesser's process executor to the Composer\Util\Git
// helpers of internal/util/vcs.
func (g *VersionGuesser) vcsProcess() vcs.Process { return vcsProcess{g.process} }

type vcsProcess struct{ ProcessExecutor }

func (p vcsProcess) Execute(command util.Command, output *string, cwd string) (int, error) {
	if command.IsShell() {
		return 0, &util.RuntimeError{Message: "VersionGuesser runs argument lists only: " + command.Line()}
	}

	return p.ProcessExecutor.Execute(command.Args(), output, cwd)
}

// Executor is the *util.ProcessExecutor the commands run with, nil for
// another ProcessExecutor (a test's mock): vcs.GetVersion uses the version
// earlier runs kept only for a real one.
func (p vcsProcess) Executor() *util.ProcessExecutor {
	if e, ok := p.ProcessExecutor.(processExecutor); ok {
		return e.ProcessExecutor
	}

	return nil
}

func (p vcsProcess) ExecuteFunc(command util.Command, _ func(typ, buffer string), _ string) (int, error) {
	return 0, &util.RuntimeError{Message: "VersionGuesser cannot stream the output of " + command.String()}
}

// Helpers.

func jsonEncode(v any) string {
	s, _ := php.JSONEncode(v, 0)

	return s
}

// guessConfig is all GuessVersion reads of the package configuration; the
// guess is the same for configurations that agree on it (GuessKey).
type guessConfig struct {
	// branchAlias is extra.branch-alias
	branchAlias *php.Array
	// selfVersion: the encoded configuration holds "self.version"
	selfVersion bool
	// nonFeatureBranches is non-feature-branches joined with "|"
	nonFeatureBranches string
	// the svn layout's paths, preg-quoted
	trunkPath, branchesPath, tagsPath string
}

func newGuessConfig(packageConfig *php.Array) guessConfig {
	c := guessConfig{
		branchAlias: packageConfig.ArrayAt("extra").ArrayAt("branch-alias"),
		selfVersion: strings.Contains(jsonEncode(packageConfig), `"self.version"`),
	}
	if v := packageConfig.At("non-feature-branches"); php.ToBool(v) {
		if list, ok := v.(*php.Array); ok {
			parts := make([]string, 0, list.Len())
			for _, s := range list.All() {
				parts = append(parts, php.ToString(s))
			}

			c.nonFeatureBranches = strings.Join(parts, "|")
		}
	}
	pathOption := func(key, def string) string {
		if v := packageConfig.At(key); v != nil {
			return php.PregQuote(php.ToString(v), "#")
		}

		return def
	}
	c.trunkPath = pathOption("trunk-path", "trunk")
	c.branchesPath = pathOption("branches-path", "branches")
	c.tagsPath = pathOption("tags-path", "tags")

	return c
}

// GuessKey is a key of what GuessVersion reads of packageConfig: for two
// configurations of the same key, GuessVersion guesses alike (in the same
// checkout, unchanged).
func GuessKey(packageConfig *php.Array) string {
	c := newGuessConfig(packageConfig)
	branchAlias := "" // none
	if c.branchAlias != nil {
		branchAlias = jsonEncode(c.branchAlias)
	}
	var b strings.Builder
	for _, part := range [...]string{branchAlias, strconv.FormatBool(c.selfVersion), c.nonFeatureBranches, c.trunkPath, c.branchesPath, c.tagsPath} {
		b.WriteString(strconv.Itoa(len(part)))
		b.WriteByte(':')
		b.WriteString(part)
	}

	return b.String()
}
