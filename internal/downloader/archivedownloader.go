// Ports src/Composer/Downloader/ArchiveDownloader.php, and the store-backed
// extraction that replaces its extract() for zip, tar, xz and gzip dists
// (deviations 1 and 2 of docs/PORTING.md).

package downloader

import (
	"crypto/sha1" //nolint:gosec // compares a file before and after an event
	"encoding/hex"
	"errors"
	"io"
	"os"
	"slices"

	"github.com/stubbedev/maestro/internal/archive"
	mio "github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/resolver/operation"
	"github.com/stubbedev/maestro/internal/store"
	"github.com/stubbedev/maestro/internal/util"
)

// ArchiveDownloader ports Composer\Downloader\ArchiveDownloader; the
// constructors below give its subclasses.
type ArchiveDownloader struct {
	*FileDownloader
	// extract is the subclass's extract() for the formats the store does
	// not handle (rar, phar).
	extract func(p pkg.PackageInterface, file, path string) error
}

// NewZipDownloader is new ZipDownloader(...).
func NewZipDownloader(deps Deps) (*ArchiveDownloader, error) {
	return newArchiveDownloader(deps, `Composer\Downloader\ZipDownloader`, archive.Zip)
}

// NewTarDownloader is new TarDownloader(...).
func NewTarDownloader(deps Deps) (*ArchiveDownloader, error) {
	return newArchiveDownloader(deps, `Composer\Downloader\TarDownloader`, archive.Tar)
}

// NewXzDownloader is new XzDownloader(...).
func NewXzDownloader(deps Deps) (*ArchiveDownloader, error) {
	return newArchiveDownloader(deps, `Composer\Downloader\XzDownloader`, archive.Xz)
}

// NewGzipDownloader is new GzipDownloader(...).
func NewGzipDownloader(deps Deps) (*ArchiveDownloader, error) {
	return newArchiveDownloader(deps, `Composer\Downloader\GzipDownloader`, archive.Gzip)
}

// NewRarDownloader is new RarDownloader(...).
func NewRarDownloader(deps Deps) (*ArchiveDownloader, error) {
	a, err := newArchiveDownloader(deps, `Composer\Downloader\RarDownloader`, 0)
	a.extract = a.extractRar

	return a, err
}

// NewPharDownloader is new PharDownloader(...).
func NewPharDownloader(deps Deps) (*ArchiveDownloader, error) {
	a, err := newArchiveDownloader(deps, `Composer\Downloader\PharDownloader`, 0)
	a.extract = a.extractPhar

	return a, err
}

func newArchiveDownloader(deps Deps, class string, format archive.Format) (*ArchiveDownloader, error) {
	d := newFileDownloader(deps, class)
	d.format = format
	a := &ArchiveDownloader{FileDownloader: d}
	d.self = a

	return a, d.collectGarbage()
}

func (a *ArchiveDownloader) download(c call, p pkg.PackageInterface, path string, prev pkg.PackageInterface) (*Promise, error) {
	if a.format == 0 {
		return a.FileDownloader.download(c, p, path, prev)
	}

	st, err := a.startDownload(c, p, path)
	if err != nil {
		return nil, err
	}

	promise, err := a.attempt(st)
	if err != nil {
		return nil, err
	}

	result := then(promise, func(file string) (*Promise, string, error) {
		if !st.stagedFromStore {
			return a.stageAsync(p, st.fileName, !st.changed), "", nil
		}

		return nil, file, nil
	}, nil)
	if a.format == archive.Zip {
		// ZipDownloader::download() calls parent::download() at line 100
		return result, nil
	}

	return result, nil
}

func (a *ArchiveDownloader) installOperationAppendix(pkg.PackageInterface, string) (string, error) {
	return ": Extracting archive", nil
}

func (a *ArchiveDownloader) install(c call, p pkg.PackageInterface, path string) (*Promise, error) {
	if c.output {
		c.io.WriteError("  - "+operation.FormatInstall(p, false)+": Extracting archive", true, mio.Normal)
	}

	// clean up the target directory, unless it contains the vendor dir, as
	// the vendor dir contains the archive to be extracted. This is the case
	// when installing with create-project in the current directory but in
	// that case we ensure the directory is empty already in
	// ProjectInstaller so no need to empty it here.
	if err := a.emptyUnlessContainsVendor(path); err != nil {
		return nil, err
	}

	if a.format == 0 {
		return a.installExtracted(p, path)
	}

	fileName, err := a.fileName(p, path)
	if err != nil {
		return nil, err
	}

	a.mu.Lock()
	s := a.staged[fileName]
	delete(a.staged, fileName)
	a.mu.Unlock()

	if s == nil {
		// installed without a download through this downloader (the
		// archive was put in place by someone else): extract it now
		if err := util.EnsureDirectoryExists(a.vendorDir() + "/composer"); err != nil {
			return nil, err
		}

		s = a.stage(p, fileName)
	}

	// avoid cleaning up $path if installing in "." for eg create-project as
	// we can not delete the directory we are currently in on windows
	if !isDir(path) || !isCwd(path) {
		a.addCleanupPath(p, path)
	}

	// Composer's ZipDownloader extracts with an asynchronous unzip, so what
	// follows the extraction (the failure, or the move into place and the
	// asynchronous removal of the temporary directory) settles the promise
	// on a later loop tick; the tar, xz and gzip extractors run
	// synchronously and throw. The tree is moved into place at once here;
	// only the settlement follows Composer.
	sched := a.process.Scheduler()
	async := a.format == archive.Zip

	if s.err != nil {
		fail := func() (string, error) {
			for _, w := range s.warnings {
				c.io.WriteError(w, true, mio.Normal)
			}

			a.cleanupFailed(p, path, s.dir)

			return "", s.err
		}

		if async {
			return util.Later(sched, fail), nil
		}

		_, err := fail()

		return nil, err
	}

	if err := a.placeStaged(p, fileName, s.dir, path); err != nil {
		if async {
			return util.Later(sched, func() (string, error) { return "", err }), nil
		}

		return rejected(err), nil
	}

	// Composer: removeDirectoryAsync($temporaryDir)->then(...)
	_, _ = util.RemoveDirectoryPhp(s.dir)

	return util.Later(sched, func() (string, error) {
		a.removeCleanupPath(p, s.dir)
		a.removeCleanupPath(p, path)

		return "", nil
	}), nil
}

// placeStaged is install()'s success callback for a staged tree: the
// archive is deleted and the tree moved into place.
func (a *ArchiveDownloader) placeStaged(p pkg.PackageInterface, fileName, dir, path string) error {
	if fileExists(fileName) {
		if err := util.Unlink(fileName); err != nil {
			return err
		}
	}

	return a.moveIntoPlace(p, dir, path, false)
}

// installExtracted is ArchiveDownloader::install's extraction into a
// temporary directory, for the formats extracted by extract().
func (a *ArchiveDownloader) installExtracted(p pkg.PackageInterface, path string) (*Promise, error) {
	temporaryDir := a.randomDir()

	a.addCleanupPath(p, temporaryDir)
	// avoid cleaning up $path if installing in "." for eg create-project as
	// we can not delete the directory we are currently in on windows
	if !isDir(path) || !isCwd(path) {
		a.addCleanupPath(p, path)
	}

	if err := util.EnsureDirectoryExists(temporaryDir); err != nil {
		return nil, err
	}

	fileName, err := a.fileName(p, path)
	if err != nil {
		return nil, err
	}

	if err := a.extract(p, fileName, temporaryDir); err != nil {
		a.cleanupFailed(p, path, temporaryDir)

		return nil, err
	}

	if fileExists(fileName) {
		if err := util.Unlink(fileName); err != nil {
			return rejected(err), nil
		}
	}

	if err := a.moveIntoPlace(p, temporaryDir, path, true); err != nil {
		return rejected(err), nil
	}

	promise, err := a.fs.RemoveDirectoryAsync(temporaryDir)
	if err != nil {
		return rejected(err), nil
	}

	return then(promise, func(bool) (*Promise, string, error) {
		a.removeCleanupPath(p, temporaryDir)
		a.removeCleanupPath(p, path)

		return nil, "", nil
	}, nil), nil
}

// cleanupFailed is install()'s $cleanup closure.
func (a *ArchiveDownloader) cleanupFailed(p pkg.PackageInterface, path, temporaryDir string) {
	// remove cache if the file was corrupted
	a.clearLastCacheWrite(p)

	// clean up
	_, _ = a.fs.RemoveDirectory(temporaryDir)
	if isDir(path) && !isCwd(path) {
		_, _ = a.fs.RemoveDirectory(path)
	}

	a.removeCleanupPath(p, temporaryDir)

	if real, ok := php.Realpath(path); ok {
		a.removeCleanupPath(p, real)
	}
}

// moveIntoPlace is the success callback of install(): the extracted tree
// is renamed to path in one go when path is clear, else merged into it.
// singleDir applies the single top-level directory rule (the store's trees
// have it applied already).
func (a *ArchiveDownloader) moveIntoPlace(p pkg.PackageInterface, extracted, path string, singleDir bool) error {
	renameAsOne := false

	if !fileExists(path) {
		renameAsOne = true
	} else if empty, err := util.IsDirEmpty(path); err != nil {
		return err
	} else if empty {
		// errors are ignored, and simply do not renameAsOne
		if removed, err := util.RemoveDirectoryPhp(path); err == nil && removed {
			renameAsOne = true
		}
	}

	from := extracted

	if singleDir {
		contentDir, err := folderContent(extracted)
		if err != nil {
			return err
		}

		if len(contentDir) == 1 && isDir(contentDir[0]) {
			from = contentDir[0]
		}
	}

	if renameAsOne {
		// if the target $path is clear, we can rename the whole package in
		// one go instead of looping over the contents
		return a.fs.Rename(from, path)
	}

	// only one dir in the archive, extract its contents out of it
	return a.renameRecursively(p, from, path)
}

// renameRecursively is install()'s $renameRecursively: renames (and merges
// if needed) a folder into another one. For custom installers, where
// packages may share paths, the source directory gets merged into the
// target one if the target exists.
func (a *ArchiveDownloader) renameRecursively(p pkg.PackageInterface, from, to string) error {
	contentDir, err := folderContent(from)
	if err != nil {
		return err
	}

	// move files back out of the temp dir
	for _, file := range contentDir {
		target := to + "/" + baseName(file)

		if isDir(target) {
			if !isDir(file) {
				return &util.RuntimeError{Message: "Installing " + p.String() + " would lead to overwriting the " + target + " directory with a file from the package, invalid operation."}
			}

			if err := a.renameRecursively(p, file, target); err != nil {
				return err
			}

			continue
		}

		if err := a.fs.Rename(file, target); err != nil {
			return err
		}
	}

	return nil
}

// folderContent is install()'s $getFolderContent: the entries of dir,
// excluding .DS_Store.
func folderContent(dir string) ([]string, error) {
	f, err := os.Open(dir)
	if err != nil {
		return nil, err
	}

	names, err := f.Readdirnames(-1)
	_ = f.Close()

	if err != nil {
		return nil, err
	}

	names = slices.DeleteFunc(names, func(name string) bool { return name == ".DS_Store" })
	for i, name := range names {
		names[i] = dir + "/" + name
	}

	return names, nil
}

// staged is a package tree materialized from the store by a download,
// waiting for install() to move it into place, or the extraction error
// install() reports.
type staged struct {
	err error
	dir string
	// warnings are printed before err is reported.
	warnings []string
}

// storeDist is a package's identity in the store.
func storeDist(p pkg.PackageInterface) store.Dist {
	return store.Dist{
		Name:      p.Name(),
		Type:      p.DistType().S,
		URL:       p.DistURL().S,
		Reference: p.DistReference().S,
		Shasum:    p.DistSha1Checksum().S,
	}
}

// storeAccess says whether the shared store may be read and written: as
// Composer's files cache would be (none when cache-files-ttl is 0 or the
// cache directory is unusable, read only with cache-read-only).
func (d *FileDownloader) storeAccess() (read, write bool) {
	if d.store == nil || d.cache == nil || !d.cache.IsEnabled() {
		return false, false
	}

	return true, !d.cache.IsReadOnly()
}

// lookupStore returns the release of p's dist when the shared store holds
// it (store-backed downloads only).
func (d *FileDownloader) lookupStore(p pkg.PackageInterface) *store.Release {
	if read, _ := d.storeAccess(); !read || d.format == 0 {
		return nil
	}

	rel, err := d.store.Lookup(storeDist(p))
	if err != nil {
		return nil
	}

	return rel
}

// fromStore is download()'s cache hit for a dist whose release the store
// holds: the package is materialized from the store into a staging
// directory instead of extracting the cached archive. When objects went
// missing from the store (or were found modified and dropped), the store
// is healed from the cached archive, opened when the cache was read: it is
// copied to the temporary file, as copyTo would have, and extracted.
//
// POST_FILE_DOWNLOAD fires first, as Composer fires it on the resolved
// promise of a cache hit before the next download starts and before
// anything is extracted. Composer copies the cached archive to the
// temporary file the event names, so it is copied first when a listener
// may read it; a listener that changed it gets what it left extracted,
// without the store (whose release is the cached archive's).
func (d *FileDownloader) fromStore(st *dlState, url dlURL, checksum pkg.NullString) *Promise {
	p := st.p
	rel, cached := st.release, st.archive
	st.release, st.archive = nil, nil
	copied := d.postListened(st, url, checksum)
	sum := ""

	if copied {
		var err error
		if sum, err = copyOpenFileSha1(cached, st.fileName); err != nil {
			_ = cached.Close()

			return rejected(err)
		}
	}

	if err := d.dispatchPost(st, url, checksum); err != nil {
		_ = cached.Close()

		return rejected(err)
	}

	changed := copied && !sameSha1(st.fileName, sum)
	s := d.newStaged(p)

	materialized := util.GoBackground(d.process.Scheduler(), func() (string, error) {
		defer func() { _ = cached.Close() }()

		if changed {
			return "", d.extractToStore(p, st.fileName, s.dir, false)
		}

		// Composer extracts the archive on every install, so PharData's
		// checks apply although the files come from the store.
		if d.format == archive.Tar {
			if err := pharDataCheck(st.fileName); err != nil {
				return "", err
			}

			if err := pharCompressionCheckAt(st.fileName, cached, d.extensionLoaded); err != nil {
				return "", err
			}
		}

		err := d.takeMaterial(p, rel, s.dir, d.importOptions(p))
		if _, ok := errors.AsType[*store.MissingError](err); !ok {
			return "", err
		}

		if !copied {
			if err := copyOpenFile(cached, st.fileName); err != nil {
				return "", err
			}
		}

		return "", d.extractToStore(p, st.fileName, s.dir, true)
	})

	// failures are reported by install(), as an extraction failure would be
	finish := func(err error) string {
		st.stagedFromStore = true
		d.finishStage(p, st.fileName, s, err)

		return st.fileName
	}

	return then(materialized, func(string) (*Promise, string, error) {
		return nil, finish(nil), nil
	}, func(err error) (*Promise, string, error) {
		return nil, finish(err), nil
	})
}

// copyOpenFileSha1 is copyOpenFile, returning the sha1 of what it copied.
func copyOpenFileSha1(src *os.File, target string) (string, error) {
	h := sha1.New() //nolint:gosec // compared with the file after POST_FILE_DOWNLOAD, not a security check

	if err := copyOpenFile(io.TeeReader(src, h), target); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// copyOpenFile is copy() from an open file to a new file at target.
func copyOpenFile(src io.Reader, target string) error {
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o666) //nolint:gosec // copy()'s mode, the umask applies
	if err != nil {
		return err
	}

	_, err = io.Copy(out, src)
	if cerr := out.Close(); err == nil {
		err = cerr
	}

	return err
}

func (d *FileDownloader) setStaged(fileName string, s *staged) {
	d.mu.Lock()
	d.staged[fileName] = s
	d.mu.Unlock()
}

// stage extracts the archive of p into the store and materializes it into
// a staging directory, recording the outcome for install().
func (d *FileDownloader) stage(p pkg.PackageInterface, fileName string) *staged {
	s := d.newStaged(p)
	d.finishStage(p, fileName, s, d.extractToStore(p, fileName, s.dir, true))

	return s
}

// stageAsync is stage for a download: the extraction runs on its own
// goroutine while the other downloads go on, and the promise resolves to
// fileName once its outcome is recorded. shared is extractToStore's.
func (d *FileDownloader) stageAsync(p pkg.PackageInterface, fileName string, shared bool) *Promise {
	s := d.newStaged(p)

	extracted := util.GoBackground(d.process.Scheduler(), func() (string, error) {
		return fileName, d.extractToStore(p, fileName, s.dir, shared)
	})

	return then(extracted, func(string) (*Promise, string, error) {
		d.finishStage(p, fileName, s, nil)

		return nil, fileName, nil
	}, func(err error) (*Promise, string, error) {
		// reported by install()
		d.finishStage(p, fileName, s, err)

		return nil, fileName, nil
	})
}

// newStaged picks the staging directory of a package.
func (d *FileDownloader) newStaged(p pkg.PackageInterface) *staged {
	s := &staged{dir: d.randomDir()}
	d.addCleanupPath(p, s.dir)

	return s
}

// finishStage records the outcome of an extraction for install().
func (d *FileDownloader) finishStage(p pkg.PackageInterface, fileName string, s *staged, err error) {
	if err != nil {
		s.warnings, s.err = d.extractionError(p, fileName, s.dir, err)
	}

	d.setStaged(fileName, s)
}

// extractToStore inserts the archive into the shared store, or into a
// temporary one when the shared one must not be written or shared is
// false (an archive a POST_FILE_DOWNLOAD listener changed is not the
// dist's), and materializes it at dir.
func (d *FileDownloader) extractToStore(p pkg.PackageInterface, fileName, dir string, shared bool) error {
	if d.format == archive.Tar {
		if err := pharDataCheck(fileName); err != nil {
			return err
		}

		if err := pharCompressionCheck(fileName, d.extensionLoaded); err != nil {
			return err
		}
	}

	s := d.store

	if _, write := d.storeAccess(); !write || !shared {
		method, err := store.ParseMethod(os.Getenv(store.MethodEnv))
		if err != nil {
			return err
		}

		tmp := d.randomDir()

		defer func() { _ = os.RemoveAll(tmp) }()

		if s, err = store.Open(tmp, &store.Options{Method: method}); err != nil {
			return err
		}
	}

	return s.Install(storeDist(p), fileName, dir, d.importOptions(p))
}

// importOptions is how p's files come from the store. Composer plugins
// (PluginInstaller's types) get files of their own, never hardlinks:
// plugins such as phpstan/extension-installer and
// infection/extension-installer rewrite their own files in place, which
// through a hardlink would change the store's object and every other
// project's copy of the plugin.
func (d *FileDownloader) importOptions(p pkg.PackageInterface) store.ImportOptions {
	t := p.Type()

	return store.ImportOptions{Unshared: t == "composer-plugin" || t == "composer-installer"}
}

// isCwd is realpath($path) === Platform::getCwd().
func isCwd(path string) bool {
	real, ok := php.Realpath(path)
	if !ok {
		return false
	}

	cwd, err := util.GetCwd(false)

	return err == nil && real == cwd
}

// baseName is PHP's basename() of a path built by this package.
func baseName(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[i+1:]
		}
	}

	return path
}
