# Hand-off notes between ports

Requirements finished ports place on later ones. Read the section for any
package you port.

## internal/downloader, internal/installer (from internal/store, internal/archive)
- Import method: `store.ParseMethod(os.Getenv(store.MethodEnv))`; the store never reads the environment itself.
- Verify the dist shasum as Composer's FileDownloader does; the store does not.
- Reproduce PharData's extension check: Composer fails tar dists whose URL path has no extension; `archive` doesn't check it.
- Create the parent of `dst` before `store.Materialize`.
- BinaryInstaller's chmod must go through `store.Chmod`, so a hardlinked file is unshared before its mode changes.
- A warm worktree must use `store.Install(dist, "", dst)` before any download: `ErrNotFound`/`*MissingError` mean "fetch the archive".

## internal/autoload (from internal/classmap)
- Set `classmap.Parser.ShortOpenTag` from the user's real php configuration (php.ini usually turns it Off; the Go default is PHP's built-in On).
- Errors from the exclusion `Matcher` come back unwrapped; treat them like PHP's PcreException (a RuntimeException).

## internal/platform (from internal/util)
- `util.IniGetAll`/`IniGetMessage` take a callback supplying the loaded ini files; platform detection must provide it.

## Cleanup (DRY): done; remaining internal/util follow-ups
semver, console, classmap, io and util's pcre.go now use internal/php (Sprintf, StripTags, Levenshtein, Stripcslashes, Escapeshellarg, Basename, Add, StringsLooseEqual, SortSlice, Regexp). Left in internal/util because that package was being extended concurrently; switch them when convenient:
- `phpNumeric` (php.go) duplicates `php.IsNumeric`/`php.ToFloat`; `phpTrimChars` duplicates `php.TrimChars`; `varExportString` duplicates `php.VarExport` for strings; `lowerASCII`/`hasPrefixFold`/`equalFoldASCII` overlap `php.Strtolower`/`php.Strcasecmp`.
- `phpBasename(path, false)` equals `php.Basename(path, "")` (keep the Windows variant).
- `escapeShellArg` (executablefinder.go) is only a true `escapeshellarg()` in ExecutableFinder (`command -v -- ...`); use `php.Escapeshellarg` there (it drops invalid UTF-8 and rejects NUL like PHP). ProcessExecutor/Process::escapeArgument are `str_replace`-based in PHP, not escapeshellarg, so they keep their own quoting.
- `passwordArg` (processexecutor.go) and console's private patterns (table, input_argv, progress_bar, formatter, html_formatter, question) still use Go's `regexp`; PORTING.md asks for `php.MustCompile` with the verbatim pattern. `NewConfirmationQuestion`/`NewStrictConfirmationQuestion` take `*regexp.Regexp`, so changing those is an API change.

## internal/composer, internal/util/http, internal/io (from internal/config, internal/json)
- `config.Config.Get` is `Get(key, flags) (any, error)` (PLUGINS.md §7). `io.Config` and `http.Config` want `Get(key) any` and return types tied to their own `ConfigSource`; internal/composer adapts `*config.Config` to both (the methods of `config.ConfigSource` are a superset of `http.ConfigSource`).
- `config.Config.Merge` returns the TypeError PHP throws on wrongly typed values; adapters for `io.Config` may drop it.
- `json.File.Read` takes an `HTTPDownloader{Get(url) (string, error)}` for http(s) paths; wrap `util/http.HttpDownloader`.
- Composer's ErrorHandler makes PHP warnings ErrorExceptions; config, json and the manipulator return `*util.ErrorException` at those sites. Later ports should do the same.

## Cleanup (DRY), from internal/config, internal/json, internal/policy
- internal/config/filterurl.go ports filter_var(FILTER_VALIDATE_URL) (oracle-tested) and duplicates util's private validateIPv4/IPv6; move it into internal/util (or php) and use it from internal/json/jsonschema's URI check, which approximates it.
- PLATFORM_PACKAGE_REGEX is copied in internal/json/manipulator.go; use internal/repository's once it exists.
- BasePackage::packageNameToRegexp is copied in internal/policy; use internal/pkg's.
- `.ref/jsonlint` (seld/jsonlint 1.12.1) should be added to `ref-sync` in devenv.nix.

## Repo hygiene (when no agent is editing the packages involved)
- Gzip every existing golden over 1 MB (internal/{classmap,util,json,json/jsonlint,json/jsonschema,semver,php,config,...}/testdata), updating oracle scripts to write .json.gz and tests to read it.

## internal/config (from internal/util/http)
- `*config.Config` must satisfy `http.Config`: `Get(key string) any` (same as `io.Config`), `ProhibitURLByConfig(url string, io io.IO, repoOptions *php.Array) error` (returns `*util.TransportError`), `ConfigSource()`, `AuthConfigSource()`, `LocalAuthConfigSource()` (nil for null), each returning a value with `Name() string`, `AddConfigSetting(name string, value any) error`, `RemoveConfigSetting(name string) error`.
- Lists (`github-domains`, `gitlab-domains`, ...) come back as `*php.Array`; `store-auths` as `true`/`false`/`"prompt"`.

## internal/composer, internal/command, internal/platform (from internal/util/http)
- Build downloaders with `http.CreateHttpDownloader(io, config, options, rt)` (Factory::createHttpDownloader); `rt` is an `http.Runtime` giving the php version, platform php version (PlatformRepository::getPlatformPhpVersion), Composer's running command/operation statics and maestro's version for the User-Agent. `http.StaticRuntime` is a ready implementation.
- `Application::doRun` hints: `http.GetExceptionHints(err)`; exit code from `*util.TransportError`'s `Code`.
- Loop: `http.NewLoop(downloader, processExecutor)`; `Loop.Wait` takes any `*util.Promise` (as `http.Waitable`); `SyncHelper` is `http.DownloadAndInstallPackageSync` (generic over the package type) and `Loop.Await`.

## internal/repository, internal/downloader (from internal/util/http)
- Composer\Cache is `cache.New(io, dir, allowlist, fs, readOnly)`; `Cache::$cacheCollected` is process-wide like PHP's static.
- `prevent_url_access_callable`/`prevent_ip_access_callable` options hold `http.RegisterCallable(fn)` handles (a `*php.Array` cannot hold funcs).
- Tests mock HttpDownloader with `internal/util/http/httpmock` (HttpDownloaderMock); code taking a downloader for GET only should accept `http.Getter`.
- `HttpDownloader::outputWarnings` is `http.OutputWarnings(io, url, data) (bool, error)`.
- Response::decodeJson errors are `*jsonlint.ParsingError` naming only the URL.

## Cleanup (DRY) once internal/php is final (from internal/util/http)
- internal/util/http/phpfuncs.go holds rawurlencode, rawurldecode, urlencode, http_build_query and a date() subset; move them into internal/php.
- `util.IO` (int verbosities) and `io.IO` (io.Verbosity) differ; internal/util/http adapts with `utilIO` (deps.go) to build a ProcessExecutor. Unify them.

## Users of internal/pkg (from internal/pkg)
- `pkg.PackageInterface` is sealed (unexported `base()`): only the six pkg types implement it. `instanceof AliasPackage` is a type assertion to `pkg.Alias`, `instanceof Package`/`CompletePackage` are `pkg.AsPackage`/`pkg.AsCompletePackage`; `clone` is `pkg.Clone`.
- PHP `?string` is `pkg.NullString`; free-form arrays are `*php.Array`, shared not copied (Clone before modifying). Getters never return a nil array except for PHP null (source/dist mirrors, php-ext).
- Link maps are `pkg.Links` (immutable, key order and non-target keys kept); build them with `pkg.LinksBuilder` (`Set` = `$links[$k] = $l`). `Link.PrettyConstraint()` returns an error where PHP throws.
- Share one `*pkg.VersionParser` between loaders: Composer's static parsed-constraint cache is per parser here.
- `pkg.IsPlatformPackage` is PlatformRepository::isPlatformPackage; repository code should call it, not re-implement the regex.
- Repositories implement `pkg.Repository` (`RepoName()`); PlatformRepository also `pkg.PlatformRepositoryMarker` (`IsPlatformRepository() bool`), which `IsPlatform()` checks.
- `loader.ArrayLoader.LoadPackages` expects expanded metadata (ComposerRepository runs MetadataMinifier::expand first).
- RootPackageLoader needs a non-nil `loader.VersionGuesser` (Composer builds one itself): pass `version.NewVersionGuesser(version.NewProcessExecutor(pe), io)` with `pe.EnableAsync()` done; plus `loader.RepositoryManager` (`AddDefaultRepositories`: RepositoryFactory::defaultRepos + addRepository) and `loader.RootConfig` (`Repositories()`).
- `version.VersionSelector` takes the platform repository's packages and a `RepositorySet` interface; set `PHPVersion` from the detected PHP (Composer uses the PHP running it). Platform filters implement `version.PlatformRequirementFilter` (+ `IgnoreAllPlatformRequirementFilter` / `IgnoreListPlatformRequirementFilter`).
- Locker's lock-entry tweaks (dropping version_normalized, ...) are not in `dumper.ArrayDumper`, as in Composer.
- PackageSorter and PackageInfo (Composer\Util) live in internal/pkg (`pkg.SortPackages`, `pkg.GetViewSourceURL`, ...): internal/util cannot import packages.
- Plugin shim: there is no snapshot constructor yet (build packages with the constructors and setters); `AliasPackage::hasSelfVersionRequires` has no setter.

## Cleanup (DRY) from internal/pkg
- internal/pkg/loader/phpfilters.go copies internal/util's unexported parse_url port (for ValidatingArrayLoader::filterUrl) and holds filter_var(FILTER_VALIDATE_EMAIL); export parse_url (internal/php or util) and share.
- internal/pkg/loader/datetime.go is a subset of PHP's date parser (new \DateTime): ISO 8601 style dates, "@timestamp", offsets, common abbreviations and zone identifiers; relative formats and timelib's exact error positions are not reproduced. Move to internal/php if anything else needs DateTime.
- `loader.IsValidPerforcePort` is Composer\Util\Perforce::isValidPort; the Perforce port should use it (or own it and have the loader call it).
- internal/pkg/version keeps private ports of Git::cleanEnv/getVersion/buildRevListCommand/getNoShowSignatureFlags/parseRevListOutput/checkForRepoOwnershipError, Svn::cleanEnv and HgDriver::getBranches (git version cached per guesser, not statically); switch to the util Git/Svn ports when they exist.

## internal/repository/vcs, internal/downloader, internal/pkg, internal/command (from internal/util/vcs)
- Git/Hg/Svn/Perforce take `vcs.Process` (Execute, ExecuteFunc, GetErrorOutput: `*util.ProcessExecutor` or `*processmock.Mock`) and `http.Config` (adapt `*config.Config` as for util/http; Svn reads `Get("http-basic")`, a null value counting as absent like Config::has).
- Static caches stay process-wide: `vcs.GetVersion`/`GetHgVersion`/`Svn.BinaryVersion` run the binary once per process. Tests that pin the version (GitDownloaderTest/VersionGuesserTest `initGitVersion`) call `vcs.SetVersion("1.0.0", true)` and reset with `vcs.SetVersion("", false)` in t.Cleanup; such tests must not run in parallel.
- Process mock for every package: `internal/util/processmock` (ProcessExecutorMock): `processmock.Cmd(args...)` is a list expectation, `processmock.Shell(line)` a string one (PHP `===`: they never match each other); unexpected commands in strict mode return `*processmock.AssertionError` from Execute.
- `util.Process.SetInput` gives a Process stdin (Symfony's $input); `util.NewFinishedProcess` builds an already-run Process for test doubles; `util.Command.Args/Line/IsShell`; `util.SplitLines`.
- `util.SecurityError` is Composer\Exception\SecurityException (Perforce raises it); `pkg.SecurityError` duplicates it: make it `type SecurityError = util.SecurityError` so errors.As works across.
- `vcs.IsValidPort` is Perforce::isValidPort; `loader.IsValidPerforcePort` duplicates it: call `vcs.IsValidPort` (pkg/loader may import util/vcs).
- internal/pkg/version's private Git/Svn helpers (see above) can now use `vcs.CleanEnv`, `vcs.GetVersion`, `vcs.BuildRevListCommand`, `vcs.GetNoShowSignatureFlags`, `vcs.ParseRevListOutput`, `vcs.CheckForRepoOwnershipError`, `vcs.SvnCleanEnv`.
- util/http now exports `Rawurlencode`, `Rawurldecode`, `StoreAuthOf`, `ConfigList`, `NewProcessExecutor(io.IO)` (exports.go); they still belong in internal/php (see the cleanup note above).

## internal/repository, internal/composer, internal/plugin, internal/autoload, internal/command (from internal/platform)
- `platform.Detector` probes `php` on the PATH once per process (no disk cache; doc.go says why). Create one per process in internal/composer, call `Start()` early so the ~20 ms probe overlaps loading, and share it; `Snapshot()`/`Runtime()` wait.
- Use `snapshot.ComposerView(os.LookupEnv)`, not the raw snapshot, wherever Composer's own process is emulated: it applies bin/composer's xdebug restart (xdebug gone from the Runtime; the returned skipped version is `XdebugHandler::getSkippedVersion()` for PlatformRepository's ext-xdebug), memory_limit/display_errors and the xdebug ini changes. `CheckBinComposer()` gives bin/composer's aborts (print the message, exit 1).
- PlatformRepository takes a `platform.Runtime` and a `platform.HhvmVersionDetector`; port PlatformRepositoryTest with `platform/platformmock` (`Runtime` with per-method funcs and call recording, `ConstantMap`, `InvokeMap`, `HhvmDetector`). Invoke takes `platform.Func(name)`/`platform.StaticMethod(class, method)`; ResourceBundle/Imagick results implement `platform.ResourceBundle`/`platform.Imagick`. `testdata/oracle/*.json` hold Composer's `platform_packages` for six php builds, a ready golden for the PlatformRepository port.
- Without php `Detector` returns a `*PHPNotFoundError` (`errors.Is(err, platform.ErrPHPNotFound)`). PlatformRepository should then proceed with only the config.platform overrides when `php` is overridden, and return that error otherwise.
- The php binary for plugins/scripts is `platform.FindPHP()` (PLUGINS.md §7 puts `PhpBinary()` in internal/util; it lives here because util is owned elsewhere; move it if wanted). `util.IniGetAll(snapshot.IniFiles)` is IniHelper; `classmap.Parser.ShortOpenTag = snapshot.ShortOpenTag()`; `http.Runtime.PHPVersion()` is `snapshot.Version` ("" without php).
- `platform.HTMLEntityDecode` ports html_entity_decode (HTML 4.01, PHP 8.1 default flags); move it into internal/php when convenient.

## internal/composer, internal/command, internal/installer, internal/plugin, internal/autoload, internal/downloader, internal/repository, internal/resolver (from internal/eventdispatcher)
- Every event type lives in internal/eventdispatcher (the dispatcher creates them, and all their dispatchers sit above it): `Event`/`BaseEvent`, `ScriptEvent` (Composer\Script\Event), `PackageEvent`, `InstallerEvent`, `CommandEvent`, `PreCommandRunEvent`, `PreFileDownloadEvent`, `PostFileDownloadEvent`, `PrePoolCreateEvent`, `PHPEvent` (PHP-owned), plus the PackageEvents/InstallerEvents/PluginEvents name constants. internal/script holds only the ScriptEvents constants (`script.PostInstallCmd`, ...), which the dispatcher imports.
- Resolver types are held loosely: an operation must implement `eventdispatcher.Operation` (`String()` = `__toString`); `Transaction` and PrePoolCreateEvent's request are `any`; repositories are `pkg.Repository`; PreFileDownloadEvent's downloader is an `http.Getter`.
- Factory: `eventdispatcher.New(composer, io, process)`, `SetRunScripts(!disableScripts)`, then `SetScriptRuntime(plugin runtime)`, `SetPHP(&eventdispatcher.PlatformPHP{View: ...})` with the process-wide `ComposerView` taken at startup (the default probes php itself), and `SetEnsureComposerBinary(fn)`.
- `composer.Composer` must implement `eventdispatcher.Composer`: `Package()`, `Config()`, and `ScriptAutoloader()` returning the current local repository's canonical packages, the generator's `SetDevMode`, and `CreateLoader(packages)` = buildPackageMap(im, root, packages) + parseAutoloads + createLoader(map, vendor-dir) (with createLoader's classmap warnings) as `*eventdispatcher.LoaderContents`. A PartialComposer implements only `Package()`/`Config()`.
- COMPOSER_BINARY: set it at startup, as bin/composer does (`Platform::putEnv('COMPOSER_BINARY', realpath(argv[0]))`), to the launcher's deterministic path `<shim dir>/bin/composer` without extracting anything; the `SetEnsureComposerBinary` hook extracts the shim before a script process starts. The launcher (docs/PLUGINS.md D13) is a PHP 7.2.5-compatible script run as `php -d allow_url_fopen=… -d disable_functions=… -d memory_limit=… <launcher> args…` (`@composer …`, `composer …` scripts, `PHP_BINARY $COMPOSER_BINARY` in plugins); it must run maestro with the same arguments (find maestro through an env var maestro sets at startup, e.g. MAESTRO_BINARY, not a path baked into the shared shim dir), inherit stdin/stdout/stderr and the environment, and exit with maestro's exit code.
- COMPOSER_DEV_MODE is not the dispatcher's: Installer::run, RunScriptCommand, ScriptAliasCommand, ReinstallCommand and AutoloadGenerator::dump set it, as in Composer. `composer exec` dispatches `__exec_command`; `run-script`/script aliases pass the `script-alias-input` flag.
- Plugin runtime: PHP string listeners become `eventdispatcher.Script`, other callables `PHPCallable`; `removeListener` maps to `MatchCallable`/`MatchObject`/`MatchScript`. A PHP \Error crossing to Go must implement `eventdispatcher.PHPError`; a PHP ScriptExecutionException must be an `*eventdispatcher.ScriptExecutionError` for `errors.As`.

## Cleanup (DRY) from internal/eventdispatcher
- `determineBinaryCaller` (eventdispatcher.go) is a private port of BinaryInstaller::determineBinaryCaller; internal/installer's BinaryInstaller should share one copy (internal/installer may import eventdispatcher, or move it to internal/util).

## Users of internal/autoload (internal/composer, internal/eventdispatcher, internal/plugin, internal/installer, internal/repository, internal/command)
- `autoload.NewGenerator(dispatcher, io)` is AutoloadGenerator; set `Generator.Parser.ShortOpenTag` from the user's php `short_open_tag` (internal/platform) before dumping. Setters as in PHP; `SetApcu(apcu, prefix *string)` (nil prefix: random).
- Collaborators are narrow interfaces defined in autoload: `Config` (`Get(key, flags) (any, error)`, i.e. `*config.Config`), `InstalledRepository` (`DevPackageNames()`, `CanonicalPackages()`), `InstallationManager` (`InstallPath(p) (path string, ok bool, err error)`, ok=false for null), `EventDispatcher` (`DispatchScript(name, devMode, args, flags *php.Array) (int, error)`), `Locker` (`IsLocked() (bool, error)`, `LockData() (*php.Array, error)`). Platform requirement filters are `version.PlatformRequirementFilter` / `version.IgnoreAllPlatformRequirementFilter` (internal/pkg/version).
- `Dump(config, localRepo, root, im, targetDir, scanPsr, suffix, locker, strictAmbiguous)`: suffix "" is null, locker may be nil. Returns the `*classmap.ClassMap` (DumpAutoloadCommand counts it for "Generated optimized autoload files containing N classes"; that output line is the command's, not the generator's).
- `BuildPackageMap`/`ParseAutoloads(packageMap, root, DevFilter)` (`NoDevFilter`, `LegacyDevFilter`, `DevPackageNames(names)` for false/true/list) and `CreateLoader(autoloads, vendorDir) (*ClassLoader, error)`: the ClassLoader holds the registered PSR-0/4/classmap arrays (PHP key order); the plugin runtime builds the PHP ClassLoader from them.
- `autoload.ClassLoaderPHP`, `autoload.InstalledVersionsPHP` and `autoload.License` are Composer's files, embedded verbatim: FilesystemRepository::write must write `InstalledVersionsPHP` to vendor/composer/InstalledVersions.php; the plugin shim can reuse `ClassLoaderPHP`.
- ClassLoader.php and LICENSE are written only when their content differs (safeCopy), without copying the source mtime (Composer touches them with the phar's mtime).

## internal/util (from internal/autoload)
- Added exports `util.Dirname` (PHP dirname) and `util.RealpathOK` (realpath() with its false result).
- Fixed `phpRealpath` (behind `util.Realpath`/`RealpathOK`): it cleaned the path lexically (filepath.Abs) before resolving symlinks, so `realpath('/missing/..')` succeeded where PHP returns false.

## internal/composer, internal/installer, internal/eventdispatcher, internal/plugin (from internal/downloader)
- Factory::createDownloadManager: build `downloader.Deps{IO, Config, HTTPDownloader, EventDispatcher, Cache, Filesystem, Process, Store, Metadata, IniFiles}` once and pass it to `NewZipDownloader`, `NewTarDownloader`, `NewXzDownloader`, `NewGzipDownloader`, `NewRarDownloader`, `NewPharDownloader`, `NewFileDownloader`, `NewPathDownloader` (each returns an error where Cache::gc fails), registered with `DownloadManager.SetDownloader`. `Cache` is the files cache (`cache.New(io, cache-files-dir, "a-z0-9_./", fs, cache-read-only)`) and must be nil when cache-files-ttl <= 0, as in Composer: the archive downloaders read the shared store only with a usable files cache and write it only when that cache is not read-only (else they extract through a temporary store). `Store` is `store.Open(cache.Store(), &store.Options{Method: store.ParseMethod(os.Getenv(store.MethodEnv))})`. `SetPreferences` takes the preferred-install `*php.Array`.
- Zip/tar/xz/gzip downloads materialize the package into a staging directory under vendor/composer/ during the download phase (parallel); Install renames it into place and reports extraction errors after its "Extracting archive" line. InstallationManager must call Cleanup after every operation, as Composer does, to drop staging directories of aborted installs.
- `FileDownloader::$downloadMetadata` is `downloader.Metadata` (`Deps.Metadata`); InstallationManager resets and reads it for install notifications.
- PRE_FILE_DOWNLOAD/POST_FILE_DOWNLOAD use eventdispatcher's events through `downloader.EventDispatcher` (`Dispatch(name, event) (int, error)`, which `*eventdispatcher.EventDispatcher` implements). The first attempt's PRE is dispatched on the calling goroutine; retries, mirror fallbacks and POST come from download goroutines (Composer: promise callbacks inside the loop's wait), so the dispatcher, once it runs PHP listeners, must queue those and run them on the main flow (PLUGINS.md §5.14). For a dist taken from the store, POST_FILE_DOWNLOAD names the temporary file, which does not exist.
- `DownloadManager.Sync()` is the manager as `http.DownloadAndInstallPackageSync` takes it.
- Operations return `(*downloader.Promise, error)`: the error is a synchronous throw. Downloaders implement `downloader.Classer` for get_class().

## Cleanup (DRY), from internal/downloader
- internal/downloader/operation.go repeats Install/Update/UninstallOperation::format; internal/resolver owns the operations (it is above the downloader, so the downloader keeps its copy unless format moves lower).
- internal/downloader/archivablefiles.go ports ArchivableFilesFinder, Base/Git/ComposerExcludeFilter and Symfony's Glob::toRegex (oracle-checked) for PathDownloader's mirroring; the archiver port (internal/pkg/archiver) should own them and the downloader import it.
- `isBinPathInsidePackage` (filedownloader.go) is BinaryInstaller::isBinPathInsidePackage; internal/installer may use or own it.
- Added exports: `util.IsExecutable` (is_executable()), `store.Umask` (the process umask).
