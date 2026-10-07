#!/usr/bin/env bash
# Writes the reference Composer's command list and help texts into
# internal/command/testdata, which TestHelp_List, TestHelp_ListJSON and
# TestHelp_All compare maestro with:
#
#   list.txt, list.json   `composer list` and `composer list --format=json`
#   help/<command>.txt    `composer help <command>`, for each command given,
#                         by default every command `composer list --raw`
#                         lists
#
# Run from the repository root inside the devenv shell:
#
#   tools/oracle/command/help.sh [command...]
#
# $_SERVER['PHP_SELF'] (shown by %command.full_name%) is "composer": the
# script runs Composer through a symlink of that name; the Go tests set
# console.ScriptName to the same. The completion command's help shows
# realpath(PHP_SELF), the reference's bin/composer, which a test cannot
# resolve: it becomes "composer" as maestro prints it then.
set -euo pipefail
root=$(pwd)
out=$root/internal/command/testdata
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
ln -s "$root/.ref/composer/bin/composer" "$tmp/composer"
mkdir -p "$tmp/home" "$out/help"

composer() {
	(cd "$tmp" && COMPOSER_HOME="$tmp/home" COMPOSER_NO_INTERACTION=1 NO_COLOR=1 COLUMNS=80 \
		php composer "$@" --no-ansi)
}

real=$(realpath "$root/.ref/composer/bin/composer")
composer list > "$out/list.txt"
composer list --format=json | sed "s#$(printf '%s' "$real" | sed 's#/#\\\\/#g')#composer#g" > "$out/list.json"

if [ $# -eq 0 ]; then
	set -- $(composer list --raw | awk '{print $1}')
fi
for cmd in "$@"; do
	composer help "$cmd" | sed "s#$real#composer#g" > "$out/help/$cmd.txt"
done
