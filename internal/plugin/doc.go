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
// # Differences from Composer that are not observable
//
// When PHP code calls exit() or dies of a fatal error mid-call, Go-side
// deferred cleanup (InstallationManager's runCleanup, say) still runs
// before maestro exits with PHP's status; Composer's process would end at
// once. That cleanup writes nothing.
package plugin
