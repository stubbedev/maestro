package composer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg/loader"
)

// countingGuesser guesses version 1.0.0, counting the guesses.
type countingGuesser struct{ guesses int }

func (g *countingGuesser) GuessVersion(*php.Array, string) (*loader.VersionData, error) {
	g.guesses++

	return &loader.VersionData{Version: "1.0.0.0", PrettyVersion: "1.0.0"}, nil
}

func (*countingGuesser) RootVersionFromEnv() (string, error) { return "", nil }

// A root package is guessed once per directory and configuration, however
// the directory is named.
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
	}
	if inner.guesses != 1 {
		t.Errorf("the same root was guessed %d times", inner.guesses)
	}

	if _, err := g.GuessVersion(php.ArrayOf("name", "a/b", "extra", php.ArrayOf("x", 1)), dir); err != nil {
		t.Fatal(err)
	}
	if _, err := g.GuessVersion(config, filepath.Join(dir, "other")); err != nil {
		t.Fatal(err)
	}
	if inner.guesses != 3 {
		t.Errorf("another configuration or directory was guessed %d times in all, want 3", inner.guesses)
	}
}
