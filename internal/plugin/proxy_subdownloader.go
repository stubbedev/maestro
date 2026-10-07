// Subclasses of FileDownloader written in PHP (docs/PLUGINS.md §4.8): a
// plugin's class extending FileDownloader (or ZipDownloader, ...) with
// methods of its own. Its object is maestro's downloader of the Composer
// class it extends (so parent::download() and the inherited methods are
// maestro's), and its overrides are called wherever Composer would call
// them: by the DownloadManager it is given to (subclassDownloader), and by
// the inherited code through $this (downloader.Hooks).

package plugin

import (
	"slices"

	"github.com/stubbedev/maestro/internal/downloader"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/plugin/rpc"
	"github.com/stubbedev/maestro/internal/resolver/operation"
)

// subclassDownloader is maestro's downloader of a subclass written in PHP
// as its DownloadManager uses it: the methods the subclass overrides are
// the PHP object's, the others maestro's downloader's.
type subclassDownloader struct {
	downloader.Downloader
	php        *proxyDownloader
	overridden []string
}

var (
	_ downloader.Downloader     = (*subclassDownloader)(nil)
	_ downloader.ChangeReporter = (*subclassDownloader)(nil)
)

func (s *subclassDownloader) overrides(method string) bool {
	return slices.Contains(s.overridden, method)
}

// PHPClass implements php.Classer.
func (s *subclassDownloader) PHPClass() string { return s.php.obj.Class }

// InstallationSource implements downloader.Downloader.
func (s *subclassDownloader) InstallationSource() string {
	if s.overrides("getInstallationSource") {
		return s.php.InstallationSource()
	}

	return s.Downloader.InstallationSource()
}

// Download implements downloader.Downloader.
func (s *subclassDownloader) Download(p pkg.PackageInterface, path string, prev pkg.PackageInterface) (*downloader.Promise, error) {
	if s.overrides("download") {
		return s.php.Download(p, path, prev)
	}

	return s.Downloader.Download(p, path, prev)
}

// Prepare implements downloader.Downloader.
func (s *subclassDownloader) Prepare(typ operation.Type, p pkg.PackageInterface, path string, prev pkg.PackageInterface) (*downloader.Promise, error) {
	if s.overrides("prepare") {
		return s.php.Prepare(typ, p, path, prev)
	}

	return s.Downloader.Prepare(typ, p, path, prev)
}

// Install implements downloader.Downloader.
func (s *subclassDownloader) Install(p pkg.PackageInterface, path string) (*downloader.Promise, error) {
	if s.overrides("install") {
		return s.php.Install(p, path)
	}

	return s.Downloader.Install(p, path)
}

// Update implements downloader.Downloader.
func (s *subclassDownloader) Update(initial, target pkg.PackageInterface, path string) (*downloader.Promise, error) {
	if s.overrides("update") {
		return s.php.Update(initial, target, path)
	}

	return s.Downloader.Update(initial, target, path)
}

// Remove implements downloader.Downloader.
func (s *subclassDownloader) Remove(p pkg.PackageInterface, path string) (*downloader.Promise, error) {
	if s.overrides("remove") {
		return s.php.Remove(p, path)
	}

	return s.Downloader.Remove(p, path)
}

// Cleanup implements downloader.Downloader.
func (s *subclassDownloader) Cleanup(typ operation.Type, p pkg.PackageInterface, path string, prev pkg.PackageInterface) (*downloader.Promise, error) {
	if s.overrides("cleanup") {
		return s.php.Cleanup(typ, p, path, prev)
	}

	return s.Downloader.Cleanup(typ, p, path, prev)
}

// LocalChanges implements downloader.ChangeReporter.
func (s *subclassDownloader) LocalChanges(p pkg.PackageInterface, path string) (pkg.NullString, error) {
	if s.overrides("getLocalChanges") {
		v, err := s.php.r.callObject(s.php.obj, "getLocalChanges", s.php.pkgValue(p), path)
		if err != nil || v == nil {
			return pkg.NullString{}, err
		}

		return pkg.Str(php.ToString(v)), nil
	}
	if cr, ok := s.Downloader.(downloader.ChangeReporter); ok {
		return cr.LocalChanges(p, path)
	}

	return pkg.NullString{}, nil
}

// adoptSubclassDownloader sets up maestro's downloader d of the Composer
// class a subclass written in PHP extends: the hooks of its overrides
// (methods of a class outside Composer's namespace) and the downloader
// its DownloadManager uses.
func (r *Runtime) adoptSubclassDownloader(o *rpc.PHPObject, d downloader.Downloader, overrides []string) {
	if len(overrides) == 0 {
		return
	}
	proxy := &proxyDownloader{r: r, obj: o}
	sub := &subclassDownloader{Downloader: d, php: proxy, overridden: overrides}

	if fd := downloader.FileDownloaderOf(d); fd != nil {
		var h downloader.Hooks
		if sub.overrides("download") {
			h.Download = func(p pkg.PackageInterface, path string, prev pkg.PackageInterface, output bool) (*downloader.Promise, error) {
				return proxy.promise("download", proxy.pkgValue(p), path, proxy.pkgValue(prev), output)
			}
		}
		if sub.overrides("install") {
			h.Install = func(p pkg.PackageInterface, path string, output bool) (*downloader.Promise, error) {
				return proxy.promise("install", proxy.pkgValue(p), path, output)
			}
		}
		if sub.overrides("remove") {
			h.Remove = func(p pkg.PackageInterface, path string, output bool) (*downloader.Promise, error) {
				return proxy.promise("remove", proxy.pkgValue(p), path, output)
			}
		}
		stringHook := func(method string) func(p pkg.PackageInterface, s string) (string, error) {
			if !sub.overrides(method) {
				return nil
			}

			return func(p pkg.PackageInterface, s string) (string, error) {
				v, err := r.callObject(o, method, proxy.pkgValue(p), s)

				return php.ToString(v), err
			}
		}
		h.InstallOperationAppendix = stringHook("getInstallOperationAppendix")
		h.FileName = stringHook("getFileName")
		h.ProcessURL = stringHook("processUrl")
		fd.SetHooks(h)
	}

	r.phpObjs.mu.Lock()
	defer r.phpObjs.mu.Unlock()
	if r.phpObjs.subclassDownloaders == nil {
		r.phpObjs.subclassDownloaders = map[downloader.Downloader]*subclassDownloader{}
	}
	r.phpObjs.subclassDownloaders[d] = sub
}

// managedDownloader is the downloader a DownloadManager uses for d: a
// subclass's, when d stands for one written in PHP.
func (r *Runtime) managedDownloader(d downloader.Downloader) downloader.Downloader {
	r.phpObjs.mu.Lock()
	defer r.phpObjs.mu.Unlock()

	if sub, ok := r.phpObjs.subclassDownloaders[d]; ok {
		return sub
	}

	return d
}
