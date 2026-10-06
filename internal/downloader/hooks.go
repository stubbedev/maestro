// Subclasses of FileDownloader written in PHP (the plugin shim): a
// plugin's class extending FileDownloader (or ZipDownloader, ...) with
// methods of its own runs on maestro's downloader of the class it
// extends, its overrides called where Composer's code calls $this->...

package downloader

import (
	"github.com/stubbedev/maestro/internal/pkg"
)

// Hooks are the overrides of a subclass written in PHP: where Composer's
// FileDownloader (and its subclasses) call $this->download(), install(),
// remove(), getInstallOperationAppendix(), getFileName() or processUrl(),
// the subclass's method runs. A nil field keeps the class's own.
type Hooks struct {
	Download                 func(p pkg.PackageInterface, path string, prev pkg.PackageInterface, output bool) (*Promise, error)
	Install                  func(p pkg.PackageInterface, path string, output bool) (*Promise, error)
	Remove                   func(p pkg.PackageInterface, path string, output bool) (*Promise, error)
	InstallOperationAppendix func(p pkg.PackageInterface, path string) (string, error)
	FileName                 func(p pkg.PackageInterface, path string) (string, error)
	ProcessURL               func(p pkg.PackageInterface, url string) (string, error)
}

// hookedSelf is the $this of a downloader with hooks: the subclass's
// overrides, else the class's own methods (own).
type hookedSelf struct {
	own hooks
	h   *Hooks
}

func (s hookedSelf) download(c call, p pkg.PackageInterface, path string, prev pkg.PackageInterface) (*Promise, error) {
	if s.h.Download != nil {
		return s.h.Download(p, path, prev, c.output)
	}

	return s.own.download(c, p, path, prev)
}

func (s hookedSelf) install(c call, p pkg.PackageInterface, path string) (*Promise, error) {
	if s.h.Install != nil {
		return s.h.Install(p, path, c.output)
	}

	return s.own.install(c, p, path)
}

func (s hookedSelf) remove(c call, p pkg.PackageInterface, path string) (*Promise, error) {
	if s.h.Remove != nil {
		return s.h.Remove(p, path, c.output)
	}

	return s.own.remove(c, p, path)
}

func (s hookedSelf) installOperationAppendix(p pkg.PackageInterface, path string) (string, error) {
	if s.h.InstallOperationAppendix != nil {
		return s.h.InstallOperationAppendix(p, path)
	}

	return s.own.installOperationAppendix(p, path)
}

// fileDownloader is the FileDownloader a downloader is or extends (the
// method is promoted to the subclasses embedding it).
func (d *FileDownloader) fileDownloader() *FileDownloader { return d }

// FileDownloaderOf returns the FileDownloader d is or extends, or nil.
func FileDownloaderOf(d Downloader) *FileDownloader {
	if f, ok := d.(interface{ fileDownloader() *FileDownloader }); ok {
		return f.fileDownloader()
	}

	return nil
}

// SetHooks installs the overrides of a subclass written in PHP.
func (d *FileDownloader) SetHooks(h Hooks) {
	d.hooks = &h
	d.self = hookedSelf{own: d.ownSelf(), h: d.hooks}
}

// ownSelf is the class's own $this methods, without a subclass's hooks.
func (d *FileDownloader) ownSelf() hooks {
	if s, ok := d.self.(hookedSelf); ok {
		return s.own
	}

	return d.self
}

// OwnDownload is the class's own download() ($output included): what a
// subclass's parent::download() runs.
func (d *FileDownloader) OwnDownload(p pkg.PackageInterface, path string, prev pkg.PackageInterface, output bool) (*Promise, error) {
	return d.ownSelf().download(call{d.io, output}, p, path, prev)
}

// OwnInstall is the class's own install(): parent::install().
func (d *FileDownloader) OwnInstall(p pkg.PackageInterface, path string, output bool) (*Promise, error) {
	return d.ownSelf().install(call{d.io, output}, p, path)
}

// OwnRemove is the class's own remove(): parent::remove().
func (d *FileDownloader) OwnRemove(p pkg.PackageInterface, path string, output bool) (*Promise, error) {
	return d.ownSelf().remove(call{d.io, output}, p, path)
}

// OwnInstallOperationAppendix is the class's own
// getInstallOperationAppendix().
func (d *FileDownloader) OwnInstallOperationAppendix(p pkg.PackageInterface, path string) (string, error) {
	return d.ownSelf().installOperationAppendix(p, path)
}

// OwnFileName is the class's own getFileName($package, $path).
func (d *FileDownloader) OwnFileName(p pkg.PackageInterface) string { return d.ownFileName(p) }

// OwnProcessURL is the class's own processUrl($package, $url).
func (d *FileDownloader) OwnProcessURL(p pkg.PackageInterface, url string) (string, error) {
	return d.ownProcessURL(p, url)
}

// DistPath is getDistPath($package, $component): PATHINFO_EXTENSION
// (ext) or PATHINFO_BASENAME.
func (d *FileDownloader) DistPath(p pkg.PackageInterface, ext bool) string { return d.distPath(p, ext) }

// ClearLastCacheWrite is clearLastCacheWrite($package).
func (d *FileDownloader) ClearLastCacheWrite(p pkg.PackageInterface) { d.clearLastCacheWrite(p) }

// AddCleanupPath is addCleanupPath($package, $path).
func (d *FileDownloader) AddCleanupPath(p pkg.PackageInterface, path string) {
	d.addCleanupPath(p, path)
}

// RemoveCleanupPath is removeCleanupPath($package, $path).
func (d *FileDownloader) RemoveCleanupPath(p pkg.PackageInterface, path string) {
	d.removeCleanupPath(p, path)
}

// fileName is $this->getFileName($package, $path): a subclass's, or the
// class's own.
func (d *FileDownloader) fileName(p pkg.PackageInterface, path string) (string, error) {
	if d.hooks != nil && d.hooks.FileName != nil {
		return d.hooks.FileName(p, path)
	}

	return d.ownFileName(p), nil
}

// processURL is $this->processUrl($package, $url): a subclass's, or the
// class's own.
func (d *FileDownloader) processURL(p pkg.PackageInterface, url string) (string, error) {
	if d.hooks != nil && d.hooks.ProcessURL != nil {
		return d.hooks.ProcessURL(p, url)
	}

	return d.ownProcessURL(p, url)
}
