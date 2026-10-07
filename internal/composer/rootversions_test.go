package composer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg/loader"
	"github.com/stubbedev/maestro/internal/util"
)

// countingGuesser guesses version 1.0.0 (nothing when none), counting the
// guesses.
type countingGuesser struct {
	guesses int
	none    bool
}

func (g *countingGuesser) GuessVersion(*php.Array, string) (*loader.VersionData, error) {
	g.guesses++
	if g.none {
		return nil, nil
	}

	return &loader.VersionData{Version: "1.0.0.0", PrettyVersion: "1.0.0"}, nil
}

func (*countingGuesser) RootVersionFromEnv() (string, error) { return "", nil }

// A root package is guessed once per directory and what the guess reads
// of its configuration, however the directory is named.
func TestRootVersionGuesser_GuessesEachRootOnce(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.Mkdir(filepath.Join(dir, "other"), 0o755); err != nil {
		t.Fatal(err)
	}
	inner := &countingGuesser{}
	g := rootVersionGuesser{inner, &rootVersions{}}
	config := php.ArrayOf("name", "a/b")

	for _, path := range []string{".", dir, dir + "/"} {
		data, err := g.GuessVersion(config, path)
		if err != nil || data == nil || data.PrettyVersion != "1.0.0" {
			t.Fatalf("GuessVersion(%q) = %+v, %v", path, data, err)
		}
		// the caller's copy
		data.PrettyVersion = "changed"
	}
	// what the guess does not read
	if _, err := g.GuessVersion(php.ArrayOf("name", "a/b", "require", php.ArrayOf("c/d", "^1.0")), dir); err != nil {
		t.Fatal(err)
	}
	if inner.guesses != 1 {
		t.Errorf("the same root was guessed %d times", inner.guesses)
	}

	for _, other := range []*php.Array{
		php.ArrayOf("name", "a/b", "extra", php.ArrayOf("branch-alias", php.ArrayOf("dev-main", "1.x-dev"))),
		php.ArrayOf("name", "a/b", "require", php.ArrayOf("c/d", "self.version")),
		php.ArrayOf("name", "a/b", "non-feature-branches", php.ListOf("x")),
		php.ArrayOf("name", "a/b", "trunk-path", "t"),
	} {
		if _, err := g.GuessVersion(other, dir); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := g.GuessVersion(config, filepath.Join(dir, "other")); err != nil {
		t.Fatal(err)
	}
	if inner.guesses != 6 {
		t.Errorf("another configuration or directory was guessed %d times in all, want 6", inner.guesses)
	}
	if data, _ := g.GuessVersion(config, dir); data == nil || data.PrettyVersion != "1.0.0" {
		t.Errorf("the guess kept is %+v", data)
	}
}

// That there is no version is a guess kept too; no guess is kept across
// foreign code, or while it runs.
func TestRootVersionGuesser_ForgetsAcrossForeignCode(t *testing.T) {
	dir := t.TempDir()
	inner := &countingGuesser{none: true}
	g := rootVersionGuesser{inner, &rootVersions{}}
	config := php.ArrayOf("name", "a/b")
	guess := func() {
		t.Helper()
		if data, err := g.GuessVersion(config, dir); err != nil || data != nil {
			t.Fatalf("GuessVersion = %+v, %v", data, err)
		}
	}

	guess()
	guess()
	if inner.guesses != 1 {
		t.Fatalf("no version: guessed %d times", inner.guesses)
	}

	done := util.RunForeignCode()
	guess()
	guess()
	if inner.guesses != 3 {
		t.Fatalf("while foreign code runs: guessed %d times in all, want 3", inner.guesses)
	}
	done()
	guess()
	guess()
	if inner.guesses != 4 {
		t.Fatalf("after foreign code ran: guessed %d times in all, want 4", inner.guesses)
	}
}
