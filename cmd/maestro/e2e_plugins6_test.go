//go:build unix

// The plugins of docs/PLUGINS.md's phase 6 (internals emulation):
// symfony/flex, php-http/discovery, bamarni/composer-bin-plugin,
// symfony/thanks and vaimo/composer-patches, compared with Composer as
// TestE2EPlugins compares the others.

package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
)

func phase6PluginScenarios() []scenario {
	return []scenario{
		{
			// symfony/flex 2.11.0 (docs/PLUGINS.md §5.12): the recipes of a
			// local endpoint (testdata/e2e/flex-recipes, served over http),
			// symfony.lock, the first install's re-run (a clone of the
			// Installer on the stack, __construct()ed with a new Composer),
			// the pack it unpacks, `req logger` (an alias resolved in
			// activate() by rewriting the ArgvInput found on the stack),
			// its commands added to the running Application, its
			// PRE_OPERATIONS_EXEC Transaction built with Closure::bind.
			name:    "plugin-flex",
			fixture: "plugin-flex",
			setup:   flexEndpoint,
			steps: []step{
				{args: []string{"install"}, normalize: normalizeFlexSession},
				{args: []string{"req", "logger"}, normalize: normalizeFlexSession},
				{args: []string{"recipes"}},
				{args: []string{"recipes", "psr/log"}},
				{args: []string{"recipes:install", "--force", "--reset"}, normalize: normalizeFlexSession},
				{args: []string{"list"}},
				{args: []string{"help", "recipes"}},
				{args: []string{"install", "-vvv"}, normalize: normalizeFlexSession},
				{args: []string{"rem", "logger", "-v"}, normalize: normalizeFlexSession},
				{args: []string{"update"}, normalize: normalizeFlexSession},
				{args: []string{"install", "--no-plugins"}},
			},
		},
		{
			// php-http/discovery 1.20.0: its POST_UPDATE_CMD listener
			// requires implementations of the abstractions required, finds
			// the Installer on the stack, clones it, __construct()s the
			// clone with a new Composer (Factory::create) and runs it,
			// reading the EventDispatcher's protected runScripts and the
			// Installer's platformRequirementFilter by array cast; then a
			// VersionSelector on a new RepositorySet.
			name:    "plugin-discovery",
			fixture: "plugin-discovery",
			steps: []step{
				{args: []string{"update"}},
				{args: []string{"install"}},
				{args: []string{"update", "-v"}},
				{args: []string{"dump-autoload"}},
			},
		},
		{
			// bamarni/composer-bin-plugin 1.9.1: `bin <namespace>` runs a
			// new Application in vendor-bin/<namespace> (chdir, putenv);
			// install and update are forwarded to every namespace (its
			// COMMAND listener, and POST_AUTOLOAD_DUMP reading the
			// ConsoleIO's protected input and output).
			name:    "plugin-bamarni",
			fixture: "plugin-bamarni",
			steps: []step{
				{args: []string{"update"}},
				{args: []string{"bin", "all", "install"}},
				{args: []string{"bin", "tools", "update", "-v"}},
				{args: []string{"install"}},
				{args: []string{"bin", "lint", "show"}},
				{args: []string{"dump-autoload"}},
				{args: []string{"bin", "all", "update", "--no-plugins"}},
			},
		},
		{
			// symfony/thanks 1.4.0: activate() finds the Application and
			// its ArgvInput on the stack and adds its commands; `thanks`
			// asks GitHub's GraphQL API through a new HttpDownloader
			// (without credentials: Composer's authentication error).
			name:    "plugin-thanks",
			fixture: "plugin-thanks",
			steps: []step{
				{args: []string{"install"}},
				{args: []string{"list"}},
				{args: []string{"help", "thanks"}},
				{args: []string{"update"}},
				{args: []string{"thanks", "--dry-run"}},
			},
		},
		{
			// vaimo/composer-patches 6.0.3 (best effort, docs/PLUGINS.md
			// phase 6): a clone of the Config, a FileDownloader created in
			// PHP, packages reinstalled through InstallationManager with
			// its own InstallOperation subclass, its patch commands.
			name:    "plugin-vaimo",
			fixture: "plugin-vaimo",
			steps: []step{
				{args: []string{"install"}},
				{args: []string{"install"}},
				{args: []string{"patch:list"}},
				{args: []string{"patch:redo"}},
				{args: []string{"patch:undo"}},
				{args: []string{"install"}},
				{args: []string{"update"}},
			},
		},
	}
}

var (
	flexServerOnce sync.Once
	flexServerURL  string
)

// flexEndpoint serves testdata/e2e/flex-recipes over http (flex reads its
// endpoints with HttpDownloader::add(), which needs an http status) and
// writes its URL into the project's composer.json. One server serves the
// whole test process.
func flexEndpoint(t *testing.T, root string) {
	t.Helper()

	flexServerOnce.Do(func() {
		dir, err := filepath.Abs(filepath.Join("testdata", "e2e", "flex-recipes"))
		if err != nil {
			t.Fatal(err)
		}
		srv := httptest.NewServer(http.FileServer(http.Dir(dir)))
		flexServerURL = srv.URL
	})

	path := filepath.Join(root, "project", "composer.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.ReplaceAll(string(data), "@ENDPOINT@", flexServerURL)), 0o644); err != nil {
		t.Fatal(err)
	}
}

// flexSession is flex's random session id in its "Symfony operations"
// line.
var flexSession = regexp.MustCompile(`(Symfony operations: \d+ recipes? \()[0-9a-f]{32}\)`)

// normalizeFlexSession replaces flex's random session id.
func normalizeFlexSession(s string) string { return flexSession.ReplaceAllString(s, "${1}<session>)") }
