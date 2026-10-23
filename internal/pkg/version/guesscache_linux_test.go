package version

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/loader"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/fsstate"
	"github.com/stubbedev/maestro/internal/util/vcs"
)

// guessRepo is a git checkout with one commit on main, guessed in with
// the guesses kept in a cache of the test's own that trusts every file,
// in an environment of the test's own (no configuration of the user's).
func guessRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git in PATH")
	}
	for _, name := range [...]string{"GIT_TERMINAL_PROMPT", "GIT_ASKPASS", "LANGUAGE", "DYLD_LIBRARY_PATH"} {
		t.Setenv(name, os.Getenv(name))
	}
	for _, name := range gitLocationEnv {
		t.Setenv(name, "")
		os.Unsetenv(name)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	keptGuesses.Store(&guessCacheConfig{dir: t.TempDir(), trust: fsstate.Margin(-time.Hour)})
	vcs.SetVersion("", false)
	t.Cleanup(func() {
		UseGuessCache("")
		vcs.SetVersion("", false)
	})

	repo, ok := php.Realpath(t.TempDir())
	if !ok {
		t.Fatal("no real path for the checkout")
	}
	git(t, repo, "init", "-q", "-b", "main")
	git(t, repo, "commit", "-q", "--allow-empty", "-m", "first")

	return repo
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.org", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// guessIn guesses the version of the checkout at dir with real processes,
// logging the commands they run when debug is set.
func guessIn(t *testing.T, dir string, debug bool) *loader.VersionData {
	t.Helper()
	var out io.IO
	if debug {
		buffer, err := io.NewBufferIO("", int(console.VerbosityDebug), nil)
		if err != nil {
			t.Fatal(err)
		}
		out = buffer
	}
	process := util.NewProcessExecutor(out)
	process.EnableAsync()
	data, err := NewVersionGuesser(NewProcessExecutor(process), nil).GuessVersion(php.NewArray(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if data == nil {
		t.Fatal("no version guessed")
	}

	return data
}

// planted is the version plantGuess keeps: one no git would answer, so a
// guess returning it took the kept entry without running git.
var planted = &loader.VersionData{Version: "9.9.9.9", PrettyVersion: "planted", Commit: pkg.Str("feedface")}

// plantGuess replaces the kept guess of the checkout at dir, for the
// repository as it is now, with planted.
func plantGuess(t *testing.T, dir string) {
	t.Helper()
	kept, ok := keptGuessFor(dir)
	if !ok {
		t.Fatal("guesses in the checkout are not kept")
	}
	if !kept.holds() {
		t.Fatal("the guess was not kept")
	}
	kept.store(newGuessConfig(php.NewArray()).key(), planted)
}

// A kept guess is taken while the repository is unchanged, and leaves the
// environment the guess leaves; every change of what git would answer
// from forces a new guess.
func TestGuessCache_ChangesForceAGuess(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(t *testing.T, repo string)
	}{
		{"commit", func(t *testing.T, repo string) {
			git(t, repo, "commit", "-q", "--allow-empty", "-m", "second")
		}},
		{"new tag", func(t *testing.T, repo string) { git(t, repo, "tag", "v1.0.0") }},
		{"branch switch", func(t *testing.T, repo string) { git(t, repo, "checkout", "-q", "-b", "feature") }},
		{"packed refs", func(t *testing.T, repo string) { git(t, repo, "pack-refs", "--all") }},
		{"detached HEAD", func(t *testing.T, repo string) { git(t, repo, "checkout", "-q", "--detach") }},
		{"remote branch", func(t *testing.T, repo string) { git(t, repo, "update-ref", "refs/remotes/origin/main", "HEAD") }},
		{"repository config", func(t *testing.T, repo string) { git(t, repo, "config", "core.abbrev", "12") }},
		{"global config", func(t *testing.T, _ string) {
			if err := os.WriteFile(filepath.Join(os.Getenv("HOME"), ".gitconfig"), []byte("[core]\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
		{"git environment", func(t *testing.T, _ string) { t.Setenv("GIT_CONFIG_COUNT", "0") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := guessRepo(t)
			guessed := guessIn(t, repo, false)
			plantGuess(t, repo)

			os.Unsetenv("GIT_TERMINAL_PROMPT")
			if got := guessIn(t, repo, false); *got != *planted {
				t.Fatalf("an unchanged repository guessed %+v, not the kept %+v", got, planted)
			}
			if v, _ := os.LookupEnv("GIT_TERMINAL_PROMPT"); v != "0" {
				t.Errorf("a kept guess left GIT_TERMINAL_PROMPT=%q, the guess sets 0", v)
			}

			tc.change(t, repo)
			if got := guessIn(t, repo, false); *got == *planted {
				t.Fatalf("the kept guess was taken after the change")
			}
			if tc.name == "commit" && guessIn(t, repo, false).Commit == guessed.Commit {
				t.Error("the commit is not the new HEAD's")
			}
		})
	}
}

// A guess is neither kept nor taken where git's answers are not what the
// key covers, or where its commands are logged (-vvv).
func TestGuessCache_NotKept(t *testing.T) {
	t.Run("debug output logs the commands", func(t *testing.T) {
		repo := guessRepo(t)
		guessIn(t, repo, false)
		plantGuess(t, repo)
		if got := guessIn(t, repo, true); *got == *planted {
			t.Fatal("a guess logging its commands took the kept one")
		}
	})

	t.Run("GIT_DIR", func(t *testing.T) {
		repo := guessRepo(t)
		t.Setenv("GIT_DIR", filepath.Join(repo, ".git"))
		if _, ok := keptGuessFor(repo); ok {
			t.Fatal("guesses are kept with GIT_DIR set")
		}
	})

	t.Run("linked worktree", func(t *testing.T) {
		repo := guessRepo(t)
		worktree := filepath.Join(t.TempDir(), "wt")
		git(t, repo, "worktree", "add", "-q", worktree)
		dir, _ := php.Realpath(worktree)
		if _, ok := keptGuessFor(dir); ok {
			t.Fatal("guesses are kept in a linked worktree")
		}
	})

	t.Run("no repository", func(t *testing.T) {
		guessRepo(t)
		dir, _ := php.Realpath(t.TempDir())
		if _, ok := keptGuessFor(dir); ok {
			t.Fatal("guesses are kept outside a repository")
		}
	})

	t.Run("subdirectory", func(t *testing.T) {
		repo := guessRepo(t)
		sub := filepath.Join(repo, "app")
		if err := os.Mkdir(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		if _, ok := keptGuessFor(sub); !ok {
			t.Fatal("guesses are not kept in a checkout's subdirectory")
		}
	})
}

// A guess is not kept while a file it depends on is too recent to tell a
// later change from it.
func TestGuessCache_RecentFilesNotKept(t *testing.T) {
	repo := guessRepo(t)
	keptGuesses.Store(&guessCacheConfig{dir: keptGuesses.Load().dir, trust: fsstate.Margin(time.Hour)})
	guessIn(t, repo, false)
	if kept, _ := keptGuessFor(repo); kept.holds() {
		t.Fatal("a guess was kept from files changed within the margin")
	}
}

// What an entry holds reads back as written, null and empty strings
// apart, and is taken only for the configuration it was guessed for.
func TestGuessCache_Entry(t *testing.T) {
	g := keptGuess{path: filepath.Join(t.TempDir(), "entry"), state: []byte("state"), trusted: true}
	data := &loader.VersionData{
		Version:              "1.0.x-dev",
		PrettyVersion:        "dev-1.0",
		Commit:               pkg.Str(""),
		FeatureVersion:       pkg.NullString{},
		FeaturePrettyVersion: pkg.Str("dev-feature"),
	}
	g.store("config", data)
	got, ok := g.load("config")
	if !ok || *got != *data {
		t.Fatalf("read back %+v (%v), want %+v", got, ok, data)
	}
	if _, ok := g.load("other"); ok {
		t.Error("taken for another configuration")
	}
	if other := (keptGuess{path: g.path, state: []byte("other")}); other.holds() {
		t.Error("taken for another state")
	}

	contents, err := os.ReadFile(g.path)
	if err != nil {
		t.Fatal(err)
	}
	for n := range contents {
		if err := os.WriteFile(g.path, contents[:n], 0o644); err != nil {
			t.Fatal(err)
		}
		if _, ok := g.load("config"); ok {
			t.Fatalf("a truncated entry (%d of %d bytes) was taken", n, len(contents))
		}
	}
}
