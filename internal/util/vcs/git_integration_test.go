// Real-git tests of Git's bare-mirror cache (syncMirror,
// fetchRefOrSyncMirror) against local temporary repositories.

package vcs

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/stubbedev/maestro/internal/util"
)

// recordingProcess runs commands with a real ProcessExecutor and records
// them (with their cwd) for checking the exact commands run.
type recordingProcess struct {
	*util.ProcessExecutor

	mu  sync.Mutex
	log []string
}

func (r *recordingProcess) record(command util.Command, cwd string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.log = append(r.log, commandString(command)+" @"+cwd)
}

func (r *recordingProcess) Execute(command util.Command, output *string, cwd string) (int, error) {
	r.record(command, cwd)

	return r.ProcessExecutor.Execute(command, output, cwd)
}

func (r *recordingProcess) ExecuteFunc(command util.Command, handler func(typ, buffer string), cwd string) (int, error) {
	r.record(command, cwd)

	return r.ProcessExecutor.ExecuteFunc(command, handler, cwd)
}

func (r *recordingProcess) take() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	log := r.log
	r.log = nil

	return log
}

func gitEnv(t *testing.T) {
	t.Helper()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@example.org")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@example.org")
	t.Setenv("COMPOSER_DISABLE_NETWORK", "")
	// CleanEnv changes these; t.Setenv restores them
	t.Setenv("GIT_TERMINAL_PROMPT", "")
	t.Setenv("LANGUAGE", "")
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = dir

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}

	return strings.TrimSpace(string(out))
}

func newUpstream(t *testing.T) (dir, head string) {
	t.Helper()

	dir = t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")

	if err := os.WriteFile(filepath.Join(dir, "composer.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	git(t, dir, "add", ".")
	git(t, dir, "commit", "-q", "-m", "first")

	return dir, git(t, dir, "rev-parse", "HEAD")
}

// The mirror cache with the Go reader answering where it can, and with
// git asked everything, as on Windows, where the reader is off.
func TestGitIntegration_SyncMirrorAndFetchRef(t *testing.T) {
	t.Run("reader", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("the reader always runs git on Windows")
		}

		testSyncMirrorAndFetchRef(t, true)
	})
	t.Run("git", func(t *testing.T) {
		// any GIT_TEST_ variable turns the reader off
		t.Setenv("GIT_TEST_MAESTRO_NO_READER", "1")
		testSyncMirrorAndFetchRef(t, false)
	})
}

func testSyncMirrorAndFetchRef(t *testing.T, reader bool) {
	gitEnv(t)

	upstream, first := newUpstream(t)
	cache := filepath.Join(t.TempDir(), "cache-vcs", "repo")
	process := &recordingProcess{ProcessExecutor: util.NewProcessExecutor(nil)}

	if err := CleanEnv(process); err != nil {
		t.Fatal(err)
	}

	if v, ok, err := GetVersion(process); err != nil || !ok || v == "" {
		t.Fatalf("git version %q %v %v", v, ok, err)
	}

	process.take()

	g := NewGit(newFakeIO(), newFakeConfig(nil), process, util.NewFilesystem(nil))

	// initial clone into a fresh mirror
	ok, err := g.SyncMirror(upstream, cache)
	if err != nil || !ok {
		t.Fatalf("SyncMirror: %v %v", ok, err)
	}

	if got := git(t, cache, "rev-parse", "--git-dir"); got != "." {
		t.Fatalf("not a bare mirror: %q", got)
	}

	if got := git(t, cache, "config", "remote.origin.mirror"); got != "true" {
		t.Fatalf("remote.origin.mirror %q", got)
	}

	if got, want := process.take(), []string{
		"git rev-parse --git-dir @" + cache,
		"git clone --mirror -- " + upstream + " " + cache + " @",
		"git remote -v @" + cache,
		"git remote set-url origin -- " + upstream + " @" + cache,
	}; !slices.Equal(got, want) {
		// the first rev-parse only runs when the directory exists
		if !slices.Equal(got, want[1:]) {
			t.Fatalf("commands:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want[1:], "\n"))
		}
	}

	// a ref already in the mirror: no sync; the first mirror the Go
	// reader may read is also asked of git, and the configuration probed
	ok, err = g.FetchRefOrSyncMirror(upstream, cache, first, "")
	if err != nil || !ok {
		t.Fatalf("FetchRefOrSyncMirror: %v %v", ok, err)
	}

	gitDir := "git rev-parse --git-dir @" + cache
	verify := func(ref string) string { return "git rev-parse --quiet --verify " + ref + "^{commit} @" + cache }

	want := []string{gitDir, verify(first)}
	if reader {
		want = append([]string{"git config --list --show-origin -z @" + cache}, want...)
	}

	if got := process.take(); !slices.Equal(got, want) {
		t.Fatalf("commands:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	// a new commit upstream: the mirror is updated
	if err := os.WriteFile(filepath.Join(upstream, "b"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}

	git(t, upstream, "add", ".")
	git(t, upstream, "commit", "-q", "-m", "second")
	second := git(t, upstream, "rev-parse", "HEAD")

	ok, err = g.FetchRefOrSyncMirror(upstream, cache, second, "")
	if err != nil || !ok {
		t.Fatalf("FetchRefOrSyncMirror: %v %v", ok, err)
	}

	// from now on the reader answers: git only verifies what it does not
	// hold, and syncs
	syncCmds := []string{
		"git remote -v @" + cache,
		"git remote set-url origin -- " + upstream + " @" + cache,
		"git remote update --prune origin @" + cache,
		"git gc --auto @" + cache,
		"git remote -v @" + cache,
		"git remote set-url origin -- " + upstream + " @" + cache,
	}
	if reader {
		want = append([]string{verify(second)}, syncCmds...)
	} else {
		want = slices.Concat([]string{gitDir, verify(second), gitDir}, syncCmds, []string{gitDir, verify(second)})
	}

	if got := process.take(); !slices.Equal(got, want) {
		t.Fatalf("commands:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	// a tag created after the ref was cached: the sha is in the mirror
	// but the pretty version is unknown, so the mirror is synced
	git(t, upstream, "tag", "v1.2.0", first)

	ok, err = g.FetchRefOrSyncMirror(upstream, cache, first, "1.2.0")
	if err != nil || !ok {
		t.Fatalf("FetchRefOrSyncMirror: %v %v", ok, err)
	}

	if got := git(t, cache, "tag"); got != "v1.2.0" {
		t.Fatalf("tags %q", got)
	}

	log := process.take()
	if !slices.Contains(log, "git remote update --prune origin @"+cache) {
		t.Fatalf("mirror not synced:\n%s", strings.Join(log, "\n"))
	}

	// a known branch: no sync
	ok, err = g.FetchRefOrSyncMirror(upstream, cache, second, "dev-main")
	if err != nil || !ok {
		t.Fatalf("FetchRefOrSyncMirror: %v %v", ok, err)
	}

	if log := process.take(); slices.Contains(log, "git remote update --prune origin @"+cache) {
		t.Fatalf("mirror synced:\n%s", strings.Join(log, "\n"))
	}

	// git branch and git tag were compared once; now neither runs
	ok, err = g.FetchRefOrSyncMirror(upstream, cache, second, "dev-main")
	if err != nil || !ok {
		t.Fatalf("FetchRefOrSyncMirror: %v %v", ok, err)
	}

	want = nil
	if !reader {
		want = []string{gitDir, verify(second), "git branch @" + cache, "git tag @" + cache}
	}

	if got := process.take(); !slices.Equal(got, want) {
		t.Fatalf("commands:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	// a ref that does not exist anywhere
	ok, err = g.FetchRefOrSyncMirror(upstream, cache, strings.Repeat("0", 40), "")
	if err != nil || ok {
		t.Fatalf("FetchRefOrSyncMirror(missing): %v %v", ok, err)
	}

	// the default branch of the remote
	if branch, ok := g.GetMirrorDefaultBranch(upstream, cache, true); !ok || branch != "main" {
		t.Fatalf("default branch %q %v", branch, ok)
	}
}

func TestGitIntegration_SyncMirrorReplacesNonRepository(t *testing.T) {
	gitEnv(t)

	upstream, head := newUpstream(t)
	cache := t.TempDir()

	if err := os.WriteFile(filepath.Join(cache, "junk"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	process := util.NewProcessExecutor(nil)
	g := NewGit(newFakeIO(), newFakeConfig(nil), process, util.NewFilesystem(process))

	if ok, err := g.FetchRefOrSyncMirror(upstream, cache, head, ""); err != nil || !ok {
		t.Fatalf("FetchRefOrSyncMirror: %v %v", ok, err)
	}

	if _, err := os.Stat(filepath.Join(cache, "junk")); !os.IsNotExist(err) {
		t.Fatalf("junk kept: %v", err)
	}
}

func TestGitIntegration_RunCommandsFailure(t *testing.T) {
	gitEnv(t)

	missing := filepath.Join(t.TempDir(), "missing")
	process := util.NewProcessExecutor(nil)
	g := NewGit(newFakeIO(), newFakeConfig(nil), process, util.NewFilesystem(process))

	err := g.RunCommands([][]string{{"git", "clone", "--", "%url%", filepath.Join(t.TempDir(), "x")}}, missing, "", true, nil)
	if err == nil || !strings.HasPrefix(err.Error(), "Failed to execute git clone -- "+missing+" ") {
		t.Fatalf("got %v", err)
	}
}

func TestGitIntegration_SyncMirrorNetworkDisabled(t *testing.T) {
	gitEnv(t)
	t.Setenv("COMPOSER_DISABLE_NETWORK", "1")

	fio := newFakeIO()
	g := NewGit(fio, newFakeConfig(nil), util.NewProcessExecutor(nil), util.NewFilesystem(nil))

	if ok, err := g.SyncMirror("https://u:p@example.org/r.git", t.TempDir()); err != nil || ok {
		t.Fatalf("got %v %v", ok, err)
	}

	if want := "<warning>Aborting git mirror sync of https://u:***@example.org/r.git as network is disabled</warning>"; len(fio.writes) != 1 || fio.writes[0] != want {
		t.Fatalf("writes %q", fio.writes)
	}
}
