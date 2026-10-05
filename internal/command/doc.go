// Package command ports Composer\Console\Application and Composer\Command:
// the application (application.go), BaseCommand, BaseConfigCommand,
// BaseDependencyCommand, CompletionTrait and PackageDiscoveryTrait, and one
// file per command. Commands register themselves in Composer's order
// (registry.go); internal/command/commandtest holds the test drivers of
// Composer's TestCase and Symfony's ApplicationTester.
//
// Version: the Application is "Composer" at Composer::getVersion() with
// Composer's release date, so `--version` prints exactly Composer's first
// line; Application::doRun's stderr lines follow, plus a last stderr line
// "maestro version X" (X is cmd/maestro's stamped version).
//
// Plugins: commands of CommandProvider capabilities come from the
// Composer's PluginManager when it implements PluginCommandProvider, and
// composer.json scripts naming a Symfony Command class from
// ScriptCommandProvider (both the plugin runtime's); without them there are
// no plugin commands and every script is a ScriptAliasCommand. Errors
// implementing ExitCoder (PHP exit() in plugin code) end the run with their
// code, unrendered.
//
// Divergences:
//   - Exceptions are rendered as Symfony does from their PHP class and throw
//     site. Errors raised here carry Composer's file and line (Error);
//     JSON parse and schema errors get theirs from knownThrowSite; other
//     errors of lower packages that do not implement console.Throwable
//     render as "In n/a line n/a:" (the message box is identical).
//   - --profile reports Go's memory statistics and timing.
//   - The xdebug and PHP version warnings follow the PHP Composer would run
//     on (internal/platform's ComposerView); there are no dev-build warnings.
//   - Registering the project's autoloader for command class scripts is the
//     plugin runtime's job (Application::getComposer(false) is still called
//     for its side effects).
//   - self-update updates maestro from the GitHub releases of
//     github.com/stubbedev/maestro (docs/PORTING.md deviation 4, see
//     SelfUpdateCommand); clear-cache also prunes maestro's package store
//     with the files cache, silently.
package command
