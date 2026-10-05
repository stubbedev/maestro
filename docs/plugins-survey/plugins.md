# Per-plugin notes

These notes were read from each package's source. Event priorities are 0
unless a number is given. Abbreviations: **LI** = `Composer\Installer\LibraryInstaller`,
**IM** = `InstallationManager`, **RM** = `RepositoryManager`,
**localRepo** = `getRepositoryManager()->getLocalRepository()`. "In place"
means the plugin mutates an object that Composer itself reads afterwards, so
the host must see the change.

## Custom installers (install paths)

**composer/installers 2.3.0.** It has no events. `activate` calls
`IM::addInstaller(new Installer)`; `deactivate` calls `removeInstaller` with
the same object.
- `Installer` extends LI. Its constructor passes `($io, $composer, 'library', ?Filesystem, ?BinaryInstaller)` to the parent.
- It overrides `supports`, which matches about 100 framework type prefixes plus a regex built from each sub-installer's location keys.
- It overrides `getInstallPath`. This reads root `extra.installer-paths`, package `extra.installer-name` and `getcwd()`, and calls `$this->filesystem->isAbsolutePath`.
- It overrides `uninstall` as `parent::uninstall()->then(...)`.
- It calls `new Package('dummy/pkg', ...)` on every `supports()` call.
- CakePHPInstaller calls `localRepo->findPackage('cakephp/cakephp', new Semver\Constraint\Constraint(...))`.
- BitrixInstaller calls `$io->ask()` from inside `getInstallPath`, keeps a static `$checkedDuplicates`, and removes directories.
- It sets `extra.plugin-modifies-install-path`, so PluginManager weights it to load first.

**oomphinc/composer-installers-extender 2.0.1.** It extends
`Composer\Installers\Installer`, which belongs to a different plugin, so that
plugin's classes must be autoloadable as dependencies. `supports` checks root
`extra.installer-types`. `getInstallPath` uses `BaseInstaller`, falling back to
`LibraryInstaller::getInstallPath` called non-statically on an ancestor.

**mnsami/composer-custom-directory-installer 2.2.1.** `extra.class` is an
array of three plugin classes.
- One installer extends LI with default type `library`, so it shadows Composer's own library installer. It checks root `extra.installer-types`.
- One installer extends **`Composer\Installer\PluginInstaller`**. The inherited `install`/`update`/`uninstall` must really register, deactivate and uninstall plugins through `getPluginManager()`, including rollback.
- One installer extends `Composer\Installer\PearInstaller`, which is guarded by `class_exists`. The shim must **not** define PearInstaller, because Composer 2 does not have it.

**cakephp/plugin-installer 2.0.2.**
- `pre-autoload-dump`: it scans `extra.plugin-paths` and then calls **root `setAutoload`/`setDevAutoload` in place** to add psr-4 entries.
- `post-autoload-dump`: it lists localRepo packages of type `cakephp-plugin` and writes `vendor/cakephp-plugins.php`.
- It uses `realpath(vendor-dir)`.

**johnpbloch/wordpress-core-installer 2.0.0.** Its installer extends LI and
supports `wordpress-core`. The install path comes from root
`extra.wordpress-install-dir` and is **relative**. It calls
`Config::get('vendor-dir', Config::RELATIVE_PATHS)` and keeps a static map of
installed paths, which throws when the same path is used twice. The plugin
object must live for the whole run.

**typo3/cms-composer-installers 5.0.2.**
- It listens to `PRE_AUTOLOAD_DUMP` and `POST_AUTOLOAD_DUMP`, and also calls `addSubscriber($this)` itself from `activate`, so it is registered twice. A guard runs each event once.
- Pre: it writes `vendor/typo3/autoload-include.php`, adds it to **root autoload `files` in place**, then requires it, which calls `putenv(TYPO3_PATH_*)`. Later scripts must see those environment variables.
- Post: `AutoloadGenerator::buildPackageMap` → `parseAutoloads` → `createLoader()->register()`, then it runs `<Ns>\Composer\InstallerScripts::register` classes from installed packages. These write `public/index.php` and similar files.
- It uses **reflection on private `Composer\Config::$baseDir`**, `new NullIO`, `IO::warning` (PSR-3), and `Filesystem::findShortestPathCode`.

**magento/magento-composer-installer 0.5.0.**
- Its installer extends LI with type `magento-module` and calls `initializeVendorDir()`. It overrides `getInstallPath`, `install`, `update` and `uninstall` as `parent::*()->then(queue deploy)`.
- `POST_INSTALL_CMD`/`POST_UPDATE_CMD` (priority 1) deploys files into the Magento root, writes `app/etc/vendor_path.php`, and runs chmod.
- `POST_PACKAGE_UNINSTALL` removes deployed directories.
- It calls `new AutoloadGenerator(new EventDispatcher($composer, $io))` and `Autoload\ClassMapGenerator::createMap`.
- It calls **`askConfirmation` inside `activate`**, can throw from `activate`, and reads `$io->getOption()`, which is not part of the interface and is dead code on Composer 2.

**yiisoft/yii2-composer 2.0.11.** Its installer extends LI and supports
`yii2-extension`.
- `install`/`update`/`uninstall` are `parent::*()->then(...)` and rewrite `vendor/yiisoft/extensions.php`, which is `require`d back, with `opcache_invalidate`.
- `POST_PACKAGE_UPDATE` records versions using `VersionParser::isUpgrade`. `POST_UPDATE_CMD` prints notes from UPGRADE.md.
- Static script callbacks (`postCreateProject`, `postInstall`) print with `echo`.
- It imports `Composer\Script\CommandEvent`, which does not exist in Composer 2, but never uses it.

**endroid/installer 1.5.2.** `POST_INSTALL_CMD`/`POST_UPDATE_CMD` (priority 1)
copies each package's `.install/<type>/**` into `getcwd()`. It uses
`IM::getInstallPath`. Requires PHP 8.4.

## Generated files after install or autoload dump

**phpstan/extension-installer 1.4.3.** `POST_INSTALL_CMD`/`POST_UPDATE_CMD`
rewrites its **own** `src/GeneratedConfig.php`, which relies on `__DIR__`
being the real vendor path.
- It reads localRepo packages and `IM::getInstallPath` (which can be null).
- It reads `getRequires()['phpstan/phpstan']->getConstraint()->getLowerBound()/getUpperBound()`, so constraints must be **real composer/semver objects**.
- It uses `MultiConstraint`, `Intervals::compactConstraint` and `getFullPrettyVersion`.

**infection/extension-installer 0.1.2.** Same pattern as phpstan. It writes
its own `src/GeneratedExtensionsConfig.php`.

**dealerdirect/phpcodesniffer-composer-installer 1.2.1.**
`POST_INSTALL_CMD`/`POST_UPDATE_CMD` runs `php phpcs --config-set installed_paths` through
`new ProcessExecutor($io)->execute($cmd, $out, $cwd)`.
- It uses Symfony **Finder** and **PhpExecutableFinder**, which Composer bundles; the plugin does not require them.
- It calls `localRepo->findPackages(name, '>= 3.0.0')` with a string constraint.
- It checks `instanceof AliasPackage` and `instanceof RootPackageInterface`.
- It also has a static `Plugin::run(Event)` script entry point.

**drupal/core-vendor-hardening 11.4.8.**
- `PRE_PACKAGE_INSTALL`/`UPDATE` calls **`$package->setBinaries()` in place** on the operation's package, before the BinaryInstaller runs.
- `POST_PACKAGE_*` deletes subpaths from installed packages.
- `POST_AUTOLOAD_DUMP` writes `vendor/.htaccess`.
- It keeps state across events.

**drupal/core-composer-scaffold 11.4.8.**
- `POST_PACKAGE_INSTALL` records packages.
- `PluginEvents::COMMAND` checks for `require`.
- **Static** method `preAutoloadDump` is registered on the instance. It appends classmap entries to **root autoload in place** and writes `vendor/drupal/DrupalInstalled.php`.
- `POST_INSTALL/UPDATE_CMD` scaffolds files, calls `dispatchScript('pre-drupal-scaffold-cmd'/'post-…')` (custom events that run root scripts), and runs git through `ProcessExecutor`.
- It provides the command `drupal:scaffold` (alias `scaffold`) through CommandProvider.

**drupal/core-project-message 11.4.8.** On `POST_CREATE_PROJECT_CMD` and
`POST_INSTALL_CMD` it prints `extra.drupal-core-project-message` with
`io->write(array)`.

**symfony/runtime 8.1.0.** `getSubscribedEvents()` returns `[]` until
`activate()` has set a static flag, so `activate` must run before
`addSubscriber`. `POST_AUTOLOAD_DUMP` writes `vendor/autoload_runtime.php`.
It uses Symfony **Filesystem** (bundled) and `Factory::getComposerFile`.

**composer/package-versions-deprecated 1.11.99.5.** `POST_AUTOLOAD_DUMP`
(static method) reads `getLocker()->getLockData()` (the in-memory **current**
lock), root `getReplaces`, and `AliasPackage::getAliasOf`. It writes
`vendor/composer/package-versions-deprecated/src/PackageVersions/Versions.php`.
PluginManager never allows it as a plugin (hard-coded `false`).

**ocramius/package-versions 2.12.0.** This is a library, not a plugin. It
reads `InstalledVersions` only.

**typo3/class-alias-loader 2.0.1.** `pre-autoload-dump`:
- Calls `buildPackageMap`.
- Requires each package's alias-map PHP file.
- Writes `vendor/typo3/alias-loader-include.php` and `vendor/composer/autoload_classaliasmap_static.php`.
- **Adds the include to root autoload `files` in place.**
- Reads the existing `vendor/autoload.php` to recover the autoloader suffix.

**tbachert/spi 1.0.5.** It sets `plugin-optional: true`. `PRE_AUTOLOAD_DUMP`:
- `buildPackageMap` → `parseAutoloads` → `createLoader()->register()`. This **executes project code**: `require`s files and calls `class_exists` and reflection on attributes.
- Calls `InstalledVersions::isInstalled/satisfies`, expecting project data.
- Writes `vendor/composer/GeneratedServiceProviderData.php` with `Filesystem::filePutContentsIfModified`.
- Changes **root autoload in place** and also **`setAutoload` on localRepo `Package` objects in place**.

**mcaskill/composer-exclude-files 5.0.0.** `PRE_AUTOLOAD_DUMP` calls
`buildPackageMap(IM, root, localRepo->getCanonicalPackages())`, compares
**`$package === $rootPackage`**, and calls **`setAutoload()` in place on
localRepo packages** to drop `files` entries. This is the hardest identity
case.

**php-http/discovery 1.20.0.** It sets `plugin-optional: true`.
- `PRE_AUTOLOAD_DUMP`: writes `vendor/composer/GeneratedDiscoveryStrategy.php` and changes **root autoload in place**.
- `POST_UPDATE_CMD`:
  1. Edits composer.json through `JsonManipulator`.
  2. Finds the `Composer\Installer` object with `debug_backtrace(DEBUG_BACKTRACE_PROVIDE_OBJECT)`.
  3. Calls `stopPropagation()`.
  4. Calls `Factory::create`.
  5. Calls `clone $installer; $installer->__construct(9 services)`.
  6. Sets `setPlatformRequirementFilter(((array)$orig)["\0*\0platformRequirementFilter"])`, a protected property read through an array cast.
  7. Calls `run()`, which re-runs the install in-process.
  8. Uses `new Locker(...)`, `Locker::getContentHash`, `JsonFile::write` and `VersionSelector`.
- It also reads `((array)$eventDispatcher)["\0*\0runScripts"]`.

**stubbedev/{atlassian,ds,jenkins,sentry}-mcp.**
- `POST_INSTALL_CMD`/`POST_UPDATE_CMD` downloads a native binary with curl into the plugin's own directory, using `__DIR__`. Errors become `writeError` warnings.
- The only API calls are `IO::write`/`writeError`.
- ds-mcp's plugin lives in `packaging/composer/src`, and it uses PharData/ZipArchive.

**stubbedev/laravel-dev-mcp 0.0.15.** Same as above, plus:
- It writes `<dirname(vendor-dir)>/.mcp.json`.
- It reads `\Composer\InstalledVersions::getPrettyVersion('stubbedev/laravel-dev-mcp')`. Inside Composer's process this must hold the project's data. Real Composer loads it in `Factory::createComposer` and reloads it on every local repository write.

**laravel/framework 13.x (`Illuminate\Foundation\ComposerScripts`).** These are
PHP-callable *scripts*, not a plugin: `postInstall`, `postUpdate` and
`postAutoloadDump` take a `Script\Event`, and `prePackageUninstall` takes a
`PackageEvent`.
- They `require_once vendor/autoload.php` inside Composer's process, so the project's classes coexist with Composer's bundled classes.
- They unlink `bootstrap/cache/*.php`.
- `prePackageUninstall` boots Laravel and spawns PHP.

**pestphp/pest-plugin 4.0.0 and 5.0.0 (same logic).**
- `post-autoload-dump` runs `new DumpCommand` with `->setComposer()` and `->run(new ArrayInput([]), new ConsoleOutput())`. This is a `BaseCommand` run **outside any Application**.
- The command writes `vendor/pest-plugins.json`.
- CommandProvider adds `pest:dump-plugins`.
- `uninstall()` deletes the file.

**captainhook/hook-installer 1.0.5.** `POST_INSTALL/UPDATE_CMD` runs
`proc_open(PHP_BINARY captainhook install --ansi|--no-ansi ...)` with
inherited stdio and **throws** on failure. It uses `IO::isDecorated` and
`write($m, false)`.

**phpro/grumphp 2.25.0.**
- `PRE_PACKAGE_INSTALL`/`UPDATE`/`UNINSTALL` and `POST_PACKAGE_INSTALL` look at **`getOperations()`** (the full list) with `instanceof *Operation` and `getNames()`.
- If grumphp is being uninstalled, it runs `grumphp git:deinit` *during* `PRE_PACKAGE_UNINSTALL`, while the files still exist.
- `POST_*_CMD` runs `configure` and `git:init` via `proc_open` with inherited stdio. It passes `--ansi`/`--no-ansi` from `isDecorated()`.

**pyrech/composer-changelogs 2.2.0.**
- `POST_PACKAGE_*` collects operation objects. `POST_UPDATE_CMD` runs at a **priority computed in `activate()`** (static).
- It prints changelogs and can **prompt** (`askConfirmation`) to commit through `exec`.
- It uses **reflection on private `Config::$baseDir`** and `Semver\Comparator`.

**civicrm/composer-downloads-plugin 4.0.0.** `POST_PACKAGE_INSTALL`/`UPDATE`
and `POST_*_CMD` (priority 10):
- Builds `Subpackage extends Package` objects (`setDistUrl/Type`, `setTargetDir`, `setInstallationSource`, and writes public `$id`).
- Calls `getDownloadManager()->download($sub, $path)` and `->install()`. These return promises, and `->then($file => ...)` expects the **downloaded file path** as the resolved value.
- Calls `getLoop()->wait([$p])`.
- Checks `Composer::getVersion()` against 2.0.0.

## Pool, root package, repositories, re-runs

**symfony/flex 2.11.0.** This is the heaviest plugin.
- `PRE_POOL_CREATE` filters `getPackages()` by `extra.symfony.require` and calls `setPackages()`. It uses `getRequest()->getFixedOrLockedPackages()`, `AliasPackage::getAliasOf`, `Intervals::haveIntersections`, and does HTTP during the event.
- `PRE_OPERATIONS_EXEC` builds `new Transaction($pkgs, $event->getTransaction()->resultPackageMap)` through `Closure::bind` into Transaction's scope.
- `PRE_UPDATE_CMD` finds the `Installer` with **`debug_backtrace`** and calls `setSuggestedPackagesReporter(new SuggestedPackagesReporter(new NullIO))`, which suppresses Composer's suggestion output. It disables its downloader when a `GlobalCommand` frame is on the stack.
- `activate()` scans the backtrace for a `Composer\Console\Application` frame whose `args[0]` is `ArgvInput`. With that frame it:
  - calls `$app->find`, `mergeApplicationDefinition` and `$input->bind`;
  - **rewrites the private `ArgvInput::$tokens`** (alias resolution such as `req orm`);
  - calls `$app->add(new XxxCommand)` four times;
  - sets `$_SERVER['COMPOSER_PREFER_DEV_OVER_PRERELEASE']`.
- `'auto-scripts'` (a custom event reached through `@auto-scripts`) calls `stopPropagation()` and runs the script map itself. Plugin listeners must therefore come before script listeners at priority 0.
- `POST_INSTALL/UPDATE_CMD`:
  - recipes, writing `symfony.lock`, config files, package.json, and composer.json edits through `JsonManipulator`;
  - composer.lock hash: `new Locker`, `Locker::getContentHash`, `JsonFile::write`;
  - **`$composer->setLocker()`**, `localRepo->removePackage()`, `setDevPackageNames()` and `localRepo->write()`;
  - a possible full re-run: `Factory::create` + `clone $installer; ->__construct(...); ->run()`.
- `activate()` calls `IM::addInstaller(new SymfonyPackInstaller($io))`, which **extends `Composer\Installer\MetapackageInstaller`** (type `symfony-pack`).
- `activate()` loads every PHP file of its own `src/` with `class_exists` so all Flex classes come from one version.
- It uses HttpDownloader `add`/`get`, `new Loop`, `new Cache`, `RepositoryManager::getRepositories()->loadPackages`, `RepositorySet` + `VersionSelector`, and **array-cast reads of protected properties**.
- `UpdateEvent extends Script\Event` and **skips the parent constructor**.
- It prompts with `askAndValidate`, `askConfirmation` and `select`.

**symfony/thanks 1.4.1.** `activate()` sniffs the backtrace for `Application` +
`ArgvInput`, then calls `$app->add(new ThanksCommand)` and adds `FundCommand`
if `!$app->has('fund')`. `POST_UPDATE_CMD` sends a GitHub GraphQL request through
`new HttpDownloader($io, $config)->get(url, POST options)`, using Composer's
github-oauth credentials.

**wikimedia/composer-merge-plugin 2.1.0.** It listens at priority **50000** to
`INIT`, `PRE_INSTALL_CMD`, `PRE_UPDATE_CMD` and `PRE_AUTOLOAD_DUMP`, merging
other composer.json files into the **root package in place before solving**:
- `setRequires`/`DevRequires`/`Conflicts`/`Replaces`/`Provides` with `new Link` objects and `MultiConstraint::create`;
- `setSuggests`, `setAutoload`, `setDevAutoload`, `setStabilityFlags`, `setExtra`, `setScripts`, `setReferences`, `setAliases`;
- `setRepositories([...RepositoryInterface objects...])`;
- `RM::createRepository` + `prependRepository`.

It uses `new ArrayLoader()->load()` (expects CompletePackage) and
`Intervals::isSubsetOf/clear`. On first install it re-runs the update
in-process: `Installer::create($io, Factory::create($io, null, false))->setUpdate(true)->setUpdateAllowList(...)->run()`.

**laminas/laminas-dependency-plugin 2.7.0.**
- `PRE_POOL_CREATE` (priority 1000) swaps zend packages for laminas packages from `RM::findPackage(name, version)` and calls `setPackages` and `setUnacceptableFixedPackages`.
- `PRE_COMMAND_RUN` **calls `$input->setArgument('packages', ...)`**, which must change the arguments the running command sees.
- `POST_AUTOLOAD_DUMP` (priority -1000) rewrites composer.json, calls `IM::uninstall` (promise never awaited), and runs **`new Console\Application()->run(new ArrayInput(['command'=>'update','--lock'=>true,...]))` in-process**. It reads `new ArgvInput()`, so `$_SERVER['argv']` must be Composer's argv.
- It ships a stub `Composer\Plugin\PrePoolCreateEvent`, guarded by `class_exists`, for Composer 1.

**laminas/laminas-component-installer 3.8.0.** On `POST_PACKAGE_*` (dev mode
only) it edits `config/*.php`, using files that **must already be on disk**.
- It **prompts** with `ask` in a loop during package events.
- It builds `new InstalledRepository([RootPackage::getRepository() ?? new RootPackageRepository(clone root), localRepo, new PlatformRepository([], platform)])`.
- It builds `new CompositeRepository(RepositoryFactory::defaultReposWithDefaultManager(new NullIO))`, which may hit the network.

**zaporylie/composer-drupal-optimizations 1.2.0.** On plugin API 2.x it logs
one verbose line and does nothing. Its Composer 1 path binds a closure onto
RepositoryManager privates and is dead code.

**cweagans/composer-patches 1.7.3.**
- `PRE_INSTALL/UPDATE_CMD` **uninstalls** packages whose patches changed, through `IM::uninstall(localRepo, new UninstallOperation)` + `getLoop()->wait()`, before solving.
- The first `PRE_PACKAGE_*` reads **`getOperations()`**.
- `POST_PACKAGE_INSTALL/UPDATE` (priority **10**) applies patches **synchronously into the install path**, using `getInstaller($type)->getInstallPath()`, `new RemoteFilesystem`, `ProcessExecutor` and `patch`/`git apply`.
- It **calls `setExtra(+patches_applied)` in place on the localRepo package**, which must end up in installed.json.
- It dispatches its own `PatchEvent extends Event` with `dispatch(null, $event)`.

**cweagans/composer-patches 2.0.0.** Sets `plugin-modifies-downloads` and
`plugin-modifies-install-path`.
- `PRE_PACKAGE_*` resolves patches and writes `patches.lock.json`.
- `POST_PACKAGE_*` (priority 10) applies them synchronously.
- `getPluginManager()->getPluginCapabilities(Custom::class, [...])` uses its own capability interfaces and expects `$args['plugin']` to be injected. `getPlugins()` must return the live instances.
- It uses `new HttpDownloader()->copy()`, `JsonFile`, `ProcessExecutor` and git.
- Commands: `patches-doctor` (checks that `getApplication()->getVersion()` starts with `2`), `patches-relock`, and `patches-repatch`, which uninstalls then calls **`getApplication()->run(new ArrayInput(['command'=>'install']))`**.

**vaimo/composer-patches 6.0.3.**
- `activate()` calls **`Factory::createGlobal(new NullIO, true)`**, i.e. a second Composer.
- `PRE_AUTOLOAD_DUMP` reapplies patches. It reinstalls packages itself: `IM::uninstall` → `getInstaller()->download()` → `React\Promise\all` → `IM::install(new ResetOperation extends InstallOperation)` → `getLoop()->wait()`.
- It calls **`setExtra` in place** on localRepo packages and then `localRepo->write()`.
- It rewrites composer.lock without a new hash.
- It calls `getDownloaderForPackage()->getLocalChanges()` and `new FileDownloader(...)->download(fakePackage)`.
- **`OutputUtils extends ConsoleIO`** reads the protected `$output->setVerbosity()`. `Silencer` type-hints `ConsoleIO`.
- Commands: `patch`, `patch:apply`/`undo`/`redo`/`list`/`validate`. They `dispatchScript(POST_INSTALL_CMD)`.

**bamarni/composer-bin-plugin 1.9.1.**
- `PluginEvents::COMMAND` and `POST_AUTOLOAD_DUMP` can forward `install`/`update` to every `vendor-bin/*` namespace. That path requires **`$event->getIO() instanceof ConsoleIO`** and reads ConsoleIO's protected `input`/`output`/`helperSet`.
- Command `bin <ns> ...` (a proxy command that ignores validation errors):
  - calls `chdir(vendor-bin/<ns>)`;
  - calls `putenv(COMPOSER_BIN_DIR)`;
  - runs **`(new Composer\Console\Application())->doRun(new StringInput(...), $output)`** in-process, with a fresh Composer in that directory;
  - calls `getApplication()->resetComposer()` and `resetComposer()` on every command in `all()`.
- It uses `Factory::createConfig`, `JsonFile::validateSchema` and `Config::merge`.

**ergebnis/composer-normalize 2.54.0.** CommandProvider maps to `self::class`,
so Composer constructs a second instance. Command `normalize [file]`
(`--diff --dry-run --no-check-lock --no-update-lock --indent-size --indent-style`):
- calls `new Composer\Factory()` and then `->createComposer($io, $file)` (`instanceof Composer`);
- checks `Locker::isLocked/isFresh`;
- writes composer.json;
- then runs **`new Console\Application()->run(new ArrayInput(['command'=>'update','--lock'=>true,'--no-autoloader'=>true,'--working-dir'=>...]), $output)`**.

It reads `InstalledVersions` for its own version. It ships its own
justinrainbow/json-schema, which conflicts with the bundled copy.

**ramsey/composer-repl 1.5.1 (code in ramsey/composer-repl-lib).** It
requires **`composer/composer` as a real dependency**, so a second copy of
Composer's classes ends up in the project autoloader.
- The constructor calls `Factory::getComposerFile()`.
- `activate` stores Composer in a static.
- Command `repl` (alias `shell`) calls `setComposer()`, `Config::disableProcessTimeout()` and `requireComposer()`, then runs a TTY Symfony Process.

## Not plugins

**stubbedev/laravel-stoli 0.2.4** (required as `dev-master` by the Kontainer
project) is a `library` with only `extra.laravel`. It touches no Composer API
and needs nothing from the shim.
