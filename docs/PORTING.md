# Porting Composer to Go

maestro is a port of Composer 2.10.3 to Go and a drop-in replacement for it:
anything that drives Composer (people, CI, scripts, plugins, other tools)
must work unchanged against maestro. It never runs or embeds composer.phar.
`php` is only used where Composer itself executes PHP code on the project's
behalf: scripts, plugins (through maestro's own PHP shim), platform
detection and `exec`.

Compatibility is defined by a contract, not by byte-for-byte output
everywhere: what tools and people depend on is frozen and must match
Composer exactly; how errors and diagnostics are presented is maestro's
own. "The contract" below says which is which.

## The contract

### Frozen: identical to Composer, byte for byte

- **CLI surface.** Commands and their aliases, arguments, options, short
  options, defaults, validation (what is accepted and what is refused),
  `help` contents, `list`'s commands, their order and descriptions, every
  `list --format` and `list --raw` output, and option
  suggestions/completion. The banner heading `list`'s text output is
  maestro's (deviation 8).
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
is `internal/ui`'s: a labelled headline with the message,
then its causes (previous errors), what the input may have meant ("Did
you mean this?"), hints, the usage of a command given wrong input
(wrapped to the terminal when decorated) and, at `-v`, debugging details
(Go error types, a plugin exception's PHP trace, the exit code). The
alternatives come from the error (`console.Alternativer`): a command's
or namespace's (Symfony's), a mistyped long option's and a package's
that `require` could not find (Composer's), and the installed packages a
name given to `show` may have meant (maestro's, by the same Levenshtein
rule, `console.FindAlternatives`). The error's message keeps Composer's
"Did you mean" block for plugins; `ui.DidYouMean` is the one definition
of that block, so the rendering takes exactly it out of the message.
Other free output keeps Composer's
text without having to match it exactly, and nothing is built only to
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
- One theme. `internal/ui`'s palette is a set of typed roles
  (`ui.Role`: success, notice, warning, danger, highlight, accent, muted,
  emphasis, package, version, link, ...) on the 16 ANSI colours, with
  backgrounds only where Composer has them (`<question>`, `<error>`,
  `<warning>`, init's banner).
  Everything styled takes its look from a role: diagnostics
  (`Diagnostic.Lines`), Composer's formatter tags through one table
  (`ui.ComposerTags`: `info`, `comment`, `question`, `error`, `warning`,
  `highlight`, registered by `Factory::createAdditionalStyles`), and
  maestro's own text through each role's tag (`ui.RoleX.Wrap(text)`,
  `<maestro-x>`, which every formatter knows) or its inline style
  (`Inline()`, to combine with `href=`). Frozen output carries Composer's
  tags, so each tag's role is the tag's style in Composer, byte for byte
  with `--ansi` (`<error>` white on red, `<warning>` black on yellow,
  `<info>` green, ...; a test compares them with Symfony's styles), and
  every role's escape sequences are built as Symfony builds a style's: a
  set and an unset code per attribute (`\e[32m...\e[39m`), never a full
  reset, so text after a role keeps the styles around it. Diagnostics
  use the danger and warning roles; links stay OSC 8 hyperlinks. No package but
  `internal/ui` and the formatter engine in `internal/console` writes a
  colour or escape sequence of its own, which a test enforces.
- Progress and prompts on a terminal. When the error output is
  decorated and a terminal (`console.IsStyledTerminal`), the progress
  bar is drawn as a solid line in the theme (`ui.ProgressBar`), and a
  question read from an interactive terminal is marked with an accent
  `?` before its text (`ui.Prompt`). Both stay line-based, in
  `internal/console`'s progress bar and question helper: Composer's
  questions (autocompletion, hidden answers, attempts) and its one-line
  redraw already work on every console, so no full-screen program takes
  over the terminal. Glyphs (`ui.Glyph`) fall back to ASCII on a Windows
  console without the UTF-8 code page or a non-UTF-8 locale. The texts,
  defaults, answers and streams are Composer's.
- Reports for people, colour only. Every `--format` value a command
  offers is classified once, with its surface (`ui.Formats` of
  `ui.Format{Name, Surface}`; `ui.Frozen` or `ui.Free`), and the option's
  suggestions are built from that table, so no format exists
  unclassified; a test checks every such option against the contract
  (`text` and `table` free, `json`, `plain`, `summary` and any other
  frozen). Free text output is styled through `Surface.Style(role,
  text)`, which leaves frozen output alone: the same characters in the
  same columns as undecorated, with colour added (licences by OSI
  approval, fund's vendors and links, check-platform-reqs' providers,
  suggests' packages and reasons, show's descriptions and tree
  constraints). `suggests --list` stays Composer's, as do `depends`,
  `prohibits` and every frozen format. A test strips the escape
  sequences of each command's decorated text output and compares it
  with the undecorated output. The one free format whose shape changes is
  audit's `table`: decorated, its advisories are a report
  (`ui.AdvisoryReport`) grouped by package, the most severe first, with
  a severity badge, the title wrapped to the terminal and every other
  field (IDs and URL as links, an ignored advisory's reason) beneath it;
  undecorated it is Composer's tables, byte for byte.
- diagnose. Its report is free (only its exit code and checks are
  frozen). Undecorated it is Composer's lines plus `Maestro version: X`
  after `Composer version`, so a report says which tool ran; decorated
  it is a check list (`ui.CheckList`): the facts (versions, binaries) in
  a label column, then one line per check with its outcome's glyph
  (`✔`, `!`, `✖`; `OK`, `WARN`, `FAIL` without Unicode) and what a
  passed check found, a warning's or failure's messages beneath it as a
  diagnostic. Each check's line is written once it has a result.
- The install/update summary. Each operation line's bullet is a
  mark (`ui.Mark`, through `operation.Item`/`InstallItem`/...): "-"
  undecorated, decorated a glyph in a role (`+` install, `↑` upgrade,
  `↓` downgrade, `−` removal). "Lock file operations"/"Package
  operations" are a tally (`ui.Tally`) whose zero counts are muted; the
  "Nothing to ..." lines, "Generating autoload files", the funding and
  suggestion pointers are muted. The text, lines, streams and verbosity
  are Composer's. maestro's tags (`<maestro-...>`) never reach a PHP IO
  or output a plugin hands maestro: `ui.Foreign` turns them into
  Composer's inline styles first.

## Reference sources

`.ref/` (gitignored) holds the exact sources being ported, with their tests;
`ref-sync` (in the devenv shell, also `just ref`) recreates it:

| Path | What |
| --- | --- |
| `.ref/composer` | composer/composer at tag 2.10.3 (`internal/upstream`), with `vendor/` installed (`--no-dev`) |
| `.ref/semver` | composer/semver |
| `.ref/class-map-generator` | composer/class-map-generator |
| `.ref/spdx-licenses` | composer/spdx-licenses |
| `.ref/metadata-minifier` | composer/metadata-minifier |
| `.ref/jsonlint` | seld/jsonlint |

The libraries are checked out at the versions Composer's composer.lock
pins, and so are the others Composer ships, ported from
`.ref/composer/vendor/`: symfony/console (the subset in
`internal/console`), justinrainbow/json-schema (the parts in
`internal/json/jsonschema`) and composer/ca-bundle (`internal/util/http`).

The PHP source is the specification of behaviour. For the frozen surface,
when the Go and PHP disagree, the Go is wrong: port logic faithfully,
including its quirks, ordering, messages and edge cases, and do not
"improve" it unless it is one of the deliberate deviations below. For the
free surface, the PHP decides what information is reported and when, not
its exact presentation.

## Following upstream

maestro tracks the latest stable Composer. Its release is named in one
place, `internal/upstream`: the version, the release date, the plugin and
runtime API versions and the official phar's sha256. The Go code
(Composer::VERSION, the platform packages, the user agent, the banner),
the e2e tests and `ref-sync` (which checks out the libraries at the
versions that release's composer.lock pins) read it from there; scripts
through `tools/upstream/composer-version.sh`. Tests check the shim's
copies of the constants against it.

`tools/upstream/bump.sh <version>` (`just bump-composer <version>`) moves
the pin: it downloads the release's phar, checks its sha256 against
getcomposer.org's, reads the constants out of it and rewrites
`internal/upstream`, the shim's constants and the docs that name the
release.

`.github/workflows/upstream-composer.yml` checks Composer's releases
daily. It opens an `upstream` issue for each newer one (pre-releases
included, titled as such) with its release notes, the compare link, a
diffstat of the paths that matter, the bundled libraries its
composer.lock changes and what is left to port, and for the newest
stable one opens the bump PR (`tools/upstream/bump-pr.sh`), linked from
the issue. The issue is the trigger to port the release.

## Deliberate deviations

These change frozen behaviour on purpose; nothing else may.

1. **Package store, pnpm style.** Dist archives are extracted once into a
   per-file content-addressed store (`internal/store`) shared by every project
   and git worktree on the machine. Files are imported into `vendor/` as
   pnpm's `auto` does: reflink clone where the filesystem can, else
   hardlink, else copy (`MAESTRO_PACKAGE_IMPORT_METHOD=clone|hardlink|copy`
   forces one method). On Linux, `auto` hard-links the files of a release
   into the project whose install extracted it, which writes each file
   once. Composer plugins (`composer-plugin`,
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
   The store lives in maestro's own cache directory ("maestro's own
   caches"). Releases not used for `cache-files-ttl` are pruned, at most
   once a day, by a command that installed packages, once it is done.
2. **No external extractors.** zip/tar/gz/bz2/xz extraction is native Go
   (`internal/archive`), reproducing exactly what Composer's preferred path
   (system `unzip -qq`, `tar`) produces on Unix. In macOS's C locale, unzip
   writes U+0080 to U+00FF as Latin-1 bytes APFS refuses and Composer falls
   back to ZipArchive; maestro refuses such names there rather than
   escaping them as glibc's unzip does. On Windows, where Composer extracts
   zips with ZipArchive or 7-Zip, maestro extracts as unzip would on Unix.
3. **Speed.** Parallelism and caching wherever results stay identical
   (see "maestro's own caches" for what is kept between runs).
   One request goes elsewhere: a zip dist at Packagist's
   `https://api.github.com/repos/{owner}/{repo}/zipball/{ref}`, which
   GitHub only redirects to
   `https://codeload.github.com/{owner}/{repo}/legacy.zip/{ref}`, is
   requested there (the same bytes; the files cache keys on the original
   URL), from the original URL when that fails in any way, and never when
   a `PRE_FILE_DOWNLOAD` listener could change the request
   (`internal/downloader/codeload.go`).
4. **self-update** updates maestro from its GitHub releases.
5. **Plugins** run in maestro's own PHP shim (`internal/plugin/php`), which
   reimplements Composer's public PHP plugin API, not Composer's code.
6. **`--version`** prints Composer's exact lines, then one extra stderr
   line, `Maestro version X`, so people can tell which tool they run;
   anything parsing stdout or the first line sees Composer's output.
7. **No stray empty `vendor/bin`.** Composer's
   `BinaryInstaller::removeBinaries` creates the bin dir even when the
   removed package has no binaries, so an emptied bin dir survives or not
   depending on the order removals finish. maestro only touches the bin dir
   for packages with binaries: an empty bin dir is never created by a
   removal, and the last binary removed takes it away.
8. **The `list` banner** (also a bare `maestro`), like deviation 6, says
   which tool runs: maestro's logo and `Maestro version X (Composer
   2.10.3 compatible)` replace Composer's logo and long version, decorated
   or not, piped or not. Only the banner changes: the usage, options and
   commands below it, `list --raw` and `list --format=json|xml|md` stay
   Composer's (`TestHelp_List` and the e2e tests compare all of it but the
   banner).

### maestro's own caches

Besides Composer's caches, which it keeps exactly as Composer does,
maestro keeps caches of its own in its cache directory (`MAESTRO_CACHE_DIR`,
else `$XDG_CACHE_HOME/maestro`, else the platform cache directory;
`internal/cache.Dir`). `internal/cache` (`own.go`) names every one of them,
and a test fails when this table and it disagree.

Every entry is used only when it provably matches what it was made from:
the same bytes (decoded JSON), the same content hash (parse results), or
the same identity of every file it depends on (device, inode, mode, size,
modification and change times; an identity too close to the time the
entry was written is not trusted, as git treats racily clean index
entries). Parse results and schema validations are used only by the
binary that made them. Anything else is computed again and the entry
overwritten, so a stale entry costs time and never changes frozen output.

`clear-cache` clears each together with a Composer cache directory,
silently (its output stays Composer's): what follows `cache-dir` goes
with a full clear of it, so a full `clear-cache` leaves none of maestro's
derived data behind; `--gc` ages only the store and the decoded metadata,
as each other cache bounds its own size.

| Path | Holds | Used while | `clear-cache` |
| --- | --- | --- | --- |
| `store/v1` | the package store (deviation 1): the extracted files of dists and source clones, and per release the class map scan results of its files (`derived/`) | content-addressed; every import checks a file's size, mode and hash-derived modification time and heals it | with `cache-files-dir` (emptied); `--gc` and, once a day, an install prune releases unused for `cache-files-ttl` |
| `p2` | Packagist p2 metadata files from Composer's repo cache, decoded | the JSON is byte-identical: the cached file's identity is the one a read that could trust it saw (a timestamp tick old), or else a copy of the JSON compares equal | with `cache-repo-dir`; `--gc` removes what was not written for `cache-ttl` |
| `decoded` | large local JSON files read on most runs (`vendor/composer/installed.json`), decoded; at most 64 | the JSON is byte-identical | with `cache-dir` |
| `classmap/v1.bin` | the classes found in each file content seen, by SHA-256 and parser settings, and each file's content hash by its identity | the same maestro binary; the content hash, or the file's identity | with `cache-dir` |
| `classmap/records` | a project's class map with the identity of every file and directory its scans depended on; at most 64 | the same scans, and every identity unchanged | with `cache-dir` |
| `platform` | php's probed platform (Linux), keyed on the php binary, its ini files, the environment that can change what it reports and the files it loaded; at most 64 | all of those unchanged, for 24 hours at most | with `cache-dir` |
| `git-version` | `git --version` (Linux, real process executor only, never at `-vvv`), keyed on the git binary's identity | the binary unchanged, for 24 hours at most | with `cache-dir` |
| `schema/validated` | the SHA-256 of the last 64 documents that validated against Composer's schemas without a finding | the same maestro binary and document | with `cache-dir` |
| `cacert` | the embedded CA bundle written out as a file, for what needs a path to one (`CaBundle::getBundledCaBundlePath`) | named by its hash | with `cache-dir` |

### Go runtime settings

When neither `GOGC` nor `GOMEMLIMIT` is set, maestro starts with the
garbage collector off and a 64 MiB memory limit (`cmd/maestro/gc.go`):
most commands finish without collecting at all. A run that reaches the
limit collects once, and from then on runs with Go's defaults (`GOGC=100`,
no limit). An explicit `GOGC` or `GOMEMLIMIT` works as for any Go program
and turns this off.

## Layout

Go packages mirror Composer namespaces. `package` is reserved in Go, so
`Composer\Package` is `internal/pkg`.

| Go package | Ports |
| --- | --- |
| `internal/php` | PHP runtime semantics the port relies on: arrays (ordered maps with PHP key coercion), `json_decode`/`json_encode` (all flags Composer uses), `var_export`, comparisons and sorts (PHP 8, stable), `version_compare`, `strnatcmp`, `+`, `sprintf`, string helpers (`strip_tags`, `levenshtein`, `stripcslashes`, `escapeshellarg`, ...), the path functions (`getcwd`, `realpath`, `dirname`, `basename`, `pathinfo`; the lint forbids Go's `os.Getwd`, `filepath.Abs` and `filepath.EvalSymlinks` elsewhere), and its own PCRE2 10.48-compatible regex engine (internal/php/doc.go says why) |
| `internal/phperr` | an error's PHP previous exception (`Chained`, `PreviousOf`) and the root Composer's sources are reported under (see "Errors") |
| `internal/upstream` | nothing: the Composer release maestro ports, named once ("Following upstream") |
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
| `internal/switches` | nothing: the environment variables of the opt-in tests and the profiling build ("Test switches", "Profiling") |
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
- Behaviour never depends on the text of a message maestro words itself
  (free output, reworded at will): what failed travels as data. A
  transport failure carries curl's errno, text and phase
  (`util.TransportError.Curl`: `IsTimeout`, `IsTransferTimeout`,
  `IsResolveFailure`), which hints and retries read where Composer reads
  curl's or PHP's message. Matching text stays only where the text comes
  from outside maestro (a server, git, PHP), with a comment saying so.
- Errors carry no throw site or PHP call stack: rendering is maestro's
  own, so Composer's file and line of a `new` expression, the
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
   insensitively, not how they are rendered. A scenario may also freeze
   files the run leaves (composer.json restored after a failure, a lock
   that must not be written), share a project from `_projects/`, make
   files read-only and answer questions on stdin; see
   `tools/oracle/errors/errors.sh`.
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

Every command maestro registers (aliases and hidden commands included;
plugin commands are out of scope) has at least one **positive** test (a
representative input succeeds; the test asserts the frozen surface) and
one **negative** test (bad input or an unmet precondition; the test asserts
the exit code, the stream and the essential message). The guard
`TestEveryCommandHasPositiveAndNegativeTests` enforces it from the table
in `internal/command/coverage_test.go`, one entry per command:

- The command is named by its constructor (`cover(command.NewShowCommand,
  ...)`); its aliases and every registered name are read from the
  Application, never listed.
- Tests are `Positive(...)` or `Negative(...)` of `Go(TestX)` (the test
  function itself, so a rename breaks the build) or `E2E(scenario, args...)`
  (checked against the literal steps of `cmd/maestro`'s scenarios, and
  that the step runs the command).
- Errors-oracle scenarios (`testdata/errors`) are not listed: each counts
  for the command its `args` run, positive when Composer exited 0 and
  negative otherwise.
- A command lacking a kind carries `Pending: <issue>`, its "Tests:
  <command>" issue (label `testing`). The guard fails on a registered
  command without an entry, an entry for no command, a command lacking a
  kind and not pending, and a pending command that has both kinds: remove
  `Pending` in the change that completes the coverage.

Tests must pass on every CI machine, not just the one that recorded them:

- Compare realpath()ed paths with `testutil.RealTempDir` (on macOS /var is
  /private/var).
- Goldens name no machine: an oracle writes placeholders for the paths of
  the machine it runs on (`/home/user`, `/home/oracle`; see
  `tools/oracle/php/preg_anonymize.php`) and leaves PHP's TypeError call
  sites out. The errors oracle normalises Composer's runs and maestro's
  with one definition, `testutil.NormalizeOracle` (errors.sh runs it as
  `tools/oracle/errors/normalize`), which also replaces the CA bundle,
  php.ini files, archive tools and php details the machine has.
  `TestTestdataNamesNoMachine` (`internal/testutil`) fails on a home
  directory or an installed package's path (`/nix/store/...`, Homebrew's)
  in any testdata.
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

Tests, vet and lint run in the dev container (`compose.yaml`, one profile
per task), so a run depends on nothing of the machine it runs on (user,
home directory, umask, locale, php, unzip, tar, caches) and leaves nothing
on it. The justfile drives it: `just test` (with `MAESTRO_PHP_TESTS=1`;
arguments go to `go test`, e.g. `just test ./internal/config -run X`),
`just vet`, `just lint`, `just deadcode`, `just tidy-check`, `just build`,
`just check` (vet, lint, deadcode, tidy-check, test, build),
`just test-race`, `just e2e` and `just shell`. Oracles that run Composer's
PHP from `.ref/` and other tools run in the devenv shell
(`devenv shell -- bash -c '...'` from the repo root). A port is done when
`go vet`, `golangci-lint run` and `go test -race` with
`MAESTRO_PHP_TESTS=1` pass for its packages (`CGO_ENABLED=1` for `-race`).
CI (`.github/workflows/ci.yml`) runs the tests on Linux and macOS, and on
Windows in shards.

### Test switches

Environment variables a developer or CI sets for tests that are off by
default, and for the inputs they take. `internal/switches` declares them
all; its tests fail when this section and the code disagree. A switch
that turns something on takes `1`; any other value, or none, leaves it
off.

| Variable | Does |
| --- | --- |
| `MAESTRO_PHP_TESTS=1` | runs the tests that run `php`: scripts, plugins, platform detection (the dev container and CI set it) |
| `MAESTRO_E2E=1` | runs the end-to-end comparison with Composer (`cmd/maestro`) and the functional fixtures (`cmd/maestro`, `internal/command`); they need php, git, unzip and the network, and are slow |
| `MAESTRO_NETWORK_TESTS=1` | runs the tests that install real packages from GitHub (`internal/command`'s status test) |
| `MAESTRO_ORACLE_LIVE=1` | runs the classmap oracles against the PHP implementation live instead of the goldens |
| `MAESTRO_PERF_BUDGETS=1` | fails the plugin runtime's timing tests when a timing misses its budget (docs/PLUGINS.md §5.16); without it they only log it, as wall time depends on machine load. Set it on a quiet machine |

With `MAESTRO_E2E=1`:

| Variable | Does |
| --- | --- |
| `MAESTRO_E2E_BIN=<file>` | the maestro binary to compare; default: one built from the tree |
| `MAESTRO_E2E_KEEP=<dir>` | runs the scenarios in `<dir>` and keeps each tool's project, caches and output there; default: a temporary directory, removed as each scenario ends |
| `MAESTRO_E2E_WARM=0` | runs only the cold phase (empty caches); default: cold, then warm (a fresh project on the caches and store the cold phase left) |
| `MAESTRO_E2E_PLUGINS=<a,b,...>` | runs only the named plugin scenarios (`TestE2EPlugins`); the others skip |
| `MAESTRO_E2E_REPORT=<file>` | writes `TestE2E`'s wall-time table (docs/BENCHMARKS.md) to `<file>` |
| `MAESTRO_E2E_PRIVATE_APP=<dir>` | the checkout of a private Laravel application, with its composer.lock, for the `private-app` scenario and docs/BENCHMARKS.md's private-app rows; without it that scenario skips |

Test inputs:

| Variable | Does |
| --- | --- |
| `MAESTRO_ORACLE_SEED=<n>`, `MAESTRO_ORACLE_COUNT=<n>` | the seed (default 42) and number of cases (default 50000) of the live classmap random oracle |
| `MAESTRO_ORACLE_VERSIONS=<dir>` | a set `tools/oracle/classmap/versions.sh` wrote, for the classmap versions oracle; default: the committed `testdata/oracle` |
| `MAESTRO_TEST_DISTS=<dir>` | the real dists `go run ./tools/fetchdists` fetched, for the store's differential test; default: `maestro-test-dists` in the user cache directory, and the test skips when it is empty |
| `MAESTRO_TEST_UNZIP=<file>`, `MAESTRO_TEST_TAR=<file>` | the reference `unzip` and `tar` the archive tests compare with; default: `unzip`, and `tar` then `gtar`, from PATH (macOS CI points them at Homebrew's Info-ZIP and GNU tar) |
| `MAESTRO_P2_DIRS=<path list>` | more directories of cached Packagist `provider-*.json` files the decoded metadata cache's test checks, besides Composer's cache directories |

### Profiling

A binary built with `go build -tags maestro_profile ./cmd/maestro` (never a
release) writes profiles of its run to the files these name;
docs/BENCHMARKS.md shows what they found.

| Variable | Writes |
| --- | --- |
| `MAESTRO_CPUPROFILE=<file>` | a CPU profile (`go tool pprof`) |
| `MAESTRO_MEMPROFILE=<file>` | an allocation profile, sampled every 4 KiB, when the run ends (`go tool pprof`) |
| `MAESTRO_TRACE=<file>` | an execution trace (`go tool trace`) |

## Tools

| Tool | Does |
| --- | --- |
| `tools/oracle/<package>` | generates the goldens of differential tests from `.ref/` |
| `tools/shimvendor` | vendors the PHP libraries Composer's phar ships into the shim (`internal/plugin/php/lib`) |
| `tools/shimgen` | generates the shim's API-parity stubs and golden from Composer's `src/` |
| `tools/fetchdists` | downloads real dist archives for the store's differential test |
| `tools/deadcode` | fails on functions nothing reaches (`just deadcode`, CI) |
| `tools/tidycheck` | fails when `go mod tidy` would change go.mod or go.sum (`just tidy-check`, CI) |
| `tools/upstream` | reads and bumps the Composer release maestro ports; opens its issues and bump PRs ("Following upstream") |

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

## Working alongside other changes

Several changes are often in flight at once, each in its own worktree.
Keep a change to the packages its task needs, build and test those
(`go test ./internal/semver/...`) while working, and rebase on main
before landing it. If a change needs something another package does not
provide yet, say so rather than reaching into that package.

Dependencies:

- Add one with `go get module@version`.
- Before pushing, `go mod tidy` must leave go.mod and go.sum unchanged.
  `tools/tidycheck/check.sh` checks that without rewriting anything
  (`go mod tidy -diff`); CI's lint job, `just tidy-check` and `just check`
  all run it. When it reports a diff, run `go mod tidy` once and commit the
  result.
- Run `go mod tidy` on an up-to-date main, not in a tree other changes
  share: there several of them edit go.mod at once, and tidying drops the
  requirements of code that is not in the tree yet.

## Plugin requirements on every package

Plugins run against maestro's Go state through the PHP shim specified in
docs/PLUGINS.md. Its section 7 lists what the other packages must provide
(change counters on packages, the local repository and Config; installer
calls routed through an overridable interface; no package-level mutable
state, so Installer, Factory and Application are re-entrant; ...). Read it
before porting any package it names.
