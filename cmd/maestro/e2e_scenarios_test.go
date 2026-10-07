package main

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// scenarios are the end-to-end scenarios, in the order they run.
func scenarios() []scenario {
	return append(fixtureScenarios(), realWorldScenarios()...)
}

// fixtureScenarios run on the fixture projects of testdata/e2e and on
// generated repositories.
func fixtureScenarios() []scenario {
	return []scenario{
		{
			name:    "install-from-lock",
			fixture: "basic",
			steps: []step{
				{args: []string{"install"}},
				{args: []string{"install"}},
				{args: []string{"i", "--dry-run"}},
				{args: []string{"install", "--audit", "--audit-format=json"}},
				{args: []string{"install", "--dry-run"}, env: []string{"COMPOSER_NO_DEV=1", "COMPOSER_IGNORE_PLATFORM_REQS=1"}},
			},
		},
		{
			name:    "commands",
			fixture: "basic",
			steps: []step{
				{args: []string{"install", "--no-scripts"}},
				{args: []string{"dump-autoload", "-o"}},
				{args: []string{"dump-autoload", "-a"}},
				{args: []string{"dump-autoload", "--apcu"}},
				{args: []string{"dump-autoload", "--no-dev"}},
				{args: []string{"dump-autoload"}},
				{args: []string{"show"}},
				{args: []string{"show", "-t"}},
				{args: []string{"show", "--format=json"}},
				{args: []string{"show", "monolog/monolog"}},
				{args: []string{"show", "-s"}},
				{args: []string{"show", "-i", "--name-only"}},
				{args: []string{"show", "-p"}},
				{args: []string{"licenses"}},
				{args: []string{"licenses", "--format=json"}},
				{args: []string{"fund"}, normalize: normalizeFund},
				{args: []string{"fund", "--format=json"}, normalize: normalizeFund},
				{args: []string{"suggests"}},
				{args: []string{"suggests", "--all"}},
				{args: []string{"suggests", "--by-suggestion"}},
				{args: []string{"why", "psr/log"}},
				{args: []string{"why", "psr/log", "-t"}},
				{args: []string{"why-not", "psr/log", "4.0"}},
				{args: []string{"why-not", "php", "9"}},
				{args: []string{"check-platform-reqs"}},
				{args: []string{"check-platform-reqs", "--format=json"}},
				{args: []string{"validate"}},
				{args: []string{"validate", "--strict"}},
				{args: []string{"status"}},
				{args: []string{"status", "-v"}},
				{args: []string{"audit"}},
				{args: []string{"audit", "--format=json"}},
				{args: []string{"outdated"}},
				{args: []string{"outdated", "--direct", "--format=json"}},
				{args: []string{"show", "-l"}},
				{args: []string{"show", "-a", "psr/log"}},
				{args: []string{"run-script", "--list"}},
				{args: []string{"list"}},
				{args: []string{"list", "--raw"}},
				{args: []string{"list", "--short"}},
				{args: []string{"list", "--format=json", "--short"}},
				{args: []string{"list", "--format=xml"}},
				{args: []string{"list", "--format=md"}},
				{args: []string{}},
				{args: []string{"help"}},
				{args: []string{"-h"}},
				{args: []string{"help", "validate", "--format=json"}},
				{args: []string{"help", "validate", "--format=xml"}},
				{args: []string{"help", "validate", "--format=md"}},
				{args: []string{"help", "validate", "--raw"}},
				{args: []string{"help", "u"}},
				{args: []string{"help", "instal"}},
				{args: []string{"about"}},
				{args: []string{"completion", "bash"}},
				{args: []string{"_complete", "-n", "-c1", "--shell=bash", "-icomposer", "-irequ"}},
				{args: []string{"_complete", "-n", "-c2", "--shell=bash", "-icomposer", "-ishow", "-i--fo"}},
				{args: []string{"_complete", "-n", "-c3", "--shell=bash", "-icomposer", "-ishow", "-i--format", "-i"}},
				{args: []string{"_complete", "-n", "-c2", "--shell=bash", "-icomposer", "-iwhy", "-i"}},
				// Without a project the suggestion callback fails, which
				// _complete swallows: no suggestions, exit 0.
				{args: []string{"_complete", "-n", "-c2", "--shell=bash", "-icomposer", "-iwhy", "-i"}, dir: "."},
				{args: []string{"diagnose"}, setup: installPubKeys, normalize: normalizeDiagnose},
			},
		},
		{
			name:    "update",
			fixture: "update",
			steps: []step{
				{args: []string{"install"}},
				{args: []string{"u", "--dry-run"}},
				{args: []string{"update", "-i"}, env: interactive, stdin: "psr/log\n\nyes\n"},
				{args: []string{"update", "psr/log:1.1.2", "--no-install"}},
				{args: []string{"upgrade", "psr/log"}},
				{args: []string{"update", "--with", "psr/log:^1.1.3", "--no-install"}},
				{args: []string{"update", "mirrors"}},
				{args: []string{"update", "nothing", "--no-install"}},
				{args: []string{"update", "--prefer-stable", "-m", "--dry-run"}},
				{args: []string{"update", "-w", "monolog/monolog", "--dry-run"}},
				{args: []string{"update", "-W", "psr/container", "--dry-run"}},
				{args: []string{"update", "--dry-run"}, env: []string{"COMPOSER_PREFER_LOWEST=1", "COMPOSER_MINIMAL_CHANGES=1"}},
				{args: []string{"update", "--dry-run"}},
				{args: []string{"update", "psr/log"}},
				{args: []string{"update", "monolog/monolog", "--with-dependencies"}},
				{args: []string{"update", "psr/container", "--with-all-dependencies"}},
				{args: []string{"update", "--prefer-lowest"}},
				{args: []string{"update", "--no-dev"}},
				{args: []string{"update"}},
				{args: []string{"update", "--lock"}},
				{args: []string{"update", "nothing/matches"}},
				{args: []string{"update", "psr/*", "--no-install"}},
				{args: []string{"show", "--latest"}},
			},
		},
		{
			name:    "require-remove",
			fixture: "require",
			steps: []step{
				{args: []string{"require", "psr/log"}},
				{args: []string{"require", "monolog/monolog:^1.27"}},
				{args: []string{"remove", "psr/log"}},
				{args: []string{"require", "monolog/monolog:^1.27"}},
				{args: []string{"require", "--dev", "psr/container:^1.1"}},
				{args: []string{"require", "psr/container:^1.1"}},
				{args: []string{"require", "--sort-packages", "psr/link:^1.0", "--no-update"}},
				{args: []string{"update"}},
				{args: []string{"r", "psr/http-message:^1.1", "--no-update"}},
				{args: []string{"require", "psr/http-message:^1.1", "--update-no-dev"}},
				{args: []string{"require", "psr/event-dispatcher:^1.0", "-W"}},
				{args: []string{"require", "psr/http-factory:^1.0", "-m"}},
				{args: []string{"require", "psr/clock:^1.0", "--no-install"}},
				{args: []string{"require", "psr/http-client:^1.0", "-o"}},
				{args: []string{"require", "psr/http-client:^1.0", "-a", "--apcu-autoloader-prefix=e2e"}},
				{args: []string{"require", "php:>=8.1", "ext-json:*"}},
				{args: []string{"require", "psr/log:^99"}},
				{args: []string{"require", "nothing/at-all"}},
				{args: []string{"require", "psr/simple-cache", "--prefer-lowest", "--update-with-dependencies"}},
				{args: []string{"require", "psr/cache", "--fixed"}},
				{args: []string{"remove", "monolog/monolog", "--update-with-all-dependencies"}},
				{args: []string{"remove", "--dev", "psr/container"}},
				{args: []string{"remove", "not/required"}},
				{args: []string{"remove", "psr/link", "--no-update"}},
				// the lock is stale: both refuse
				{args: []string{"bump"}},
				{args: []string{"bump", "--dry-run"}},
				// fresh again: the listing, then the rewrite of composer.json
				// and of the lock's content-hash
				{args: []string{"update"}},
				{args: []string{"bump", "--dry-run"}},
				{args: []string{"bump"}},
			},
		},
		{
			name:    "path-repositories",
			fixture: "path",
			steps: []step{
				{args: []string{"update"}},
				{args: []string{"install"}, setup: removeVendor},
				{
					args:  []string{"update", "acme/b"},
					setup: writeFile("project/packages/b/src/C.php", "<?php\n\nnamespace Acme\\B;\n\nfinal class C\n{\n}\n"),
				},
				{args: []string{"reinstall", "acme/b"}},
				{args: []string{"status"}},
				{args: []string{"show", "acme/a"}},
				{args: []string{"exec", "--list"}},
				{args: []string{"exec", "b-tool"}},
			},
		},
		{
			name:    "vcs-repositories",
			fixture: "vcs",
			setup:   vcsRepos,
			steps: []step{
				{args: []string{"update"}},
				{args: []string{"install"}, setup: removeVendor},
				{args: []string{"update", "acme/lib", "acme/tool"}, setup: vcsReposNext},
				{args: []string{"status", "-v"}, setup: writeFile("project/vendor/acme/tool/src/Changed.php", "<?php\n")},
				{args: []string{"show", "acme/lib", "--all"}},
				{args: []string{"require", "acme/lib:dev-develop"}},
				{args: []string{"require", "acme/lib:dev-develop", "--prefer-source"}, setup: removeVendor},
				{args: []string{"require", "acme/lib:dev-develop", "--prefer-install=source"}, setup: removeVendor},
				{args: []string{"install", "--prefer-dist"}, setup: removeVendor},
			},
		},
		{
			name:    "prefer-source",
			fixture: "prefer-source",
			steps: []step{
				{args: []string{"update", "--prefer-source"}},
				{args: []string{"status"}},
				{args: []string{"install", "--prefer-dist"}, setup: removeVendor},
				{args: []string{"install", "--prefer-source"}, setup: removeVendor},
				{args: []string{"install", "--prefer-install=dist"}, setup: removeVendor},
				{args: []string{"install", "--prefer-install=source"}, setup: removeVendor},
				{args: []string{"install", "--prefer-install=auto"}, setup: removeVendor},
				{args: []string{"config", "preferred-install", "source"}},
				{args: []string{"install"}, setup: removeVendor},
			},
		},
		{
			name:    "artifact-repository",
			fixture: "artifact",
			setup:   artifacts,
			steps: []step{
				{args: []string{"update"}},
				{args: []string{"install"}, setup: removeVendor},
				{args: []string{"require", "acme/art:1.0.0"}},
			},
		},
		{
			name:    "platform",
			fixture: "platform",
			steps: []step{
				{args: []string{"update"}},
				{args: []string{"check-platform-reqs"}},
				{args: []string{"check-platform-reqs", "--lock"}},
				{args: []string{"config", "platform.php", "--unset"}},
				{args: []string{"update", "psr/log"}},
				{args: []string{"require", "ext-nonexistent:*"}},
				{args: []string{"require", "ext-nonexistent:*", "--ignore-platform-req=ext-nonexistent"}},
				{args: []string{"install"}, setup: removeVendor},
				{args: []string{"install", "--ignore-platform-reqs"}},
				{args: []string{"check-platform-reqs"}},
				{args: []string{"check-platform-reqs", "--format=json"}},
				{args: []string{"check-platform-reqs", "--no-dev", "-f", "json"}},
				{args: []string{"check-platform-reqs", "--lock", "--no-dev"}},
				{args: []string{"update", "--ignore-platform-req=ext-*"}},
				{args: []string{"require", "php:>=99", "--ignore-platform-req=php+"}},
				{args: []string{"update", "--ignore-platform-reqs"}},
				{args: []string{"dump-autoload"}},
				{args: []string{"dump-autoload", "--ignore-platform-req=php"}},
				{args: []string{"dump-autoload", "--ignore-platform-reqs"}},
			},
		},
		{
			name:    "unsatisfiable",
			fixture: "unsatisfiable",
			steps: []step{
				{args: []string{"update"}},
				{args: []string{"update", "-v"}},
				{args: []string{"install"}},
				{args: []string{"why-not", "psr/log", "^2.0"}},
				{args: []string{"require", "psr/log:^99"}},
				{args: []string{"require", "psr/log:^1.0", "--dry-run"}},
				{args: []string{"require", "psr/log:^1.0"}},
				{args: []string{"config", "conflict.psr/log", ">=1.1"}},
				{args: []string{"update"}, setup: addConflict},
			},
		},
		{
			name:    "scripts",
			fixture: "scripts",
			steps: []step{
				{args: []string{"install"}},
				{args: []string{"install", "--no-dev"}},
				{args: []string{"update"}},
				{args: []string{"run-script", "hello"}},
				{args: []string{"hello"}},
				{args: []string{"hi"}},
				{args: []string{"run-script", "env-test"}},
				{args: []string{"run-script", "composer-call"}},
				{args: []string{"run-script", "args", "--", "one", "two"}},
				{args: []string{"args", "three"}},
				{args: []string{"run-script", "fail"}},
				{args: []string{"run-script", "nested"}},
				{args: []string{"run-script", "bin-path"}},
				{args: []string{"run-script", "php-binary"}},
				{args: []string{"run-script", "--list"}},
				{args: []string{"run-script", "post-install-cmd", "--no-dev"}},
				{args: []string{"run-script", "nonexistent"}},
				{args: []string{"dump-autoload", "--no-scripts"}},
				{args: []string{"install", "--no-scripts"}},
				{args: []string{"list"}},
				{args: []string{"list", "--raw"}},
				{args: []string{"list", "--format=json"}},
				{args: []string{"help", "hello"}},
				{args: []string{"help", "hi"}},
			},
		},
		{
			// PHP-callable scripts run in the plugin runtime's PHP process
			// (docs/PLUGINS.md), which plugin tier 2 wires into the
			// dispatcher.
			name:    "scripts-php-callable",
			fixture: "scripts",
			steps: []step{
				{args: []string{"install", "--no-scripts"}},
				{args: []string{"run-script", "callable", "--", "x", "y"}},
				{args: []string{"run-script", "callable-io"}},
				{args: []string{"run-script", "timeout"}},
			},
		},
		{
			name:  "create-project",
			setup: vcsRepos,
			steps: []step{
				{args: []string{"create-project", "monolog/monolog", "monolog", "1.27.1", "--no-dev"}},
				{args: []string{"create-project", "acme/lib", "lib", "--repository={\"type\":\"vcs\",\"url\":\"@ROOT@/repos/lib\"}"}},
				{args: []string{"create-project", "acme/lib", "lib-src", "1.0.0", "--prefer-source", "--repository={\"type\":\"vcs\",\"url\":\"@ROOT@/repos/lib\"}"}},
				{args: []string{"create-project", "psr/log", "psr-log", "1.1.4", "--no-install"}},
				{args: []string{"create-project", "psr/log", "psr-log", "1.1.4"}},
				{args: []string{"create-project", "nothing/at-all", "nothing"}},
			},
		},
		{
			name:    "config",
			fixture: "config",
			steps: []step{
				{args: []string{"config", "--list"}},
				{args: []string{"config", "vendor-dir"}},
				{args: []string{"config", "sort-packages", "true"}},
				{args: []string{"config", "sort-packages"}},
				{args: []string{"config", "repositories.foo", "vcs", "https://example.org/foo.git"}},
				{args: []string{"config", "repositories.bar", `{"type": "path", "url": "../bar", "options": {"symlink": false}}`}},
				{args: []string{"config", "repositories"}},
				{args: []string{"config", "--unset", "repositories.foo"}},
				{args: []string{"config", "extra.foo.bar", "baz"}},
				{args: []string{"config", "--json", "extra.list", `["a", "b"]`}},
				{args: []string{"config", "--merge", "--json", "extra.list", `["c"]`}},
				{args: []string{"config", "extra"}},
				{args: []string{"config", "allow-plugins.acme/plugin", "true"}},
				{args: []string{"config", "preferred-install.acme/*", "source"}},
				{args: []string{"config", "process-timeout", "600"}},
				{args: []string{"config", "-g", "process-timeout", "900"}},
				{args: []string{"config", "-g", "--list"}},
				{args: []string{"config", "-g", "github-protocols", "https", "ssh"}},
				{args: []string{"config", "--global", "--unset", "github-protocols"}},
				{args: []string{"config", "--auth", "http-basic.repo.example.org", "user", "secret"}},
				{args: []string{"config", "--source", "process-timeout"}},
				{args: []string{"config", "-g", "--source", "process-timeout"}},
				{args: []string{"config", "name"}},
				{args: []string{"config", "description", "A new description."}},
				{args: []string{"config", "nothing-here"}},
				{args: []string{"config", "process-timeout", "abc"}},
				{args: []string{"config", "--unset", "sort-packages"}},
				{args: []string{"config", "--list", "--source"}},
				{args: []string{"config", "--absolute", "vendor-dir"}},
			},
		},
		{
			name: "init",
			steps: []step{
				{args: []string{
					"init", "--name", "acme/new", "--description", "A new package.", "--author", "Jane Doe <jane@example.org>",
					"--type", "library", "--homepage", "https://example.org", "--license", "MIT",
					"--require", "psr/log:^3.0", "--require-dev", "psr/container:^2.0", "--stability", "stable",
					"--autoload", "src/", "--no-interaction",
				}},
				{args: []string{"validate"}},
				{args: []string{"install"}},
				{args: []string{"init", "--name", "acme/again"}},
			},
		},
		{
			name:    "warm-worktree",
			fixture: "basic",
			steps: []step{
				{args: []string{"install"}},
				// a second worktree of the same project, from the warm
				// caches and store only: the network is disabled
				{
					args:  []string{"install"},
					dir:   "worktree",
					env:   []string{"COMPOSER_DISABLE_NETWORK=1"},
					setup: copyProject("worktree"),
				},
			},
		},
		{
			// The package store finds every object modified in place (what
			// a plugin rewriting its own files did to objects an earlier
			// maestro linked into vendor/) and heals itself from the
			// archives Composer's files cache keeps: offline, with
			// Composer's cache hit output. Then the archives are gone too,
			// and both tools download again.
			name:    "store-heal",
			fixture: "basic",
			steps: []step{
				{args: []string{"install"}},
				{
					args:  []string{"install", "-vv", "--no-progress"},
					dir:   "wt1",
					env:   []string{"COMPOSER_DISABLE_NETWORK=1"},
					setup: setups(copyProject("wt1"), taintStore),
				},
				{
					args:  []string{"install", "-vv", "--no-progress"},
					dir:   "wt2",
					setup: setups(copyProject("wt2"), removeAll("cache/files")),
				},
				{args: []string{"install", "-vv", "--no-progress"}, dir: "wt3", setup: copyProject("wt3")},
			},
		},
		{
			name:    "verbosity",
			fixture: "basic",
			steps: []step{
				{args: []string{"install", "-v"}},
				{args: []string{"install", "-vv", "--no-progress"}, setup: removeVendor},
				{args: []string{"install", "--quiet"}, setup: removeVendor},
				{args: []string{"update", "--dry-run", "-v"}},
				{args: []string{"update", "--dry-run", "-vv", "psr/log"}},
				{args: []string{"show", "--ansi"}},
				{args: []string{"show", "-t", "--ansi", "monolog/monolog"}},
				{args: []string{"why", "psr/log", "--ansi"}},
				{args: []string{"licenses", "--ansi"}},
				{args: []string{"outdated", "--ansi", "--direct"}},
				{args: []string{"validate", "--ansi"}},
				{args: []string{"list", "--ansi"}},
				{args: []string{"help", "install", "--ansi"}},
				{args: []string{"help", "require"}},
				{args: []string{"--version"}},
				{args: []string{"-d", "project", "show", "--name-only"}, dir: "."},
				{args: []string{"--working-dir=project", "dump-autoload", "-v"}, dir: "."},
				{args: []string{"archive", "--format=zip", "--dir=../archives"}},
				{args: []string{"archive", "psr/log", "--format=tar", "--dir=../archives", "--file=psr-log"}},
				{args: []string{"search", "psr/log", "--only-name"}},
				{args: []string{"home", "-s", "monolog/monolog"}},
				{args: []string{"clear-cache"}},
			},
		},
		{
			// The fixture sets archive-format and archive-dir in its
			// config, leaves a file out with archive.exclude and one with
			// a .gitattributes export-ignore.
			name:    "archive",
			fixture: "archive",
			steps: []step{
				{args: []string{"update"}},
				{args: []string{"archive"}},
				{args: []string{"archive", "--format=tar", "--file=filtered"}},
				{args: []string{"archive", "--format=tar", "--file=unfiltered", "--ignore-filters"}},
				{args: []string{"archive", "--format=tar.gz", "--file=gz"}},
				// several of packagist's releases of a package not installed match
				{args: []string{"archive", "psr/container", "^1.1", "--file=psr-container"}},
				{args: []string{"archive", "nope/nope-xyz"}, dir: "empty", setup: mkdir("empty")},
			},
		},
		{
			name:    "security",
			fixture: "security",
			steps: []step{
				{args: []string{"update"}},
				{args: []string{"update", "--no-security-blocking"}},
				{args: []string{"audit"}},
				{args: []string{"audit", "--format=json"}},
				{args: []string{"audit", "--format=plain"}},
				{args: []string{"audit", "--format=summary"}},
				{args: []string{"audit", "--locked", "--no-dev"}},
				{args: []string{"audit", "--abandoned=fail"}},
				{args: []string{"audit", "--ignore-severity=medium", "--ignore-severity=high"}},
				{args: []string{"audit", "--ignore-severity=medium", "--ignore-severity=high", "--format=json"}},
				{args: []string{"config", "--json", "audit.ignore", `["PKSA-hn62-zkx4-1y5q"]`}},
				{args: []string{"audit"}},
				{args: []string{"audit", "--format=summary"}},
				{args: []string{"config", "--unset", "audit.ignore"}},
				{args: []string{"audit", "--locked"}, setup: removeVendor},
				{args: []string{"config", "audit.block-insecure", "false"}},
				{args: []string{"install"}, setup: removeVendor},
				{args: []string{"install", "--audit"}},
				{args: []string{"install", "--audit", "--audit-format=json"}},
				{args: []string{"update", "--audit-format=json"}},
				{args: []string{"update", "--no-audit"}},
				{args: []string{"require", "guzzlehttp/psr7:^2.4", "--audit"}},
			},
		},
		{
			name: "global",
			steps: []step{
				{args: []string{"global", "require", "psr/log:^3.0"}},
				{args: []string{"global", "show"}},
				{args: []string{"glob", "config", "home"}},
				{args: []string{"g", "show"}},
				{args: []string{"global", "config", "home"}, env: []string{"COMPOSER=nope.json"}},
				{args: []string{"global", "-v", "show"}},
				{args: []string{"global", "show", "--format=json"}},
				{args: []string{"_complete", "-n", "-c2", "--shell=bash", "-icomposer", "-iglobal", "-iconf"}},
				{args: []string{"_complete", "-n", "-c3", "--shell=bash", "-icomposer", "-iglobal", "-iconfig", "-i--li"}},
				{args: []string{"global", "update"}},
				{args: []string{"global", "config", "--list"}},
				{
					args: []string{"global", "config", "repositories.tools", "path", "@ROOT@/tools/pwd"},
					setup: setups(
						writeFile("tools/pwd/composer.json", `{"name": "acme/pwd-tool", "version": "1.0.0", "bin": ["bin/pwd-tool"]}`),
						writeFile("tools/pwd/bin/pwd-tool", "#!/usr/bin/env php\n<?php\necho getcwd(), \"\\n\";\n"),
					),
				},
				{args: []string{"global", "require", "acme/pwd-tool"}},
				// runs in the directory global was started from
				{args: []string{"global", "exec", "pwd-tool"}},
				{args: []string{"global", "require", "nothing/at-all"}},
				{args: []string{"global", "remove", "not/required"}},
				{args: []string{"global", "remove", "psr/log"}},
			},
		},
		{
			name:    "validate",
			fixture: "validate",
			steps: []step{
				{args: []string{"validate"}},
				{args: []string{"validate", "--no-check-all"}},
				{args: []string{"validate", "--no-check-publish", "--no-check-version"}},
				{args: []string{"validate", "--strict", "--with-dependencies"}},
				{args: []string{"update", "--no-install", "--ignore-platform-reqs"}},
				{args: []string{"validate", "--check-lock"}},
				{args: []string{"dump-autoload"}},
			},
		},
		{
			// Questions answered on stdin: Composer is interactive with a
			// piped stdin when COMPOSER_TESTS_ARE_RUNNING=1 and Symfony's
			// SHELL_INTERACTIVE are set (Application::doRun).
			name:    "prompts",
			fixture: "require",
			setup:   vcsRepos,
			steps: []step{
				{args: []string{"require"}, env: interactive, stdin: "psr/log\n^1.1\n\n"},
				{args: []string{"require", "--dev", "psr/log"}, env: interactive, stdin: "yes\n"},
				{args: []string{"require", "psr/log"}, env: interactive, stdin: "no\n"},
				{args: []string{"require", "mockery/mockery", "--no-update"}, env: interactive, stdin: "yes\n"},
				{args: []string{"remove", "psr/log"}, env: interactive},
				{
					args: []string{"init"}, dir: "init", env: interactive, setup: mkdir("init"),
					stdin: "acme/prompted\nA prompted package.\nn\nstable\nlibrary\nMIT\nno\nno\nn\nyes\n",
				},
				{
					args:  []string{"create-project", "acme/lib", "lib", "--prefer-source", "--repository={\"type\":\"vcs\",\"url\":\"@ROOT@/repos/lib\"}"},
					dir:   ".",
					env:   interactive,
					stdin: "n\n",
				},
			},
		},
		{
			name:    "autoload",
			fixture: "autoload",
			steps: []step{
				{args: []string{"update"}},
				{args: []string{"dump-autoload", "-o"}},
				{args: []string{"dump-autoload", "-o", "--strict-psr"}},
				{args: []string{"dump-autoload", "--strict-ambiguous", "-o"}},
				{args: []string{"dump-autoload", "-a", "--no-dev"}},
				{args: []string{"dumpautoload", "-o"}},
				// a new class a real dump would add to the class map
				{args: []string{"dump-autoload", "-o", "--dry-run"}, setup: writeFile("project/src/DryRun.php", "<?php\n\nnamespace App;\n\nfinal class DryRun\n{\n}\n")},
				{args: []string{"dump-autoload", "--apcu-prefix=e2e"}},
				{args: []string{"install", "--no-autoloader"}, setup: removeDir("project/lib/vendor")},
				{args: []string{"install", "--optimize-autoloader", "--dry-run"}, setup: removeDir("project/lib/vendor")},
				{args: []string{"install", "--download-only"}},
				{args: []string{"install", "--classmap-authoritative", "--apcu-autoloader-prefix=e2e"}},
				{args: []string{"install", "--apcu-autoloader"}},
				{args: []string{"update", "--no-autoloader"}},
				{args: []string{"update", "-o"}},
				{args: []string{"update", "-a"}},
				{args: []string{"update", "--apcu-autoloader"}},
				{args: []string{"install", "-o", "--strict-psr-autoloader"}},
				{args: []string{"config", "optimize-autoloader", "true"}},
				{args: []string{"install"}},
				{args: []string{"config", "classmap-authoritative", "true"}},
				{args: []string{"config", "apcu-autoloader", "true"}},
				{args: []string{"install"}},
				{args: []string{"dump-autoload"}},
				{args: []string{"config", "--unset", "optimize-autoloader"}},
				{args: []string{"config", "--unset", "classmap-authoritative"}},
				{args: []string{"config", "--unset", "apcu-autoloader"}},
				{args: []string{"install", "--no-dev"}},
				{args: []string{"dump-autoload", "--dev"}},
				{args: []string{"show", "--self"}},
				{args: []string{"depends", "psr/log", "--recursive", "--tree"}},
				{args: []string{"prohibits", "psr/log", "2.0"}},
				{args: []string{"exec", "c-run"}},
				{args: []string{"exec", "c-run.php", "--", "arg"}},
				{args: []string{"exec", "c-run", "--", "--flag", "a b"}},
				{args: []string{"exec", "c-run"}, env: []string{"C_RUN_EXIT=3"}},
				{args: []string{"exec", "-n"}},
				{args: []string{"exec", "-l"}},
				{args: []string{"exec"}, env: interactive, stdin: "0\n"},
				{args: []string{"exec"}, env: interactive, stdin: "zzz\n"},
				{args: []string{"suggests", "--all"}},
				{args: []string{"remove", "--unused"}},
			},
		},
		{
			name:    "outdated",
			fixture: "update",
			steps: []step{
				{args: []string{"install"}},
				{args: []string{"outdated"}},
				{args: []string{"outdated", "--all"}},
				{args: []string{"outdated", "--minor-only"}},
				{args: []string{"outdated", "--patch-only", "--format=json"}},
				{args: []string{"outdated", "--major-only"}},
				{args: []string{"outdated", "--strict", "psr/log"}},
				{args: []string{"outdated", "--ignore=psr/log", "--direct"}},
				{args: []string{"show", "--outdated", "--ansi"}},
				{args: []string{"show", "-l", "--format=json"}},
				{args: []string{"update", "--minimal-changes"}},
				{args: []string{"update", "--patch-only"}},
				{args: []string{"update", "--bump-after-update=dev"}},
				{args: []string{"update", "--bump-after-update=no-dev"}},
				{args: []string{"config", "bump-after-update", "true"}},
				{args: []string{"update"}},
				{args: []string{"config", "--unset", "bump-after-update"}},
				{args: []string{"update", "--bump-after-update"}},
				{args: []string{"bump", "--dev-only"}},
				{args: []string{"update", "--root-reqs"}},
			},
		},
		{
			name:    "dists",
			fixture: "dists",
			setup:   setups(distArchives, dropXzOnWindows),
			steps: []step{
				{args: []string{"update"}},
				{args: []string{"install"}, setup: removeVendor},
				{args: []string{"reinstall", "acme/tar-gz", "acme/zip-nested"}},
				{args: []string{"install", "--no-cache"}, setup: removeVendor},
			},
		},
		{
			// No composer.json: require creates it.
			name: "require-new-project",
			steps: []step{
				{args: []string{"require", "psr/log:1.0.0", "--no-update"}},
			},
		},
		{
			name:    "install-no-dev",
			fixture: "nodev",
			steps: []step{
				{args: []string{"install", "--no-dev"}},
			},
		},
		{
			name:    "install-lock-to-lock",
			fixture: "upgrade",
			steps: []step{
				{args: []string{"install"}, env: []string{"COMPOSER=composer.json.before"}},
				{args: []string{"install"}},
			},
		},
	}
}

// setups runs several setups in order.
func setups(fns ...func(*testing.T, string)) func(*testing.T, string) {
	return func(t *testing.T, root string) {
		t.Helper()

		for _, fn := range fns {
			fn(t, root)
		}
	}
}

// removeAll returns a setup deleting a path under the scenario root.
func removeAll(rel string) func(*testing.T, string) {
	return func(t *testing.T, root string) {
		t.Helper()

		if err := os.RemoveAll(filepath.Join(root, rel)); err != nil {
			t.Fatal(err)
		}
	}
}

// taintStore writes into the installed package files in place, as an
// editor or a careless tool would, for both tools alike (Composer's vendor
// files are its own; maestro's are hardlinks to its store's objects where
// the filesystem has no reflinks, so the edit reaches the store: the risk
// deviation 1 accepts), then overwrites every object of maestro's package
// store (MAESTRO_CACHE_DIR's store/v1/files) that no package file links to.
// The next install must not spread either edit.
func taintStore(t *testing.T, root string) {
	t.Helper()

	taint := func(path string) error {
		f, err := os.OpenFile(path, os.O_WRONLY, 0)
		if err != nil {
			return err
		}

		_, err = f.WriteAt([]byte("tainted"), 0)
		if cerr := f.Close(); err == nil {
			err = cerr
		}

		return err
	}

	vendor := filepath.Join(root, "project", "vendor")

	err := filepath.WalkDir(vendor, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if rel, _ := filepath.Rel(vendor, path); d.IsDir() && (rel == "composer" || rel == "bin") {
			return filepath.SkipDir
		}

		if !d.Type().IsRegular() || strings.Count(filepath.ToSlash(path[len(vendor):]), "/") < 3 {
			return nil
		}

		return taint(path)
	})
	if err != nil {
		t.Fatal(err)
	}

	err = filepath.WalkDir(filepath.Join(root, "mcache", "store", "v1", "files"), func(path string, d os.DirEntry, err error) error {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}

		if err != nil || !d.Type().IsRegular() {
			return err
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		if linkCount(path, info) > 1 {
			return nil
		}

		return taint(path)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// removeVendor deletes the project's vendor directory.
func removeVendor(t *testing.T, root string) {
	t.Helper()

	if err := os.RemoveAll(filepath.Join(root, "project", "vendor")); err != nil {
		t.Fatal(err)
	}
}

// writeFile returns a setup writing a file under the scenario root.
func writeFile(rel, content string) func(*testing.T, string) {
	return func(t *testing.T, root string) {
		t.Helper()

		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(path, []byte(strings.ReplaceAll(content, "@ROOT@", rootPath(root))), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// vcsRepos creates the git repositories of the vcs and create-project
// scenarios: repos/lib (tags v1.0.0 and v1.1.0, a develop branch) and
// repos/tool (a main branch with a binary).
func vcsRepos(t *testing.T, root string) {
	t.Helper()

	lib := filepath.Join(root, "repos", "lib")
	gitRepo(t, lib,
		commit{files: map[string]string{
			"composer.json": libComposerJSON("acme/lib", `,
    "require": {"psr/log": "^1.0 || ^3.0"}`),
			"src/Lib.php": "<?php\n\nnamespace Acme\\Lib;\n\nfinal class Lib\n{\n}\n",
			"README.md":   "# acme/lib\n",
		}, tag: "v1.0.0"},
		commit{files: map[string]string{"src/Helper.php": "<?php\n\nnamespace Acme\\Lib;\n\nfinal class Helper\n{\n}\n"}, tag: "v1.1.0"},
		commit{branch: "develop", files: map[string]string{"src/Dev.php": "<?php\n\nnamespace Acme\\Lib;\n\nfinal class Dev\n{\n}\n"}},
	)

	tool := filepath.Join(root, "repos", "tool")
	gitRepo(t, tool,
		commit{files: map[string]string{
			"composer.json": `{
    "name": "acme/tool",
    "description": "A generated tool.",
    "license": "MIT",
    "bin": ["bin/tool"],
    "extra": {"branch-alias": {"dev-main": "1.x-dev"}}
}
`,
			"bin/tool": "#!/usr/bin/env php\n<?php\necho \"tool\\n\";\n",
		}},
	)
}

// vcsReposNext adds a release to repos/lib and a commit to repos/tool.
func vcsReposNext(t *testing.T, root string) {
	t.Helper()

	gitRepo(t, filepath.Join(root, "repos", "lib"),
		commit{files: map[string]string{"src/Next.php": "<?php\n\nnamespace Acme\\Lib;\n\nfinal class Next\n{\n}\n"}, tag: "v1.2.0"})
	gitRepo(t, filepath.Join(root, "repos", "tool"),
		commit{files: map[string]string{"src/Changed.php": "<?php\n// upstream\n"}})
}

// artifacts writes the artifact repository: acme/art 1.0.0 and 1.1.0.
func artifacts(t *testing.T, root string) {
	t.Helper()

	for _, v := range []string{"1.0.0", "1.1.0"} {
		zipFile(t, filepath.Join(root, "artifacts", "acme-art-"+v+".zip"), map[string]string{
			"art/": "",
			"art/composer.json": libComposerJSON("acme/art", `,
    "version": "`+v+`"`),
			"art/src/":        "",
			"art/src/Art.php": "<?php\n\nnamespace Acme\\Lib;\n\nfinal class Art\n{\n}\n",
		})
	}
}

// addConflict makes composer.json conflict with what it requires.
func addConflict(t *testing.T, root string) {
	t.Helper()

	path := filepath.Join(root, "project", "composer.json")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	data = []byte(strings.Replace(string(data), `"require": {`, `"conflict": {
        "psr/log": "1.1.4"
    },
    "require": {`, 1))

	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// copyProject returns a setup copying the scenario's fixture to another
// directory of the root (a second worktree).
func copyProject(dir string) func(*testing.T, string) {
	return func(t *testing.T, root string) {
		t.Helper()

		copyFixture(t, filepath.Join("testdata", "e2e", "basic"), filepath.Join(root, dir), root)
	}
}

// interactive makes both tools ask their questions on a piped stdin.
var interactive = []string{"COMPOSER_NO_INTERACTION=", "COMPOSER_TESTS_ARE_RUNNING=1", "SHELL_INTERACTIVE=1"}

// mkdir returns a setup creating a directory under the scenario root.
func mkdir(rel string) func(*testing.T, string) {
	return func(t *testing.T, root string) {
		t.Helper()

		if err := os.MkdirAll(filepath.Join(root, rel), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

// installPubKeys puts Composer's public keys (composer.github.io's
// snapshots.pub and releases.pub) into COMPOSER_HOME, so that the phar's
// "Checking pubkeys" passes and its exit code reflects the checks both
// tools run.
func installPubKeys(t *testing.T, root string) {
	t.Helper()

	for _, name := range []string{"keys.dev.pub", "keys.tags.pub"} {
		data, err := os.ReadFile(filepath.Join("testdata", "e2e", "keys", name))
		if err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(filepath.Join(root, "home", name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// pharOnlyDiagnose are the checks DiagnoseCommand runs only from a phar
// ("Checking pubkeys" with the key fingerprints, "Checking Composer
// version"): maestro is not a phar and updates itself from its own
// releases (PORTING.md deviation 4), so it runs them as Composer from
// source does, never.
var pharOnlyDiagnose = regexp.MustCompile(`(?s)Checking pubkeys: .*?\nChecking Composer version: [^\n]*\n`)

// diagnoseVariable are the lines of `composer diagnose` that name the
// running binary.
var diagnoseVariable = regexp.MustCompile(`(?m)^(PHP binary path:).*$`)

// normalizeDiagnose: see pharOnlyDiagnose and diagnoseVariable.
func normalizeDiagnose(s string) string {
	return diagnoseVariable.ReplaceAllString(pharOnlyDiagnose.ReplaceAllString(s, ""), "$1 <variable>")
}

// normalizeFund: FundCommand lists packages in the order the parallel
// metadata requests completed (nondeterministic in Composer, start order
// in maestro), so the vendor => url => packages mapping it prints is
// compared, not its order (the text format repeats a package line only
// when it changes, which the parsing undoes).
func normalizeFund(s string) string {
	fundings := map[string]map[string][]string{}

	if trimmed := strings.TrimSpace(s); strings.HasPrefix(trimmed, "{") {
		if err := json.Unmarshal([]byte(trimmed), &fundings); err != nil {
			return s
		}
	} else {
		var vendor string
		var packages []string

		for line := range strings.SplitSeq(s, "\n") {
			line = strings.TrimSuffix(line, "\r") // PHP_EOL on Windows

			switch {
			case strings.HasPrefix(line, "    "):
				if vendor != "" {
					fundings[vendor][strings.TrimSpace(line)] = packages
				}
			case strings.HasPrefix(line, "  "):
				packages = strings.Split(strings.TrimSpace(line), ", ")
			case line != "" && !strings.Contains(line, " "):
				vendor = line
				fundings[vendor] = map[string][]string{}
			}
		}
	}

	for _, links := range fundings {
		for _, packages := range links {
			slices.Sort(packages)
		}
	}

	out, err := json.MarshalIndent(fundings, "", "  ")
	if err != nil {
		return s
	}

	return string(out)
}
