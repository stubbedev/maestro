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
