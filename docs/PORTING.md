# Porting Composer to Go

maestro is a 1:1 port of Composer 2.10.3 to Go. It is a drop-in replacement:
same commands, options, output, exit codes, files written (composer.lock,
vendor/composer/*, vendor/bin proxies), byte for byte. It never runs or
embeds composer.phar. `php` is only used where Composer itself executes PHP
code on the project's behalf: scripts, plugins (through maestro's own PHP
shim), platform detection and `exec`.

## Reference sources

`.ref/` (gitignored) holds the exact sources being ported, with their tests;
`ref-sync` (in the devenv shell) recreates it:

| Path | What |
| --- | --- |
| `.ref/composer` | composer/composer at tag 2.10.3, with `vendor/` installed |
| `.ref/semver` | composer/semver 3.4.4 |
| `.ref/class-map-generator` | composer/class-map-generator 1.7.3 |
| `.ref/spdx-licenses` | composer/spdx-licenses 1.6.0 |
| `.ref/metadata-minifier` | composer/metadata-minifier 1.0.1 |

The PHP source is the specification. When the Go and PHP disagree, the Go is
wrong. Port logic faithfully, including its quirks, ordering, error messages
and edge cases. Do not "improve" behaviour unless it is one of the deliberate
deviations listed below.

## Deliberate deviations (the only ones)

1. **Package store, pnpm style.** Dist archives are extracted once into a
   per-file content-addressed store (`internal/store`) shared by every project
   and git worktree on the machine. Files are imported into `vendor/` by
   reflink clone, then hardlink, then copy (pnpm's `auto`). File contents,
   modes, symlinks and directory layout match what Composer + `unzip` would
   produce; file mtimes are not preserved.
2. **No external extractors.** zip/tar/gz/bz2/xz extraction is native Go,
   reproducing exactly what Composer's preferred path (system `unzip -qq`,
   `tar`) produces on Unix.
3. **Speed.** Parallelism and caching wherever results stay identical.
4. **self-update** updates maestro from its GitHub releases.
5. **Plugins** run in maestro's own PHP shim (`internal/plugin/php`), which
   reimplements Composer's public PHP plugin API, not Composer's code.

## Layout

Go packages mirror Composer namespaces. `package` is reserved in Go, so
`Composer\Package` is `internal/pkg`.

| Go package | Ports |
| --- | --- |
| `internal/php` | PHP runtime semantics the port relies on: arrays (ordered maps with PHP key coercion), `json_decode`/`json_encode` (all flags Composer uses), `var_export`, comparisons and sorts (PHP 8, stable), `version_compare`, `strnatcmp`, string helpers, PCRE via regexp2 |
| `internal/semver` | composer/semver |
| `internal/classmap` | composer/class-map-generator |
| `internal/spdx` | composer/spdx-licenses (+ its JSON data) |
| `internal/console` | the Symfony Console subset Composer uses: input parsing, output formatting/styles/verbosity, tables, progress bar, questions, application help/list |
| `internal/io` | Composer\IO |
| `internal/util` (+ subpackages) | Composer\Util |
| `internal/json` | Composer\Json + res/*.json schemas |
| `internal/pkg` (+ `loader`, `dumper`, `version`, `archiver`, `comparer`) | Composer\Package |
| `internal/config` | Composer\Config, Composer\Config\* |
| `internal/platform` | Composer\Platform (php/extension detection runs `php` once, cached) |
| `internal/repository` (+ `vcs`) | Composer\Repository |
| `internal/resolver` | Composer\DependencyResolver |
| `internal/store` | the per-file content-addressed store (deviation 1) |
| `internal/downloader` | Composer\Downloader |
| `internal/installer` | Composer\Installer\* (namespace) |
| `internal/autoload` | Composer\Autoload |
| `internal/eventdispatcher`, `internal/script` | Composer\EventDispatcher, Composer\Script |
| `internal/plugin` (+ `php/` shim) | Composer\Plugin |
| `internal/advisory`, `internal/policy`, `internal/filterlist` | the namespaces of those names |
| `internal/composer` | Composer\Composer, Factory, Installer (the top-level classes), Cache, Locker glue |
| `internal/command` | Composer\Command\*, Composer\Console\Application |
| `cmd/maestro` | entry point; also runs as `composer` |

Lower packages never import higher ones. Where PHP has a circular
reference, break it with an interface in the lower package.

## Rules for every port

- Each Go file starts its doc/comment with the PHP file(s) it ports, e.g.
  `// Ports src/Composer/Semver/VersionParser.php.`
- Keep PHP names recognisable (`NormalizeVersion` for `normalize`,
  `ParseConstraints`, ...) so the two can be read side by side.
- Exceptions become Go errors carrying the same message text. Where Composer
  distinguishes exception classes (callers catch specific ones), use
  distinct error types and `errors.As`.
- Any user-visible string (messages, warnings, help text, JSON output)
  must be copied exactly, including punctuation, spacing and `<info>` tags.
- Data that Composer keeps as free-form PHP arrays (extra, scripts,
  autoload, config, raw composer.json) is a `*php.Array`, so key order,
  int/string key coercion and list/object encoding behave as in PHP.
- Concurrency is allowed only where output and side effects stay identical
  and deterministic.

## Tests

1. **Port Composer's own tests.** Every PHPUnit test in the reference repos
   that covers a ported class is ported to a Go test in the same package,
   named after it (`TestVersionParser_NormalizeSucceeds` for
   `VersionParserTest::testNormalizeSucceeds`), with every data provider
   case. Fixtures (`*.test`, `*.json`, autoload goldens, ...) are copied
   verbatim into the package's `testdata/`, preserving their paths. They are
   MIT licensed; `testdata/LICENSE-composer` carries the notice.
2. **Differential oracle tests.** For pure logic, also generate goldens by
   running the real PHP implementation from `.ref/` with `php` (in the dev
   shell). Generator scripts live in `tools/oracle/<package>/` and write into
   the package's `testdata/`. Goldens are committed, so `go test` needs
   neither php nor the network.
3. **End to end.** `cmd/maestro` tests (`MAESTRO_E2E=1`) run real Composer
   2.10.3 (a pinned phar, downloaded into the test cache with a checksum
   check, never shipped) and maestro on the same projects and compare
   output, lock files and vendor trees.

Run everything inside the devenv shell (`devenv shell -- bash -c '...'` from
the repo root; it has Go, golangci-lint, php, unzip, gh, just). A port
is done when `go vet`, `golangci-lint run` and `go test -race` pass for its
packages (`CGO_ENABLED=1` for `-race`).

## Working alongside other ports

Several packages are ported at once in this tree. Build and test only your
own packages (`go test ./internal/semver/...`), not `./...`. Add a
dependency with `go get module@version` only; never run `go mod tidy`.
Don't edit other packages. If you need something from one that doesn't
exist yet, say so in your report.

## Plugin requirements on every package

Plugins run against maestro's Go state through the PHP shim specified in
docs/PLUGINS.md. Its section 7 lists what the other packages must provide
(change counters on packages, the local repository and Config; installer
calls routed through an overridable interface; no package-level mutable
state, so Installer, Factory and Application are re-entrant; ...). Read it
before porting any package it names.
