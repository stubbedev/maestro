#!/usr/bin/env bash
# Differential oracle for exception rendering and verbose startup output:
# runs the reference Composer (.ref/composer/bin/composer) and maestro on
# every scenario of internal/command/testdata/errors at default, -v, -vv
# and -vvv verbosity, and compares stdout+stderr and the exit code byte for
# byte. Run from the repository root inside the devenv shell:
#
#   tools/oracle/errors/errors.sh [-k] [-w] [scenario...]
#
# MAESTRO names the maestro binary (default: built from ./cmd/maestro into
# a temporary directory). -k keeps the work directory and prints its path.
# -w also writes Composer's normalised output of each run to the
# scenario's golden file <verbosity>.txt (default, v, vv, vvv), which
# TestErrorRendering (internal/command/errorstest) compares maestro with
# in-process, without php's Composer or this script.
#
# A scenario is a directory holding:
#   project/  the working directory (copied fresh for every run)
#   args      the command line, one argument per line
#   env       optional KEY=VALUE lines (@SERVER@ is the local HTTP server)
#   home/     optional COMPOSER_HOME contents
# The local HTTP server (tools/oracle/errors/router.php, `php -S`) serves
# internal/command/testdata/errors/_server.
#
# Normalised before comparing: the scenario's temporary paths and the
# server port, the machine in the -vvv "Running ... with PHP" line, the
# random cache garbage collection (COMPOSER_TEST_SUITE=1 also keeps it from
# running: Cache::gcIsNecessary, one run in 51 otherwise, creates the files
# cache directory, which changes clear-cache's output) and temporary archive
# directory names,
# what depends on the machine's php (its version, and the pool and rule
# counts, which include one platform package per loaded extension), the
# resolution time, and the PHP call stack lines of "Exception trace:" (Composer
# prints its own PHP frames with absolute paths there; maestro prints the
# throw site only), whose "at" line is reduced to the file's basename.
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

maestro=${MAESTRO:-}
if [ -z "$maestro" ]; then
	maestro=$work/maestro
	go build -o "$maestro" ./cmd/maestro || exit 1
fi

port=$(php -r '$s=stream_socket_server("tcp://127.0.0.1:0");echo explode(":",stream_socket_get_name($s,false))[1];')
php -S 127.0.0.1:"$port" -t "$data/_server" "$root/tools/oracle/errors/router.php" >/dev/null 2>&1 &
srvpid=$!
for _ in $(seq 50); do php -r 'exit(@fsockopen("127.0.0.1",'"$port"')?0:1);' && break; sleep 0.1; done
server=http://127.0.0.1:$port

if [ $# -eq 0 ]; then
	set -- $(cd "$data" && ls -d */ | sed 's#/##' | grep -v '^_')
fi

normalize() {
	sed -E \
		-e "s#$work/run#@DIR@#g" \
		-e "s#127\.0\.0\.1:$port#@SERVER@#g" \
		-e 's#^(  at )(/|phar://)[^ ]*/([^/ ]+:([0-9]+|n/a))$#\1\3#' \
		-e '/^ [^ ]+ at (\/|phar:\/\/)[^ ]*:[0-9]+$/d' \
		-e '/^Running cache garbage collection$/d' \
		-e 's/^(Running [^ ]+ \([^)]*\) with PHP ).* on .*$/\1@PHP@ on @OS@/' \
		-e 's#/tmp/composer_archive[0-9a-f]+#/tmp/composer_archive@RAND@#g' \
		-e 's/^(Memory usage: )[0-9.]+MiB \(peak: [0-9.]+MiB\), time: [0-9.]+s$/\1@PROFILE@/' \
		-e 's/^(Analyzed )[0-9]+( (packages|rules) to resolve dependencies)$/\1@N@\2/' \
		-e 's/^(Dependency resolution completed in )[0-9.]+( seconds)$/\1@TIME@\2/' \
		-e 's/(but your php version \()[^)]*(\) does not satisfy)/\1@PHPVERSION@\2/'
}

run() { # name bin out verbosity
	local name=$1 bin=$2 out=$3 vflag=$4 dir=$data/$1
	rm -rf "$work/run"
	mkdir -p "$work/run/home" "$work/run/cache"
	cp -a "$dir/project" "$work/run/p"
	[ -d "$dir/home" ] && cp -a "$dir/home/." "$work/run/home/"
	local -a args=() envs=()
	mapfile -t args < "$dir/args"
	[ -n "$vflag" ] && args+=("$vflag")
	if [ -f "$dir/env" ]; then
		while IFS= read -r l; do [ -n "$l" ] && envs+=("${l//@SERVER@/$server}"); done < "$dir/env"
	fi
	(
		cd "$work/run/p" &&
			env -i PATH="$PATH" HOME="$work/run/home" COMPOSER_HOME="$work/run/home" \
				COMPOSER_CACHE_DIR="$work/run/cache" COMPOSER_NO_INTERACTION=1 NO_COLOR=1 COLUMNS=80 \
				COMPOSER_TEST_SUITE=1 \
				"${envs[@]}" $bin "${args[@]}" --no-ansi > "$out" 2>&1
		echo "exit $?" >> "$out"
	)
	normalize < "$out" > "$out.n"
}

fail=0
total=0
for name in "$@"; do
	for v in "" -v -vv -vvv; do
		total=$((total + 1))
		run "$name" "php $root/.ref/composer/bin/composer" "$work/c.out" "$v"
		if [ $write = 1 ]; then
			cp "$work/c.out.n" "$data/$name/${v#-}.txt"
			[ -z "$v" ] && mv "$data/$name/.txt" "$data/$name/default.txt"
		fi
		run "$name" "$maestro" "$work/m.out" "$v"
		if ! diff -u "$work/c.out.n" "$work/m.out.n" > "$work/diff"; then
			fail=$((fail + 1))
			echo "DIFF $name ${v:-(default)}"
			sed 's/^/    /' "$work/diff" | head -${DIFFLINES:-40}
		fi
	done
done
echo "$((total - fail))/$total identical"
[ $fail = 0 ]
