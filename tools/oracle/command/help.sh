#!/usr/bin/env bash
# Writes the reference Composer's `help <command>` output for each command
# given into internal/command/testdata/help/<command>.txt. Run from the
# repository root inside the devenv shell:
#
#   tools/oracle/command/help.sh config
#
# $_SERVER['PHP_SELF'] (shown by %command.full_name%) is "composer": the
# script runs Composer through a symlink of that name; the Go tests set
# console.ScriptName to the same.
set -euo pipefail
root=$(pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
ln -s "$root/.ref/composer/bin/composer" "$tmp/composer"
mkdir -p "$tmp/home" "$root/internal/command/testdata/help"
# "list" is written from `composer list` (the command list, also as
# list.json from --format=json), not `help list`. The completion command's
# help shows realpath(PHP_SELF), the reference's bin/composer, which a test
# cannot resolve: it becomes "composer" as maestro prints it then.
for cmd in "$@"; do
	if [ "$cmd" = list ]; then
		(cd "$tmp" && COMPOSER_HOME="$tmp/home" COMPOSER_NO_INTERACTION=1 NO_COLOR=1 COLUMNS=80 \
			php composer list --no-ansi) > "$root/internal/command/testdata/help/list.txt"
		(cd "$tmp" && COMPOSER_HOME="$tmp/home" COMPOSER_NO_INTERACTION=1 NO_COLOR=1 COLUMNS=80 \
			php composer list --format=json --no-ansi) |
			sed "s#$(printf '%s' "$root/.ref/composer/bin/composer" | sed 's#/#\\\\/#g')#composer#g" > "$root/internal/command/testdata/help/list.json"
		continue
	fi
	(cd "$tmp" && COMPOSER_HOME="$tmp/home" COMPOSER_NO_INTERACTION=1 NO_COLOR=1 COLUMNS=80 \
		php composer help "$cmd" --no-ansi) > "$root/internal/command/testdata/help/$cmd.txt"
done
