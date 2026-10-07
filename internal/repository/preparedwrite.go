// Ports nothing: the files of a FilesystemRepository write built while
// the installer waits on the network (deliberate deviation 3, speed).

package repository

import (
	"io/fs"
	"os"
	"slices"

	"github.com/stubbedev/maestro/internal/json"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/util"
)

// encodingFile is a JSONFile that tells what its Write writes
// (json.File).
type encodingFile interface {
	Encoded(hash any, options php.JSONFlag) (string, error)
}

// preparedWrite is a write built by PrepareWrite.
type preparedWrite struct {
	in           writeInput
	installPaths []pkg.NullString // of in.canonical

	done chan struct{}
	out  *writeOutput
	err  error
	// installed.json and installed.php as they were once out was built
	current [2]currentFile
}

// currentFile is a file's contents, and its description from before and
// after they were read; info is nil when the file could not be read.
type currentFile struct {
	path    string
	content []byte
	info    fs.FileInfo
}

func readCurrentFile(path string) currentFile {
	before, err := os.Stat(path)
	if err != nil || !before.Mode().IsRegular() {
		return currentFile{}
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return currentFile{}
	}
	after, err := os.Stat(path)
	if err != nil || !sameFileStamp(before, after) {
		return currentFile{}
	}

	return currentFile{path, content, after}
}

// holds reports whether the file holds content, as it was read, and did
// not change since.
func (f currentFile) holds(content string) bool {
	if f.info == nil || string(f.content) != content {
		return false
	}
	now, err := os.Stat(f.path)

	return err == nil && sameFileStamp(f.info, now)
}

// sameFileStamp reports whether a and b describe the same file with the
// same size, mode and modification time.
func sameFileStamp(a, b fs.FileInfo) bool {
	return os.SameFile(a, b) && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime()) && a.Mode() == b.Mode()
}

// PrepareWrite builds, in the background, the files a later Write with
// devMode writes when the repository's packages are then the ones it holds
// now and its dev package names devPackageNames: the installer calls it
// before it waits on the network, when the lock file asks for no package
// operation, so that the final write of the no-op install that follows
// only compares (installed.json's and installed.php's contents take some
// milliseconds to build for a hundred packages). It writes nothing; the
// install paths are looked up here, with im. Write takes what was built
// when everything it is made of is the same, the install paths included,
// and builds it itself otherwise; files it finds as they were read here
// are not read again.
func (r *FilesystemRepository) PrepareWrite(devMode bool, im InstallationManager, devPackageNames []string) {
	r.preparedWrite = nil
	enc, ok := r.file.(encodingFile)
	if !ok || r.deferWrites {
		return
	}
	canonical, err := r.CanonicalPackages()
	if err != nil {
		return
	}
	packages, err := r.Packages()
	if err != nil {
		return
	}
	// write creates the directory; this writes nothing
	repoDir := util.Dirname(r.file.Path())
	if st, err := os.Stat(repoDir); err != nil || !st.IsDir() {
		return
	}
	repoDir = util.NormalizePath(util.Realpath(repoDir))
	installPaths := make([]pkg.NullString, len(canonical))
	for i, p := range canonical {
		if installPaths[i], err = r.relativeInstallPath(im, p, repoDir); err != nil {
			return
		}
	}
	in := writeInput{devMode, slices.Clone(canonical), slices.Clone(packages), slices.Clone(devPackageNames), r.rootPackage, r.dumpVersions, repoDir}
	pw := &preparedWrite{in: in, installPaths: installPaths, done: make(chan struct{})}
	r.preparedWrite = pw
	jsonPath := r.file.Path()
	go func() {
		defer close(pw.done)
		// build asks for the install paths in the order of in.canonical
		next := 0
		pw.out, pw.err = in.build(func(pkg.PackageInterface) (pkg.NullString, error) {
			path := installPaths[next]
			next++

			return path, nil
		}, func(data *php.Array) (string, error) {
			return enc.Encoded(data, json.DefaultEncodeFlags)
		})
		pw.current[0] = readCurrentFile(jsonPath)
		if in.dumpVersions {
			pw.current[1] = readCurrentFile(repoDir + "/installed.php")
		}
	}()
}

// takePreparedWrite is the output PrepareWrite built when it was built of
// in and of the install paths im gives now, nil otherwise. The prepared
// write is dropped either way.
func (r *FilesystemRepository) takePreparedWrite(in writeInput, im InstallationManager) *writeOutput {
	pw := r.preparedWrite
	if pw == nil {
		return nil
	}
	r.preparedWrite = nil
	<-pw.done
	if pw.err != nil || !pw.in.same(in) {
		return nil
	}
	for i, p := range in.canonical {
		if path, err := r.relativeInstallPath(im, p, in.repoDir); err != nil || path != pw.installPaths[i] {
			return nil
		}
	}
	out := pw.out
	out.unchangedFns = [2]func() bool{
		func() bool { return out.encodedOK && pw.current[0].holds(out.encoded) },
		func() bool { return pw.current[1].holds(out.code) },
	}

	return out
}

// same reports whether a and b are the same input: the same packages
// (the same objects), names and paths.
func (in writeInput) same(b writeInput) bool {
	samePackage := func(x, y pkg.PackageInterface) bool { return x == y }

	return in.devMode == b.devMode && in.dumpVersions == b.dumpVersions && in.repoDir == b.repoDir &&
		in.rootPackage == b.rootPackage &&
		slices.EqualFunc(in.canonical, b.canonical, samePackage) &&
		slices.EqualFunc(in.repoPackages, b.repoPackages, samePackage) &&
		slices.Equal(in.devPackageNames, b.devPackageNames)
}
