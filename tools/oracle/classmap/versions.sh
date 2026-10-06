#!/usr/bin/env bash
# Regenerates internal/classmap/testdata/oracle/versions.{bin,golden}: what
# php_strip_whitespace() and PhpFileParser::findClasses() return for the
# cases of versions.php under every PHP minor version Composer 2.10 runs on
# (7.2 to 8.5), with short_open_tag On and Off. The PHPs are the official
# php:X.Y-cli Docker images (the latest patch release of each minor
# version); run from the repository root, with docker, a local php and
# .ref/composer.
#
# versions.golden starts with a "#" line naming the columns (PHP_VERSION
# and short_open_tag, "on" or "off"), then has one line per case: the
# record (see versions.php) of each column, or "=" when it is the previous
# column's.
#
#   tools/oracle/classmap/versions.sh [seed count outdir]
#
# writes another random set into outdir instead, for
# MAESTRO_ORACLE_VERSIONS=outdir go test ./internal/classmap -run Versions.
set -euo pipefail

versions=(7.2 7.3 7.4 8.0 8.1 8.2 8.3 8.4 8.5)
out=${3:-internal/classmap/testdata/oracle}
composer=$(realpath .ref/composer)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

cp tools/oracle/classmap/versions.php "$tmp/"
php "$tmp/versions.php" cases "$tmp/cases.bin" ${1:+"$1"} ${2:+"$2"}

header="#"
columns=()
for tag in on off; do
	flag=1
	[ "$tag" = off ] && flag=0
	for v in "${versions[@]}"; do
		full=$(docker run --rm "php:$v-cli" php -r 'echo PHP_VERSION;')
		docker run --rm -v "$tmp:/w" -v "$composer:/composer:ro" "php:$v-cli" \
			php -d short_open_tag=$flag /w/versions.php record /w/cases.bin /composer > "$tmp/$v-$tag.txt"
		header+=" $full/$tag"
		columns+=("$tmp/$v-$tag.txt")
	done
done

cp "$tmp/cases.bin" "$out/versions.bin"
{
	echo "$header"
	paste -d' ' "${columns[@]}" | awk '{
		line = $1
		for (i = 2; i <= NF; i++) line = line " " ($i == $(i-1) ? "=" : $i)
		print line
	}'
} > "$out/versions.golden"
