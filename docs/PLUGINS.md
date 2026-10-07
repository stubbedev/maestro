# Composer plugins and PHP-callable scripts in maestro

This is the design and implementation spec for `internal/plugin` and its PHP
shim `internal/plugin/php` (deviation 5 in [PORTING.md](PORTING.md)). The raw
survey behind it is in [plugins-survey/](plugins-survey/): `plugins.md` holds
per-plugin behaviour notes, and `usage-matrix.tsv` lists every Composer symbol
each plugin references. Sources for all surveyed versions are in `.ref/plugins/`
(gitignored; `plugins-survey/tools/fetch.py` recreates it).

Contents:

1. Goal and constraints
2. Survey summary
3. Key decisions
4. Required API surface
5. Architecture
6. IPC message spec
7. Go-side interfaces needed from other packages
8. Coverage by tier
9. Tests
10. Risks
11. Size

---

## 1. Goal and constraints

Composer runs three kinds of project PHP code inside its own process:

- **Plugins**: packages of type `composer-plugin`, or the legacy
  `composer-installer`. Their `extra.class` is instantiated, `activate()`d and
  subscribed to events. The plugin can then call almost anything reachable from
  `Composer\Composer`.
- **PHP-callable scripts**: `"post-install-cmd": "Vendor\\Class::method"`
  entries in root `scripts`, called statically with a `Composer\Script\Event`.
- **Command-class scripts**: `"foo": "Vendor\\FooCommand"` entries, run as a
  Symfony Console command inside a fresh `Symfony\Component\Console\Application`.

maestro is Go, so this code runs in a separate `php` child process. The child
loads a shim: PHP classes under Composer's real names, backed by maestro's Go
state over IPC. Requirements:

- **Unchanged plugins.** Plugins load, run and see the same API, the same
  `class_exists`/`method_exists` answers, the same constants
  (`Composer::VERSION === '2.10.3'`, `PluginInterface::PLUGIN_API_VERSION === '2.9.0'`)
  and the same object identities as under Composer 2.10.3.
- **Identical results.** Output bytes and their order, exit codes, files
  written, `composer.lock` and the vendor tree match real Composer.
- **Not Composer's code.** The shim reimplements Composer's public API. It
  does not embed `composer/composer`'s `src/`. There are two exceptions:
  `vendor/composer/ClassLoader.php` and `InstalledVersions.php`. maestro must
  already embed these verbatim because it writes them into every vendor dir.
- **PHP 7.2.5 compatible.** The shim must run on every PHP that Composer
  2.10.3 runs on (7.2.5 to 8.5), because the child is the project's `php`.
  CI runs the PHP-backed plugin and command tests on 8.4 and on 7.2 (the
  `php-7_2` job); only the API parity test needs PHP 8 (its golden is PHP
  8.4's reflection).
- **Zero cost when unused.** No `php` process starts unless a plugin is
  actually registered or a PHP-callable or command-class script is dispatched.

---

## 2. Survey summary

The survey covers 47 package versions of 45 packages: the 25 requested
packages plus 20 more found on Packagist. 42 of the 45 are plugins. Source: `plugins-survey/packages.tsv` and `plugins.md`. "Tier" is
the API tier (§8) the plugin needs to pass its e2e fixture.

| Plugin (version) | Events (priority) | What it mutates or needs | Hard parts | Tier |
| --- | --- | --- | --- | --- |
| stubbedev/{atlassian,ds,jenkins,sentry}-mcp | POST_INSTALL_CMD, POST_UPDATE_CMD | Downloads a binary into its own `__DIR__`; `IO::write/writeError` | none | 2 |
| stubbedev/laravel-dev-mcp 0.0.15 | same | Writes `.mcp.json`; `Config::get('vendor-dir')` | `InstalledVersions::getPrettyVersion()` must return project data | 2 |
| laravel/framework `Illuminate\Foundation\ComposerScripts` | PHP-callable scripts: post-install/update/autoload-dump, pre-package-uninstall | Unlinks `bootstrap/cache/*.php`; `require_once vendor/autoload.php` in-process | Project autoloader coexists with the shim | 2 |
| pestphp/pest-plugin 4.0.0 / 5.0.0 | post-autoload-dump | Writes `vendor/pest-plugins.json`; CommandProvider `pest:dump-plugins` | `BaseCommand::run()` outside any Application; `new ConsoleOutput` | 2 (event), 4 (command) |
| phpstan/extension-installer 1.4.3 | POST_INSTALL_CMD, POST_UPDATE_CMD | Rewrites its own `src/GeneratedConfig.php` | Real composer/semver constraint objects (`getLowerBound`); `IM::getInstallPath` | 2 |
| infection/extension-installer 0.1.2 | same | Rewrites its own generated config | same as phpstan | 2 |
| dealerdirect/phpcodesniffer-composer-installer 1.2.1 | POST_INSTALL_CMD, POST_UPDATE_CMD | Runs `phpcs --config-set` through `ProcessExecutor` | Bundled Symfony Finder/PhpExecutableFinder; `findPackages(name, '>= 3.0.0')` | 2 |
| captainhook/hook-installer 1.0.5 | POST_INSTALL/UPDATE_CMD | `proc_open` with inherited stdio; throws on failure | `isDecorated()`; exception exit code | 2 |
| phpro/grumphp 2.25.0 | PRE/POST_PACKAGE_*, POST_*_CMD | `proc_open`, `git:init` | `getOperations()` (all ops) + `instanceof *Operation` | 2 |
| symfony/runtime 8.1.0 | POST_AUTOLOAD_DUMP | Writes `vendor/autoload_runtime.php` | `getSubscribedEvents()` returns `[]` until `activate()` ran; bundled Symfony Filesystem; `Factory::getComposerFile` | 2 |
| composer/package-versions-deprecated 1.11.99.5 | static POST_AUTOLOAD_DUMP | Writes `Versions.php` | Never allowed as a plugin (hard-coded); `getLocker()->getLockData()` is the in-memory lock | 2 |
| tbachert/spi 1.0.5 | PRE_AUTOLOAD_DUMP | Root autoload **in place**; localRepo package `setAutoload` **in place**; writes generated file | `buildPackageMap`/`parseAutoloads`/`createLoader()->register()` runs project code | 2 |
| composer/installers 2.3.0 | none | `IM::addInstaller` (LibraryInstaller subclass) | `supports()` on every type; `parent::uninstall()->then()`; loaded first (weight -10000) | 3 |
| oomphinc/composer-installers-extender 2.0.1 | none | Installer extends composer/installers' Installer | Cross-plugin class inheritance | 3 |
| mnsami/composer-custom-directory-installer 2.2.1 | none | Three installers: extends LibraryInstaller (type `library`), PluginInstaller, PearInstaller | `extra.class` is an array; **PearInstaller must not exist**; inherited PluginInstaller install/update must really (de)register plugins | 3 |
| drupal/core-composer-scaffold 11.4.8 | POST_PACKAGE_INSTALL, COMMAND, static PRE_AUTOLOAD_DUMP, POST_*_CMD | Root autoload in place; scaffolds files; `dispatchScript('pre-drupal-scaffold-cmd')`; git via ProcessExecutor; command `drupal:scaffold` | Static listener registered on an instance; custom script events | 2 (events), 4 (command) |
| cweagans/composer-patches 1.7.3 | PRE_INSTALL/UPDATE_CMD, PRE_PACKAGE_*, POST_PACKAGE_* (10) | Uninstalls packages before solving; patches install paths; `setExtra` in place on a localRepo package, which must reach installed.json | `IM::uninstall` + `getLoop()->wait()`; own `PatchEvent extends Event`; `getInstaller($type)->getInstallPath()` | 5 |
| cweagans/composer-patches 2.0.0 | PRE/POST_PACKAGE_* (10) | `patches.lock.json`; applies patches | `getPluginCapabilities(Custom)`; `getPlugins()` live instances; commands; `getApplication()->run(ArrayInput install)` | 5 |
| wikimedia/composer-merge-plugin 2.1.0 | INIT, PRE_INSTALL/UPDATE_CMD, PRE_AUTOLOAD_DUMP (50000) | Root package requires/devRequires/…/repositories **before solving**; `RM::prependRepository` | `new Link`, `MultiConstraint::create`, `ArrayLoader::load`; nested `Installer::create(...)->run()` | 5 |
| ergebnis/composer-normalize 2.54.0 | none | Command `normalize` writes composer.json, then runs `update --lock` | `new Factory()->createComposer`; `new Console\Application()->run(ArrayInput)` | 4 |
| bamarni/composer-bin-plugin 1.9.1 | COMMAND, POST_AUTOLOAD_DUMP | Command `bin`: `chdir`, `putenv`, nested `Application::doRun(StringInput)` | Needs `$io instanceof ConsoleIO` and reads its protected `input`/`output`/`helperSet`; `resetComposer()` | 6 |
| symfony/flex 2.11.0 | PRE_POOL_CREATE, PRE_OPERATIONS_EXEC, PRE/POST_*_CMD, auto-scripts | Pool filtering, recipes, composer.json/lock rewrites, `setLocker`, localRepo writes, re-runs install; `SymfonyPackInstaller extends MetapackageInstaller` | `debug_backtrace` for `Application`+`ArgvInput` and `Installer`; rewrites private `ArgvInput::$tokens`; `Closure::bind` into Transaction; array-cast reads of protected props | 6 |
| php-http/discovery 1.20.0 | PRE_AUTOLOAD_DUMP, POST_UPDATE_CMD | Root autoload in place; composer.json edit; re-runs the install in-process | `debug_backtrace` to find `Installer`; `clone` + `__construct(9 services)`; protected props through `(array)` casts | 6 |

Surveyed beyond the request (in `plugins.md`, same tiering): cakephp/plugin-installer, civicrm/composer-downloads-plugin, drupal/core-project-message, drupal/core-vendor-hardening, endroid/installer, johnpbloch/wordpress-core-installer, laminas/laminas-component-installer, laminas/laminas-dependency-plugin, magento/magento-composer-installer, mcaskill/composer-exclude-files, ocramius/package-versions, pyrech/composer-changelogs, ramsey/composer-repl, symfony/thanks, typo3/class-alias-loader, typo3/cms-composer-installers, vaimo/composer-patches, yiisoft/yii2-composer, zaporylie/composer-drupal-optimizations.

What the survey shows:

1. **Almost all use is shallow.** 42 packages reference `Composer` and
   `IOInterface`, 41 implement `PluginInterface`, and 35 subscribe to events. The dominant
   pattern is "on POST_INSTALL/UPDATE_CMD or *_AUTOLOAD_DUMP, read config, the
   local repo and install paths, then write a file". That needs Composer
   getters, `Config::get`, IO, the local repository, `IM::getInstallPath` and
   package getters (tier 2).
2. **Mutation in place is common.** Root package and localRepo package
   setters (`setAutoload`, `setExtra`, `setBinaries`, `setRequires`, …) are
   called on objects that Composer reads afterwards. Identity
   (`$package === $rootPackage`) and write-back are required.
3. **Subclassing Composer classes is common.** Plugins extend LibraryInstaller
   (5), BaseCommand (7), MetapackageInstaller, PluginInstaller, Package,
   Script\Event, Event, ConsoleIO, Cache, ComposerRepository and
   MultiConstraint. The shim must provide real PHP base classes with the same
   protected members, not interfaces over RPC.
4. **Some plugins run Composer itself in-process.** They call
   `Factory::create`, `Installer::create()->run()` and
   `new Application()->run()` from inside an event, then go on.
5. **Four plugins reach into Composer internals.** flex, php-http/discovery,
   bamarni and vaimo use `debug_backtrace` objects, private and protected
   properties, `Closure::bind` and `clone` plus `__construct`. They are tier 6,
   handled by explicit emulation (§5.12).

---

## 3. Key decisions

| # | Decision | Why |
| --- | --- | --- |
| D1 | **One long-lived `php` child per maestro process**, started lazily on first need and kept until exit. Nested Composer instances share it. | PHP static state (plugin singletons, `InstalledVersions`, `class_exists` caches, the eval-renamed classes of PluginManager) must persist exactly as in Composer's single process. |
| D2 | **Go owns all Composer state. PHP holds mirrors.** Value-like objects (packages, events, operations, input) are mirrored with field data. Service objects (Config, managers, repositories, IO, Locker, …) are thin proxies whose methods are RPC calls. | One source of truth. The Go ports stay the implementation, and the shim stays thin. |
| D3 | **Synchronous, re-entrant RPC over one channel.** Exactly one side runs at any moment. Either side may call the other while handling a call, to any depth. | Mirrors Composer's single call stack, so output order and side effects stay deterministic. There are no races and no locks. |
| D4 | **State syncs at every control transfer.** Each message carries the deltas (revision-based) of mirrored objects, env vars, cwd and Composer statics changed since the last message. | In-place mutations (`setAutoload`, `setExtra`, root `setRequires`, `putenv`, `chdir`) become visible to the other side before it runs again. That is the only moment it could observe them. |
| D5 | **Transport: inherited pipes on fds 3 and 4** (Unix), loopback TCP with a 256-bit token (Windows). Frames are a 4-byte length plus JSON. | stdin, stdout and stderr stay the user's terminal, so plugin `echo`, `proc_open` with inherited stdio and TTY prompts behave as in Composer. JSON is native to PHP and fast enough. |
| D6 | **Vendor the third-party libraries the 2.10.3 phar ships** (§5.1) verbatim into the shim: symfony/console 5.4.47, process, filesystem, finder, string, polyfills, composer/semver, pcre, class-map-generator, ca-bundle, metadata-minifier, spdx-licenses, xdebug-handler, react/promise, seld/jsonlint, signal-handler, phar-utils, justinrainbow/json-schema, marc-mabe/php-enum, psr/log, psr/container. | Plugins use these directly, assume Composer bundles them, and depend on their exact behaviour (Symfony Console commands, real semver constraint objects, React promises). They are MIT and not Composer's code. Loading them first in the child also reproduces the phar's class precedence. |
| D7 | **API presence parity.** Every class, interface, trait, public or protected method and constant in Composer 2.10.3's `src/` (minus `Compiler`, `PHPStan\*`) exists in the shim with the same signature. Unsupported members throw `Maestro\Shim\UnsupportedApiException`. No class Composer lacks is defined (no `PearInstaller`, no `Script\CommandEvent`). | Plugins feature-sniff with `class_exists`/`method_exists`/`defined` (flex checks `BaseConfigCommand`, mnsami guards `PearInstaller`, laminas ships a stub of `PrePoolCreateEvent`). The answers must be identical. |
| D8 | **Plugin loading is split.** Go does the policy: which packages, order, `allow-plugins` with its prompt, API version, autoload map. PHP does the mechanism: ClassLoader, `files` requires, the eval-rename hack, instantiation, `activate`, `addSubscriber`. | The policy needs Go's repositories, autoload generator and config. The mechanism must happen inside PHP. |
| D9 | **Custom installers use a virtual-dispatch table.** At `addInstaller`, the shim reports which `InstallerInterface`/`LibraryInstaller` methods the PHP class overrides. Go calls PHP only for those. Inherited behaviour runs natively in Go, and calls back into PHP for overridden hooks such as `getInstallPath`. | `composer/installers` overrides only `supports`, `getInstallPath` and `uninstall`. Downloads and extraction stay native and parallel (deviations 1 and 3). |
| D10 | **Plugin commands run in PHP** on the vendored Symfony Console: bind, validate, interact, execute. Go's Application lists, describes and dispatches them through proxy commands built from mirrored definitions. | Exact Symfony semantics for plugin code. `list`/`help`/completion output stays in Go's byte-identical console port. |
| D11 | **Output path.** `IOInterface` calls are RPC'd to Go's single IO. Symfony `OutputInterface` objects in PHP write straight to the inherited fd 1 or 2. Go flushes its output before every transfer to PHP. | One IO state (verbosity, `--profile` prefix, overwrite tracking, authentications). Symfony output objects get the exact behaviour of the vendored library. |
| D12 | **Exceptions cross both ways with identity.** A PHP exception that crosses Go and comes back into PHP is rethrown as the *same object*. A Go error becomes the mapped PHP class. | Plugins catch their own exception classes around nested calls. Rendering needs class, code, message and previous (maestro renders errors itself, without throw sites). |
| D13 | **`COMPOSER_BINARY` points to a PHP launcher stub** (`<shim>/bin/composer`), which `proc_open`s maestro with inherited stdio and passes its exit code through. | Plugins and `@composer` scripts run `PHP_BINARY $COMPOSER_BINARY …`. Pointing PHP at the Go binary would fail. |
| D14 | **No opcache or ini changes beyond what `bin/composer` does.** | `ini_get` and `extension_loaded` stay identical to Composer. Startup cost is controlled by lazy class loading instead (§5.16). |

---

## 4. Required API surface

**Backing** says how each member is implemented:

- **PHP**: pure shim code with no RPC.
- **Mirror**: a PHP object with local field data, synced (§5.3).
- **RPC**: each call is forwarded to Go.
- **Vendored**: the third-party library itself.
- **Stub**: present for parity (D7) but throws `UnsupportedApiException`.

**Used by** is the number of distinct surveyed packages that reference the
symbol (`usage-matrix.tsv`). Method counts match by name only. **Tier** is the
tier (§8) it belongs to. Rows marked **internal** are Composer `@internal` or
implementation detail. They are supported only where a surveyed plugin needs
them, and refusing them is acceptable elsewhere.

### 4.1 Plugin contract (PHP, tier 2)

| Symbol | Used by | Backing |
| --- | --- | --- |
| `Plugin\PluginInterface` (`activate`, `deactivate`, `uninstall`, `PLUGIN_API_VERSION = '2.9.0'`) | 41 | PHP interface |
| `EventDispatcher\EventSubscriberInterface::getSubscribedEvents` (all 3 shapes: `'m'`, `['m', prio]`, `[['m', prio], …]`) | 35 | PHP interface |
| `Plugin\Capable::getCapabilities`, `Plugin\Capability\Capability`, `Capability\CommandProvider::getCommands` | 6 | PHP interface (tier 4) |
| `Plugin\PluginEvents` constants: INIT, COMMAND, PRE_FILE_DOWNLOAD, POST_FILE_DOWNLOAD, PRE_COMMAND_RUN, PRE_POOL_CREATE | 6 | PHP |
| `Script\ScriptEvents` (all 14 constants) | 31 | PHP |
| `Installer\PackageEvents` (6), `Installer\InstallerEvents::PRE_OPERATIONS_EXEC` | 14 / 2 | PHP |
| `Plugin\PluginBlockedException` | 0 | PHP class |

### 4.2 Composer object graph

| Symbol | Used by | Backing | Tier |
| --- | --- | --- | --- |
| `Composer` / `PartialComposer`: `getPackage`, `getConfig`, `getRepositoryManager`, `getInstallationManager`, `getEventDispatcher`, `getLocker`, `getDownloadManager`, `getPluginManager`, `getAutoloadGenerator`, `getLoop`, `getArchiveManager`, `isGlobal` | 42 (`getPackage` 36, `getConfig` 30, `getRepositoryManager` 24) | Proxy. Each getter is an RPC that returns a handle, cached in PHP until a setter or sync invalidates it. | 2 |
| `Composer::VERSION`, `BRANCH_ALIAS_VERSION`, `RELEASE_DATE`, `SOURCE_VERSION`, `RUNTIME_API_VERSION`, `getVersion()` | 2 | PHP constants (`'2.10.3'`, `''`, `'2026-08-27 13:34:23'`, `''`, `'2.2.2'`) | 2 |
| `Composer::get/setRunningCommand`, `get/setRunningOperation` (static) | 0 | PHP statics, synced (§5.3) | 2 |
| `Composer::setLocker`, `setPackage`, `setConfig`, other setters | 1 (flex) | RPC | 5 |
| `Config::get($key, $flags)` incl. `RELATIVE_PATHS` | 31 | RPC on every call (no caching; Config can change through `merge`) | 2 |
| `Config::all`, `raw`, `has`, `merge`, `getRepositories`, `getConfigSource`, `getAuthConfigSource`, `setConfigSource`, `prohibitUrlByConfig` | 4 | RPC | 2 |
| `Config::disableProcessTimeout` (static, also used as a script) | 2 | Native in Go when used as a script (§5.5). From PHP code it is an RPC that sets Go's ProcessExecutor timeout. | 2 |
| private `Config::$baseDir` read by reflection (typo3, pyrech) | 2 | Declared private property, filled on mirror creation. **internal** | 6 |
| `Config\JsonConfigSource` (`addConfigSetting`, `addRepository`, `addLink`, `removeLink`, `getName`) | 1 | RPC proxy; `new JsonConfigSource(new JsonFile(...))` from PHP is an RPC constructor | 5 |
| `Factory::getComposerFile`, `getLockFile` (static) | 10 / 1 | RPC (honours Go's env, so `COMPOSER` set by `putenv` in PHP is seen after sync) | 2 |
| `Factory::create`, `createGlobal`, `createConfig`, `new Factory()->createComposer`, `createRemoteFilesystem`, `createHttpDownloader` | 3 / 1 / 1 / 1 | RPC: Go builds a new Composer (loading plugins, re-entrantly) and returns its handle | 5 |

### 4.3 IO

| Symbol | Used by | Backing | Tier |
| --- | --- | --- | --- |
| `IOInterface::write`, `writeError` (string or array, newline, verbosity) | 31 / 19 | RPC to Go IO | 2 |
| `isVerbose`, `isVeryVerbose`, `isDebug`, `isDecorated`, `isInteractive` | 8 / 1 / 1 / 3 / 3 | Mirror fields of the IO handle (refreshed by sync) | 2 |
| `overwrite`, `overwriteError`, `writeRaw`, `writeErrorRaw` | 1 | RPC | 2 |
| `ask`, `askConfirmation`, `askAndValidate`, `askAndHideAnswer`, `select` | 3 / 3 / 1 / 0 / 1 | RPC. The validator callable is passed as a handle, and Go calls back into PHP for each attempt. | 2 |
| `getAuthentications`, `hasAuthentication`, `getAuthentication`, `setAuthentication`, `loadConfiguration` | 1 | RPC (they change Go's HTTP auth) | 2 |
| PSR-3 `emergency` … `debug`, `log` (`warning` 3, `info` 2, `debug` 2) | 3 | RPC | 2 |
| `IO\NullIO` (`new NullIO`) | 6 | PHP-local. Given to Go, it maps to Go's NullIO with no RPC. | 2 |
| `IO\BufferIO` | 0 | PHP-local reimplementation over vendored StreamOutput(php://memory) | 4 |
| `IO\ConsoleIO` class, `instanceof ConsoleIO` | 2 | The Go IO's mirror is a `ConsoleIO` whose protected `$input`, `$output` and `$helperSet` are real vendored Symfony objects (§5.9). It is extended by bamarni, and vaimo calls `$output->setVerbosity()`. **internal** (protected) | 6 |

### 4.4 Events

| Symbol | Used by | Backing | Tier |
| --- | --- | --- | --- |
| `EventDispatcher\Event` (`getName`, `getArguments`, `getFlags`, `isPropagationStopped`, `stopPropagation`). Same protected props `$name`, `$args`, `$flags`, `$propagationStopped`. Subclassable. | 3 (+1 subclass) | Mirror | 2 |
| `Script\Event` (`getComposer`, `getIO`, `isDevMode`, `get/setOriginatingEvent`). flex subclasses it and skips the parent constructor. | 26 | Mirror | 2 |
| `Installer\PackageEvent` (`getOperation` 13, `getOperations` 3, `getLocalRepo`, `isDevMode`, `getComposer`, `getIO`) | 14 | Mirror. `getOperations()` returns the same operation mirrors for every event of one execute. | 2 |
| `Installer\InstallerEvent` (`getTransaction`, `isExecutingOperations`) | 2 | Mirror | 5 |
| `Plugin\PrePoolCreateEvent` (all getters; `setPackages`, `setUnacceptableFixedPackages`) | 2 | Mirror with lazy package tiers (§5.3) | 5 |
| `Plugin\CommandEvent` (`getCommandName`, `getInput`, `getOutput`), `PreCommandRunEvent` (`getInput`, `getCommand`) | 3 / 1 | Mirror; the input is a mirror (§5.9) | 4 |
| `Plugin\PreFileDownloadEvent` (`get/setProcessedUrl`, `get/setCustomCacheKey`, `get/setTransportOptions`, `getType`, `getContext`, `getHttpDownloader`), `PostFileDownloadEvent` | 0 | Mirror | 5 |
| `EventDispatcher\EventDispatcher`: `addSubscriber`, `addListener`, `removeListener`, `dispatch`, `dispatchScript`, `dispatchPackageEvent`, `dispatchInstallerEvent`, `hasEventListeners`, `setRunScripts` | 4 (`dispatch` 3, `dispatchScript` 2) | Proxy. `addSubscriber` runs in PHP and turns into `addListener` RPCs. | 2 |
| `EventDispatcher\ScriptExecutionException` (`new`, catch) | 2 | PHP class; maps to Go's ScriptExecutionException both ways | 2 |
| `new EventDispatcher($composer, $io)` (magento) | 1 | RPC constructor of a second Go dispatcher | 5 |

### 4.5 Packages, links, constraints

| Symbol | Used by | Backing | Tier |
| --- | --- | --- | --- |
| `PackageInterface` getters: `getName` 24, `getExtra` 35, `getType` 13, `getVersion` 10, `getPrettyName` 9, `getPrettyVersion` 7, `getRequires` 7, `getAutoload` 11, `getTargetDir`, `getDevRequires`, `getFullPrettyVersion`, `getSourceReference`, `getSourceUrl`, `getDistUrl`, `getBinaries`, `getReplaces`, `getStability`, `getInstallationSource`, `getUniqueName`, `getPrettyString`, `__toString`, `getRepository`, `getReleaseDate`, … (all of them) | 23 | Mirror | 2 |
| `RootPackageInterface`: `getMinimumStability`, `getStabilityFlags`, `getReferences`, `getAliases`, `getScripts`, `getDevAutoload`, `getSuggests`, `getConfig`, `getRepositories`, `getPreferStable` | 10 | Mirror | 2 |
| Setters, in place: `setAutoload` 7, `setDevAutoload`, `setExtra` 3, `setBinaries`, `setRequires`, `setDevRequires`, `setConflicts`, `setReplaces`, `setProvides`, `setSuggests`, `setStabilityFlags`, `setScripts`, `setReferences`, `setAliases` 5, `setRepositories`, `setDistUrl`, `setDistType`, `setDistSha1Checksum`, `setTargetDir`, `setInstallationSource`, `setSourceReference`, … | 10 | Mirror. On a Go-owned package each setter is an RPC (`pkg.<setter>`) that runs maestro's setter, whose new state comes back with the reply (§5.3); on a PHP-born package it sets the field. | 2 (all of them) |
| `AliasPackage`, `CompleteAliasPackage`, `RootAliasPackage` (`getAliasOf` 4, `instanceof`) | 5 | Mirror (the alias holds a handle to its target) | 2 |
| `Package`, `CompletePackage`, `RootPackage` (`new`, `extends Package` by civicrm, public `$id`) | 5 | Mirror; PHP-born objects register lazily (§5.3) | 2 (read), 5 (new or extend) |
| `BasePackage` constants (`STABILITY_*`, `$stabilities`, `SUPPORTED_LINK_TYPES`), `packageNameToRegexp`, `packageNamesToRegexp` | 3 | PHP | 2 |
| `Link` (`getSource`, `getTarget` 3, `getConstraint` 4, `getPrettyConstraint` 4, `getDescription`, `new Link`) | 1 (+getters 7) | PHP value class (no handle) | 2 |
| `Semver\*`: `VersionParser`, `Constraint`, `MultiConstraint` (also extended), `MatchAllConstraint`, `MatchNoneConstraint`, `Intervals`, `Comparator`, `Semver` | 4 / 4 / 2 / 1 / 0 / 3 / 1 / 1 | **Vendored** composer/semver 3.4.4. Constraints cross as structures (§6.4). | 2 |
| `Package\Version\VersionParser` (Composer's subclass: `parseNameVersionPairs`, `isUpgrade` static, `normalizeStability`, `parseStability`) | 6 | PHP subclass of vendored semver; the Composer-specific methods are RPC | 2 |
| `Package\Version\VersionSelector` (`findBestCandidate`, `findRecommendedRequireVersion` 2) | 2 | RPC constructor plus proxy | 5 |
| `Package\Loader\ArrayLoader::load`, `loadPackages`; `Dumper\ArrayDumper::dump` | 1 / 0 | RPC; returns new Go-owned mirrors | 5 |
| `Package\Locker`: `isLocked` 4, `isFresh`, `getLockData` 4, `getLockedRepository`, `getContentHash` (static) 2, `getPlatformRequirements`, `setLockData`, `new Locker` | 3 | RPC proxy | 2 (read), 5 (write and `new`) |

### 4.6 Repositories

| Symbol | Used by | Backing | Tier |
| --- | --- | --- | --- |
| `RepositoryManager`: `getLocalRepository` 23, `getRepositories` 3, `findPackage` 8, `findPackages`, `createRepository`, `addRepository` 2, `prependRepository`, `setRepositoryClass`, `setLocalRepository` | 24 | Proxy | 2 (local), 5 (the rest) |
| Local repository (`InstalledFilesystemRepository`): `getPackages` 15, `getCanonicalPackages` 7, `findPackage`, `findPackages`, `hasPackage`, `count`, `search`, `getDevPackageNames`, `setDevPackageNames`, `getDevMode`, `isFresh`, `addPackage` 3, `removePackage` 2, `write` | 23 | Service proxy: every method of a Go-owned repository is an RPC (`repo.*`) to maestro's repository, so queries are Go's port (string constraints parsed by Go), and the packages come back as the same mirrors each time. A repository created in PHP (`new ArrayRepository`) keeps Composer's behaviour locally. | 2 (read and write) |
| `ArrayRepository`, `InstalledArrayRepository`, `InstalledRepository` (`findPackagesWithReplacersAndProviders`, `getDependents`), `CompositeRepository`, `RootPackageRepository`, `PlatformRepository` (`new`, `PLATFORM_PACKAGE_REGEX`) | 1–3 each | PHP reimplementations over mirrors. `PlatformRepository` construction is an RPC (Go detects the platform). | 5 |
| Remote repositories (`ComposerRepository`, VCS, …): `loadPackages`, `findPackage(s)`, `getPackages`, `search`, `getRepoName`, `getProviders` | 2 | RPC proxy. **Subclassing `ComposerRepository` is a Stub** (zaporylie's subclass is dead code on API 2). | 5 |
| `RepositoryFactory::defaultRepos`, `defaultReposWithDefaultManager`, `manager` | 2 / 1 / 1 | RPC | 5 |
| `RepositorySet` (`new`, `addRepository`, `findPackages`, `createPoolForPackage`) | 2 | RPC proxy. **internal** | 6 |
| `RepositoryUtils::filterRequiredPackages` | 0 | RPC | 5 |

### 4.7 Installation

| Symbol | Used by | Backing | Tier |
| --- | --- | --- | --- |
| `InstallationManager::getInstallPath` | 14 | RPC. Go calls back into a PHP installer if that installer owns the type. | 2 |
| `addInstaller` 7, `removeInstaller` 3, `getInstaller` 3, `isPackageInstalled`, `install`, `update`, `uninstall`, `execute`, `ensureBinariesPresence`, `disablePlugins`, `setOutputProgress`, `notifyInstalls` | 7 | RPC. Installers cross as handles plus a vtable (§5.6). | 3 |
| `InstallerInterface` (all 9 methods) | 1 implements | PHP interface | 3 |
| `LibraryInstaller` (extended by 5): constructor `($io, $partialComposer, $type = 'library', ?Filesystem, ?BinaryInstaller)`. Protected props `$composer`, `$vendorDir`, `$binDir`, `$downloadManager`, `$io`, `$type`, `$filesystem`, `$binaryInstaller`. Methods `supports`, `isInstalled`, `download`, `prepare`, `install`, `update`, `uninstall`, `cleanup`, `getInstallPath`, `ensureBinariesPresence`, `getPackageBasePath`, `installCode`, `updateCode`, `removeCode`, `initializeVendorDir` | 5 | PHP base class. Each inherited method is an RPC to Go's LibraryInstaller acting on this instance's Go peer (§5.6). | 3 |
| `PluginInstaller`, `MetapackageInstaller` (extended by mnsami and flex), `NoopInstaller`, `ProjectInstaller` | 1 / 1 | Same pattern as LibraryInstaller | 3 |
| `BinaryInstaller` (`installBinaries`, `removeBinaries`, `determineBinaryCaller` static) | 1 | RPC | 3 |
| `DependencyResolver\Operation\*`: `InstallOperation`, `UpdateOperation` (`getInitialPackage` 3, `getTargetPackage` 10), `UninstallOperation`, `MarkAlias*Operation`, `OperationInterface` (`getOperationType` 2, `show`, `__toString`); `new UninstallOperation` (3) and `new InstallOperation` | 7 | Mirror (value plus package handles). PHP-born ops register lazily. | 2 |
| `Transaction` (`getOperations`; flex: `new Transaction($present, $resultMap)` and `Closure::bind` reading private `$resultPackageMap`) | 1 | Mirror with private `$presentPackages` and `$resultPackageMap`. The constructor is an RPC that computes the operations. **internal** | 6 |
| `Installer\SuggestedPackagesReporter` (`new`, passed to `Installer::setSuggestedPackagesReporter`) | 1 | RPC proxy | 6 |
| `Installer\InstallerEvent`, `PackageEvent` | see §4.4 | | |

### 4.8 Download, HTTP, process, loop

| Symbol | Used by | Backing | Tier |
| --- | --- | --- | --- |
| `Util\ProcessExecutor`: `new ProcessExecutor($io)`, `execute($cmd, &$out, $cwd)` 6, `executeTty`, `executeAsync` (promise), `getErrorOutput` 3, `splitLines`, `escape` (static) 2, `setTimeout`/`getTimeout` (static) | 5 | RPC (Go spawns; streamed output goes straight to the terminal). `escape` and `splitLines` are PHP-local ports. | 2 |
| `Util\Filesystem` (`new` 14): `normalizePath` 8, `isAbsolutePath` 7, `remove` 7, `ensureDirectoryExists` 6, `findShortestPath` 4, `findShortestPathCode`, `removeDirectory` 3, `rename`, `copy`, `copyThenRemove`, `relativeSymlink`, `filePutContentsIfModified`, `isDirEmpty`, `emptyDirectory`, `size`, `isLocalPath`, `getPlatformPath`, `junction`, `isSymlinkedDirectory`, … | 14 | RPC to `internal/util.Filesystem`. Paths resolve against the **PHP process's cwd**, which travels in every sync. | 2 |
| `Util\Platform` (`isWindows`, `getEnv`, `putEnv`, `clearEnv`, `getCwd`, `realpath`, `isTty`, `getDevNull`, `isDocker`, `workaroundFilesystemIssues`) | 2 | PHP-local (env and cwd are synced) | 2 |
| `Util\Silencer` | 0 | PHP-local | 2 |
| `Util\Loop` (`getHttpDownloader`, `getProcessExecutor`, `wait` 5, `new Loop`) | 1 new | Proxy. `wait` sends promise handles to Go and resolves their PHP deferreds with the results. | 5 |
| `Util\HttpDownloader` (`get`, `add`, `copy`, `addCopy`, `wait`, `getOptions`, `setOptions`, `new HttpDownloader($io, $config)`), `Util\Http\Response` (`getBody`, `getHeaders`, `getStatusCode`, `decodeJson`, `getHeader`), `Util\RemoteFilesystem` | 3 / 1 / 3 | RPC. Responses are PHP value objects. | 5 |
| `Downloader\DownloadManager` (`download`, `install`, `update`, `remove`, `prepare`, `cleanup`, `getDownloaderForPackage`, `getDownloader`, `setPreferDist`, `setPreferSource`) | 5 | RPC proxy. Promises resolve to the downloaded file path (civicrm relies on that). | 5 |
| `FileDownloader` (`new`, vaimo), `VcsDownloader`, `PathDownloader`, `ChangeReportInterface::getLocalChanges`, `VcsCapableDownloaderInterface` | 1 | RPC proxies. **internal**-ish | 6 |
| `Downloader\TransportException` (`getStatusCode`, `getHeaders`, `getResponse`, `new`) | 3 | PHP class; maps both ways | 2 |
| `Cache` (`new Cache($io, $dir)`, `read`, `write`, `copyFrom`, `copyTo`, `gc`, `isEnabled`; zaporylie `extends Cache`) | 3 | RPC proxy. Subclass constructors call the parent, which creates a Go peer. | 5 |
| `React\Promise\*` (`PromiseInterface::then` on installer returns, `resolve`, `all`) | 4 | Vendored react/promise 3.3.0 | 3 |

### 4.9 Autoload and InstalledVersions

| Symbol | Used by | Backing | Tier |
| --- | --- | --- | --- |
| `AutoloadGenerator`: `buildPackageMap` 4, `parseAutoloads` 2, `createLoader` 2, `setDevMode`, `setClassMapAuthoritative`, `setApcu`, `setRunScripts`, `setPlatformRequirementFilter`, `dump`; `new AutoloadGenerator($dispatcher)` | 6 | Proxy. Package maps cross as `[[pkg handle, path], …]`. `createLoader` asks Go for the resolved prefixes and classmap (Go does the scanning) and builds a real `ClassLoader` in PHP. | 2 |
| `Autoload\ClassLoader` (`register`, `unregister`, `loadClass`, `findFile`, `add*`, `getPrefixes*`, `getClassMap`) | 3 | **Verbatim** `ClassLoader.php` (the file maestro writes to `vendor/composer/`) | 2 |
| `Autoload\ClassMapGenerator::createMap` (deprecated wrapper) | 1 | PHP wrapper over vendored composer/class-map-generator, as Composer does | 3 |
| `InstalledVersions` (`getPrettyVersion` 3, `getReference` 2, `getRootPackage` 2, `isInstalled` 2, `satisfies`, `getRawData`, `getAllRawData`, `getVersion`, `getInstallPath`, `getInstalledPackages`, `reload`) | 5 | **Verbatim** `InstalledVersions.php`. Go pushes `reload` with the same data and at the same points as Composer (§5.4). | 2 |

### 4.10 JSON

| Symbol | Used by | Backing | Tier |
| --- | --- | --- | --- |
| `Json\JsonFile`: `new` 8, `read` 8, `write`, `exists` 5, `getPath` 5, `validateSchema` (incl. `LAX_SCHEMA`), static `encode`, `parseJson` 2 | 9 | RPC (Go's byte-identical encoder and seld/jsonlint-compatible errors). Exceptions map to `Seld\JsonLint\ParsingException` and `JsonValidationException`. | 2 |
| `Json\JsonManipulator` (`new` 3): `addLink`, `removeSubNode`, `addSubNode`, `addMainKey`, `removeMainKey`, `addConfigSetting`, `addRepository`, `addProperty`, `getContents` 4, … | 3 | PHP keeps `$contents`; each mutator is a stateless RPC `(contents, args) → (bool, contents)` | 5 |
| `Seld\JsonLint\JsonParser`, `ParsingException` | 1 / 3 | Vendored | 2 |

### 4.11 Commands and Console

| Symbol | Used by | Backing | Tier |
| --- | --- | --- | --- |
| `Command\BaseCommand` (extended by 7): `getComposer`, `requireComposer` 4, `tryComposer`, `setComposer` 3, `resetComposer`, `getIO`, `setIO`, `isProxyCommand`, `getApplication` 3, `initialize`, `getPreferredInstallOptions`, `formatRequirements`, `normalizeRequirements`, `renderTable`, `getTerminalWidth`, `complete`, `createComposerInstance`, `getPlatformRequirementFilter` | 9 | PHP class extending vendored `Symfony\Component\Console\Command\Command`. Composer-object methods are RPC. | 4 |
| `Command\GlobalCommand`, `BaseConfigCommand`, every built-in command class | 1 / 1 | PHP classes whose instances mirror Go's built-in commands (for `instanceof`, `find`, `getDefinition`); `run` from PHP is an RPC | 4 (presence), 6 (frames) |
| `Console\Application`: `new` 3 + `run`/`doRun` (nested runs), `find`, `has`, `add`/`addCommand`, `all`, `get`, `getVersion`, `getComposer`, `resetComposer`, `getIO`, `getDefinition`, `getHelperSet`, `setAutoExit`, `setCatchExceptions` | 5 | PHP subclass of vendored Symfony Application. The running app is a mirror of Go's Application. `new Application()` creates a new Go app peer. | 4 |
| `Symfony\Component\Console\*`: Input (ArgvInput, ArrayInput, StringInput, InputArgument, InputOption, InputInterface), Output (ConsoleOutput, StreamOutput, OutputInterface), helpers, questions, exceptions | 10 | Vendored symfony/console 5.4.47 | 4 |
| `Symfony\Component\{Process,Filesystem,Finder}` | 4 / 2 / 1 | Vendored | 2 |

### 4.12 Nested runs and plugin management

| Symbol | Used by | Backing | Tier |
| --- | --- | --- | --- |
| `Installer::create($io, $composer)`, setters (`setUpdate`, `setUpdateAllowList`, `setDevMode`, `setDumpAutoloader`, `setOptimizeAutoloader`, `setPreferSource/Dist`, `setAudit`, `setPlatformRequirementFilter`, `setSuggestedPackagesReporter`, …), `run()` | 3 | PHP builder object that records settings; `run()` is RPC `installer.run`, executed re-entrantly by Go | 5 |
| `Installer::__construct` with 9 services, `clone` | 1 (discovery) | Supported through frames (§5.12). **internal** | 6 |
| `PluginManager`: `getPlugins` (live instances), `getPluginCapabilities` (with `$ctorArgs`), `getPluginCapability`, `addPlugin`, `removePlugin`, `uninstallPlugin`, `registerPackage`, `deactivatePackage`, `uninstallPackage`, `isPluginAllowed`, `getGlobalComposer`, `getRegisteredPlugins` | 2 | Split (§5.4): the instance list and capabilities are PHP; registration and allow policy are RPC | 2 (load), 4 (capabilities) |
| `Util\PackageSorter`, `Util\Git`, `Util\Svn`, `Util\Url`, `Util\ComposerMirror`, `Util\GitHub`/`GitLab`/`Bitbucket`, `Util\AuthHelper` | 0 | RPC proxies where trivial, Stub otherwise | 6 or never |
| `DependencyResolver\Pool`, `PoolBuilder`, `Solver`, `Request` (`getFixedOrLockedPackages`, `getUpdateAllowList`), `Problem`, `Rule*`; `new Pool` (discovery) | 2 | `Request` is a mirror inside PrePoolCreateEvent; `new Request()` is maestro's. `new Pool($packages)` is PHP-local (a dumb container). Everything else is **internal** and Stub. | 5 / 6 |
| `SelfUpdate\*`, `Advisory\*`, `Policy\*`, `FilterList\*`, `Question\StrictConfirmationQuestion`, `Console\GithubActionError`, `Console\HtmlOutputFormatter` | 0 | Stub (presence only), except the Question and Console helpers, which are PHP-local, `Advisory\AuditConfig` (a mirror) and the `PolicyConfig` of `BaseCommand::createPolicyConfig()` (maestro's, its members presence-only) | — |

Total: about 160 classes have working behaviour. The remaining ~150 Composer
classes are generated stubs (§5.1).

---

## 5. Architecture

### 5.1 Components and layout

```
internal/plugin/                      Go (ports Composer\Plugin\*; hosts the bridge)
  manager.go          PluginManager port: policy, ordering, allow-plugins, API check, autoload plan
  runtime.go          php child lifecycle: spawn, boot, shutdown, exit propagation
  rpc/                transport (pipes/TCP), framing, codec (§6), call stack, handle tables, sync engine
  mirror_*.go         snapshot/apply for packages, links, constraints, events, operations, input, transaction
  svc_*.go            handlers for PHP→Go methods, one file per Composer area (§6.6)
  proxy_*.go          Go types that implement Go interfaces by calling PHP: Installer, Command, IO, Output, Listener, Repository
  shim.go             go:embed of php/, extraction to the cache dir, launcher stub
  php/                the shim (PHP), embedded
    bootstrap.php     entry point: ini, error handler, autoloaders, handshake, serve loop
    src/Maestro/…     runtime: Rpc, Codec, Handles, Mirror base, Sync, Frames, UnsupportedApiException
    src/Composer/…    shim classes, same relative paths as Composer's src/ (so file names in traces match)
    src/Composer/Autoload/ClassLoader.php, src/Composer/InstalledVersions.php   verbatim, from the same embed the vendor writer uses
    stubs/Composer/…  generated presence-parity stubs (tools/shimgen)
    lib/…             vendored third-party libraries (D6), with their LICENSE files, plus
                      vendor/composer's installed.json, installed.php, autoload_classmap.php
                      and autoload_files.php. Not "vendor/": Go module zips drop every file
                      below a nested vendor/ directory, which would empty a released embed.
    autoload.php      the generated static class map and `files` list (the index)
    MANIFEST          "<sha256> <path>" per file; the cache directory is named after its sha256
    bin/composer      the COMPOSER_BINARY launcher (D13)
    res/              Composer's res/*.json schemas, from internal/json's embed (not in the source tree),
                      where JsonFile::COMPOSER_SCHEMA_PATH and LOCK_SCHEMA_PATH point, as in the phar
tools/shimgen/        generator: reflects .ref/composer/src (as plugins-survey/tools/apiindex.php does),
                      writes stubs for every class not hand-written, and a parity golden used by tests
tools/shimvendor/     copies the exact library versions from .ref/composer/vendor (per .ref/composer/composer.lock)
```

**Vendored set.** This is exactly the phar's runtime dependencies (from
`.ref/composer/vendor/composer/installed.json`): composer/ca-bundle 1.5.12,
class-map-generator 1.7.3, metadata-minifier 1.0.1, pcre 2.3.2, semver 3.4.4,
spdx-licenses 1.6.0, xdebug-handler 3.0.5; justinrainbow/json-schema 6.10.0;
marc-mabe/php-enum 4.7.2; psr/container 1.1.1; psr/log 1.1.4; react/promise
3.3.0; seld/jsonlint 1.12.1, phar-utils 1.2.1, signal-handler 2.0.2;
symfony/console 5.4.47, deprecation-contracts 2.5.4, filesystem 5.4.45,
finder 5.4.45, polyfill-ctype/-intl-grapheme/-intl-normalizer/-mbstring/-php73/-php80/-php81/-php84,
process 5.4.51, service-contracts 2.5.4, string 5.4.47.
`tools/shimvendor` copies them (the files `Compiler.php` puts into the phar,
verbatim) and records their versions in `lib/composer/installed.json`. A test
fails if they drift from `.ref/composer/composer.lock` or from the files
`.ref/composer/vendor` holds. Shared generator code is
`internal/plugin/shimbuild`; `go generate ./internal/plugin` rebuilds the index
and the manifest after a hand-written shim file changes, and a test fails when
either is stale.

**Shim autoloading.** `bootstrap.php` registers one prepended autoloader
before anything else. It is a static classmap generated at build time,
covering the shim, the stubs and the vendored libraries, plus the vendored
polyfills' `files`. This mirrors the phar: its classes win over a project's
copy of the same names (ramsey/composer-repl requires `composer/composer`,
and ergebnis ships its own json-schema). The PluginManager and dispatcher
autoloaders are appended after it, as Composer's are.

**Stubs.** A stub has every public and protected method with Composer's
exact signature. Each body throws:

```
throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\X::y() in plugins yet');
```

`UnsupportedApiException` extends `\LogicException`. Stub constants and
static properties keep Composer's values, so code that only reads constants
works.

**Embedding and extraction.** At first use, `php/` is extracted to
`<COMPOSER_CACHE_DIR>/maestro/shim/<sha256 of MANIFEST>/`. The write is
atomic: build into a temp dir (files read-only, the launcher `0555`,
directories `0700`), write `MANIFEST` last, rename into place. A directory is
valid when its `MANIFEST` equals the embedded one; a broken one is moved
aside and extracted again. Directories stay writable for their owner so that
clearing the cache can delete the shim. `ClassLoader.php` and
`InstalledVersions.php` are written from `internal/autoload`'s embed, and
`res/*.json` from `internal/json/res`'s (they are not in the source tree,
`shimbuild.VirtualFiles`). The shim is reused across runs and projects.

### 5.2 Process model and lifecycle

**When the child starts.** The PHP runtime is a lazily initialised singleton
per maestro process (`plugin.Runtime`). The first call that needs PHP starts
it. That happens in exactly these cases:

1. `PluginManager.RegisterPackage` reaches the instantiation step. All gates
   have passed: plugins enabled, plugin API version, flex < 1.9.8 check,
   `allow-plugins`, and `extra.class` present.
2. The dispatcher reaches a PHP-callable script (`isPhpScript`) or a
   command-class script (`isCommandClass`) that is not native (§5.5).
3. A PHP-born handle is needed. This only happens once PHP is already running.

Gating messages (for example "was skipped because it requires a Plugin API
version…") and `PluginBlockedException` happen in Go, without starting PHP.

**Which `php` binary.** The child uses the same binary that
`internal/platform` uses for platform detection: one shared resolver in
`internal/util`. It looks up `php` on `PATH`, as Composer's shebang
`#!/usr/bin/env php` does. If none is found while PHP is required, maestro
prints this maestro-only error and exits with code 1:

```
maestro: plugin "<name>" requires PHP but no php binary was found in PATH
```

For a script the message is `script <callable> requires PHP …`. A PHP below
7.2.5 gets Composer's own message from `bin/composer` and exit code 1.

**Command line.**

```
<php> [xdebug args] <shimdir>/bootstrap.php
```

No `-n` and no `-d` flags, except the xdebug handling below. This keeps the
user's ini, as Composer does.

- **Xdebug.** Composer's `XdebugHandler` restarts PHP without xdebug unless
  `COMPOSER_ALLOW_XDEBUG=1`. maestro reproduces the result in Go: if
  platform detection saw xdebug loaded and `COMPOSER_ALLOW_XDEBUG` is not
  set, it writes the same temporary ini that XdebugHandler writes (all loaded
  ini files concatenated with the xdebug `zend_extension` lines commented
  out) and starts the child with `-n -c <tmp.ini>`. It also sets the env vars
  XdebugHandler sets on restart (`COMPOSER_ORIGINAL_INIS`, and
  `PHPRC`/`PHP_INI_SCAN_DIR` as its `getRestartSettings` does), as
  ported from `.ref/composer/vendor/composer/xdebug-handler/src/XdebugHandler.php`
  (`internal/plugin/xdebug.go`; `mergeLoadedConfig` compares the loaded
  settings with `parse_ini_string()` of the files; maestro only claims a
  file value equal when it needs no evaluation, so it may write a setting
  again with the value php already loaded, which changes nothing. A test
  checks that `php -n -c <tmp.ini>` has the same extensions and settings as
  plain `php`. As in XdebugHandler, a restart that cannot be prepared leaves
  xdebug on).
- **Env.** Go's current environment (see the env sync in §5.3), plus:
  - `COMPOSER_BINARY=<shimdir>/bin/composer`
  - `MAESTRO_IPC=fd:3,4` (Unix) or `MAESTRO_IPC=tcp:127.0.0.1:<port>` and
    `MAESTRO_IPC_TOKEN=<hex>` (Windows)

  The shim removes `MAESTRO_IPC*` from its env (`putenv`, `$_SERVER`,
  `$_ENV`) in the first lines of `bootstrap.php`, before any plugin code runs.
- **stdio.** stdin, stdout and stderr are maestro's own fds 0, 1 and 2,
  inherited unchanged.

**bootstrap.php sequence.** It mirrors `bin/composer` and
`Application::doRun` in this order:

1. If `PHP_VERSION_ID < 70205`, print Composer's message and `exit(1)`.
2. Read the IPC settings, open the channel, and remove the env vars.
3. Set `setlocale(LC_ALL, 'C')` and `error_reporting(-1)`.
4. Set `display_errors` as `bin/composer` does: `stderr`, or `0` when
   `log_errors` is on with an empty `error_log`.
5. Apply `memory_limit` as `bin/composer` does: `COMPOSER_MEMORY_LIMIT`, or
   raise the limit to 1536M if it is lower.
6. Register the shim autoloader and require the polyfill `files`.
7. Send `hello` (§6.5) and receive `boot`: Go answers `hello` with `null`,
   and PHP enters the serve loop, where Go's first call is `boot`. It
   carries:
   - `$_SERVER['argv']` and `$argc` (maestro's `os.Args`; `argv[0]` is what
     the user typed);
   - `SCRIPT_NAME`, `SCRIPT_FILENAME` and `PHP_SELF` (set to `argv[0]`);
   - the IO handle and its state;
   - and, in its sync block, the cwd and the Composer statics.
8. Call `ErrorHandler::register($io)`. The shim's ErrorHandler is a
   reimplementation with Composer's behaviour (the same
   `\ErrorException`s, the same notices at the same verbosity), using the
   boot IO; the deprecation notices and warnings it reports are rendered
   by maestro (`ui.diagnostic`, internal/ui), as maestro's own are. (As
   `bin/composer` does, step 6 already registered it without an IO.)
9. Enter the serve loop: wait for requests from Go.

bin/composer's other checks (the non-CLI SAPI warning, the opcache preload
shutdown function, HHVM 4, iconv or mbstring, the Windows `$_SERVER`
workaround) run at their places in this sequence too.

**Shutdown and exit.**

- **Normal end.** Before exiting with code N, Go flushes its output, sends
  `shutdown {code: N}`, and waits for the child to exit. The shim calls
  `exit(N)`, so PHP runs `register_shutdown_function` callbacks and
  destructors, as at the end of Composer's process. maestro then exits with
  the **child's** exit status, because a shutdown function that calls
  `exit(M)` changes Composer's status too.
- **`exit()` from plugin code, or a PHP fatal error.** The PHP process ends
  while Go waits on a call. Go treats EOF together with a child exit status S
  as `*plugin.PHPExit{Code: S}`. That error propagates to `cmd/maestro`
  without further output, and maestro exits with S. A fatal error prints its
  own `PHP Fatal error: …` on stderr and gives 255, as in Composer. Go-side
  deferred cleanup still runs, for example InstallationManager's
  `runCleanup`. Composer would not run it on `exit()`, but it produces no
  output, so the difference is not observable.
- **SIGINT/SIGTERM.** Both processes are in the same process group and
  receive the signal. Go runs Composer's signal-handler behaviour. If the
  child dies of the signal mid-call, Go treats it as above, with exit status
  128+signal.

### 5.3 Object model: ownership, handles, mirrors, sync

**Ownership.** Every object that crosses the channel has exactly one
**owner** (the side holding the real object) and gets a **handle**:

- positive integers are allocated by Go, for Go-owned objects;
- negative integers are allocated by PHP, for PHP-owned objects.

Handles are never reused within a run, and there is no garbage collection.
That is acceptable for a single command run and keeps identity trivial.
Each side keeps an identity map from handle to object. The same handle always
yields the same PHP object, so `===` and `spl_object_id` stay stable.

**Kinds of crossing objects.**

| Kind | Owner | Other side holds | Examples |
| --- | --- | --- | --- |
| Service proxy | Go | PHP proxy, every method an RPC (some immutable values cached) | Composer, PartialComposer, Config, RepositoryManager, InstallationManager, DownloadManager, EventDispatcher, Locker, AutoloadGenerator, Loop, HttpDownloader, Cache, remote repositories, Application (Go's), built-in commands |
| Data mirror | Go | PHP object with a local copy of the fields and a revision; dirty tracking on setters | Package family, local and root package repositories (the package list), events, operations, Transaction, Request, input, IO state |
| PHP object | PHP | Go proxy implementing the needed Go interface by calling PHP | plugin instances, PHP installers, PHP commands, capability objects, listener callables (objects, closures, `[obj, 'm']`, `'Class::m'`), PHP IO or output objects passed to Go, validators |
| PHP-born data | PHP until first crossing, then Go | after registration it is a normal data mirror | `new Package(...)`, `new CompletePackage`, `new InstallOperation(...)`, PatchEvent instances |
| Value | n/a | copied each time | `Link`, constraints, `Http\Response`, scalars and arrays |

**Mirror revisions (the sync engine).**

- Go side: every mirrorable Go object exposes `Rev() uint64`, a counter that
  every setter increments (§7). For each handle sent to PHP, the bridge
  remembers the `Rev` it last sent.
- PHP side: a `Maestro\Shim\MirrorAdapter` per family (registered by shim
  base class) builds, reads and writes mirrors from outside the class, and
  `Maestro\Shim\Mirrors` keeps the revisions and dirty-field sets by object,
  so mirror classes declare exactly Composer's members (D7). Shim setters
  call `Mirrors::touch($this, 'field')`. Direct property writes are not
  tracked, because mirrors keep their data in private fields, as Composer's
  do.
- Go side, `internal/plugin/rpc`: a mirror implements `rpc.Mirror`
  (`PHPClass`, `MirrorBase`, `Rev`, `MirrorSnapshot`, `ApplyMirror`); Go
  sends the full snapshot of a changed mirror unless it also implements
  `rpc.DeltaMirror` (changed fields since a revision). PHP-born mirrors are
  built by the `rpc.MirrorFactory` registered for their base.

**How the mirrors use it.** The package, event, operation, IO and
PluginManager mirrors never send dirty fields: their setters are RPCs
(`pkg.setExtra`, `event.stopPropagation`, `pm.disablePlugins`, ...) that
run maestro's own setter, so Composer's setter semantics (an alias
forwarding to its package, `setSourceDistReferences` touching three
fields, `setRepository` refusing a second repository) are maestro's
code; the changed `Rev` sends the new snapshot with the reply, before the
setter returns in PHP. Dirty-field sync remains for mirrors whose setters
are plain fields (tests use it). Snapshots are full, except the core tier
of PRE_POOL_CREATE's package lists (below). Service objects (Composer, Config, the
managers, repositories, the Locker, the AutoloadGenerator) are not mirrors:
PHP holds an instance of their Composer class built without its
constructor, whose methods are RPCs; a stub class's methods throw
UnsupportedApiException. Go objects that are not maestro's own types are
wrapped once per object (`bridge.go`), so identity holds.

At **every message boundary**:

- **Go → PHP** (a request or a response): for each handle PHP knows whose
  `Rev` differs from the last sent, send the changed fields (or the full
  snapshot) in `s.o` (§6.3). PHP applies them in place. Object identity is
  preserved.
- **PHP → Go**: for every dirty mirror, send the dirty fields in `s.o`. Go
  applies them through the normal setters, which bump `Rev`, and then marks
  that revision as the one PHP has, so it does not echo back.

The same blocks carry other process state:

- **env**: Go's `os.Environ()`, and on the PHP side the environment the
  processes Composer starts get: Symfony Process's default environment,
  the variables `getenv()` and `$_SERVER` both have, with `$_ENV`'s on
  top. Each is diffed against the last state sent. So
  `Platform::putEnv()` (which sets `$_SERVER` and `$_ENV` too) in PHP code
  reaches maestro and every process it starts, while a plain `putenv()` of
  a new variable does not, exactly as it reaches no process Composer
  starts (the e2e suite checks this against Composer). The receiver
  applies changes with `putenv`; the PHP side also updates `$_SERVER` and
  `$_ENV`, as `Platform::putEnv` does.
- **cwd**: `getcwd()` and `os.Getwd()`. The receiver calls `chdir`.
- **statics**: `Composer::$runningCommand`, `$runningOperation`,
  `ProcessExecutor` timeout, `Platform` static caches that Composer exposes.

Scanning is O(known handles) per transfer. That is a few hundred integer
compares in normal runs and about 10k during PRE_POOL_CREATE, which is cheap.

**Lazy snapshot tiers (packages, PRE_POOL_CREATE's lists).** A
package mirror arrives in one of two tiers:

- **core**: id, class, name, prettyName, version, prettyVersion, type,
  stability, isDev, alias target handle, repository handle.
- **full**: all remaining fields.

PHP requests `pkg.load` the first time any non-core getter runs. One
`pkg.load` fetches that package and the core-tier packages still without
their fields that PHP received first, 64 in all at most: code that reads
a field of one package of a list mostly reads it of the others in
order, so a list read in full takes a round trip per 64 packages. Go
sends full snapshots when it knows they will be used: local-repository
packages, the root package, and the operation packages of package
events. Go sends core snapshots for PRE_POOL_CREATE package lists, which
can hold more than 10k packages, and for the package lists of
transactions.

**PHP-born data objects.** PHP-born events are adopted this way (a
plugin's Event subclass passed to `dispatch()`: Go builds an
`eventdispatcher.PHPEvent` from its snapshot, and listeners get the very
same object back), and so are packages and operations created in PHP.

`new Package('dummy/pkg', '1.0.0.0', '1.0.0')`
stays purely local. composer/installers creates one on every `supports()`
call and never sends it. When such an object first crosses to Go (in a
param, a return value or a setter argument), the codec sends its class and
full snapshot. Go creates the Go object, assigns a positive handle and
answers in `s.reg`. PHP rebinds the same PHP object to that handle, so from
then on it is a Go-owned mirror. A user subclass (civicrm's
`Subpackage extends Package`) keeps its PHP class. Go sees the nearest shim
base class, recorded in the snapshot as `"base"`.

### 5.4 Plugin loading

`internal/plugin.Manager` ports `PluginManager.php` 1:1 for everything
before instantiation:

- `loadInstalledPlugins` and `loadRepository`, including the weights
  (`composer/installers` and `plugin-modifies-install-path` get -10000),
  `PackageSorter::sortPackages` and `RepositoryUtils::filterRequiredPackages`,
  plus the "not required by the root package anymore" warning.
- `registerPackage` gates: disabled plugins, `composer-plugin-api` missing or
  mismatched (exact warning texts), the flex < 1.9.8 rule, `isPluginAllowed`
  (with the interactive y/n/d/? prompt that writes `allow-plugins` through
  the config source, `composer/package-versions-deprecated` always false,
  and pre-2.2 lock BC), `extra.class` validation, and already registered.
- Autoload plan: clone the root package with `files` cleared,
  `collectDependencies` over `InstalledRepository([local, rootPackageRepo, global?])`,
  `shouldAutoloadPackage` keys, install paths (which may call a PHP
  installer), and `AutoloadGenerator.ParseAutoloads`. Then compute the
  `createLoader` contents in Go: psr-0, psr-4, classmap (scanned by
  `internal/classmap`) and `files` (identifier → path).

Go then calls PHP `plugin.load` (§6.5) with the package handle, classes,
loader contents, `isGlobal`, `legacyInstaller`, `failOnMissingClasses` and
`runningInGlobalDir`. The shim's `PluginManager` (a PHP class) does
mechanically what Composer does:

1. Create a `ClassLoader` from the contents and call `register(false)`.
2. Require each `files` entry through `composerRequire`, skipping the
   one identifier Composer's PluginManager skips.
3. For each class, if `class_exists($class, false)`, do the eval-rename
   hack: `_composer_tmp<N>`, with `__FILE__`, `__DIR__` and `__CLASS__`
   replaced, and a static counter that persists for the whole process.
4. For a legacy installer, check `is_a(InstallerInterface)`, write the
   warning, then call `new $class($io, $composer)` and `IM::addInstaller`.
5. Otherwise check `is_a(PluginInterface)` and call `new $class()`, then
   `addPlugin`. `addPlugin` checks the source package with an RPC to Go's
   `isPluginAllowed`, then writes `Loading plugin <class> (from <pkg>[, installed globally])`
   at DEBUG verbosity, appends to `$plugins`, calls `activate()`, and calls
   `addSubscriber` if the class implements `EventSubscriberInterface`.
6. If the class is not found and `failOnMissingClasses` is set, throw
   `UnexpectedValueException` with Composer's text.

The return value lists the registered PHP objects (handles and classes). Go
records them in `registeredPlugins[name]`.

The following PHP-side `PluginManager` methods are local:

- `getPlugins`, `getPluginCapability` and `getPluginCapabilities` (with
  `$ctorArgs['plugin']` injected, and the exception texts);
- `removePlugin` (writes `Unloading plugin`, calls `deactivate`, then the
  dispatcher's `removeListener`);
- `uninstallPlugin`.

`registerPackage`, `deactivatePackage`, `uninstallPackage`,
`isPluginAllowed`, `arePluginsDisabled`, `disablePlugins`,
`getRegisteredPlugins` and `getGlobalComposer` are RPC calls into the Go
manager. Go calls back `plugin.deactivate` / `plugin.uninstall` for the
PHP-side steps.

**InstalledVersions.** Go mirrors Composer's two load points:

1. In `Factory.createComposer` with a full load, when plugins and scripts
   are not disabled, the class was not yet "loaded" (a per-runtime flag in
   Go) and `vendor/composer/installed.php` exists: Go reads the file then,
   and validates it with the `safelyLoadInstalledVersions` regex once the
   data is first needed (most runs never start PHP). If PHP is not yet
   running, Go records the data as pending. When PHP starts, or right away if it is
   running, it sends `iv.reload {data}` with `__DIR__` already substituted.
2. On every `FilesystemRepository.Write` with `dumpVersions`, Go sends
   `iv.reload {data, selfDir}`. The shim calls `InstalledVersions::reload`
   and sets the private `selfDir` and `installedIsLocalDir`, as Composer
   does. If PHP is not running, Go records only the latest data (which
   then wins over the first point's at boot: `boot`'s `ivPending`).
   `composer.Runtime.SetInstalledVersionsSink` receives the data with the
   repository's directory, the selfDir.

**The PHP side** (`Maestro\Shim\Plugins`): `plugin.load` gets the
manager (the PHP PluginManager mirror, whose composer, io, globalComposer,
disablePlugins and runningInGlobalDir properties Go fills), the package,
the classes, the loader (`{vendorDir, psr0, psr4, classmap}`: the
autoloads Go parsed and the class map Go scanned with createLoader's
warnings) and the `files` to require; it returns the registered objects.

### 5.5 Event dispatch

`internal/eventdispatcher` ports `EventDispatcher.php` completely in Go. The
listener table `listeners[event][priority][]` holds Go `Listener` values of
three kinds:

- a script string;
- a Go func (for Go-internal listeners, if any);
- a PHP callable handle with its display name, computed in PHP at
  registration: `get_class($obj).'->'.$method`, or `Class::method` for a
  static method passed as a string.

Ordering is exactly Composer's: `krsort` of the priorities, and script
listeners appended to priority 0 after the PHP listeners. That is how flex's
plugin listener stops `auto-scripts` before the script runs.

From PHP:

- `addListener(name, callable|string, prio)` and `addSubscriber(obj)` (which
  expands `getSubscribedEvents()` in PHP and so honours symfony/runtime's
  `activate` ordering) become `ed.addListener` RPCs.
- `removeListener($obj|$callable)` becomes `ed.removeListener` with the
  handle. Go removes callables whose handle matches, or whose object handle
  matches when an object is passed.

**Dispatch loop** (`doDispatch`, Go). For each listener in order:

| Listener | Go action |
| --- | --- |
| PHP callable | `makeAutoloader` step (below), then `listener.call {h, event}`. PHP checks `is_callable`. If it is not callable, Go throws `RuntimeException("Subscriber X::m for event E is not callable, make sure the function is defined and public")`. Before the call, Go writes the VERBOSE line `> event: Class->method`. The return value `false` maps to 1, anything else to 0. |
| `@script` / `@composer …` / `@php …` / `@putenv …` / shell | Pure Go, as ported. `@composer` runs `<php> <COMPOSER_BINARY> …`, which is the launcher stub (D13). |
| PHP-callable script `Class::method` | If `class` is in the **native table**, run it in Go and start no PHP. The table holds exactly the static methods of Composer's own classes that only change Composer state; today that is `Composer\Config::disableProcessTimeout`. Otherwise, `makeAutoloader` then `script.php {class, method, event}`. PHP returns `notAutoloadable`, `notCallable` or `ok + returnFalse`. Go prints the matching `Class … is not autoloadable` or `Method … is not callable` warning at QUIET. The `> Class::method` echo (`executeEventPhpScript`) is written by Go before the call. An exception becomes "Script %s handling the %s event terminated with an exception" and is rethrown. |
| Command-class script | `makeAutoloader`, then `script.commandClass {class, eventName, input, output}`. PHP checks `class_exists` and `is_a(Command)`, Go checks the reserved name (with Go's warnings), then PHP builds the vendored Symfony `Application` exactly as `EventDispatcher.php` lines 345–374 do and runs it. The output is the ConsoleIO's `$output` mirror. |

**The `before` hook.** Composer writes the "> ..." line after its PHP
checks (`class_exists`, `is_callable`) and right before the PHP code
runs, and checks the reserved event names of command classes there too.
So each of `listener.call`, `script.php` and `script.commandClass` calls
maestro back with `dispatch.before` once its checks passed; Go writes the
line (or vetoes the command class) there. An exception after that hook
was thrown by the PHP code ("Script %s handling the %s event terminated
with an exception"); PHP ending (exit(), a fatal error) is not reported,
as Composer's process ends right there.

**PHP callables** are registered by handle: a closure or invokable object
as itself, an array callable `[$objOrClass, 'm']` as a holder object per
distinct value (equal arrays share it, so `removeListener($callable)`
matches as `===` does), with the object's handle for
`removeListener($object)`.

**`makeAutoloader`.** This is a Go port: the callable key, the
`previousListeners` set, the hash over `name/version` of the canonical
local packages plus `/dev`, and `setDevMode`. When the hash changes, Go
computes the package map and parse result and sends `autoload.install`
with the loader contents. PHP unregisters the previous dispatcher loader,
creates a new `ClassLoader` and calls `register(false)`. It does not require
`files`, matching `createLoader`.

**The `finally` block.** Composer snapshots `spl_autoload_functions()`
before the listeners and re-appends prepended autoloaders after them. Go
brackets each dispatch that called PHP at least once with
`dispatch.begin {depth}` (sent lazily, just before the first PHP call) and
`dispatch.end {depth}`. PHP runs the same algorithm on its own autoloader
stack. Nested dispatches nest.

**Other dispatcher logic.** Event-stack circular detection
(`Circular call to script handler '%s' detected`),
`ensureBinDirIsInPath`, `COMPOSER_SKIP_SCRIPTS` and `COMPOSER_DEBUG_EVENTS`
are all Go, and any env changes reach PHP through sync.

**Event objects.** Go creates the event. On the first PHP listener of a
dispatch, Go sends a full event mirror: class, name, args, flags, and the
typed fields (composer and IO handles, devMode, localRepo and operation
handles, the operations list, transaction, packages, …). The same PHP event
object is reused for every PHP listener of that dispatch, and for nested
dispatches that pass the same event. After each call, PHP's dirty event
fields (`propagationStopped`, PrePoolCreate `packages` and
`unacceptableFixedPackages`, PreFileDownload `processedUrl`,
`customCacheKey` and `transportOptions`, and so on) come back in the sync
block. Go checks `isPropagationStopped` after each listener, as Composer
does.

**Events created in PHP.** `dispatch(null, $patchEvent)` and
`dispatchScript('custom')` are RPCs. A PHP-owned event (a user subclass) is
dispatched by Go with the event handle. PHP listeners get the original
object, and script listeners see its `getName`, `getArguments` and
`getFlags` (fetched once). A `ScriptEvent` for `@script` references inside
it is created in Go, with `originatingEvent` pointing at the PHP event
handle.

### 5.6 Custom installers

`IM::addInstaller($installer)` (RPC `im.addInstaller`) sends:

- the installer handle;
- its class;
- its **base kind**: the nearest shim base, one of `library`, `plugin`,
  `metapackage`, `noop`, `project`, or `interface` for a direct
  `InstallerInterface` implementation (magento);
- its **override set**: the names of the `InstallerInterface` and
  base-class methods whose declaring class (`ReflectionMethod::getDeclaringClass`)
  is not a shim class.

Go wraps it in a `proxyInstaller` and prepends it, which resets the
per-type cache, as Composer does.

`proxyInstaller` implements `installer.Installer` (§7). For each method:

- if the method is in the override set, or the base kind is `interface`, it
  calls `installer.call {h, method, args}` into PHP;
- otherwise it calls the Go base implementation (`installer.LibraryInstaller`
  etc.). Its *virtual hooks* (`Supports`, `IsInstalled`, `GetInstallPath`,
  `GetPackageBasePath`, `InstallCode`, `UpdateCode`, `RemoveCode`,
  `EnsureBinariesPresence`) dispatch back through the same proxy. When
  `getInstallPath` is overridden, Go's native install asks PHP for the path.

**Inherited methods called from PHP.** The PHP base classes
(`LibraryInstaller`, etc.) are real classes:

- The constructor sets Composer's protected properties and creates a Go peer
  through RPC `installer.new {kind, h, io, composer, type, …}`. Go builds the
  base installer with the vtable pointing to this handle, and a
  provisional override set (reflection is done in the constructor too).
- An inherited method called from PHP (`parent::install($repo, $package)`)
  is RPC `installer.base {h, method, args}`, which runs the Go base
  implementation for that peer.

Calls therefore go PHP → Go (base) → PHP (override hook) → …. That is
exactly PHP's virtual dispatch.

**Promises.** Methods that return `?PromiseInterface` in Composer:

- When Go calls PHP, the PHP result is either `null` or a promise. PHP hands
  the promise over as `{id, s}` (its state: fulfilled, rejected or
  pending, `Maestro\Shim\Promises::watch`); Go stands in for it with a
  promise of its own. A pending one settles when PHP reports it
  (`promise.settled {id, ok}`), which runs maestro's continuations (the
  InstallationManager's execute-operation → cleanup → write chain) right
  there, as React runs them while the promise settles.
- When PHP calls a Go base method, Go returns its promise the same way:
  settled ones become `\React\Promise\resolve()`/`reject()`, a pending one a
  `Deferred` that Go settles (`promise.settle {id, ok}`) on the goroutine
  settling its promise (the one driving the event loop, which holds the
  PHP baton). `parent::uninstall()->then(cb)` thus runs `cb` when the
  removal finishes, after the "Removing" lines of the other operations of
  the batch, exactly as in Composer.
- Rejection reasons cross as exceptions: the side that needs one calls the
  other (`promise.reason` into PHP, `promise.rejection` into Go), which
  throws it, so a PHP exception keeps its identity (D12).

**Ordering within a batch.** InstallationManager's Go port calls `download`
for every operation in order, then `prepare` → op → `cleanup` → `write` per
operation in order, exactly as `executeBatch` sequences them. Only the work
inside native operations (HTTP, store import) runs in parallel. Operations
whose installer is a PHP proxy with an overridden `download`, `prepare`,
`install`, `update`, `uninstall` or `cleanup` are executed synchronously, in
operation order, on the main goroutine. Lines such as
`  - Installing x (v): Extracting archive` are written in op order in all
cases, as Composer writes them when an install is started (§5.14).

**PluginInstaller subclass** (mnsami). The Go `PluginInstaller` base
performs `registerPackage`, `deactivatePackage`, `uninstallPackage` and the
rollback on failure through the Go plugin manager. That re-enters PHP for
`plugin.load`, so the inherited behaviour is real.

**Stateful installers** (wordpress-core-installer's static path map,
BitrixInstaller's `ask()` from `getInstallPath`) work naturally, because the
PHP object lives for the whole process and IO calls re-enter Go.

**Implementation.** `svc_installer.go`, `promises.go`,
`php/src/Maestro/Shim/Installers.php` and `Promises.php`, and the shim
classes `LibraryInstaller`, `PluginInstaller`, `MetapackageInstaller`,
`NoopInstaller`, `ProjectInstaller`, `BinaryInstaller`:

- The base constructors set Composer's properties in PHP (as Composer's
  constructors do, through the Config proxy) and call
  `installer.new {obj, describe(obj), params}`; Go builds the peer
  (`installer.NewLibraryInstaller` etc.) and gives it the proxy as its
  `installer.Virtuals`. `NoopInstaller` has no constructor: its peer is
  created on its first base call (or `addInstaller`). The override set is
  computed once, by reflection, in `Installers::describe`.
- Every other base method is `installer.base {obj, level, method, args}`;
  `level` is the shim class declaring it (`self::class`), so
  `parent::install()` in a PluginInstaller subclass runs Go's
  PluginInstaller and `LibraryInstaller::getInstallPath($p)` (oomphinc)
  runs LibraryInstaller's even on a PluginInstaller peer. The peer's
  `vendorDir` comes back with every result and is written into the PHP
  property (initializeVendorDir() resolves it in Go), and goes to PHP with
  every `installer.call`. `getDownloadManager()`, `getPluginManager()` and
  `disablePlugins()` are PHP, as in Composer.
- The proxy the InstallationManager holds is one value per PHP object
  (`removeInstaller` compares identities) and has the PHP object's
  interfaces: `BinaryPresenceInterface` for LibraryInstaller subclasses
  and interface implementations that declare it, `instanceof
  PluginInstaller` (`disablePlugins()`) for PluginInstaller subclasses.
  `im.getInstaller` returns the PHP object itself, or a proxy object of
  maestro's own installer's class.
- A `BinaryInstaller` created in PHP has a Go peer
  (`installer.newBinary`), which the LibraryInstaller peers it is passed
  to use; a subclass overriding `installBinaries()`/`removeBinaries()` is
  called in PHP instead.
- Legacy `composer-installer` plugins are added by `plugin.load` and
  removed again by `plugin.deactivate`/`plugin.uninstall` through
  `removeInstaller`, as PluginManager does.

### 5.7 Commands and Symfony Console

**Composer-side flow** (Go port of `Console\Application`). When
`mayNeedPluginCommand` is true and plugins are enabled, Go calls
`getPluginCommands()`:

1. `getComposer(false, false)` or `Factory::createGlobal(...)` loads the
   plugins.
2. Go calls PHP `capability.commands {composer, io}`. The PHP
   `PluginManager::getPluginCapabilities(CommandProvider::class, ['composer'=>…, 'io'=>…])`
   calls `getCommands()`, checks that the result is an array of
   `BaseCommand` (with Composer's exception texts), and returns a
   **command descriptor** for each command. The descriptor contains:
   - handle and class;
   - name, aliases, description, help, processed help;
   - hidden, enabled, isProxyCommand, `ignoreValidationErrors`;
   - usages and process title;
   - the native definition (each argument: name, mode, description,
     default; each option: name, shortcut, mode, description, default,
     negatable).
3. Go builds `proxyCommand` values (an embedded `*console.Command` plus a
   handle). It then applies Composer's "would override a Composer command"
   check and `addCommand`.

`list`, `help`, `--format=json|xml|md` and completion all run in Go from the
descriptor. `getSynopsis` and the merged application definition are computed
by the Go port, as for built-in commands.

Plugins that call `$app->add(new XCommand)` in `activate()` (flex, thanks)
go through the Application mirror: `app.add {cmd}`. PHP builds the
descriptor and Go registers the proxy.

**Running a plugin command.** `proxyCommand.Run(in, out)` sends
`command.run {h, tokens, interactive, decorated, verbosity, frames}`. PHP:

1. Builds `new ArgvInput(array_merge([argv0], tokens))` and sets
   interactive.
2. Gets the shim `ConsoleOutput` mirror bound to fds 1 and 2, with Go's
   verbosity, decoration and formatter styles (Composer's `warning` and
   `highlight` styles, as `Factory::createOutput` registers them).
3. Calls `$cmd->setApplication($appMirror)`, if not already set, and
   `$cmd->run($input, $output)`.

From there everything happens in vendored Symfony and the shim:
`mergeApplicationDefinition` (the Application mirror's `getDefinition()` is
Go's definition, mirrored once), bind and validate, `initialize` (the shim's
`BaseCommand::initialize`, ported from Composer, including
`PRE_COMMAND_RUN` dispatch through RPC and the `COMPOSER_*` env option
mapping), `interact` and `execute`. The return value or exception goes back
to Go, and Go's Application renders exceptions (§5.10).

**`BaseCommand` outside an Application.** pest calls
`(new DumpCommand)->setComposer($c)->run(new ArrayInput([]), new ConsoleOutput(...))`.
This is pure PHP: vendored Symfony runs it, and the output object writes to
fd 1 directly.

**Symfony Console: vendored, not reimplemented** (D6). The shim uses the
real symfony/console rather than a minimal PHP reimplementation of
Command/Input/Output, because:

- Plugin commands use the full surface: helpers, `Question`, `Table`,
  `ProgressBar`, `InputOption::VALUE_NEGATABLE`, `SymfonyStyle`,
  `setCode`, `getHelper('question')`, Closure binding on `ArgvInput`.
- Reimplementing it would duplicate symfony's behaviour without its tests.
- symfony/console 5.4 is MIT, supports PHP 7.2.5, and is what the phar
  ships, so `instanceof` checks and the exception classes are identical.

Cost: about 1.1 MB of embedded source, loaded lazily per class.

**Implementation.** `proxy_command.go`, `svc_console.go` and
`php/src/Maestro/Shim/Console.php` (with the Input, Output, Command and
Application adapters, `GoOutput`/`GoConsoleOutput` and `Definitions`), and
the shim classes `Composer\Console\Application`, `Console\Input\InputOption`,
`Console\Input\InputArgument`, `Question\StrictConfirmationQuestion`,
`IO\BufferIO` and `IO\ConsoleIO` (Composer's behaviour for IOs created in
PHP), `Factory::create`/`createComposer`/`createGlobal`/`createConfig`:

- A PHP command crosses as a description (`Console::describe`: class,
  `instanceof BaseCommand`, name, aliases, description, help, hidden,
  enabled, isProxyCommand, ignoreValidationErrors, process title, usages,
  native definition). Its proxy embeds `command.BaseCommand` for BaseCommand
  subclasses (`isProxyCommand`, telemetry), a plain `console.Command`
  otherwise; one proxy per PHP object. `command.run` attaches the command
  to the Application mirror, gives it the name and description maestro's
  Application gave it (script commands), and runs `$command->run($input,
  $output)`; `command.complete` runs `$command->complete()` on a
  `CompletionInput::fromTokens()` of maestro's tokens.
- The input of `command.run` (and of CommandEvent/PreCommandRunEvent) is
  maestro's own as a mirror: a vendored ArgvInput/StringInput/ArrayInput
  filled from maestro's state (definition, arguments, options,
  interactivity, tokens or parameters). PHP's changes go back while it stays
  bound to maestro's definition (a polled adapter, `PolledMirrorAdapter`),
  so a PRE_COMMAND_RUN listener's `setOption()` reaches maestro's command;
  once PHP rebinds it (Symfony's `Command::run`) it is PHP's. An input
  created in PHP and given to `run()`/`doRun()` is adopted as it crosses
  (the codec's PHP-born mirror path): maestro runs it and its binding shows
  in PHP's object, as in Composer.
- Outputs: maestro's console output on the inherited stdout/stderr is a
  vendored `ConsoleOutput` writing to fds 1 and 2 itself (D11); any other
  maestro output (a test's buffer) is a `Maestro\Shim\GoOutput`
  (`GoConsoleOutput` with an error output) whose formatted writes come to
  maestro (`output.write`). Their verbosity and decoration follow maestro's
  and PHP's `setVerbosity()`/`setDecorated()` go back (polled). An output
  created in PHP and given to maestro is a `phpOutput` proxy (`object.call`
  for `write` and the setters; its verbosity and decoration are cached,
  since maestro reads them while it syncs).
- Command-class scripts in events (`script.commandClass`) get the
  ConsoleIO's output that way (a new ConsoleOutput for any other IO), as
  EventDispatcher does.
- maestro's Application crosses as a `Composer\Console\Application` mirror
  constructed as Symfony's (name, version); `new Application()` in PHP
  creates a maestro Application on Setup's Factory (`app.new`). Symfony's
  own methods (find, has, all, get, add, getDefinition, setAutoExit, ...)
  stay PHP's: `getDefaultCommands()` is maestro's commands (`app.commands`:
  PHP commands as themselves, maestro's as instances of their classes
  filled with Symfony's Command properties), `getDefaultInputDefinition()`,
  `getHelp()` and `getLongVersion()` are Composer's, and `run()`/`doRun()`
  run maestro's Application (`app.run`/`app.doRun`, with the commands PHP
  added through `add()`); with auto-exit PHP exits with the code itself.
  maestro's own commands run directly from PHP (`$app->find('install')
  ->run($input, $output)`) run Symfony's `Command::run()` in PHP, as in
  Composer; the hooks it calls on them (`initialize()`, `interact()`,
  `execute()`, and the classes' own `run()`, `isProxyCommand()` and
  `complete()` overrides, generated into the stubs by tools/shimgen) are
  maestro's command's (`builtin.*`), on the input (adopted as it crosses,
  bound to the same definition) and output PHP passed, so a
  PRE_COMMAND_RUN listener's change shows in PHP's input object. Creating
  Composer's command classes in PHP (`new InstallCommand()`, a subclass
  of one) is not supported: their `configure()` stays a stub (maestro's
  commands are Go objects; only the instances its Applications hold have
  one behind them).
- Script commands: before looking for them, Composer's Application
  registers the root package's class loader (createLoader, with its
  warnings); internal/command calls `RegisterProjectLoader` for that, and
  the loader is registered in PHP (`autoload.register`) now or when PHP
  starts. `ScriptCommand` starts PHP only when it runs already or that
  loader finds a file for the class (ClassLoader::findFile, `loaderMayDefine`),
  then `command.script` does `class_exists()`, `is_subclass_of(Command)`,
  the SingleCommandApplication warning and `new $class($script)`.
- `getPluginCommands()` asks PHP only when PHP runs (no plugin was
  instantiated otherwise).
- The bundled packages: the shim registers a ClassLoader of its `lib/`
  (the phar's vendor directory) at boot, so `InstalledVersions` reports
  Composer's bundled packages first, as the phar's does (the functional
  fixtures installed-versions/installed-versions2 check it).

### 5.8 Where plugin mutations flow back

Because of D3 and D4, every mutation is visible to Go when the PHP call
returns. The table lists the points where Composer *reads* what plugins may
have changed. The Go ports must read the state at the same point, through
the Go object and never from a cached copy.

| Composer step | Plugins mutate | Go must (re)read after the dispatch |
| --- | --- | --- |
| `Factory::createComposer` → loadInstalledPlugins → `PluginEvents::INIT` | root package (merge-plugin), repositories (`RM::prependRepository`), installers | root package links, repositories, installer list |
| `BaseCommand::initialize` → `PRE_COMMAND_RUN`; command → `COMMAND` | `$input->setArgument/setOption` (laminas), ArgvInput tokens (flex, during `activate`) | the input's tokens, arguments and options (input mirror, §5.9) |
| `PRE_INSTALL_CMD` / `PRE_UPDATE_CMD` | root requires and devRequires, stability flags, aliases, references, extra, scripts, repositories (merge-plugin at priority 50000); uninstalls (cweagans 1) | everything the solver request is built from; the local repository |
| `PRE_POOL_CREATE` (PoolBuilder) | `setPackages`, `setUnacceptableFixedPackages` | the event's package handle lists, used as the pool input |
| `PRE_OPERATIONS_EXEC` | none observed (flex only reads) | the transaction |
| `PRE_PACKAGE_*` | the operation package (`setBinaries`, drupal), other uninstalls | the operation's package fields, before the installer runs |
| installer `getInstallPath` / `supports` (PHP installers) | n/a | called on demand; no caching beyond Composer's per-type installer cache |
| `POST_PACKAGE_*` | localRepo package `setExtra` (cweagans 1), files on disk | the localRepo package fields, before the next `$repo->write()` |
| `PRE_FILE_DOWNLOAD` | processed URL, cache key, transport options | the event fields, before the HTTP request is created |
| `PRE_AUTOLOAD_DUMP` (AutoloadGenerator::dump) | root autoload, devAutoload and `files` (typo3, cakephp, discovery, drupal), localRepo package autoload (mcaskill, spi) | the root and local packages, when building the package map |
| `POST_AUTOLOAD_DUMP`, `POST_INSTALL_CMD`, `POST_UPDATE_CMD` | composer.json and lock (flex, discovery), `setLocker`, localRepo add/remove/write, nested installs | Composer object getters (the locker may be replaced), local repository |
| any point | `putenv`, `chdir` | env and cwd (sync) before spawning any process or resolving relative paths |

### 5.9 Output, input, prompts

**IO.** The Go IO (`internal/io`) is the only `IOInterface` instance in
normal runs. Its PHP mirror is the shim class `Composer\IO\ConsoleIO`
(`BufferIO`/`NullIO` when Go's IO is that kind). Every `IOInterface` method
is RPC, except the verbosity, decoration and interactivity flags, which are
mirror fields. The mirror's protected properties match Composer's:

- `$input`: an `ArgvInput` mirror;
- `$output`: the shim `ConsoleOutput`, a subclass of vendored
  `Symfony\Component\Console\Output\ConsoleOutput` writing to fds 1 and 2,
  with Go's verbosity, decoration and the Composer formatter styles;
- `$helperSet`: a vendored `HelperSet` with `QuestionHelper` and the others
  Composer registers.

Those properties only exist for plugins that reach into them. The shim's own
IO methods never use them; they RPC. Verbosity changes made in PHP
(`$output->setVerbosity()`, vaimo) are synced to Go's output by the
ConsoleOutput mirror, which tracks its `verbosity` and `decorated` setters.

**Raw writes.** PHP `echo`, `fwrite(STDOUT)`, the Symfony outputs, and child
processes spawned by plugins all write to the inherited fds. Go guarantees
ordering by flushing every buffered writer it owns before any message to
PHP. PHP CLI output is unbuffered (`output_buffering=0`, `implicit_flush=1`),
and the shim calls `fflush(STDOUT)` and `fflush(STDERR)` before replying,
in case user code changed buffering. If the plugin left an `ob_start()`
buffer open, the shim does not flush it. That matches Composer, where such
output also stays in the buffer.

**Prompts.** `IOInterface::ask*` and `select` are RPC to Go, which renders
the question and reads stdin. Symfony `QuestionHelper` used directly by
plugin commands reads PHP's `STDIN`, which is the same terminal. To keep
stdin coherent between the two readers:

- Go never reads stdin ahead. Go's question reader reads unbuffered and
  stops at the newline, which `internal/console` must guarantee (§7).
- PHP's `STDIN` stream buffer only fills while PHP reads.
- On a TTY the line discipline delivers line by line, so both sides see
  exactly what Composer's single process would. Piped stdin is non-interactive
  under Composer's own rules (`posix_isatty`), except with
  `SHELL_INTERACTIVE`. That edge case is a documented risk (§10).

**Input mirror.** The PHP `ArgvInput` mirror is a real vendored `ArgvInput`.
Go sends its full state: tokens, parsed flag, definition, arguments,
options, interactive. PHP sets the private and protected properties with
`Closure::bind`. On return, PHP compares the state and sends it back if it
changed. That covers `setArgument`, `setOption`, `bind` and private
`$tokens` rewrites. Go applies the state to its `console.ArgvInput`, which
needs a state import and export API (§7).

### 5.10 Exceptions and exit codes

**PHP → Go.** Every request handler in the shim catches `\Throwable` at the
RPC boundary and returns an `err` message (§6.2) with:

- `class`, and `classes` (the class chain plus interfaces, for
  `instanceof` mapping in Go);
- `message`, `code`, `file`, `line`, `trace` (frames: file, line, class,
  type, function);
- `previous` (recursive);
- the exception's PHP handle `h`;
- typed extras for known classes: `TransportException`
  (statusCode, headers, response), `ScriptExecutionException`,
  `PluginBlockedException`, `JsonValidationException` (errors),
  `ParsingException` (details).

Go turns it into `*plugin.PHPException`, which implements
`phperr.Exception` (its `instanceof` answered from the class list PHP
sent) and `console.Tracer`, so the Go Application renders and catches it
as any other exception. Composer-known classes map to
the Go error types the ports use (`errors.As` targets):

- `ScriptExecutionException` → `eventdispatcher.ScriptExecutionError`
  (whose exit code Composer returns);
- `TransportException` → `util.TransportError`, with its code overridden
  like `Application::doRun` does;
- `ParsingException` → its `GithubActionError` emission;
- `PluginBlockedException`, `JsonValidationException`, and
  `InvalidArgumentException` / `CommandNotFoundException` for Application
  lookup.

`\Error` subclasses render as Composer does for symfony < 6.4: the
`renderThrowable` path with `max(1, code)`. `exit()` and fatal errors follow
§5.2.

**Go → PHP.** When Go returns an error to a PHP caller:

- if it is a `*PHPException` with a handle, PHP rethrows the original
  object (D12);
- otherwise PHP constructs the mapped class (`phperr.ClassOf`: the
  class the error stands for, else `\RuntimeException`) with the same
  message, code and previous chain. It is built without its constructor
  (`ReflectionClass::newInstanceWithoutConstructor`, then the properties set
  by reflection), because Composer's exception classes take other
  constructor arguments and stubs throw. internal/util's error types map to
  their PHP classes (`RuntimeError` → `\RuntimeException`, `TransportError`
  → `TransportException` with its code, ...). A Go error that wraps a PHP
  exception becomes a `\RuntimeException` whose previous is that very
  object.

Go-raised exceptions carry no throw site or trace: the shim gives
them the location and the PHP stack they are thrown into (§5.12). A PHP
exception going back to PHP carries its own trace.

**Exit codes.** These follow Composer's rules, all of which are in Go:
`ScriptExecutionException` code, Symfony's `code <= 0 → 1`, a 255 cap, and
`Installer::ERROR_*`. The child's own exit status matters only for
`exit()`, fatal errors and shutdown functions (§5.2).

### 5.11 Nested Composer, Installer and Application runs

All of these are re-entrant calls from PHP into Go, executed while Go is
still inside the outer step. For example, merge-plugin's
`Installer::create(...)->run()` runs from inside POST_UPDATE_CMD dispatch.

- `Factory::create`, `createGlobal` and `createComposer` →
  `factory.create {io, localConfig, disablePlugins, disableScripts, cwd, fullLoad}`.
  Go builds a new Composer with its own Config, managers, dispatcher and
  plugin manager. Its plugin loading calls `plugin.load` in the same child.
  Classes already loaded get the `_composer_tmpN` rename, as in Composer.
  Go returns the Composer handle.
- `Installer::create($io, $composer)` → PHP builder. `->run()` →
  `installer.run {composer, io, settings}`. Go runs `composer.Installer.Run`
  and returns its exit code. Exceptions propagate.
- `new Console\Application()` → `app.new` creates a fresh Go Application
  peer, with no plugins loaded until it runs. `->run($input, $output)` /
  `->doRun()` → `app.run {app, input, output}`:
  - The input is described by kind: `argv` tokens, an `ArrayInput`
    parameter array, or a `StringInput` string. Go builds the Go equivalent.
  - The output is the shim ConsoleOutput, in which case Go uses its own
    console output with the PHP object's verbosity and decoration, or any
    other PHP `OutputInterface`. In that case Go wraps it in
    `proxyOutput`, which forwards `write(messages, newline, options)`
    unformatted to the PHP object, so PHP's own formatter and verbosity
    filtering apply.
  - `setAutoExit(false)` and `setCatchExceptions` are recorded on the Go
    peer before running. With auto-exit (the Symfony default), Go performs
    the PHP-side `exit($code)` by replying `{exit: code}`, and the shim then
    calls `exit($code)` in PHP. This reproduces laminas' in-process
    `update` ending the process.
- `getApplication()->run(...)` inside a plugin command (cweagans 2) targets
  the running Go Application mirror. It is the same call, on the existing
  app.

**Re-entrancy requirements for the Go ports.** `composer.Installer`,
`Factory`, `Application`, the solver, the InstallationManager and the
dispatcher must not use package-level mutable state. `Composer`'s static
`runningCommand` and `runningOperation` live in a per-process struct that
is saved and restored exactly as PHP statics behave (§7).

### 5.12 Internals emulation (tier 6)

**Backtrace frames.** flex and php-http/discovery inspect
`debug_backtrace()` for objects that are on Composer's PHP stack. Every Go →
PHP call that can run plugin code (`plugin.load`, `listener.call`,
`script.*`, `installer.call`, `command.run`) carries `frames`: an
outermost-first list of `[object, args, function]` for the method calls
that would be on Composer's stack at that point, `function` named as PHP's
backtrace names it ("Composer\Installer->run"):

- the Application's `run()` (no arguments from bin/composer, the input
  and output of a nested run) and Symfony's `run()` below it, Composer's
  and Symfony's `doRun($input, $output)`, `doRunCommand()`, and the
  running command's `run()`, `initialize()`, `interact()` and `execute()`
  (the console's `CallOn` tells maestro's Application, which pushes them);
- the Application's `getComposer()`, with the arguments of its call site
  (BaseCommand's three, one or two from the Application itself; none when
  PHP code called it, whose frame is real), and its private
  `getPluginCommands()`;
- the `Composer\Installer`'s `run()`, `doUpdate()` and `doInstall()`;
- the PluginManager's `loadInstalledPlugins()`, `loadRepository()` and
  `registerPackage()` while a plugin loads (`addPlugin()` is the shim's own
  PHP code, so its frame is real).

Go maintains the frame stack (composer.Runtime); every call into PHP code
carries the frames pushed since PHP last called maestro.

The shim enters each frame by calling Composer's method itself on the
frame's object with its arguments (a closure of the method, so no virtual
dispatch). The shim's implementation of that method starts with
`Frames::resumes($this, __FUNCTION__)`, which runs the rest of the call
there (the inner frames, then the handler) instead of the method's work:
`debug_backtrace()` shows Composer's `class`, `function`, `object` and
`args`. The methods entered are the shim's `Application::run()`/`doRun()`/
`getComposer()`/`getPluginCommands()` (private), `Installer::run()`/
`doUpdate()`/`doInstall()`, `PluginManager::loadInstalledPlugins()`/
`registerPackage()`/`loadRepository()` (private), the hooks of maestro's
own commands (`Console::builtin()`), and the bundled Symfony Console's
`Application::run()`/`doRun()`/`doRunCommand()` and `Command::run()`.

The Console is Composer's vendor/ copy, line for line, which traces name
as Composer's files, so its files stay as they are on disk:
`Maestro\Shim\SymfonyHooks` requires `Application.php` and
`Command/Command.php` (when the shim's autoloader loads their classes)
through a `file` stream wrapper that hands PHP their code with the
`Frames::resumes()` call after the opening brace of those methods, on the
brace's line. `__FILE__`, the lines and the rest of the code are the
files'; the wrapper is PHP's own again as soon as the file is open, and a
file that does not have the methods as expected is required as it is.
This works with opcache (its file cache included). Every frame PHP shows
is one of Composer's; the ones maestro does not push (the EventDispatcher's,
a command's own helpers such as RequireCommand's `doUpdate()`, Factory's)
are not there. plugin-internals compares the console's, the
PluginManager's and the Installer's frames with Composer's.

**Exception traces.** An exception's trace is the PHP stack, without the
shim's machinery (`Maestro\Shim\Traces`). An exception PHP code throws
goes to maestro with its frames down to the shim's call into that code,
which keeps the shim's location; -v shows that trace (`console.Tracer`).
Thrown back into PHP (a Go error, or a PHP exception passing through
maestro), an exception's trace is its own frames, if any, then the PHP
stack it is thrown into. maestro records no frames or throw sites of
Composer's for Go errors (errors render through internal/ui, and nothing
reads them). Files PHP code sees are named as Composer's stack
names them, under the root maestro sends at boot (`composerRoot`,
`phperr.Root`): the bundled libraries are Composer's vendor/ files line
for line; the shim's Composer classes name Composer's line where they call
plugin code with an `@line N` comment (`@line path:N` for the eval() of an
already loaded plugin class, "PluginManager.php(305) : eval()'d code"),
and keep their own location elsewhere; so does an error PHP raises there
(ErrorHandler names the `\ErrorException`'s location as Composer's).

**Deprecation notices and warnings.** Composer has one ErrorHandler: its
`$hasShownDeprecationNotice` is a static the sync engine keeps in step
with internal/util's (`util.DeprecationNoticeShown`), so below -v a
notice of plugin code after one of maestro's is hidden, as in Composer.
How a notice looks is maestro's (docs/PORTING.md "The contract"): the
shim's ErrorHandler decides what to report, as Composer's does, and
hands it to maestro (`ui.diagnostic` [io, kind, message], kind
`deprecation`, `note` or `warning`), which renders it with internal/ui
("Deprecated: <message>", "Note: More deprecation notices were
hidden, ...") on the IO's error output, decorated by that output's rules,
whether the IO is maestro's or one created in PHP. Composer's source
location and "Stack trace:" are not shown; the `\ErrorException`s the
handler throws keep Composer's class and location (D12).
plugin-runtime's `run-script deprecation` (with and without -v) compares
the notices' texts with Composer's.

**Asynchronous processes.** The processes PHP code starts with
`$loop->getProcessExecutor()->executeAsync()` run in PHP (Symfony
Process, as in Composer). Composer has one loop, whose `wait()` polls them
with its downloads whoever waits: the PHP executor of a loop is a job
source of maestro's `http.Loop` (`loop.phpProcessExecutor`), which
maestro's waits poll (`countActiveJobs()`, `object.call`) while
`executeAsync()` said it has jobs (`proc.asyncStarted`), count in the
progress bar and end with what a poll throws (a `ProcessTimedOutException`).
The executor `getProcessExecutor()` gives PHP writes to the IO of maestro's
loop's executor, as Composer's one executor does.

A ProcessExecutor PHP code gives one of maestro's services (`new
FileDownloader(..., $filesystem, $process)`, `new RepositoryManager()`,
`new Locker()`, `new EventDispatcher()`, `createDownloadManager()`) is
honoured: maestro's service gets an executor standing for it
(`processExecutorOf`, one per object): for a loop's executor, that loop's
own (a loop created in PHP gets one on its scheduler), whose `wait()`
drives the asynchronous jobs, so a FileDownloader created in PHP with it
removes packages (`Filesystem::removeDirectoryAsync()`); for any other,
one on the same IO, asynchronous after `enableAsync()` (`proc.describe`).
A Filesystem given runs on its executor (`fs.describe`). The two
executors share the loop but not `maxJobs`.

**`Composer\Installer` mirror.** This is a Go-owned service proxy whose
setters are RPC to the live Go Installer
(`setSuggestedPackagesReporter(new SuggestedPackagesReporter(new NullIO))`
silences suggestions). For discovery's `clone $installer;
$installer->__construct(io, package, dm, rm, locker, im, ed, ag, ...)`:

- `__clone` creates a new Go Installer peer through RPC (`installer.clone`);
- `__construct` on an existing peer re-initialises it from the passed
  handles;
- the protected properties that discovery reads through `(array)` casts
  (`platformRequirementFilter`, and EventDispatcher's `runScripts`) are
  declared protected and kept up to date by the mirror, so the array-cast
  keys `"\0*\0name"` exist with the right values.

The rule: for every protected or private property a surveyed plugin reads
by cast or reflection, the shim class declares it with the same name and
visibility and fills it from Go. The full list for v1:

- `Installer::$platformRequirementFilter`
- `EventDispatcher::$runScripts`
- `Config::$baseDir`
- `ConsoleIO::$input`, `$output`, `$helperSet`
- `ArgvInput::$tokens`
- `Transaction::$resultPackageMap`, `$presentPackages`

**`Closure::bind` into Transaction.** This works because the private
properties exist and are filled. `new Transaction(present, resultMap)` runs
the Go algorithm through RPC and fills `$operations`.

### 5.13 Flags, allow-plugins, API version, root

**`--no-plugins`.** Handled in Go (`disablePluginsByDefault`, the
`PluginManager` disable modes `true`/`'local'`/`'global'`). PHP is never
started for plugins. Script listeners still run, including PHP-callable
scripts.

**`--no-scripts`.** This sets `setRunScripts(false)` on the dispatcher. It
filters script listeners only; plugin listeners still run. That is a common
mistake, so keep it exact.

**`allow-plugins`.** Go only (`Manager.isPluginAllowed`). This includes the
interactive prompt, persistence through `JsonConfigSource` and `Config.Merge`,
`Too many failed prompts, aborting.`, and the `PluginBlockedException` text.

**Plugin API version.** This is Go's `PluginAPIVersion = "2.9.0"`, also
emitted as the `composer-plugin-api` platform package. The shim's
`PluginInterface::PLUGIN_API_VERSION` constant must equal it. A test pins
both.

**Root user.** The `isRunningAsRoot`, `COMPOSER_ALLOW_SUPERUSER`, `sudo -K`
and non-interactive auto-disable logic is in Go's Application, before any
PHP starts.

**Global plugins.** The global Composer is created by Go
(`createGlobalComposer`). Its plugins load through the same `plugin.load`
call with `isGlobal: true`, after the local ones, as `loadInstalledPlugins`
orders them.

### 5.14 Concurrency rules (Go side)

- Only the goroutine that currently holds the **PHP baton** may send a
  request. The baton is a mutex owned by `plugin.Runtime`. Every RPC entry
  point asserts that it is called on the baton-holding goroutine. The
  baton-holding goroutine is the main flow, plus goroutines spawned by
  handlers of PHP → Go calls, which inherit it explicitly.
  Implementation (`rpc.Conn`): a top-level call takes the free baton for its
  goroutine (by goroutine id); calls nest on that goroutine; a call from any
  other goroutine meanwhile fails with `rpc.ErrBaton` instead of corrupting
  the stack; `Runtime.Delegate(fn)` lends the baton to the goroutine running
  `fn`.
- Parallel work that must reach an IO created in PHP (a process's output
  written through a ProcessExecutor's IO, a download's debug lines, a
  signal handler's line) does so through `rpc.Conn.Run`: at once on the
  holder, on the calling goroutine when the baton is free (a guest, which
  other calls wait for instead of failing with `ErrBaton`), or posted to
  the holder, which makes the posted calls in order before its next call,
  before it gives the baton up, and while it waits for parallel work
  (util's wait hooks, run by the Scheduler's and Process's waits and
  `util.WaitServing`). A call whose result does not matter (a write) does
  not wait; one that returns a value (`isDebug()`, `ask()`) waits for the
  holder, so a wait of the holder for such work must serve the hooks.
  `util.Process` runs its output callback on the goroutine waiting for the
  process, as Symfony's `wait()` does. Such an IO is `io.Foreign`: the
  shortcuts maestro takes on its own IOs that skip or reorder calls are not
  taken (the install notifications are sent and waited for as Composer
  does). Platform requirement filters written in PHP are only called on
  the main flow.
- Parallel Go work (HTTP downloads, store imports, classmap scans) must
  never call plugin code otherwise. Every place where Composer would call
  plugin code during a "parallel" phase calls it synchronously in
  Composer's order on the main flow, and only then starts the parallel
  part:
  - `PRE_FILE_DOWNLOAD` and `POST_FILE_DOWNLOAD` events: Composer dispatches
    PRE when a download is queued and POST in the promise callback, at loop
    wait. Go dispatches POST in queue order after waiting, which is
    Composer's order whenever the downloads were started in that order.
    A cache hit's promise is already resolved, so its POST fires at once,
    right after its PRE, the store hit included; nothing of the package is
    materialized before it (no tree made ahead while a POST listener
    waits), and a file the listener changed is what gets extracted,
    without the shared store. Document any divergence.
  - `getInstallPath` and `supports` for PHP installers.
  - PHP installer `download`, `prepare`, `install`, `update`, `uninstall`
    and `cleanup`.

### 5.15 Security

- PHP starts only when an allowed plugin is about to be instantiated, or a
  script that the root package itself declares runs. A non-allowed plugin's
  code is never loaded: not its autoloader and not its `files`. This is the
  same guarantee Composer gives, enforced in Go before PHP exists.
- The IPC channel exposes nothing beyond what Composer exposes in-process.
  Plugins are fully trusted code once allowed, exactly as in Composer.
- On Unix, fds 3 and 4 are inherited by the child. PHP cannot set
  `FD_CLOEXEC` on them, so grandchildren spawned by plugins inherit them too.
  Go never relies on EOF to detect child exit; it watches the process.
  Grandchildren that write to fd 4 can corrupt the channel. No surveyed
  plugin does that, and the risk is noted.
- On Windows the loopback socket requires the 256-bit token as the first
  frame. The listener accepts exactly one connection and then closes.
- The shim directory in the cache is created `0700` and its files are made
  read-only at extraction (§5.1). Its manifest is compared with the embedded
  one on every start.

### 5.16 Startup cost

Budgets, checked by `internal/plugin`'s runtime tests (a miss fails them
only with `MAESTRO_PERF_BUDGETS=1`, as wall time depends on machine load):

- under 40 ms for starting the child and its handshake;
- under 2 ms per trivial round trip.

How the design meets them:

- **Lazy loading.** The shim classmap autoloader loads only the classes
  used. A typical tier-2 run loads about 25 shim files and no Symfony
  Console.
- **One child.** There is no per-event spawn.
- **Batching.** A plugin load is one round trip. The event mirror is sent
  once per dispatch. Full package snapshots for the local repository are sent
  in one `repo.packages` reply.
- **No opcache tuning** (D14).

---

## 6. IPC message spec

### 6.1 Transport and framing

- **Unix.** Go creates two `os.Pipe()` pairs and passes the child's ends as
  `ExtraFiles`. Child fd 3 is Go → PHP (PHP reads); child fd 4 is PHP → Go
  (PHP writes). PHP opens `php://fd/3` with `'rb'` and `php://fd/4` with
  `'wb'`.
- **Windows.** Go listens on `127.0.0.1:0` and passes `tcp:<port>` and the
  token. PHP connects with `stream_socket_client` and sends a `hello` frame
  that includes the token.
- **Frame.** A 4-byte unsigned big-endian length N (1 ≤ N ≤ 2^30), followed
  by N bytes of UTF-8 JSON encoding one message object.
- **Writes.** Each side writes a frame with a single buffered write and
  flushes. PHP loops `fwrite` until all bytes are written, and reads with
  `fread` loops until N bytes are read.

### 6.2 Messages

```json
{"k":"call","id":17,"m":"listener.call","a":{...},"s":{...}}
{"k":"ret","id":17,"v":<value>,"s":{...}}
{"k":"err","id":17,"x":<exception>,"s":{...}}
```

- **`id`.** Each side numbers its own calls from 1, ascending. A `ret` or
  `err` must answer the innermost outstanding call of the *other* side. A
  mismatch is a protocol error: Go kills the child and fails with an
  internal error.
- **`s`.** Optional on every message (§6.3).
- **Waiting.** While waiting for `ret` or `err`, each side serves incoming
  `call`s (re-entrancy). That is the only concurrency, and it is strictly
  stack-shaped.
- **`exit`.** A `ret` may carry `"exit": <int>` instead of `v`. It is
  allowed only in reply to `app.run` and `command.run` when the Symfony
  Application had auto-exit enabled. PHP then calls `exit(int)`.

### 6.3 Sync block `s`

```json
{
  "o":   [ {"h": 12, "r": 7, "f": {"autoload": {...}, "extra": {...}}},
           {"h": 40, "r": 1, "full": {...snapshot...}} ],
  "reg": [ {"tmp": -9, "h": 311} ],
  "env": {"set": {"TYPO3_PATH_ROOT": "/p/public"}, "unset": ["FOO"]},
  "cwd": "/p/vendor-bin/tools",
  "st":  {"runningCommand": "update", "runningOperation": null, "processTimeout": 0},
  "io":  {"verbosity": 32, "decorated": true, "interactive": true}
}
```

- `o`: mirror updates. `r` is the sender's new revision, `f` holds the
  changed fields, and `full` replaces the whole snapshot.
- `reg`: handle assignments for PHP-born objects that Go registered while
  handling this call.
- All keys are optional.

The receiver applies the whole block before processing the rest of the
message.

### 6.4 Value encoding

PHP values map to JSON as follows. A **tag** is a JSON object whose first
key starts with U+0000.

| PHP value | JSON |
| --- | --- |
| `null`, `bool`, `string` (valid UTF-8) | same |
| `int` | JSON integer |
| finite `float` | JSON number that always contains `.` or `e` (PHP: `JSON_PRESERVE_ZERO_FRACTION` and `serialize_precision=-1`). Go decodes it as `float64`. |
| `INF`, `-INF`, `NAN` | `{"\u0000f":"INF"}` etc. |
| `string` with invalid UTF-8 | `{"\u0000b":"<base64>"}` |
| `array` that is a list | JSON array |
| other `array` | JSON object in key order. Int keys become decimal strings; both decoders coerce numeric-string keys back to ints, which is PHP semantics and `php.Array` semantics. |
| `array` whose first key starts with `"\0"`, or with any string key that is not UTF-8 | `{"\u0000e":[[k,v],…]}` (explicit pairs; a non-UTF-8 key is itself a `\u0000b` tag) |
| `stdClass` | `{"\u0000s":{…props…}}` |
| object, closure or callable array | `{"\u0000o":h, "c":"Class", "base":"Composer\\Package\\Package", "d":<snapshot>}` |

For objects, `c`, `base` and `d` are sent only the first time the receiver
sees `h`. `d` is present only for mirrors. A callable array `[obj, 'm']` is
an ordinary PHP array of a handle and a string.

Go decodes into `internal/php` values (`nil`, `bool`, `int64`, `float64`,
`string`, `*php.Array`, `*php.Object`). An `\u0000o` tag decodes to the Go
object itself for a Go handle, and to an `*rpc.PHPObject` (same pointer per
handle) for a PHP one; both implement `php.Opaque`, which lets a `*php.Array`
hold them. The encoder also accepts a bare `rpc.Handle`. A stdClass without
properties encodes as `{"\u0000s":[]}` (PHP's `json_encode([])`).

The shim encodes with:

```
json_encode($v, JSON_UNESCAPED_SLASHES|JSON_UNESCAPED_UNICODE|JSON_PRESERVE_ZERO_FRACTION|JSON_THROW_ON_ERROR)
```

It runs a pre-pass walker that converts objects, binary strings and
non-finite floats. The walker always runs; it is cheap compared with the
encode. Decoding uses `json_decode(…, true, 512 + depth headroom, JSON_BIGINT_AS_STRING|JSON_THROW_ON_ERROR)`,
then a post-pass that resolves tags.

**Constraint value** (inside snapshots and params):

```json
{"\u0000c":"constraint","op":">=","v":"1.0.0.0-dev","p":">=1.0"}
{"\u0000c":"multi","and":true,"cs":[...],"p":"^1.0"}
{"\u0000c":"all","p":"*"}
{"\u0000c":"none","p":null}
```

PHP rebuilds them with the vendored constructors and `setPrettyString`,
never by re-parsing, so the structure is exact. A user subclass of
`MultiConstraint` is encoded as `multi`.

**Link value:**

```json
{"\u0000c":"link","src":"a/b","tgt":"c/d","c":<constraint>,"d":"requires","pc":"^1.0"}
```

**Package snapshot `d`** (full tier):

- `id`;
- `name`, `prettyName`, `version`, `prettyVersion`, `releaseDate`
  (ISO 8601 or null);
- `type`, `targetDir`, `extra`;
- `binaries`, `installationSource`;
- `sourceType`, `sourceUrl`, `sourceReference`, `sourceMirrors`;
- `distType`, `distUrl`, `distReference`, `distSha1Checksum`, `distMirrors`;
- `transportOptions`;
- `requires`, `devRequires`, `conflicts`, `provides`, `replaces` (maps of
  link values);
- `suggests`, `autoload`, `devAutoload`, `includePaths`,
  `phpExt`, `isDefaultBranch`, `notificationUrl`;
- complete packages add `scripts`, `repositories`, `license`, `keywords`,
  `authors`, `description`, `homepage`, `support`, `funding`, `abandoned`,
  `archiveName`, `archiveExcludes`;
- root packages add `minimumStability`, `preferStable`, `stabilityFlags`,
  `config`, `references`, `aliases`;
- aliases carry `aliasOf` (a handle), `rootPackageAlias`,
  `hasSelfVersionRequires`;
- `repo` is the repository handle or null.

The field names are the PHP property names, so the shim's mirror base copies
them generically. Arrays are `php.Array` values (key order preserved).

**Exception `x`:** see §5.10. The shape is
`{"class","classes":[…],"message","code","file","line","trace":[…],"previous":x|null,"h":h|null,"extra":{…}}`.

### 6.5 Go → PHP methods

PHP serves these. `frames` is optional everywhere (§5.12).

| Method | Params | Returns |
| --- | --- | --- |
| `boot` | `argv`, `server` (`SCRIPT_NAME`, …), `io` (handle, kind, state; `null` for a runtime without an IO), `composerVersion` (`"Class::CONST" => value`, a self-test), `composerRoot` (the directory Composer's files are named under, §5.12), `terminal` (the lines and columns Symfony's Application puts in the environment), `ivPending` (InstalledVersions data waiting for PHP, §5.4), `require` (files to require last; tests). The cwd and statics come in the sync block: the child starts in maestro's cwd. | `null` |
| `shutdown` | `code` | never returns (the child exits) |
| `plugin.load` | `pm`, `package`, `classes[]`, `loader` (`{vendorDir, psr0, psr4, classmap}`), `files` (identifier => path), `isGlobal`, `legacyInstaller`, `failOnMissing`, `runningInGlobalDir` | `{registered: [object, …]}` |
| `plugin.deactivate` / `plugin.uninstall` | `pm`, `objects[]` | `null` |
| `capability.commands` | `composer`, `io` | `[descriptor…]` (§5.7) |
| `command.script` | `class`, `script`, `io` | descriptor, or `null` when the script is not a command class |
| `command.complete` | `command`, `app`, `tokens[]`, `index` | `{values: [...], options: [names]}` |
| `autoload.register` | `vendorDir`, `psr0`, `psr4`, `classmap` | `null` (a project class loader, `register(false)`) |
| `listener.call` | `h` (the callable or its holder), `event` | `{status: "ok"\|"notCallable", returnedFalse: bool}` |
| `script.php` | `class`, `method`, `event` | `{status: "ok"\|"notAutoloadable"\|"notCallable", returnedFalse: bool}` |
| `script.commandClass` | `class`, `event`, `input` (string), `output` (the ConsoleIO's output, or null) | `{status: "ok"\|"notAutoloadable"\|"notCommand"\|"skipped", code: int}` |
| `callable.invoke` | `callable`, `args[]` | value (a validator of `askAndValidate`) |
| `installer.call` | `h`, `method`, `args[]`, `vendorDir?`, `frames` | `{v: value}`, or `{p: {id, s}}` for a promise (§5.6) |
| `promise.settle` | `id`, `ok` | `null`: maestro's promise `id` settled; PHP settles its Deferred |
| `promise.reason` | `id` | throws the rejection reason of PHP's promise `id` |
| `command.run` | `command`, `app`, `input` (an input mirror), `output` (an output mirror), `name`, `description`, `frames` | `int` |
| `object.call` | `object`, `method`, `args[]` | value. A generic call on a PHP-owned object, used by Go proxies of PHP outputs (`write`, `setVerbosity`, `getVerbosity`, `setDecorated`, `isDecorated`), IOs, platform requirement filters, repositories and downloaders. Only methods of the interface the proxy implements may be called (and a FileDownloader subclass's protected hooks). |
| `downloader.describe` | `object` | `{parents: [class…], overrides: [method…]}` of a FileDownloader subclass written in PHP |
| `proc.describe` / `fs.describe` | `object` | `{io, async}` of a ProcessExecutor, `{executor}` of a Filesystem PHP code gives a service of maestro's (§5.12) |
| `autoload.install` | `vendorDir`, `psr0`, `psr4`, `classmap` | `null` |
| `dispatch.begin` / `dispatch.end` | `depth` | `null` |
| `iv.reload` | `data`, `selfDir?` | `null` |
| `ping` | none | `null` (handshake and health check in tests) |

### 6.6 PHP → Go methods

Go serves these. Grouped by namespace, with one Go handler file each. Params
always include the receiver handle `h` for instance methods. Names follow
`<area>.<phpMethodName>` so the mapping is mechanical: the shim proxy
method's body is literally `return $this->__rpc(__FUNCTION__, func_get_args());`.

| Namespace | Methods |
| --- | --- |
| `hello` | First frame from PHP: `{proto: 1, token?, phpVersion, phpBinary, sapi, pid}` |
| `dispatch.before` | The `before` hook of `listener.call`, `script.php` and `script.commandClass` (§5.5); returns whether to go on |
| `composer.*` | `get` (`what`: package\|config\|rm\|im\|dm\|ed\|pm\|locker\|ag\|loop\|archive), `set*`, `isGlobal` |
| `config.*` | `get` (key, flags), `all`, `raw`, `has`, `merge`, `getRepositories`, `getConfigSource`, `getAuthConfigSource`, `setConfigSource`, `prohibitUrlByConfig`, `disableProcessTimeout`, `getBaseDir` |
| `cfgsrc.*` | `JsonConfigSource` methods |
| `io.*` | every `IOInterface` method, plus `state` |
| `ed.*` | `addListener`, `removeListener`, `dispatch`, `dispatchScript`, `dispatchPackageEvent`, `dispatchInstallerEvent`, `hasEventListeners`, `setRunScripts`, `new` |
| `pm.*` | `registerPackage`, `deactivatePackage`, `uninstallPackage`, `isPluginAllowed`, `arePluginsDisabled`, `disablePlugins`, `getRegisteredPlugins`, `getGlobalComposer`, `setRunningInGlobalDir` |
| `pkg.*` | every setter of the package classes (`setExtra`, `setAutoload`, `replaceVersion`, …), `getRepository`, `getSourceUrls`, `getDistUrls`, `urls` (ComposerMirror URLs of a PHP-born package); `load` and `register` with the lazy tiers and PHP-born packages |
| `event.*` | `stopPropagation`, `setOriginatingEvent` |
| `repo.*` | `packages` (handles plus snapshots), `findPackage`, `findPackages`, `search`, `loadPackages`, `getProviders`, `addPackage`, `removePackage`, `write`, `reload`, `setDevPackageNames`, `getDevPackageNames`, `getDevMode`, `isFresh`, `getRepoName`, `count`, `new` (array/installed/composite/platform/root) |
| `rm.*` | `getLocalRepository`, `getRepositories`, `findPackage`, `findPackages`, `createRepository`, `addRepository`, `prependRepository`, `setRepositoryClass`, `setLocalRepository` |
| `im.*` | `getInstallPath`, `addInstaller`, `removeInstaller`, `getInstaller`, `isPackageInstalled`, `download`, `install`, `update`, `uninstall`, `markAliasInstalled`, `markAliasUninstalled`, `execute`, `reset`, `ensureBinariesPresence`, `disablePlugins`, `setOutputProgress`, `notifyInstalls` |
| `installer.*` | `new`, `base`, `newBinary`, `binary`, `determineBinaryCaller` (the installers, §5.6); `clone`, `run` (Composer\Installer), Installer setters |
| `promise.*` | `settled {id, ok}` (PHP's promise `id` settled), `rejection {id}` (throws the rejection reason of maestro's promise `id`) |
| `dm.*`, `downloader.*` | DownloadManager and downloader methods (promises are returned as handles) |
| `http.*` | HttpDownloader `get`, `add`, `copy`, `addCopy`, `wait`, `getOptions`, `setOptions`, `new`; RemoteFilesystem |
| `loop.*` | `wait` (promise handles), `getHttpDownloader`, `getProcessExecutor`, `new`; `processExecutorIO` (the IO of the loop's executor) |
| `ui.diagnostic` | `io`, `kind` (deprecation, note, warning), `message`: a notice or warning of the shim's ErrorHandler, rendered by internal/ui on the IO's error output (§5.12) |
| `proc.*` | `execute` (cmd string\|array, cwd, io, capture, tty: the PHP ProcessExecutor keeps its error output), `splitLines`, `escape`, `requiresGitDirEnv`; `executeAsync`; the timeout is a synced static |
| `fs.*` | every `Util\Filesystem` method (no receiver: maestro's Filesystem works on the shared working directory) |
| `json.*` | `new` (the maestro peer of a PHP JsonFile, which keeps read()'s indentation), `read`, `write`, `validateSchema`, `validateJsonSchema`, `validateSyntax`, `encode`, `parseJson`, `detectIndenting`, `manipulate` (method, contents, args) |
| `locker.*` | `isLocked`, `isFresh`, `getLockData`, `getLockedRepository`, `getContentHash`, `getPlatformRequirements`, `setLockData`, `new`, … |
| `ag.*` | `buildPackageMap`, `parseAutoloads`, `createLoader` (returns loader contents), `dump`, setters, `new` |
| `vp.*` | `parseNameVersionPairs`, `isUpgrade`, `normalizeStability` (the Composer-specific VersionParser methods) |
| `loader.*` | `ArrayLoader` load, `loadPackages`; `ArrayDumper` dump |
| `selector.*`, `reposet.*`, `transaction.*`, `pool.*` | tier 5–6 internals |
| `factory.*` | `create`, `createGlobal`, `createComposer`, `createConfig`, `getComposerFile`, `getLockFile`, `createRemoteFilesystem`, `createHttpDownloader` |
| `app.*` | `new`, `run`, `doRun`, `commands` (getDefaultCommands), `getComposer`, `resetComposer`, `getIO`, `getDisablePluginsByDefault`, `getDisableScriptsByDefault`, `getInitialWorkingDirectory` (Symfony's own methods stay PHP's, §5.7) |
| `output.*` | `write` (a GoOutput's formatted message), `errorOutput` |
| `cache.*` | `Cache` methods, `new` |
| `builtin.*` | `initialize`, `interact`, `execute`, `run`, `isProxyCommand`, `complete`: the hooks of maestro's own commands run from PHP (§5.7) |
| `policy.*` | `fromConfig`, `withBlockingDisabled` (BaseCommand::createPolicyConfig()) |
| `callable.go` | `[callable, args]`: calls the Go function a `Maestro\Shim\GoCallable` stands for |
| `request.new`, `config.new`, `rm.new`, `im.new`, `dm.new`, `pm.new` | Composer's services constructed in PHP, adopted |

The areas above also serve the protected methods a PHP
subclass calls on itself (`ed.doDispatch`, `ag.getPlatformCheck`,
`installer.doUpdate`, `downloader.getFileName`, `rfs.get`, …) and a few
more members (`io.enableDebugging`, `io.enableTimestamps`,
`fs.removeEdgeCases`, `factory.getHomeDir`, …); §8 "Beyond the
surveyed plugins" lists them.

Every method listed in §4 with Backing **RPC** has exactly one handler
here. An RPC method that is not registered returns `err` with class
`Maestro\Shim\UnsupportedApiException`. A unit test asserts that the handler
set equals the set of shim proxy methods.

---

## 7. Go-side interfaces needed from other packages

These are what `internal/plugin` relies on in other packages (PORTING.md
"Plugin requirements on every package" points here). `internal/plugin`
imports all of them, and none of them imports `internal/plugin`.

**`internal/php`**
- JSON encode and decode of `php.Array` with PHP key coercion.
- A decode hook so the plugin codec can intercept tag objects (`\u0000…`
  first key) without a second pass, or a public walker.

**`internal/pkg`** (Composer\Package)
- `Rev() uint64` on every package type, incremented by every setter.
- A setter for every field in the snapshot of §6.4, with Composer's
  semantics (`SetRepository` refuses a second repository).
- `Class() string`, giving the concrete PHP class name: Package,
  CompletePackage, RootPackage, AliasPackage, CompleteAliasPackage or
  RootAliasPackage.
- A constructor from a full snapshot (for PHP-born packages).
- `Link` and constraints from `internal/semver` must expose their structure
  (operator, version, pretty string, conjunctive, children) and be
  constructible from it.
- `Locker` and `ArrayLoader`/`ArrayDumper` APIs as in Composer.

**`internal/config`**
- `Get(key string, flags int) (any, error)` returning php values, and
  `All`, `Raw`, `Has`, `Merge(*php.Array, source string)`, `Repositories`,
  `ConfigSource`/`AuthConfigSource` (interface `ConfigSource` with
  Composer's methods), `BaseDir() string`, `DisableProcessTimeout()`.

**`internal/io`**
- An `IO` interface with every `IOInterface` method. The `validator` of
  `AskAndValidate` is a `func(any) (any, error)`, so the plugin package can
  pass a PHP-backed closure.
- `Kind()`: console, buffer or null.
- `Flush()`, which flushes everything Go has buffered to fds 1 and 2.
- The console question reader must read stdin unbuffered, up to and
  including the newline, and never ahead.

**`internal/console`**
- `Commander` is enough for proxy commands. They also need:
  - `Command.SetNativeDefinition`, or construction from a descriptor;
  - `ArgvInput.ExportState() InputState` and `ImportState(InputState)`
    covering tokens, parsed, definition, arguments, options, interactive.
- `Application.Add`, `Find` and `Has` must accept external commanders.
- `phperr.Exception` and `console.Tracer` (exist) are implemented by
  `*plugin.PHPException`.

**`internal/eventdispatcher`**
- `Listener`: a sum type of `Script(string)`, `GoFunc`, `PHPCallable{H Handle, Display string, ObjectH Handle}`.
- `AddListener(event, Listener, prio)`.
- `RemoveListener(match func(Listener) bool)`.
- `Dispatch(Event) (int, error)`, plus `DispatchScript`,
  `DispatchPackageEvent`, `DispatchInstallerEvent`, `HasEventListeners`,
  `SetRunScripts`, `RunScripts() bool`.
- An `Event` interface with `Name`, `Arguments`, `Flags`,
  `IsPropagationStopped`, `StopPropagation`, `Rev`, plus typed events
  mirroring Composer's classes, and `PHPEvent{H}` for PHP-owned events.
- `ScriptRuntime`, an interface defined here and implemented by
  `plugin.Runtime`, injected by `internal/composer`:

  ```
  CallListener(l PHPCallable, ev Event) (callable bool, ret any, err error)
  CallPHPScript(class, method string, ev Event) (status string, returnedFalse bool, err error)
  RunCommandClass(class string, ev Event, input string) (status string, code int, err error)
  InstallAutoloader(loader LoaderContents) error   // makeAutoloader
  DispatchBegin(depth int); DispatchEnd(depth int)
  ```

  When no runtime is injected (`--no-plugins` and no PHP scripts), the
  dispatcher must not need one.
- `ScriptExecutionError` carrying the exit code.

**`internal/installer`**
- `Installer` interface: `Supports(t) bool`, `IsInstalled(repo, p) bool`,
  `Download(p, prev) *util.Promise`, `Prepare(type, p, prev)`,
  `Install(repo, p)`, `Update(repo, initial, target)`, `Uninstall(repo, p)`,
  `Cleanup(type, p, prev)`, and `InstallPath(p) (string, bool)`, where false
  means `null`.
- `LibraryInstaller`, `PluginInstaller`, `MetapackageInstaller`,
  `NoopInstaller` and `ProjectInstaller` built with a **`Virtuals`
  interface** (`Supports`, `IsInstalled`, `InstallPath`, `PackageBasePath`,
  `InstallCode`, `UpdateCode`, `RemoveCode`, `EnsureBinariesPresence`). All
  internal calls to overridable methods go through it. The default
  `Virtuals` is the struct itself.
- `Manager` (InstallationManager): `AddInstaller` (prepend and reset the
  cache), `RemoveInstaller`, `Installer(type)`, `InstallPath`, `Execute`
  with the batch rules and ordering of §5.6/§5.14, and a hook to run
  operations of PHP-backed installers synchronously.
- `PluginManager` interface consumed by `PluginInstaller`:
  `RegisterPackage(p, failOnMissing, global) error`,
  `DeactivatePackage(p) error`, `UninstallPackage(p) error`.

**`internal/repository`**
- `RepositoryInterface` and `WritableRepository` with all Composer methods.
- `Rev()` on the local repository's package list, changed by
  add/remove/reload.
- A repository `Class()` for the PHP mirror class.
- `FilesystemRepository.Write` calls an `InstalledVersionsSink` hook (the
  plugin runtime) with the installed.php data.
- `PlatformRepository`, `InstalledRepository`, `CompositeRepository` and
  `RootPackageRepository` constructible from Go.
- `RepositoryFactory` functions.

**`internal/autoload`**
- `BuildPackageMap`, `ParseAutoloads`, `CreateLoaderContents` (psr-0,
  psr-4, classmap with scanning, files), `SetDevMode`, and `Dump`, with
  the PRE_AUTOLOAD_DUMP dispatch before the package map is built, as in
  Composer.

**`internal/composer`**
- `Composer` and `PartialComposer` structs satisfying a `plugin.Composer`
  interface (all getters and setters).
- `Factory.CreateComposer(io, localConfig, disablePlugins, cwd, fullLoad, disableScripts)`,
  `CreateGlobal`, `CreateConfig`.
- `Installer` (Composer\Installer) with all setters, `Run() (int, error)`,
  re-entrant, plus `Clone()`.
- `Statics`: `runningCommand` and `runningOperation`, a per-process struct.
- A frame-stack push and pop API used by Application, the command runner
  and `Installer.Run` (§5.12).

**`internal/command`**
- The Application port calls `plugin.Manager.PluginCommands(composer, io)`.
- `BaseCommand` exposes `Definition()` and its mirror class name for frames.
- Nested `app.run` support: constructing a fresh Application and running
  it with a given `console.Input`/`Output`.

**`internal/util`**
- `PhpBinary()` (shared with platform).
- `ProcessExecutor` with a static timeout getter and setter.
- `Filesystem`.
- `TransportError`.

---

## 8. Coverage by tier

The plugin API is grouped into six tiers by what a plugin needs (the
**Tier** columns of §2 and §4). Each tier is covered by e2e fixtures that
compare maestro with Composer 2.10.3 (§9.3) and by in-process tests.
Tiers 1 to 3 cover the plugins of most real projects, including a typical
Laravel application's pest, stubbedev and Laravel scripts.

**Tier 1: runtime and protocol.**
- Shim extraction and embed; `bootstrap.php` with the full `bin/composer`
  prologue.
- Transport (pipes and TCP), framing, codec (§6.4), handles, the
  re-entrant call stack, the sync engine (env, cwd, statics, mirror
  revisions), exceptions both ways (identity), the exit, fatal and
  shutdown paths, and the `COMPOSER_BINARY` launcher.
- `tools/shimvendor` (vendored libraries plus the drift test) and
  `tools/shimgen` (stubs plus the parity golden).
- Tests: codec goldens from PHP, a re-entrancy torture test (depth 50,
  alternating), exception identity, `exit()` mid-call, env/cwd sync.

**Tier 2: core plugin API.**
- The Go `PluginManager` port (policy) and the PHP `PluginManager`
  (mechanism, eval-rename).
- Composer, Config, IO and EventDispatcher proxies; Event, Script\Event and
  PackageEvent mirrors; the Package family, Link and constraint mirrors;
  the local repository mirror and the repository proxies' write methods;
  `IM::getInstallPath`.
- `Util\Filesystem`, `ProcessExecutor`, `Platform`; `JsonFile`;
  `InstalledVersions` reload; `AutoloadGenerator` (`buildPackageMap`,
  `parseAutoloads`, `createLoader`).
- PHP-callable scripts, command-class scripts (`script.commandClass`),
  `makeAutoloader`, the dispatch bracket, and the native
  `Config::disableProcessTimeout`.
- `allow-plugins` with the prompt; `--no-plugins` and `--no-scripts`.
- In-place mutation of root and local packages (`setAutoload`, `setExtra`,
  `setBinaries`).
- `BaseCommand` for commands run in PHP (pest's post-autoload-dump runs
  its command with `setComposer()`), the PHP-born events of `dispatch()`.
  PluginInstaller needs no PHP peer here: the Go PluginInstaller calls the
  Go manager.

Fixtures (`cmd/maestro/testdata/e2e/plugin-*`, run by `TestE2EPlugins` in
cmd/maestro/e2e_plugins_test.go): stubbedev, laravel, pest (event),
phpstan, infection, dealerdirect, captainhook, grumphp, symfony-runtime,
package-versions, spi, drupal (events), plus plugin-api, plugin-runtime and
plugin-global, which are local; the others come from Packagist at the
versions their committed composer.lock pins.

**Tier 3: custom installers.**
- `InstallerInterface`, the base classes (LibraryInstaller,
  PluginInstaller, MetapackageInstaller, ProjectInstaller, NoopInstaller,
  BinaryInstaller) and their Go peers (vtable, override sets).
- Promises (vendored react/promise) and the promise bridge (§5.6):
  pending promises stay pending on both sides, so callbacks of PHP
  installers run when Composer runs them. Three removals in one batch
  print their "Removing" lines, then the installer's callbacks, in the
  order the removals started, where Composer's is the order its `rm -rf`
  processes finish, so the e2e fixtures remove one package per step.
- `addInstaller`/`removeInstaller`; `im.download`/`install`/`update`/
  `uninstall`/`markAlias*`/`execute`/`reset`.
- `Autoload\ClassMapGenerator::createMap` and `dump`.

Fixtures: `plugin-installers` (composer/installers: Packagist
wordpress-plugins and path packages of drupal, cakephp, moodle, wordpress
theme and silverstripe types; installer-paths by vendor, type and name;
installer-name; binaries from a custom path; removals),
`plugin-installers-extender`, `plugin-custom-directory` (mnsami, with a
plugin installed and activated by its PluginInstaller subclass from a
custom path), `plugin-wordpress-core` and `plugin-yii2`.
`TestPlugins_Installers` (MAESTRO_PHP_TESTS=1) covers the interface and
MetapackageInstaller kinds, a legacy composer-installer, identity through
add/remove/getInstaller and a rejected promise.

**Tier 4: commands and Symfony Console** (§5.7).
- Vendored symfony/console; `BaseCommand` (with
  `createComposerInstance`, `getPreferredInstallOptions`,
  `formatRequirements`, `normalizeRequirements`, `renderTable`,
  `getTerminalWidth`) and built-in command mirrors.
- `CommandProvider` and `capability.commands`, with proxy commands in
  `list`, `help` and completion.
- `command.run`; the Application mirror (`find`, `has`, `add`, `all`,
  nested `run`); command-class scripts as commands.
- `CommandEvent` and `PreCommandRunEvent` with the input mirror;
  `BufferIO`; `Factory::create`, `createGlobal`, `createConfig` and
  `createComposer`.

Fixtures: `plugin-pest` (`pest:dump-plugins`, `list`, `help`, completion,
`--no-plugins`), `plugin-drupal` (`scaffold` by its alias and
`drupal:scaffold`, `help`, `list`), `plugin-normalize`
(ergebnis/composer-normalize 2.54.0: `--dry-run --diff`, `normalize` with
its nested `update --lock` in a new Application and its own `new
Factory()->createComposer()`, `-vvv`, an option error) and
`plugin-runtime`'s command-class script (`hello-command`: listed,
described, completed, run with arguments and options, its errors). The
functional fixtures installed-versions, installed-versions2 and
plugin-autoloading-only-loads-dependencies pass (cmd/maestro
`TestAllFunctional`), and RunScriptCommandTest's two Symfony-command tests
run on the plugin runtime. `TestPlugins_Commands` (MAESTRO_PHP_TESTS=1)
covers a plugin's commands in-process: help, completion of a
`Composer\Console\Input\InputOption`'s values, the Application and
maestro's commands as PHP sees them, BaseCommand's helpers, BufferIO, a
PRE_COMMAND_RUN listener changing a built-in command's input, `new
Factory()->createComposer()`, nested Applications writing to the
command's output and to a PHP BufferedOutput, and a command-class script.

**Tier 5: resolver-time and write APIs.**
- `PRE_POOL_CREATE` (lazy package tiers), `PRE_OPERATIONS_EXEC`.
- Root package setters before solving; RepositoryManager
  `createRepository`/`prependRepository`; `ArrayLoader`; the
  `JsonManipulator` RPC; `Locker` writes and `setLocker`; local repository
  add/remove/write.
- `Factory::create` and `Installer::create()->run()` re-entrancy.
- DownloadManager, HttpDownloader and Loop promises; Cache;
  `PreFileDownloadEvent`/`PostFileDownloadEvent`; VersionSelector;
  `PlatformRepository`, `InstalledRepository` and `CompositeRepository`
  construction.

What crosses and how:

- Objects PHP constructs from maestro's classes become maestro's: the
  constructor calls `<area>.new` with `$this`, maestro builds its object
  and adopts PHP's (`rpc.Conn.Adopt`, the sync block's `reg`), so identity
  holds and the methods are maestro's from then on: `PlatformRepository`
  (the platform detected, as the constructor does), `(Installed)FilesystemRepository`,
  `RepositorySet`, `VersionSelector`, `Locker`, `JsonConfigSource`,
  `HttpDownloader`, `RemoteFilesystem`, `Loop`, `Cache` (also a subclass's
  `parent::__construct()`), `SuggestedPackagesReporter` and
  `EventDispatcher` (`ed.new`, a second dispatcher set up as Factory sets
  up a Composer's). Packages and operations created in PHP are adopted when
  they first cross (`new Package`, a plugin's `Package` subclass such as
  civicrm's, `new UninstallOperation`): maestro builds its own from the
  snapshot (`adopt_package.go`, `adoptOperation`). A `JsonManipulator` has
  a maestro peer (`json.newManipulator`, `json.manipulate`).
- PHP reimplementations with Composer's behaviour: `CompositeRepository`,
  `InstalledRepository`, `RepositoryFactory` (its manager is maestro's,
  `repofactory.manager`), `Util\Http\Response`, the platform requirement
  filters, `BaseIO::loadConfiguration` for IOs created in PHP, and
  ProcessExecutor's asynchronous jobs (`executeAsync` runs Symfony Process
  in PHP, as Composer does; maestro's loop waits for them with its own
  jobs, §5.12).
- `Composer\Installer` records its settings in Composer's properties;
  `run()` (`installer.run`) builds maestro's Installer from them (the
  services, the settings, the platform filter) and runs it re-entrantly.
- PRE_POOL_CREATE: the event, its `Request` (`request.*`) and lists;
  `setPackages()`/`setUnacceptableFixedPackages()` are maestro's setters, so
  the pool is built from them. The packages of these lists PHP does not
  know yet cross with their core fields only (id, names, versions, type,
  stability, dev: the lazy tier of §5.3); the getters of the other fields
  fetch them (`pkg.load`, `Maestro\Shim\LazyPackages`). PRE_OPERATIONS_EXEC:
  `Transaction::getOperations()` of maestro's transactions. PRE/POST_FILE_DOWNLOAD:
  mirrors whose setters (URL, cache key, transport options) are maestro's;
  a metadata event's context is `['repository' => ..., 'response' =>
  Response]`.
- Promises carry values (`promiseValueToPHP`): `DownloadManager::download()`
  resolves with the file's path, `HttpDownloader::add()` with a `Response`.
  A `ProcessExecutor::execute()` with a callable output gets its chunks on
  the goroutine holding the PHP baton.
- The generated stubs of maestro's own remote repositories
  (`ComposerRepository`, `VcsRepository`, `FilterRepository`, ...) serve
  RepositoryInterface's methods (`repo.*`), and `Transaction`'s
  `getOperations()` (tools/shimgen `remoteMethods`).

Fixtures: `plugin-merge` (wikimedia/composer-merge-plugin 2.1.0: includes,
a `package` repository created and prepended, `replace`, scripts, extra,
dev sections, the first install's nested update, a changed include),
`plugin-patches1` (cweagans/composer-patches 1.7.3: patching,
`patches_applied` in installed.json, the uninstall before solving when the
patches change), `plugin-patches2` (2.0.0: patches.lock.json,
`patches-relock`, `patches-repatch`, `patches-doctor`, `help`),
`plugin-laminas-dependency` (laminas-dependency-plugin 2.7.0's code from a
path repository: its release requires composer-plugin-api <2.3.0, so
Composer 2.10 skips it; PRE_POOL_CREATE slipstreaming, composer.json
rewrite, nested `update --lock`), `plugin-laminas-component`
(laminas-component-installer 3.8.0), `plugin-civicrm`
(civicrm/composer-downloads-plugin 4.0.0: file and archive downloads, root
and package) and `plugin-download-events` (a path plugin on
PRE/POST_FILE_DOWNLOAD). `TestPlugins_ResolverAPIs` (MAESTRO_PHP_TESTS=1)
covers the rest in-process. Steps whose output a plugin echoes from a
child process in chunks (cweagans's `-v` patch output) are left out of the
fixtures, as the chunk boundaries vary from run to run in Composer as in
maestro.

**Tier 6: internals emulation** (§5.12).
- Backtrace frames.
- `Composer\Installer` mirror with `clone`/`__construct`.
- Protected and private property parity list.
- Transaction with `Closure::bind`.
- ConsoleIO protected members.
- `RepositorySet`, `Pool`, `SuggestedPackagesReporter`.

What crosses and how:

- Frames (`frames.go`, `Maestro\Shim\Frames`): the console's method calls
  (`doRun($input, $output)`, the command's hooks), `Installer::run()`,
  `doUpdate()`, `doInstall()` and the PluginManager's loading go on
  composer.Runtime's frame stack, each named as Composer's method. Every
  call into PHP code (`plugin.load`, `listener.call`, `script.*`,
  `installer.call`, `command.run`) carries the frames pushed since PHP
  last called maestro, and the shim enters each by calling Composer's
  method on its object, which resumes the call (§5.12):
  `debug_backtrace()` shows `class`, `function`, `object` and `args` as
  Composer's frames do, nested runs included (two Installers on the stack
  during a nested run; the PHP Installer whose `run()` maestro runs is its
  own frame). A plugin adding commands with Symfony's `add()` to the
  Application it found there reaches maestro's Application (`app.added`).
- `Composer\Installer` (`internals.go`): maestro's running Installer crosses
  as a Composer\Installer whose properties are its settings and
  collaborators (a service mirror: `Maestro\Shim\Adapter\ServiceAdapter`
  writes the properties in their declaring class's scope); its setters
  record the property, as Composer's do, and send it back as a dirty field
  that maestro's Installer takes (flex's
  `setSuggestedPackagesReporter(new SuggestedPackagesReporter(new
  NullIO))`, discovery's `setAudit(false)`). `clone` gives PHP's own
  Installer object, `__construct()` re-initialises it with the services
  passed, and its `run()` runs a new maestro Installer from its properties
  (`installer.run`), the AuditConfig and PolicyConfig of the original
  included. The platform requirement filter crosses as its description
  and becomes the PHP filter object again.
- The protected and private property list of §5.12 is filled from Go:
  the Installer's properties, `EventDispatcher::$runScripts`,
  `Config::$baseDir`, `ConsoleIO::$input`/`$output` (the run's input and
  output mirrors) and `$helperSet` (a HelperSet with a QuestionHelper, as
  `Application::doRun()` builds it), `ArgvInput::$tokens` (the input
  mirror), `Transaction::$presentPackages`/`$resultPackageMap`/
  `$resultPackagesByName` (keyed by spl_object_id, packages in the lazy
  core tier), and `LockTransaction`'s `$presentMap`, `$unlockableMap` and
  `$resultPackages`.
- `new Transaction($present, $result)` (also inside `Closure::bind`)
  keeps Composer's properties and computes its operations with maestro's
  algorithm (`transaction.new`).
- `clone` of maestro's Config gives an independent copy (the clone's
  private `maestroOrigin` names the original; `object.clone`).
- `RepositorySet::createPool*()` returns a PHP-local `Pool` of the
  packages maestro's pool holds (with its ids) and of what its builder
  removed, as Composer's PoolBuilder passes it to `new Pool()`: the
  optimizer's removals by name and by kept package (keyed by its
  `spl_object_id()`), the security advisories' (Composer's advisory
  objects) and the filter lists' (`FilterListEntry` objects, one per entry
  however many versions it removed), and the abandoned versions;
  `getSecurityAdvisories()`/`getMatchingSecurityAdvisories()` return
  Composer's advisory objects. `new Pool(...)` is PHP-local.
- Repositories written in PHP given to `RepositoryManager::addRepository()`/
  `prependRepository()` or `RepositorySet::addRepository()`, and
  repository classes registered with `setRepositoryClass()` (created in
  PHP by `object.new`), are maestro's through proxies calling the PHP
  object (`object.call`); so are downloaders given to
  `DownloadManager::setDownloader()` (`object.promise` hands their
  promises over). maestro's downloaders serve their methods to PHP
  (`downloader.*`, generated into the stubs by tools/shimgen), and `new
  FileDownloader(...)` (or a subclass without a constructor of its own)
  creates maestro's (`downloader.new`). A PHP subclass of FileDownloader
  (or ZipDownloader, ...) with overrides is maestro's downloader of the
  class it extends, its overrides called by a DownloadManager it is given
  to and, through `$this`, by the inherited code (`downloader.Hooks`); its
  protected helpers are maestro's, and its promise-returning methods
  return React promises. An operation subclass created in PHP (vaimo's
  `ResetOperation`) is adopted as the Composer operation it extends.
- Go errors thrown into PHP have the PHP stack they are thrown into;
  exceptions PHP code throws reach maestro's -v rendering with their PHP
  trace (§5.12, "Exception traces").
- maestro's loop drives and counts the processes PHP code started on a
  loop's executor whenever it waits, as Composer's one loop does (§5.12).

Fixtures (cmd/maestro e2e_plugins6_test.go): `plugin-flex` (symfony/flex
2.11.0 with a local recipes endpoint served over http from
testdata/e2e/flex-recipes: the first install's re-run, recipes and
symfony.lock, pack unpacking, `req logger` alias resolution, `recipes`,
`recipes:install`, `rem`, `-vvv`, `--no-plugins`; flex's random session
id is normalised), `plugin-discovery` (php-http/discovery 1.20.0's
auto-install), `plugin-bamarni` (bamarni/composer-bin-plugin 1.9.1: `bin
all install`, `bin <ns> ...`, forwarding), `plugin-thanks` (symfony/thanks
1.4.0: its commands, `thanks` without credentials), `plugin-vaimo`
(vaimo/composer-patches 6.0.3: patches applied and reapplied, `patch:*`;
its remote patch downloads, a FileDownloader from PHP, are covered
in-process) and `plugin-internals` (frames, traces, pools and
asynchronous processes against Composer). `TestPlugins_Internals`
(MAESTRO_PHP_TESTS=1) covers the rest in-process.

**Beyond the surveyed plugins.** The shim also implements members no
surveyed plugin needs:

- maestro's own commands run from PHP (`$app->find('install')->run()`,
  §5.7): Symfony's `Command::run()` runs in PHP; `initialize()`,
  `interact()`, `execute()` and the classes' own `run()`,
  `isProxyCommand()` and `complete()` are maestro's command's
  (`builtin.*`, generated into the command stubs by tools/shimgen). An
  input created in PHP is adopted as it crosses; the sync engine sends a
  PHP-born mirror's changes made by the call that adopted it with its
  registration, so the hooks and Symfony's `setArgument('command')` see
  the same input.
- BaseCommand's `getPlatformRequirementFilter()`, `getAuditFormat()` and
  `createAuditConfig()` (Composer's code; `Advisory\AuditConfig` is
  Composer's value object, mirrored by its polled public properties), and
  `createPolicyConfig()` (maestro's `PolicyConfig`, `policy.*`).
- IOs created in PHP given to maestro (`phpIO`, proxy_io.go): every
  `IOInterface` method is the PHP object's (`object.call`), maestro's
  validators cross as `Maestro\Shim\GoCallable` (`callable.go`), and the
  IO crosses back as itself. A platform requirement filter class of a
  plugin's own is used the same way (`phpFilter`). On maestro's IO,
  `ConsoleIO::getTable()`/`getProgressBar()` are Composer's on its output
  mirror, `enableDebugging()`/`enableTimestamps()` set maestro's prefixes
  (timestamps formatted as `DateTime::format()`, `php.DateFormat`).
- Composer's services constructed in PHP are maestro's (adopted):
  `new Config()`, `AutoloadGenerator`, `RepositoryManager`,
  `InstallationManager`, `DownloadManager`, `PluginManager`, `Request`.
  `AutoloadGenerator::dump()` returns Composer's `ClassMap`.
- Factory's helpers (`getHomeDir()`, `getCacheDir()`, `getDataDir()`,
  `createDownloadManager()`, `createArchiveManager()` maestro's;
  `addLocalRepository()`, `createGlobalComposer()`,
  `createInstallationManager()`, `createDefaultInstallers()`,
  `createPluginManager()`, `purgePackages()`, `loadRootPackage()`
  Composer's code on them).
- Protected methods a subclass calls on itself, Composer's code or
  maestro's: Filesystem (`directorySize()`, `getProcess()`,
  `removeDirectoryAsync()`), `Cache::getFinder()`,
  `DownloadManager::resolvePackageInstallPreference()`,
  `JsonManipulator::detectIndenting()`, RemoteFilesystem (`get()`,
  `getRemoteContents()`, `callbackGet()`, `promptAuthAndRetry()`,
  `getOptionsForUrl()`), Transaction (`calculateOperations()`,
  `getProvidersInResult()`, `getRootPackages()`), EventDispatcher
  (`doDispatch()`, `getListeners()`, `getScriptListeners()`,
  `pushEvent()`, `popEvent()`, `getPhpExecCommand()`, `executeTty()`,
  `executeEventPhpScript()`, the `is*()` checks), BinaryInstaller (the
  proxy code generators and installers), AutoloadGenerator (all of
  them; after a dump its file generators reproduce maestro's files),
  Installer (`doUpdate()`, `doInstall()`). maestro's own code does not
  call a subclass's overrides of these, except FileDownloader's (tier 6).
- `LockTransaction::getAliases()` is Composer's code,
  `getNewLockPackages()` and `setNonDevPackages()` maestro's.
- Composer's exception classes `TransportException` (with the response's
  headers, body and status of one maestro throws), `FilesystemException`,
  `JsonValidationException` (with its errors), `InvalidPackageException`,
  `SolverBugException`; the shim's own exceptions carry Composer's throw
  line (`Exceptions::at()`).
- `ConsoleOutput::section()` of a maestro output that is not its console
  (a `maestro-output://` stream).

`TestShimStubs_*` (`internal/plugin/stubs_*_php_test.go`,
MAESTRO_PHP_TESTS=1) cover these in-process; the `plugin-stubs` e2e
fixture compares with Composer 2.10.3 Composer's commands run from PHP,
BaseCommand's helpers given to an Installer and IOs created in PHP.

**Remaining stubs.** These members of the hand-written shim (`php/src`)
throw `UnsupportedApiException`, and why:

- `Installer::extractDevPackages()`: it takes the solver's
  `PolicyInterface`; `DefaultPolicy` is presence-only, as its methods
  work on the solver's `Pool`, which never crosses (a `Pool` in PHP is a
  copy).
- `LockTransaction::__construct()` and `setResultPackages()`: they take
  the solver's `Pool` and `Decisions`, which never cross.
- `RepositorySet::createPool()` with a `PoolOptimizer`,
  `SecurityAdvisoryPoolFilter` or `FilterListPoolFilter`: presence-only
  solver internals whose constructors are stubs, so no plugin can pass
  one.
- `new RemoteFilesystem()` with an `AuthHelper`: `Util\AuthHelper` is
  presence-only (its constructor is a stub).
- The methods of a dispatcher, FilesystemRepository,
  InstalledFilesystemRepository or PlatformRepository whose constructor
  did not run (a subclass not calling `parent::__construct()`, an object
  made without its constructor): such an object has no maestro peer,
  which the constructor creates.
- `ConsoleIO::getTable()`/`getProgressBar()` on a maestro IO without a
  console output (maestro's ConsoleIO and BufferIO always have one).
- Running maestro's Application with an input that is not an
  `ArgvInput`, `StringInput` or `ArrayInput`: maestro parses the command
  line itself, from tokens or parameters, which another
  `InputInterface` implementation does not expose.
- Creating Composer's command classes in PHP (`configure()`, so
  `new InstallCommand()` or a subclass of one) and their protected
  helpers (`ShowCommand::printTable()`, ...): maestro's commands are Go
  objects; only the instances its Applications hold have one behind
  them.
- `Maestro\Shim\Server`'s "no handler" error is a protocol mismatch
  between maestro and its shim, not an API member.

Known limits: `AutoloadGenerator::getAutoloadRealFile()` takes
`$prependAutoloader` as `'true'` or anything else. (A `ProcessExecutor` or
`Filesystem` given to a service created in PHP is honoured, and the calls
of maestro's parallel work reach an IO created in PHP in Composer's order:
§5.12, §5.14.)

The generated stubs (`php/stubs`: VCS drivers, SelfUpdate, the solver's
internals, `Util\*` helpers such as `Url` or `PackageSorter`, ...) are
presence-only (D7), implemented when an e2e fixture or a user report needs
them. `UnsupportedApiException` messages name the exact method, so these
reports are actionable.

---

## 9. Tests

### 9.1 Unit and integration tests (Go, `go test`)

- **Codec.** Golden round-trips. `tools/oracle/plugin/codec.php` (run in
  the dev shell) writes `internal/plugin/rpc/testdata/codec/*.json` from
  PHP values covering:
  - int and string key coercion, nested lists and maps, empty arrays;
  - binary strings, INF/NAN, floats such as `1.0`, `0.1` and `1e100`;
  - stdClass, and NUL-prefixed keys.

  Go decodes them, re-encodes, and compares bytes. Goldens are committed.
- **Sync engine.** Simulated peers (a Go fake of the PHP side) check
  revision bookkeeping, no echo, the `reg` remap and the full-snapshot
  fallback.
- **Plugin manager policy.** The `PluginManager`-relevant tests of
  `.ref/composer/tests/Composer/Test/Plugin/PluginInstallerTest.php`
  (allow-plugins rules, API version checks, ordering and weights, global vs
  local) are ported against a recording fake runtime, with the exact
  message texts, and `EventDispatcherTest.php` is ported into
  `internal/eventdispatcher`, with the PHP callable listener cases against
  the fake runtime.
- **Handler coverage.** The set of `svc_*` handlers equals the set of RPC
  methods the shim proxies reference, extracted from the shim source.

### 9.2 Shim tests (require php: `MAESTRO_PHP_TESTS=1`, run in CI)

- **Presence parity.** `tools/shimgen` writes
  `internal/plugin/testdata/apiparity.json` from `.ref/composer/src` by
  reflection: class kind, parent, interfaces, and for each public or
  protected method its name, parameter names, types, defaults, by-ref,
  return type, static and visibility; constants with values (a string below
  the declaring file's directory as `__DIR__ . '/…'`, so the golden does
  not hold the checkout's path and the shim matches it on any machine). The
  test reflects the shim in a real php and requires an exact match, except
  for an explicit allowlist (for example `Compiler`). It also requires that
  no extra `Composer\*` classes exist.
- **Vendored drift.** The vendored library versions equal
  `.ref/composer/composer.lock`.
- **Runtime.** The real child against a scripted Go peer:
  - the handshake and round-trip budgets (§5.16);
  - re-entrancy depth;
  - `exit(3)` mid-listener gives maestro exit 3;
  - a fatal error gives 255 and the PHP message;
  - a shutdown function runs after Go's last output;
  - `Platform::putEnv` in a listener is visible to a following shell
    script, a plain `putenv` of a new variable is not (as in Composer,
    whose child processes get Symfony Process's default environment);
  - `chdir` in a listener affects `Filesystem::isAbsolutePath` resolution.
- **PHP versions.** CI runs the PHP-backed plugin and command tests on PHP
  8.4 and on 7.2 (§1).

### 9.3 End-to-end fixtures (`MAESTRO_E2E=1`)

The plugin fixtures run on `cmd/maestro`'s e2e harness (`TestE2EPlugins`
in e2e_plugins_test.go and e2e_plugins6_test.go, on `TestE2E`'s runner):

- **Fixtures.** Each is a project in
  `cmd/maestro/testdata/e2e/plugin-<name>/` (composer.json and, for
  Packagist plugins, the composer.lock pinning their versions); its steps
  (one command each, with optional setup, input and normalisation) are
  listed in the test. Local plugins come from path repositories in the
  fixture; flex's recipes come from a local endpoint
  (`testdata/e2e/flex-recipes`).
- **Each scenario runs with both tools,** real Composer 2.10.3 (the pinned
  phar) and maestro, in fresh copies of the fixture with isolated
  `COMPOSER_HOME` and caches, cold (empty caches) and then warm (the
  caches the cold phase left).
- **Compared:** the exit code; stdout and stderr (after normalising
  timings, temporary paths and other run-specific values; errors and
  deprecation notices by the information they carry, as docs/PORTING.md
  "Tests" describes); and the whole scenario tree after every step:
  `composer.json`, `composer.lock`, the vendor tree (paths, contents,
  modes, symlink targets; mtimes ignored) and every file a plugin writes
  outside vendor (`.mcp.json`, `symfony.lock`, `config/*`,
  `bootstrap/cache`, patch results, `.htaccess`, …).

Each fixture's steps cover, as the plugin allows: `install` from the lock
and `update`; `require` and `remove`; `dump-autoload` with and without
`-o`; `--no-plugins` and `--no-scripts`; `-vvv`; the plugin's own commands
with `list`, `help` and completion; and, for plugin-api, a plugin not in
`allow-plugins` (the blocked error) and one no longer required (the
warning). §8 lists the fixtures of each tier.

---

## 10. Risks

| # | Risk | Likelihood / impact | Mitigation |
| --- | --- | --- | --- |
| 1 | **Internals-dependent plugins** (flex, discovery, bamarni, vaimo) rely on stack frames, private and protected props, clone and re-construct, and `Closure::bind`. Any upstream refactor on their side or a gap in the emulation breaks them. | High / high (flex is among the most installed plugins) | Tier 6 emulation with per-plugin fixtures; the property parity list (§5.12); unsupported paths throw a named `UnsupportedApiException` instead of misbehaving silently |
| 2 | **Output interleaving** between two processes: unflushed Go buffers, PHP `ob_*` buffers, `overwrite()` and progress bars spanning both sides | Medium / high (byte-identical output is a hard goal) | Flush before every transfer (D11); IO methods all go through Go; e2e compares exact bytes |
| 3 | **Exception file and line** differ for exceptions raised in shim or Go code, because Composer's own source lines don't exist | Certain / low | Error rendering and deprecation notices are maestro's own (no locations or stacks shown), and maestro records no throw sites or frames of Composer's for Go errors; this only matters to plugins reading `getFile()`, `getLine()` or `getTrace()`, which see the shim's locations for exceptions maestro raised; the shim's own throw sites and raised errors name Composer's line |
| 4 | **Promise timing.** `then()` callbacks of PHP installers, and parallel Go operations, change completion order | Medium / low | The promise bridge (§5.6) keeps promises pending on both sides until Composer would run their callbacks; fixtures for `->then()` installers (composer/installers, yii2) |
| 5 | **stdin sharing** when stdin is a pipe and the run is still interactive (`SHELL_INTERACTIVE`): PHP's STDIN buffer can swallow lines meant for Go | Low / medium | Go reads unbuffered; a stdin relay that hands stdin to PHP only while PHP runs a prompt would close it |
| 6 | **Re-entrancy of Go ports.** Nested `Installer::run`, `Factory::create` and Application runs from inside events require every Go port to be free of package-level state | Medium / high | Requirement in §7; e2e for merge-plugin, discovery, ergebnis, laminas |
| 7 | **Performance.** PRE_POOL_CREATE with large pools, chatty plugins (`getInstallPath` per package, many Filesystem calls), and PHP startup on every command in plugin projects | Medium / medium | Lazy tiers, batching, measured budgets (§5.16) |
| 8 | **API parity drift.** A shim signature differs from Composer's, so a plugin that extends a class gets a fatal "Declaration must be compatible" | Medium / high | Generated parity test with exact signatures (§9.2) |
| 9 | **PHP version spread.** The shim must parse and run on 7.2.5 to 8.5; deprecations show up as Composer-style deprecation notices | Medium / medium | CI on PHP 7.2 and 8.4; the shim follows Composer's own PHP style rules |
| 10 | **fd leakage to grandchildren** (Unix) and corruption of the channel | Low / high | Process-exit detection, not EOF; framing validation kills the child on garbage |
| 11 | **Xdebug restart parity** (env vars, ini contents) | Low / low | Port of XdebugHandler's ini building, with a test that the restart ini loads what plain `php` loads |
| 12 | **Windows transport** (TCP, `proc_open` quoting in the launcher stub) | Medium / medium | The transport abstraction (D5); CI's Windows plugin shard and the Windows e2e run |
| 13 | **Vendored library precedence.** A project shipping a newer symfony/console could expect its own classes, but the shim's win. This is the same as with Composer's phar, so it is correct, but surprising. | Low / low | Matches Composer; no action |

---

## 11. Size

Approximate line counts:

| Part | Lines |
| --- | --- |
| `internal/plugin` and its subpackages, Go (runtime, transport, codec, sync, PluginManager port, mirrors, proxies, service handlers) | 18,000 |
| Their Go tests | 6,500 |
| PHP shim, hand-written (`php/src`: the `Maestro\*` runtime about 5,500, Composer's classes about 12,000) | 17,500 |
| `tools/shimgen`, `tools/shimvendor`, `internal/plugin/shimbuild` | 1,200 |

Besides these, the shim embeds about 76,000 lines of vendored PHP
(`php/lib`) and 8,000 lines of generated stubs (`php/stubs`), both
produced by tools. The plugin e2e fixtures number 35.
