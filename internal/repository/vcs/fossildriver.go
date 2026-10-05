// Ports src/Composer/Repository/Vcs/FossilDriver.php.

package vcs

import (
	"os"
	"strings"
	"time"

	"github.com/stubbedev/maestro/internal/cache"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
)

// FossilDriver ports Composer\Repository\Vcs\FossilDriver: a Fossil
// repository read from a local checkout, or from a clone in the caches.
type FossilDriver struct {
	vcsDriver
	// tags and branches are nil until loaded.
	tags, branches *php.Array
	// repoFile is "" for null.
	repoFile    string
	checkoutDir string
}

func newFossilDriver(repoConfig *php.Array, deps Deps) Driver {
	return NewFossilDriver(repoConfig, deps)
}

// NewFossilDriver is new FossilDriver($repoConfig, $io, $config,
// $httpDownloader, $process).
func NewFossilDriver(repoConfig *php.Array, deps Deps) *FossilDriver {
	d := &FossilDriver{}
	d.init(d, repoConfig, deps)

	return d
}

// Class returns the PHP class name.
func (d *FossilDriver) Class() string { return fossilDriverType.Class }

// Initialize ports FossilDriver::initialize.
func (d *FossilDriver) Initialize() error {
	// Make sure fossil is installed and reachable.
	if err := d.checkFossil(); err != nil {
		return err
	}

	// Ensure we are allowed to use this URL by config.
	if err := d.config.ProhibitURLByConfig(d.url, d.io, nil); err != nil {
		return err
	}

	// Only if url points to a locally accessible directory, assume it's the checkout directory.
	// Otherwise, it should be something fossil can clone from.
	if util.IsLocalPath(d.url) && isDir(d.url) {
		d.checkoutDir = d.url
	} else {
		repoDir := php.ToString(d.config.Get("cache-repo-dir"))
		vcsDir := php.ToString(d.config.Get("cache-vcs-dir"))

		if !cache.IsUsable(repoDir) || !cache.IsUsable(vcsDir) {
			return &util.RuntimeError{Message: "FossilDriver requires a usable cache directory, and it looks like you set it to be disabled"}
		}

		localName := replace(alnumOnly, "-", d.url)
		d.repoFile = repoDir + "/" + localName + ".fossil"
		d.checkoutDir = vcsDir + "/" + localName + "/"

		if err := d.updateLocalRepo(); err != nil {
			return err
		}
	}

	if _, err := d.Tags(); err != nil {
		return err
	}

	_, err := d.Branches()

	return err
}

// checkFossil ports checkFossil(): fossil must be callable.
func (d *FossilDriver) checkFossil() error {
	var ignoredOutput string

	code, err := d.process.Execute(util.Cmd("fossil", "version"), &ignoredOutput, "")
	if err != nil {
		return err
	}

	if code != 0 {
		return &util.RuntimeError{Message: "fossil was not found, check that it is installed and in your PATH env.\n\n" + d.process.GetErrorOutput()}
	}

	return nil
}

// updateLocalRepo ports updateLocalRepo(): clone or update the local
// fossil repository.
func (d *FossilDriver) updateLocalRepo() error {
	fs := util.NewFilesystem(nil)
	if err := util.EnsureDirectoryExists(d.checkoutDir); err != nil {
		return err
	}

	if !util.IsWritable(util.Dirname(d.checkoutDir)) {
		return &util.RuntimeError{Message: "Can not clone " + util.SanitizeURL(d.url) + ` to access package information. The "` + d.checkoutDir + `" directory is not writable by the current user.`}
	}

	var output string

	// update the repo if it is a valid fossil repository
	if isFile(d.repoFile) && isDir(d.checkoutDir) {
		code, err := d.process.Execute(util.Cmd("fossil", "info"), &output, d.checkoutDir)
		if err != nil {
			return err
		}

		if code == 0 {
			code, err := d.process.Execute(util.Cmd("fossil", "pull"), &output, d.checkoutDir)
			if err != nil {
				return err
			}

			if code != 0 {
				d.io.WriteError("<error>Failed to update "+util.SanitizeURL(d.url)+", package information from this repository may be outdated ("+d.process.GetErrorOutput()+")</error>", true, io.Normal)
			}

			return nil
		}
	}

	// clean up directory and do a fresh clone into it
	if _, err := fs.RemoveDirectory(d.checkoutDir); err != nil {
		return err
	}

	if _, err := fs.Remove(d.repoFile); err != nil {
		return err
	}

	if err := util.EnsureDirectoryExists(d.checkoutDir); err != nil {
		return err
	}

	code, err := d.process.Execute(util.Cmd("fossil", "clone", "--", d.url, d.repoFile), &output, "")
	if err != nil {
		return err
	}

	if code != 0 {
		return &util.RuntimeError{Message: "Failed to clone " + util.SanitizeURL(d.url) + " to repository " + d.repoFile + "\n\n" + d.process.GetErrorOutput()}
	}

	code, err = d.process.Execute(util.Cmd("fossil", "open", "--nested", "--", d.repoFile), &output, d.checkoutDir)
	if err != nil {
		return err
	}

	if code != 0 {
		return &util.RuntimeError{Message: "Failed to open repository " + d.repoFile + " in " + d.checkoutDir + "\n\n" + d.process.GetErrorOutput()}
	}

	return nil
}

// RootIdentifier ports FossilDriver::getRootIdentifier.
func (d *FossilDriver) RootIdentifier() (string, error) { return "trunk", nil }

// URL ports FossilDriver::getUrl.
func (d *FossilDriver) URL() string { return d.url }

// Source ports FossilDriver::getSource.
func (d *FossilDriver) Source(identifier string) *php.Array {
	return php.ArrayOf("type", "fossil", "url", d.URL(), "reference", identifier)
}

// Dist ports FossilDriver::getDist.
func (d *FossilDriver) Dist(string) *php.Array { return nil }

// FileContent ports FossilDriver::getFileContent.
func (d *FossilDriver) FileContent(file, identifier string) (string, bool, error) {
	if err := invalidIdentifier("fossil", identifier); err != nil {
		return "", false, err
	}

	var content string
	if _, err := d.process.Execute(util.Cmd("fossil", "cat", "-r", identifier, "--", file), &content, d.checkoutDir); err != nil {
		return "", false, err
	}

	if php.Trim(content) == "" {
		return "", false, nil
	}

	return content, true, nil
}

// ChangeDate ports FossilDriver::getChangeDate.
func (d *FossilDriver) ChangeDate(string) (time.Time, bool, error) {
	var output string
	if _, err := d.process.Execute(util.Cmd("fossil", "finfo", "-b", "-n", "1", "composer.json"), &output, d.checkoutDir); err != nil {
		return time.Time{}, false, err
	}

	// [, $date] = explode(' ', trim($output), 3);
	fields := strings.SplitN(php.Trim(output), " ", 3)
	date := ""

	if len(fields) > 1 {
		date = fields[1]
	}

	t, err := parseDate(date)

	return t, err == nil, err
}

// Tags ports FossilDriver::getTags.
func (d *FossilDriver) Tags() (*php.Array, error) {
	if d.tags == nil {
		tags, err := d.list([]string{"fossil", "tag", "list"}, func(line string) string { return line })
		if err != nil {
			return nil, err
		}

		d.tags = tags
	}

	return d.tags, nil
}

var leadingStar = php.MustCompile(`/^\*/`)

// Branches ports FossilDriver::getBranches.
func (d *FossilDriver) Branches() (*php.Array, error) {
	if d.branches == nil {
		branches, err := d.list([]string{"fossil", "branch", "list"}, func(line string) string {
			return php.Trim(replace(leadingStar, "", php.Trim(line)))
		})
		if err != nil {
			return nil, err
		}

		d.branches = branches
	}

	return d.branches, nil
}

// list runs command and maps each output line, through name, to itself.
func (d *FossilDriver) list(command []string, name func(string) string) (*php.Array, error) {
	var output string
	if _, err := d.process.Execute(util.Cmd(command...), &output, d.checkoutDir); err != nil {
		return nil, err
	}

	refs := php.NewArray()

	for _, line := range util.SplitLines(output) {
		n := name(line)
		refs.Set(n, n)
	}

	return refs, nil
}

var (
	fossilHostURL = php.MustCompile(`#(^(?:https?|ssh)://(?:[^@]@)?(?:chiselapp\.com|fossil\.))#i`)
	fossilPathURL = php.MustCompile(`!/fossil/|\.fossil!`)
)

// fossilSupports ports FossilDriver::supports.
func fossilSupports(deps Deps, url string, _ bool) (bool, error) {
	if matches(fossilHostURL, url) || matches(fossilPathURL, url) {
		return true, nil
	}

	// local filesystem
	if util.IsLocalPath(url) {
		url = util.GetPlatformPath(url)
		if !isDir(url) {
			return false, nil
		}

		// check whether there is a fossil repo in that path
		var output string

		code, err := deps.Process.Execute(util.Cmd("fossil", "info"), &output, url)

		return err == nil && code == 0, err
	}

	return false, nil
}

// isFile is is_file($path).
func isFile(path string) bool {
	fi, err := os.Stat(path)

	return err == nil && fi.Mode().IsRegular()
}
