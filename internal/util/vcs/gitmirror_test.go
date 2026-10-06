// The Go reader of bare mirrors against real git.

package vcs

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/util"
)

// gitRaw is git's untrimmed stdout and exit code.
func gitRaw(t *testing.T, dir string, args ...string) (string, int) {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = dir

	out, err := cmd.Output()
	if exitErr := (*exec.ExitError)(nil); errors.As(err, &exitErr) {
		return string(out), exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}

	return string(out), 0
}

// newMirror is a bare mirror of an upstream with branches (one nested),
// lightweight and annotated tags, all packed (refs and objects), plus a
// loose branch, a loose annotated tag and loose objects added after.
func newMirror(t *testing.T) (dir string, commits []string) {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("the reader always runs git on Windows")
	}

	gitEnv(t)

	up, _ := newUpstream(t)

	// similar long messages so that repacking deltifies some commits
	long := strings.Repeat("a fairly long commit message line that repeats\n", 80)
	for i := range 12 {
		if err := os.WriteFile(filepath.Join(up, "f"), []byte(strconv.Itoa(i)), 0o644); err != nil {
			t.Fatal(err)
		}

		git(t, up, "add", ".")
		git(t, up, "commit", "-q", "-m", "commit "+strconv.Itoa(i)+"\n\n"+long)

		switch i {
		case 2:
			git(t, up, "tag", "v1.0.0")
		case 4:
			git(t, up, "tag", "-a", "-m", "annotated", "v1.1.0")
		case 6:
			git(t, up, "branch", "feature/nested")
		case 8:
			git(t, up, "branch", "1.x")
		}
	}

	dir = filepath.Join(t.TempDir(), "mirror")
	git(t, filepath.Dir(dir), "clone", "-q", "--mirror", up, dir)
	git(t, dir, "repack", "-adf", "-q", "--window=250", "--depth=50")

	// loose refs and loose objects
	tree := git(t, dir, "rev-parse", "main^{tree}")
	loose := git(t, dir, "commit-tree", "-p", "main", "-m", "loose", tree)
	git(t, dir, "update-ref", "refs/heads/loose", loose)
	git(t, dir, "tag", "-a", "-m", "loose annotated", "v2.0.0", loose)
	git(t, dir, "update-ref", "refs/heads/main", loose)

	out, _ := gitRaw(t, dir, "rev-list", "--all")

	return dir, strings.Fields(out)
}

func TestMirrorReader_MatchesGit(t *testing.T) {
	dir, commits := newMirror(t)

	if out, _ := gitRaw(t, dir, "rev-parse", "--git-dir"); out != ".\n" {
		t.Fatalf("rev-parse --git-dir %q", out)
	}

	m := openMirror(dir)
	if m == nil {
		t.Fatal("openMirror: nil for a bare mirror")
	}

	want, _ := gitRaw(t, dir, "branch")
	if got, ok := m.branchOutput(); !ok || got != want {
		t.Fatalf("branch: %q %v, git: %q", got, ok, want)
	}

	want, _ = gitRaw(t, dir, "tag")
	if got, ok := m.tagOutput(); !ok || got != want {
		t.Fatalf("tag: %q %v, git: %q", got, ok, want)
	}

	if len(commits) < 14 {
		t.Fatalf("commits %v", commits)
	}

	for _, sha := range commits {
		if _, code := gitRaw(t, dir, "rev-parse", "--quiet", "--verify", sha+"^{commit}"); code != 0 {
			t.Fatalf("git does not verify %s", sha)
		}

		if !m.isCommit(sha) {
			t.Fatalf("isCommit(%s) false", sha)
		}
	}

	// some commits are deltas in the pack, so the base chain was followed
	verify, _ := gitRaw(t, dir, "verify-pack", "-v", filepath.Join(dir, "objects", "pack", packName(t, dir)))
	deltas := 0

	for line := range strings.SplitSeq(verify, "\n") {
		if f := strings.Fields(line); len(f) == 7 && f[1] == "commit" {
			deltas++
		}
	}

	if deltas == 0 {
		t.Fatal("no deltified commit in the pack")
	}

	// not answered: a tag object (to peel), a tree, a missing object,
	// abbreviated and symbolic references
	tagObject := git(t, dir, "rev-parse", "v1.1.0")
	tree := git(t, dir, "rev-parse", "main^{tree}")

	for _, ref := range []string{tagObject, tree, strings.Repeat("0", 40), commits[0][:12], "main", "v1.0.0", strings.ToUpper(commits[0])} {
		if m.isCommit(ref) {
			t.Fatalf("isCommit(%s) true", ref)
		}
	}
}

func packName(t *testing.T, dir string) string {
	t.Helper()

	matches, _ := filepath.Glob(filepath.Join(dir, "objects", "pack", "*.idx"))
	if len(matches) != 1 {
		t.Fatalf("packs %v", matches)
	}

	return filepath.Base(matches[0])
}

func TestMirrorReader_LooseOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the reader always runs git on Windows")
	}

	gitEnv(t)

	up, head := newUpstream(t)
	dir := filepath.Join(t.TempDir(), "mirror")
	git(t, filepath.Dir(dir), "clone", "-q", "--mirror", up, dir)
	// a local clone copies the objects loose
	if matches, _ := filepath.Glob(filepath.Join(dir, "objects", "pack", "*.idx")); len(matches) != 0 {
		t.Fatalf("packs %v", matches)
	}

	// no packed-refs
	_ = os.Remove(filepath.Join(dir, "packed-refs"))
	git(t, dir, "update-ref", "refs/heads/main", head)

	m := openMirror(dir)
	if m == nil {
		t.Fatal("openMirror nil")
	}

	if !m.isCommit(head) {
		t.Fatal("loose commit not found")
	}

	want, _ := gitRaw(t, dir, "branch")
	if got, ok := m.branchOutput(); !ok || got != want {
		t.Fatalf("branch: %q %v, git: %q", got, ok, want)
	}

	want, _ = gitRaw(t, dir, "tag")
	if got, ok := m.tagOutput(); !ok || got != want {
		t.Fatalf("tag: %q %v, git: %q", got, ok, want)
	}
}

// Everything the reader does not know for sure is left to git.
func TestMirrorReader_DefersToGit(t *testing.T) {
	type check func(*testing.T, string)

	notOpened := func(t *testing.T, dir string) {
		t.Helper()

		if openMirror(dir) != nil {
			t.Fatal("openMirror answered")
		}
	}

	noBranches := func(t *testing.T, dir string) {
		t.Helper()

		m := openMirror(dir)
		if m == nil {
			t.Fatal("openMirror nil")
		}

		if _, ok := m.branchOutput(); ok {
			t.Fatal("branchOutput answered")
		}
	}

	cases := map[string]struct {
		change func(*testing.T, string)
		check  check
	}{
		"detached HEAD": {func(t *testing.T, dir string) {
			writeFile(t, filepath.Join(dir, "HEAD"), git(t, dir, "rev-parse", "main")+"\n")
		}, notOpened},
		"HEAD names a tag": {func(t *testing.T, dir string) {
			writeFile(t, filepath.Join(dir, "HEAD"), "ref: refs/tags/v1.0.0\n")
		}, notOpened},
		"alternates": {func(t *testing.T, dir string) {
			writeFile(t, filepath.Join(dir, "objects", "info", "alternates"), "/nonexistent\n")
		}, notOpened},
		"extensions": {func(t *testing.T, dir string) {
			git(t, dir, "config", "extensions.worktreeConfig", "true")
		}, notOpened},
		"include": {func(t *testing.T, dir string) {
			git(t, dir, "config", "include.path", "/nonexistent")
		}, notOpened},
		"not bare": {func(t *testing.T, dir string) {
			git(t, dir, "config", "core.bare", "false")
		}, notOpened},
		"replace refs": {func(t *testing.T, dir string) {
			git(t, dir, "replace", git(t, dir, "rev-parse", "main"), git(t, dir, "rev-parse", "loose"))
		}, notOpened},
		"packed replace refs": {func(t *testing.T, dir string) {
			git(t, dir, "replace", git(t, dir, "rev-parse", "main~2"), git(t, dir, "rev-parse", "main~3"))
			git(t, dir, "pack-refs", "--all")
			_ = os.RemoveAll(filepath.Join(dir, "refs", "replace"))
		}, func(t *testing.T, dir string) {
			t.Helper()

			m := openMirror(dir)
			if m == nil {
				t.Fatal("openMirror nil")
			}

			if m.isCommit(git(t, dir, "rev-parse", "main~3")) {
				t.Fatal("isCommit answered")
			}
		}},
		"symbolic branch": {func(t *testing.T, dir string) {
			git(t, dir, "symbolic-ref", "refs/heads/alias", "refs/heads/main")
		}, noBranches},
		"dangling branch": {func(t *testing.T, dir string) {
			writeFile(t, filepath.Join(dir, "refs", "heads", "dangling"), strings.Repeat("1", 40)+"\n")
		}, noBranches},
		"odd branch name": {func(t *testing.T, dir string) {
			git(t, dir, "update-ref", "refs/heads/café", "main")
		}, noBranches},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			dir, _ := newMirror(t)
			c.change(t, dir)
			c.check(t, dir)
		})
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMirrorReader_ConfigProbe(t *testing.T) {
	for _, c := range []struct {
		key, value string
		safe       bool
	}{
		{"user.name", "x", true},
		{"color.ui", "auto", true},
		{"color.ui", "", true},
		{"color.branch", "always", false},
		{"column.ui", "always", false},
		{"includeif.gitdir:~/x.path", "y", false},
		{"safe.barerepository", "explicit", false},
		{"safe.barerepository", "all", true},
	} {
		if got := mirrorConfigSafe(c.key, c.value); got != c.safe {
			t.Errorf("%s=%s: %v", c.key, c.value, got)
		}
	}
}

// A disagreement with git turns the reader off for that answer.
func TestMirrorReader_Calibration(t *testing.T) {
	fio := newFakeIO()
	g := NewGit(fio, newFakeConfig(nil), util.NewProcessExecutor(nil), util.NewFilesystem(nil))

	g.calibrate(queryBranch, true)

	if !g.trusted(queryBranch) || g.trusted(queryTag) {
		t.Fatal("trust after agreement")
	}

	g.calibrate(queryBranch, false)
	g.calibrate(queryBranch, true)

	if g.trusted(queryBranch) {
		t.Fatal("trusted after disagreement")
	}

	if len(fio.writes) != 1 {
		t.Fatalf("writes %q", fio.writes)
	}
}
