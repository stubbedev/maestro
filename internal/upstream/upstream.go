// Package upstream names the Composer release maestro ports, the one place
// it is named (docs/PORTING.md, "Following upstream"): Composer::VERSION
// and friends (internal/composer), the platform packages
// (internal/repository), the user agent (internal/util/http), the e2e phar
// (cmd/maestro) and devenv.nix's ref-sync all read it from here.
//
// tools/upstream/bump.sh rewrites these constants from the release itself
// (its phar and sources), together with the shim's copies and the docs, so
// keep each on its own line in this form.
package upstream

const (
	// ComposerVersion is Composer::VERSION.
	ComposerVersion = "2.10.3"
	// ComposerReleaseDate is Composer::RELEASE_DATE, as the phar has it.
	ComposerReleaseDate = "2026-08-27 13:34:23"
	// ComposerPharSHA256 is the sha256 of the release's official
	// composer.phar (https://getcomposer.org/download/<version>/composer.phar),
	// which the e2e tests compare maestro with.
	ComposerPharSHA256 = "7a2d379d5b8ffdaa028580ef26494c36d2feef4b178d3dd1473a4dbc5e17c8d6"
	// PluginAPIVersion is PluginInterface::PLUGIN_API_VERSION.
	PluginAPIVersion = "2.9.0"
	// RuntimeAPIVersion is Composer::RUNTIME_API_VERSION.
	RuntimeAPIVersion = "2.2.2"
)
