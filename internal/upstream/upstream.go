// Package upstream names the Composer release maestro ports, the one place
// it is named (docs/PORTING.md, "Following upstream"): Composer::VERSION
// and friends (internal/composer), the platform packages
// (internal/repository), the user agent (internal/util/http), the e2e phar
// (cmd/maestro) and nix/shell.nix's ref-sync all read it from here.
//
// tools/upstream/bump.sh rewrites these constants from the release itself
// (its phar and sources), together with the shim's copies and the docs, so
// keep each on its own line in this form.
package upstream

const (
	// ComposerVersion is Composer::VERSION.
	ComposerVersion = "2.11.2"
	// ComposerReleaseDate is Composer::RELEASE_DATE, as the phar has it.
	ComposerReleaseDate = "2026-11-10 10:09:37"
	// ComposerPharSHA256 is the sha256 of the release's official
	// composer.phar (https://getcomposer.org/download/<version>/composer.phar),
	// which the e2e tests compare maestro with.
	ComposerPharSHA256 = "dd158c0afd41d15fac89e1b4e47b5d42911fe316c3687cd5edac98319b25d028"
	// PluginAPIVersion is PluginInterface::PLUGIN_API_VERSION.
	PluginAPIVersion = "2.11.0"
	// RuntimeAPIVersion is Composer::RUNTIME_API_VERSION.
	RuntimeAPIVersion = "2.2.2"
)
