// Ports nothing: see guesscache.go.

package version

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/stubbedev/maestro/internal/util/fsstate"
	"github.com/stubbedev/maestro/internal/util/vcs"
)

// gitLocationEnv are the variables that point git elsewhere than the .git
// directory repoState finds above the checkout, or stop its search: with
// any of them set, guesses are not kept.
var gitLocationEnv = [...]string{
	"GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR",
	"GIT_CEILING_DIRECTORIES", "GIT_DISCOVERY_ACROSS_FILESYSTEM",
}

// gitConfigEnv are the variables, besides git's own GIT_*, that decide
// which configuration files git reads and the language of what it prints.
var gitConfigEnv = [...]string{"HOME", "XDG_CONFIG_HOME", "LANG", "LC_ALL", "LC_MESSAGES"}

// repoGitFiles are the files of a .git directory git's answers to the
// guess depend on: what HEAD is, the packed refs, the history's shallow
// and graft boundaries, the repository's configuration and a reftable's
// list of tables.
var repoGitFiles = [...]string{"HEAD", "packed-refs", "shallow", "config", "info/grafts", "reftable/tables.list"}

// repoState adds to k everything git's answers to the guess in the
// checkout at dir, a real path, depend on: the git binary, the
// environment it reads, its global and system configuration files, the
// owner of dir (git refuses a repository of another), and the files and
// ref directories of the .git directory git finds above dir. Every ref
// update replaces a file (by rename), which changes its directory's
// identity, so the refs are known by their directories'. It reports
// whether every file it saw is old enough to be trusted (margin), and
// false for ok when guesses in dir are not kept: a variable of
// gitLocationEnv is set, the git binary is not keyed (vcs.KeyGitBinary),
// or no .git directory is found on dir's file system (a .git file, of a
// linked worktree or a submodule, included).
func repoState(k *fsstate.KeyHash, dir string, now time.Time, margin fsstate.Margin) (trusted, ok bool) {
	for _, name := range gitLocationEnv {
		if _, set := os.LookupEnv(name); set {
			return false, false
		}
	}
	gitDir, owner, ok := findGitDir(dir)
	if !ok || !vcs.KeyGitBinary(k, now, margin) {
		return false, false
	}

	k.String(dir)
	k.String(gitDir)
	k.String(strconv.FormatUint(uint64(owner), 10))
	k.String(strconv.Itoa(os.Geteuid()))

	env := os.Environ()
	slices.Sort(env)
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if (strings.HasPrefix(name, "GIT_") && !vcs.CleanEnvSets(name)) || slices.Contains(gitConfigEnv[:], name) {
			k.String(kv)
		}
	}

	trusted = true
	add := func(path string, id fsstate.ID, found bool) {
		k.String(path)
		if !found {
			k.String("absent")

			return
		}
		k.String("present")
		k.ID(id)
		trusted = trusted && id.Trusted(now, margin)
	}
	stat := func(path string) {
		id, found := fsstate.Stat(path)
		add(path, id, found)
	}

	for _, path := range configFiles() {
		stat(path)
	}
	for _, name := range repoGitFiles {
		stat(filepath.Join(gitDir, filepath.FromSlash(name)))
	}
	for _, path := range refDirs(filepath.Join(gitDir, "refs")) {
		id, found := fsstate.Lstat(path)
		add(path, id, found)
	}

	return trusted, true
}

// findGitDir returns the .git directory git finds for dir, and dir's
// owner: the nearest one in dir or above it, on dir's file system (git
// stops where the device changes, a btrfs subvolume's boundary included).
// ok is false for none, or for a .git that is not a directory.
func findGitDir(dir string) (gitDir string, owner uint32, ok bool) {
	info, err := os.Stat(dir)
	if err != nil {
		return "", 0, false
	}
	st, isUnix := info.Sys().(*syscall.Stat_t)
	if !isUnix {
		return "", 0, false
	}
	for d := dir; ; {
		gitDir := filepath.Join(d, ".git")
		if info, err := os.Lstat(gitDir); err == nil {
			return gitDir, st.Uid, info.IsDir()
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "", 0, false
		}
		if id, found := fsstate.Stat(parent); !found || id.Dev != st.Dev {
			return "", 0, false
		}
		d = parent
	}
}

// configFiles are the global and system configuration files git reads,
// as git finds them.
func configFiles() []string {
	var files []string
	if global, set := os.LookupEnv("GIT_CONFIG_GLOBAL"); set {
		files = append(files, global)
	} else {
		home := os.Getenv("HOME")
		xdg := os.Getenv("XDG_CONFIG_HOME")
		if xdg == "" {
			xdg = filepath.Join(home, ".config")
		}
		files = append(files, filepath.Join(home, ".gitconfig"), filepath.Join(xdg, "git", "config"))
	}
	if system, set := os.LookupEnv("GIT_CONFIG_SYSTEM"); set {
		return append(files, system)
	}

	return append(files, "/etc/gitconfig")
}

// refDirs are root and every directory below it, in a stable order.
func refDirs(root string) []string {
	dirs := []string{root}
	for i := 0; i < len(dirs); i++ {
		entries, err := os.ReadDir(dirs[i])
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				dirs = append(dirs, filepath.Join(dirs[i], e.Name()))
			}
		}
	}

	return dirs
}
