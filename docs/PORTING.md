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
   and git worktree on the machine. Files are imported into `vendor/` as
   pnpm's `auto` does: reflink clone where the filesystem can, else
   hardlink, else copy (`MAESTRO_PACKAGE_IMPORT_METHOD=clone|hardlink|copy`
   forces one method). Composer plugins (`composer-plugin`,
   `composer-installer`) are never hard-linked, since plugins rewrite their
   own files in place. Accepted risk, as with pnpm: a tool that writes in
   place into a hard-linked vendor file changes that file in every project
   linked to it until their next install of the release; the store notices
   before importing it again (size, mode and a hash-derived modification
   time are checked on every import) and heals itself, so the edit never
   reaches a later install. The dist archives stay in Composer's files
   cache exactly as Composer keeps them, which decides "Loading from cache"
   vs "Downloading" and heals the store. File contents, modes, symlinks and
   directory layout match what Composer + `unzip` would produce; file
   mtimes are not preserved. internal/store's documentation states the
   exact guarantees.
2. **No external extractors.** zip/tar/gz/bz2/xz extraction is native Go,
   reproducing exactly what Composer's preferred path (system `unzip -qq`,
   `tar`) produces on Unix. In macOS's C locale, unzip writes U+0080 to
   U+00FF as Latin-1 bytes APFS refuses and Composer falls back to
   ZipArchive; maestro refuses such names there rather than escaping them
   as glibc's unzip does. On Windows, where Composer extracts zips with
   ZipArchive or 7-Zip, maestro extracts as unzip would on Unix.
3. **Speed.** Parallelism and caching wherever results stay identical.
4. **self-update** updates maestro from its GitHub releases.
5. **Plugins** run in maestro's own PHP shim (`internal/plugin/php`), which
   reimplements Composer's public PHP plugin API, not Composer's code.
6. **`--version`** prints Composer's exact lines, then one extra stderr
   line, `maestro version X`, so people can tell which tool they run;
   anything parsing stdout or the first line sees Composer's output.
7. **No stray empty `vendor/bin`.** Composer's
   `BinaryInstaller::removeBinaries` creates the bin dir even when the
   removed package has no binaries, so an emptied bin dir survives or not
   depending on the order removals finish. maestro only touches the bin dir
   for packages with binaries: an empty bin dir is never created by a
   removal, and the last binary removed takes it away.

## Layout

Go packages mirror Composer namespaces. `package` is reserved in Go, so
`Composer\Package` is `internal/pkg`.

| Go package | Ports |
| --- | --- |
| `internal/php` | PHP runtime semantics the port relies on: arrays (ordered maps with PHP key coercion), `json_decode`/`json_encode` (all flags Composer uses), `var_export`, comparisons and sorts (PHP 8, stable), `version_compare`, `strnatcmp`, `+`, `sprintf`, string helpers (`strip_tags`, `levenshtein`, `stripcslashes`, `escapeshellarg`, `basename`, ...), and its own PCRE2 10.48-compatible regex engine (internal/php/doc.go says why) |
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

Cycle-breaking decisions already made:

- Repository and downloader types are registered by name, as Composer's
  Factory does with `setRepositoryClass` / `setDownloader`:
  `internal/repository` defines the interfaces, the generic repositories and
  RepositoryManager with a type registry; `internal/repository/composerrepo`
  (ComposerRepository) and `internal/repository/vcs` (VcsRepository and its
  drivers) import it; `internal/composer`'s Factory wires them together. The
  same for `internal/downloader` (DownloadManager, file/archive/path
  downloaders) and `internal/downloader/vcs` (Git/Hg/Svn/Fossil/Perforce
  downloaders).
- `RepositorySet` lives in `internal/repository` without its createPool*
  methods; those are functions in `internal/resolver` taking the set.
- `Composer\Package\Locker` is `internal/locker` (it needs repositories).
- Composer\Util\{Git,Hg,Svn,Perforce} are `internal/util/vcs`.

## Rules for every port

- Each Go file starts its doc/comment with the PHP file(s) it ports, e.g.
  `// Ports src/Composer/Semver/VersionParser.php.`
- Keep PHP names recognisable (`NormalizeVersion` for `normalize`,
  `ParseConstraints`, ...) so the two can be read side by side.
- Exceptions become Go errors carrying the same message text. Where Composer
  distinguishes exception classes (callers catch specific ones), use
  distinct error types and `errors.As`.
- Every exception that can reach the console also carries its throw site,
  which Symfony prints ("In Factory.php line 317:"): error types embed
  `phperr.Site` (internal/phperr) and each construction sets
  `Site: phperr.At("<PHP file basename>", <line of the new expression>)`;
  a `$previous` exception goes in the type's previous field
  (`phperr.Chained`, never Unwrap); a class `util.PHPClassOf` cannot know
  is given by a `PHPClass()` method. tools/oracle/errors checks the
  rendering against Composer.
- Any user-visible string (messages, warnings, help text, JSON output)
  must be copied exactly, including punctuation, spacing and `<info>` tags.
- Data that Composer keeps as free-form PHP arrays (extra, scripts,
  autoload, config, raw composer.json) is a `*php.Array`, so key order,
  int/string key coercion and list/object encoding behave as in PHP.
- Concurrency is allowed only where output and side effects stay identical
  and deterministic.
- Line endings: where Composer or Symfony Console use `PHP_EOL` (every line
  `writeln()` and `write($msg, true)` end, `newLine()`, messages joined
  with it), the port uses `php.EOL`, which is `"\r\n"` on Windows; where
  they write a literal `"\n"`, so does the port. Tests that compare
  captured output with `"\n"` expectations pass it through
  `php.NormalizeEOL` (Symfony's `getDisplay(true)`); tests about a
  `PHP_EOL` site force Windows' with `php.SetEOLForTest(t, "\r\n")`.

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
   neither php nor the network. A golden over 1 MB is committed gzipped
   (`*.json.gz`, written by the oracle script, read with compress/gzip).
3. **End to end.** `cmd/maestro` tests (`MAESTRO_E2E=1`) run real Composer
   2.10.3 (a pinned phar, downloaded into the test cache with a checksum
   check, never shipped) and maestro on the same projects and compare
   output, lock files and vendor trees.

Run everything inside the devenv shell (`devenv shell -- bash -c '...'` from
the repo root; it has Go, golangci-lint, php, unzip, gh, just). A port
is done when `go vet`, `golangci-lint run` and `go test -race` pass for its
packages (`CGO_ENABLED=1` for `-race`).

## Tooling hazard: `\u` escapes

Writing files through the editor tools can turn `\uXXXX` escape sequences
into the literal characters they name. In Go source, write such strings
with explicit byte escapes or build them in code, and check any file that
must contain a literal `\u` with `grep -n '\\u'` after writing it.

## Regular expressions

Use `internal/php`'s `Compile`/`MustCompile` and the `Preg*` functions with
Composer's patterns pasted verbatim. It is a PCRE2-compatible engine
(recursion, subroutines and all); don't use `regexp` or regexp2 for ported
patterns.

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
