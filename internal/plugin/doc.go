// Package plugin ports Composer\Plugin and hosts the bridge to PHP that
// Composer plugins and PHP-callable scripts run through (deviation 5 in
// docs/PORTING.md; docs/PLUGINS.md is the specification).
//
// The runtime and its protocol (phase 1):
//
//   - shim.go embeds the PHP shim (php/: bootstrap.php, the Maestro\Shim
//     runtime, the hand-written Composer classes of php/src, the
//     presence-parity stubs of tools/shimgen and the libraries
//     tools/shimvendor vendors) and extracts it into
//     <cache-dir>/maestro/shim/<sha256 of its manifest>/, atomically.
//   - runtime.go is plugin.Runtime: one long-lived php child per maestro
//     process (D1), started on first need with bin/composer's prologue
//     (xdebug.go ports its xdebug restart), and shut down so PHP's
//     shutdown functions run and its exit status becomes maestro's.
//   - process.go starts the child with its channel: inherited pipes on
//     fds 3/4, or a loopback socket with a token (Windows).
//   - rpc/ is the channel itself: framing, the value codec, handles, the
//     re-entrant call stack, the sync engine and exceptions both ways.
//
// The core Composer API (phase 2):
//
//   - manager.go ports PluginManager's policy (load order, allow-plugins
//     with its prompt, the plugin API check, global plugins, the autoload
//     plan); php/src/Maestro/Shim/Plugins.php is its mechanism.
//   - scriptruntime.go makes the Runtime internal/eventdispatcher's
//     ScriptRuntime: PHP callables, Class::method and command-class
//     scripts, makeAutoloader's loader and the dispatch bracket.
//   - setup.go wires it all into a composer.Factory (cmd/maestro).
//   - bridge.go, objects.go, values.go and the mirror_*.go/svc_*.go files
//     are the Go side of the API in PHP: what maestro's objects cross as
//     (service proxies, package/event/operation/IO mirrors, link and
//     constraint values) and the `<area>.<method>` handlers the shim's
//     classes call (api.go registers them; coverage_test.go checks the
//     set against the shim's source).
//
// Custom installers (phase 3):
//
//   - svc_installer.go gives the shim's installer base classes their Go
//     peers and holds the proxies maestro's InstallationManager calls:
//     PHP for the methods a class overrides, the peer otherwise, with the
//     peer's own calls to overridable methods coming back through the
//     proxy (docs/PLUGINS.md §5.6, D9).
//   - promises.go bridges maestro's promises and PHP's React promises
//     both ways, pending ones included.
//
// Commands and the Symfony Console (phase 4):
//
//   - proxy_command.go holds the proxies maestro's Application lists,
//     describes, completes and runs for commands written in PHP (the
//     commands of CommandProvider capabilities, composer.json scripts
//     naming a Symfony Command class): they run in PHP on the vendored
//     symfony/console (`command.run`), from the description PHP gave when
//     they crossed. It also gives PHP maestro's Application (a
//     Composer\Console\Application whose Symfony part is PHP's over maestro's
//     commands) and commands (instances of their classes), and runs the
//     Applications PHP creates or runs (`app.*`), re-entrantly.
//   - svc_console.go makes maestro's inputs and outputs PHP mirrors
//     (vendored ArgvInput/ArrayInput/StringInput objects whose changes come
//     back, a ConsoleOutput on the inherited fds or a GoOutput writing
//     through maestro) and PHP's outputs maestro's (phpOutput); an input
//     created in PHP is adopted when it first crosses.
//   - php/src/Maestro/Shim/Console.php is the PHP half.
//
// Resolver-time and write APIs (phase 5):
//
//   - adopt_package.go and Runtime.adopt make objects PHP creates
//     maestro's: packages and operations when they first cross, services
//     (PlatformRepository, Locker, Cache, HttpDownloader, ...) when PHP
//     constructs them.
//   - svc_loader.go (ArrayLoader, ArrayDumper), svc_runinstaller.go
//     (Composer\Installer's run() and SuggestedPackagesReporter),
//     svc_http.go (HttpDownloader, RemoteFilesystem, Loop, DownloadManager,
//     Cache), svc_resolverevents.go (PRE_POOL_CREATE with its Request and
//     lazily loaded packages, PRE/POST_FILE_DOWNLOAD, transactions),
//     svc_selector.go (RepositorySet, VersionSelector) and the repository,
//     Locker, JSON and config source handlers of svc_repo.go,
//     svc_composer.go and svc_util.go.
//
// Internals emulation (phase 6):
//
//   - frames.go gives PHP code the frames Composer's stack would hold
//     (the Application, the running command, the Installer):
//     Maestro\Shim\Frames enters each through a closure bound to its
//     object, so debug_backtrace() finds them.
//   - internals.go makes maestro's services carry the protected and
//     private properties plugins read (the running Composer\Installer's
//     settings, EventDispatcher::$runScripts, Config::$baseDir, a
//     Transaction's packages) and serves clones of maestro's objects.
//   - proxy_repository.go lets maestro use repositories and downloaders
//     written in PHP; svc_downloader.go serves maestro's downloaders to
//     PHP and creates the ones PHP constructs.
//
// The remaining stubs (issue #1, docs/PLUGINS.md §8 "Remaining stubs"):
//
//   - svc_builtin.go runs maestro's own commands' hooks when PHP runs one
//     of them (`builtin.*`).
//   - proxy_io.go and proxy_filter.go let maestro use IOs and platform
//     requirement filters created in PHP, and hand Go functions to PHP as
//     callables (`callable.go`).
//   - svc_new.go builds maestro's services PHP constructs (Config,
//     AutoloadGenerator, the managers); svc_policy.go mirrors AuditConfig
//     and serves BaseCommand::createPolicyConfig().
//   - proxy_subdownloader.go and svc_filedownloader.go give FileDownloader
//     subclasses written in PHP their overrides and their parent's code;
//     svc_edinternals.go serves EventDispatcher's protected methods.
//
// # Differences from Composer that are not observable
//
// When PHP code calls exit() or dies of a fatal error mid-call, Go-side
// deferred cleanup (InstallationManager's runCleanup, say) still runs
// before maestro exits with PHP's status; Composer's process would end at
// once. That cleanup writes nothing.
package plugin
