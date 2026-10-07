// Ports src/Composer/Downloader/SvnDownloader.php.

package vcs

import (
	"strconv"
	"strings"

	mio "github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/semver"
	"github.com/stubbedev/maestro/internal/util"
	vcsutil "github.com/stubbedev/maestro/internal/util/vcs"
)

// SvnDownloader ports Composer\Downloader\SvnDownloader.
type SvnDownloader struct {
	vcsDownloader
	// cacheCredentials is $cacheCredentials, guarded by vcsDownloader.mu.
	cacheCredentials bool
}

// NewSvnDownloader is new SvnDownloader($io, $config, $process, $fs).
func NewSvnDownloader(deps Deps) *SvnDownloader {
	d := &SvnDownloader{cacheCredentials: true}
	d.vcsDownloader = newVcsDownloader(deps, d, `Composer\Downloader\SvnDownloader`)

	return d
}

var (
	svnModified = php.MustCompile(`{^ *[^X ] +}m`)
	svnRevision = php.MustCompile(`{@(\d+)$}`)
	svnURL      = php.MustCompile(`#<url>(.*)</url>#`)
	svnRevOnly  = php.MustCompile(`{.*@(\d+)$}`)
)

func (d *SvnDownloader) doDownload(_ pkg.PackageInterface, _, url string, _ pkg.PackageInterface) error {
	vcsutil.SvnCleanEnv()

	if _, found, err := vcsutil.NewSvn(url, d.io, d.config, d.process).BinaryVersion(); err != nil {
		return err
	} else if !found {
		return &util.RuntimeError{Message: "svn was not found in your PATH, skipping source download"}
	}

	return nil
}

func (d *SvnDownloader) doInstall(p pkg.PackageInterface, path, url string) error {
	vcsutil.SvnCleanEnv()

	ref := p.SourceReference().S

	if repoConfig, ok := vcsRepoConfig(p); ok {
		if v, ok := repoConfig.Get("svn-cache-credentials"); ok {
			d.mu.Lock()
			d.cacheCredentials = php.ToBool(v)
			d.mu.Unlock()
		}
	}

	d.io.WriteError(" Checking out "+ref, true, mio.Normal)
	return d.svnExecute(p, url, []string{"svn", "co"}, url+"/"+ref, "", path)
}

func (d *SvnDownloader) doUpdate(_, target pkg.PackageInterface, path, url string) error {
	vcsutil.SvnCleanEnv()

	ref := target.SourceReference().S

	if !d.hasMetadataRepository(path) {
		return &util.RuntimeError{Message: "The .svn directory is missing from " + path + ", see https://getcomposer.org/commit-deps for more information"}
	}

	binaryVersion, _, err := vcsutil.NewSvn(url, d.io, d.config, d.process).BinaryVersion()
	if err != nil {
		return err
	}

	command := []string{"svn", "switch"}
	if ok, _ := semver.VersionCompareOp(binaryVersion, "1.7.0", ">="); ok {
		command = append(command, "--ignore-ancestry")
	}

	d.io.WriteError(" Checking out "+ref, true, mio.Normal)
	return d.svnExecute(target, url, command, url+"/"+ref, path, "")
}

// LocalChanges is getLocalChanges().
func (d *SvnDownloader) LocalChanges(_ pkg.PackageInterface, path string) (pkg.NullString, error) {
	if !d.hasMetadataRepository(path) {
		return pkg.NullString{}, nil
	}

	var output string
	if _, err := d.execute([]string{"svn", "status", "--ignore-externals"}, &output, path); err != nil {
		return pkg.NullString{}, err
	}

	if ok, err := svnModified.IsMatch(output); err != nil || !ok {
		return pkg.NullString{}, err
	}

	return pkg.Str(output), nil
}

func (d *SvnDownloader) newSvn(baseURL string) *vcsutil.Svn {
	svn := vcsutil.NewSvn(baseURL, d.io, d.config, d.process)

	d.mu.Lock()
	svn.SetCacheCredentials(d.cacheCredentials)
	d.mu.Unlock()

	return svn
}

// svnExecute is execute(): runs an SVN command, fixing up the process
// with credentials if necessary. Empty cwd and path are null. The output
// is unused.
func (d *SvnDownloader) svnExecute(p pkg.PackageInterface, baseURL string, command []string, url, cwd, path string) error {
	_, err := d.newSvn(baseURL).Execute(command, url, cwd, path, d.io.IsVerbose())
	if err != nil && phperr.InstanceOf(err, "RuntimeException") {
		return &util.RuntimeError{Message: p.PrettyName() + " could not be downloaded, " + err.Error()}
	}

	return err
}

func (d *SvnDownloader) cleanChanges(p pkg.PackageInterface, path string, update bool) error {
	changes, err := d.LocalChanges(p, path)
	if err != nil || !changes.Valid {
		return err
	}

	if !d.io.IsInteractive() {
		if d.config.Get("discard-changes") == true {
			return d.discardChanges(path)
		}

		return d.vcsDownloader.cleanChanges(p, path, update)
	}

	lines, err := changeLines(changes.S)
	if err != nil {
		return err
	}

	countChanges := len(lines)
	d.io.WriteError("    <error>"+p.PrettyName()+" has modified file"+plural(countChanges)+":</error>", true, mio.Normal)
	d.io.WriteErrorMessages(lines[:min(10, countChanges)], true, mio.Normal)

	if countChanges > 10 {
		remainingChanges := countChanges - 10
		d.io.WriteError("    <info>"+strconv.Itoa(remainingChanges)+" more file"+plural(remainingChanges)+` modified, choose "v" to view the full list</info>`, true, mio.Normal)
	}

	action := "uninstall"
	if update {
		action = "update"
	}

	for {
		answer, err := d.io.Ask("    <info>Discard changes [y,n,v,?]?</info> ", "?")
		if err != nil {
			return err
		}

		switch askString(answer) {
		case "y":
			return d.discardChanges(path)
		case "n":
			return &util.RuntimeError{Message: "Update aborted"}
		case "v":
			d.io.WriteErrorMessages(lines, true, mio.Normal)
		default:
			d.io.WriteErrorMessages([]string{
				"    y - discard changes and apply the " + action,
				"    n - abort the " + action + " and let you manually clean things up",
				"    v - view modified files",
				"    ? - print help",
			}, true, mio.Normal)
		}
	}
}

func plural(n int) string {
	if n == 1 {
		return ""
	}

	return "s"
}

func (d *SvnDownloader) commitLogs(fromReference, toReference, path string) (string, error) {
	fromOK, err := svnRevision.IsMatch(fromReference)
	if err != nil {
		return "", err
	}
	toOK := false
	if fromOK {
		if toOK, err = svnRevision.IsMatch(toReference); err != nil {
			return "", err
		}
	}

	if !fromOK || !toOK {
		return "Could not retrieve changes between " + fromReference + " and " + toReference + " due to missing revision information", nil
	}

	// retrieve the svn base url from the checkout folder
	var output string
	if err := d.mustExecute([]string{"svn", "info", "--non-interactive", "--xml", "--", path}, &output, path); err != nil {
		return "", err
	}

	matches, err := svnURL.MatchStrictGroups(output)
	if err != nil {
		return "", err
	}

	if matches == nil {
		return "", &util.RuntimeError{Message: "Unable to determine svn url for path " + path}
	}

	baseURL := matches.Get(1)

	// strip paths from references and only keep the actual revision
	fromRevision, _, err := svnRevOnly.Replace(fromReference, "$1", -1)
	if err != nil {
		return "", err
	}

	toRevision, _, err := svnRevOnly.Replace(toReference, "$1", -1)
	if err != nil {
		return "", err
	}

	command := []string{"svn", "log", "-r", fromRevision + ":" + toRevision, "--incremental"}

	logs, err := d.newSvn(baseURL).ExecuteLocal(command, path, "", d.io.IsVerbose())
	if err != nil && phperr.InstanceOf(err, "RuntimeException") {
		return "", &util.RuntimeError{Message: "Failed to execute " + strings.Join(command, " ") + "\n\n" + err.Error()}
	}

	return logs, err
}

func (d *SvnDownloader) discardChanges(path string) error {
	code, err := d.execute([]string{"svn", "revert", "-R", "."}, nil, path)
	if err != nil {
		return err
	}

	if code != 0 {
		return &util.RuntimeError{Message: "Could not reset changes\n\n:" + d.process.GetErrorOutput()}
	}

	return nil
}

func (d *SvnDownloader) hasMetadataRepository(path string) bool {
	return isDir(path + "/.svn")
}
