//go:build windows

// Ports nothing: see probecache.go, whose entries on Windows are keyed on
// the modules the probing process loaded (probe_windows.go).

package platform

import (
	"os"
	"strconv"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/stubbedev/maestro/internal/util/fsstate"
)

// maxProbeCacheAge is the usual age: the modules of the process that
// answered (a day of upgrades short of one) are the whole truth on
// Windows.
const maxProbeCacheAge = 24 * time.Hour

// probeMappedFiles is what the probing process loaded: the modules
// Windows listed while php waited for its standard input to end
// (probe_windows.go).
func probeMappedFiles(s *Snapshot, _ string, _ []string) ([]string, bool) {
	return s.mappedFiles, s.hasMappedFiles
}

// unameString is what identifies the running system, which the snapshot
// records php_uname() of: the computer name, the Windows version and its
// build (RtlGetVersion, which reports the system whatever the process's
// manifest asks compatibility for). What an update changes in the files
// the entry keeps, the system libraries among the modules, their
// signatures tell; this is what they cannot.
func unameString() string {
	v := windows.RtlGetVersion()
	host, _ := os.Hostname()

	return "Windows\x00" + host + "\x00" + strconv.FormatUint(uint64(v.MajorVersion), 10) + "." +
		strconv.FormatUint(uint64(v.MinorVersion), 10) + "." + strconv.FormatUint(uint64(v.BuildNumber), 10)
}

// probeWrapper reports whether the php that answered ran as the binary
// started. A wrapper on Windows spawns its php (scoop's and chocolatey's
// shims do) instead of replacing itself with it, so the modules listed
// are the wrapper's, not the php's that answered, and the files its
// result depends on cannot be told: cacheable is false then. php reports
// the binary it runs as (PHP_BINARY), which is compared with the one
// started by identity: the same file's names differ in case and 8.3
// forms. wrapper is always false: an entry keyed on what a wrapper reads
// needs one whose php's files are known, which exec() gives Linux and
// CreateProcess does not.
func probeWrapper(s *Snapshot, resolved string) (wrapper, cacheable bool) {
	started, startedOK := fsstate.Stat(resolved)
	answered, answeredOK := fsstate.Stat(s.PHPBinary())

	return false, startedOK && answeredOK && started == answered
}

// probeIniDirs appends to dirs where else php looks for its ini files on
// Windows (php_init_config): the IniFilePath the registry names (the
// first of php's keys that exists wins, and a key without the value
// names no directory), and the Windows directories, the user's and the
// system's, which a terminal-services session tells apart.
func probeIniDirs(dirs []string, s *Snapshot) []string {
	for _, name := range phpRegistryKeys(s) {
		key, err := registry.OpenKey(registry.LOCAL_MACHINE, name, registry.QUERY_VALUE)
		if err != nil {
			continue
		}

		path, _, err := key.GetStringValue("IniFilePath")
		_ = key.Close()

		if err == nil && path != "" {
			dirs = append(dirs, path)
		}

		break
	}

	if dir, err := windows.GetWindowsDirectory(); err == nil {
		dirs = append(dirs, dir)
	}

	if dir, err := windows.GetSystemWindowsDirectory(); err == nil {
		dirs = append(dirs, dir)
	}

	return dirs
}

// phpRegistryKeys is the registry keys php opens for IniFilePath, most
// specific first: the running php's version as PHP_VERSION spells it,
// its numeric forms, then the unversioned one.
func phpRegistryKeys(s *Snapshot) []string {
	keys := []string{`SOFTWARE\PHP\` + s.Version}

	if s.VersionID > 0 {
		major, rest := s.VersionID/10000, s.VersionID%10000
		minor, patch := rest/100, rest%100
		num := func(v int64) string { return strconv.FormatInt(v, 10) }

		keys = append(keys,
			`SOFTWARE\PHP\`+num(major)+"."+num(minor)+"."+num(patch),
			`SOFTWARE\PHP\`+num(major)+"."+num(minor),
			`SOFTWARE\PHP\`+num(major))
	}

	return append(keys, `SOFTWARE\PHP`)
}
