// Ports PHP's path functions: getcwd(), realpath(), dirname() (zend_dirname),
// basename() (php_basename) and pathinfo()'s extension. They follow the
// platform PHP runs on; the ...On variants take it as an argument, for the
// ports that decide by Platform::isWindows().

package php

import (
	"path/filepath"
	"runtime"
	"strings"
)

// onWindows is PHP_OS_FAMILY === 'Windows' for the running maestro.
const onWindows = runtime.GOOS == "windows"

// Getcwd ports getcwd(): the physical working directory, as getcwd(3)
// reports it, never $PWD.
func Getcwd() (string, error) { return getwd() }

// Realpath ports realpath(): the absolute, symlink-free path of an
// existing file (junctions resolved too on Windows); realpath("") is the
// working directory. Every component must exist, also one followed by
// "..": the path is not cleaned lexically before it is resolved, so
// "link/../x" is x next to the link's target. ok is false (PHP's false)
// when the path does not resolve.
func Realpath(path string) (real string, ok bool) {
	abs := path
	if !filepath.IsAbs(path) {
		cwd, err := Getcwd()
		if err != nil {
			return "", false
		}
		abs = cwd + string(filepath.Separator) + path
	}
	real, err := EvalSymlinks(abs)
	if err != nil {
		return "", false
	}

	return real, true
}

// RealpathString is (string) realpath($path): "" for false.
func RealpathString(path string) string {
	real, _ := Realpath(path)

	return real
}

// Dirname ports dirname($path).
func Dirname(path string) string { return DirnameOn(path, onWindows) }

// DirnameOn ports dirname($path) as PHP runs it on Windows or not: there
// a drive letter is kept and '\' separates too.
func DirnameOn(path string, windows bool) string {
	if path == "" {
		return ""
	}

	drive := ""
	if windows && len(path) >= 2 && isASCIILetter(path[0]) && path[1] == ':' {
		// The drive spec is kept as is; dirname("c:") is "c:".
		drive, path = path[:2], path[2:]
		if path == "" {
			return drive
		}
	}

	sep := "/"
	if windows {
		sep = `\`
	}

	end := len(path) - 1
	for end >= 0 && isPathSep(path[end], windows) {
		end--
	}
	if end < 0 {
		return drive + sep
	}
	for end >= 0 && !isPathSep(path[end], windows) {
		end--
	}
	if end < 0 {
		return drive + "."
	}
	for end >= 0 && isPathSep(path[end], windows) {
		end--
	}
	if end < 0 {
		return drive + sep
	}

	return drive + path[:end+1]
}

// Basename ports basename($path, $suffix): the last component without
// trailing separators, minus suffix when the component ends with it and
// is longer than it.
func Basename(path, suffix string) string { return BasenameOn(path, suffix, onWindows) }

// BasenameOn ports basename($path, $suffix) as PHP runs it on Windows or
// not: there '\' separates too.
func BasenameOn(path, suffix string, windows bool) string {
	end := len(path)
	for end > 0 && isPathSep(path[end-1], windows) {
		end--
	}
	start := end
	for start > 0 && !isPathSep(path[start-1], windows) {
		start--
	}
	if len(suffix) < end-start && path[end-len(suffix):end] == suffix {
		end -= len(suffix)
	}

	return path[start:end]
}

// PathinfoExtension ports pathinfo($path, PATHINFO_EXTENSION).
func PathinfoExtension(path string) string { return PathinfoExtensionOn(path, onWindows) }

// PathinfoExtensionOn ports pathinfo($path, PATHINFO_EXTENSION) as PHP
// runs it on Windows or not.
func PathinfoExtensionOn(path string, windows bool) string {
	base := BasenameOn(path, "", windows)
	if i := strings.LastIndexByte(base, '.'); i >= 0 {
		return base[i+1:]
	}

	return ""
}

// isPathSep reports whether c separates path components for dirname() and
// basename(): '/', and on Windows '\' too.
func isPathSep(c byte, windows bool) bool { return c == '/' || windows && c == '\\' }

func isASCIILetter(c byte) bool { return c|0x20 >= 'a' && c|0x20 <= 'z' }
