// Ports Symfony Filesystem's mirror(), copy() and symlink(), which
// PathDownloader mirrors path repositories with.

package downloader

import (
	"io"
	"io/fs"
	"os"
	"strings"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg/archiver"
	"github.com/stubbedev/maestro/internal/util"
)

// mirror is Symfony's Filesystem::mirror($originDir, $targetDir,
// $iterator) without options.
func mirror(originDir, targetDir string, files []archiver.File) error {
	targetDir = strings.TrimRight(targetDir, `/\`)
	originDir = strings.TrimRight(originDir, `/\`)

	if _, err := os.Lstat(originDir); err != nil {
		return &util.IOError{Message: `The origin directory specified "` + originDir + `" was not found.`, Path: originDir}
	}

	if err := symfonyMkdir(targetDir); err != nil {
		return err
	}

	filesCreatedWhileMirroring := map[string]bool{}

	for _, f := range files {
		file := f.Pathname
		realPath, _ := php.Realpath(file)
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
		return &util.IOError{Message: `Failed to copy "` + originFile + `" because file does not exist.`, Path: originFile, Class: util.ClassFileNotFound}
	}

	if err := symfonyMkdir(php.Dirname(targetFile)); err != nil {
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
	if err := symfonyMkdir(php.Dirname(targetDir)); err != nil {
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
