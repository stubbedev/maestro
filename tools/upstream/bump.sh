#!/usr/bin/env bash
# Moves the pin to another Composer release: downloads its official phar,
# checks it against the sha256 getcomposer.org publishes, reads
# Composer::VERSION, RELEASE_DATE, RUNTIME_API_VERSION and
# PluginInterface::PLUGIN_API_VERSION out of it (without running it), and
# rewrites everything that names them: internal/upstream/upstream.go (which
# the Go code, the e2e tests and ref-sync read), the shim's copies of the
# constants (and its MANIFEST) and the docs that name the release. Then it
# runs the tests that check those copies. Porting the release's changes is
# what remains (docs/PORTING.md, "Following upstream").
#
# Needs curl, php, perl and go. `just bump-composer <version>` runs it.
#
# Usage: tools/upstream/bump.sh <version>
set -euo pipefail

new=${1:?usage: tools/upstream/bump.sh <version>}
root=$(cd "$(dirname "$0")/../.." && pwd)
cd "$root"
version() { tools/upstream/composer-version.sh "$@"; }

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

url=https://getcomposer.org/download/$new/composer.phar
curl -sSfL -o "$work/composer.phar" "$url"
published=$(curl -sSfL "$url.sha256sum" | cut -d' ' -f1)
sha=$(sha256sum "$work/composer.phar" | cut -d' ' -f1)
if [ "$sha" != "$published" ]; then
	echo "bump.sh: $url has sha256 $sha, getcomposer.org publishes $published" >&2
	exit 1
fi

# NAME<TAB>value of the constants, read from the phar's sources
read_constants() {
	php -r '
		$p = "phar://" . $argv[1];
		$c = file_get_contents("$p/src/Composer/Composer.php") . file_get_contents("$p/src/Composer/Plugin/PluginInterface.php");
		foreach (["VERSION", "RELEASE_DATE", "RUNTIME_API_VERSION", "PLUGIN_API_VERSION"] as $name) {
			if (!preg_match("/const $name = \x27([^\x27]*)\x27/", $c, $m)) { fwrite(STDERR, "no $name in the phar\n"); exit(1); }
			echo $name, "\t", $m[1], "\n";
		}' "$work/composer.phar"
}
declare -A phar
while IFS=$'\t' read -r name value; do phar[$name]=$value; done < <(read_constants)
if [ "${phar[VERSION]}" != "$new" ]; then
	echo "bump.sh: the $new phar says it is ${phar[VERSION]}" >&2
	exit 1
fi

# the docs name the release, its date and API versions as the old ones
declare -A old=(
	[version]=$(version) [date]=$(version ComposerReleaseDate)
	[plugin]=$(version PluginAPIVersion) [runtime]=$(version RuntimeAPIVersion)
)

# set FILE PATTERN VALUE: the quoted value on the line PATTERN starts
set_value() {
	NAME="$2" VALUE="$3" perl -pi -e 's/^(\s*\Q$ENV{NAME}\E\s*=\s*)(["\x27])[^"\x27]*\2/$1$2$ENV{VALUE}$2/' "$1"
}

go=internal/upstream/upstream.go
set_value "$go" ComposerVersion "$new"
set_value "$go" ComposerReleaseDate "${phar[RELEASE_DATE]}"
set_value "$go" ComposerPharSHA256 "$sha"
set_value "$go" PluginAPIVersion "${phar[PLUGIN_API_VERSION]}"
set_value "$go" RuntimeAPIVersion "${phar[RUNTIME_API_VERSION]}"

shim=internal/plugin/php
set_value "$shim/src/Composer/Composer.php" "public const VERSION" "$new"
set_value "$shim/src/Composer/Composer.php" "public const RELEASE_DATE" "${phar[RELEASE_DATE]}"
set_value "$shim/src/Composer/Composer.php" "public const RUNTIME_API_VERSION" "${phar[RUNTIME_API_VERSION]}"
set_value "$shim/stubs/Composer/Plugin/PluginInterface.php" "public const PLUGIN_API_VERSION" "${phar[PLUGIN_API_VERSION]}"

# replace OLD NEW FILE...: OLD where it stands alone (not inside a longer
# version), in the docs that state what maestro ports
replace() {
	OLD="$1" NEW="$2" perl -pi -e 's/(?<![\d.])\Q$ENV{OLD}\E(?![\d])/$ENV{NEW}/g' "${@:3}"
}
docs=(README.md docs/PORTING.md docs/PLUGINS.md)
replace "${old[version]}" "$new" "${docs[@]}"
replace "${old[date]}" "${phar[RELEASE_DATE]}" "${docs[@]}"
replace "'${old[plugin]}'" "'${phar[PLUGIN_API_VERSION]}'" "${docs[@]}"
replace "'${old[runtime]}'" "'${phar[RUNTIME_API_VERSION]}'" "${docs[@]}"

go generate ./internal/plugin # the shim's MANIFEST
go test ./internal/upstream ./internal/composer ./internal/plugin -run 'TestUpstream|TestComposer_GetVersion|TestShim_PinnedVersions|TestShim_ManifestIsCurrent'
echo "Composer ${old[version]} -> $new (${phar[RELEASE_DATE]}, plugin API ${phar[PLUGIN_API_VERSION]}, runtime API ${phar[RUNTIME_API_VERSION]})"
