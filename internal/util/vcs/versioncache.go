// Ports nothing: git's version kept across runs (deliberate deviation 3,
// speed).

package vcs

import (
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/fsstate"
)

// keptGitVersion is where and how GetVersion keeps git's version across
// runs; nil or a dir of "" for nowhere (UseVersionCache).
var keptGitVersion atomic.Pointer[versionCacheConfig]

type versionCacheConfig struct {
	dir string
	// trust is how much older than an entry the binary's modification
	// and change times must be for the entry to be written: a binary
	// replaced within the timestamps' granularity may not show it.
	trust fsstate.Margin
}

// UseVersionCache makes GetVersion keep the version of git in dir for
// later runs, "" nowhere (the default). Only a *util.ProcessExecutor's
// runs use it (never a test's mock), and only while it does not log the
// commands it runs (-vvv), whose log then shows `git --version` run as
// Composer's does. An entry is keyed on the git binary the executor would
// start, which must be a binary named git, not a script, and its file's
// identity (device, inode, mode, size, modification and change times);
// it is used for a day at most (versionCacheMaxAge). Linux only (on
// macOS /usr/bin/git is a shim running the selected Xcode's git, and on
// Windows git.exe is often a shim too).
func UseVersionCache(dir string) {
	keptGitVersion.Store(&versionCacheConfig{dir: dir})
}

// versionCacheMaxAge is how long a kept git version is used at most,
// however unchanged the binary looks.
const versionCacheMaxAge = 24 * time.Hour

// versionFormat is the version of the entries; its Header starts one, the
// version follows it.
var versionFormat = fsstate.Format{Name: "git-version", Version: 5}

// executor is a Process that runs commands with a *util.ProcessExecutor
// (VersionGuesser's adapter).
type executor interface {
	Executor() *util.ProcessExecutor
}

// versionCachePath is where the kept version of git, as process runs it,
// is, "" when it is not kept (see UseVersionCache).
func versionCachePath(process Process) string {
	c := keptGitVersion.Load()
	if c == nil || c.dir == "" {
		return ""
	}
	p, ok := process.(*util.ProcessExecutor)
	if e, isExecutor := process.(executor); !ok && isExecutor {
		p, ok = e.Executor(), true
	}
	if !ok || p == nil || p.LogsCommands() {
		return ""
	}
	key := gitBinaryKey(util.DirectToolPath("git"), time.Now(), c.trust)
	if key == "" {
		return ""
	}

	return filepath.Join(c.dir, key)
}

// loadVersion returns the version kept at path, if any.
func loadVersion(path string) (string, bool) {
	if path == "" {
		return "", false
	}
	info, err := os.Stat(path)
	if err != nil || time.Since(info.ModTime()) > versionCacheMaxAge {
		return "", false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	version, ok := strings.CutPrefix(string(data), versionFormat.Header())
	if !ok || version == "" || strings.ContainsAny(version, "\n\x00") {
		return "", false
	}

	return version, true
}

// storeVersion keeps version at path; failures only lose it.
func storeVersion(path, version string) {
	if path == "" {
		return
	}
	_ = fsstate.WriteAtomic(path, []byte(versionFormat.Header()+version))
}
