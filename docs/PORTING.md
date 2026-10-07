# Porting Composer to Go

maestro is a port of Composer 2.10.3 to Go and a drop-in replacement for it:
anything that drives Composer (people, CI, scripts, plugins, other tools)
must work unchanged against maestro. It never runs or embeds composer.phar.
`php` is only used where Composer itself executes PHP code on the project's
behalf: scripts, plugins (through maestro's own PHP shim), platform
detection and `exec`.

Compatibility is defined by a contract, not by byte-for-byte output
everywhere: what tools and people depend on is frozen and must match
Composer exactly; how errors and diagnostics are presented is maestro's own
(issue #13). "The contract" below says which is which.

## The contract

### Frozen: identical to Composer, byte for byte

- **CLI surface.** Commands and their aliases, arguments, options, short
  options, defaults, validation (what is accepted and what is refused),
  `list`/`help` contents and option suggestions/completion.
- **Exit codes**, for every outcome, including partial ones
  (`outdated --strict`, `audit` with advisories or abandoned packages,
  `validate` with warnings, `check-platform-reqs` failures, script failures
  passing their code through).
- **Files written.** composer.json and composer.lock (key order,
  indentation, escaping, trailing newline, content-hash), everything in
  vendor/composer/* (autoloaders, installed.json, installed.php,
  platform_check.php), vendor/bin proxies, the installed package trees
  (deviation 1 states the exact guarantee), `archive` output, and the
  caches Composer shares with maestro (repository metadata, dist archives,
  VCS mirrors) in Composer's layout.
- **Inputs read.** composer.json schema and semantics, config keys,
  auth.json, `COMPOSER*` environment variables, repository protocols and
  their metadata formats, version and constraint semantics, resolution
  results (same lock for the same inputs and repositories).
- **Machine-readable output.** Every `--format=json`, `--format=summary`
  and other non-table format (`show`, `outdated`, `audit`, `licenses`,
  `fund`, `search`, `check-platform-reqs`), `show --name-only`/`-N`,
  `show --path`/`-P`, `config <key>` reads and `config --list`,
  `status`, `depends`/`why` and `prohibits`/`why-not`, `--version`'s first
  line, and anything else tools or CI are known to parse. Which stream a
  line goes to (stdout or stderr) is frozen for all output, since scripts
  redirect them separately. Undecorated output (`--no-ansi`, no tty)
  carries no escape sequences.
- **Interaction.** Which questions are asked, in which order, their
  defaults, and what `--no-interaction` answers.
- **Scripts and events.** Which run, in which order, with which arguments,
  working directory, environment (`COMPOSER_*`, `PATH` with vendor/bin,
  `COMPOSER_DEV_MODE`, ...) and stdio, and how their exit codes propagate.
- **Plugin API.** What docs/PLUGINS.md specifies: plugins see Composer's
  classes, methods, events and exception classes, and keep working.

### Free: maestro's own

- **Error rendering.** No need to imitate PHP's exception boxes, PHP class
  names, "In File.php line N:" headings, "Exception trace:" call stacks at
  `-v`, TypeError sites or `phar://` source paths. The information (what
  failed, on which input, what to do) and the exit code stay.
- **Warnings and deprecation notices.** Same information, maestro's
  wording and format.
- **Transport errors.** Clear messages instead of curl's or PHP's stream
  wrapper's exact strings.
- **`-v`/`-vv`/`-vvv` debug output.** For debugging maestro, not a mirror
  of Composer's internals.
- **Human progress output of successful runs** ("Loading composer
  repositories with package information", "Installing x/y (1.0.0)", the
  update summary, ...). Users and documentation expect it, so it keeps
  Composer's text unless there is a reason to change it, but it is not
  frozen.

maestro's own format for errors, warnings, deprecation notices and hints
is `internal/ui`'s (#13 step 4): a labelled headline with the message,
then its causes (previous errors), hints, the usage of a command given
wrong input and, at `-v`, debugging details (Go error types, a plugin
exception's PHP trace, the exit code). Other free output keeps Composer's
text; it no longer has to match exactly, and nothing new is built only to
reproduce PHP's presentation.

### Presentation of free output

maestro's own presentation (error rendering, warnings, progress, prompts)
may be built with charm.sh components (lipgloss for styling and layout,
bubbles/huh for progress and prompts, ...), under these rules:

- Only on the free surface. Machine-readable output, files and anything
  else frozen are written as Composer writes them, never through a
  renderer.
- Styling only when decorated: with `--no-ansi`, `NO_COLOR`, a non-tty
  stream or Composer's own decoration rules saying no, output is plain
  text with no escape sequences, as Composer's is. Verbosity flags
  (`-q`, `-v`...) keep their meaning.
- Interactive components only where Composer asks a question on a tty and
  interaction is on; the question asked, its default and the
  `--no-interaction` answer stay frozen, and a non-tty or
  `--no-interaction` run never starts a TUI.
- Streams stay frozen: what Composer writes to stderr, maestro writes to
  stderr, whatever renders it.
- Windows consoles and `"\r\n"` endings (see "Line endings") keep working.
- Rendering lives in one place (a maestro-owned package over
  `internal/console`/`internal/io`), not scattered through the ports, so
  ported code keeps reporting information and the renderer decides how it
  looks.

## Reference sources

`.ref/` (gitignored) holds the exact sources being ported, with their tests;
`ref-sync` (in the devenv shell, also `just ref`) recreates it:

| Path | What |
| --- | --- |
| `.ref/composer` | composer/composer at tag 2.10.3, with `vendor/` installed (`--no-dev`) |
| `.ref/semver` | composer/semver 3.4.4 |
| `.ref/class-map-generator` | composer/class-map-generator 1.7.3 |
| `.ref/spdx-licenses` | composer/spdx-licenses 1.6.0 |
| `.ref/metadata-minifier` | composer/metadata-minifier 1.0.1 |
| `.ref/jsonlint` | seld/jsonlint 1.12.1 |

Other libraries Composer ships are ported from `.ref/composer/vendor/`, at
the version Composer 2.10.3's lock file pins: symfony/console 5.4 (the
subset in `internal/console`), justinrainbow/json-schema 6.10.0 (the parts
in `internal/json/jsonschema`) and composer/ca-bundle (`internal/util/http`).

The PHP source is the specification of behaviour. For the frozen surface,
when the Go and PHP disagree, the Go is wrong: port logic faithfully,
including its quirks, ordering, messages and edge cases, and do not
"improve" it unless it is one of the deliberate deviations below. For the
free surface, the PHP decides what information is reported and when, not
its exact presentation.

## Deliberate deviations

These change frozen behaviour on purpose; nothing else may.

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
   exact guarantees. Source installs cloned from the mirror cache are kept
   in the store too, `.git` included, and imported unshared instead of
   cloned again when the mirror, the reference, git and its configuration
   are unchanged; their reflog times, index stat data and untracked-cache
   ident (the work tree path) are rewritten to what a fresh clone would hold (`internal/downloader/vcs/gitstore.go`).
   The store lives in maestro's own cache directory
   (`MAESTRO_CACHE_DIR`, else `$XDG_CACHE_HOME/maestro`, else the platform
   cache directory; `internal/cache.Dir`).
2. **No external extractors.** zip/tar/gz/bz2/xz extraction is native Go
   (`internal/archive`), reproducing exactly what Composer's preferred path
   (system `unzip -qq`, `tar`) produces on Unix. In macOS's C locale, unzip
   writes U+0080 to U+00FF as Latin-1 bytes APFS refuses and Composer falls
   back to ZipArchive; maestro refuses such names there rather than
   escaping them as glibc's unzip does. On Windows, where Composer extracts
   zips with ZipArchive or 7-Zip, maestro extracts as unzip would on Unix.
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
| `internal/php` | PHP runtime semantics the port relies on: arrays (ordered maps with PHP key coercion), `json_decode`/`json_encode` (all flags Composer uses), `var_export`, comparisons and sorts (PHP 8, stable), `version_compare`, `strnatcmp`, `+`, `sprintf`, string helpers (`strip_tags`, `levenshtein`, `stripcslashes`, `escapeshellarg`, ...), the path functions (`getcwd`, `realpath`, `dirname`, `basename`, `pathinfo`; the lint forbids Go's `os.Getwd`, `filepath.Abs` and `filepath.EvalSymlinks` elsewhere), and its own PCRE2 10.48-compatible regex engine (internal/php/doc.go says why) |
| `internal/phperr` | an error's PHP previous exception (`Chained`, `PreviousOf`) and the root Composer's sources are reported under (see "Errors") |
| `internal/semver` | composer/semver |
| `internal/classmap` | composer/class-map-generator |
| `internal/spdx` | composer/spdx-licenses (+ its JSON data) |
| `internal/metadataminifier` | composer/metadata-minifier |
| `internal/console` | the Symfony Console subset Composer uses: input parsing, output formatting/styles/verbosity, tables, progress bar, questions, application help/list |
| `internal/ui` | maestro's own presentation of errors and diagnostics (lipgloss; "Presentation of free output") |
| `internal/io` | Composer\IO |
| `internal/util` | Composer\Util (filesystem, ProcessExecutor, Platform, Url, Zip/Tar, Loop, ...) |
| `internal/util/http` | Composer\Util\{HttpDownloader, RemoteFilesystem, AuthHelper, GitHub, GitLab, Bitbucket, Forgejo, StreamContextFactory, ProxyManager, Http\*}, composer/ca-bundle |
| `internal/util/vcs` | Composer\Util\{Git, Hg, Svn, Perforce} |
| `internal/util/fspath` | Composer\Util\Filesystem's path functions that touch no file (`normalizePath`, `isAbsolutePath`), for the packages below internal/util too |
| `internal/util/fsstate` | nothing: how maestro's own caches (deviation 3) tell a file changed (`ID`, `Stamp`, the racy-timestamp `Margin`), read a file as it was at one moment (`ReadStable`), name entries (`KeyHash`) and replace files atomically (`WriteAtomic`) |
| `internal/json` | Composer\Json + res/*.json schemas |
| `internal/json/jsonlint` | seld/jsonlint |
| `internal/json/jsonschema` | the justinrainbow/json-schema format validators Composer's schema uses |
| `internal/pkg` (+ `loader`, `dumper`, `version`, `archiver`, `comparer`) | Composer\Package |
| `internal/locker` | Composer\Package\Locker |
| `internal/config` | Composer\Config, Composer\Config\* |
| `internal/cache` | Composer\Cache, and where maestro keeps its own files |
| `internal/platform` | Composer\Platform (php/extension detection runs `php` once, cached; on Linux its result is reused across runs while nothing it came from changed) |
| `internal/filter` | Composer\Filter\PlatformRequirementFilter |
| `internal/repository` | Composer\Repository: interfaces, generic repositories, RepositoryManager, RepositorySet |
| `internal/repository/composerrepo` | Composer\Repository\ComposerRepository |
| `internal/repository/vcs` | Composer\Repository\VcsRepository and Vcs\* drivers |
| `internal/resolver` | Composer\DependencyResolver |
| `internal/resolver/operation` | Composer\DependencyResolver\Operation |
| `internal/store` | the per-file content-addressed store (deviation 1) |
| `internal/archive` | native archive extraction (deviation 2) and ArchiveDownloader's choice of package directory |
| `internal/archive/deflate64` | Deflate64 decompression (zip method 9) for it: Go's compress/flate inflater with Deflate64's window, length and distance codes |
| `internal/downloader` | Composer\Downloader: DownloadManager, file/archive/path downloaders |
| `internal/downloader/vcs` | Composer\Downloader\{Git, Hg, Svn, Fossil, Perforce}Downloader |
| `internal/installer` | Composer\Installer\* (namespace) |
| `internal/autoload` | Composer\Autoload |
| `internal/eventdispatcher`, `internal/script` | Composer\EventDispatcher, Composer\Script |
| `internal/plugin` | Composer\Plugin, and the Go side of the plugin runtime |
| `internal/plugin/php` | the PHP shim plugins run in (deviation 5, docs/PLUGINS.md) |
| `internal/plugin/rpc` | maestro's half of the IPC channel to the shim (docs/PLUGINS.md §5.3, §6) |
| `internal/plugin/shimbuild` | what the shim's build tools share: Compiler.php's choice of vendored files |
| `internal/advisory`, `internal/policy`, `internal/filterlist` (+ `source`) | the namespaces of those names |
| `internal/composer` | Composer\Composer, Factory, Installer (the top-level classes) |
| `internal/command` | Composer\Command\*, Composer\Console\Application |
| `cmd/maestro` | entry point; also runs as `composer` |

Test-only packages, imported only from `_test.go` files: `internal/testutil`
(shared helpers), `internal/util/processmock` (ProcessExecutorMock),
`internal/util/http/httpmock`, `internal/platform/platformmock`,
`internal/archive/archivetest` (archive corpora, runners for the real
extractors), `internal/command/commandtest` (Composer's TestCase and
ApplicationTester) and `internal/command/errorstest` (the errors oracle).

Lower packages never import higher ones. Where PHP has a circular
reference, break it with an interface in the lower package.

Cycle-breaking decisions already made:

- Repository and downloader types are registered by name, as Composer's
  Factory does with `setRepositoryClass` / `setDownloader`:
  `internal/repository` defines the interfaces, the generic repositories and
  RepositoryManager with a type registry; `internal/repository/composerrepo`
  and `internal/repository/vcs` import it; `internal/composer`'s Factory
  wires them together. The same for `internal/downloader` and
  `internal/downloader/vcs`.
- `RepositorySet` lives in `internal/repository` without its createPool*
  methods; those are functions in `internal/resolver` taking the set.
- `Composer\Package\Locker` is `internal/locker` (it needs repositories).
- Composer\Util\{Git,Hg,Svn,Perforce} are `internal/util/vcs`.

## Rules for every port

- Each Go file starts its doc/comment with the PHP file(s) it ports, e.g.
  `// Ports src/Composer/Semver/VersionParser.php.`
- Keep PHP names recognisable (`NormalizeVersion` for `normalize`,
  `ParseConstraints`, ...) so the two can be read side by side.
- User-visible strings on the frozen surface (machine-readable output,
  help text, option descriptions, questions, files) are copied exactly,
  including punctuation, spacing and `<info>` tags. On the free surface,
  Composer's text is the default (see "The contract").
- State lives in the object that owns it. Package-level mutable state is
  only a port of a PHP static (`Git::$version`, `ErrorHandler::$io`,
  `ProxyManager`'s instance, `CaBundle`'s caches, ...), which is
  process-wide in Composer too, nested runs from plugins included. A
  knob or observation point a test needs (a limit, a time margin, a
  counter, a callback) is a field of the owning object whose zero value
  is the production behaviour, never a package variable a test assigns.
- A port of public PHP API stays while Go code or a ported test calls it;
  with neither it is deleted, as is any other code nothing reaches
  (`tools/deadcode`, run by `just check` and CI). Plugins never reach Go's
  ports of the libraries the shim vendors (composer/semver,
  spdx-licenses, ...): they get the PHP originals. A helper only tests
  use lives in a `_test.go` file (`export_test.go` for external tests).
- Data that Composer keeps as free-form PHP arrays (extra, scripts,
  autoload, config, raw composer.json) is a `*php.Array`, so key order,
  int/string key coercion and list/object encoding behave as in PHP.
- Concurrency is allowed only where output and side effects stay identical
  and deterministic. Asynchronous work completes in start order where
  Composer's completion order is nondeterministic (`util.Scheduler`).
- Float arithmetic that ports PHP stays unfused: PHP rounds after every
  opcode, but Go may fuse `a - b*c` into one FMA on arm64. Wrap products
  in an explicit `float64(...)` (jsonschema's `fmodNonZero`).
- `date()`, `new DateTime()` and friends use PHP's default time zone,
  `php.DefaultTimezone()` (the user's `date.timezone`, else UTC), never
  Go's local zone.
- Line endings: where Composer or Symfony Console use `PHP_EOL` (every line
  `writeln()` and `write($msg, true)` end, `newLine()`, messages joined
  with it), the port uses `php.EOL`, which is `"\r\n"` on Windows; where
  they write a literal `"\n"`, so does the port. Tests that compare
  captured output with `"\n"` expectations pass it through
  `php.NormalizeEOL` (Symfony's `getDisplay(true)`); tests about a
  `PHP_EOL` site force Windows' with `php.SetEOLForTest(t, "\r\n")`.

### Errors

- Exceptions become Go errors carrying the same information, with
  Composer's message text by default. An error that stands for a PHP
  exception says which (`phperr.Exception`: a `PHPClass()` method, and
  `PHPCode()` when its code is not 0); `phperr.ClassOf` and
  `phperr.InstanceOf` resolve any error, a Go wrapper adding context
  standing for the exception it wraps and a plain Go error for a
  `\RuntimeException`. A `catch (X $e)` is `phperr.InstanceOf(err, X)`,
  which follows PHP's class hierarchy (`internal/phperr`'s class table,
  checked against PHP and against every class the sources name); use
  `errors.As` only to read the fields of a specific Go type.
- Errors that can reach a plugin keep their PHP class: plugins catch
  Composer's exception classes, and docs/PLUGINS.md (D12) maps Go errors
  to them both ways through the same `phperr.ClassOf`; a `$previous`
  exception goes in the type's previous field (`phperr.Chained`, never
  Unwrap, which a test in `internal/phperr` checks). PHP's engine errors
  (TypeError, ValueError, ...) are `php.EngineError`.
- Errors carry no throw site or PHP call stack: rendering is maestro's
  own (#13), so Composer's file and line of a `new` expression, the
  frames of an exception's trace and PHP's TypeError call site (",
  called in X on line N") are not ported. The plugin runtime keeps the
  frames PHP code finds with `debug_backtrace()` (`composer.Runtime`'s
  `PushFrame`, `internal/plugin/frames.go`).

## Tests

1. **Port Composer's own tests.** Every PHPUnit test in the reference repos
   that covers a ported class is ported to a Go test in the same package,
   named after it (`TestVersionParser_NormalizeSucceeds` for
   `VersionParserTest::testNormalizeSucceeds`), with every data provider
   case. Fixtures (`*.test`, `*.json`, autoload goldens, ...) are copied
   verbatim into the package's `testdata/`, preserving their paths. They are
   MIT licensed; `testdata/LICENSE-composer` carries the notice. A test
   that only asserts free output (an error's exact text or rendering)
   asserts the information it carries instead.
2. **Differential oracle tests.** For pure logic, also generate goldens by
   running the real PHP implementation from `.ref/` with `php` (in the dev
   shell). Generator scripts live in `tools/oracle/<package>/` and write into
   the package's `testdata/`. Goldens are committed, so `go test` needs
   neither php nor the network. A golden over 1 MB is committed gzipped
   (`*.json.gz`, written by the oracle script, read with compress/gzip).
   Oracles compare the frozen surface exactly. The errors oracle
   (`tools/oracle/errors`, `internal/command/errorstest`) compares exit
   codes and stdout exactly and checks that stderr reports the messages of
   Composer's exceptions (or a scenario's listed lines), whitespace-
   insensitively, not how they are rendered.
3. **End to end.** `cmd/maestro` tests (`MAESTRO_E2E=1`) run real Composer
   2.10.3 (a pinned phar, downloaded into the test cache with a checksum
   check, never shipped) and maestro on the same projects and compare
   exit codes, frozen output, lock files and vendor trees. They run on
   Linux and on Windows (`.github/workflows/e2e.yml`, weekly and on
   demand), where both tools run natively: junctions are compared by
   target, modes are what the read-only attribute gives, and `"\r\n"`
   endings are compared as they are. The Windows run leaves the xz dist
   out of the dists scenario: Composer extracts it with whatever `tar`
   PATH finds, there Git for Windows' GNU tar, which takes the drive
   letter of an absolute path for a remote host.

Tests must pass on every CI machine, not just the one that recorded them:

- Compare realpath()ed paths with `testutil.RealTempDir` (on macOS /var is
  /private/var).
- Goldens name no machine: an oracle writes placeholders for the paths of
  the machine it runs on (`/home/user`, `/home/oracle`; see
  `tools/oracle/php/preg_anonymize.php`) and leaves PHP's TypeError call
  sites out. `TestTestdataNamesNoMachine` (`internal/testutil`) fails on a
  home directory in any testdata.
- Where Composer follows readdir order, maestro does too
  (`util.ReadDirOrder`). Tests compare such results without depending on
  order, or build fixtures whose order is fixed. The live PHP oracles stay
  exact.
- Differential archive tests need the reference extractors: Info-ZIP UnZip
  6.00 (`archivetest.NeedInfoZip`) and GNU tar (`archivetest.NeedGNUTar`).
  They skip with a message when these are missing.
- Tests that need a progress bar clear `CI` (`t.Setenv("CI", "")`), because
  GitHub Actions sets it.
- Process-wide caches (the git/svn versions in `internal/util/vcs`) are
  pinned with `vcs.SetVersion`/`vcs.SetSvnVersion` and reset in
  `t.Cleanup`. Tests that do this must not run in parallel.

Opt-in test switches:

| Variable | Turns on |
| --- | --- |
| `MAESTRO_PHP_TESTS=1` | tests that run `php` (scripts, plugins, platform detection) |
| `MAESTRO_E2E=1` | the end-to-end comparison with Composer (network, slow) |
| `MAESTRO_ORACLE_LIVE=1` | classmap oracles run against the PHP implementation live instead of the goldens |
| `MAESTRO_TEST_DISTS=<dir>` | the store's differential test over real dists fetched by `go run ./tools/fetchdists` |
| `MAESTRO_TEST_UNZIP`, `MAESTRO_TEST_TAR` | which reference `unzip`/`tar` the archive tests compare with |

Tests, vet and lint run in the dev container (`compose.yaml`, one profile
per task), so a run depends on nothing of the machine it runs on (user,
home directory, umask, locale, php, unzip, tar, caches) and leaves nothing
on it. The justfile drives it: `just test` (with `MAESTRO_PHP_TESTS=1`;
arguments go to `go test`, e.g. `just test ./internal/config -run X`),
`just vet`, `just lint`, `just deadcode`, `just build`, `just check`
(vet, lint, deadcode, test, build), `just test-race`, `just e2e` and `just shell`. Oracles that run
Composer's PHP from `.ref/` and other tools run in the devenv shell
(`devenv shell -- bash -c '...'` from the repo root). A port is done when `go vet`, `golangci-lint run` and `go test -race` with
`MAESTRO_PHP_TESTS=1` pass for its packages (`CGO_ENABLED=1` for `-race`).
CI (`.github/workflows/ci.yml`) runs the tests on Linux and macOS, and on
Windows in shards.

## Tools

| Tool | Does |
| --- | --- |
| `tools/oracle/<package>` | generates the goldens of differential tests from `.ref/` |
| `tools/shimvendor` | vendors the PHP libraries Composer's phar ships into the shim (`internal/plugin/php/lib`) |
| `tools/shimgen` | generates the shim's API-parity stubs and golden from Composer's `src/` |
| `tools/fetchdists` | downloads real dist archives for the store's differential test |

## Tooling hazard: `\u` escapes

Writing files through the editor tools can turn `\uXXXX` escape sequences
into the literal characters they name. In Go source, write such strings
with explicit byte escapes or build them in code, and check any file that
must contain a literal `\u` with `grep -n '\\u'` after writing it.

## Regular expressions

Use `internal/php`'s `Compile`/`MustCompile` and the `Preg*` functions with
Composer's patterns pasted verbatim. It is a PCRE2-compatible engine
(recursion, subroutines and all); don't use `regexp` or regexp2 for ported
patterns. regexp2 is a dependency only for a test that cross-checks
semver's hand-written matchers (`internal/semver/regex_test.go`).

A match can fail (backtrack or recursion limit, bad UTF-8 under `/u`), and
many simple-looking patterns do on subjects well under 1 MB:
`(.+?)\s*$` fails at about 10 KB of whitespace, and globs turned into
`.*.*.*c` fail at about 150 bytes. What happens next depends on how the
PHP calls it:

- `Composer\Pcre\Preg::*` throws PcreException (a RuntimeException).
  Return the `*php.PcreError` up the stack, as far as PHP lets it travel
  (check for `catch (\Exception)`/`catch (\RuntimeException)` on the way).
- Bare `preg_*` returns false/null (preg_grep: the entries collected so
  far). Do what the PHP caller does with that: `!preg_match` is true, and
  preg_replace's null concatenates as "".
- Ignore the error (`_`) only where the pattern provably cannot fail:
  bounded work per start position (a single character class, an anchored
  fixed-length prefix, possessive quantifiers before a disjoint literal)
  or a subject bounded far below the limit. Say why in a comment at the
  site. Helpers that panic or swallow errors must not wrap patterns that
  can fail.
- `util.SanitizeURL` (only used to build messages) returns "" where Preg
  would throw. Anything that derives more than a message from a URL (cache
  directory names) uses `util.SanitizeURLChecked`.

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
