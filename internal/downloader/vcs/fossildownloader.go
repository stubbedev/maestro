// Ports src/Composer/Downloader/FossilDownloader.php.

package vcs

import (
	"strings"

	mio "github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/util"
)

// FossilDownloader ports Composer\Downloader\FossilDownloader.
type FossilDownloader struct {
	vcsDownloader
}

// NewFossilDownloader is new FossilDownloader($io, $config, $process, $fs).
func NewFossilDownloader(deps Deps) *FossilDownloader {
	d := &FossilDownloader{}
	d.vcsDownloader = newVcsDownloader(deps, d, `Composer\Downloader\FossilDownloader`)

	return d
}

func (d *FossilDownloader) doDownload(pkg.PackageInterface, string, string, pkg.PackageInterface) error {
	return nil
}

func (d *FossilDownloader) doInstall(p pkg.PackageInterface, path, url string) error {
	// Ensure we are allowed to use this URL by config
	if err := d.config.ProhibitURLByConfig(url, d.io, nil); err != nil {
		return err
	}

	repoFile := path + ".fossil"
	realPath := util.Realpath(path)
	ref := p.SourceReference().S

	d.io.WriteError("Cloning "+ref, true, mio.Normal)

	if err := d.mustExecute(phperr.At("FossilDownloader.php", 117), []string{"fossil", "clone", "--", url, repoFile}, nil, ""); err != nil {
		return err
	}

	if err := d.mustExecute(phperr.At("FossilDownloader.php", 117), []string{"fossil", "open", "--nested", "--", repoFile}, nil, realPath); err != nil {
		return err
	}

	return d.mustExecute(phperr.At("FossilDownloader.php", 117), []string{"fossil", "update", "--", ref}, nil, realPath)
}

func (d *FossilDownloader) doUpdate(_, target pkg.PackageInterface, path, url string) error {
	// Ensure we are allowed to use this URL by config
	if err := d.config.ProhibitURLByConfig(url, d.io, nil); err != nil {
		return err
	}

	d.io.WriteError(" Updating to "+target.SourceReference().S, true, mio.Normal)

	if !d.hasMetadataRepository(path) {
		return &util.RuntimeError{Site: phperr.At("FossilDownloader.php", 64), Message: "The .fslckout file is missing from " + path + ", see https://getcomposer.org/commit-deps for more information"}
	}

	realPath := util.Realpath(path)
	if err := d.mustExecute(phperr.At("FossilDownloader.php", 117), []string{"fossil", "pull"}, nil, realPath); err != nil {
		return err
	}

	return d.mustExecute(phperr.At("FossilDownloader.php", 117), []string{"fossil", "up", "--", target.SourceReference().S}, nil, realPath)
}

// LocalChanges is getLocalChanges().
func (d *FossilDownloader) LocalChanges(_ pkg.PackageInterface, path string) (pkg.NullString, error) {
	if !d.hasMetadataRepository(path) {
		return pkg.NullString{}, nil
	}

	var output string
	if _, err := d.execute([]string{"fossil", "changes"}, &output, util.Realpath(path)); err != nil {
		return pkg.NullString{}, err
	}

	return trimmedOrNull(output), nil
}

func (d *FossilDownloader) commitLogs(_, toReference, path string) (string, error) {
	var output string
	if err := d.mustExecute(phperr.At("FossilDownloader.php", 117), []string{"fossil", "timeline", "-t", "ci", "-W", "0", "-n", "0", "before", toReference}, &output, util.Realpath(path)); err != nil {
		return "", err
	}

	// the reference is not quoted, as in Composer
	match := `/\d\d:\d\d:\d\d\s+\[` + toReference + `\]/`

	var log strings.Builder

	for _, line := range util.SplitLines(output) {
		found, err := php.PregIsMatch(match, line)
		if err != nil {
			return "", err
		}

		if found {
			break
		}

		log.WriteString(line)
	}

	return log.String(), nil
}

func (d *FossilDownloader) hasMetadataRepository(path string) bool {
	return isFile(path+"/.fslckout") || isFile(path+"/_FOSSIL_")
}
