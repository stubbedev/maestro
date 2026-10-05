// Ports src/Composer/Downloader/PathDownloader.php.

package downloader

import (
	"errors"
	"fmt"
	"os"
	"strings"

	mio "github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/archiver"
	"github.com/stubbedev/maestro/internal/pkg/dumper"
	"github.com/stubbedev/maestro/internal/pkg/version"
	"github.com/stubbedev/maestro/internal/resolver/operation"
	"github.com/stubbedev/maestro/internal/util"
)

// The install strategies of PathDownloader.
const (
	strategySymlink = 10
	strategyMirror  = 20
)

// PathDownloader ports Composer\Downloader\PathDownloader: it installs a
// package from a local path by symlink (junction on Windows) or by
// mirroring.
type PathDownloader struct {
	*FileDownloader
}

// NewPathDownloader is new PathDownloader(...).
func NewPathDownloader(deps Deps) (*PathDownloader, error) {
	d := newFileDownloader(deps, `Composer\Downloader\PathDownloader`)
	pd := &PathDownloader{FileDownloader: d}
	d.self = pd

	return pd, d.collectGarbage()
}

func (d *PathDownloader) download(_ call, p pkg.PackageInterface, path string, _ pkg.PackageInterface) (*Promise, error) {
	path = util.TrimTrailingSlash(path)

	url := p.DistURL()
	if !url.Valid {
		return nil, &util.RuntimeError{Message: "The package " + p.PrettyName() + " has no dist url configured, cannot download."}
	}

	realURL, ok := util.RealpathOK(url.S)
	if !ok || !isDir(realURL) {
		return nil, &util.RuntimeError{Message: fmt.Sprintf("Source path \"%s\" is not found for package %s", url.S, p.Name())}
	}

	realPath, _ := util.RealpathOK(path)
	if realPath == realURL {
		return resolved(""), nil
	}

	if strings.HasPrefix(realPath+string(os.PathSeparator), realURL+string(os.PathSeparator)) {
		// IMPORTANT NOTICE: If you wish to change this, don't. You are
		// wasting your time and ours.
		//
		// Please see https://github.com/composer/composer/pull/5974 and
		// https://github.com/composer/composer/pull/6174 for previous
		// attempts that were shut down because they did not work well
		// enough or introduced too many risks.
		return nil, &util.RuntimeError{Message: fmt.Sprintf("Package %s cannot install to \"%s\" inside its source at \"%s\"", p.Name(), realPath, realURL)}
	}

	return resolved(""), nil
}

func (d *PathDownloader) install(c call, p pkg.PackageInterface, path string) (*Promise, error) {
	path = util.TrimTrailingSlash(path)

	url := p.DistURL()
	if !url.Valid {
		return nil, &util.RuntimeError{Message: "The package " + p.PrettyName() + " has no dist url configured, cannot install."}
	}

	realURL, ok := util.RealpathOK(url.S)
	if !ok {
		return nil, &util.RuntimeError{Message: "Failed to realpath " + url.S}
	}

	if realPath, ok := util.RealpathOK(path); ok && realPath == realURL {
		if c.output {
			appendix, err := d.installOperationAppendix(p, path)
			if err != nil {
				return nil, err
			}

			c.io.WriteError("  - "+operation.FormatInstall(p, false)+appendix, true, mio.Normal)
		}

		return resolved(""), nil
	}

	// Get the transport options with default values
	transportOptions := p.TransportOptions().Clone()
	if !transportOptions.Has("relative") {
		transportOptions.Set("relative", true)
	}

	currentStrategy, allowedStrategies := computeAllowedStrategies(transportOptions)

	if _, err := d.fs.RemoveDirectory(path); err != nil {
		return nil, err
	}

	if c.output {
		c.io.WriteError("  - "+operation.FormatInstall(p, false)+": ", false, mio.Normal)
	}

	isFallback := false

	if currentStrategy == strategySymlink {
		if err := d.symlinkPackage(c, transportOptions, url.S, realURL, path); err != nil {
			if _, ok := errors.AsType[*util.IOError](err); !ok {
				return nil, err
			}

			if allowedStrategies&allowMirror == 0 {
				return nil, &util.RuntimeError{Message: fmt.Sprintf("Symlink from \"%s\" to \"%s\" failed!", realURL, path)}
			}

			if c.output {
				c.io.WriteError("", true, mio.Normal)
				c.io.WriteError("    <error>Symlink failed, fallback to use mirroring!</error>", true, mio.Normal)
			}

			currentStrategy = strategyMirror
			isFallback = true
		}
	}

	// Fallback if symlink failed or if symlink is not allowed for the
	// package
	if currentStrategy == strategyMirror {
		realURL = util.NormalizePath(realURL)

		if c.output {
			indent := ""
			if isFallback {
				indent = "    "
			}

			c.io.WriteError(indent+"Mirroring from "+url.S, false, mio.Normal)
		}

		finder, err := archiver.NewArchivableFilesFinder(realURL, nil, false)
		if err != nil {
			return nil, err
		}

		if err := mirror(realURL, path, finder.Files()); err != nil {
			return nil, err
		}
	}

	if c.output {
		c.io.WriteError("", true, mio.Normal)
	}

	return resolved(""), nil
}

// symlinkPackage is install()'s symlink strategy.
func (d *PathDownloader) symlinkPackage(c call, transportOptions *php.Array, url, realURL, path string) error {
	if util.IsWindows() {
		// Implement symlinks as NTFS junctions on Windows
		if c.output {
			c.io.WriteError("Junctioning from "+url, false, mio.Normal)
		}

		return d.fs.Junction(realURL, path)
	}

	path = strings.TrimRight(path, "/")

	if c.output {
		c.io.WriteError("Symlinking from "+url, false, mio.Normal)
	}

	if relative, _ := transportOptions.Get("relative"); relative != true {
		return symfonySymlink(realURL+"/", path)
	}

	absolutePath := path
	if !util.IsAbsolutePath(absolutePath) {
		cwd, err := util.GetCwd(false)
		if err != nil {
			return err
		}

		absolutePath = cwd + string(os.PathSeparator) + path
	}

	shortestPath, err := util.FindShortestPath(absolutePath, realURL, false, true)
	if err != nil {
		return err
	}

	return symfonySymlink(shortestPath+"/", path)
}

func (d *PathDownloader) remove(c call, p pkg.PackageInterface, path string) (*Promise, error) {
	path = util.TrimTrailingSlash(path)

	// realpath() may resolve Windows junctions to the source path, so we'll
	// check for a junction first to prevent a false positive when checking
	// if the dist and install paths are the same. See
	// https://bugs.php.net/bug.php?id=77639
	//
	// For junctions don't blindly rely on Filesystem::removeDirectory as it
	// may be overzealous. If a process inadvertently locks the file the
	// removal will fail, but it would fall back to recursive delete which
	// is disastrous within a junction. So in that case we have no other
	// real choice but to fail hard.
	if util.IsWindows() && util.IsJunction(path) {
		if c.output {
			c.io.WriteError("  - "+operation.FormatUninstall(p)+", source is still present in "+path, true, mio.Normal)
		}

		if removed, err := util.RemoveJunction(path); err != nil || !removed {
			c.io.WriteError("    <warning>Could not remove junction at "+path+" - is another process locking it?</warning>", true, mio.Normal)

			return nil, &util.RuntimeError{Message: "Could not reliably remove junction for package " + p.Name()}
		}

		return resolved(""), nil
	}

	url := p.DistURL()
	if !url.Valid {
		return nil, &util.RuntimeError{Message: "The package " + p.PrettyName() + " has no dist url configured, cannot remove."}
	}

	// ensure that the source path (dist url) is not the same as the install
	// path, which can happen when using custom installers, see
	// https://github.com/composer/composer/pull/9116 not using realpath
	// here as we do not want to resolve the symlink to the original dist
	// url it points to
	absPath, err := absolute(path)
	if err != nil {
		return nil, err
	}

	absDistURL, err := absolute(url.S)
	if err != nil {
		return nil, err
	}

	if util.NormalizePath(absPath) == util.NormalizePath(absDistURL) {
		if c.output {
			c.io.WriteError("  - "+operation.FormatUninstall(p)+", source is still present in "+path, true, mio.Normal)
		}

		return resolved(""), nil
	}

	return d.FileDownloader.remove(c, p, path)
}

// absolute is $fs->isAbsolutePath($path) ? $path : Platform::getCwd().'/'.$path.
func absolute(path string) (string, error) {
	if util.IsAbsolutePath(path) {
		return path, nil
	}

	cwd, err := util.GetCwd(false)
	if err != nil {
		return "", err
	}

	return cwd + "/" + path, nil
}

// VcsReference is getVcsReference($package, $path).
func (d *PathDownloader) VcsReference(p pkg.PackageInterface, path string) (pkg.NullString, error) {
	path = util.TrimTrailingSlash(path)
	guesser := version.NewVersionGuesser(version.NewProcessExecutor(d.process), d.io)

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

func (d *PathDownloader) installOperationAppendix(p pkg.PackageInterface, path string) (string, error) {
	url := p.DistURL()
	if !url.Valid {
		return "", &util.RuntimeError{Message: "The package " + p.PrettyName() + " has no dist url configured, cannot install."}
	}

	realURL, ok := util.RealpathOK(url.S)
	if !ok {
		return "", &util.RuntimeError{Message: "Failed to realpath " + url.S}
	}

	if realPath, ok := util.RealpathOK(path); ok && realPath == realURL {
		return ": Source already present", nil
	}

	currentStrategy, _ := computeAllowedStrategies(p.TransportOptions())

	if currentStrategy == strategySymlink {
		if util.IsWindows() {
			return ": Junctioning from " + url.S, nil
		}

		return ": Symlinking from " + url.S, nil
	}

	return ": Mirroring from " + url.S, nil
}

// The allowed strategies, as a set.
const (
	allowSymlink = 1 << iota
	allowMirror
)

// computeAllowedStrategies is computeAllowedStrategies(): the strategy to
// use and the allowed ones.
func computeAllowedStrategies(transportOptions *php.Array) (current, allowed int) {
	// When symlink transport option is null, both symlink and mirror are
	// allowed
	current = strategySymlink
	allowed = allowSymlink | allowMirror

	if mirrorPathRepos, _ := util.GetEnv("COMPOSER_MIRROR_PATH_REPOS"); php.ToBool(mirrorPathRepos) {
		current = strategyMirror
	}

	switch symlinkOption, _ := transportOptions.Get("symlink"); symlinkOption {
	case true:
		current = strategySymlink
		allowed = allowSymlink
	case false:
		current = strategyMirror
		allowed = allowMirror
	}

	// Junctions are safe on every Windows maestro runs on, and symlink()
	// is always available.
	return current, allowed
}
