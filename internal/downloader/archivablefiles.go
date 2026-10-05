// Ports src/Composer/Package/Archiver/ArchivableFilesFinder.php,
// BaseExcludeFilter.php, GitExcludeFilter.php and ComposerExcludeFilter.php
// (with Symfony Finder's Glob::toRegex and the Finder options they use),
// and Symfony Filesystem's mirror(), copy() and symlink(), which
// PathDownloader mirrors path repositories with.
//
// The archiver (internal/pkg/archiver) is ported later; it should take
// these over and this package import them.

package downloader

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"slices"
	"strings"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
)

// vcsPatterns is Finder::$vcsPatterns, the directories ignoreVCS(true)
// prunes.
var vcsPatterns = [...]string{".svn", "_svn", "CVS", "_darcs", ".arch-params", ".monotone", ".bzr", ".git", ".hg"}

// excludePattern is one of BaseExcludeFilter's [$pattern, $negate,
// $stripLeadingSlash].
type excludePattern struct {
	pattern           string
	negate            bool
	stripLeadingSlash bool
}

// archivableFiles is iterating new ArchivableFilesFinder($sources,
// $excludes): the paths under sources, sorted by name, without the VCS
// directories, the export-ignore'd and excluded files, and directories
// that are not empty.
func archivableFiles(sources string, excludes []string) ([]string, error) {
	sourcesRealPath, ok := util.RealpathOK(sources)
	if !ok {
		return nil, &util.RuntimeError{Message: `Could not realpath() the source directory "` + sources + `"`}
	}

	sources = util.NormalizePath(sourcesRealPath)

	filters, err := gitExcludePatterns(sources)
	if err != nil {
		return nil, err
	}

	filters = append(filters, generatePatterns(excludes)...)

	type found struct{ path, sortKey string }

	var files []found

	var walk func(dir string) error

	walk = func(dir string) error {
		entries, err := readDirUnsorted(dir)
		if err != nil {
			return err
		}

		for _, name := range entries {
			path := dir + "/" + name
			fi, lerr := os.Lstat(path)

			if lerr != nil {
				continue
			}

			isLink := fi.Mode()&fs.ModeSymlink != 0
			isDirectory := isDir(path)

			// ExcludeDirectoryFilterIterator prunes VCS directories
			if isDirectory && slices.Contains(vcsPatterns[:], name) {
				continue
			}

			realpath, ok := util.RealpathOK(path)
			if ok && (!isLink || strings.HasPrefix(realpath, sources)) && !excluded(filters, sources, realpath) {
				files = append(files, found{path: path, sortKey: realpath})
			}

			// RecursiveDirectoryIterator does not follow symlinks
			if isDirectory && !isLink {
				if err := walk(path); err != nil {
					return err
				}
			}
		}

		return nil
	}

	if err := walk(sources); err != nil {
		return nil, err
	}

	// sortByName: strcmp of the real paths
	slices.SortStableFunc(files, func(a, b found) int { return strings.Compare(a.sortKey, b.sortKey) })

	out := make([]string, 0, len(files))

	for _, f := range files {
		// ArchivableFilesFinder::accept(): directories only when empty
		if isDir(f.path) {
			if empty, err := util.IsDirEmpty(f.path); err != nil || !empty {
				continue
			}
		}

		out = append(out, f.path)
	}

	return out, nil
}

// excluded runs the filters over a file, as ArchivableFilesFinder's filter
// closure does.
func excluded(filters []excludePattern, sources, realpath string) bool {
	relativePath := util.NormalizePath(realpath)
	relativePath = strings.TrimPrefix(relativePath, sources)

	exclude := false

	for _, f := range filters {
		path := relativePath
		if f.stripLeadingSlash && path != "" {
			path = path[1:]
		}

		// errors (PcreException) are suppressed
		if ok, err := php.PregIsMatch(f.pattern, path); err == nil && ok {
			exclude = !f.negate
		}
	}

	return exclude
}

// gitExcludePatterns is new GitExcludeFilter($sourcePath): the
// export-ignore rules of .gitattributes.
func gitExcludePatterns(sourcePath string) ([]excludePattern, error) {
	data, err := os.ReadFile(sourcePath + "/.gitattributes")
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	var patterns []excludePattern

	for line := range bytes.Lines(data) {
		l := php.Trim(string(line))
		if !php.ToBool(l) || strings.HasPrefix(l, "#") {
			continue
		}

		parts, err := php.PregSplit(`#\s+#`, l, -1, 0)
		if err != nil || len(parts) != 2 {
			continue
		}

		switch parts[1] {
		case "export-ignore":
			patterns = append(patterns, generatePattern(parts[0]))
		case "-export-ignore":
			patterns = append(patterns, generatePattern("!"+parts[0]))
		}
	}

	return patterns, nil
}

// generatePatterns is BaseExcludeFilter::generatePatterns.
func generatePatterns(rules []string) []excludePattern {
	patterns := make([]excludePattern, 0, len(rules))
	for _, rule := range rules {
		patterns = append(patterns, generatePattern(rule))
	}

	return patterns
}

// generatePattern is BaseExcludeFilter::generatePattern: an exclude
// pattern from a gitignore rule.
func generatePattern(rule string) excludePattern {
	negate := false
	pattern := ""

	if rule != "" && rule[0] == '!' {
		negate = true
		rule = strings.TrimLeft(rule, "!")
	}

	if firstSlashPosition := strings.IndexByte(rule, '/'); firstSlashPosition == 0 {
		pattern = "^/"
	} else if firstSlashPosition < 0 || len(rule)-1 == firstSlashPosition {
		pattern = "/"
	}

	rule = strings.Trim(rule, "/")

	// remove delimiters as well as caret (^) and dollar sign ($) from the
	// regex
	regex := globToRegex(rule)
	rule = regex[2 : len(regex)-2]

	return excludePattern{pattern: "{" + pattern + rule + "(?=$|/)}", negate: negate}
}

// globToRegex is Symfony's Glob::toRegex($glob) with its defaults (strict
// leading dot, strict wildcard slash, '#' delimiter).
func globToRegex(glob string) string {
	const delimiter = '#'

	firstByte := true
	escaping := false
	inCurlies := 0

	var regex strings.Builder

	regex.WriteString("#^")

	for i := 0; i < len(glob); i++ {
		car := string(glob[i])

		if firstByte && car != "." {
			regex.WriteString(`(?=[^\.])`)
		}

		firstByte = car == "/"

		if firstByte && i+2 < len(glob) && glob[i+1:i+3] == "**" && (i+3 >= len(glob) || glob[i+3] == '/') {
			car = "[^/]++/"
			if i+3 >= len(glob) {
				car += "?"
			}

			car = `(?=[^\.])` + car
			car = "/(?:" + car + ")*"

			i += 2
			if i+1 < len(glob) {
				i++
			}
		}

		switch {
		case car == string(delimiter) || car == "." || car == "(" || car == ")" || car == "|" || car == "+" || car == "^" || car == "$":
			regex.WriteString(`\` + car)
		case car == "*":
			if escaping {
				regex.WriteString(`\*`)
			} else {
				regex.WriteString("[^/]*")
			}
		case car == "?":
			if escaping {
				regex.WriteString(`\?`)
			} else {
				regex.WriteString("[^/]")
			}
		case car == "{":
			if escaping {
				regex.WriteString(`\{`)
			} else {
				regex.WriteString("(")
				inCurlies++
			}
		case car == "}" && inCurlies > 0:
			if escaping {
				regex.WriteString("}")
			} else {
				regex.WriteString(")")
				inCurlies--
			}
		case car == "," && inCurlies > 0:
			if escaping {
				regex.WriteString(",")
			} else {
				regex.WriteString("|")
			}
		case car == `\`:
			if escaping {
				regex.WriteString(`\\`)
				escaping = false
			} else {
				escaping = true
			}

			continue
		default:
			regex.WriteString(car)
		}

		escaping = false
	}

	regex.WriteString("$#")

	return regex.String()
}

// readDirUnsorted lists a directory's names in directory order.
func readDirUnsorted(dir string) ([]string, error) {
	f, err := os.Open(dir)
	if err != nil {
		return nil, err
	}

	defer func() { _ = f.Close() }()

	return f.Readdirnames(-1)
}

// mirror is Symfony's Filesystem::mirror($originDir, $targetDir,
// $iterator) without options.
func mirror(originDir, targetDir string, files []string) error {
	targetDir = strings.TrimRight(targetDir, `/\`)
	originDir = strings.TrimRight(originDir, `/\`)

	if _, err := os.Lstat(originDir); err != nil {
		return &util.IOError{Message: `The origin directory specified "` + originDir + `" was not found.`, Path: originDir}
	}

	if err := symfonyMkdir(targetDir); err != nil {
		return err
	}

	filesCreatedWhileMirroring := map[string]bool{}

	for _, file := range files {
		realPath, _ := util.RealpathOK(file)
		if file == targetDir || realPath == targetDir || filesCreatedWhileMirroring[realPath] {
			continue
		}

		target := targetDir + file[len(originDir):]
		filesCreatedWhileMirroring[target] = true

		fi, err := os.Lstat(file)
		if err != nil {
			return &util.IOError{Message: `Unable to guess "` + file + `" file type.`, Path: file}
		}

		switch {
		case fi.Mode()&fs.ModeSymlink != 0:
			link, err := os.Readlink(file)
			if err != nil {
				return &util.IOError{Message: err.Error(), Path: file}
			}

			if err := symfonySymlink(link, target); err != nil {
				return err
			}
		case fi.IsDir():
			if err := symfonyMkdir(target); err != nil {
				return err
			}
		case fi.Mode().IsRegular():
			if err := symfonyCopy(file, target); err != nil {
				return err
			}
		default:
			return &util.IOError{Message: `Unable to guess "` + file + `" file type.`, Path: file}
		}
	}

	return nil
}

// symfonyMkdir is Symfony's Filesystem::mkdir($dir).
func symfonyMkdir(dir string) error {
	if isDir(dir) {
		return nil
	}

	if err := os.MkdirAll(dir, 0o777); err != nil && !isDir(dir) {
		return &util.IOError{Message: `Failed to create "` + dir + `": ` + util.Strerror(err), Path: dir}
	}

	return nil
}

// symfonyCopy is Symfony's Filesystem::copy($originFile, $targetFile): it
// keeps the executable bits and the modification time, and does not
// overwrite a newer target.
func symfonyCopy(originFile, targetFile string) error {
	origin, err := os.Stat(originFile)
	if err != nil || !origin.Mode().IsRegular() {
		return &util.IOError{Message: `Failed to copy "` + originFile + `" because file does not exist.`, Path: originFile}
	}

	if err := symfonyMkdir(util.Dirname(targetFile)); err != nil {
		return err
	}

	if target, err := os.Stat(targetFile); err == nil && target.Mode().IsRegular() && !origin.ModTime().After(target.ModTime()) {
		return nil
	}

	src, err := os.Open(originFile)
	if err != nil {
		return &util.IOError{Message: `Failed to copy "` + originFile + `" to "` + targetFile + `" because source file could not be opened for reading: ` + util.Strerror(err), Path: originFile}
	}

	defer func() { _ = src.Close() }()

	dst, err := os.OpenFile(targetFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o666) //nolint:gosec // copy() creates files 0666 & ~umask
	if err != nil {
		return &util.IOError{Message: `Failed to copy "` + originFile + `" to "` + targetFile + `" because target file could not be opened for writing: ` + util.Strerror(err), Path: originFile}
	}

	_, err = io.Copy(dst, src)
	if cerr := dst.Close(); err == nil {
		err = cerr
	}

	if err != nil {
		return &util.IOError{Message: `Failed to copy "` + originFile + `" to "` + targetFile + `".`, Path: originFile}
	}

	// Like `cp`, preserve executable permission bits
	if target, err := os.Stat(targetFile); err == nil {
		_ = os.Chmod(targetFile, target.Mode().Perm()|origin.Mode().Perm()&0o111)
	}

	// Like `cp`, preserve the file modification time
	_ = os.Chtimes(targetFile, origin.ModTime(), origin.ModTime())

	return nil
}

// symfonySymlink is Symfony's Filesystem::symlink($originDir, $targetDir).
func symfonySymlink(originDir, targetDir string) error {
	if err := symfonyMkdir(util.Dirname(targetDir)); err != nil {
		return err
	}

	if fi, err := os.Lstat(targetDir); err == nil && fi.Mode()&fs.ModeSymlink != 0 {
		if link, err := os.Readlink(targetDir); err == nil && link == originDir {
			return nil
		}

		if err := os.Remove(targetDir); err != nil {
			return &util.IOError{Message: `Failed to remove file "` + targetDir + `": ` + util.Strerror(err), Path: targetDir}
		}
	}

	if err := os.Symlink(originDir, targetDir); err != nil {
		return &util.IOError{Message: `Failed to create "symbolic" link from "` + originDir + `" to "` + targetDir + `": ` + util.Strerror(err), Path: targetDir}
	}

	return nil
}
