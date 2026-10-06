// FileDownloader's own methods called from PHP (docs/PLUGINS.md §4.8): what
// a subclass written in PHP calls on itself (parent::download(),
// parent::getFileName(), $this->addCleanupPath(), ...) runs the class's own
// code, not the subclass's overrides again.

package plugin

import (
	"strings"

	"github.com/stubbedev/maestro/internal/downloader"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/util"
)

// ownDownload is $downloader->download() called from PHP: on one of
// maestro's file downloaders, the class's own method with $output;
// another downloader's Download.
func ownDownload(d downloader.Downloader, p pkg.PackageInterface, path string, prev pkg.PackageInterface, output bool) (*downloader.Promise, error) {
	if fd := downloader.FileDownloaderOf(d); fd != nil {
		return fd.OwnDownload(p, path, prev, output)
	}

	return d.Download(p, path, prev)
}

// ownInstall is ownDownload for install().
func ownInstall(d downloader.Downloader, p pkg.PackageInterface, path string, output bool) (*downloader.Promise, error) {
	if fd := downloader.FileDownloaderOf(d); fd != nil {
		return fd.OwnInstall(p, path, output)
	}

	return d.Install(p, path)
}

// ownRemove is ownDownload for remove().
func ownRemove(d downloader.Downloader, p pkg.PackageInterface, path string, output bool) (*downloader.Promise, error) {
	if fd := downloader.FileDownloaderOf(d); fd != nil {
		return fd.OwnRemove(p, path, output)
	}

	return d.Remove(p, path)
}

// The PATHINFO_* constants getDistPath() takes.
const (
	pathinfoDirname   = 1
	pathinfoBasename  = 2
	pathinfoExtension = 4
	pathinfoFilename  = 8
)

// pathinfo is pathinfo($path, $component) for one component ("" for one
// the path has not).
func pathinfo(path string, component int) string {
	base := php.Basename(path, "")
	dot := strings.LastIndexByte(base, '.')
	switch component {
	case pathinfoDirname:
		if path == "" {
			return ""
		}

		return util.Dirname(path)
	case pathinfoBasename:
		return base
	case pathinfoExtension:
		if dot < 0 {
			return ""
		}

		return base[dot+1:]
	case pathinfoFilename:
		if dot < 0 {
			return base
		}

		return base[:dot]
	}

	return ""
}

func (r *Runtime) registerFileDownloaderHelpers() {
	helper := func(name string, fn func(fd *downloader.FileDownloader, p pkg.PackageInterface, a args) (any, error)) {
		r.Handle("downloader."+name, func(v any) (any, error) {
			a := argsOf("downloader."+name, v)
			d, err := receiver[downloader.Downloader](a)
			if err != nil {
				return nil, err
			}
			fd := downloader.FileDownloaderOf(d)
			if fd == nil {
				return nil, a.errorf("param 0 is not a FileDownloader")
			}
			p, err := packageParam(a, 1)
			if err != nil {
				return nil, err
			}

			return fn(fd, p, a)
		})
	}

	helper("getFileName", func(fd *downloader.FileDownloader, p pkg.PackageInterface, _ args) (any, error) {
		return fd.OwnFileName(p), nil
	})
	helper("processUrl", func(fd *downloader.FileDownloader, p pkg.PackageInterface, a args) (any, error) {
		return fd.OwnProcessURL(p, a.str(2))
	})
	helper("getInstallOperationAppendix", func(fd *downloader.FileDownloader, p pkg.PackageInterface, a args) (any, error) {
		return fd.OwnInstallOperationAppendix(p, a.str(2))
	})
	// getDistPath($package, $component): pathinfo() of the dist URL's path.
	helper("getDistPath", func(_ *downloader.FileDownloader, p pkg.PackageInterface, a args) (any, error) {
		return pathinfo(util.URLPath(strings.ReplaceAll(p.DistURL().S, `\`, "/")), a.integer(2)), nil
	})
	helper("clearLastCacheWrite", func(fd *downloader.FileDownloader, p pkg.PackageInterface, _ args) (any, error) {
		fd.ClearLastCacheWrite(p)

		return nil, nil
	})
	helper("addCleanupPath", func(fd *downloader.FileDownloader, p pkg.PackageInterface, a args) (any, error) {
		fd.AddCleanupPath(p, a.str(2))

		return nil, nil
	})
	helper("removeCleanupPath", func(fd *downloader.FileDownloader, p pkg.PackageInterface, a args) (any, error) {
		fd.RemoveCleanupPath(p, a.str(2))

		return nil, nil
	})
}
