// Ports src/Composer/Downloader/FileDownloader.php.

package downloader

import (
	"crypto/md5" //nolint:gosec // Composer's hash('md5', ...) of the temporary file name
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // Composer's sha1 cache keys and dist shasums
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	iofs "io/fs"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/stubbedev/maestro/internal/archive"
	"github.com/stubbedev/maestro/internal/eventdispatcher"
	mio "github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/comparer"
	"github.com/stubbedev/maestro/internal/resolver/operation"
	"github.com/stubbedev/maestro/internal/store"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/http"
)

// Deps are the collaborators of the downloaders, the constructor arguments
// of Composer's FileDownloader (plus the store). IO, Config and
// HTTPDownloader are required; the others may be nil.
type Deps struct {
	IO             mio.IO
	Config         Config
	HTTPDownloader HTTPDownloader
	// EventDispatcher fires PRE_FILE_DOWNLOAD and POST_FILE_DOWNLOAD.
	EventDispatcher EventDispatcher
	// Cache is Composer's files cache (cache-files-dir), nil when
	// cache-files-ttl is 0 (Factory::createDownloadManager). Every
	// downloader keeps its archives in it as Composer does; the archive
	// downloaders also follow it to decide whether the package store may
	// be read and written (see the package documentation).
	Cache Cache
	// Filesystem defaults to one running commands through Process.
	Filesystem *util.Filesystem
	// Process defaults to a new ProcessExecutor on IO.
	Process *util.ProcessExecutor
	// Store is the shared package store (cache.Store()).
	Store *store.Store
	// Metadata collects FileDownloader::$downloadMetadata.
	Metadata *Metadata
	// IniFiles lists the php.ini files of the user's php, for IniHelper's
	// messages.
	IniFiles func() []string
	// ExtensionLoaded is extension_loaded() of the PHP Composer would run
	// on: PharData decompresses tar.gz and tar.bz2 dists only with zlib and
	// bz2. nil counts every extension as loaded.
	ExtensionLoaded func(name string) bool
}

// hooks are the methods subclasses of FileDownloader override; the base
// class calls them through $this.
type hooks interface {
	download(c call, p pkg.PackageInterface, path string, prev pkg.PackageInterface) (*Promise, error)
	install(c call, p pkg.PackageInterface, path string) (*Promise, error)
	remove(c call, p pkg.PackageInterface, path string) (*Promise, error)
	installOperationAppendix(p pkg.PackageInterface, path string) (string, error)
}

// call carries what PHP passes implicitly: the IO ($this->io, which
// getLocalChanges swaps for a NullIO) and the $output flag.
type call struct {
	io     mio.IO
	output bool
}

// FileDownloader ports Composer\Downloader\FileDownloader, the downloader of
// "file" dists, and the base of the archive and path downloaders.
type FileDownloader struct {
	io       mio.IO
	config   Config
	http     HTTPDownloader
	events   EventDispatcher
	cache    Cache
	fs       *util.Filesystem
	process  *util.ProcessExecutor
	store    *store.Store
	metadata *Metadata
	iniFiles func() []string
	// extensionLoaded is Deps.ExtensionLoaded (never nil).
	extensionLoaded func(name string) bool
	self            hooks
	// hooks are the overrides of a subclass written in PHP (SetHooks).
	hooks *Hooks
	class string
	// format is the archive format store-backed downloaders extract (0
	// for none).
	format archive.Format
	umask  iofs.FileMode
	// retryDelay and nextURLDelay are Composer's usleep()s.
	retryDelay, nextURLDelay time.Duration

	mu sync.Mutex
	// lastCacheWrites maps a package name to its cache key.
	lastCacheWrites map[string]string
	// additionalCleanupPaths maps a package name to paths to remove.
	additionalCleanupPaths map[string][]string
	// staged maps the temporary file name of a package to its
	// materialized tree (store-backed downloads).
	staged map[string]*staged
	// specs are the trees materialized ahead of their downloads
	// (prefetchMaterial), by package; a taken one is nil.
	specs map[pkg.PackageInterface]*specMaterial
	// specCreated are the directories created for them, outermost first;
	// specChecked and specReady tell whether their directory was looked
	// at and is there.
	specCreated            []string
	specChecked, specReady bool
}

// NewFileDownloader is new FileDownloader($io, $config, $httpDownloader,
// $eventDispatcher, $cache, $filesystem, $process). It fails where the
// cache garbage collection it may run fails.
func NewFileDownloader(deps Deps) (*FileDownloader, error) {
	d := newFileDownloader(deps, `Composer\Downloader\FileDownloader`)

	return d, d.collectGarbage()
}

func newFileDownloader(deps Deps, class string) *FileDownloader {
	d := &FileDownloader{
		io:                     deps.IO,
		config:                 deps.Config,
		http:                   deps.HTTPDownloader,
		events:                 deps.EventDispatcher,
		cache:                  deps.Cache,
		fs:                     deps.Filesystem,
		process:                deps.Process,
		store:                  deps.Store,
		metadata:               deps.Metadata,
		iniFiles:               deps.IniFiles,
		extensionLoaded:        deps.ExtensionLoaded,
		class:                  class,
		umask:                  store.Umask(),
		retryDelay:             500 * time.Millisecond,
		nextURLDelay:           100 * time.Millisecond,
		lastCacheWrites:        map[string]string{},
		additionalCleanupPaths: map[string][]string{},
		staged:                 map[string]*staged{},
	}

	if d.process == nil {
		d.process = http.NewProcessExecutor(d.io)
	}

	if d.fs == nil {
		d.fs = util.NewFilesystem(d.process)
	}

	if d.iniFiles == nil {
		d.iniFiles = func() []string { return nil }
	}

	if d.extensionLoaded == nil {
		d.extensionLoaded = func(string) bool { return true }
	}

	d.self = d

	return d
}

// collectGarbage is the constructor's cache garbage collection; the
// package store is pruned with the same TTL.
func (d *FileDownloader) collectGarbage() error {
	if d.cache == nil || !d.cache.GcIsNecessary() {
		return nil
	}

	d.io.WriteError("Running cache garbage collection", true, mio.VeryVerbose)

	ttl := php.ToInt(d.config.Get("cache-files-ttl"))
	if _, err := d.cache.Gc(int(ttl), php.ToInt(d.config.Get("cache-files-maxsize"))); err != nil {
		return err
	}

	if d.store != nil {
		if _, err := d.store.Prune(time.Duration(ttl) * time.Second); err != nil {
			return err
		}
	}

	return nil
}

// Class is get_class().
func (d *FileDownloader) Class() string { return d.class }

// InstallationSource is getInstallationSource().
func (d *FileDownloader) InstallationSource() string { return "dist" }

// Download is download($package, $path, $prevPackage).
func (d *FileDownloader) Download(p pkg.PackageInterface, path string, prev pkg.PackageInterface) (*Promise, error) {
	return d.self.download(call{d.io, true}, p, path, prev)
}

// Install is install($package, $path).
func (d *FileDownloader) Install(p pkg.PackageInterface, path string) (*Promise, error) {
	return d.self.install(call{d.io, true}, p, path)
}

// Remove is remove($package, $path).
func (d *FileDownloader) Remove(p pkg.PackageInterface, path string) (*Promise, error) {
	return d.self.remove(call{d.io, true}, p, path)
}

// Prepare is prepare($type, $package, $path, $prevPackage).
func (d *FileDownloader) Prepare(string, pkg.PackageInterface, string, pkg.PackageInterface) (*Promise, error) {
	return resolved(""), nil
}

// dlURL is one entry of download()'s $urls.
type dlURL struct {
	base, processed, cacheKey string
}

// dlState is what download()'s closures share by reference.
type dlState struct {
	c        call
	p        pkg.PackageInterface
	fileName string
	urls     []dlURL
	retries  int
	// release and archive are a cache hit of a store-backed download whose
	// release the store holds: the release, and the cached archive, open
	// in case the store lost objects of it.
	release *store.Release
	archive *os.File
	// stagedFromStore: the package was materialized from the store.
	stagedFromStore bool
}

// cacheKey is download()'s $cacheKeyGenerator.
func cacheKey(p pkg.PackageInterface, key string) string {
	sum := sha1.Sum([]byte(key)) //nolint:gosec // Composer's cache key

	return p.Name() + "/" + hex.EncodeToString(sum[:]) + "." + p.DistType().S
}

func (d *FileDownloader) download(c call, p pkg.PackageInterface, path string, _ pkg.PackageInterface) (*Promise, error) {
	st, err := d.startDownload(c, p, path)
	if err != nil {
		return nil, err
	}

	return d.attempt(st)
}

// startDownload is the synchronous part of download(): the URLs, the
// temporary file name and the directories.
func (d *FileDownloader) startDownload(c call, p pkg.PackageInterface, path string) (*dlState, error) {
	if !p.DistURL().Valid {
		return nil, &util.InvalidArgumentError{Message: "The given package is missing url information"}
	}

	distURLs := p.DistURLs()
	st := &dlState{
		c: c, p: p, retries: 3, urls: make([]dlURL, 0, len(distURLs)),
	}

	for _, url := range distURLs {
		processed, err := d.processURL(p, url)
		if err != nil {
			return nil, err
		}
		// we use the complete download url here to avoid conflicting
		// entries from different packages, which would potentially allow a
		// given package in a third party repo to pre-populate the cache for
		// the same package in packagist for example.
		st.urls = append(st.urls, dlURL{base: url, processed: processed, cacheKey: cacheKey(p, processed)})
	}

	fileName, err := d.fileName(p, path)
	if err != nil {
		return nil, err
	}
	st.fileName = fileName

	if err := util.EnsureDirectoryExists(path); err != nil {
		return nil, err
	}

	if err := util.EnsureDirectoryExists(util.Dirname(st.fileName)); err != nil {
		return nil, err
	}

	return st, nil
}

// attempt is download()'s $download closure.
func (d *FileDownloader) attempt(st *dlState) (*Promise, error) {
	p := st.p
	url := st.urls[0]

	if d.events != nil {
		getter, _ := d.http.(http.Getter)

		ev := eventdispatcher.NewPreFileDownloadEvent(eventdispatcher.PreFileDownload, getter, url.processed, "package", p)
		if _, err := d.events.Dispatch(ev.Name(), ev); err != nil {
			return nil, err
		}

		if key := ev.CustomCacheKey(); key.Valid {
			url.cacheKey = cacheKey(p, key.S)
		} else if ev.ProcessedURL() != url.processed {
			url.cacheKey = cacheKey(p, ev.ProcessedURL())
		}

		url.processed = ev.ProcessedURL()
	}

	st.urls[0] = url

	checksum := p.DistSha1Checksum()

	var result *Promise

	hit, err := d.fromCache(st, url.cacheKey, checksum)
	if err != nil {
		return nil, err
	}

	if hit {
		if st.c.output {
			st.c.io.WriteError("  - Loading <info>"+p.Name()+"</info> (<comment>"+p.FullPrettyVersion(true, pkg.DisplaySourceRefIfDev)+"</comment>) from cache", true, mio.VeryVerbose)
		}

		if st.release != nil {
			return d.fromStore(st, url, checksum), nil
		}

		result = resolved(st.fileName)
	} else {
		if st.c.output {
			st.c.io.WriteError("  - Downloading <info>"+p.Name()+"</info> (<comment>"+p.FullPrettyVersion(true, pkg.DisplaySourceRefIfDev)+"</comment>)", true, mio.Normal)
		}

		transfer, err := d.http.AddCopy(url.processed, st.fileName, p.TransportOptions())
		if err != nil {
			return nil, err
		}

		result = then(transfer, func(r *http.Response) (*Promise, string, error) {
			return nil, d.accept(st, r), nil
		}, func(e error) (*Promise, string, error) {
			promise, err := d.reject(st, e)

			return promise, "", err
		})
	}

	return then(result, func(result string) (*Promise, string, error) {
		// in case of retry, the first call's Promise chain finally calls
		// this twice at the end, once with $result being the returned
		// $fileName from $accept, and then once for every failed request
		// with a null result, which can be skipped.
		if result == "" {
			return nil, st.fileName, nil
		}

		if !fileExists(st.fileName) {
			return nil, "", &util.UnexpectedValueError{Message: util.SanitizeURL(url.base) + " could not be saved to " + st.fileName + ", make sure the directory is writable and you have internet connectivity"}
		}

		if checksum.S != "" {
			sum, err := sha1File(st.fileName)
			if err != nil {
				return nil, "", err
			}

			if sum != checksum.S {
				return nil, "", &util.UnexpectedValueError{Message: "The checksum verification of the file failed (downloaded from " + util.SanitizeURL(url.base) + ")"}
			}
		}

		if err := d.dispatchPost(st, url, checksum); err != nil {
			return nil, "", err
		}

		return nil, st.fileName, nil
	}, nil), nil
}

// fromCache is the files cache branch of download(): the cached archive
// copied to the temporary file when present with a valid checksum. For a
// store-backed download whose release the store holds, the archive is not
// copied but kept open in st (the package comes from the store).
func (d *FileDownloader) fromCache(st *dlState, key string, checksum pkg.NullString) (bool, error) {
	if d.cache == nil {
		return false, nil
	}

	// use from cache if it is present and has a valid checksum or we have
	// no checksum to check against
	if checksum.S != "" {
		sum, ok, err := d.cache.Sha1(key)
		if err != nil {
			return false, err
		}

		if !ok || sum != checksum.S {
			return false, nil
		}
	}

	if rel := d.lookupStore(st.p); rel != nil {
		f, err := d.cache.Open(key)
		if err != nil || f == nil {
			return false, err
		}

		st.release, st.archive = rel, f
	} else if copied, err := d.cache.CopyTo(key, st.fileName); err != nil || !copied {
		return false, err
	}

	// mark the file as having been written in cache even though it is only
	// read from cache, so that if the cache is corrupt the archive will be
	// deleted and the next attempt will re-download it
	// see https://github.com/composer/composer/issues/10028
	if !d.cache.IsReadOnly() {
		d.mu.Lock()
		d.lastCacheWrites[st.p.Name()] = key
		d.mu.Unlock()
	}

	return true, nil
}

// dispatchPost fires POST_FILE_DOWNLOAD.
func (d *FileDownloader) dispatchPost(st *dlState, url dlURL, checksum pkg.NullString) error {
	if d.events == nil {
		return nil
	}

	ev := postFileDownloadEvent(st, url, checksum)
	_, err := d.events.Dispatch(ev.Name(), ev)

	return err
}

// postListened reports whether dispatchPost would reach a listener, which
// may read the file it names. A dispatcher that cannot tell counts as
// having one.
func (d *FileDownloader) postListened(st *dlState, url dlURL, checksum pkg.NullString) bool {
	if d.events == nil {
		return false
	}

	l, ok := d.events.(listenerChecker)

	return !ok || l.WillDispatchTo(postFileDownloadEvent(st, url, checksum))
}

func postFileDownloadEvent(st *dlState, url dlURL, checksum pkg.NullString) *eventdispatcher.PostFileDownloadEvent {
	return eventdispatcher.NewPostFileDownloadEvent(eventdispatcher.PostFileDownload, pkg.Str(st.fileName), checksum, url.processed, "package", st.p)
}

// accept is download()'s $accept closure.
func (d *FileDownloader) accept(st *dlState, r *http.Response) string {
	p := st.p
	key := st.urls[0].cacheKey

	if d.metadata != nil {
		var fileSize any

		if fi, err := os.Stat(st.fileName); err == nil {
			fileSize = fi.Size()
		} else if v, ok := r.Header("Content-Length"); ok {
			fileSize = v
		} else {
			fileSize = "?"
		}

		d.metadata.set(p.Name(), fileSize)
	}

	if d.cache != nil && !d.cache.IsReadOnly() {
		d.mu.Lock()
		d.lastCacheWrites[p.Name()] = key
		d.mu.Unlock()

		_, _ = d.cache.CopyFrom(key, st.fileName)
	}

	r.Collect()

	return st.fileName
}

// reject is download()'s $reject closure.
func (d *FileDownloader) reject(st *dlState, e error) (*Promise, error) {
	// clean up
	if fileExists(st.fileName) {
		if err := util.Unlink(st.fileName); err != nil {
			return nil, err
		}
	}

	d.clearLastCacheWrite(st.p)

	if isIrrecoverable(e) {
		return nil, e
	}

	if _, ok := errors.AsType[*util.MaxFileSizeExceededError](e); ok {
		return nil, e
	}

	if transport, ok := errors.AsType[*util.TransportError](e); ok {
		// if we got an http response with a proper code, then requesting
		// again will probably not help, abort
		if c := transport.Code; c != 0 && c != 500 && c != 502 && c != 503 && c != 504 {
			st.retries = 0
		}

		// special error code returned when network is being artificially
		// disabled
		if transport.StatusCode == 499 {
			st.retries = 0
			st.urls = st.urls[:0]
		}
	}

	if st.retries > 0 {
		time.Sleep(d.retryDelay)
		st.retries--

		return d.attempt(st)
	}

	if len(st.urls) > 0 {
		st.urls = st.urls[1:]
	}

	if len(st.urls) > 0 {
		class, code := util.PHPClassOf(e)
		name := st.p.Name()

		if st.c.io.IsDebug() {
			st.c.io.WriteError("    Failed downloading "+name+": ["+class+"] "+strconv.Itoa(code)+": "+e.Error(), true, mio.Normal)
			st.c.io.WriteError("    Trying the next URL for "+name, true, mio.Normal)
		} else {
			st.c.io.WriteError("    Failed downloading "+name+", trying the next URL ("+strconv.Itoa(code)+": "+e.Error()+")", true, mio.Normal)
		}

		st.retries = 3
		time.Sleep(d.nextURLDelay)

		return d.attempt(st)
	}

	return nil, e
}

// Cleanup is cleanup($type, $package, $path, $prevPackage).
func (d *FileDownloader) Cleanup(_ string, p pkg.PackageInterface, path string, _ pkg.PackageInterface) (*Promise, error) {
	fileName, err := d.fileName(p, path)
	if err != nil {
		return nil, err
	}
	if fileExists(fileName) {
		if err := util.Unlink(fileName); err != nil {
			return nil, err
		}
	}

	vendorDir := d.vendorDir()
	vendor, _, _ := strings.Cut(p.PrettyName(), "/")
	dirsToCleanUp := [...]string{path, vendorDir + "/" + vendor, vendorDir + "/composer/", vendorDir}

	d.mu.Lock()
	delete(d.staged, fileName)
	paths := d.additionalCleanupPaths[p.Name()]
	d.mu.Unlock()

	for _, pathToClean := range paths {
		if _, err := d.fs.Remove(pathToClean); err != nil {
			return nil, err
		}
	}

	cwd, _ := util.GetCwd(true)

	for _, dir := range dirsToCleanUp {
		if !isDir(dir) {
			continue
		}

		empty, err := util.IsDirEmpty(dir)
		if err != nil {
			return nil, err
		}

		if real, _ := util.RealpathOK(dir); empty && real != cwd {
			if _, err := util.RemoveDirectoryPhp(dir); err != nil {
				return nil, err
			}
		}
	}

	return resolved(""), nil
}

func (d *FileDownloader) install(c call, p pkg.PackageInterface, path string) (*Promise, error) {
	if c.output {
		c.io.WriteError("  - "+operation.FormatInstall(p, false), true, mio.Normal)
	}

	// clean up the target directory, unless it contains the vendor dir, as
	// the vendor dir contains the file to be installed. This is the case
	// when installing with create-project in the current directory but in
	// that case we ensure the directory is empty already in
	// ProjectInstaller so no need to empty it here.
	if err := d.emptyUnlessContainsVendor(path); err != nil {
		return nil, err
	}

	if err := util.EnsureDirectoryExists(path); err != nil {
		return nil, err
	}

	fileName, err := d.fileName(p, path)
	if err != nil {
		return nil, err
	}
	if err := d.fs.Rename(fileName, path+"/"+d.distPath(p, false)); err != nil {
		return nil, err
	}

	// Single files can not have a mode set like files in archives so we
	// make sure if the file is a binary that it is executable
	for _, bin := range p.Binaries().All() {
		binPath := path + "/" + php.ToString(bin)
		if !fileExists(binPath) || util.IsExecutable(binPath) {
			continue
		}

		// a bin resolving outside of the package would let it chmod an
		// arbitrary host file, this is reported by BinaryInstaller later in
		// the same install (GHSA-96h3-5x6v-m776)
		if !util.IsBinPathInsidePackage(path, binPath) {
			continue
		}

		_ = store.Chmod(binPath, 0o777&^d.umask)
	}

	return resolved(""), nil
}

// emptyUnlessContainsVendor empties path unless it contains the vendor
// directory (create-project in the current directory).
func (d *FileDownloader) emptyUnlessContainsVendor(path string) error {
	if strings.Contains(util.NormalizePath(d.vendorDir()), util.NormalizePath(path+string(os.PathSeparator))) {
		return nil
	}

	return d.fs.EmptyDirectory(path, true)
}

// distPath is getDistPath($package, PATHINFO_EXTENSION) (ext) or
// PATHINFO_BASENAME.
func (d *FileDownloader) distPath(p pkg.PackageInterface, ext bool) string {
	path := util.URLPath(strings.ReplaceAll(p.DistURL().S, `\`, "/"))
	base := php.Basename(path, "")

	if !ext {
		return base
	}

	if i := strings.LastIndexByte(base, '.'); i >= 0 {
		return base[i+1:]
	}

	return ""
}

func (d *FileDownloader) clearLastCacheWrite(p pkg.PackageInterface) {
	if d.cache == nil {
		return
	}

	d.mu.Lock()
	key, ok := d.lastCacheWrites[p.Name()]
	delete(d.lastCacheWrites, p.Name())
	d.mu.Unlock()

	if ok {
		_, _ = d.cache.Remove(key)
	}
}

func (d *FileDownloader) addCleanupPath(p pkg.PackageInterface, path string) {
	d.mu.Lock()
	d.additionalCleanupPaths[p.Name()] = append(d.additionalCleanupPaths[p.Name()], path)
	d.mu.Unlock()
}

func (d *FileDownloader) removeCleanupPath(p pkg.PackageInterface, path string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	paths := d.additionalCleanupPaths[p.Name()]
	for i, pathToClean := range paths {
		if pathToClean == path {
			d.additionalCleanupPaths[p.Name()] = append(paths[:i:i], paths[i+1:]...)

			return
		}
	}
}

// Update is update($initial, $target, $path).
func (d *FileDownloader) Update(initial, target pkg.PackageInterface, path string) (*Promise, error) {
	msg, err := operation.FormatUpdate(initial, target)
	if err != nil {
		return nil, err
	}

	appendix, err := d.self.installOperationAppendix(target, path)
	if err != nil {
		return nil, err
	}

	d.io.WriteError("  - "+msg+appendix, true, mio.Normal)

	promise, err := d.self.remove(call{d.io, false}, initial, path)
	if err != nil {
		return nil, err
	}

	return then(promise, func(string) (*Promise, string, error) {
		promise, err := d.self.install(call{d.io, false}, target, path)

		return promise, "", err
	}, nil), nil
}

func (d *FileDownloader) remove(c call, p pkg.PackageInterface, path string) (*Promise, error) {
	if c.output {
		c.io.WriteError("  - "+operation.FormatUninstall(p), true, mio.Normal)
	}

	promise, err := d.fs.RemoveDirectoryAsync(path)
	if err != nil {
		return nil, err
	}

	return then(promise, func(result bool) (*Promise, string, error) {
		if !result {
			return nil, "", &util.RuntimeError{Message: "Could not completely delete " + path + ", aborting."}
		}

		return nil, "", nil
	}, nil), nil
}

// ownFileName is getFileName($package, $path): the temporary file the dist
// is downloaded to. Composer hashes spl_object_id($package) in, so every
// package object gets its own; the object's address serves here.
func (d *FileDownloader) ownFileName(p pkg.PackageInterface) string {
	extension := d.distPath(p, true)
	if extension == "" {
		extension = p.DistType().S
	}

	sum := md5.Sum(fmt.Appendf(nil, "%s%p", p.String(), p)) //nolint:gosec // a file name, as Composer's

	return strings.TrimRight(d.vendorDir()+"/composer/tmp-"+hex.EncodeToString(sum[:])+"."+extension, ".")
}

func (d *FileDownloader) installOperationAppendix(pkg.PackageInterface, string) (string, error) {
	return "", nil
}

// ownProcessURL is processUrl($package, $url).
func (d *FileDownloader) ownProcessURL(p pkg.PackageInterface, url string) (string, error) {
	if ref := p.DistReference(); ref.Valid {
		return util.UpdateDistReference(url, ref.S, configList(d.config, "github-domains"), configList(d.config, "gitlab-domains"))
	}

	return url, nil
}

// LocalChanges is getLocalChanges($package, $path).
func (d *FileDownloader) LocalChanges(p pkg.PackageInterface, path string) (pkg.NullString, error) {
	c := call{mio.NewNullIO(), false}
	targetDir := util.TrimTrailingSlash(path)

	output, e := func() (string, error) {
		if isDir(targetDir + "_compare") {
			if _, err := d.fs.RemoveDirectory(targetDir + "_compare"); err != nil {
				return "", err
			}
		}

		promise, err := d.self.download(c, p, targetDir+"_compare", nil)
		if err != nil {
			return "", err
		}

		d.http.Wait()

		// the download may still extract into the store after its request
		if _, err := promise.Await(); err != nil {
			return "", err
		}

		if err := await(d.self.install(c, p, targetDir+"_compare")); err != nil {
			return "", err
		}

		var cmp comparer.Comparer
		cmp.SetSource(targetDir + "_compare")
		cmp.SetUpdate(targetDir)

		if err := cmp.DoCompare(); err != nil {
			return "", err
		}

		output := cmp.GetChangedAsString(true, false)

		if _, err := d.fs.RemoveDirectory(targetDir + "_compare"); err != nil {
			return "", err
		}

		return output, nil
	}()

	if e != nil {
		if d.io.IsDebug() {
			return pkg.NullString{}, e
		}

		class, _ := util.PHPClassOf(e)

		return pkg.Str("Failed to detect changes: [" + class + "] " + e.Error()), nil
	}

	return pkg.NonEmpty(php.Trim(output)), nil
}

// vendorDir is $this->config->get('vendor-dir').
func (d *FileDownloader) vendorDir() string {
	return php.ToString(d.config.Get("vendor-dir"))
}

// configList reads a list setting (github-domains, ...).
func configList(config Config, key string) []string {
	a, ok := config.Get(key).(*php.Array)
	if !ok {
		return nil
	}

	out := make([]string, 0, a.Len())
	for _, v := range a.All() {
		out = append(out, php.ToString(v))
	}

	return out
}

// randomDir is $vendorDir.'/composer/'.bin2hex(random_bytes(4)), a name not
// taken yet.
func (d *FileDownloader) randomDir() string {
	base := d.vendorDir() + "/composer/"

	for {
		var b [4]byte

		_, _ = rand.Read(b[:])

		dir := base + hex.EncodeToString(b[:])
		if !isDir(dir) {
			return dir
		}
	}
}

func sha1File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}

	defer func() { _ = f.Close() }()

	h := sha1.New() //nolint:gosec // the dist shasum is SHA-1
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// fileExists is PHP's file_exists().
func fileExists(path string) bool {
	_, err := os.Stat(path)

	return err == nil
}

// isDir is PHP's is_dir().
func isDir(path string) bool {
	fi, err := os.Stat(path)

	return err == nil && fi.IsDir()
}
