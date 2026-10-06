// Ports nothing: dist downloads started ahead of time (deliberate
// deviation 3, speed).

package downloader

import (
	"github.com/stubbedev/maestro/internal/eventdispatcher"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/util/http"
)

// copyPrefetcher is an HTTPDownloader that can start a later AddCopy's
// transfer ahead of time (http.HttpDownloader.PrefetchCopy).
type copyPrefetcher interface {
	PrefetchCopy(url string, options *php.Array)
}

// Prefetch starts, without output, the transfer a later Download(p, ...,
// prev) will make, when that download will come from a dist URL the files
// cache does not hold: install from a lock calls it while the lock is
// verified. The transfer goes to a private spool and only the request of
// that Download takes it (http.HttpDownloader.PrefetchCopy), so the
// Download prints "Downloading" and fills the files cache as without it;
// a guess that turns out wrong costs a request and nothing else.
func (m *DownloadManager) Prefetch(p, prev pkg.PackageInterface) {
	if p.Type() == "metapackage" {
		return
	}

	sources, err := m.availableSources(p, prev)
	if err != nil || len(sources) == 0 || sources[0] != "dist" {
		return
	}

	d, ok := m.downloaders[php.Strtolower(p.DistType().S)]
	if !ok {
		return
	}

	switch className(d) {
	case `Composer\Downloader\ZipDownloader`, `Composer\Downloader\TarDownloader`, `Composer\Downloader\GzipDownloader`,
		`Composer\Downloader\XzDownloader`, `Composer\Downloader\RarDownloader`, `Composer\Downloader\PharDownloader`,
		`Composer\Downloader\FileDownloader`:
	default:
		return
	}

	if f := FileDownloaderOf(d); f != nil {
		f.prefetch(p)
	}
}

// prefetch is Prefetch for d's own download(): the first URL as the
// $download closure requests it, unless the files cache answers it or a
// listener may change the request (PRE_FILE_DOWNLOAD) or a PHP subclass
// overrides the download.
func (d *FileDownloader) prefetch(p pkg.PackageInterface) {
	pf, ok := d.http.(copyPrefetcher)
	if !ok || d.hooks != nil || !p.DistURL().Valid {
		return
	}

	urls := p.DistURLs()
	if len(urls) == 0 {
		return
	}

	processed, err := d.processURL(p, urls[0])
	if err != nil {
		return
	}

	if d.events != nil {
		l, ok := d.events.(listenerChecker)
		if !ok {
			return
		}

		getter, _ := d.http.(http.Getter)
		if l.WillDispatchTo(eventdispatcher.NewPreFileDownloadEvent(eventdispatcher.PreFileDownload, getter, processed, "package", p)) {
			return
		}
	}

	// a cached archive is taken as a hit without hashing it (download()
	// hashes it when there is a checksum): a corrupt one costs the head
	// start, nothing else
	if d.cache != nil {
		h, ok := d.cache.(interface{ Holds(file string) bool })
		if !ok || h.Holds(cacheKey(p, processed)) {
			return
		}
	}

	pf.PrefetchCopy(processed, p.TransportOptions())
}
