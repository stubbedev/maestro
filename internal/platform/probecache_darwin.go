//go:build darwin

// Ports nothing: see probecache.go, whose entries on macOS are keyed on
// what php's load commands say it loads (macho.go), walked from the php
// that answered, rather than on what the running process mapped: asking
// it needs cgo, which maestro does not build with.

package platform

import (
	"os"
	"path/filepath"
	"time"

	"github.com/stubbedev/maestro/internal/util/fsstate"
)

// maxProbeCacheAge is shorter than elsewhere: what php loaded is walked
// from its load commands (macho.go), which cannot see what a library
// loads at runtime (an OpenSSL provider, an ICU data bundle), so an
// upgrade that changes only those goes unseen until the age ends.
const maxProbeCacheAge = 6 * time.Hour

// probeWrapper reports whether the php that answered ran as the binary
// started: php reports the binary it runs as (PHP_BINARY), compared by
// identity, the same file's names differing in which path named it. A
// wrapper - an executable that replaces itself with the php it chose, or
// spawns it (version managers' shims are scripts, with no cache key at
// all) - is keyed on the variables it may read (wrapperEnvNames) and the
// working directory; cacheable is false only when what answered cannot
// be told: no PHP_BINARY, or no identity for either file.
func probeWrapper(s *Snapshot, resolved string) (wrapper, cacheable bool) {
	answered := s.PHPBinary()
	if answered == "" {
		return false, false
	}

	a, aOK := fsstate.Stat(answered)
	started, sOK := fsstate.Stat(resolved)
	if !aOK || !sOK {
		return false, false
	}

	return a != started, true
}

// probeIniDirs: php looks for its ini files nowhere else than here.
func probeIniDirs(dirs []string, _ *Snapshot) []string { return dirs }

// probeMappedFiles is what php loaded, walked from the php that answered
// (probe.php cannot read its own images on macOS): the binary with the
// dylibs its load commands name and theirs (macho.go), and the extension
// files it loaded (extensionFiles), which dlopen names no load command
// of php's. ok is false when the php that answered cannot be read as a
// Mach-O file: what it loads cannot be told.
func probeMappedFiles(s *Snapshot, resolved string, iniExtensions []string) ([]string, bool) {
	root := resolved
	if answered := s.PHPBinary(); answered != "" {
		root = answered
	}

	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}

	walk := newMachoWalk(root)
	if !walk.add(root, root) {
		return nil, false
	}

	for _, file := range extensionFiles(s, iniExtensions) {
		walk.add(file, root)
	}

	return walk.paths, len(walk.paths) > 0
}

// extensionFiles is the files the extensions php loaded live in: what
// its ini files load, kept whether it is there (php loaded it) or not
// (php warned; it may come), and each loaded extension's and zend
// extension's name in the extension directory (PHP_EXTENSION_DIR), where
// it is only when it is: an extension compiled into php has no file.
func extensionFiles(s *Snapshot, iniExtensions []string) []string {
	v, _ := s.Constant("PHP_EXTENSION_DIR")
	dir, _ := v.(string)

	files := make([]string, 0, len(iniExtensions)+len(s.Extensions)+len(s.ZendExtensions))

	for _, name := range iniExtensions {
		if filepath.IsAbs(name) {
			files = append(files, name)

			continue
		}

		if dir != "" {
			files = append(files, filepath.Join(dir, name))
		}
	}

	inDir := func(name string) {
		if dir == "" {
			return
		}

		p := filepath.Join(dir, name)

		if info, err := os.Stat(p); err == nil && info.Mode().IsRegular() {
			files = append(files, p)
		}
	}

	for _, e := range s.Extensions {
		inDir(e.Name + ".so")
	}

	for _, e := range s.ZendExtensions {
		inDir(e + ".so")
	}

	return files
}
