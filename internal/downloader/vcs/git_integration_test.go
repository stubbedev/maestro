// Real-git tests of GitDownloader against local temporary repositories:
// install, local and unpushed changes, update with stashed changes, the
// mirror cache, the VCS reference and removal.

package vcs

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/console"
	mio "github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/resolver/operation"
	"github.com/stubbedev/maestro/internal/util"
	vcsutil "github.com/stubbedev/maestro/internal/util/vcs"
)

// gitEnv isolates git from the user's configuration and resets the git
// version cache so the real binary is asked.
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
	keepEnv(t)

	vcsutil.SetVersion("", false)
	t.Cleanup(func() { vcsutil.SetVersion("", false) })
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

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	noError(t, os.WriteFile(path, []byte(content), 0o644))
}

func readFile(t *testing.T, path string) string {
	t.Helper()

	b, err := os.ReadFile(path)
	noError(t, err)

	return string(b)
}

// upstream is an origin repository: commit v1 (a.txt, b.txt) tagged
// 1.0.0, then v2 changing a.txt, on branch main.
type upstream struct {
	dir, v1, v2 string
}

func newUpstream(t *testing.T, dir string) upstream {
	t.Helper()

	noError(t, os.MkdirAll(dir, 0o755))
	git(t, dir, "init", "-q")
	git(t, dir, "symbolic-ref", "HEAD", "refs/heads/main")
	writeFile(t, filepath.Join(dir, "a.txt"), "v1\n")
	writeFile(t, filepath.Join(dir, "b.txt"), "b\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-q", "-m", "first")
	git(t, dir, "tag", "1.0.0")
	v1 := git(t, dir, "rev-parse", "HEAD")
	writeFile(t, filepath.Join(dir, "a.txt"), "v2\n")
	git(t, dir, "commit", "-q", "-am", "second <change>")

	return upstream{dir: dir, v1: v1, v2: git(t, dir, "rev-parse", "HEAD")}
}

func realGitDownloader(t *testing.T, io mio.IO, kv ...any) *GitDownloader {
	t.Helper()

	process := util.NewProcessExecutor(nil)
	process.EnableAsync()

	return NewGitDownloader(Deps{IO: io, Config: newConfig(t, kv...), Process: process})
}

func TestGitIntegration_InstallChangesUpdateRemove(t *testing.T) {
	gitEnv(t)

	up := newUpstream(t, filepath.Join(t.TempDir(), "upstream"))
	path := filepath.Join(t.TempDir(), "vendor", "dummy", "pkg")
	buffer := bufferIO(t, 0)
	d := realGitDownloader(t, buffer, "discard-changes", "stash")

	v1 := sourcePackage("1.0.0.0", "1.0.0", up.v1, up.dir)
	noError(t, run(installSteps(d, v1, path)...))

	if got := git(t, path, "rev-parse", "HEAD"); got != up.v1 {
		t.Fatalf("HEAD %s, want %s", got, up.v1)
	}

	if got := readFile(t, filepath.Join(path, "a.txt")); got != "v1\n" {
		t.Fatalf("a.txt %q", got)
	}

	if got := git(t, path, "remote", "get-url", "origin"); got != up.dir {
		t.Fatalf("origin %q", got)
	}

	changes, err := d.LocalChanges(v1, path)
	noError(t, err)

	if changes.Valid {
		t.Fatalf("unexpected changes %q", changes.S)
	}

	// a local change survives the update through stash/pop
	writeFile(t, filepath.Join(path, "b.txt"), "local\n")

	changes, err = d.LocalChanges(v1, path)
	noError(t, err)

	if changes.S != "M b.txt" {
		t.Fatalf("changes %+v", changes)
	}

	main := sourcePackage("dev-main", "dev-main", up.v2, up.dir)
	noError(t, run(updateSteps(d, v1, main, path)...))

	if got := git(t, path, "rev-parse", "HEAD"); got != up.v2 {
		t.Fatalf("HEAD %s, want %s", got, up.v2)
	}

	if got := git(t, path, "rev-parse", "--abbrev-ref", "HEAD"); got != "main" {
		t.Fatalf("branch %q", got)
	}

	if got := readFile(t, filepath.Join(path, "b.txt")); got != "local\n" {
		t.Fatalf("stashed change not reapplied: %q", got)
	}

	if out := php.NormalizeEOL(buffer.Output()); !strings.Contains(out, "Re-applying stashed changes") || !strings.Contains(out, "Checking out "+up.v2[:10]) {
		t.Fatalf("output %q", out)
	}

	// pushed state: no unpushed changes
	unpushed, err := d.UnpushedChanges(main, path)
	noError(t, err)

	if unpushed.S != "" {
		t.Fatalf("unpushed %+v", unpushed)
	}

	git(t, path, "commit", "-q", "-am", "local commit")

	unpushed, err = d.UnpushedChanges(main, path)
	noError(t, err)

	if unpushed.S != "M\tb.txt" {
		t.Fatalf("unpushed %+v", unpushed)
	}

	ref, err := d.VcsReference(main, path)
	noError(t, err)

	if head := git(t, path, "rev-parse", "HEAD"); ref.S != head {
		t.Fatalf("VcsReference %+v, want %s", ref, head)
	}

	noError(t, run(func() (*Promise, error) { return d.Remove(main, path) }))

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("%s still exists: %v", path, err)
	}
}

func TestGitIntegration_VerboseUpdateLogsCommits(t *testing.T) {
	gitEnv(t)

	up := newUpstream(t, filepath.Join(t.TempDir(), "upstream"))
	path := filepath.Join(t.TempDir(), "pkg")
	buffer := bufferIO(t, console.VerbosityVerbose)
	d := realGitDownloader(t, buffer)

	v1 := sourcePackage("1.0.0.0", "1.0.0", up.v1, up.dir)
	main := sourcePackage("dev-main", "dev-main", up.v2, up.dir)

	noError(t, run(installSteps(d, v1, path)...))
	noError(t, run(updateSteps(d, v1, main, path)...))

	short := git(t, path, "rev-parse", "--short", up.v2)
	// the console turns the escaped \< back into <
	if want := "    Pulling in changes:\n      " + short + " - t: second <change>\n      \n"; !strings.Contains(php.NormalizeEOL(buffer.Output()), want) {
		t.Fatalf("output %q lacks %q", php.NormalizeEOL(buffer.Output()), want)
	}

	// and back
	buffer2 := bufferIO(t, console.VerbosityVerbose)
	d.io = buffer2
	noError(t, run(updateSteps(d, main, v1, path)...))

	if !strings.Contains(php.NormalizeEOL(buffer2.Output()), "    Rolling back changes:\n") {
		t.Fatalf("output %q", php.NormalizeEOL(buffer2.Output()))
	}
}

func TestGitIntegration_InstallThroughMirrorCache(t *testing.T) {
	gitEnv(t)

	// a relative url is not a local path to Composer, so it is mirrored
	work := t.TempDir()
	t.Chdir(work)

	up := newUpstream(t, filepath.Join(work, "upstream.git"))
	cacheDir := filepath.Join(t.TempDir(), "vcs")
	buffer := bufferIO(t, 0)
	d := realGitDownloader(t, buffer, "cache-vcs-dir", cacheDir)

	p := sourcePackage("1.0.0.0", "1.0.0", up.v1, "upstream.git")
	path := filepath.Join(work, "vendor", "pkg")
	noError(t, run(installSteps(d, p, path)...))

	mirror := filepath.Join(cacheDir, "upstream.git")
	if got := git(t, mirror, "rev-parse", "--git-dir"); got != "." {
		t.Fatalf("no bare mirror at %s: %q", mirror, got)
	}

	if got := git(t, path, "rev-parse", "HEAD"); got != up.v1 {
		t.Fatalf("HEAD %s", got)
	}

	// --dissociate: the clone does not depend on the mirror
	if _, err := os.Stat(filepath.Join(path, ".git", "objects", "info", "alternates")); !os.IsNotExist(err) {
		t.Fatalf("alternates left behind: %v", err)
	}

	if got := git(t, path, "remote", "get-url", "composer"); got != "upstream.git" {
		t.Fatalf("composer remote %q", got)
	}

	if out := php.NormalizeEOL(buffer.Output()); !strings.Contains(out, "  - Syncing dummy/pkg (1.0.0) into cache") || !strings.Contains(out, "Cloning "+up.v1[:10]+" from cache") {
		t.Fatalf("output %q", out)
	}

	if _, err := d.Cleanup(operation.TypeInstall, p, path, nil); err != nil {
		t.Fatal(err)
	}
}

func TestGitIntegration_DiscardChanges(t *testing.T) {
	gitEnv(t)

	up := newUpstream(t, filepath.Join(t.TempDir(), "upstream"))
	path := filepath.Join(t.TempDir(), "pkg")
	d := realGitDownloader(t, bufferIO(t, 0), "discard-changes", true)

	v1 := sourcePackage("1.0.0.0", "1.0.0", up.v1, up.dir)
	main := sourcePackage("dev-main", "dev-main", up.v2, up.dir)

	noError(t, run(installSteps(d, v1, path)...))
	writeFile(t, filepath.Join(path, "a.txt"), "local\n")
	noError(t, run(updateSteps(d, v1, main, path)...))

	if got := readFile(t, filepath.Join(path, "a.txt")); got != "v2\n" {
		t.Fatalf("a.txt %q", got)
	}
}
