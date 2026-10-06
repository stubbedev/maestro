// Ports src/Composer/Package/Archiver/ArchivableFilesFinder.php, with the
// parts of Symfony Finder (5.4) it configures: in(), filter(),
// ignoreVCS(true), ignoreDotFiles(false) and sortByName().

package archiver

import (
	"io/fs"
	"os"
	"slices"
	"strings"

	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/util"
)

// vcsPatterns is Finder::$vcsPatterns, the directories ignoreVCS(true)
// prunes wherever they are.
var vcsPatterns = []string{".svn", "_svn", "CVS", "_darcs", ".arch-params", ".monotone", ".bzr", ".git", ".hg"}

// File is one of the Symfony\Component\Finder\SplFileInfo objects
// ArchivableFilesFinder yields.
type File struct {
	// Pathname is getPathname(): the source directory joined with
	// RelativePathname (symbolic links are not resolved).
	Pathname string
	// RelativePathname is getRelativePathname().
	RelativePathname string
	// RealPath is getRealPath().
	RealPath string
	// IsDir is isDir() (true for a symbolic link to a directory).
	IsDir bool
}

// ArchivableFilesFinder ports ArchivableFilesFinder: the files that go
// into an archive of a directory, honouring .gitattributes export-ignore
// and composer.json's archive.exclude rules. Directories are only listed
// when they are empty.
type ArchivableFilesFinder struct {
	files []File
}

// NewArchivableFilesFinder ports ArchivableFilesFinder::__construct; the
// directory is walked here, where PHP walks it on the first iteration.
// excludes are Composer's own exclude rules; ignoreFilters drops them and
// .gitattributes.
//
// Errors are a *util.RuntimeError when sources has no real path, a
// *util.InvalidArgumentError (Symfony's DirectoryNotFoundException) when
// it is not a directory, a *util.UnexpectedValueError for unreadable
// directories (Symfony's AccessDeniedException is one), and the
// ErrorExceptions of reading .gitattributes or of a pattern that does not
// compile.
func NewArchivableFilesFinder(sources string, excludes []string, ignoreFilters bool) (*ArchivableFilesFinder, error) {
	sourcesRealPath, ok := util.RealpathOK(sources)
	if !ok {
		return nil, &util.RuntimeError{Site: phperr.At("ArchivableFilesFinder.php", 52), Message: `Could not realpath() the source directory "` + sources + `"`}
	}

	sources = util.NormalizePath(sourcesRealPath)

	var filters []excludeFilter

	if !ignoreFilters {
		gitFilter, err := NewGitExcludeFilter(sources)
		if err != nil {
			return nil, err
		}

		filters = []excludeFilter{gitFilter, NewComposerExcludeFilter(sources, excludes)}
	}

	files, err := findFiles(sources, func(f File, isLink bool) (bool, error) {
		if isLink && !strings.HasPrefix(f.RealPath, sources) {
			return false, nil
		}

		relativePath := strings.TrimPrefix(util.NormalizePath(f.RealPath), sources)

		exclude := false

		for _, filter := range filters {
			var err error
			if exclude, err = filter.Filter(relativePath, exclude); err != nil {
				return false, err
			}
		}

		return !exclude, nil
	})
	if err != nil {
		return nil, err
	}

	// accept(): directories only when they are empty
	accepted := files[:0]

	for _, f := range files {
		if f.IsDir {
			empty, err := util.IsDirEmpty(f.Pathname)
			if err != nil {
				return nil, openDirError("FilesystemIterator", f.Pathname, err, phperr.At("ArchivableFilesFinder.php", 109))
			}

			if !empty {
				continue
			}
		}

		accepted = append(accepted, f)
	}

	return &ArchivableFilesFinder{files: accepted}, nil
}

// Files returns the files in iteration order.
func (f *ArchivableFilesFinder) Files() []File { return f.files }

// findFiles iterates Finder::create()->in($dir)->ignoreVCS(true)
// ->ignoreDotFiles(false)->filter($filter)->sortByName(), where filter only
// sees files that have a real path.
func findFiles(dir string, filter func(f File, isLink bool) (bool, error)) ([]File, error) {
	if !isDir(dir) {
		return nil, &util.InvalidArgumentError{Class: util.ClassDirectoryNotFound, Message: `The "` + dir + `" directory does not exist.`, Site: phperr.At("Finder.php", 592)}
	}

	// Finder::normalizeDir and Symfony's RecursiveDirectoryIterator::current
	base := dir + "/"
	if dir == "/" {
		base = dir
	}

	var files []File

	var walk func(subPath string) error

	walk = func(subPath string) error {
		names, err := readDirNames(base + subPath)
		if err != nil {
			// Symfony's RecursiveDirectoryIterator::__construct calls the
			// SPL constructor at line 48, for the root (constructed by
			// Finder) and for subdirectories (by parent::getChildren());
			// getChildren() rethrows the latter as an AccessDeniedException
			// (line 127) with the SPL exception as its previous.
			inner := openDirError("RecursiveDirectoryIterator", strings.TrimSuffix(base+subPath, "/"), err, phperr.At("RecursiveDirectoryIterator.php", 48))
			if subPath == "" {
				return inner
			}

			return &util.UnexpectedValueError{
				Class:   util.ClassAccessDenied,
				Message: inner.Error(),
				Prev:    inner,
				Site:    phperr.At("RecursiveDirectoryIterator.php", 127),
			}
		}

		for _, name := range names {
			f := File{Pathname: base + subPath + name, RelativePathname: subPath + name}
			f.IsDir = isDir(f.Pathname)

			// ExcludeDirectoryFilterIterator
			if f.IsDir && slices.Contains(vcsPatterns, name) {
				continue
			}

			fi, err := os.Lstat(f.Pathname)
			if err != nil {
				continue
			}

			isLink := fi.Mode()&fs.ModeSymlink != 0

			// CustomFilterIterator
			var ok bool
			if f.RealPath, ok = util.RealpathOK(f.Pathname); ok {
				keep, err := filter(f, isLink)
				if err != nil {
					return err
				}

				if keep {
					files = append(files, f)
				}
			}

			// RecursiveDirectoryIterator::hasChildren() does not follow
			// links
			if fi.IsDir() {
				if err := walk(f.RelativePathname + "/"); err != nil {
					return err
				}
			}
		}

		return nil
	}

	if err := walk(""); err != nil {
		return nil, err
	}

	// SortableIterator::SORT_BY_NAME compares the real paths with strcmp;
	// uasort is stable
	slices.SortStableFunc(files, func(a, b File) int { return strings.Compare(a.RealPath, b.RealPath) })

	return files, nil
}

// openDirError is the UnexpectedValueException a SPL directory iterator
// throws for a directory it cannot open, at site.
func openDirError(class, dir string, err error, site phperr.Site) error {
	return &util.UnexpectedValueError{Message: class + "::__construct(" + dir + "): Failed to open directory: " + util.Strerror(err), Site: site}
}

// readDirNames lists a directory's names in readdir() order.
func readDirNames(dir string) ([]string, error) {
	f, err := os.Open(dir)
	if err != nil {
		return nil, err
	}

	defer func() { _ = f.Close() }()

	return f.Readdirnames(-1)
}

// isDir is is_dir(): true for a directory or a link to one.
func isDir(path string) bool {
	fi, err := os.Stat(path)

	return err == nil && fi.IsDir()
}
