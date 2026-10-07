// Ports src/Composer/Repository/Vcs/HgDriver.php.

package vcs

import (
	"time"

	"github.com/stubbedev/maestro/internal/cache"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
	uvcs "github.com/stubbedev/maestro/internal/util/vcs"
)

// HgDriver ports Composer\Repository\Vcs\HgDriver: a Mercurial
// repository read from a local clone, or from a clone in cache-vcs-dir.
type HgDriver struct {
	vcsDriver
	// tags and branches are nil until loaded.
	tags, branches *php.Array
	// rootIdentifier is "" until computed.
	rootIdentifier string
	repoDir        string
}

func newHgDriver(repoConfig *php.Array, deps Deps) Driver { return NewHgDriver(repoConfig, deps) }

// NewHgDriver is new HgDriver($repoConfig, $io, $config, $httpDownloader,
// $process).
func NewHgDriver(repoConfig *php.Array, deps Deps) *HgDriver {
	d := &HgDriver{}
	d.init(d, repoConfig, deps)

	return d
}

// Class returns the PHP class name.
func (d *HgDriver) Class() string { return hgDriverType.Class }

var alnumOnly = php.MustCompile(`{[^a-z0-9]}i`)

// Initialize ports HgDriver::initialize.
func (d *HgDriver) Initialize() error {
	if util.IsLocalPath(d.url) {
		d.repoDir = d.url
	} else if err := d.updateClone(); err != nil {
		return err
	}

	if _, err := d.Tags(); err != nil {
		return err
	}

	_, err := d.Branches()

	return err
}

// updateClone is initialize()'s clone or pull of a remote repository.
func (d *HgDriver) updateClone() error {
	cacheDir := php.ToString(d.config.Get("cache-vcs-dir"))
	if !cache.IsUsable(cacheDir) {
		return &util.RuntimeError{Message: "HgDriver requires a usable cache directory, and it looks like you set it to be disabled"}
	}

	safeURL, err := util.SanitizeURLChecked(d.url)
	if err != nil {
		return err
	}
	d.repoDir = cacheDir + "/" + replaceInfallible(alnumOnly, "-", safeURL) + "/"

	fs := util.NewFilesystem(nil)
	if err := util.EnsureDirectoryExists(cacheDir); err != nil {
		return err
	}

	if !util.IsWritable(php.Dirname(d.repoDir)) {
		return &util.RuntimeError{Message: "Can not clone " + util.SanitizeURL(d.url) + ` to access package information. The "` + cacheDir + `" directory is not writable by the current user.`}
	}

	// Ensure we are allowed to use this URL by config
	if err := d.config.ProhibitURLByConfig(d.url, d.io, nil); err != nil {
		return err
	}

	hgUtils := uvcs.NewHg(d.io, d.config, d.process)

	// update the repo if it is a valid hg repository
	if isDir(d.repoDir) {
		var output string

		code, err := d.process.Execute(util.Cmd("hg", "summary"), &output, d.repoDir)
		if err != nil {
			return err
		}

		if code == 0 {
			code, err := d.process.Execute(util.Cmd("hg", "pull"), &output, d.repoDir)
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
	if _, err := fs.RemoveDirectory(d.repoDir); err != nil {
		return err
	}

	repoDir := d.repoDir

	return hgUtils.RunCommand(func(url string) util.Command {
		return util.Cmd("hg", "clone", "--noupdate", "--", url, repoDir)
	}, d.url, "")
}

// RootIdentifier ports HgDriver::getRootIdentifier.
func (d *HgDriver) RootIdentifier() (string, error) {
	if d.rootIdentifier == "" {
		var output string
		if _, err := d.process.Execute(util.Cmd("hg", "tip", "--template", "{node}"), &output, d.repoDir); err != nil {
			return "", err
		}

		d.rootIdentifier = util.SplitLines(output)[0]
	}

	return d.rootIdentifier, nil
}

// URL ports HgDriver::getUrl.
func (d *HgDriver) URL() string { return d.url }

// Source ports HgDriver::getSource.
func (d *HgDriver) Source(identifier string) *php.Array {
	return php.ArrayOf("type", "hg", "url", d.URL(), "reference", identifier)
}

// Dist ports HgDriver::getDist.
func (d *HgDriver) Dist(string) *php.Array { return nil }

// FileContent ports HgDriver::getFileContent.
func (d *HgDriver) FileContent(file, identifier string) (string, bool, error) {
	if err := invalidIdentifier("hg", identifier); err != nil {
		return "", false, err
	}

	var content string
	if _, err := d.process.Execute(util.Cmd("hg", "cat", "-r", identifier, "--", file), &content, d.repoDir); err != nil {
		return "", false, err
	}

	if !php.ToBool(php.Trim(content)) {
		return "", false, nil
	}

	return content, true, nil
}

// ChangeDate ports HgDriver::getChangeDate.
func (d *HgDriver) ChangeDate(identifier string) (time.Time, bool, error) {
	if err := invalidIdentifier("hg", identifier); err != nil {
		return time.Time{}, false, err
	}

	var output string
	if _, err := d.process.Execute(util.Cmd("hg", "log", "--template", "{date|rfc3339date}", "-r", identifier), &output, d.repoDir); err != nil {
		return time.Time{}, false, err
	}

	date, err := parseDate(php.Trim(output))

	return date, err == nil, err
}

var hgTag = php.MustCompile(`(^([^\s]+)\s+\d+:(.*)$)`)

// Tags ports HgDriver::getTags.
func (d *HgDriver) Tags() (*php.Array, error) {
	if d.tags != nil {
		return d.tags, nil
	}

	var output string
	if _, err := d.process.Execute(util.Cmd("hg", "tags"), &output, d.repoDir); err != nil {
		return nil, err
	}

	tags := php.NewArray()

	for _, tag := range util.SplitLines(output) {
		if !php.ToBool(tag) {
			continue
		}

		m, err := match(hgTag, tag)
		if err != nil {
			return nil, err
		}
		if m != nil {
			tags.Set(m.Get(1), m.Get(2))
		}
	}

	tags.Delete("tip")
	d.tags = tags

	return tags, nil
}

var (
	hgBranch   = php.MustCompile(`(^([^\s]+)\s+\d+:([a-f0-9]+))`)
	hgBookmark = php.MustCompile(`(^(?:[\s*]*)([^\s]+)\s+\d+:(.*)$)`)
)

// Branches ports HgDriver::getBranches.
func (d *HgDriver) Branches() (*php.Array, error) {
	if d.branches != nil {
		return d.branches, nil
	}

	branches, err := d.refs([]string{"hg", "branches"}, hgBranch)
	if err != nil {
		return nil, err
	}

	bookmarks, err := d.refs([]string{"hg", "bookmarks"}, hgBookmark)
	if err != nil {
		return nil, err
	}

	// Branches will have preference over bookmarks
	d.branches = php.ArrayMerge(bookmarks, branches)

	return d.branches, nil
}

// refs runs command and maps the names to the identifiers of the lines re
// matches, skipping names starting with "-".
func (d *HgDriver) refs(command []string, re *php.Regexp) (*php.Array, error) {
	var output string
	if _, err := d.process.Execute(util.Cmd(command...), &output, d.repoDir); err != nil {
		return nil, err
	}

	refs := php.NewArray()

	for _, line := range util.SplitLines(output) {
		if !php.ToBool(line) {
			continue
		}

		m, err := match(re, line)
		if err != nil {
			return nil, err
		}
		if m != nil && m.Get(1)[0] != '-' {
			refs.Set(m.Get(1), m.Get(2))
		}
	}

	return refs, nil
}

var hgURL = php.MustCompile(`#(^(?:https?|ssh)://(?:[^@]+@)?bitbucket.org|https://(?:.*?)\.kilnhg.com)#i`)

// hgSupports ports HgDriver::supports.
func hgSupports(deps Deps, url string, deep bool) (bool, error) {
	if ok, err := matches(hgURL, url); err != nil || ok {
		return ok, err
	}

	var output string

	// local filesystem
	if util.IsLocalPath(url) {
		url = util.GetPlatformPath(url)
		if !isDir(url) {
			return false, nil
		}

		// check whether there is a hg repo in that path
		if code, err := deps.Process.Execute(util.Cmd("hg", "summary"), &output, url); err != nil || code == 0 {
			return err == nil, err
		}
	}

	if !deep {
		return false, nil
	}

	code, err := deps.Process.Execute(util.Cmd("hg", "identify", "--", url), &output, "")

	return err == nil && code == 0, err
}
