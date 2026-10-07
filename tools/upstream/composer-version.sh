#!/usr/bin/env bash
# Prints a constant of the Composer release maestro ports, from
# internal/upstream/upstream.go, the one place it is named: ComposerVersion
# by default, or the constant named (ComposerPharSHA256, ...). Scripts that
# need the release (devenv.nix's ref-sync, the oracles, tools/upstream/*)
# read it through this.
#
# Usage: tools/upstream/composer-version.sh [Constant]
set -euo pipefail

name=${1:-ComposerVersion}
file="$(cd "$(dirname "$0")/../.." && pwd)/internal/upstream/upstream.go"
v=$(sed -n "s/^[[:space:]]*$name = \"\\([^\"]*\\)\"\$/\\1/p" "$file")
if [ -z "$v" ]; then
	echo "composer-version.sh: no $name = \"...\" line in $file" >&2
	exit 1
fi
echo "$v"
