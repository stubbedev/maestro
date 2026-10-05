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
- Done: PathDownloader mirrors with `archiver.NewArchivableFilesFinder` (internal/pkg/archiver owns the finder, the exclude filters and Glob::toRegex); internal/downloader/mirror.go keeps only Symfony's mirror/copy/symlink.
- `isBinPathInsidePackage` (filedownloader.go) is BinaryInstaller::isBinPathInsidePackage; internal/installer may use or own it.
- Added exports: `util.IsExecutable` (is_executable()), `store.Umask` (the process umask).

## internal/composer, internal/command, internal/plugin (from internal/pkg/archiver)
- Factory::createArchiveManager: `archiver.NewArchiveManager(dm.Sync(), loop)` (the manager takes the download manager as `archiver.DownloadManager`, which `DownloadManager.Sync()` satisfies, because internal/downloader imports archiver), then `AddArchiver(archiver.NewZipArchiver())` and `AddArchiver(archiver.NewPharArchiver())`, both unconditionally (ZipArchive and Phar are always "available"; tar.bz2 always compresses).
- `Archive(p, format, targetDir, fileName pkg.NullString, ignoreFilters)`; `PackageFilenameParts` returns ordered `[]FilenamePart` (PHP's array<string,string>). ArchiveCommand's own output is the command's.
- Errors keep PHP's classes: `*archiver.PharError` (PharException) and `*archiver.BadMethodCallError` pass through PharArchiver unwrapped, as in Composer; a pattern that does not compile is a `*util.ErrorException` (Composer's ErrorHandler turns preg_match's warning into one, also in PathDownloader's mirroring).
- Plugin archivers (Archiver interface) take excludes as `[]string`; ArchiveManager's getSupportedFormats only knows the two built-in classes, as in PHP.

## Cleanup (DRY), from internal/pkg/archiver
- `sysGetTempDir` (archivemanager.go) duplicates console's private `sysTempDir` (sys_get_temp_dir()); `typeError` duplicates internal/pkg/loader's TypeError message builder. Move both into internal/php (or util) and share them.
- go.mod lists github.com/dsnet/compress (bzip2 writer) as `// indirect` though archiver imports it directly; a `go mod tidy` when no port is running fixes the marker.

## Users of internal/repository and internal/locker (internal/composer, internal/resolver, internal/installer, internal/command, internal/plugin, composerrepo, vcs)
- Every RepositoryInterface method returns an error where PHP can throw (remote repositories fail anywhere); `RepoName()` cannot and counts the packages loaded so far. String constraints are parsed by the caller (`repository.ParseConstraint`); a nil constraint is PHP's null (any version). `search()`'s `?string $type` is `""` for null; results are `SearchResult` (Abandoned nil = unset), providers `[]ProviderInfo` keyed by name. `Class()` gives the PHP class (plugin mirrors); writable repositories have `Rev()`.
- Subclassing ArrayRepository from another package: not possible (the initialize/addPackage hooks are unexported). composerrepo/vcs repositories implement RepositoryInterface themselves; use `repository.CreateAliasPackage` (ArrayRepository::createAliasPackage), `NewConstraintMap`/`NameMap` (ordered `array<string, X>`), and `LoadResult`/`AlreadyLoaded`.
- `instanceof CompositeRepository` is `repository.AsComposite` (true for InstalledRepository too); other `instanceof` are type assertions on the concrete pointer types. `RepositoryUtils::flattenRepositories` is `FlattenRepositories`.
- Registry: `repository.Manager(io, cfg, httpDownloader, dispatcher, process, ExternalTypes{Composer: composerrepo ctor, VCS: vcs ctor})` registers every type in Composer's order (internal/composer passes the subpackage constructors; a nil one leaves its types unregistered). A `Constructor` is `func(config *php.Array, deps repository.Deps) (RepositoryInterface, error)`; `Deps` = io, *config.Config, *http.HttpDownloader, EventDispatcher (`any`; assert what you need), *util.ProcessExecutor. Composer creates the HttpDownloader when none is passed; here the caller must (`http.CreateHttpDownloader`). `RepositoryFactory::defaultReposWithDefaultManager` is not ported (it needs Factory): build the manager in internal/composer and call `DefaultRepos(io, cfg, rm)` (returns `*NameMap[RepositoryInterface]`, by name). `ConfigFromString(repo, allowFilesystem, httpDownloader)` drops PHP's unused $io/$config; its 'filesystem' config holds the json path where PHP holds the JsonFile object.
- `loader.RootPackageLoader`'s `RepositoryManager.AddDefaultRepositories` = `DefaultRepos(nil, cfg, rm)` then `rm.AddRepository` for each.
- `RepositorySet.FindPackages(name, c, flags)` returns `([]pkg.PackageInterface, error)`; `version.RepositorySet` (internal/pkg/version) has no error result: change it to return one (or wrap) when wiring VersionSelector. createPool* (internal/resolver) call `set.LockForPool()` (the installed-repository check + lock) and read `RootAliases()`, `RootReferences()`, `AcceptableStabilities()`, `StabilityFlags()`, `TemporaryConstraints()`, `Repositories()`. Root aliases come in as `[]RootAlias` (`RootAliasesFromArray(root.Aliases())`).
- Security advisories and filter lists: the data classes `PartialSecurityAdvisory`/`SecurityAdvisory` (Composer\Advisory) and `FilterListEntry` (Composer\FilterList) live in internal/repository, because repositories create them and internal/advisory and internal/filterlist sit above it (their auditors take the RepositorySet). Those packages should alias them (`type SecurityAdvisory = repository.SecurityAdvisory`); IgnoredSecurityAdvisory embeds SecurityAdvisory and satisfies `repository.Advisory`. Providers implement `AdvisoryProvider` / `FilterListProvider` (maps are `*NameMap`).
- PlatformRepository: `NewPlatformRepository(packages, configPlatform, PlatformOptions{Runtime, HhvmDetector, SkippedXdebugVersion})`. Pass `platform.NewRuntime(view)` and the skipped version from `snapshot.ComposerView(os.LookupEnv)`, and `platform.NewHhvmDetector(...)` (nil: no HHVM detection). A nil Runtime is "no php": only config.platform overrides when php is overridden, else `*platform.PHPNotFoundError` from the first package lookup. `PlatformRepository::getPlatformPhpVersion()` (a process-wide static in PHP) is the repository's `PlatformPhpVersion()`; feed it to `http.StaticRuntime.SetPlatformPHPVersion`. Composer's own versions are `repository.ComposerVersion`, `PluginAPIVersion`, `RuntimeAPIVersion`.
- FilesystemRepository/InstalledFilesystemRepository take a `repository.JSONFile` (`*json.File`) and write installed.json, installed.php and `autoload.InstalledVersionsPHP` (oracle-checked byte for byte). `SetInstalledVersionsSink(fn)` receives the installed.php data where Composer reloads InstalledVersions (plugin runtime). `SafelyLoadInstalledVersions(path)` evaluates an installed.php (the grammar Composer accepts) without PHP. internal/autoload must not import internal/repository (repository imports autoload for the embedded file).
- `InstallationManager` here is `InstallPath(p) (path string, ok bool, err error)` (same as autoload's).
- PathRepository and the Locker run git through `vcs.*` with a `repository.Process` (`vcs.Process` + ExecuteAsync; `*util.ProcessExecutor` and `*processmock.Mock` qualify); `repository.NewGuesserProcess` adapts one to `version.ProcessExecutor`.
- Locker: `locker.New(io, lockFile JSONFile, im, composerJSONContents, process)`; `SetLockData(LockDataInput{...}, write)` (DevPackages nil = PHP null; Aliases/StabilityFlags/Platform* are `*php.Array`); getters return errors (the lock file is read lazily). Lock files are oracle-checked byte for byte, including the always-true `setLockData` result when the data holds empty objects (PHP compares stdClass with arrays).
- ArtifactRepository walks directories in byte order of names; PHP's RecursiveDirectoryIterator uses readdir() order (file-system dependent).

## Cleanup (DRY), from internal/repository
- internal/pkg/version's private Git helpers (versionguesser.go) should use internal/util/vcs, as PathRepository and the Locker now do.
- `pkg.IsPlatformPackage` is matched by hand; `repository.IsPlatformPackage` and `PlatformPackageRegex` wrap/document it. internal/json/manipulator.go's copy of PLATFORM_PACKAGE_REGEX can use `repository.PlatformPackageRegex` (or pkg's function).
- Exports added for this port: `php.Serialize`/`AppendSerialize` (serialize()), `classmap.GlobOnlyDir` (glob() with GLOB_ONLYDIR|GLOB_BRACE|GLOB_MARK; move with the rest of glob.go into internal/util when convenient), `loader.ParseDateTime` (the loader's external test hook of the same name was dropped), `config.Config.ForIO()` (io.Config adapter for loadConfiguration; internal/composer can reuse it).

## internal/composer, internal/command, internal/plugin (from internal/repository/vcs, internal/downloader/vcs)
- `*config.Config` adapts to `http.Config` (util/http, util/vcs, the VCS drivers and downloaders) with `cfg.ForHTTP()` (internal/config/httpconfig.go), as to `io.Config` with `ForIO()`.
- Factory: `repository.Manager(io, cfg, httpDownloader, dispatcher, process, repository.ExternalTypes{Composer: ..., VCS: vcs.NewRepository})` (`internal/repository/vcs`). It registers "vcs" and Composer's aliases (git, bitbucket, git-bitbucket, github, gitlab, svn, fossil, perforce, hg); as in Composer 2.10.3, "forgejo" is a driver of "vcs" repositories, not a repository type. Direct construction: `vcs.NewVcsRepository(repoConfig, io, cfg.ForHTTP(), httpDownloader, process, vcs.Options{Drivers, VersionCache})` (nil drivers: `vcs.DefaultDrivers()`).
- Factory::createDownloadManager: call `vcs.Register(dm, vcs.Deps{IO, Config: cfg.ForHTTP(), Process, Filesystem})` (`internal/downloader/vcs`) before setting the archive downloaders (Composer's order). Use the same `*util.ProcessExecutor` (async enabled: Remove uses RemoveDirectoryAsync) and `*util.Filesystem` as `downloader.Deps`.
- SvnDownloader (`svn-cache-credentials`) and PerforceDownloader read the repo config of a package whose repository has `Class() == vcs.VcsRepositoryClass` and `RepoConfig()`; `*vcs.VcsRepository` provides both (the downloaders do not import the repository package).
- The VCS code keeps Composer's process-wide effects: `Git::cleanEnv`/`Svn::cleanEnv` change the process environment, and the git/hg/svn versions are cached process-wide (tests pin them with `vcs.SetVersion`/`SetHgVersion`/`SetSvnVersion` and must not run in parallel).
- VcsRepository's static `supports()` probes (`git tag`, `hg summary`, `svn info`, `fossil info`, `p4 info`) run through the repository's process executor where Composer creates a new ProcessExecutor (same commands); a plugin mirror of `getDriver()` is `VcsRepository.Driver()` (`Driver.Class()` gives the PHP class).

## Users of internal/repository (from internal/repository/vcs)
- Types outside internal/repository can now extend ArrayRepository (PHP `extends ArrayRepository`): embed it, call `r.Extend(outer, initialize)` first, and start `initialize` with `r.InitializeBase()` (parent::initialize()); AddPackage/FindPackage/... then work as in PHP. composerrepo may use it instead of reimplementing the ArrayRepository methods.

## Cleanup (DRY), from internal/repository/vcs and internal/downloader/vcs
- New shared exports: `util.IsRuntimeException(err)` (`catch (\RuntimeException)`; internal/downloader now uses it), `util.IsWritable` (is_writable(); internal/config/writable_*.go and internal/cache/sys_*.go keep private copies: switch them), `php.Base64Decode(s, strict)` (base64_decode, oracle-checked), `http.Urlencode`, `http.HTTPBuildQuery` (belong in internal/php with the other phpfuncs.go helpers), `vcs.SetSvnVersion`.
- `downloader.FilesystemError` is a \RuntimeException in PHP, but `util.IsRuntimeException` does not cover it (util cannot import internal/downloader; DownloadManager's update fallback checks it): add the check in the downloader or move the type into util.
- internal/downloader: `formatInstall`/`formatUpdate`/`formatUninstall`/`phpClassOf` are exported (`FormatInstall`, ..., `PHPClassOf`) for the VCS downloaders.

## internal/composer, internal/command, internal/plugin, internal/resolver (from internal/repository/composerrepo, internal/advisory, internal/filterlist)
- Factory: pass `composerrepo.Constructor` as `repository.ExternalTypes{Composer: ...}`. It needs a non-nil `*http.HttpDownloader` (Composer would create one) and asserts `composerrepo.EventDispatcher` (`Dispatch(name, eventdispatcher.Event)`) on `Deps.EventDispatcher`. Direct construction: `composerrepo.New(repoConfig, io, cfg, httpDownloader, dispatcher)`.
- Metadata PRE_FILE_DOWNLOAD/POST_FILE_DOWNLOAD events carry a `*composerrepo.MetadataContext{Response, Repository}` as context (PHP's `['repository' => $this]` / `['response' => ..., 'repository' => ...]`; a `*php.Array` cannot hold objects). The plugin shim maps it to the PHP array. Unlike internal/downloader, every dispatch happens on the goroutine calling the repository (doc.go says how the parallel loading is ordered).
- `getPackageNames` is `ComposerRepository.PackageNames(filter)` (values only; PHP's Preg::grep keeps keys, which no caller uses).
- `repository.SearchResult.Raw` (new field) holds a search API result array with all its keys (downloads, favers, repository, url, ...): SearchCommand's JSON output must encode `Raw` when it is set.
- Auditor: `advisory.Auditor.Audit(io, repoSet, policyConfig, packages, format, warningOnly, providerSet)`; the provider set is `filterlist.CreateFilterListProviderSet(policyConfig, repos, httpDownloader)` (nil for none). `RepositorySet` satisfies `advisory.RepositorySet`.
- composer/metadata-minifier is `internal/metadataminifier` (`Expand`, `Minify`); internal/pkg/loader's test helper `expand` duplicates `Expand` and can use it.

## Cleanup (DRY), from internal/repository/composerrepo, internal/advisory, internal/filterlist
- Exports added: `repository.SearchResult.Raw`, `repository.PackageVersionsConstraintMap(packages)` (the name => OR of `== version` map RepositorySet::getMatchingSecurityAdvisories and FilterListProviderSet build; RepositorySet uses it), `filterlist.PostOptions`/`HTTPOptions`/`AppendHeader` (`$options['http']` manipulation shared by FilterListApiClient and ComposerRepository; util/http's private `appendHeader`/`httpArray` do the same and could use them or move them into util/http).
- `composerrepo.exceptionClass` (get_class of a loader exception) overlaps `downloader.PHPClassOf`; a shared `util.PHPClassOf` would serve both.
- `Response::decodeJson` errors name only the URL (internal/util/http, see above); Composer's message also has jsonlint's details. ComposerRepository passes them through unchanged.

## Audit: regex errors must surface as Composer does
Several ports treat a regex engine failure (backtrack/recursion limit) as "no match". In PHP that depends on the call: Composer\Pcre\Preg::* throws PcreException (a RuntimeException); bare preg_match/preg_replace return false/null and the caller decides. Audit every internal/php Preg/Regexp call site across the tree and make each one follow its PHP call exactly (throw where Preg throws, false/null where raw preg_* is used). Do this after the regex engine's backtrack-limit accounting is final (pcre-perf task).

## Users of internal/resolver (internal/composer, internal/installer, internal/plugin, internal/command)
- Installer::doUpdate/doInstall build the inputs themselves (createRepositorySet, createRequest, requirePackagesForUpdate, createPolicy, createPoolOptimizer); internal/resolver/oracle_test.go's `buildOracleRequest` mirrors the update path and can serve as the template. Pools: `resolver.CreatePool(set, request, io, CreatePoolOptions{EventDispatcher, PoolOptimizer, IgnoredTypes, AllowedTypes (nil = null), SecurityAdvisoryPoolFilter, FilterListPoolFilter})`, `CreatePoolWithAllPackages(set)`, `CreatePoolForPackage(s)(set, names, lockedRepo)`; solving: `resolver.NewSolver(policy, pool, io).Solve(request, platformRequirementFilter)` returns a `*LockTransaction` or a `*SolverProblemsError` (`Code()` 2). Never pass a typed-nil `EventDispatcher`.
- `SolverProblemsError.PrettyString(set, request, pool, isVerbose, isDevExtraction, env)` and the problem messages need a `resolver.Environment`: `ExtensionLoaded(name)` (extension_loaded() of the PHP Composer emulates, i.e. the process-wide `ComposerView`) and `IniFiles()` (`util.IniGetAll(snapshot.IniFiles)`). nil means no extensions and no ini files. The "You are using Composer 2, which some of your plugins seem to be incompatible with" hint is always added (Composer skips it only when PHPUnit is loaded).
- Operations are `internal/resolver/operation` (below the downloaders): type-assert the concrete `*operation.InstallOperation` etc. for `instanceof`; `Show(lock) (string, error)` (UpdateOperation's version comparison can fail in theory), `String()` is `show(false)`. They satisfy `eventdispatcher.Operation`. `Transaction.PresentPackages()`/`ResultPackageMap()` expose the private fields flex reads through `Closure::bind`.
- `LockTransaction.Aliases(rootAliases *php.Array) *php.Array` is getAliases (input and output are the root alias arrays); `NewLockPackages(devMode, updateMirrors)`, `SetNonDevPackages(extraction)`; `NewLocalRepoTransaction(lockedRepo, localRepo)`.
- PRE_POOL_CREATE: the event's request is the `*resolver.Request`, its root aliases `resolver.RootAliasesArray(set.RootAliasList())` (package => version => alias array), its packages a plain list (PHP passes the builder's index-keyed array).
- Pool filters: `NewSecurityAdvisoryPoolFilter(advisory.Auditor{}, policyConfig, io)` and `NewFilterListPoolFilter(policyConfig, filterlist.FilterListAuditor{}, httpDownloader (http.Getter), blockScope, repositories, io)`.
- Divergences: DefaultPolicy keys its caches by the pool object (Composer by spl_object_id, which PHP may reuse for a later pool after the first is freed); `Decisions.String()` leaves out undone decisions (debug output only); RuleWatchChain is folded into RuleWatchGraph; rule hashes are not xxh3 (only used to find duplicates).
- Installer fixtures (tests/Composer/Test/Fixtures/installer) need the whole Installer; nothing in the resolver blocks them.

## From internal/resolver (changes in other packages)
- `version.RepositorySet.FindPackages` now returns `([]pkg.PackageInterface, error)` and `VersionSelector.FindBestCandidate` passes the error on; `*repository.RepositorySet` satisfies the interface (asserted in internal/resolver).
- internal/repository: `RepositorySet.RootAliasList()` (the root aliases in the order given, for the PHP array of PRE_POOL_CREATE) and the exported `RootAliasesPerPackage` (was rootAliasesPerPackage).
- internal/downloader/operation.go: `FormatInstall`/`FormatUninstall`/`FormatUpdate` now call internal/resolver/operation, which owns InstallOperation/UninstallOperation/UpdateOperation::format; callers may switch to the operation package directly and the file can go.
- Real-world oracle: tools/oracle/resolver/record.php (needs the network) recorded what PoolBuilder loads from Packagist for the kontainer project (private repositories dropped), laravel/laravel and symfony/symfony-demo into internal/resolver/testdata/oracle/*.json.gz; tools/oracle/resolver/solve.php resolves them offline in 7 variants into solve.json, and `--bench` times them for comparison with `BenchmarkOracleSolve`.
