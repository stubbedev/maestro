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
