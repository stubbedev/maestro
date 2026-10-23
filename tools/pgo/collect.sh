#!/usr/bin/env bash
# Regenerates cmd/maestro/default.pgo, the CPU profile `go build` uses for
# profile-guided optimisation of ./cmd/maestro. It builds the profiling
# binary (-tags maestro_profile), creates the laravel and symfony projects
# of docs/BENCHMARKS.md in a temporary directory with caches of their own,
# then profiles every run of rounds × (update --dry-run offline and warm,
# no-op install, dump-autoload -o, warm install) and merges the profiles.
# The offline update runs at -v, where an update always resolves (the
# resolution cache is not used): it profiles the resolver, the warm one
# the kept resolution's path.
# Needs php and the network.
#
# Usage (from the repo root; `just pgo` runs it):
#   tools/pgo/collect.sh [rounds]
set -euo pipefail

rounds=${1:-6}
root=$(pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

export COMPOSER_HOME=$work/home COMPOSER_CACHE_DIR=$work/cache MAESTRO_CACHE_DIR=$work/mcache
export COMPOSER_TEST_SUITE=1 COMPOSER_NO_INTERACTION=1
flags=(--no-plugins --no-scripts)
bin=$work/maestro
go build -tags maestro_profile -o "$bin" ./cmd/maestro

cd "$work"
"$bin" create-project laravel/laravel laravel v13.10.1 "${flags[@]}" -q
"$bin" create-project symfony/skeleton:v7.4.99 symfony "${flags[@]}" -q
(cd symfony && "$bin" require symfony/webapp-pack "${flags[@]}" -q)

mkdir profiles
n=0
# run profiles the command in a project; the arguments after the
# directory end with the verbosity flag.
run() {
	local dir=$1
	shift
	n=$((n + 1))
	(cd "$work/$dir" && MAESTRO_CPUPROFILE=$work/profiles/$n.prof "$bin" "$@" "${flags[@]}" >/dev/null 2>&1)
}
for _ in $(seq "$rounds"); do
	for p in laravel symfony; do
		COMPOSER_DISABLE_NETWORK=1 run "$p" update --dry-run -v
		run "$p" update --dry-run -q
		run "$p" install -q
		run "$p" dump-autoload -o -q
		rm -rf "fresh-$p" && mkdir "fresh-$p"
		cp "$p/composer.json" "$p/composer.lock" "fresh-$p/"
		run "fresh-$p" install -q
	done
done

go tool pprof -proto profiles/*.prof >"$root/cmd/maestro/default.pgo"
echo "wrote cmd/maestro/default.pgo from $n profiles"
