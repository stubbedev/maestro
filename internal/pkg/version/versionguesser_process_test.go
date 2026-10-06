package version_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg/version"
	"github.com/stubbedev/maestro/internal/util"
)

// TestVersionGuesser_RealProcesses guesses with real processes outside a
// git checkout: git fails, the fallbacks run (started ahead, all at once),
// the absent ones (hg, fossil) are not started, and svn's answer is the
// version, as when each runs in turn.
func TestVersionGuesser_RealProcesses(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell scripts")
	}

	keepEnv(t)
	resetGitVersion(t)

	bin := t.TempDir()
	log := filepath.Join(t.TempDir(), "log")

	tools := map[string]string{
		"git": `echo "git $*" >> ` + log + `
if [ "$1" = --version ]; then echo "git version 2.52.0"; exit 0; fi
echo "fatal: not a git repository" >&2; exit 128
`,
		"svn": `echo "svn $*" >> ` + log + `
echo "<info><entry><url>https://svn.example.org/repo/branches/feature</url></entry></info>"
`,
	}

	for name, script := range tools {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// only absolute entries: the guesser can tell hg and fossil are absent
	t.Setenv("PATH", bin)

	process := version.NewProcessExecutor(util.NewProcessExecutor(nil))

	data, err := version.NewVersionGuesser(process, nil).GuessVersion(php.NewArray(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	if data == nil || data.Version != "dev-feature" || data.PrettyVersion != "dev-feature" {
		t.Fatalf("guessed %+v", data)
	}

	ran, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}

	// each ran once (the order of the ones started ahead is not fixed)
	want := map[string]bool{
		"git --version": true,
		"git branch -a --no-color --no-abbrev -v":                                  true,
		"git describe --exact-match --tags":                                        true,
		"git rev-list --no-commit-header --format=%H -n1 HEAD --no-show-signature": true,
		"svn info --xml": true,
	}

	for line := range strings.SplitSeq(strings.TrimSpace(string(ran)), "\n") {
		if !want[line] {
			t.Errorf("unexpected or repeated run %q", line)
		}
		delete(want, line)
	}

	if len(want) > 0 {
		t.Errorf("not run: %v (log %q)", want, ran)
	}
}
