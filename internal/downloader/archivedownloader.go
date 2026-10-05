// Ports src/Composer/Downloader/ArchiveDownloader.php, and the store-backed
// extraction that replaces its extract() for zip, tar, xz and gzip dists
// (deviations 1 and 2 of docs/PORTING.md).

package downloader

import (
	"errors"
	"os"
	"slices"

	"github.com/stubbedev/maestro/internal/archive"
	mio "github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/pkg"
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

	st.tryStore = true

	promise, err := a.attempt(st)
	if err != nil {
		return nil, err
	}

	return then(promise, func(file string) (*Promise, string, error) {
		if !st.stagedFromStore {
			a.stage(p, st.fileName)
		}

		return nil, file, nil
	}, nil), nil
}

func (a *ArchiveDownloader) installOperationAppendix(pkg.PackageInterface, string) (string, error) {
	return ": Extracting archive", nil
}

func (a *ArchiveDownloader) install(c call, p pkg.PackageInterface, path string) (*Promise, error) {
	if c.output {
		c.io.WriteError("  - "+FormatInstall(p)+": Extracting archive", true, mio.Normal)
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

	fileName := a.fileName(p)

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

	if s.err != nil {
		for _, w := range s.warnings {
			c.io.WriteError(w, true, mio.Normal)
		}

		a.cleanupFailed(p, path, s.dir)

		return rejected(s.err), nil
	}

	if fileExists(fileName) {
		if err := util.Unlink(fileName); err != nil {
			return rejected(err), nil
		}
	}

	if err := a.moveIntoPlace(p, s.dir, path, false); err != nil {
		return rejected(err), nil
	}

	_, _ = util.RemoveDirectoryPhp(s.dir)
	a.removeCleanupPath(p, s.dir)
	a.removeCleanupPath(p, path)

	return resolved(""), nil
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

	fileName := a.fileName(p)

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

	if real, ok := util.RealpathOK(path); ok {
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
// it.
func (d *FileDownloader) lookupStore(p pkg.PackageInterface) *store.Release {
	if read, _ := d.storeAccess(); !read {
		return nil
	}

	rel, err := d.store.Lookup(storeDist(p))
	if err != nil {
		return nil
	}

	return rel
}

// fromStore is download()'s cache hit for a dist in the store: the package
// is materialized into a staging directory instead of copying the archive.
// When objects went missing from the store, the archive is downloaded
// after all.
func (d *FileDownloader) fromStore(st *dlState, rel *store.Release, url dlURL, checksum pkg.NullString) *Promise {
	p := st.p

	if st.c.output {
		st.c.io.WriteError("  - Loading <info>"+p.Name()+"</info> (<comment>"+p.FullPrettyVersion(true, pkg.DisplaySourceRefIfDev)+"</comment>) from cache", true, mio.VeryVerbose)
	}

	dir := d.randomDir()
	d.addCleanupPath(p, dir)

	materialized, resolve, reject := util.NewDeferred[string](nil)

	go func() {
		if err := d.store.Materialize(rel, dir); err != nil {
			reject(err)

			return
		}

		resolve("")
	}()

	return then(materialized, func(string) (*Promise, string, error) {
		st.stagedFromStore = true
		d.setStaged(st.fileName, &staged{dir: dir})

		if err := d.dispatchPost(st, url, checksum); err != nil {
			return nil, "", err
		}

		return nil, st.fileName, nil
	}, func(err error) (*Promise, string, error) {
		d.removeCleanupPath(p, dir)

		if _, ok := errors.AsType[*store.MissingError](err); ok {
			promise, err := d.attempt(st)

			return promise, "", err
		}

		// reported by install(), as an extraction failure would be
		st.stagedFromStore = true
		d.setStaged(st.fileName, &staged{dir: dir, err: err})

		return nil, st.fileName, nil
	})
}

func (d *FileDownloader) setStaged(fileName string, s *staged) {
	d.mu.Lock()
	d.staged[fileName] = s
	d.mu.Unlock()
}

// stage extracts the downloaded archive of p into the store and
// materializes it into a staging directory, recording the outcome for
// install().
func (d *FileDownloader) stage(p pkg.PackageInterface, fileName string) *staged {
	s := &staged{dir: d.randomDir()}
	d.addCleanupPath(p, s.dir)

	if err := d.extractToStore(p, fileName, s.dir); err != nil {
		s.warnings, s.err = d.extractionError(p, fileName, s.dir, err)
	}

	d.setStaged(fileName, s)

	return s
}

// extractToStore inserts the archive into the shared store, or into a
// temporary one when the shared one must not be written, and materializes
// it at dir.
func (d *FileDownloader) extractToStore(p pkg.PackageInterface, fileName, dir string) error {
	if d.format == archive.Tar {
		if err := pharDataCheck(fileName); err != nil {
			return err
		}
	}

	s := d.store

	if _, write := d.storeAccess(); !write {
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

	return s.Install(storeDist(p), fileName, dir)
}

// isCwd is realpath($path) === Platform::getCwd().
func isCwd(path string) bool {
	real, ok := util.RealpathOK(path)
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
