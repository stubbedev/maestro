#!/usr/bin/env bash
# Fails when a function is unreachable even from the tests on every
# platform maestro is built for (golang.org/x/tools/cmd/deadcode -test),
# except the entries tools/deadcode/allow.txt keeps on purpose. A function
# only one platform's build or tests reach (Windows' process code, ...)
# is live.
#
# Usage (from the repo root; `just deadcode` runs it in the dev container):
#   tools/deadcode/check.sh
set -euo pipefail

allow=tools/deadcode/allow.txt
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# built for this machine: GOOS below picks the platform it analyses
GOBIN="$work" go install golang.org/x/tools/cmd/deadcode@v0.51.0
deadcode="$work/deadcode"

# "path: unreachable func: Name", without the position, which moves with
# every edit above it
for goos in linux darwin windows; do
    GOOS=$goos $deadcode -test ./... | sed -E 's/^([^:]+):[0-9]+:[0-9]+:/\1:/' | sort -u > "$work/$goos"
done
comm -12 "$work/linux" "$work/darwin" | comm -12 - "$work/windows" > "$work/dead"
{ grep -v -e '^#' -e '^$' "$allow" || true; } | sort -u > "$work/allow"

comm -23 "$work/dead" "$work/allow" > "$work/new"
comm -13 "$work/dead" "$work/allow" > "$work/stale"

status=0
if [ -s "$work/new" ]; then
    echo "Unreachable even from the tests; delete it (or, if it must stay, add it to $allow with why):"
    sed 's/^/  /' "$work/new"
    status=1
fi
if [ -s "$work/stale" ]; then
    echo "Listed in $allow but reachable (or gone); remove the entry:"
    sed 's/^/  /' "$work/stale"
    status=1
fi

exit $status
