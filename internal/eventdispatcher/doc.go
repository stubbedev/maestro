// Package eventdispatcher ports Composer\EventDispatcher and the event
// classes (Composer\Script\Event, Installer\PackageEvent and
// InstallerEvent, the Plugin\* events), which live here because the
// dispatcher creates them and every package dispatching them imports this
// one. internal/script holds the ScriptEvents constants.
//
// Shell, @php, @putenv, @composer and @script listeners run in Go, as
// ported. PHP code (plugin callables, Class::method scripts, command
// classes) runs through a ScriptRuntime (the plugin runtime of
// docs/PLUGINS.md), which is only needed when such a listener is reached.
// Composer\Config::disableProcessTimeout is run natively, so the common
// `"Composer\\Config::disableProcessTimeout"` script starts no php.
//
// makeAutoloader builds the class loader when Composer would, including the
// generator's output, but hands it to the runtime just before its next PHP
// call rather than registering it at once.
package eventdispatcher
