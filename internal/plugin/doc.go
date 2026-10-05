// Package plugin ports Composer\Plugin and hosts the bridge to PHP that
// Composer plugins and PHP-callable scripts run through (deviation 5 in
// docs/PORTING.md; docs/PLUGINS.md is the specification).
//
// Phase 1 (this package today) is the runtime and its protocol, with no
// Composer API yet:
//
//   - shim.go embeds the PHP shim (php/: bootstrap.php, the Maestro\Shim
//     runtime, the presence-parity stubs of tools/shimgen and the
//     libraries tools/shimvendor vendors) and extracts it into
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
// Later phases plug in through Runtime.Handle (PHP → Go services, one
// handler per `<area>.<method>`, docs/PLUGINS.md §6.6),
// Runtime.RegisterMirrorFactory and rpc.Mirror (data mirrors),
// Runtime.RegisterTag and rpc.ValueEncoder (constraint and link values),
// Runtime.Call (Go → PHP methods, §6.5), and Options.Statics (Composer's
// statics, owned by internal/composer).
//
// # Differences from Composer that are not observable
//
// When PHP code calls exit() or dies of a fatal error mid-call, Go-side
// deferred cleanup (InstallationManager's runCleanup, say) still runs
// before maestro exits with PHP's status; Composer's process would end at
// once. That cleanup writes nothing.
package plugin
