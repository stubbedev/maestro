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

// preparedWrite is a write built by PrepareWrite, keyed on the state of
// everything it is made of but the install paths.
type preparedWrite = util.Ahead[writeKey, *prepared]

// prepared is what PrepareWrite built.
type prepared struct {
	installPaths []pkg.NullString // of the key's canonical packages
	out          *writeOutput
	// installed.json and installed.php as they were once out was built
	current [2]currentFile
}

// writeKey is the state a write's input is in: the input, and the
// revision of each of its packages, which changes whenever a setter
// changes the package (through the plugin API, say), so that a package
// changed in place is not taken for the one the write was built of.
type writeKey struct {
	in   writeInput
	revs []uint64 // of in.canonical, then in.repoPackages, then in.rootPackage
}

// keyOf is the state in is in now.
func keyOf(in writeInput) writeKey {
	revs := make([]uint64, 0, len(in.canonical)+len(in.repoPackages)+1)
	for _, p := range in.canonical {
		revs = append(revs, p.Rev())
	}
	for _, p := range in.repoPackages {
		revs = append(revs, p.Rev())
	}
	if in.rootPackage != nil {
		revs = append(revs, in.rootPackage.Rev())
	}

	return writeKey{in, revs}
}

// same reports whether a and b are the same state: the same packages (the
// same objects, at the same revisions), names and paths.
func (a writeKey) same(b writeKey) bool {
	samePackage := func(x, y pkg.PackageInterface) bool { return x == y }

	return a.in.devMode == b.in.devMode && a.in.dumpVersions == b.in.dumpVersions && a.in.repoDir == b.in.repoDir &&
		a.in.rootPackage == b.in.rootPackage &&
		slices.EqualFunc(a.in.canonical, b.in.canonical, samePackage) &&
		slices.EqualFunc(a.in.repoPackages, b.in.repoPackages, samePackage) &&
		slices.Equal(a.in.devPackageNames, b.in.devPackageNames) &&
		slices.Equal(a.revs, b.revs)
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
// now, in the state they are in now, and its dev package names
// devPackageNames: the installer calls it before it waits on the network,
// when the lock file asks for no package operation, so that the final
// write of the no-op install that follows only compares (installed.json's
// and installed.php's contents take some milliseconds to build for a
// hundred packages). It writes nothing; the install paths are looked up
// here, with im. Write takes what was built when everything it is made of
// is the same, the install paths included, and builds it itself
// otherwise; files it finds as they were read here are not read again.
// DiscardPreparedWrite drops it.
func (r *FilesystemRepository) PrepareWrite(devMode bool, im InstallationManager, devPackageNames []string) {
	r.DiscardPreparedWrite()
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
	jsonPath := r.file.Path()
	r.preparedWrite = util.StartAheadFunc(keyOf(in), writeKey.same, func() (*prepared, error) {
		pw := &prepared{installPaths: installPaths}
		// build asks for the install paths in the order of in.canonical
		next := 0
		var err error
		if pw.out, err = in.build(func(pkg.PackageInterface) (pkg.NullString, error) {
			path := installPaths[next]
			next++

			return path, nil
		}, func(data *php.Array) (string, error) {
			return enc.Encoded(data, json.DefaultEncodeFlags)
		}); err != nil {
			return nil, err
		}
		pw.current[0] = readCurrentFile(jsonPath)
		if in.dumpVersions {
			pw.current[1] = readCurrentFile(repoDir + "/installed.php")
		}

		return pw, nil
	}, nil)
}

// DiscardPreparedWrite drops the write PrepareWrite built, once it is
// built: the installer calls it when the install it was prepared for
// ends without writing the repository.
func (r *FilesystemRepository) DiscardPreparedWrite() {
	pw := r.preparedWrite
	r.preparedWrite = nil
	pw.Discard()
}

// takePreparedWrite is the output PrepareWrite built when it was built of
// in, in the state it is in now, and of the install paths im gives now;
// nil otherwise. The prepared write is dropped either way.
func (r *FilesystemRepository) takePreparedWrite(in writeInput, im InstallationManager) *writeOutput {
	pw := r.preparedWrite
	r.preparedWrite = nil
	if pw == nil {
		return nil
	}
	p, ok := pw.Take(keyOf(in))
	if !ok {
		return nil
	}
	for i, pk := range in.canonical {
		if path, err := r.relativeInstallPath(im, pk, in.repoDir); err != nil || path != p.installPaths[i] {
			return nil
		}
	}
	out := p.out
	out.unchangedFns = [2]func() bool{
		func() bool { return out.encodedOK && p.current[0].holds(out.encoded) },
		func() bool { return p.current[1].holds(out.code) },
	}

	return out
}
