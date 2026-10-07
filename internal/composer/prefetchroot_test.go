package composer

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/vcs"
)

// homeDir is the global composer.json's directory newFactoryMock makes.
var homeDir = regexp.MustCompile(`/[^ ]*composer-test-[0-9]+`)

// The root version's guess takes the git branch PrefetchRootVersion
// started, in the environment vcs.CleanEnv gives it, and loads the same
// root package with the same output, -vvv's command log included, as
// without it.
func TestPrefetchRootVersion_GuessIsUnchanged(t *testing.T) {
	if util.IsWindows() {
		t.Skip("a shell script stands in for git")
	}
	t.Setenv("MAESTRO_CACHE_DIR", t.TempDir())
	t.Setenv("COMPOSER", "")
	t.Setenv("COMPOSER_ROOT_VERSION", "")
	vcs.SetVersion("2.40.0", true)
	t.Cleanup(func() { vcs.SetVersion("", false) })

	bin := t.TempDir()
	runs := filepath.Join(bin, "runs")
	// the branch shows whether git ran in the environment the guess
	// prepares
	script := `#!/bin/sh
echo "$1" >> '` + runs + `'
if [ "$1" = branch ]; then
	if [ "$GIT_TERMINAL_PROMPT" = 0 ] && [ "$LANGUAGE" = C ]; then b=main; else b=unclean; fi
	echo "* $b 0123456789abcdef0123456789abcdef01234567 message"
fi
`
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	dir := tempDir(t)
	t.Chdir(dir)
	if err := os.WriteFile("composer.json", []byte(`{"name": "a/b"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	load := func(prefetch bool) (string, string, int) {
		t.Helper()
		// the environment before vcs.CleanEnv prepares it
		t.Setenv("GIT_TERMINAL_PROMPT", "1")
		t.Setenv("LANGUAGE", "en")
		_ = os.Remove(runs)

		if prefetch {
			PrefetchRootVersion()
		}
		out, err := io.NewBufferIO("", console.VerbosityDebug, console.NewOutputFormatter(false))
		if err != nil {
			t.Fatal(err)
		}
		f := newFactoryMock(t, testRuntime(t, 0))
		f.LoadRootPackageFunc = nil
		c, err := f.CreateComposer(out, nil, PluginsDisabled, "", true)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := os.ReadFile(runs)

		return c.Package().PrettyVersion(), homeDir.ReplaceAllString(strings.ReplaceAll(out.Output(), dir, "DIR"), "HOME"), strings.Count(string(data), "branch\n")
	}

	// the first load checks the CA file, once per process
	load(false)
	version, output, branches := load(false)
	if version != "dev-main" || branches != 1 {
		t.Fatalf("without prefetch: version %q, %d git branch runs", version, branches)
	}
	if !strings.Contains(output, "Executing command (DIR): 'git' 'branch' '-a' '--no-color' '--no-abbrev' '-v'") {
		t.Fatalf("the guess's command is not logged:\n%s", output)
	}

	prefetchedVersion, prefetchedOutput, branches := load(true)
	if prefetchedVersion != version || prefetchedOutput != output {
		t.Errorf("with prefetch: version %q, output\n%s\nwant %q,\n%s", prefetchedVersion, prefetchedOutput, version, output)
	}
	if branches != 1 {
		t.Errorf("git branch ran %d times, want the prefetched run only", branches)
	}

	// a composer.json naming a version leaves the guess alone
	if err := os.WriteFile("composer.json", []byte(`{"name": "a/b", "version": "1.2.3"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(runs)
	PrefetchRootVersion()
	if _, err := os.Stat(runs); err == nil {
		t.Error("git ran for a composer.json with a version")
	}
}

// The installed.php Factory::createComposer loads for InstalledVersions is
// the file as it was then, however late the plugin runtime asks for it.
func TestFactory_InstalledVersionsAreReadAtCreate(t *testing.T) {
	t.Setenv("MAESTRO_CACHE_DIR", t.TempDir())
	t.Setenv("COMPOSER", "")
	dir := tempDir(t)
	t.Chdir(dir)
	if err := os.WriteFile("composer.json", []byte(`{"name": "a/b", "version": "1.0.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll("vendor/composer", 0o755); err != nil {
		t.Fatal(err)
	}
	installed := func(version string) {
		t.Helper()
		code := "<?php return array(\n    'root' => array(\n        'name' => 'a/b',\n        'install_path' => __DIR__ . '/../../',\n    ),\n" +
			"    'versions' => array(\n        'a/b' => array(\n            'pretty_version' => '" + version + "',\n        ),\n    ),\n);\n"
		if err := os.WriteFile("vendor/composer/installed.php", []byte(code), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	installed("1.0.0")

	rt := testRuntime(t, 0)
	if _, err := newFactoryMock(t, rt).CreateComposer(newBufferIO(t), nil, PluginsEnabled, "", false); err != nil {
		t.Fatal(err)
	}
	installed("2.0.0")

	data := rt.InstalledVersions()
	if got := data.ArrayAt("versions").ArrayAt("a/b").At("pretty_version"); got != "1.0.0" {
		t.Errorf("pretty_version %v, want the 1.0.0 installed.php had at create", got)
	}
	if got := data.ArrayAt("root").At("install_path"); got != dir+"/vendor/composer/../../" {
		t.Errorf("install_path %v, want __DIR__ resolved", got)
	}
}
