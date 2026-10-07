#!/usr/bin/env bash
# Prints the Composer release maestro ports: http.ComposerVersion in
# internal/util/http/runtime.go, the one place it is named (devenv.nix's
# ref-sync and tools/upstream/open-issues.sh read it through this script).
#
# Usage (from the repo root): tools/upstream/composer-version.sh
set -euo pipefail

v=$(sed -n 's/^const ComposerVersion = "\([^"]*\)"$/\1/p' internal/util/http/runtime.go)
if [ -z "$v" ]; then
	echo 'composer-version.sh: no `const ComposerVersion = "..."` line in internal/util/http/runtime.go' >&2
	exit 1
fi
echo "$v"
