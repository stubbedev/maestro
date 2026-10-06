package php

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// maxReparsePoints is the number of links Windows follows in one path.
const maxReparsePoints = 63

// EvalSymlinks resolves path as PHP's realpath() does on Windows
// (tsrm_realpath_r): every symlink and junction on it, component by
// component. filepath.EvalSymlinks alone would not do: since Go 1.23 it
// leaves junctions (mount points) alone, as os.Lstat reports them as
// irregular files, and a path through one then fails as "not a
// directory"; Composer's Filesystem::junction() makes them for path
// repositories.
func EvalSymlinks(path string) (string, error) {
	for range maxReparsePoints {
		next, found, err := followLink(path)
		if err != nil {
			return "", err
		}

		if !found {
			// No link is left: EvalSymlinks only gives the names their
			// case and long forms, as FindFirstFile does in PHP.
			return filepath.EvalSymlinks(next)
		}

		path = next
	}

	return "", &fs.PathError{Op: "realpath", Path: path, Err: syscall.ELOOP}
}

// followLink walks path (absolute, uncleaned: a component before ".."
// must exist) and replaces the first symlink or junction on it by its
// target. Without one it returns the path with "." and ".." resolved.
func followLink(path string) (string, bool, error) {
	vol := filepath.VolumeName(path)
	parts := strings.FieldsFunc(path[len(vol):], func(r rune) bool { return r == '\\' || r == '/' })
	cur := vol + `\`

	for i, part := range parts {
		switch part {
		case ".":
			continue
		case "..":
			cur = filepath.Dir(cur)

			continue
		}

		cur = filepath.Join(cur, part)

		info, err := os.Lstat(cur)
		if err != nil {
			return "", false, err
		}

		if info.Mode()&(fs.ModeSymlink|fs.ModeIrregular) == 0 {
			if !info.IsDir() && i < len(parts)-1 {
				return "", false, &fs.PathError{Op: "realpath", Path: cur, Err: syscall.ENOTDIR}
			}

			continue
		}

		target, err := os.Readlink(cur)
		if err != nil {
			continue // another kind of reparse point: a file of its own
		}

		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(cur), target)
		}

		return target + `\` + strings.Join(parts[i+1:], `\`), true, nil
	}

	return cur, false, nil
}
