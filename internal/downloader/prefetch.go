// Ports nothing: dist downloads started ahead of time (deliberate
// deviation 3, speed).

package downloader

import (
	"errors"
	iofs "io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/stubbedev/maestro/internal/eventdispatcher"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/store"
	"github.com/stubbedev/maestro/internal/util"
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
// overrides the download. A cached archive's tree is materialized ahead
// only when no POST_FILE_DOWNLOAD listener waits: Composer extracts after
// that event, from the file the listener may have changed, and nothing
// of the package may exist before it.
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

	postListened := false

	if d.events != nil {
		l, ok := d.events.(listenerChecker)
		if !ok {
			return
		}

		getter, _ := d.http.(http.Getter)
		if l.WillDispatchTo(eventdispatcher.NewPreFileDownloadEvent(eventdispatcher.PreFileDownload, getter, processed, "package", p)) {
			return
		}

		postListened = l.WillDispatchTo(eventdispatcher.NewPostFileDownloadEvent(eventdispatcher.PostFileDownload, pkg.NullString{}, p.DistSha1Checksum(), processed, "package", p))
	}

	// a cached archive is taken as a hit without hashing it (download()
	// hashes it when there is a checksum): a corrupt one costs the head
	// start, nothing else
	if d.cache != nil {
		h, ok := d.cache.(interface{ Holds(file string) bool })
		if !ok {
			return
		}

		if h.Holds(cacheKey(p, processed)) {
			if !postListened {
				d.prefetchMaterial(p)
			}

			return
		}
	}

	pf.PrefetchCopy(processed, p.TransportOptions())
}

// specMaterial is a package tree materialized from the store ahead of the
// download that needs it (prefetchMaterial), in a directory of its own
// next to the download's staging directories: its result is that
// directory, removed when nobody takes it.
type specMaterial = util.Ahead[materialKey, string]

// materialKey is what a materialized tree is made of. A Release never
// changes once loaded (Release.Entries), so the same one is the same
// tree.
type materialKey struct {
	rel  *store.Release
	opts store.ImportOptions
}

// prefetchMaterial starts materializing p's release from the store when
// the files cache holds its archive: download() will then find a cache
// hit and materialize that release (fromStore), and takes this tree
// instead when it is of the same release with the same options
// (takeMaterial), which is what materializing it then would give. Nothing
// shows: the tree is in a directory of vendor/composer of its own, and
// DiscardPrefetched removes what no download took, with vendor/composer
// and vendor when they were created for it.
func (d *FileDownloader) prefetchMaterial(p pkg.PackageInterface) {
	rel := d.lookupStore(p)
	if rel == nil {
		return
	}

	base := d.vendorDir() + "/composer"

	d.mu.Lock()
	defer d.mu.Unlock()

	if _, ok := d.specs[p]; ok {
		return
	}

	if !d.specDirs(base) {
		return
	}

	if d.specs == nil {
		d.specs = map[pkg.PackageInterface]*specMaterial{}
	}

	key, dir := materialKey{rel, d.importOptions(p)}, d.randomDir()
	d.specs[p] = util.StartAhead(key, func() (string, error) {
		return dir, d.store.Materialize(key.rel, dir, key.opts)
	}, func(dir string) { _ = os.RemoveAll(dir) })
}

// specDirs makes sure the directory dir exists for the trees materialized
// ahead, creating the missing ones of its ancestors (as mkdir -p does, the
// umask applying) and remembering them; false when one is in the way
// (anything but a directory, a symlink included) or cannot be created.
// d.mu is held.
func (d *FileDownloader) specDirs(dir string) bool {
	if d.specChecked {
		return d.specReady
	}

	d.specChecked = true

	var missing []string

	for p := filepath.Clean(dir); ; p = filepath.Dir(p) {
		fi, err := os.Lstat(p)
		if err == nil {
			if !fi.IsDir() {
				return false
			}

			break
		}

		if !errors.Is(err, iofs.ErrNotExist) || filepath.Dir(p) == p {
			return false
		}

		missing = append(missing, p)
	}

	for _, p := range slices.Backward(missing) {
		if err := os.Mkdir(p, 0o777); err != nil {
			return false
		}

		d.specCreated = append(d.specCreated, p)
	}

	d.specReady = true

	return true
}

// takeMaterial materializes rel at dst as d.store.Materialize does, taking
// the tree prefetchMaterial made for p when it is of rel with opts.
func (d *FileDownloader) takeMaterial(p pkg.PackageInterface, rel *store.Release, dst string, opts store.ImportOptions) error {
	d.mu.Lock()
	sp := d.specs[p]
	if sp != nil {
		d.specs[p] = nil
	}
	d.mu.Unlock()

	if dir, ok := sp.Take(materialKey{rel, opts}); ok {
		if err := os.Rename(dir, dst); err == nil {
			return nil
		}

		_ = os.RemoveAll(dir)
	}

	return d.store.Materialize(rel, dst, opts)
}

// discardMaterial waits for the trees prefetchMaterial started, removes
// those no download took, then the directories created for them that are
// left empty.
func (d *FileDownloader) discardMaterial() {
	d.mu.Lock()
	specs, created := d.specs, d.specCreated
	d.specs, d.specCreated = nil, nil
	d.specChecked, d.specReady = false, false
	d.mu.Unlock()

	for _, sp := range specs {
		sp.Discard()
	}

	for _, dir := range slices.Backward(created) {
		_ = os.Remove(dir) // only while empty
	}
}

// DiscardPrefetched ends what Prefetch started that no download took:
// install from a lock calls it once the operations ran or failed, or the
// lock did not verify.
func (m *DownloadManager) DiscardPrefetched() {
	seen := map[*FileDownloader]bool{}

	for _, d := range m.downloaders {
		if f := FileDownloaderOf(d); f != nil && !seen[f] {
			seen[f] = true
			f.discardMaterial()
		}
	}
}
