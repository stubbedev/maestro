#!/usr/bin/env bash
# Differential oracle for failing invocations: runs the reference Composer
# and maestro on every scenario of internal/command/testdata/errors at
# default, -v, -vv and -vvv verbosity, and compares what docs/PORTING.md's
# contract freezes: the exit code and stdout, exactly. How errors are
# rendered is free (#13); the error messages Composer reports are checked
# by TestErrors (internal/command/errorstest), which reads them from the
# goldens this script records. Run from the repository root inside the
# devenv shell:
#
#   tools/oracle/errors/errors.sh [-k] [-w] [scenario...]
#
# MAESTRO names the maestro binary (default: built from ./cmd/maestro into
# a temporary directory). ORACLE_COMPOSER is bin/composer of a checkout of
# Composer's sources (default: .ref/composer/bin/composer, which devenv's
# ref-sync checks out); the script fails unless it is the release maestro
# ports (tools/upstream/composer-version.sh). A phar is refused: it adds
# lines of its own to diagnose's output.
# -k keeps the work directory and prints its path. -w also writes
# Composer's normalised output of each run to the scenario's goldens:
#
#   <verbosity>.txt     stdout and stderr together, then "exit N"
#   <verbosity>.stdout  stdout alone, only when Composer wrote any
#
# (verbosity: default, v, vv, vvv), which TestErrors (internal/command/
# errorstest) compares maestro with in-process, without php's Composer or
# this script.
#
# A scenario is a directory holding:
#   project/  the working directory (copied fresh for every run)
#   base      optional names of directories of _projects/ copied into the
#             working directory first, in order, project/ (then optional)
#             over them: projects scenarios share
#   args      the command line, one argument per line
#   env       optional KEY=VALUE lines
#   home/     optional COMPOSER_HOME contents
#   messages  optional lines of Composer's output that report the error,
#             checked by TestErrors instead of the exception boxes' messages
#   readonly  optional paths of the working directory made read-only
#             before the run (the repository can't record file modes)
#   files     optional paths of the working directory whose content after
#             the run is frozen: -w records Composer's into after/<path>
#             (normalised as the output is; no after/<path> when the run
#             leaves no such file), which maestro's must equal
# @SERVER@ in env and in the files of project/ and home/ is the local HTTP
# server's URL.
# The local HTTP server (tools/oracle/errors/router.php, `php -S`) serves
# internal/command/testdata/errors/_server.
#
# Composer's output is normalised by tools/oracle/errors/normalize, which
# is testutil.NormalizeOracle, the normalisation TestErrors applies to
# maestro's: the scenario's temporary paths, the server port, where
# Composer's sources are (@COMPOSER@), what differs between runs or
# machines (random names, timings, what depends on the machine's php, the
# CA bundle, php.ini and archive tools it finds) and the banner heading the
# output of `list` (docs/PORTING.md deviation 8). COMPOSER_TEST_SUITE=1
# keeps the random cache garbage collection from running: one run in 51
# otherwise creates the files cache directory, which changes clear-cache's
# output.
set -uo pipefail
root=$(pwd)
data=$root/internal/command/testdata/errors
keep=0
write=0
while [ $# -gt 0 ]; do
	case "$1" in
	-k) keep=1; shift ;;
	-w) write=1; shift ;;
	*) break ;;
	esac
done

work=$(mktemp -d /tmp/maestro-errors.XXXXXX)
cleanup() { [ -n "${srvpid:-}" ] && kill "$srvpid" 2>/dev/null; [ $keep = 1 ] && echo "work: $work" || rm -rf "$work"; }
trap cleanup EXIT

if [ -n "${MAESTRO:-}" ]; then
	maestro=$(realpath "$MAESTRO") || exit 1
else
	maestro=$work/maestro
	go build -o "$maestro" ./cmd/maestro || exit 1
fi

entry=${ORACLE_COMPOSER:-$root/.ref/composer/bin/composer}
src=$(realpath "$(dirname "$entry")/..")
[ -f "$entry" ] && [ -f "$src/src/Composer/Composer.php" ] || { echo "$entry is not bin/composer of a checkout of Composer's sources (run ref-sync, or set ORACLE_COMPOSER)" >&2; exit 1; }
# Composer runs from a copy in the work directory, whose path has the same
# length on every machine: error boxes wrap the paths of Composer's files
# they show.
croot=$work/composer
cp -a "$src/." "$croot" || exit 1
composer=$croot/bin/composer
release=$("$root/tools/upstream/composer-version.sh") || exit 1
case "$(php "$composer" --version --no-ansi 2>/dev/null)" in
"Composer version $release "*) ;;
*) echo "$entry is not Composer $release (tools/upstream/composer-version.sh)" >&2; exit 1 ;;
esac

port=$(php -r '$s=stream_socket_server("tcp://127.0.0.1:0");echo explode(":",stream_socket_get_name($s,false))[1];')
php -S 127.0.0.1:"$port" -t "$data/_server" "$root/tools/oracle/errors/router.php" >/dev/null 2>&1 &
srvpid=$!
for _ in $(seq 50); do php -r 'exit(@fsockopen("127.0.0.1",'"$port"')?0:1);' && break; sleep 0.1; done
server=http://127.0.0.1:$port

if [ $# -eq 0 ]; then
	set -- $(cd "$data" && ls -d */ | sed 's#/##' | grep -v '^_')
fi

normalizer=$work/normalize
go build -o "$normalizer" ./tools/oracle/errors/normalize || exit 1
normalize() {
	"$normalizer" -dir "$work/run" -server "127.0.0.1:$port" -composer "$croot"
}

# run name bin out verbosity separate: runs the scenario and writes <out>.n,
# its normalised output (stdout and stderr together, or with separate=1
# stdout alone) followed by "exit N"; with separate=1 also <out>.stdout.n,
# the normalised stdout alone
run() {
	local name=$1 bin=$2 out=$3 vflag=$4 separate=$5 dir=$data/$1
	rm -rf "$work/run"
	mkdir -p "$work/run/home" "$work/run/cache"
	mkdir -p "$work/run/p"
	if [ -f "$dir/base" ]; then
		while IFS= read -r l; do [ -n "$l" ] && cp -a "$data/_projects/$l/." "$work/run/p/"; done < "$dir/base"
	fi
	[ -d "$dir/project" ] && cp -a "$dir/project/." "$work/run/p/"
	[ -d "$dir/home" ] && cp -a "$dir/home/." "$work/run/home/"
	grep -rlZF @SERVER@ "$work/run/p" "$work/run/home" | xargs -0r sed -i "s#@SERVER@#$server#g"
	if [ -f "$dir/readonly" ]; then
		while IFS= read -r l; do [ -n "$l" ] && chmod a-w "$work/run/p/$l"; done < "$dir/readonly"
	fi
	local -a args=() envs=()
	mapfile -t args < "$dir/args"
	[ -n "$vflag" ] && args+=("$vflag")
	if [ -f "$dir/env" ]; then
		while IFS= read -r l; do [ -n "$l" ] && envs+=("${l//@SERVER@/$server}"); done < "$dir/env"
	fi
	(
		cd "$work/run/p" || exit
		exec 3> "$out"
		if [ "$separate" = 1 ]; then exec 4> /dev/null; else exec 4>&3; fi
		env -i PATH="$PATH" HOME="$work/run/home" COMPOSER_HOME="$work/run/home" \
			COMPOSER_CACHE_DIR="$work/run/cache" COMPOSER_NO_INTERACTION=1 NO_COLOR=1 COLUMNS=80 \
			COMPOSER_TEST_SUITE=1 \
			"${envs[@]}" $bin "${args[@]}" --no-ansi >&3 2>&4
		echo "exit $?" > "$out.exit"
	)
	rm -rf "$out.files"
	if [ -f "$dir/files" ]; then
		while IFS= read -r l; do
			[ -n "$l" ] && [ -f "$work/run/p/$l" ] && mkdir -p "$(dirname "$out.files/$l")" && normalize < "$work/run/p/$l" > "$out.files/$l"
		done < "$dir/files"
		mkdir -p "$out.files"
	fi
	if [ "$separate" = 1 ]; then
		normalize < "$out" > "$out.stdout.n"
	fi
	cat "$out" "$out.exit" | normalize > "$out.n"
}

fail=0
total=0
for name in "$@"; do
	for v in "" -v -vv -vvv; do
		total=$((total + 1))
		golden=$data/$name/${v#-}
		[ -z "$v" ] && golden=$data/$name/default
		run "$name" "php $composer" "$work/c.out" "$v" 1
		if [ $write = 1 ]; then
			run "$name" "php $composer" "$work/c.all" "$v" 0
			cp "$work/c.all.n" "$golden.txt"
			rm -f "$golden.stdout"
			[ -s "$work/c.out.stdout.n" ] && cp "$work/c.out.stdout.n" "$golden.stdout"
			if [ -z "$v" ] && [ -d "$work/c.out.files" ]; then
				rm -rf "$data/$name/after"
				cp -a "$work/c.out.files" "$data/$name/after"
			fi
		fi
		run "$name" "$maestro" "$work/m.out" "$v" 1
		if ! diff -u "$work/c.out.n" "$work/m.out.n" > "$work/diff"; then
			fail=$((fail + 1))
			echo "DIFF $name ${v:-(default)} (stdout, exit code)"
			sed 's/^/    /' "$work/diff" | head -${DIFFLINES:-40}
		elif [ -d "$work/c.out.files" ] && ! diff -ru "$work/c.out.files" "$work/m.out.files" > "$work/diff"; then
			fail=$((fail + 1))
			echo "DIFF $name ${v:-(default)} (files)"
			sed 's/^/    /' "$work/diff" | head -${DIFFLINES:-40}
		fi
	done
done
echo "$((total - fail))/$total with Composer's exit code, stdout and files"
[ $fail = 0 ]
