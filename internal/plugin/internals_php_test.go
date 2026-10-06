package plugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/io"
)

// TestPlugins_Internals runs a plugin using Composer's internals as
// docs/PLUGINS.md's phase 6 emulates them, in-process: the Installer found
// with debug_backtrace() (its protected properties by array cast, a setter
// reaching maestro's Installer, a clone __construct()ed with a new
// Composer and run, nested frames), EventDispatcher::$runScripts,
// Config::$baseDir and a clone of the Config, Transaction's properties
// through Closure::bind and a new Transaction, RepositorySet pools and
// advisories, a PHP-local Pool, a repository written in PHP given to the
// RepositoryManager, a repository class registered with
// setRepositoryClass(), maestro's downloaders and a FileDownloader created
// in PHP.
func TestPlugins_Internals(t *testing.T) {
	requirePHP(t)

	p := newProject(t, "internals", console.VerbosityNormal)

	code, err := p.install(true)
	output := p.output()
	if err != nil || code != 0 {
		t.Fatalf("update = %d, %v\n%s\nchild:\n%s", code, err, output, p.childOutput())
	}

	// The plugin is installed by this run, after PRE_UPDATE_CMD: update
	// again (without the lock file) to see all its listeners, with a new
	// requirement whose suggestion the plugin's reporter silences.
	if err := os.Remove(filepath.Join(p.dir, "composer.lock")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(p.dir, "composer.json"))
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), `"local/lib-a": "*"`, `"local/lib-a": "*", "local/lib-b": "*"`, 1))
	if err := os.WriteFile(filepath.Join(p.dir, "composer.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	p.out, err = io.NewBufferIO("", console.VerbosityNormal, console.NewOutputFormatter(false))
	if err != nil {
		t.Fatal(err)
	}
	code, err = p.install(true)
	output = p.output()
	if err != nil || code != 0 {
		t.Fatalf("second update = %d, %v\n%s\nchild:\n%s", code, err, output, p.childOutput())
	}
	for _, want := range []string{
		"installers on the stack: 1\n",
		`platform filter: Composer\Filter\PlatformRequirementFilter\IgnoreNothingPlatformRequirementFilter devMode: true`,
		"runScripts: true",
		"baseDir set: true",
		"clone sort-packages: true original: false",
		"transaction: result 3 present 2 ops ",
		"installers on the stack: 2 (nested)",
		"nested post-update",
		"nested run: 0",
		"pool: 1 local/lib-a provides: 1",
		"advisories: 0",
		"local pool: Pool:|-      1: a/b|-      2: c/d",
		"php repository: php/made 1.2.0",
		`custom repository: MaestroTest\InternalsPlugin\CustomRepository custom repo custom/pkg 1`,
		"zip downloader source: dist",
		`file downloader: Composer\Downloader\FileDownloader dist`,
	} {
		if !strings.Contains(output, want) {
			t.Errorf("output lacks %q:\n%s\nchild:\n%s", want, output, p.childOutput())
		}
	}
	// local/lib-b's suggestion is silenced: the reporter was replaced
	// through the Installer on the stack.
	if strings.Contains(output, "suggestions were added") {
		t.Errorf("the suggestions the plugin silenced were reported:\n%s", output)
	}
	if final := p.rt.Finish(0); final != 0 {
		t.Errorf("Finish = %d", final)
	}
}
