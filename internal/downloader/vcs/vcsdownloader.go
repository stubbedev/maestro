// Ports src/Composer/Downloader/VcsDownloader.php.

package vcs

import (
	"strings"
	"sync"

	"github.com/stubbedev/maestro/internal/downloader"
	mio "github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/dumper"
	"github.com/stubbedev/maestro/internal/pkg/version"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/resolver/operation"
	"github.com/stubbedev/maestro/internal/store"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/http"
	vcsutil "github.com/stubbedev/maestro/internal/util/vcs"
)

// Process is the part of Composer\Util\ProcessExecutor the VCS downloaders
// use: what the VCS utilities run commands with, plus executeAsync for the
// version guesser of VcsReference. *util.ProcessExecutor and
// *processmock.Mock implement it.
type Process = repository.Process

// Filesystem is the part of Composer\Util\Filesystem the VCS downloaders
// use. *util.Filesystem implements it.
type Filesystem interface {
	vcsutil.Filesystem
	EmptyDirectory(dir string, ensureDirectoryExists bool) error
	RemoveDirectoryAsync(directory string) (*util.Promise[bool], error)
}

// Deps are the constructor arguments of the VCS downloaders ($io, $config,
// $process, $fs), plus maestro's package store. IO and Config are required
// (adapt *config.Config with ForHTTP); a nil Process is a new
// ProcessExecutor on IO, a nil Filesystem one running commands through
// Process.
type Deps struct {
	IO         mio.IO
	Config     http.Config
	Process    Process
	Filesystem Filesystem
	// Store keeps the checkouts the git downloader clones from the mirror
	// cache, to import them again instead of cloning (nil: never).
	Store *store.Store
}

// Register adds the VCS downloaders to dm under their types, in the order
// Factory::createDownloadManager registers them (before the archive
// downloaders): git, svn, fossil, hg, perforce. They share deps' process
// and filesystem.
func Register(dm *downloader.DownloadManager, deps Deps) {
	deps = deps.withDefaults()
	dm.SetDownloader("git", NewGitDownloader(deps))
	dm.SetDownloader("svn", NewSvnDownloader(deps))
	dm.SetDownloader("fossil", NewFossilDownloader(deps))
	dm.SetDownloader("hg", NewHgDownloader(deps))
	dm.SetDownloader("perforce", NewPerforceDownloader(deps))
}

// withDefaults fills in the process and filesystem as VcsDownloader's
// constructor does.
func (d Deps) withDefaults() Deps {
	if d.Process == nil {
		d.Process = http.NewProcessExecutor(d.IO)
	}

	if d.Filesystem == nil {
		executor, _ := d.Process.(*util.ProcessExecutor)
		d.Filesystem = util.NewFilesystem(executor)
	}

	return d
}

// impl are the abstract and overridable methods of VcsDownloader, which
// its own methods call through $this.
type impl interface {
	doDownload(p pkg.PackageInterface, path, url string, prev pkg.PackageInterface) error
	doInstall(p pkg.PackageInterface, path, url string) error
	doUpdate(initial, target pkg.PackageInterface, path, url string) error
	commitLogs(fromReference, toReference, path string) (string, error)
	hasMetadataRepository(path string) bool
	LocalChanges(p pkg.PackageInterface, path string) (pkg.NullString, error)
	cleanChanges(p pkg.PackageInterface, path string, update bool) error
	reapplyChanges(path string) error
}

// vcsDownloader ports the abstract Composer\Downloader\VcsDownloader. The
// downloaders work synchronously, as Composer's do: their promises are
// settled when returned, except Remove's.
type vcsDownloader struct {
	io         mio.IO
	config     http.Config
	process    Process
	filesystem Filesystem
	self       impl
	class      string

	mu                sync.Mutex
	hasCleanedChanges map[string]bool
}

func newVcsDownloader(deps Deps, self impl, class string) vcsDownloader {
	deps = deps.withDefaults()

	return vcsDownloader{
		io:                deps.IO,
		config:            deps.Config,
		process:           deps.Process,
		filesystem:        deps.Filesystem,
		self:              self,
		class:             class,
		hasCleanedChanges: map[string]bool{},
	}
}

var (
	_ downloader.Downloader           = (*GitDownloader)(nil)
	_ downloader.ChangeReporter       = (*GitDownloader)(nil)
	_ downloader.VcsCapableDownloader = (*GitDownloader)(nil)
	_ downloader.DvcsDownloader       = (*GitDownloader)(nil)
	_ downloader.Classer              = (*GitDownloader)(nil)
)

// Class returns the PHP class name.
func (d *vcsDownloader) Class() string { return d.class }

// InstallationSource is getInstallationSource().
func (d *vcsDownloader) InstallationSource() string { return "source" }

// missingReference is the InvalidArgumentException of a package without
// a reference.
func missingReference(p pkg.PackageInterface) error {
	return &util.InvalidArgumentError{Message: "Package " + p.PrettyName() + " is missing reference information"}
}

// Download is download().
func (d *vcsDownloader) Download(p pkg.PackageInterface, path string, prev pkg.PackageInterface) (*downloader.Promise, error) {
	if !php.ToBool(p.SourceReference().S) {
		return nil, missingReference(p)
	}

	urls := prepareURLs(p.SourceURLs())
	if r := d.eachURL(urls, func(url string) error { return d.self.doDownload(p, path, url, prev) }); r.failedOnLast {
		return nil, r.err
	}

	return util.Resolved(""), nil
}

// Prepare is prepare().
func (d *vcsDownloader) Prepare(typ string, p pkg.PackageInterface, path string, prev pkg.PackageInterface) (*downloader.Promise, error) {
	switch typ {
	case "update":
		if err := d.self.cleanChanges(prev, path, true); err != nil {
			return nil, err
		}

		d.mu.Lock()
		d.hasCleanedChanges[prev.UniqueName()] = true
		d.mu.Unlock()
	case "install":
		if err := d.filesystem.EmptyDirectory(path, true); err != nil {
			return nil, err
		}
	case "uninstall":
		if err := d.self.cleanChanges(p, path, false); err != nil {
			return nil, err
		}
	}

	return util.Resolved(""), nil
}

// Cleanup is cleanup().
func (d *vcsDownloader) Cleanup(typ string, _ pkg.PackageInterface, path string, prev pkg.PackageInterface) (*downloader.Promise, error) {
	if typ == "update" {
		d.mu.Lock()
		cleaned := d.hasCleanedChanges[prev.UniqueName()]
		d.mu.Unlock()

		if cleaned {
			if err := d.self.reapplyChanges(path); err != nil {
				return nil, err
			}

			d.mu.Lock()
			delete(d.hasCleanedChanges, prev.UniqueName())
			d.mu.Unlock()
		}
	}

	return util.Resolved(""), nil
}

// Install is install().
func (d *vcsDownloader) Install(p pkg.PackageInterface, path string) (*downloader.Promise, error) {
	if !php.ToBool(p.SourceReference().S) {
		return nil, missingReference(p)
	}

	d.io.WriteError("  - "+operation.FormatInstall(p, false)+": ", false, mio.Normal)

	urls := prepareURLs(p.SourceURLs())
	if r := d.eachURL(urls, func(url string) error { return d.self.doInstall(p, path, url) }); r.failedOnLast {
		return nil, r.err
	}

	return util.Resolved(""), nil
}

// Update is update().
func (d *vcsDownloader) Update(initial, target pkg.PackageInterface, path string) (*downloader.Promise, error) {
	if !php.ToBool(target.SourceReference().S) {
		return nil, missingReference(target)
	}

	msg, err := operation.FormatUpdate(initial, target)
	if err != nil {
		return nil, err
	}

	d.io.WriteError("  - "+msg+": ", false, mio.Normal)

	urls := prepareURLs(target.SourceURLs())
	r := d.eachURL(urls, func(url string) error { return d.self.doUpdate(initial, target, path, url) })

	// print the commit logs if in verbose mode and VCS metadata is present
	// because in case of missing metadata code would trigger another exception
	if r.err == nil && d.io.IsVerbose() && d.self.hasMetadataRepository(path) {
		message := "Pulling in changes:"

		logs, err := d.self.commitLogs(initial.SourceReference().S, target.SourceReference().S, path)
		if err != nil {
			return nil, err
		}

		if php.Trim(logs) == "" {
			message = "Rolling back changes:"

			if logs, err = d.self.commitLogs(target.SourceReference().S, initial.SourceReference().S, path); err != nil {
				return nil, err
			}
		}

		if php.Trim(logs) != "" {
			lines := strings.Split(logs, "\n")
			for i, line := range lines {
				lines[i] = "      " + line
			}

			// escape angle brackets for proper output in the console
			logs = strings.ReplaceAll(strings.Join(lines, "\n"), "<", `\<`)

			d.io.WriteError("    "+message, true, mio.Normal)
			d.io.WriteError(logs, true, mio.Normal)
		}
	}

	if r.remaining == 0 && r.err != nil {
		return nil, r.err
	}

	return util.Resolved(""), nil
}

// Remove is remove().
func (d *vcsDownloader) Remove(p pkg.PackageInterface, path string) (*downloader.Promise, error) {
	d.io.WriteError("  - "+operation.FormatUninstall(p), true, mio.Normal)

	promise, err := d.filesystem.RemoveDirectoryAsync(path)
	if err != nil {
		return nil, err
	}

	return util.Then(promise, func(result bool) (string, error) {
		if !result {
			return "", &util.RuntimeError{Message: "Could not completely delete " + path + ", aborting."}
		}

		return "", nil
	}), nil
}

// VcsReference is getVcsReference(): the commit the version guesser finds
// at path.
func (d *vcsDownloader) VcsReference(p pkg.PackageInterface, path string) (pkg.NullString, error) {
	guesser := version.NewVersionGuesser(repository.NewGuesserProcess(d.process), d.io)

	packageConfig, err := dumper.ArrayDumper{}.Dump(p)
	if err != nil {
		return pkg.NullString{}, err
	}

	packageVersion, err := guesser.GuessVersion(packageConfig, path)
	if err != nil || packageVersion == nil {
		return pkg.NullString{}, err
	}

	return packageVersion.Commit, nil
}

// cleanChanges is VcsDownloader::cleanChanges: the default implementation
// just fails if there are any changes; Git and Svn override it to offer
// stashing or discarding them.
func (d *vcsDownloader) cleanChanges(p pkg.PackageInterface, path string, _ bool) error {
	changes, err := d.self.LocalChanges(p, path)
	if err != nil {
		return err
	}

	if changes.Valid {
		return &util.RuntimeError{Message: "Source directory " + path + " has uncommitted changes."}
	}

	return nil
}

// reapplyChanges is VcsDownloader::reapplyChanges: nothing to reapply.
func (d *vcsDownloader) reapplyChanges(string) error { return nil }

// urlResult is the outcome of eachURL.
type urlResult struct {
	// err is the exception of the last URL tried, nil when it succeeded.
	err error
	// failedOnLast reports that err came from the last URL: download() and
	// install() rethrow it then.
	failedOnLast bool
	// remaining counts the URLs left untried (count($urls)).
	remaining int
}

// eachURL is the `while ($url = array_shift($urls))` loop of download(),
// install() and update(): run tries each URL until one succeeds, reporting
// failures. A falsy URL (realpath() failed) ends the loop.
func (d *vcsDownloader) eachURL(urls []string, run func(url string) error) urlResult {
	var r urlResult

	for len(urls) > 0 {
		url := urls[0]
		urls = urls[1:]

		if !php.ToBool(url) {
			break
		}

		r.err = run(url)
		if r.err == nil {
			break
		}

		if d.io.IsDebug() {
			class, _ := phperr.ClassOf(r.err)
			d.io.WriteError("Failed: ["+class+"] "+r.err.Error(), true, mio.Normal)
		} else if len(urls) > 0 {
			d.io.WriteError("    Failed, trying the next URL", true, mio.Normal)
		}

		if len(urls) == 0 {
			r.failedOnLast = true
		}
	}

	r.remaining = len(urls)

	return r
}

// prepareURLs is prepareUrls(): local paths are resolved with realpath(); a
// path realpath() cannot resolve becomes "" (false).
func prepareURLs(urls []string) []string {
	out := make([]string, len(urls))

	for i, url := range urls {
		out[i] = url
		if !util.IsLocalPath(url) {
			continue
		}

		// realpath() below will not understand
		// url that starts with "file://"
		const fileProtocol = "file://"

		isFileProtocol := strings.HasPrefix(url, fileProtocol)
		if isFileProtocol {
			url = url[len(fileProtocol):]
		}

		// realpath() below will not understand %20 spaces etc.
		if strings.Contains(url, "%") {
			url = php.Rawurldecode(url)
		}

		out[i], _ = php.Realpath(url)

		if isFileProtocol {
			out[i] = fileProtocol + out[i]
		}
	}

	return out
}

// failedToExecute is `throw new \RuntimeException('Failed to execute ' .
// implode(' ', $command) . "\n\n" . $this->process->getErrorOutput())`.
func (d *vcsDownloader) failedToExecute(command []string) error {
	return &util.RuntimeError{Message: "Failed to execute " + strings.Join(command, " ") + "\n\n" + d.process.GetErrorOutput()}
}

// execute runs command, storing its output into output when non-nil.
func (d *vcsDownloader) execute(command []string, output *string, cwd string) (int, error) {
	if output == nil {
		output = new(string)
	}

	return d.process.Execute(util.Cmd(command...), output, cwd)
}

// mustExecute runs command and fails with failedToExecute unless it exits
// with 0.
func (d *vcsDownloader) mustExecute(command []string, output *string, cwd string) error {
	code, err := d.execute(command, output, cwd)
	if err != nil {
		return err
	}

	if code != 0 {
		return d.failedToExecute(command)
	}

	return nil
}

// trimmedOrNull is `$output = trim($output); return strlen($output) > 0 ?
// $output : null;`.
func trimmedOrNull(output string) pkg.NullString {
	return pkg.NonEmpty(php.Trim(output))
}

// changeLines is the `'    '.$elem` list of the modified files of
// cleanChanges.
func changeLines(changes string) ([]string, error) {
	lines, err := splitChanges.Split(changes, -1, 0)
	if err != nil {
		return nil, err
	}

	for i, line := range lines {
		lines[i] = "    " + line
	}

	return lines, nil
}

var splitChanges = php.MustCompile(`{\s*\r?\n\s*}`)

// askString is the answer of IO::ask as a switch subject: non-strings
// match no case.
func askString(answer any) string {
	s, _ := answer.(string)

	return s
}

// vcsRepoConfig returns getRepoConfig() of p's repository when it is a
// VcsRepository (internal/repository/vcs, which this package cannot
// import).
func vcsRepoConfig(p pkg.PackageInterface) (*php.Array, bool) {
	repo, ok := p.Repository().(interface {
		Class() string
		RepoConfig() *php.Array
	})
	if !ok || repo.Class() != `Composer\Repository\VcsRepository` {
		return nil, false
	}

	return repo.RepoConfig(), true
}
