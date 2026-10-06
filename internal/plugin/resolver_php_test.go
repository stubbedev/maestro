package plugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/io"
)

// TestPlugins_ResolverAPIs runs a plugin using the API of docs/PLUGINS.md's
// phase 5 in-process: root package changes before solving, PRE_POOL_CREATE
// (a package created in PHP joins the pool), PRE_OPERATIONS_EXEC, the
// ArrayLoader and ArrayDumper, a package created in PHP added to the local
// repository, repositories built in PHP (platform, installed, composite),
// RepositoryManager::createRepository()/prependRepository(), RepositorySet
// and VersionSelector, RepositoryFactory, JsonManipulator and
// JsonConfigSource, the Locker's updateHash(), a Cache, a
// SuggestedPackagesReporter, a second EventDispatcher and a nested
// Installer run.
func TestPlugins_ResolverAPIs(t *testing.T) {
	requirePHP(t)

	p := newProject(t, "resolver", console.VerbosityNormal)

	// No lock file, the plugin not installed yet: it runs from
	// POST_UPDATE_CMD on.
	code, err := p.install(true)
	output := p.output()
	if err != nil || code != 0 {
		t.Fatalf("update = %d, %v\n%s\nchild:\n%s", code, err, output, p.childOutput())
	}
	for _, want := range []string{
		"loaded: Composer\\Package\\CompletePackage Vendor/Loaded 1.2.0 requires php",
		`dumped: {"name":"Vendor\/Loaded","version":"1.2.0","version_normalized":"1.2.0.0","require":{"php":">=7.2"},"type":"library",`,
		"branch alias: '1.9999999.9999999.9999999-dev'",
		`born in local repo: true extra={"made":"in php"} repo=Composer\Repository\InstalledFilesystemRepository`,
		"born removed: true",
		"platform php: 7.4.33",
		"installed repo: 1 installed repo (installed array repo (defining 2 packages), root package repo, platform repo)",
		"composite: 3 true",
		"prepended: first package repo (defining 1 package)",
		"best: local/lib-b 2.1.0 recommended ^2.1",
		"none: false",
		"default repos: plugin,libs/*",
		`manipulated: "{\n  \"name\": \"x\/y\",\n  \"require\": {\n    \"a\/b\": \"^1.0\"\n  },\n  \"config\": {\n    \"sort-packages\": true\n  }\n}\n"`,
		`config source: extra.json "{\n    \"extra\": {\n        \"foo\": \"bar\"\n    },\n    \"require\": {\n        \"c\/d\": \"^2.0\"\n    }\n}\n"`,
		"lock file: ./composer.lock",
		"lock processed: true",
		"cache: 'v' true",
		"suggestions: 1",
		"second dispatcher listener",
		"async process: async hello Symfony\\Component\\Process\\Process",
		"http downloader: Composer\\Util\\HttpDownloader same loop downloader: true",
		"rfs tls disabled: false status: 200",
		"zip downloader: Composer\\Downloader\\ZipDownloader",
		"nested run: 0",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("first update's output lacks %q:\n%s\nchild:\n%s", want, output, p.childOutput())
		}
	}

	// Again without the lock file, the plugin installed: it changes the
	// root package and the pool before solving.
	if err := os.Remove(filepath.Join(p.dir, "composer.lock")); err != nil {
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
		"pre-update requires: maestro-test/resolver-plugin,local/lib-a,local/lib-b",
		"pool: local/lib-a 1.0.0,local/lib-b 2.1.0,maestro-test/resolver-plugin 1.0.0,maestro-test/resolver-project 1.0.0+no-version-set",
		"request requires: maestro-test/resolver-plugin,local/lib-a,local/lib-b",
		"stabilities: stable",
		"lib-a suggests: local/lib-b dist=path",
		"pool grew by: 1",
		"operations: Installing local/lib-b (2.1.0)",
		"  - Locking local/lib-b (2.1.0)",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("second update's output lacks %q:\n%s\nchild:\n%s", want, output, p.childOutput())
		}
	}
	if final := p.rt.Finish(0); final != 0 {
		t.Errorf("Finish = %d", final)
	}
}
