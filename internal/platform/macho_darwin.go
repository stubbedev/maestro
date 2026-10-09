//go:build darwin

// Ports nothing: reading what a Mach-O file loads, to key probe cache
// entries with on macOS (probecache.go), where the process itself cannot
// be asked: listing another process's images needs cgo (proc_pidinfo) or
// MIG RPC (mach_vm_region), and maestro builds without cgo. std's
// debug/macho reads the dylibs a file links and its run paths, but only
// the plain LC_LOAD_DYLIB of the five load commands that name a dylib, so
// the other four are read out of the raw commands. What no load command
// names - a library a library of php's loads at runtime with dlopen - the
// walk cannot see; the shorter max age (maxProbeCacheAge) ends entries
// before that goes stale.

package platform

import (
	"debug/macho"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

// The load commands that name a dylib, beyond LC_LOAD_DYLIB (0xc), which
// debug/macho reads: a weak dylib may be missing at load time, a
// re-exported one loads through its re-exporter, a lazy one on first use,
// an upward one back into its loader.
const (
	lcLoadWeakDylib   = 0x80000018
	lcReexportDylib   = 0x8000001f
	lcLazyLoadDylib   = 0x20
	lcLoadUpwardDylib = 0x80000023
)

// machoInfo is what a Mach-O file's load commands name: the install names
// of the dylibs it loads, and the run paths it resolves an @rpath name
// with.
type machoInfo struct {
	dylibs []string
	rpaths []string
}

// readMachO is the load commands of the Mach-O file at path, of the
// architecture this process runs on when the file is universal; false
// when the file is none, or of another architecture only, or cannot be
// read as one.
func readMachO(path string) (machoInfo, bool) {
	f, err := os.Open(path)
	if err != nil {
		return machoInfo{}, false
	}
	defer func() { _ = f.Close() }()

	file, ok := openMachO(f)
	if !ok {
		return machoInfo{}, false
	}

	var info machoInfo

	for _, load := range file.Loads {
		switch l := load.(type) {
		case *macho.Dylib:
			info.dylibs = append(info.dylibs, l.Name)
		case *macho.Rpath:
			info.rpaths = append(info.rpaths, l.Path)
		case macho.LoadBytes:
			// the commands that name a dylib but that debug/macho
			// leaves raw (LC_LOAD_DYLIB is the typed one)
			switch file.ByteOrder.Uint32(l[0:4]) {
			case lcLoadWeakDylib, lcReexportDylib, lcLazyLoadDylib, lcLoadUpwardDylib:
				name, ok := loadCommandString(l, file.ByteOrder)
				if !ok {
					return machoInfo{}, false
				}

				info.dylibs = append(info.dylibs, name)
			}
		}
	}

	return info, true
}

// openMachO is the Mach-O file behind f, of this process's architecture
// when it is universal (a universal file without that architecture is
// none). The file is not closed: it holds nothing but f, which the caller
// closes.
func openMachO(f *os.File) (*macho.File, bool) {
	fat, err := macho.NewFatFile(f)
	if err == nil {
		for _, arch := range fat.Arches {
			if arch.Cpu == hostCpu() {
				return arch.File, true
			}
		}

		return nil, false
	}

	if !errors.Is(err, macho.ErrNotFat) {
		return nil, false
	}

	file, err := macho.NewFile(f)

	return file, err == nil
}

// loadCommandString is the string a raw load command names at its lc_str
// field (both dylib_command and rpath_command carry it at offset 8,
// counted from the command's start): NUL-terminated within the command,
// and never reaching into it. ok is false when the command is malformed.
func loadCommandString(cmd macho.LoadBytes, bo binary.ByteOrder) (string, bool) {
	if len(cmd) < 12 {
		return "", false
	}

	from := int(bo.Uint32(cmd[8:12]))
	if from < 12 || from >= len(cmd) {
		return "", false
	}

	s, _, _ := strings.Cut(string(cmd[from:]), "\x00")

	return s, true
}

// hostCpu is the Mach-O CPU type this process runs as.
func hostCpu() macho.Cpu {
	switch runtime.GOARCH {
	case "arm64":
		return macho.CpuArm64
	case "amd64":
		return macho.CpuAmd64
	case "386":
		return macho.Cpu386
	case "arm":
		return macho.CpuArm
	default:
		return 0
	}
}

// resolveDylib is the file an install name names, as dyld resolves it
// when the file whose directory is loaderDir loads it within the main
// executable whose directory is exeDir: @loader_path and
// @executable_path stand for those directories (a run path may name one
// bare), an absolute name is itself, an @rpath name tries each run path
// in turn (dyld takes the first whose file exists) and falls back to the
// first, and any other name loads from the working directory, which is
// maestro's. "" when the name names nothing to watch: an @-name no run
// path resolves.
func resolveDylib(name, loaderDir, exeDir string, rpaths []string) string {
	expand := func(p string) string {
		switch p {
		case "@executable_path":
			return exeDir
		case "@loader_path":
			return loaderDir
		}

		switch {
		case strings.HasPrefix(p, "@executable_path/"):
			return filepath.Join(exeDir, strings.TrimPrefix(p, "@executable_path/"))
		case strings.HasPrefix(p, "@loader_path/"):
			return filepath.Join(loaderDir, strings.TrimPrefix(p, "@loader_path/"))
		}

		return p
	}

	switch {
	case strings.HasPrefix(name, "@executable_path/"), strings.HasPrefix(name, "@loader_path/"):
		return expand(name)
	case strings.HasPrefix(name, "@rpath/"):
		rest := strings.TrimPrefix(name, "@rpath/")

		first := ""
		for _, rpath := range rpaths {
			p := filepath.Join(expand(rpath), rest)

			if first == "" {
				first = p
			}

			if _, err := os.Stat(p); err == nil {
				return p
			}
		}

		return first
	case filepath.IsAbs(name):
		return name
	case strings.HasPrefix(name, "@"):
		return ""
	}

	if p, err := filepath.Abs(name); err == nil {
		return p
	}

	return ""
}

// machoWalk collects the files a set of Mach-O files load: the files
// themselves, the dylibs their load commands name, resolved as dyld
// resolves them, and those dylibs' own, recursively.
type machoWalk struct {
	exeDir string
	// the main executable's run paths, which dyld appends to every other
	// file's own when it resolves an @rpath name
	exeRpaths []string
	visited   map[string]bool
	paths     []string
	queue     []string
}

// newMachoWalk is a walk of the files the Mach-O file at root loads, root
// itself the main executable whose directory @executable_path names.
func newMachoWalk(root string) *machoWalk {
	return &machoWalk{exeDir: absDir(root), visited: map[string]bool{}}
}

// absDir is the directory of path, absolute however it was written.
func absDir(path string) string {
	dir, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return filepath.Dir(path)
	}

	return dir
}

// add takes a file into the walk, and with it the files its load commands
// name and those files' own. Root is the one file whose failing to read
// as a Mach-O file fails the walk: any other unreadable file - a dyld
// shared cache stub, or a weak dylib that never loaded - stays among the
// paths, watched for its own change, with what it would have named left
// out.
func (w *machoWalk) add(path, root string) bool {
	w.take(path)

	for len(w.queue) > 0 {
		path := w.queue[0]
		w.queue = w.queue[1:]

		info, ok := readMachO(path)
		if !ok {
			if path == root {
				return false
			}

			continue
		}

		// dyld resolves an @rpath name of a file with that file's run
		// paths, then the main executable's
		var rpaths []string
		if path == root {
			w.exeRpaths, rpaths = info.rpaths, info.rpaths
		} else {
			rpaths = append(slices.Clone(info.rpaths), w.exeRpaths...)
		}

		dir := absDir(path)

		for _, name := range info.dylibs {
			w.take(resolveDylib(name, dir, w.exeDir, rpaths))
		}
	}

	return true
}

// take takes a file once: "" names nothing.
func (w *machoWalk) take(path string) {
	if path == "" || w.visited[path] {
		return
	}

	w.visited[path] = true
	w.paths = append(w.paths, path)
	w.queue = append(w.queue, path)
}
