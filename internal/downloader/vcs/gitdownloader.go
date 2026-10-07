// Ports src/Composer/Downloader/GitDownloader.php.

package vcs

import (
	"slices"
	"strconv"
	"strings"

	"github.com/stubbedev/maestro/internal/cache"
	mio "github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/semver"
	"github.com/stubbedev/maestro/internal/store"
	"github.com/stubbedev/maestro/internal/util"
	vcsutil "github.com/stubbedev/maestro/internal/util/vcs"
)

// GitDownloader ports Composer\Downloader\GitDownloader: clones through
// the bare mirror cache (cache-vcs-dir), checks out commits and branches,
// and offers to discard, stash or show local changes before updating.
type GitDownloader struct {
	vcsDownloader
	gitUtil *vcsutil.Git
	// store keeps checkouts cloned from the mirror cache (gitstore.go);
	// nil disables it.
	store *store.Store

	// hasStashedChanges, hasDiscardedChanges (by path) and cachedPackages
	// (package id => reference) are guarded by vcsDownloader.mu.
	hasStashedChanges   map[string]bool
	hasDiscardedChanges map[string]bool
	cachedPackages      map[int]map[string]bool
	// gitHost is the key part of git and its environment, once computed.
	gitHost *gitHost
}

// NewGitDownloader is new GitDownloader($io, $config, $process, $fs).
func NewGitDownloader(deps Deps) *GitDownloader {
	d := &GitDownloader{
		hasStashedChanges:   map[string]bool{},
		hasDiscardedChanges: map[string]bool{},
		cachedPackages:      map[int]map[string]bool{},
	}
	d.vcsDownloader = newVcsDownloader(deps, d, `Composer\Downloader\GitDownloader`)
	d.gitUtil = vcsutil.NewGit(d.io, d.config, d.process, d.filesystem)
	d.store = deps.Store

	return d
}

var (
	nonCacheChars = php.MustCompile(`{[^a-z0-9.]}i`)
	devAffixes    = php.MustCompile(`{(?:^dev-|(?:\.x)?-dev$)}i`)
	sha1Reference = php.MustCompile(`{^[a-f0-9]{40}$}`)
	shortHashable = php.MustCompile(`{^[0-9a-f]{40}$}`)
	originRemote  = php.MustCompile(`{^origin\s+(?P<url>\S+)}m`)
	composerRmt   = php.MustCompile(`{^composer\s+(?P<url>\S+)}m`)
	headRefLine   = php.MustCompile(`{^([a-f0-9]+) HEAD$}mi`)
)

// cachePath is the mirror directory of url under cache-vcs-dir.
func (d *GitDownloader) cachePath(url string) (string, error) {
	safeURL, err := util.SanitizeURLChecked(url)
	if err != nil {
		return "", err
	}
	name, _, err := nonCacheChars.Replace(safeURL, "-", -1)
	if err != nil {
		return "", err
	}

	return php.ToString(d.config.Get("cache-vcs-dir")) + "/" + name + "/", nil
}

func (d *GitDownloader) isCached(p pkg.PackageInterface, ref string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.cachedPackages[p.ID()][ref]
}

func (d *GitDownloader) doDownload(p pkg.PackageInterface, _, url string, _ pkg.PackageInterface) error {
	// Do not create an extra local cache when repository is already local
	if util.IsLocalPath(url) {
		return nil
	}

	if err := vcsutil.CleanEnv(d.process); err != nil {
		return err
	}

	cachePath, err := d.cachePath(url)
	if err != nil {
		return err
	}

	gitVersion, found, err := vcsutil.GetVersion(d.process)
	if err != nil {
		return err
	}

	if !found {
		return &util.RuntimeError{Message: "git was not found in your PATH, skipping source download"}
	}

	// --dissociate option is only available since git 2.3.0-rc0
	if dissociate, _ := semver.VersionCompareOp(gitVersion, "2.3.0-rc0", ">="); php.ToBool(gitVersion) && dissociate && cache.IsUsable(cachePath) {
		d.io.WriteError("  - Syncing <info>"+p.Name()+"</info> (<comment>"+p.FullPrettyVersion(true, pkg.DisplaySourceRefIfDev)+"</comment>) into cache", true, mio.Normal)
		d.io.WriteError("    Cloning to cache at "+cachePath, true, mio.Debug)

		ref := p.SourceReference().S

		inMirror, err := d.gitUtil.FetchRefOrSyncMirror(url, cachePath, ref, p.PrettyVersion())
		if err != nil {
			return err
		}

		if inMirror && isDir(cachePath) {
			d.mu.Lock()
			if d.cachedPackages[p.ID()] == nil {
				d.cachedPackages[p.ID()] = map[string]bool{}
			}
			d.cachedPackages[p.ID()][ref] = true
			d.mu.Unlock()
		}
	}

	return nil
}

func (d *GitDownloader) doInstall(p pkg.PackageInterface, path, url string) error {
	if err := vcsutil.CleanEnv(d.process); err != nil {
		return err
	}

	path = d.normalizePath(path)

	cachePath, err := d.cachePath(url)
	if err != nil {
		return err
	}

	ref := p.SourceReference().S

	var (
		msg      string
		commands [][]string
		storeID  [32]byte
		useStore bool
	)

	if d.isCached(p, ref) {
		msg = "Cloning " + d.shortHash(ref) + " from cache"

		if d.storeEligible(p, path) {
			storeID, useStore = d.gitStoreKey(p, url, cachePath, path)
		}

		if useStore {
			d.io.WriteError(msg, true, mio.Normal)

			// what RunCommands checks first
			if err := d.config.ProhibitURLByConfig(url, d.io, nil); err != nil {
				return err
			}

			if d.installFromStore(storeID, path) {
				return nil
			}

			msg = ""
		}

		cloneFlags := []string{"--dissociate", "--reference", cachePath}
		if php.ToBool(arrayPath(p.TransportOptions(), "git", "single_use_clone")) {
			cloneFlags = nil
		}

		commands = [][]string{
			append([]string{"git", "clone", "--no-checkout", cachePath, path}, cloneFlags...),
			{"git", "remote", "set-url", "origin", "--", "%sanitizedUrl%"},
			{"git", "remote", "add", "composer", "--", "%sanitizedUrl%"},
		}
	} else {
		msg = "Cloning " + d.shortHash(ref)
		commands = [][]string{
			{"git", "clone", "--no-checkout", "--", "%url%", path},
			{"git", "remote", "add", "composer", "--", "%url%"},
			{"git", "fetch", "composer"},
			{"git", "remote", "set-url", "origin", "--", "%sanitizedUrl%"},
			{"git", "remote", "set-url", "composer", "--", "%sanitizedUrl%"},
		}

		if networkDisabled() {
			return &util.RuntimeError{Message: "The required git reference for " + p.Name() + " is not in cache and network is disabled, aborting"}
		}
	}

	if msg != "" {
		d.io.WriteError(msg, true, mio.Normal)
	}

	if err := d.gitUtil.RunCommands(commands, url, path, true, nil); err != nil {
		return err
	}

	if sourceURL := p.SourceURL(); sourceURL.Valid && url != sourceURL.S {
		if err := d.updateOriginURL(path, sourceURL.S); err != nil {
			return err
		}
	} else if err := d.setPushURL(path, url); err != nil {
		return err
	}

	// updateToCommit never falls back to another commit (it returns null
	// or throws), so Composer's update of the package references is dead code
	if err := d.updateToCommit(p, path, ref, p.PrettyVersion()); err != nil {
		return err
	}

	if useStore {
		// best effort: the checkout is in place either way
		_, _ = d.store.InsertDir(storeID, path)
	}

	return nil
}

func networkDisabled() bool {
	v, _ := util.GetEnv("COMPOSER_DISABLE_NETWORK")

	return php.ToBool(v)
}

func (d *GitDownloader) doUpdate(_, target pkg.PackageInterface, path, url string) error {
	if err := vcsutil.CleanEnv(d.process); err != nil {
		return err
	}

	path = d.normalizePath(path)
	if !d.hasMetadataRepository(path) {
		return &util.RuntimeError{Message: "The .git directory is missing from " + path + ", see https://getcomposer.org/commit-deps for more information"}
	}

	cachePath, err := d.cachePath(url)
	if err != nil {
		return err
	}

	ref := target.SourceReference().S

	var msg, remoteURL string

	if d.isCached(target, ref) {
		msg = "Checking out " + d.shortHash(ref) + " from cache"
		remoteURL = cachePath
	} else {
		msg = "Checking out " + d.shortHash(ref)
		remoteURL = "%url%"

		if networkDisabled() {
			return &util.RuntimeError{Message: "The required git reference for " + target.Name() + " is not in cache and network is disabled, aborting"}
		}
	}

	d.io.WriteError(msg, true, mio.Normal)

	var output string

	code, err := d.execute([]string{"git", "rev-parse", "--quiet", "--verify", ref + "^{commit}"}, &output, path)
	if err != nil {
		return err
	}

	if code != 0 {
		commands := [][]string{
			{"git", "remote", "set-url", "composer", "--", remoteURL},
			{"git", "fetch", "composer"},
			{"git", "fetch", "--tags", "composer"},
		}

		if err := d.gitUtil.RunCommands(commands, url, path, false, nil); err != nil {
			return err
		}
	}

	command := []string{"git", "remote", "set-url", "composer", "--", "%sanitizedUrl%"}
	if err := d.gitUtil.RunCommands([][]string{command}, url, path, false, nil); err != nil {
		return err
	}

	if err := d.updateToCommit(target, path, ref, target.PrettyVersion()); err != nil {
		return err
	}

	updateOriginURL := false

	code, err = d.execute([]string{"git", "remote", "-v"}, &output, path)
	if err != nil {
		return err
	}

	if code == 0 {
		originMatch, err := originRemote.Match(output)
		if err != nil {
			return err
		}
		var composerMatch *php.Match
		if originMatch != nil {
			if composerMatch, err = composerRmt.Match(output); err != nil {
				return err
			}
		}

		if originMatch != nil && composerMatch != nil {
			originURL, _ := originMatch.Named("url")
			composerURL, _ := composerMatch.Named("url")

			if sourceURL := target.SourceURL(); originURL == composerURL && (!sourceURL.Valid || composerURL != sourceURL.S) {
				updateOriginURL = true
			}
		}
	}

	if sourceURL := target.SourceURL(); updateOriginURL && sourceURL.Valid {
		return d.updateOriginURL(path, sourceURL.S)
	}

	return nil
}

// LocalChanges is getLocalChanges().
func (d *GitDownloader) LocalChanges(_ pkg.PackageInterface, path string) (pkg.NullString, error) {
	if err := vcsutil.CleanEnv(d.process); err != nil {
		return pkg.NullString{}, err
	}

	if !d.hasMetadataRepository(path) {
		return pkg.NullString{}, nil
	}

	var output string
	if err := d.mustExecute([]string{"git", "status", "--porcelain", "--untracked-files=no"}, &output, path); err != nil {
		return pkg.NullString{}, err
	}

	return trimmedOrNull(output), nil
}

// showRefs runs `git show-ref --head -d` and returns its trimmed output,
// failing as mustExecute does.
func (d *GitDownloader) showRefs(path string) (string, error) {
	var output string
	if err := d.mustExecute([]string{"git", "show-ref", "--head", "-d"}, &output, path); err != nil {
		return "", err
	}

	return php.Trim(output), nil
}

// UnpushedChanges is getUnpushedChanges().
func (d *GitDownloader) UnpushedChanges(_ pkg.PackageInterface, path string) (pkg.NullString, error) {
	if err := vcsutil.CleanEnv(d.process); err != nil {
		return pkg.NullString{}, err
	}

	path = d.normalizePath(path)
	if !d.hasMetadataRepository(path) {
		return pkg.NullString{}, nil
	}

	refs, err := d.showRefs(path)
	if err != nil {
		return pkg.NullString{}, err
	}

	match, err := headRefLine.MatchStrictGroups(refs)
	if err != nil || match == nil {
		// could not match the HEAD for some reason
		return pkg.NullString{}, err
	}

	headRef := match.Get(1)

	matches, err := php.PregMatchAllStrictGroups(`{^`+php.PregQuote(headRef, "")+` refs/heads/(.+)$}mi`, refs)
	if err != nil || len(matches) == 0 {
		// not on a branch, we are either on a not-modified tag or some sort of detached head, so skip this
		return pkg.NullString{}, err
	}

	candidateBranches := make([]string, len(matches))
	for i, m := range matches {
		candidateBranches[i] = m.Get(1)
	}

	// use the first match as branch name for now
	branch := candidateBranches[0]

	var unpushedChanges pkg.NullString

	branchNotFoundError := false

	// do two passes, as if we find anything we want to fetch and then re-try
	for i := range 2 {
		var remoteBranches []string

		// try to find matching branch names in remote repos
		for _, candidate := range candidateBranches {
			matches, err := php.PregMatchAllStrictGroups(`{^[a-f0-9]+ refs/remotes/((?:[^/]+)/`+php.PregQuote(candidate, "")+`)$}mi`, refs)
			if err != nil {
				return pkg.NullString{}, err
			}

			if len(matches) > 0 {
				for _, m := range matches {
					branch = candidate
					remoteBranches = append(remoteBranches, m.Get(1))
				}

				break
			}
		}

		// if it doesn't exist, then we assume it is an unpushed branch
		// this is bad as we have no reference point to do a diff so we just bail listing
		// the branch as being unpushed
		if len(remoteBranches) == 0 {
			unpushedChanges = pkg.Str("Branch " + branch + " could not be found on any remote and appears to be unpushed")
			branchNotFoundError = true
		} else {
			// if first iteration found no remote branch but it has now found some, reset $unpushedChanges
			// so we get the real diff output no matter its length
			if branchNotFoundError {
				unpushedChanges = pkg.NullString{}
			}

			for _, remoteBranch := range remoteBranches {
				var output string
				if err := d.mustExecute([]string{"git", "diff", "--name-status", remoteBranch + "..." + branch, "--"}, &output, path); err != nil {
					return pkg.NullString{}, err
				}

				output = php.Trim(output)
				// keep the shortest diff from all remote branches we compare against
				if !unpushedChanges.Valid || len(output) < len(unpushedChanges.S) {
					unpushedChanges = pkg.Str(output)
				}
			}
		}

		// first pass and we found unpushed changes, fetch from all remotes to make sure we have up to date
		// remotes and then try again as outdated remotes can sometimes cause false-positives
		if php.ToBool(unpushedChanges.S) && i == 0 {
			if _, err := d.execute([]string{"git", "fetch", "--all"}, nil, path); err != nil {
				return pkg.NullString{}, err
			}

			// update list of refs after fetching
			if refs, err = d.showRefs(path); err != nil {
				return pkg.NullString{}, err
			}
		}

		// abort after first pass if we didn't find anything
		if !php.ToBool(unpushedChanges.S) {
			break
		}
	}

	return unpushedChanges, nil
}

func (d *GitDownloader) cleanChanges(p pkg.PackageInterface, path string, update bool) error {
	if err := vcsutil.CleanEnv(d.process); err != nil {
		return err
	}

	path = d.normalizePath(path)

	unpushed, err := d.UnpushedChanges(p, path)
	if err != nil {
		return err
	}

	if php.ToBool(unpushed.S) && (d.io.IsInteractive() || d.config.Get("discard-changes") != true) {
		return &util.RuntimeError{Message: "Source directory " + path + " has unpushed changes on the current branch: \n" + unpushed.S}
	}

	changes, err := d.LocalChanges(p, path)
	if err != nil || !changes.Valid {
		return err
	}

	if !d.io.IsInteractive() {
		switch d.config.Get("discard-changes") {
		case true:
			return d.discardChanges(path)
		case "stash":
			if !update {
				return d.vcsDownloader.cleanChanges(p, path, update)
			}

			return d.stashChanges(path)
		}

		return d.vcsDownloader.cleanChanges(p, path, update)
	}

	lines, err := changeLines(changes.S)
	if err != nil {
		return err
	}

	d.io.WriteError("    <error>"+p.PrettyName()+" has modified files:</error>", true, mio.Normal)
	d.io.WriteErrorMessages(lines[:min(10, len(lines))], true, mio.Normal)

	if len(lines) > 10 {
		d.io.WriteError("    <info>"+strconv.Itoa(len(lines)-10)+` more files modified, choose "v" to view the full list</info>`, true, mio.Normal)
	}

	stash, action := "", "uninstall"
	if update {
		stash, action = "s,", "update"
	}

	for {
		answer, err := d.io.Ask("    <info>Discard changes [y,n,v,d,"+stash+"?]?</info> ", "?")
		if err != nil {
			return err
		}

		switch askString(answer) {
		case "y":
			return d.discardChanges(path)
		case "s":
			if update {
				return d.stashChanges(path)
			}

			d.writeHelp(action, update)
		case "n":
			return &util.RuntimeError{Message: "Update aborted"}
		case "v":
			d.io.WriteErrorMessages(lines, true, mio.Normal)
		case "d":
			if err := d.viewDiff(path); err != nil {
				return err
			}
		default:
			d.writeHelp(action, update)
		}
	}
}

// writeHelp is the `help:` branch of cleanChanges' prompt.
func (d *GitDownloader) writeHelp(action string, update bool) {
	d.io.WriteErrorMessages([]string{
		"    y - discard changes and apply the " + action,
		"    n - abort the " + action + " and let you manually clean things up",
		"    v - view modified files",
		"    d - view local modifications (diff)",
	}, true, mio.Normal)

	if update {
		d.io.WriteError("    s - stash changes and try to reapply them after the update", true, mio.Normal)
	}

	d.io.WriteError("    ? - print help", true, mio.Normal)
}

func (d *GitDownloader) reapplyChanges(path string) error {
	path = d.normalizePath(path)

	d.mu.Lock()
	stashed := d.hasStashedChanges[path]
	delete(d.hasStashedChanges, path)
	d.mu.Unlock()

	if stashed {
		d.io.WriteError("    <info>Re-applying stashed changes</info>", true, mio.Normal)

		code, err := d.execute([]string{"git", "stash", "pop"}, nil, path)
		if err != nil {
			return err
		}

		if code != 0 {
			return &util.RuntimeError{Message: "Failed to apply stashed changes:\n\n" + d.process.GetErrorOutput()}
		}
	}

	d.mu.Lock()
	delete(d.hasDiscardedChanges, path)
	d.mu.Unlock()

	return nil
}

// updateToCommit is updateToCommit(): checks out reference at path. Its
// PHP result, a commit checked out in place of a missing reference, is
// always null in Composer 2.10.
func (d *GitDownloader) updateToCommit(p pkg.PackageInterface, path, reference, prettyVersion string) error {
	d.mu.Lock()
	var force []string
	if d.hasDiscardedChanges[path] || d.hasStashedChanges[path] {
		force = []string{"-f"}
	}
	d.mu.Unlock()

	// This uses the "--" sequence to separate branch from file parameters.
	//
	// Otherwise git tries the branch name as well as file name.
	// If the non-existent branch is actually the name of a file, the file
	// is checked out.

	branch, _, err := devAffixes.Replace(prettyVersion, "", -1)
	if err != nil {
		return err
	}

	var output string

	execute := func(command []string) (bool, error) {
		output = ""
		code, err := d.execute(command, &output, path)

		return err == nil && code == 0, err
	}

	// all runs the commands while they succeed: `$execute($a) && $execute($b)`.
	all := func(commands ...[]string) (bool, error) {
		for _, command := range commands {
			if ok, err := execute(command); err != nil || !ok {
				return false, err
			}
		}

		return true, nil
	}

	var branches *string

	if ok, err := execute([]string{"git", "branch", "-r"}); err != nil {
		return err
	} else if ok {
		branchesOut := output
		branches = &branchesOut
	}

	hasRemoteBranch := func(name string) (bool, error) {
		if branches == nil {
			return false, nil
		}

		return php.PregIsMatch(`{^\s+composer/`+php.PregQuote(name, "")+`$}m`, *branches)
	}

	// Anchored and fixed-length: Preg::isMatch cannot throw.
	isSha, _ := sha1Reference.IsMatch(reference)

	// check whether non-commitish are branches or tags, and fetch branches with the remote name
	gitRef := reference
	hasReference := false
	if !isSha {
		if hasReference, err = hasRemoteBranch(reference); err != nil {
			return err
		}
	}
	if hasReference {
		command1 := slices.Concat([]string{"git", "checkout"}, force, []string{"-B", branch, "composer/" + reference, "--"})
		command2 := []string{"git", "reset", "--hard", "composer/" + reference, "--"}

		if ok, err := all(command1, command2); err != nil || ok {
			return err
		}
	}

	// try to checkout branch by name and then reset it so it's on the proper branch name
	if isSha {
		// add 'v' in front of the branch if it was stripped when generating the pretty name
		if branches != nil {
			hasBranch, err := hasRemoteBranch(branch)
			if err != nil {
				return err
			}
			if !hasBranch {
				hasVBranch, err := hasRemoteBranch("v" + branch)
				if err != nil {
					return err
				}
				if hasVBranch {
					branch = "v" + branch
				}
			}
		}

		command := []string{"git", "checkout", branch, "--"}
		fallbackCommand := slices.Concat([]string{"git", "checkout"}, force, []string{"-B", branch, "composer/" + branch, "--"})
		resetCommand := []string{"git", "reset", "--hard", reference, "--"}

		ok, err := execute(command)
		if err == nil && !ok {
			ok, err = execute(fallbackCommand)
		}

		if err == nil && ok {
			ok, err = execute(resetCommand)
		}

		if err != nil || ok {
			return err
		}
	}

	command1 := slices.Concat([]string{"git", "checkout"}, force, []string{gitRef, "--"})
	command2 := []string{"git", "reset", "--hard", gitRef, "--"}

	if ok, err := all(command1, command2); err != nil || ok {
		return err
	}

	exceptionExtra := ""

	// reference was not found (prints "fatal: reference is not a tree: $ref")
	if strings.Contains(d.process.GetErrorOutput(), reference) {
		d.io.WriteError("    <warning>"+reference+" is gone (history was rewritten?)</warning>", true, mio.Normal)

		what := "the tag was recreated"
		if p.IsDev() {
			what = "the commit was removed from the branch"
		}

		exceptionExtra = "\nIt looks like the commit hash is not available in the repository, maybe " + what + `? Run "composer update ` + p.PrettyName() + `" to resolve this.`
	}

	command := strings.Join(command1, " ") + " && " + strings.Join(command2, " ")

	return &util.RuntimeError{Message: util.SanitizeURL("Failed to execute " + command + "\n\n" + d.process.GetErrorOutput() + exceptionExtra)}
}

func (d *GitDownloader) updateOriginURL(path, url string) error {
	if _, err := d.execute([]string{"git", "remote", "set-url", "origin", "--", url}, nil, path); err != nil {
		return err
	}

	return d.setPushURL(path, url)
}

func (d *GitDownloader) setPushURL(path, url string) error {
	// set push url for github projects
	match, err := php.PregMatch(`{^(?:https?|git)://`+vcsutil.GetGitHubDomainsRegex(d.config)+`/([^/]+)/([^/]+?)(?:\.git)?$}`, url)
	if err != nil || match == nil {
		return err
	}

	pushURL := "git@" + match.Get(1) + ":" + match.Get(2) + "/" + match.Get(3) + ".git"
	if !inArrayStrict("ssh", d.config.Get("github-protocols")) {
		pushURL = "https://" + match.Get(1) + "/" + match.Get(2) + "/" + match.Get(3) + ".git"
	}

	_, err = d.execute([]string{"git", "remote", "set-url", "--push", "origin", "--", pushURL}, nil, path)

	return err
}

// inArrayStrict is in_array($needle, $list, true) for a config list.
func inArrayStrict(needle string, list any) bool {
	a, ok := list.(*php.Array)
	if !ok {
		return false
	}

	for _, v := range a.All() {
		if s, ok := v.(string); ok && s == needle {
			return true
		}
	}

	return false
}

func (d *GitDownloader) commitLogs(fromReference, toReference, path string) (string, error) {
	path = d.normalizePath(path)

	flags, err := vcsutil.GetNoShowSignatureFlags(d.process)
	if err != nil {
		return "", err
	}

	command, err := vcsutil.BuildRevListCommand(d.process, append([]string{"--format=%h - %an: %s", fromReference + ".." + toReference}, flags...))
	if err != nil {
		return "", err
	}

	var output string
	if err := d.mustExecute(command, &output, path); err != nil {
		return "", err
	}

	return vcsutil.ParseRevListOutput(output, d.process)
}

func (d *GitDownloader) discardChanges(path string) error {
	path = d.normalizePath(path)

	for _, command := range [][]string{{"git", "clean", "-df"}, {"git", "reset", "--hard"}} {
		var output string

		code, err := d.execute(command, &output, path)
		if err != nil {
			return err
		}

		if code != 0 {
			return &util.RuntimeError{Message: "Could not reset changes\n\n:" + output}
		}
	}

	d.mu.Lock()
	d.hasDiscardedChanges[path] = true
	d.mu.Unlock()

	return nil
}

func (d *GitDownloader) stashChanges(path string) error {
	path = d.normalizePath(path)

	var output string

	code, err := d.execute([]string{"git", "stash", "--include-untracked"}, &output, path)
	if err != nil {
		return err
	}

	if code != 0 {
		return &util.RuntimeError{Message: "Could not stash changes\n\n:" + output}
	}

	d.mu.Lock()
	d.hasStashedChanges[path] = true
	d.mu.Unlock()

	return nil
}

func (d *GitDownloader) viewDiff(path string) error {
	path = d.normalizePath(path)

	var output string

	code, err := d.execute([]string{"git", "diff", "HEAD"}, &output, path)
	if err != nil {
		return err
	}

	if code != 0 {
		return &util.RuntimeError{Message: "Could not view diff\n\n:" + output}
	}

	d.io.WriteError(output, true, mio.Normal)

	return nil
}

// normalizePath is normalizePath(): on Windows, the path with its existing
// part resolved by realpath().
func (d *GitDownloader) normalizePath(path string) string {
	if !util.IsWindows() || path == "" {
		return path
	}

	basePath := path

	var removed []string

	for !isDir(basePath) && basePath != `\` {
		removed = append([]string{php.Basename(basePath, "")}, removed...)
		basePath = util.Dirname(basePath)
	}

	if basePath == `\` {
		return path
	}

	real, _ := util.RealpathOK(basePath)

	return strings.TrimRight(real+"/"+strings.Join(removed, "/"), "/")
}

func (d *GitDownloader) hasMetadataRepository(path string) bool {
	return isDir(d.normalizePath(path) + "/.git")
}

// shortHash is getShortHash(). The pattern is anchored and fixed-length,
// so Preg::isMatch cannot throw.
func (d *GitDownloader) shortHash(reference string) string {
	if ok, _ := shortHashable.IsMatch(reference); !d.io.IsVerbose() && ok {
		return reference[:10]
	}

	return reference
}
