// Ports src/Composer/Downloader/HgDownloader.php.

package vcs

import (
	mio "github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/util"
	vcsutil "github.com/stubbedev/maestro/internal/util/vcs"
)

// HgDownloader ports Composer\Downloader\HgDownloader.
type HgDownloader struct {
	vcsDownloader
}

// NewHgDownloader is new HgDownloader($io, $config, $process, $fs).
func NewHgDownloader(deps Deps) *HgDownloader {
	d := &HgDownloader{}
	d.vcsDownloader = newVcsDownloader(deps, d, `Composer\Downloader\HgDownloader`)

	return d
}

func (d *HgDownloader) doDownload(pkg.PackageInterface, string, string, pkg.PackageInterface) error {
	_, found, err := vcsutil.GetHgVersion(d.process)
	if err != nil {
		return err
	}

	if !found {
		return &util.RuntimeError{Message: "hg was not found in your PATH, skipping source download"}
	}

	return nil
}

func (d *HgDownloader) doInstall(p pkg.PackageInterface, path, url string) error {
	hgUtils := vcsutil.NewHg(d.io, d.config, d.process)

	cloneCommand := func(url string) util.Command {
		return util.Cmd("hg", "clone", "--", url, path)
	}

	if err := hgUtils.RunCommand(cloneCommand, url, path); err != nil {
		return err
	}

	return d.mustExecute([]string{"hg", "up", "--", p.SourceReference().S}, nil, php.RealpathString(path))
}

func (d *HgDownloader) doUpdate(_, target pkg.PackageInterface, path, url string) error {
	hgUtils := vcsutil.NewHg(d.io, d.config, d.process)

	ref := target.SourceReference().S
	d.io.WriteError(" Updating to "+ref, true, mio.Normal)

	if !d.hasMetadataRepository(path) {
		return &util.RuntimeError{Message: "The .hg directory is missing from " + path + ", see https://getcomposer.org/commit-deps for more information"}
	}

	pull := func(url string) util.Command { return util.Cmd("hg", "pull", "--", url) }
	if err := hgUtils.RunCommand(pull, url, path); err != nil {
		return err
	}

	up := func(string) util.Command { return util.Cmd("hg", "up", "--", ref) }

	return hgUtils.RunCommand(up, url, path)
}

// LocalChanges is getLocalChanges().
func (d *HgDownloader) LocalChanges(_ pkg.PackageInterface, path string) (pkg.NullString, error) {
	if !isDir(path + "/.hg") {
		return pkg.NullString{}, nil
	}

	var output string
	if _, err := d.execute([]string{"hg", "st"}, &output, php.RealpathString(path)); err != nil {
		return pkg.NullString{}, err
	}

	return trimmedOrNull(output), nil
}

func (d *HgDownloader) commitLogs(fromReference, toReference, path string) (string, error) {
	var output string
	if err := d.mustExecute([]string{"hg", "log", "-r", fromReference + ":" + toReference, "--style", "compact"}, &output, php.RealpathString(path)); err != nil {
		return "", err
	}

	return output, nil
}

func (d *HgDownloader) hasMetadataRepository(path string) bool {
	return isDir(path + "/.hg")
}
