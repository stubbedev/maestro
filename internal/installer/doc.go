// Package installer ports Composer\Installer: InstallationManager
// (Manager), the Library, Plugin, Metapackage, Noop and Project installers,
// BinaryInstaller (the vendor/bin proxies) and SuggestedPackagesReporter.
// PackageEvent, InstallerEvent and their event names live in
// internal/eventdispatcher, which creates them.
//
// # Ordering and parallelism
//
// Composer runs on one thread: installer methods are called in operation
// order, and the then() callbacks of promises run either at once (settled
// promises) or while the loop waits. This package keeps that model (see
// Promise): every installer call, event dispatch and promise callback runs
// on the goroutine calling Manager.Execute, in Composer's order, so output
// and repository changes are deterministic and installers backed by PHP
// plugins (docs/PLUGINS.md §5.6, §5.14) are only ever called on the main
// flow. What runs in parallel is the downloaders' work behind the leaf
// promises: downloads and the extraction of dists into the package store
// and the vendor/composer staging directories during the download phase
// (which is where packages are placed), and removals during the execute
// phase.
//
// Where Composer's callbacks run in completion order (several asynchronous
// operations settling during one wait), they run here in operation order.
//
// # Plugins
//
// LibraryInstaller and the installers built on it call their overridable
// methods through Virtuals, so the plugin runtime can give a PHP subclass's
// overrides (getInstallPath, ...) to the inherited Go implementation.
// PluginInstaller reaches the plugin manager through PluginComposer.
package installer
