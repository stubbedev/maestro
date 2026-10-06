//go:build unix

// The end-to-end comparison of plugins and PHP scripts with real Composer
// (docs/PLUGINS.md §9.2, §9.3): the plugins of docs/PLUGINS.md's phase 2
// and the runtime scenarios, each fixture project run with the official
// composer.phar 2.10.3 and with maestro, compared as TestE2E compares (it
// reuses its runner: same directories, environment, normalisations and
// comparison).
//
// Plugin packages come from Packagist at the versions the fixtures'
// composer.lock files pin; plugin-api and plugin-runtime use a path
// repository plugin and need no network. Run with MAESTRO_E2E=1 (and
// MAESTRO_E2E_PLUGINS=<name,...> to run some fixtures only).
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestE2EPlugins(t *testing.T) {
	if os.Getenv("MAESTRO_E2E") == "" {
		t.Skip("set MAESTRO_E2E=1 to compare maestro's plugins with Composer 2.10.3 (php, git, unzip and the network)")
	}

	for _, tool := range []string{"php", "git", "unzip"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("%s is required: %v", tool, err)
		}
	}

	phar := composerPhar(t)
	maestro := os.Getenv("MAESTRO_E2E_BIN")
	if maestro == "" {
		maestro = buildMaestro(t, t.TempDir())
	}

	base := t.TempDir()
	if keep := os.Getenv("MAESTRO_E2E_KEEP"); keep != "" {
		base = keep
		if err := os.MkdirAll(base, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	tools := map[string][]string{
		"composer": {"php", phar},
		"maestro":  {maestro},
	}

	phases := []string{"cold", "warm"}
	if os.Getenv("MAESTRO_E2E_WARM") == "0" {
		phases = phases[:1]
	}

	var only []string
	if v := os.Getenv("MAESTRO_E2E_PLUGINS"); v != "" {
		only = strings.Split(v, ",")
	}

	var timings []timing

	for _, sc := range pluginScenarios() {
		t.Run(sc.name, func(t *testing.T) {
			if only != nil && !slices.Contains(only, sc.name) {
				t.Skip("not in MAESTRO_E2E_PLUGINS")
			}
			if sc.skip != nil {
				if reason := sc.skip(); reason != "" {
					t.Skip(reason)
				}
			}

			dir := filepath.Join(base, sc.name)
			if err := os.RemoveAll(dir); err != nil {
				t.Fatal(err)
			}

			tm := timing{scenario: sc.name, times: map[string]time.Duration{}}

			for _, phase := range phases {
				results := map[string][]stepResult{}

				for _, tool := range []string{"composer", "maestro"} {
					res, total := runScenario(t, sc, dir, tool, tools[tool], phase)
					results[tool] = res
					tm.times[tool+" "+phase] = total
				}

				compareResults(t, sc, phase, results["composer"], results["maestro"])
			}

			timings = append(timings, tm)
			t.Logf("%s: %s", sc.name, formatTimes(tm))
		})
	}

	t.Log("\n" + speedReport(timings, phases))
}

// pluginScenarios are the plugin fixtures of testdata/e2e/plugin-*.
func pluginScenarios() []scenario {
	return []scenario{
		{
			// A path repository plugin and root scripts using the Composer
			// API (docs/PLUGINS.md §4): the update without a lock file, the
			// install from it, the flags, the autoload dumps, verbosity.
			name:    "plugin-api",
			fixture: "plugin-api",
			steps: []step{
				{args: []string{"install"}},
				{args: []string{"install"}},
				{args: []string{"install", "--no-plugins"}},
				{args: []string{"install", "--no-scripts"}},
				{args: []string{"dump-autoload"}},
				{args: []string{"dump-autoload", "-o"}},
				{args: []string{"install", "-vvv"}},
				{args: []string{"run-script", "custom", "--", "x", "y"}},
				{args: []string{"update", "--lock"}},
				// Not in allow-plugins any more, non-interactive: blocked.
				{args: []string{"install"}, setup: writeFile("project/composer.json", pluginAPIBlocked)},
				// Disallowed and no longer required: skipped with a warning.
				{args: []string{"install"}, setup: writeFile("project/composer.json", pluginAPIUnrequired)},
			},
		},
		{
			// docs/PLUGINS.md §9.2's runtime cases through the Composer API:
			// exit() and fatal errors in a listener, shutdown functions,
			// putenv() and chdir() seen by what runs next.
			name:    "plugin-runtime",
			fixture: "plugin-runtime",
			steps: []step{
				{args: []string{"install"}},
				{args: []string{"run-script", "env-and-cwd"}},
				{args: []string{"run-script", "shutdown"}},
				{args: []string{"run-script", "exit3"}},
				{args: []string{"run-script", "fatal"}},
				{args: []string{"run-script", "throws"}},
				{args: []string{"run-script", "throws", "-v"}, normalize: normalizeTrace},
				{args: []string{"run-script", "returns-false"}},
				{args: []string{"run-script", "missing-class"}},
				{args: []string{"run-script", "missing-method"}},
				{args: []string{"run-script", "process-timeout"}},
				// Composer's ErrorHandler reports through the IO.
				{args: []string{"run-script", "deprecation"}},
				{args: []string{"run-script", "deprecation", "-v"}, normalize: normalizeStackTrace},
				// A Symfony command class as a script, in its own Application.
				{args: []string{"run-script", "hello-command"}},
				{args: []string{"run-script", "hello-command", "--", "you", "--shout"}},
				{args: []string{"run-script", "hello-command", "-v"}},
				{args: []string{"hello-command", "alias"}},
				{args: []string{"dump-autoload"}},
				{args: []string{"run-script", "missing-command"}},
				{args: []string{"run-script", "plain-command"}},
				// The command class as a command of maestro's Application
				// (docs/PLUGINS.md phase 4): listed, described, completed and
				// run in PHP, with its own errors.
				{args: []string{"list"}},
				{args: []string{"help", "hello-command"}},
				{args: []string{"hello-command", "--help"}},
				{args: []string{"hello-command", "you", "--shout", "-v"}},
				{args: []string{"hello-command", "a", "b"}},
				{args: []string{"hello-command", "--nope", "-v"}, normalize: func(s string) string { return normalizeBundled(normalizeTrace(s)) }},
				{args: []string{"run-script", "--list"}},
				{args: []string{"_complete", "-n", "-c1", "--shell=bash", "-icomposer", "-ihello"}},
				{args: []string{"_complete", "-n", "-c2", "--shell=bash", "-icomposer", "-ihello-command", "-i--sh"}},
			},
		},
		{
			// The kontainer project's stubbedev/*-mcp plugins: four skip
			// their binary download (their *_SKIP_DOWNLOAD switch), one
			// downloads it from GitHub and writes .mcp.json.
			name:    "plugin-stubbedev",
			fixture: "plugin-stubbedev",
			steps: []step{
				{args: []string{"install"}, env: stubbedevSkip},
				{args: []string{"install"}, env: stubbedevSkip},
				{args: []string{"update"}, env: stubbedevSkip},
				{args: []string{"install", "--no-plugins"}},
				{args: []string{"dump-autoload"}},
				{args: []string{"install", "-vvv"}, env: append(slices.Clone(stubbedevSkip), "LARAVEL_MCP_SKIP_DOWNLOAD=1")},
			},
		},
		{
			name:    "plugin-package-versions",
			fixture: "plugin-package-versions",
			steps: []step{
				{args: []string{"install"}},
				{args: []string{"dump-autoload", "-o"}},
				{args: []string{"update", "--lock"}},
				{args: []string{"install", "--no-plugins"}},
			},
		},
		{
			name:    "plugin-symfony-runtime",
			fixture: "plugin-symfony-runtime",
			steps: []step{
				{args: []string{"install"}},
				{args: []string{"dump-autoload"}},
				{args: []string{"install", "--no-dev"}},
			},
		},
		{
			name:    "plugin-spi",
			fixture: "plugin-spi",
			steps: []step{
				{args: []string{"install"}},
				{args: []string{"dump-autoload", "-o"}},
				{args: []string{"dump-autoload", "-v"}},
			},
		},
		{
			// The plugin rewrites its own src/GeneratedConfig.php in place:
			// the store and the warm phase's project must not see it.
			name:    "plugin-phpstan",
			fixture: "plugin-phpstan",
			steps: []step{
				{args: []string{"install"}},
				{args: []string{"update", "--lock"}},
				{args: []string{"remove", "phpstan/phpstan-deprecation-rules"}},
			},
		},
		{
			name:    "plugin-pest",
			fixture: "plugin-pest",
			steps: []step{
				{args: []string{"install"}},
				{args: []string{"dump-autoload"}},
				{args: []string{"install", "--no-dev"}},
				// Its CommandProvider (docs/PLUGINS.md phase 4).
				{args: []string{"pest:dump-plugins"}, setup: removeFile("project/vendor/pest-plugins.json")},
				{args: []string{"list"}},
				{args: []string{"help", "pest:dump-plugins"}},
				{args: []string{"pest:dump-plugins", "-vvv"}},
				{args: []string{"_complete", "-n", "-c1", "--shell=bash", "-icomposer", "-ipest"}},
				{args: []string{"pest:dump-plugins", "--no-plugins"}},
			},
		},
		{
			// Illuminate\Foundation\ComposerScripts: require_once of the
			// project's vendor/autoload.php in PHP, clearing
			// bootstrap/cache, and its pre-package-uninstall handler.
			name:    "plugin-laravel",
			fixture: "plugin-laravel",
			steps: []step{
				{args: []string{"install"}},
				{args: []string{"dump-autoload"}, setup: writeFile("project/bootstrap/cache/packages.php", "<?php return [];\n")},
				{args: []string{"run-script", "slow"}},
				{args: []string{"remove", "league/csv"}},
			},
		},
		{
			// As plugin-phpstan: the plugin rewrites its own file in place.
			name:    "plugin-infection",
			fixture: "plugin-infection",
			steps: []step{
				{args: []string{"install"}},
				{args: []string{"remove", "testo/bridge-infection"}},
				{args: []string{"install", "--no-plugins"}},
			},
		},
		{
			// phpcs's installed_paths, set by the plugin running phpcs
			// through ProcessExecutor (vendor/squizlabs/.../CodeSniffer.conf).
			name:    "plugin-dealerdirect",
			fixture: "plugin-dealerdirect",
			steps: []step{
				{args: []string{"install"}},
				{args: []string{"install", "-v"}},
				{args: []string{"remove", "slevomat/coding-standard"}},
			},
		},
		{
			// The hooks of a git repository, installed by the plugin running
			// captainhook with inherited stdio.
			name:    "plugin-captainhook",
			fixture: "plugin-captainhook",
			setup:   gitProject,
			steps: []step{
				{args: []string{"install"}},
				{args: []string{"install", "--no-dev"}},
				{args: []string{"install", "--no-ansi"}},
			},
		},
		{
			// grumphp's package events in a git repository.
			name:    "plugin-grumphp",
			fixture: "plugin-grumphp",
			setup:   gitProject,
			steps: []step{
				{args: []string{"install"}},
				{args: []string{"remove", "--dev", "phpro/grumphp"}},
			},
		},
		{
			// Scaffolding: post-install/update, the custom scripts it
			// dispatches, the COMMAND event.
			name:    "plugin-drupal",
			fixture: "plugin-drupal",
			steps: []step{
				{args: []string{"install"}},
				{args: []string{"install"}},
				{args: []string{"update"}},
				{args: []string{"require", "psr/log:3.0.2"}},
				{args: []string{"dump-autoload"}},
				// Its CommandProvider (docs/PLUGINS.md phase 4): the command
				// by its alias and by its name.
				{args: []string{"scaffold"}, setup: removeFile("project/web/robots.txt")},
				{args: []string{"drupal:scaffold", "-v"}},
				{args: []string{"help", "scaffold"}},
				{args: []string{"list"}},
			},
		},
		{
			// ergebnis/composer-normalize (docs/PLUGINS.md phase 4): a plugin
			// command creating its own Composer instance (new Factory()),
			// then running `update --lock` in a new Application.
			name:    "plugin-normalize",
			fixture: "plugin-normalize",
			steps: []step{
				{args: []string{"install"}},
				{args: []string{"normalize", "--dry-run", "--diff"}},
				{args: []string{"normalize"}},
				{args: []string{"normalize", "-vvv"}},
				{args: []string{"normalize", "--indent-size=x", "--indent-style=space"}},
				{args: []string{"help", "normalize"}},
			},
		},
		{
			// composer/installers (docs/PLUGINS.md phase 3): a LibraryInstaller
			// subclass overriding supports(), getInstallPath() and
			// uninstall() (whose parent::uninstall() promise it chains), for
			// framework types from Packagist and path packages:
			// installer-paths by vendor:, type: and name, installer-name,
			// framework locations, binaries from a custom path. Removals are
			// one package per step: the "Deleting" lines of several come in
			// the order Composer's `rm -rf` processes happen to finish.
			name:    "plugin-installers",
			fixture: "plugin-installers",
			steps: []step{
				{args: []string{"install"}},
				{args: []string{"install"}},
				{args: []string{"remove", "local/moodle-thing"}},
				{args: []string{"require", "local/moodle-thing:1.0.0"}},
				{args: []string{"remove", "cmb2/cmb2"}},
				{args: []string{"dump-autoload", "-o"}},
				{args: []string{"install", "-vvv"}},
				{args: []string{"update", "--lock"}},
				{args: []string{"install", "--no-plugins"}},
			},
		},
		{
			// oomphinc/composer-installers-extender: its Installer extends
			// composer/installers' (a class of another plugin) and calls
			// LibraryInstaller::getInstallPath() past its parent.
			name:    "plugin-installers-extender",
			fixture: "plugin-installers-extender",
			steps: []step{
				{args: []string{"install"}},
				{args: []string{"remove", "local/custom-thing"}},
				{args: []string{"require", "local/custom-thing:1.0.0"}},
				{args: []string{"dump-autoload"}},
			},
		},
		{
			// mnsami/composer-custom-directory-installer: three plugin
			// classes in extra.class; PearPlugin finds no
			// Composer\Installer\PearInstaller (Composer 2 has none) and
			// registers nothing; its PluginInstaller subclass installs and
			// activates a plugin from a custom path.
			name:    "plugin-custom-directory",
			fixture: "plugin-custom-directory",
			steps: []step{
				{args: []string{"install"}},
				{args: []string{"install"}},
				{args: []string{"dump-autoload"}},
				{args: []string{"remove", "local/other-thing"}},
				{args: []string{"install", "-vvv"}},
				// Uninstalled by the PluginInstaller subclass: deactivated
				// and uninstalled.
				{args: []string{"remove", "local/hello-plugin"}},
			},
		},
		{
			// johnpbloch/wordpress-core-installer: getInstallPath() keeps a
			// static map of the paths it handed out (a stateful installer).
			name:    "plugin-wordpress-core",
			fixture: "plugin-wordpress-core",
			steps: []step{
				{args: []string{"install"}},
				{args: []string{"update", "local/wordpress"}, setup: writeFile("project/packages/wordpress/composer.json", wordpressUpdated)},
				{args: []string{"install"}},
				{args: []string{"remove", "local/wordpress"}},
			},
		},
		{
			// yiisoft/yii2-composer: install()/update()/uninstall() return
			// parent's promise ->then() a callback writing
			// vendor/yiisoft/extensions.php.
			name:    "plugin-yii2",
			fixture: "plugin-yii2",
			steps: []step{
				{args: []string{"install"}},
				{args: []string{"update", "local/yii-ext"}, setup: writeFile("project/packages/yii-ext/composer.json", yiiExtUpdated)},
				{args: []string{"remove", "local/yii-other"}},
				{args: []string{"require", "local/yii-other:1.0.0"}},
				{args: []string{"install", "-v"}},
			},
		},
		{
			// A plugin installed globally (in COMPOSER_HOME) loads for every
			// project, after the project's own, "installed globally".
			name:    "plugin-global",
			fixture: "plugin-global",
			setup: writeFile("home/composer.json", `{
    "repositories": [
        {"type": "path", "url": "@ROOT@/project/globalplugin", "options": {"symlink": false}},
        {"packagist.org": false}
    ],
    "require": {"maestro-test/global-plugin": "*"},
    "config": {"allow-plugins": {"maestro-test/global-plugin": true}}
}
`),
			steps: []step{
				{args: []string{"global", "install"}},
				{args: []string{"install"}},
				{args: []string{"install", "-vvv"}},
				{args: []string{"install", "--no-plugins"}},
				{args: []string{"global", "update"}},
			},
		},
	}
}

// removeFile removes a file of the scenario root before a step (so the
// step's rewrite of it shows).
func removeFile(path string) func(t *testing.T, root string) {
	return func(t *testing.T, root string) {
		t.Helper()

		if err := os.Remove(filepath.Join(root, path)); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
}

// gitProject makes the scenario's project a git repository with one
// commit (the hooks plugins install into .git).
func gitProject(t *testing.T, root string) {
	t.Helper()

	gitRepo(t, filepath.Join(root, "project"), commit{files: map[string]string{"README.md": "# project\n"}})
}

// stubbedevSkip makes all stubbedev plugins but laravel-dev-mcp skip their
// binary download.
var stubbedevSkip = []string{
	"ATLASSIAN_MCP_SKIP_DOWNLOAD=1",
	"DS_MCP_SKIP_DOWNLOAD=1",
	"JENKINS_MCP_SKIP_DOWNLOAD=1",
	"SENTRY_MCP_SKIP_DOWNLOAD=1",
}

// pluginAPIBlocked is plugin-api's composer.json without the plugin in
// allow-plugins.
const pluginAPIBlocked = `{
    "name": "maestro-test/api-project",
    "repositories": [
        {"type": "path", "url": "plugin", "options": {"symlink": false}},
        {"packagist.org": false}
    ],
    "require": {
        "maestro-test/api-plugin": "*"
    },
    "autoload": {
        "psr-4": {"Project\\": "src/"}
    }
}
`

// pluginAPIUnrequired is plugin-api's composer.json without the plugin.
const pluginAPIUnrequired = `{
    "name": "maestro-test/api-project",
    "repositories": [
        {"type": "path", "url": "plugin", "options": {"symlink": false}},
        {"packagist.org": false}
    ],
    "autoload": {
        "psr-4": {"Project\\": "src/"}
    }
}
`

// traceFrame is a frame line of an exception trace Symfony renders at -v
// (" Class->method() at file:line").
var traceFrame = regexp.MustCompile(`(?m)^ \S+\(\) at \S+\n`)

// normalizeTrace drops the frames of exception traces (docs/PLUGINS.md §10,
// risk 3): below the plugin's own code Composer's frames are its sources'
// (EventDispatcher.php, Application.php, ...) and maestro's the shim's, by
// design. The exception's own file and line are kept.
func normalizeTrace(s string) string { return traceFrame.ReplaceAllString(s, "") }

// bundledPath is the directory of the libraries Composer bundles: in the
// phar, or in maestro's shim (docs/PLUGINS.md §5.1).
var bundledPath = regexp.MustCompile(`phar://\S*?\.phar/vendor/|\S*/maestro/shim/[0-9a-f]{64}/lib/`)

// normalizeBundled replaces the location of the bundled libraries (an
// exception thrown by the bundled symfony/console names its file).
func normalizeBundled(s string) string { return bundledPath.ReplaceAllString(s, "<bundled>/") }

// stackFrames are the frames of a "Stack trace:" Composer's ErrorHandler
// writes at -v, after the first (the plugin's own line).
var stackFrames = regexp.MustCompile(`(?m)^(Stack trace:\n \S+\n)(?: \S+\n)+`)

// normalizeStackTrace drops the frames of ErrorHandler's stack traces below
// the plugin's own line, for the reason of normalizeTrace.
func normalizeStackTrace(s string) string { return stackFrames.ReplaceAllString(s, "$1") }

// wordpressUpdated is plugin-wordpress-core's stand-in for WordPress at a
// new version.
const wordpressUpdated = `{
    "name": "local/wordpress",
    "version": "6.8.1",
    "type": "wordpress-core",
    "require": {
        "johnpbloch/wordpress-core-installer": "^2.0"
    }
}
`

// yiiExtUpdated is plugin-yii2's extension at a new version, with another
// bootstrap class.
const yiiExtUpdated = `{
    "name": "local/yii-ext",
    "version": "1.1.0",
    "type": "yii2-extension",
    "autoload": {
        "psr-4": {"local\\yiiext\\": "src/"}
    },
    "extra": {
        "bootstrap": "local\\yiiext\\NewBootstrap"
    }
}
`
